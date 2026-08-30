package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type emptyInput struct{}

type pingOutput struct {
	Pong       bool   `json:"pong" jsonschema:"whether the AviUtl2 bridge responded"`
	SessionID  string `json:"session_id" jsonschema:"identifier for this AviUtl2 process session"`
	Generation uint64 `json:"generation" jsonschema:"object handle generation"`
}

type contextOutput struct {
	Context protocol.Context `json:"context" jsonschema:"current AviUtl2 editing context"`
}

func New(client *bridge.Client, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "aviutl2-mcp", Version: version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Description: "Check whether the native bridge is loaded in a running AviUtl2 instance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, pingOutput, error) {
		result, err := client.Ping(ctx)
		if err != nil {
			return nil, pingOutput{}, err
		}
		return nil, pingOutput(result), nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_context",
		Description: "Get the current AviUtl2 scene, cursor, selection, display range, and project dimensions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, contextOutput, error) {
		result, err := client.GetContext(ctx)
		if err != nil {
			return nil, contextOutput{}, err
		}
		return nil, contextOutput{Context: result}, nil
	})
	return server
}
