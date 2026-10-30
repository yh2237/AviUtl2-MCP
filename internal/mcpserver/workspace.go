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

type scenesInput struct {
	mutationContext
	Operation     string `json:"operation" jsonschema:"list, select, or create"`
	TargetSceneID *int   `json:"target_scene_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Label         string `json:"label,omitempty"`
	protocol.SceneSettings
	workspaceTiming
}

type scenesOutput struct {
	Operation string `json:"operation"`
	protocol.ScenesResult
}

type projectInput struct {
	mutationContext
	Operation string `json:"operation" jsonschema:"create, open, or save"`
	protocol.SceneSettings
	File        string `json:"file,omitempty"`
	ShowConfirm bool   `json:"show_confirm,omitempty"`
	workspaceTiming
}

type projectOutput struct {
	Operation string `json:"operation"`
	protocol.ProjectResult
}

type outputInput struct {
	mutationContext
	Operation       string `json:"operation" jsonschema:"list_plugins, start, or status"`
	File            string `json:"file,omitempty"`
	OutputPlugin    string `json:"output_plugin,omitempty"`
	SaveProjectFile string `json:"save_project_file,omitempty"`
	JobID           uint64 `json:"job_id,omitempty" jsonschema:"job id returned by operation=start; omit for the most recent job"`
	workspaceTiming
}

type outputResult struct {
	Operation string                       `json:"operation"`
	Plugins   []protocol.ModuleInfo        `json:"plugins,omitempty"`
	Status    *protocol.OutputStatusResult `json:"status,omitempty"`
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

func runScenes(ctx context.Context, client *bridge.Client, input scenesInput) (scenesOutput, error) {
	out := scenesOutput{Operation: input.Operation}
	switch input.Operation {
	case "list":
		result, err := client.ListScenes(ctx)
		out.ScenesResult = result
		return out, err
	case "select":
		if err := input.mutationContext.validate(); err != nil {
			return scenesOutput{}, err
		}
		if (input.TargetSceneID != nil) == (input.Name != "") ||
			(input.TargetSceneID != nil && (*input.TargetSceneID < 0 || int64(*input.TargetSceneID) > 2147483647)) {
			return scenesOutput{}, errors.New("specify either a non-negative target_scene_id or an exact scene name")
		}
		callCtx, err := input.workspaceTiming.context(ctx)
		if err != nil {
			return scenesOutput{}, err
		}
		result, err := client.SelectScene(callCtx, protocol.SelectSceneParams{TargetSceneID: input.TargetSceneID, Name: input.Name}, input.expected())
		out.ScenesResult = result
		return out, err
	case "create":
		if err := input.mutationContext.validate(); err != nil {
			return scenesOutput{}, err
		}
		if strings.TrimSpace(input.Name) == "" || strings.ContainsRune(input.Name+input.Label, '\x00') {
			return scenesOutput{}, errors.New("a nonempty scene name and label without NUL are required")
		}
		if err := validateSceneSettings(input.SceneSettings); err != nil {
			return scenesOutput{}, err
		}
		callCtx, err := input.workspaceTiming.context(ctx)
		if err != nil {
			return scenesOutput{}, err
		}
		result, err := client.CreateScene(callCtx, protocol.CreateSceneParams{SceneSettings: input.SceneSettings, Name: input.Name, Label: input.Label}, input.expected())
		out.ScenesResult = result
		return out, err
	default:
		return scenesOutput{}, fmt.Errorf("unknown operation %q; use list, select, or create", input.Operation)
	}
}

func runProject(ctx context.Context, client *bridge.Client, input projectInput) (projectOutput, error) {
	out := projectOutput{Operation: input.Operation}
	if err := input.mutationContext.validate(); err != nil {
		return projectOutput{}, err
	}
	callCtx, err := input.workspaceTiming.context(ctx)
	if err != nil {
		return projectOutput{}, err
	}
	switch input.Operation {
	case "create":
		if err := validateSceneSettings(input.SceneSettings); err != nil {
			return projectOutput{}, err
		}
		result, err := client.CreateProject(callCtx, protocol.CreateProjectParams{SceneSettings: input.SceneSettings, ShowConfirm: input.ShowConfirm}, input.expected())
		out.ProjectResult = result
		return out, err
	case "open", "save":
		if err := validateWorkspacePath(input.File); err != nil {
			return projectOutput{}, err
		}
		result, err := client.ProjectFile(callCtx, input.Operation+"_project", protocol.ProjectFileParams{File: input.File, ShowConfirm: input.ShowConfirm}, input.expected())
		out.ProjectResult = result
		return out, err
	default:
		return projectOutput{}, fmt.Errorf("unknown operation %q; use create, open, or save", input.Operation)
	}
}

func runOutput(ctx context.Context, client *bridge.Client, input outputInput) (outputResult, error) {
	out := outputResult{Operation: input.Operation}
	switch input.Operation {
	case "list_plugins":
		plugins, err := client.ListOutputPlugins(ctx)
		out.Plugins = plugins
		return out, err
	case "status":
		status, err := client.GetOutputStatus(ctx, input.JobID)
		out.Status = &status
		return out, err
	case "start":
		if err := input.mutationContext.validate(); err != nil {
			return outputResult{}, err
		}
		if err := validateWorkspacePath(input.File); err != nil {
			return outputResult{}, err
		}
		if strings.TrimSpace(input.OutputPlugin) == "" || strings.ContainsRune(input.OutputPlugin, '\x00') {
			return outputResult{}, errors.New("output_plugin is required without NUL")
		}
		if input.SaveProjectFile != "" {
			if err := validateWorkspacePath(input.SaveProjectFile); err != nil {
				return outputResult{}, err
			}
			if strings.EqualFold(filepath.Clean(input.File), filepath.Clean(input.SaveProjectFile)) {
				return outputResult{}, errors.New("project and output paths must be different")
			}
		}
		callCtx, err := input.workspaceTiming.context(ctx)
		if err != nil {
			return outputResult{}, err
		}
		status, err := client.OutputFile(callCtx, protocol.OutputFileParams{
			File: input.File, OutputPlugin: input.OutputPlugin, SaveProjectFile: input.SaveProjectFile,
		}, input.expected())
		out.Status = &status
		return out, err
	default:
		return outputResult{}, fmt.Errorf("unknown operation %q; use list_plugins, start, or status", input.Operation)
	}
}

func addWorkspaceTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "scenes", Description: "List, select, or create scenes. select/create return the scene list and fresh context; old object IDs expire."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input scenesInput) (*mcp.CallToolResult, scenesOutput, error) {
			result, err := runScenes(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "project", Description: "Create, open, or save the project. Omitted scene settings inherit the current scene; show_confirm enables AviUtl2's dialog."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input projectInput) (*mcp.CallToolResult, projectOutput, error) {
			result, err := runProject(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "output", Description: "List output plugins, start asynchronous output, or read job status. Start returns a job id; acceptance is not completion."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input outputInput) (*mcp.CallToolResult, outputResult, error) {
			result, err := runOutput(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "set_object_flags", Description: "Apply group/camera-control and clipping flags to explicit, selected, or focused objects in one Undo unit. Omitted flags are preserved; supports dry_run and returns updated objects."},
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
}
