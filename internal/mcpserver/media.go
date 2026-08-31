package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type addMediaSequenceInput struct {
	mutationContext
	Files         []string `json:"files"`
	Layer         int      `json:"layer"`
	Frame         int      `json:"frame"`
	Gap           int      `json:"gap,omitempty"`
	LayerStep     int      `json:"layer_step,omitempty"`
	DefaultLength int      `json:"default_length,omitempty"`
	DryRun        bool     `json:"dry_run,omitempty"`
}
type relinkMediaInput struct {
	mutationContext
	targetSpec
	Directory string `json:"directory"`
	Recursive bool   `json:"recursive,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}
type bulkReplaceMediaInput struct {
	mutationContext
	targetSpec
	File   string `json:"file"`
	Effect string `json:"effect,omitempty"`
	Item   string `json:"item,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}
type fitMediaDurationInput struct {
	mutationContext
	targetSpec
	DryRun bool `json:"dry_run,omitempty"`
}

func addMediaTools(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "add_media_sequence", Description: "Preflight and place several media files sequentially in one Undo unit."}, func(ctx context.Context, _ *mcp.CallToolRequest, input addMediaSequenceInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		if err := input.mutationContext.validate(); err != nil {
			return nil, operationPlanOutput{}, err
		}
		if len(input.Files) == 0 || len(input.Files) > 100 || input.Layer < 0 || input.Frame < 0 {
			return nil, operationPlanOutput{}, errors.New("files and non-negative layer/frame are required")
		}
		current, err := client.GetContext(ctx)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if current.SessionID != input.SessionID || current.Generation != input.Generation || current.SceneID != input.SceneID {
			return nil, operationPlanOutput{}, errors.New("AviUtl2 context changed while preparing media sequence")
		}
		cursor := input.Frame
		ops := []protocol.BatchOperation{}
		for i, file := range input.Files {
			info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: file, Strict: true})
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if !info.Supported {
				return nil, operationPlanOutput{}, fmt.Errorf("unsupported media: %s", file)
			}
			length := input.DefaultLength
			if info.TotalTime > 0 {
				length = max(1, int(math.Round(info.TotalTime*float64(current.Rate)/float64(current.Scale))))
			}
			layer, frame := input.Layer+i*input.LayerStep, cursor
			ops = append(ops, protocol.BatchOperation{Op: "add_media", File: file, Layer: &layer, Frame: &frame, Length: length})
			cursor += max(1, length) + input.Gap
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "replace_media_bulk", Description: "Replace the media file on multiple objects while preserving placement and effects."}, func(ctx context.Context, _ *mcp.CallToolRequest, input bulkReplaceMediaInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if input.File == "" {
			return nil, operationPlanOutput{}, errors.New("file is required")
		}
		info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: input.File, Strict: true})
		if err != nil || !info.Supported {
			if err == nil {
				err = errors.New("replacement media is unsupported")
			}
			return nil, operationPlanOutput{}, err
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			ops = append(ops, protocol.BatchOperation{Op: "replace_media", ObjectID: o.ID, File: input.File, Effect: input.Effect, Item: input.Item})
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "relink_media", Description: "Relink missing or moved media by matching file names under a directory."}, func(ctx context.Context, _ *mcp.CallToolRequest, input relinkMediaInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if input.Directory == "" {
			return nil, operationPlanOutput{}, errors.New("directory is required")
		}
		index := map[string]string{}
		err = filepath.WalkDir(input.Directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && path != input.Directory && !input.Recursive {
				return filepath.SkipDir
			}
			if !entry.IsDir() {
				key := strings.ToLower(entry.Name())
				if _, exists := index[key]; !exists {
					index[key] = path
				}
			}
			return nil
		})
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			files, err := objectMedia(ctx, client, o.ID)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			for _, old := range files {
				if replacement, ok := index[strings.ToLower(filepath.Base(old))]; ok && !strings.EqualFold(old, replacement) {
					ops = append(ops, protocol.BatchOperation{Op: "replace_media", ObjectID: o.ID, File: replacement})
				}
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fit_objects_to_media", Description: "Resize media objects to their source duration using the current scene frame rate."}, func(ctx context.Context, _ *mcp.CallToolRequest, input fitMediaDurationInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		current, err := client.GetContext(ctx)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			files, err := objectMedia(ctx, client, o.ID)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if len(files) != 1 {
				continue
			}
			info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: files[0], Strict: true})
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if info.TotalTime <= 0 {
				continue
			}
			end := o.Start + max(1, int(math.Round(info.TotalTime*float64(current.Rate)/float64(current.Scale)))) - 1
			section := len(o.Sections)
			ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: o.ID, Section: &section, Frame: &end})
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
}
