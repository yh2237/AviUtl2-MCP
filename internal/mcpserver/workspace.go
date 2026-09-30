package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type workspaceTiming struct {
	TimeoutMS int `json:"timeout_ms,omitempty" jsonschema:"SDK call timeout in milliseconds; default 30000, maximum 300000. Timeout does not undo a submitted operation."`
}

func (t workspaceTiming) context(ctx context.Context) (context.Context, error) {
	if t.TimeoutMS < 0 || t.TimeoutMS > 300000 {
		return nil, errors.New("timeout_ms must be between 0 and 300000")
	}
	if t.TimeoutMS == 0 {
		t.TimeoutMS = 30000
	}
	return bridge.WithCallTimeout(ctx, time.Duration(t.TimeoutMS)*time.Millisecond), nil
}

type selectSceneInput struct {
	mutationContext
	protocol.SelectSceneParams
	workspaceTiming
}

type createSceneInput struct {
	mutationContext
	protocol.CreateSceneParams
	workspaceTiming
}

type createProjectInput struct {
	mutationContext
	protocol.CreateProjectParams
	workspaceTiming
}

type projectFileInput struct {
	mutationContext
	protocol.ProjectFileParams
	workspaceTiming
}

type outputFileInput struct {
	mutationContext
	protocol.OutputFileParams
	workspaceTiming
}

type outputStatusInput struct {
	JobID uint64 `json:"job_id,omitempty" jsonschema:"job id returned by output_file; omit for the most recent job, including after a timeout"`
}

type outputPluginsOutput struct {
	Plugins []protocol.ModuleInfo `json:"plugins"`
}

type setObjectFlagsInput struct {
	mutationContext
	targetSpec
	Flags  protocol.ObjectFlagUpdates `json:"flags"`
	DryRun bool                       `json:"dry_run,omitempty"`
}

var backgroundPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

func validateSceneSettings(s protocol.SceneSettings) error {
	for name, value := range map[string]*int{"width": s.Width, "height": s.Height, "rate": s.Rate, "scale": s.Scale, "sample_rate": s.SampleRate} {
		if value != nil && (*value < 1 || int64(*value) > 2147483647) {
			return fmt.Errorf("%s must be a positive 32-bit integer", name)
		}
	}
	if (s.Width != nil && *s.Width > 16384) || (s.Height != nil && *s.Height > 16384) {
		return errors.New("scene dimensions must not exceed 16384")
	}
	if s.Background != "" && !backgroundPattern.MatchString(s.Background) {
		return errors.New("background must be eight hexadecimal digits (RRGGBBAA)")
	}
	return nil
}

func validateWorkspacePath(file string) error {
	if strings.TrimSpace(file) == "" || strings.ContainsRune(file, '\x00') || !filepath.IsAbs(file) {
		return errors.New("an absolute file path without NUL is required")
	}
	return nil
}

func addWorkspaceTools(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_scenes", Description: "List all scene IDs and names with the active scene and current concurrency tokens."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, protocol.ScenesResult, error) {
			result, err := client.ListScenes(ctx)
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "select_scene", Description: "Select a scene by ID or unique exact name. Returns the scene list and fresh context for subsequent edits; old object IDs expire."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input selectSceneInput) (*mcp.CallToolResult, protocol.ScenesResult, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, protocol.ScenesResult{}, err
			}
			if (input.TargetSceneID != nil) == (input.Name != "") || (input.TargetSceneID != nil && (*input.TargetSceneID < 0 || int64(*input.TargetSceneID) > 2147483647)) {
				return nil, protocol.ScenesResult{}, errors.New("specify either a non-negative target_scene_id or an exact scene name")
			}
			ctx, err := input.workspaceTiming.context(ctx)
			if err != nil {
				return nil, protocol.ScenesResult{}, err
			}
			result, err := client.SelectScene(ctx, input.SelectSceneParams, input.expected())
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "create_scene", Description: "Create and select a scene. Omitted dimensions, rate, sample rate, and RRGGBBAA background inherit the current scene. Returns scenes and fresh context."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input createSceneInput) (*mcp.CallToolResult, protocol.ScenesResult, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, protocol.ScenesResult{}, err
			}
			if strings.TrimSpace(input.Name) == "" || strings.ContainsRune(input.Name+input.Label, '\x00') {
				return nil, protocol.ScenesResult{}, errors.New("a nonempty scene name and label without NUL are required")
			}
			if err := validateSceneSettings(input.SceneSettings); err != nil {
				return nil, protocol.ScenesResult{}, err
			}
			ctx, err := input.workspaceTiming.context(ctx)
			if err != nil {
				return nil, protocol.ScenesResult{}, err
			}
			result, err := client.CreateScene(ctx, input.CreateSceneParams, input.expected())
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "set_object_flags", Description: "Apply group/camera-control and clipping flags to explicit, selected, or focused objects in one Undo unit. Omitted flags are preserved; supports dry_run and returns updated objects."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input setObjectFlagsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			if input.Flags.Empty() {
				return nil, operationPlanOutput{}, errors.New("at least one flag update is required")
			}
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			ops := make([]protocol.BatchOperation, 0, len(objects))
			for _, object := range objects {
				ops = append(ops, protocol.BatchOperation{Op: "set_object_flags", ObjectID: object.ID, Flags: &input.Flags})
			}
			return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		})
	mcp.AddTool(server, &mcp.Tool{Name: "create_project", Description: "Create a new project, replacing the current project. Omitted scene settings inherit the current scene. show_confirm enables AviUtl2's save/cancel dialog (default false). Returns fresh context."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input createProjectInput) (*mcp.CallToolResult, protocol.ProjectResult, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, protocol.ProjectResult{}, err
			}
			if err := validateSceneSettings(input.SceneSettings); err != nil {
				return nil, protocol.ProjectResult{}, err
			}
			ctx, err := input.workspaceTiming.context(ctx)
			if err != nil {
				return nil, protocol.ProjectResult{}, err
			}
			result, err := client.CreateProject(ctx, input.CreateProjectParams, input.expected())
			return nil, result, err
		})
	for _, method := range []string{"open_project", "save_project"} {
		description := "Save the current project to an absolute path using SDK backup-style serialization. Returns current context."
		if method == "open_project" {
			description = "Open an absolute project file path, replacing the current project. show_confirm enables AviUtl2's save/cancel dialog (default false). Returns fresh context; old object IDs expire."
		}
		mcp.AddTool(server, &mcp.Tool{Name: method, Description: description},
			func(ctx context.Context, _ *mcp.CallToolRequest, input projectFileInput) (*mcp.CallToolResult, protocol.ProjectResult, error) {
				if err := input.mutationContext.validate(); err != nil {
					return nil, protocol.ProjectResult{}, err
				}
				if err := validateWorkspacePath(input.File); err != nil {
					return nil, protocol.ProjectResult{}, err
				}
				ctx, err := input.workspaceTiming.context(ctx)
				if err != nil {
					return nil, protocol.ProjectResult{}, err
				}
				result, err := client.ProjectFile(ctx, method, input.ProjectFileParams, input.expected())
				return nil, result, err
			})
	}
	mcp.AddTool(server, &mcp.Tool{Name: "list_output_plugins", Description: "List registered output plugin names for output_file."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, outputPluginsOutput, error) {
			result, err := client.ListOutputPlugins(ctx)
			return nil, outputPluginsOutput{Plugins: result}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "output_file", Description: "Start asynchronous output of the current scene using a registered output plugin and its current settings. Optional save_project_file saves a project snapshot first. Returns a job ID for get_output_status; acceptance is not output completion."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input outputFileInput) (*mcp.CallToolResult, protocol.OutputStatusResult, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, protocol.OutputStatusResult{}, err
			}
			if err := validateWorkspacePath(input.File); err != nil {
				return nil, protocol.OutputStatusResult{}, err
			}
			if strings.TrimSpace(input.OutputPlugin) == "" || strings.ContainsRune(input.OutputPlugin, '\x00') {
				return nil, protocol.OutputStatusResult{}, errors.New("output_plugin is required without NUL")
			}
			if input.SaveProjectFile != "" {
				if err := validateWorkspacePath(input.SaveProjectFile); err != nil {
					return nil, protocol.OutputStatusResult{}, err
				}
				if strings.EqualFold(filepath.Clean(input.File), filepath.Clean(input.SaveProjectFile)) {
					return nil, protocol.OutputStatusResult{}, errors.New("project and output paths must be different")
				}
			}
			ctx, err := input.workspaceTiming.context(ctx)
			if err != nil {
				return nil, protocol.OutputStatusResult{}, err
			}
			result, err := client.OutputFile(ctx, input.OutputFileParams, input.expected())
			return nil, result, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_output_status", Description: "Get an output job (latest when job_id is omitted), including after reconnect/timeout. ended means the SDK left output state; success, cancellation, and failure cannot be distinguished, so outcome remains unknown. Keeps the latest 20 jobs."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input outputStatusInput) (*mcp.CallToolResult, protocol.OutputStatusResult, error) {
			result, err := client.GetOutputStatus(ctx, input.JobID)
			return nil, result, err
		})
}
