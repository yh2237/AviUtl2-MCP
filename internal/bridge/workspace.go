package bridge

import (
	"context"

	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

func (c *Client) ListScenes(ctx context.Context) (protocol.ScenesResult, error) {
	return call[protocol.ScenesResult](c, ctx, "list_scenes", struct{}{}, nil)
}

func (c *Client) SelectScene(ctx context.Context, params protocol.SelectSceneParams, expected *protocol.ExpectedContext) (protocol.ScenesResult, error) {
	return call[protocol.ScenesResult](c, ctx, "select_scene", params, expected)
}

func (c *Client) CreateScene(ctx context.Context, params protocol.CreateSceneParams, expected *protocol.ExpectedContext) (protocol.ScenesResult, error) {
	return call[protocol.ScenesResult](c, ctx, "create_scene", params, expected)
}

func (c *Client) CreateProject(ctx context.Context, params protocol.CreateProjectParams, expected *protocol.ExpectedContext) (protocol.ProjectResult, error) {
	return call[protocol.ProjectResult](c, ctx, "create_project", params, expected)
}

func (c *Client) ProjectFile(ctx context.Context, method string, params protocol.ProjectFileParams, expected *protocol.ExpectedContext) (protocol.ProjectResult, error) {
	return call[protocol.ProjectResult](c, ctx, method, params, expected)
}

func (c *Client) ListOutputPlugins(ctx context.Context) ([]protocol.ModuleInfo, error) {
	return call[[]protocol.ModuleInfo](c, ctx, "list_output_plugins", struct{}{}, nil)
}

func (c *Client) OutputFile(ctx context.Context, params protocol.OutputFileParams, expected *protocol.ExpectedContext) (protocol.OutputStatusResult, error) {
	return call[protocol.OutputStatusResult](c, ctx, "output_file", params, expected)
}

func (c *Client) GetOutputStatus(ctx context.Context, jobID uint64) (protocol.OutputStatusResult, error) {
	return call[protocol.OutputStatusResult](c, ctx, "get_output_status", struct {
		JobID uint64 `json:"job_id,omitempty"`
	}{jobID}, nil)
}
