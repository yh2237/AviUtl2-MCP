package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type diagnoseInput struct {
	Reconnect  bool `json:"reconnect,omitempty" jsonschema:"close and reopen the bridge socket before diagnosing"`
	IncludeLog bool `json:"include_log,omitempty" jsonschema:"include the last fifty bridge calls"`
}

type diagnosticsOutput struct {
	ServerVersion string                     `json:"server_version"`
	Bridge        protocol.DiagnosticsResult `json:"bridge"`
	Context       protocol.Context           `json:"context"`
	ContextError  string                     `json:"context_error,omitempty"`
	LatencyMS     int64                      `json:"latency_ms"`
	Editable      bool                       `json:"editable"`
	Limitations   []string                   `json:"limitations"`
	Log           []bridge.CallLog           `json:"log,omitempty"`
}

func addDiagnosticsTools(server *mcp.Server, client *bridge.Client, version string) {
	addTool(server, &mcp.Tool{Name: "diagnose_connection", Description: "Diagnose the MCP server, native bridge, AviUtl2 host, modules, latency, and SDK limitations. reconnect reopens a stuck socket; include_log adds recent bridge calls."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input diagnoseInput) (*mcp.CallToolResult, diagnosticsOutput, error) {
			started := time.Now()
			if input.Reconnect {
				if _, err := client.Reconnect(ctx); err != nil {
					return nil, diagnosticsOutput{}, fmt.Errorf("reconnect: %w", err)
				}
			}
			bridgeInfo, err := client.Diagnostics(ctx)
			if err != nil {
				return nil, diagnosticsOutput{}, fmt.Errorf("native bridge diagnostics: %w", err)
			}
			output := diagnosticsOutput{ServerVersion: version, Bridge: bridgeInfo, Limitations: limitations(bridgeInfo)}
			if input.IncludeLog {
				output.Log = client.RecentCalls()
			}
			current, err := client.GetContext(ctx)
			if err != nil {
				// AviUtl2 returns NO_PROJECT until it finishes opening a project.
				// Report the bridge state instead of failing the whole diagnosis.
				var remote *bridge.RemoteError
				if errors.As(err, &remote) && remote.Code == "NO_PROJECT" {
					output.ContextError = remote.Message
					output.LatencyMS = time.Since(started).Milliseconds()
					output.Limitations = append([]string{"AviUtl2 has no project open yet; wait for startup to finish or create a project, then retry."}, output.Limitations...)
					return nil, output, nil
				}
				return nil, diagnosticsOutput{}, fmt.Errorf("AviUtl2 context: %w", err)
			}
			output.Context = current
			output.LatencyMS = time.Since(started).Milliseconds()
			output.Editable = current.EditState == 0
			return nil, output, nil
		})
}

// limitations lists SDK behavior the MCP layer cannot work around, plus host-dependent gaps.
func limitations(bridgeInfo protocol.DiagnosticsResult) []string {
	values := []string{
		"The AviUtl2 SDK does not expose an Undo command.",
		"Scene setting changes are not Undoable by AviUtl2.",
		"The SDK does not expose a typed track setter; reuse raw values read from AviUtl2.",
		"A cancelled or timed-out call does not stop work already sent to AviUtl2.",
	}
	if !bridgeInfo.Capabilities.LayerColor.Supported {
		host := bridgeInfo.HostVersionText
		if host == "" {
			host = "the running AviUtl2"
		}
		required := bridgeInfo.Capabilities.LayerColor.RequiresVersionText
		if required == "" {
			required = "2.1.13"
		}
		values = append(values, fmt.Sprintf("Layer colors need AviUtl2 %s or newer; %s is running.", required, host))
	}
	return values
}
