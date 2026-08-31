package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type markersOutput struct {
	Markers protocol.MarkersResult `json:"markers"`
}
type bpmOutput struct {
	BPM protocol.BPMGridResult `json:"bpm"`
}
type diagnosticsOutput struct {
	ServerVersion string                     `json:"server_version"`
	Bridge        protocol.DiagnosticsResult `json:"bridge"`
	Context       protocol.Context           `json:"context"`
	LatencyMS     int64                      `json:"latency_ms"`
	Editable      bool                       `json:"editable"`
	Limitations   []string                   `json:"limitations"`
}
type serverLogOutput struct {
	Calls []bridge.CallLog `json:"calls"`
}

func addDiagnosticsTools(server *mcp.Server, client *bridge.Client, version string) {
	mcp.AddTool(server, &mcp.Tool{Name: "get_markers", Description: "List all timeline markers and memos in the current scene."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, markersOutput, error) {
			result, err := client.GetMarkers(ctx)
			return nil, markersOutput{Markers: result}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_bpm_grid", Description: "Get the complete BPM grid in the current scene."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, bpmOutput, error) {
			result, err := client.GetBPMGrid(ctx)
			return nil, bpmOutput{BPM: result}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "diagnose_connection", Description: "Diagnose the MCP server, native bridge, AviUtl2 host, modules, latency, and SDK limitations."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, diagnosticsOutput, error) {
			started := time.Now()
			bridgeInfo, err := client.Diagnostics(ctx)
			if err != nil {
				return nil, diagnosticsOutput{}, fmt.Errorf("native bridge diagnostics: %w", err)
			}
			current, err := client.GetContext(ctx)
			if err != nil {
				return nil, diagnosticsOutput{}, fmt.Errorf("AviUtl2 context: %w", err)
			}
			return nil, diagnosticsOutput{ServerVersion: version, Bridge: bridgeInfo, Context: current, LatencyMS: time.Since(started).Milliseconds(), Editable: current.EditState == 0, Limitations: []string{"The official SDK exposes only the current scene, not a scene list or scene switching.", "The official SDK does not expose an Undo command.", "Scene setting changes are not Undoable by AviUtl2.", "The SDK does not expose a typed track setter; reuse raw values read from AviUtl2."}}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_server_log", Description: "Return the last fifty bridge calls with duration and errors."},
		func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, serverLogOutput, error) {
			return nil, serverLogOutput{Calls: client.RecentCalls()}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "reconnect_bridge", Description: "Close the current bridge socket, reconnect, and ping AviUtl2."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, pingOutput, error) {
			result, err := client.Reconnect(ctx)
			return nil, pingOutput(result), err
		})
}
