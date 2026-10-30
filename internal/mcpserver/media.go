package mcpserver

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type replaceMediaInput struct {
	mutationContext
	targetSpec
	File          string `json:"file,omitempty" jsonschema:"new media file for every target"`
	Effect        string `json:"effect,omitempty" jsonschema:"effect that owns the file item; auto-detected when the object has exactly one file item"`
	Item          string `json:"item,omitempty"`
	RelinkFromDir string `json:"relink_from_dir,omitempty" jsonschema:"relink by file name under this directory instead of file"`
	Recursive     bool   `json:"recursive,omitempty"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

func addMediaTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "replace_media", Description: "Replace media on target objects, or relink missing media by matching file names under a directory."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input replaceMediaInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if (input.File == "") == (input.RelinkFromDir == "") {
				return nil, operationPlanOutput{}, errors.New("specify either file or relink_from_dir")
			}
			ops := []protocol.BatchOperation{}
			if input.File != "" {
				if input.Item != "" && input.Effect == "" {
					return nil, operationPlanOutput{}, errors.New("item also requires effect")
				}
				info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: input.File, Strict: true})
				if err != nil {
					return nil, operationPlanOutput{}, err
				}
				if !info.Supported {
					return nil, operationPlanOutput{}, errors.New("replacement media is unsupported")
				}
				for _, object := range objects {
					ops = append(ops, protocol.BatchOperation{Op: "replace_media", ObjectID: object.ID, File: input.File, Effect: input.Effect, Item: input.Item})
				}
			} else {
				index, err := mediaIndex(input.RelinkFromDir, input.Recursive)
				if err != nil {
					return nil, operationPlanOutput{}, err
				}
				for _, object := range objects {
					files, err := objectMedia(ctx, client, object.ID)
					if err != nil {
						return nil, operationPlanOutput{}, err
					}
					for _, old := range files {
						replacement, ok := index[strings.ToLower(filepath.Base(old))]
						if ok && !strings.EqualFold(old, replacement) {
							ops = append(ops, protocol.BatchOperation{Op: "replace_media", ObjectID: object.ID, File: replacement})
						}
					}
				}
			}
			_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
			plan.Operation = "replace_media"
			return nil, plan, err
		})
}

// mediaIndex maps lower-case file names under root to their first full path.
func mediaIndex(root string, recursive bool) (map[string]string, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("relink_from_dir is required")
	}
	index := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && !recursive {
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
	return index, err
}
