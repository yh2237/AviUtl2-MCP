package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

// startFakeBridge serves a loopback bridge socket that answers with handler output.
func startFakeBridge(t *testing.T, handler func(method string, params json.RawMessage) (any, error)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				for {
					payload, err := protocol.ReadFrame(conn)
					if err != nil {
						return
					}
					var request protocol.Request
					if err := json.Unmarshal(payload, &request); err != nil {
						return
					}
					response := protocol.Response{ID: request.ID, Version: protocol.Version}
					result, callErr := handler(request.Method, request.Params)
					if callErr != nil {
						response.Error = &protocol.Error{Code: "HOST_ERROR", Message: callErr.Error()}
					} else if encoded, err := json.Marshal(result); err == nil {
						response.Result = encoded
					}
					encoded, err := json.Marshal(response)
					if err != nil || protocol.WriteFrame(conn, encoded) != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func connectTestSession(t *testing.T, address string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	server := New(bridge.NewClient(address, 2*time.Second), "test")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestToolListIncludesSceneOrientationAndLayerColors(t *testing.T) {
	tools, err := connectTestSession(t, "127.0.0.1:1").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = tool
	}
	for _, want := range []string{"inspect_scene", "set_layer_colors", "inspect_timeline", "execute_batch"} {
		if names[want] == nil {
			available := make([]string, 0, len(names))
			for name := range names {
				available = append(available, name)
			}
			sort.Strings(available)
			t.Fatalf("tool %q is missing; registered tools: %v", want, available)
		}
	}
	if names["set_layer_colors"].InputSchema == nil {
		t.Fatal("set_layer_colors has no input schema")
	}
}

func TestToolSchemasStayCompact(t *testing.T) {
	tools, err := connectTestSession(t, "127.0.0.1:1").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) < 80 {
		t.Fatalf("only %d tools were registered", len(tools.Tools))
	}
	total := 0
	for _, tool := range tools.Tools {
		if tool.OutputSchema != nil {
			t.Fatalf("tool %q declares an output schema; results travel as structuredContent so tools/list stays small", tool.Name)
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("tool %q input schema: %v", tool.Name, err)
		}
		total += len(encoded)
	}
	if total > 64<<10 {
		t.Fatalf("input schemas total %d bytes; keep tools/list below 64 KiB so clients do not spend their context on tool definitions", total)
	}
}

func TestInspectSceneToolRoundTrip(t *testing.T) {
	firstFrame, lastFrame := 0, 59
	address := startFakeBridge(t, func(method string, _ json.RawMessage) (any, error) {
		if method != "inspect_scene" {
			return nil, errors.New("unexpected bridge method " + method)
		}
		return protocol.SceneOverview{
			Context: protocol.Context{SessionID: "s", Generation: 3, SceneID: 0, Width: 1920, Height: 1080, LayerMax: 1},
			Scene:   protocol.SceneTotals{ObjectCount: 2, FirstFrame: &firstFrame, LastFrame: &lastFrame, LayerMax: 1},
			Layers: []protocol.SceneLayerSummary{{
				Index: 0, Name: "Main", Enabled: true, ObjectCount: 2,
				FirstFrame: &firstFrame, LastFrame: &lastFrame, Color: ptr("#112233ff"),
			}},
			Markers: []protocol.Marker{{Frame: 15, Memo: "beat"}},
			BPM:     []protocol.BPMPoint{{Tempo: 120, Beat: 4}},
		}, nil
	})

	session := connectTestSession(t, address)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "inspect_scene", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("inspect_scene: %v", err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var overview protocol.SceneOverview
	if err := json.Unmarshal(encoded, &overview); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if overview.Scene.ObjectCount != 2 || len(overview.Layers) != 1 || len(overview.Markers) != 1 || len(overview.BPM) != 1 {
		t.Fatalf("unexpected overview: %+v", overview)
	}
	if overview.Layers[0].Color == nil || *overview.Layers[0].Color != "#112233ff" {
		t.Fatalf("layer color was not preserved: %+v", overview.Layers[0])
	}
	if overview.Context.SessionID != "s" || overview.Context.Generation != 3 {
		t.Fatalf("context was not preserved: %+v", overview.Context)
	}
}

func TestSetLayerColorsPlansOneOperationPerLayer(t *testing.T) {
	address := startFakeBridge(t, func(method string, _ json.RawMessage) (any, error) {
		if method == "get_context" {
			return protocol.Context{SessionID: "s", Generation: 3, SceneID: 0}, nil
		}
		return nil, errors.New("dry_run must not call " + method)
	})

	session := connectTestSession(t, address)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "set_layer_colors",
		Arguments: map[string]any{
			"session_id":  "s",
			"generation":  3,
			"scene_id":    0,
			"layer_start": 2,
			"layer_end":   4,
			"colors":      []string{"ff0000", "", "default"},
			"dry_run":     true,
		},
	})
	if err != nil {
		t.Fatalf("set_layer_colors: %v", err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var plan operationPlanOutput
	if err := json.Unmarshal(encoded, &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if !plan.DryRun || len(plan.Operations) != 3 || plan.Mutation != nil {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.Operations[0].LayerColor == nil || *plan.Operations[0].LayerColor != "ff0000" {
		t.Fatalf("first layer color was not normalized: %+v", plan.Operations[0])
	}
	for index := 1; index < 3; index++ {
		if plan.Operations[index].LayerColor == nil || *plan.Operations[index].LayerColor != "" {
			t.Fatalf("operation %d should clear the layer color: %+v", index, plan.Operations[index])
		}
	}
	if plan.Operations[2].Layer == nil || *plan.Operations[2].Layer != 4 {
		t.Fatalf("layer range was not applied: %+v", plan.Operations[2])
	}
}

func ptr[T any](value T) *T { return &value }
