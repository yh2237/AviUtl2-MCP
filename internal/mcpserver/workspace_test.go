package mcpserver

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

func workspaceTestSession(t *testing.T, handler func(protocol.Request) any) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			payload, err := protocol.ReadFrame(conn)
			if err != nil {
				return
			}
			var req protocol.Request
			if err := json.Unmarshal(payload, &req); err != nil {
				t.Error(err)
				return
			}
			result, err := json.Marshal(handler(req))
			if err != nil {
				t.Error(err)
				return
			}
			response, _ := json.Marshal(protocol.Response{ID: req.ID, Version: protocol.Version, Result: result})
			if err := protocol.WriteFrame(conn, response); err != nil {
				return
			}
		}
	}()
	bridgeClient := bridge.NewClient(listener.Addr().String(), time.Second)
	t.Cleanup(func() { bridgeClient.Close() })
	server := New(bridgeClient, "test")
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return ctx, cs
}

func TestWorkspaceToolsForwardTokensAndDistinctTargetScene(t *testing.T) {
	current := protocol.Context{SessionID: "session", Generation: 8, SceneID: 2}
	requests := make(chan protocol.Request, 20)
	ctx, session := workspaceTestSession(t, func(req protocol.Request) any {
		requests <- req
		switch req.Method {
		case "list_scenes", "select_scene", "create_scene":
			return protocol.ScenesResult{Context: current, Scenes: []protocol.Scene{{ID: 2, Name: "Main", Current: true}}}
		case "create_project", "open_project", "save_project":
			return protocol.ProjectResult{Context: current, Action: req.Method}
		case "output_file", "get_output_status":
			return protocol.OutputStatusResult{Job: &protocol.OutputJob{ID: 1, State: "running", Outcome: "unknown"}}
		case "list_output_plugins":
			return []protocol.ModuleInfo{{Type: 7, Name: "Encoder"}}
		default:
			t.Errorf("unexpected bridge method %q", req.Method)
			return nil
		}
	})
	path := filepath.Join(t.TempDir(), "project.aup2")
	for _, test := range []struct {
		name     string
		params   map[string]any
		mutation bool
	}{
		{"list_scenes", map[string]any{}, false},
		{"select_scene", map[string]any{"target_scene_id": 7}, true},
		{"create_scene", map[string]any{"name": "Title", "width": 1280, "background": "00000000"}, true},
		{"create_project", map[string]any{"show_confirm": true}, true},
		{"open_project", map[string]any{"file": path}, true},
		{"save_project", map[string]any{"file": path}, true},
		{"list_output_plugins", map[string]any{}, false},
		{"output_file", map[string]any{"file": filepath.Join(t.TempDir(), "movie.mp4"), "output_plugin": "Encoder", "save_project_file": path}, true},
		{"get_output_status", map[string]any{"job_id": 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.mutation {
				test.params["session_id"] = "session"
				test.params["generation"] = 8
				test.params["scene_id"] = 2
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: test.params})
			if err != nil || result.IsError {
				var content []mcp.Content
				if result != nil {
					content = result.Content
				}
				encoded, _ := json.Marshal(content)
				t.Fatalf("tool failed: %s, %v", encoded, err)
			}
			req := <-requests
			if req.Method != test.name {
				t.Fatalf("got method %q", req.Method)
			}
			if test.mutation && (req.Context == nil || req.Context.SessionID != "session" || req.Context.Generation == nil || *req.Context.Generation != 8 || req.Context.SceneID == nil || *req.Context.SceneID != 2) {
				t.Fatalf("incorrect concurrency tokens: %+v", req.Context)
			}
			if test.name == "select_scene" {
				var params protocol.SelectSceneParams
				if err := json.Unmarshal(req.Params, &params); err != nil || params.TargetSceneID == nil || *params.TargetSceneID != 7 {
					t.Fatalf("target scene was lost: %s", req.Params)
				}
			}
			if test.name == "create_scene" {
				var params protocol.CreateSceneParams
				if err := json.Unmarshal(req.Params, &params); err != nil || params.Width == nil || *params.Width != 1280 || params.Background != "00000000" || params.Height != nil {
					t.Fatalf("scene overrides were lost: %s", req.Params)
				}
			}
		})
	}
}

func TestObjectFlagsToolDryRunAndSingleBatch(t *testing.T) {
	current := protocol.Context{SessionID: "session", Generation: 8, SceneID: 2}
	objects := []protocol.Object{{ID: 10, NativeID: "9007199254740993"}, {ID: 20}}
	batches := make(chan protocol.ExecuteBatchParams, 2)
	ctx, session := workspaceTestSession(t, func(req protocol.Request) any {
		switch req.Method {
		case "get_context":
			return current
		case "get_selection":
			return protocol.SelectionResult{Context: current, Objects: objects}
		case "inspect_objects":
			return protocol.ObjectsResult{Context: current, Objects: objects}
		case "execute_batch":
			var params protocol.ExecuteBatchParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Error(err)
			}
			batches <- params
			results := []protocol.OperationResult{}
			for index, op := range params.Operations {
				id := op.ObjectID
				results = append(results, protocol.OperationResult{Index: index, Op: op.Op, ObjectID: &id, Changed: true})
			}
			return protocol.MutationResult{Context: current, Results: results}
		default:
			t.Errorf("unexpected bridge method %q", req.Method)
			return nil
		}
	})
	args := map[string]any{"session_id": "session", "generation": 8, "scene_id": 2, "selected": true, "flags": map[string]any{"enable_camera": false}, "dry_run": true}
	for _, dry := range []bool{true, false} {
		args["dry_run"] = dry
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "set_object_flags", Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("flag tool failed: %+v, %v", result, err)
		}
		if dry {
			select {
			case <-batches:
				t.Fatal("dry_run performed mutation")
			default:
			}
		} else {
			params := <-batches
			if len(params.Operations) != 2 {
				t.Fatalf("wanted one batch for two objects: %+v", params)
			}
			for _, op := range params.Operations {
				if op.Op != "set_object_flags" || op.Flags == nil || op.Flags.EnableCamera == nil || *op.Flags.EnableCamera || op.Flags.EnableGroup != nil {
					t.Fatalf("false/omitted flag distinction lost: %+v", op)
				}
			}
		}
	}
}

func TestWorkspaceToolsRejectInvalidInputsBeforeBridge(t *testing.T) {
	ctx, session := workspaceTestSession(t, func(req protocol.Request) any { t.Errorf("invalid input reached bridge: %s", req.Method); return nil })
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"select_scene", map[string]any{"target_scene_id": 7, "name": "Both"}},
		{"create_scene", map[string]any{"name": "Title", "width": 0}},
		{"create_project", map[string]any{"background": "ffffff"}},
		{"save_project", map[string]any{"file": "relative.aup2"}},
		{"output_file", map[string]any{"file": filepath.Join(t.TempDir(), "movie.mp4"), "output_plugin": "Encoder", "timeout_ms": 300001}},
		{"set_object_flags", map[string]any{"object_ids": []int{1}, "flags": map[string]any{}}},
	} {
		test.args["session_id"] = "session"
		test.args["generation"] = 8
		test.args["scene_id"] = 2
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: test.args})
		if err == nil && !result.IsError {
			t.Fatalf("%s accepted invalid input", test.name)
		}
	}
}
