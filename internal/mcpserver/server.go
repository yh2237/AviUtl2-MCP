package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

var colorPattern = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

type emptyInput struct{}

type mutationContext struct {
	SessionID  string `json:"session_id" jsonschema:"session_id returned by get_context"`
	Generation uint64 `json:"generation" jsonschema:"generation returned by get_context"`
	SceneID    int    `json:"scene_id" jsonschema:"scene_id returned by get_context"`
}

func (c mutationContext) expected() *protocol.ExpectedContext {
	generation, sceneID := c.Generation, c.SceneID
	return &protocol.ExpectedContext{SessionID: c.SessionID, Generation: &generation, SceneID: &sceneID}
}

func (c mutationContext) validate() error {
	if strings.TrimSpace(c.SessionID) == "" || c.Generation == 0 || c.SceneID < 0 {
		return errors.New("session_id, a positive generation, and a non-negative scene_id from get_context are required")
	}
	return nil
}

type pingOutput struct {
	Pong       bool   `json:"pong" jsonschema:"whether the AviUtl2 bridge responded"`
	SessionID  string `json:"session_id" jsonschema:"identifier for this AviUtl2 process session"`
	Generation uint64 `json:"generation" jsonschema:"object handle generation"`
}

type contextOutput struct {
	Context protocol.Context `json:"context" jsonschema:"current AviUtl2 editing context"`
}

type inspectTimelineInput struct {
	LayerStart     int  `json:"layer_start" jsonschema:"first zero-based layer to inspect"`
	LayerEnd       int  `json:"layer_end" jsonschema:"last zero-based layer to inspect, inclusive"`
	FrameStart     int  `json:"frame_start" jsonschema:"first zero-based frame to inspect"`
	FrameEnd       int  `json:"frame_end" jsonschema:"last zero-based frame to inspect, inclusive"`
	MaxObjects     int  `json:"max_objects,omitempty" jsonschema:"maximum returned objects, default 200 and hard limit 1000"`
	IncludeAlias   bool `json:"include_alias,omitempty" jsonschema:"include raw AviUtl2 object alias data"`
	IncludeEffects bool `json:"include_effects,omitempty" jsonschema:"include effect names and states"`
}

type timelineOutput struct {
	Timeline protocol.TimelineResult `json:"timeline"`
}

type inspectObjectInput struct {
	ObjectID       uint64 `json:"object_id" jsonschema:"session-local object id from inspect_timeline or get_selection"`
	IncludeAlias   bool   `json:"include_alias,omitempty"`
	IncludeEffects bool   `json:"include_effects,omitempty"`
}

type objectOutput struct {
	Object protocol.ObjectResult `json:"object"`
}

type inspectObjectsInput struct {
	ObjectIDs      []uint64 `json:"object_ids" jsonschema:"one to one hundred session-local object ids"`
	IncludeAlias   bool     `json:"include_alias,omitempty"`
	IncludeEffects bool     `json:"include_effects,omitempty"`
}

type objectsOutput struct {
	Objects protocol.ObjectsResult `json:"objects"`
}

type inspectObjectValuesInput struct {
	ObjectID             uint64   `json:"object_id" jsonschema:"session-local object id"`
	Frame                *float64 `json:"frame,omitempty" jsonschema:"frame used to sample animated values; defaults to object start"`
	EffectIndex          *int     `json:"effect_index,omitempty"`
	Effect               string   `json:"effect,omitempty"`
	Items                []string `json:"items,omitempty"`
	IncludeRawValues     *bool    `json:"include_raw_values,omitempty" jsonschema:"defaults to true"`
	IncludeTrackInfo     *bool    `json:"include_track_info,omitempty" jsonschema:"defaults to true"`
	IncludeSampledValues *bool    `json:"include_sampled_values,omitempty" jsonschema:"defaults to true"`
}

type objectValuesOutput struct {
	Values protocol.ObjectValuesResult `json:"values"`
}

type effectsOutput struct {
	Effects []protocol.EffectDefinition `json:"effects"`
}

type effectItemsInput struct {
	Effect string `json:"effect" jsonschema:"exact effect name returned by list_effects"`
}

type effectItemsOutput struct {
	Effect protocol.EffectItemsResult `json:"effect"`
}

type selectionOutput struct {
	Selection protocol.SelectionResult `json:"selection"`
}

type preflightMediaInput struct {
	File   string `json:"file" jsonschema:"absolute local media path"`
	Strict bool   `json:"strict,omitempty" jsonschema:"open the file to verify it can actually be read"`
}

type mediaOutput struct {
	Media protocol.MediaInfo `json:"media"`
}

type addTextInput struct {
	mutationContext
	Text   string  `json:"text" jsonschema:"text to place on the timeline"`
	Layer  int     `json:"layer" jsonschema:"zero-based target layer"`
	Frame  int     `json:"frame" jsonschema:"zero-based start frame"`
	Length int     `json:"length" jsonschema:"duration in frames"`
	Size   float64 `json:"size,omitempty" jsonschema:"font size, default 34"`
	Color  string  `json:"color,omitempty" jsonschema:"six-digit RGB hex color, default ffffff"`
}

type addMediaInput struct {
	mutationContext
	File   string `json:"file" jsonschema:"absolute local media path"`
	Layer  int    `json:"layer" jsonschema:"zero-based target layer"`
	Frame  int    `json:"frame" jsonschema:"zero-based start frame"`
	Length int    `json:"length,omitempty" jsonschema:"duration in frames; zero lets AviUtl2 choose"`
}

type updateObjectInput struct {
	mutationContext
	ObjectID   uint64                    `json:"object_id"`
	Layer      *int                      `json:"layer,omitempty"`
	Frame      *int                      `json:"frame,omitempty"`
	Name       *string                   `json:"name,omitempty"`
	Properties []protocol.PropertyUpdate `json:"properties,omitempty"`
}

type deleteObjectInput struct {
	mutationContext
	ObjectID uint64 `json:"object_id"`
}

type duplicateObjectsInput struct {
	mutationContext
	targetSpec
	FrameOffset int `json:"frame_offset" jsonschema:"frame offset applied for each repetition"`
	LayerOffset int `json:"layer_offset,omitempty" jsonschema:"layer offset applied for each repetition"`
	Repeat      int `json:"repeat,omitempty" jsonschema:"number of copies, default 1 and maximum 20"`
}

type effectMutationInput struct {
	mutationContext
	ObjectID    uint64 `json:"object_id"`
	Effect      string `json:"effect,omitempty" jsonschema:"effect name for add_effect"`
	EffectIndex int    `json:"effect_index,omitempty" jsonschema:"zero-based effect index for delete or state changes"`
	Index       *int   `json:"index,omitempty" jsonschema:"new effect order for set_effect_state"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Locked      *bool  `json:"locked,omitempty"`
}

type batchInput struct {
	mutationContext
	Operations    []protocol.BatchOperation `json:"operations" jsonschema:"ordered operations executed in one AviUtl2 edit section and Undo unit"`
	DryRun        bool                      `json:"dry_run,omitempty"`
	ReturnObjects bool                      `json:"return_objects,omitempty"`
	TimeoutMS     int                       `json:"timeout_ms,omitempty" jsonschema:"optional execution timeout in milliseconds"`
}

type mutationOutput struct {
	Mutation protocol.MutationResult `json:"mutation"`
}

type batchOutput struct {
	DryRun     bool                      `json:"dry_run"`
	Valid      bool                      `json:"valid"`
	Operations []protocol.BatchOperation `json:"operations"`
	Mutation   *protocol.MutationResult  `json:"mutation,omitempty"`
	Objects    []protocol.Object         `json:"objects,omitempty"`
	ElapsedMS  int64                     `json:"elapsed_ms"`
}

type previewInput struct {
	Frame        int    `json:"frame" jsonschema:"zero-based frame to render"`
	MaxWidth     int    `json:"max_width,omitempty" jsonschema:"maximum preview width, default 640 and maximum 800"`
	MaxHeight    int    `json:"max_height,omitempty" jsonschema:"maximum preview height, default 640 and maximum 800"`
	ObjectID     uint64 `json:"object_id,omitempty" jsonschema:"render only this object when nonzero"`
	ApplyEffects bool   `json:"apply_effects,omitempty" jsonschema:"apply additional effects when rendering an object"`
}

type previewOutput struct {
	Frame      int    `json:"frame"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SessionID  string `json:"session_id"`
	Generation uint64 `json:"generation"`
}

func New(client *bridge.Client, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "aviutl2-mcp", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "Check whether the native bridge is loaded in a running AviUtl2 instance."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, pingOutput, error) {
			result, err := client.Ping(ctx)
			return nil, pingOutput(result), err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_context", Description: "Get scene, cursor, selection range, display range, dimensions, and concurrency tokens. Call this before mutations."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, contextOutput, error) {
			result, err := client.GetContext(ctx)
			return nil, contextOutput{Context: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "inspect_timeline", Description: "Inspect objects and layer states in a bounded timeline range."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input inspectTimelineInput) (*mcp.CallToolResult, timelineOutput, error) {
			if input.LayerStart < 0 || input.LayerEnd < input.LayerStart || input.LayerEnd-input.LayerStart > 99 {
				return nil, timelineOutput{}, errors.New("layer range must be ordered, non-negative, and at most 100 layers")
			}
			if input.FrameStart < 0 || input.FrameEnd < input.FrameStart {
				return nil, timelineOutput{}, errors.New("frame range must be ordered and non-negative")
			}
			if input.MaxObjects == 0 {
				input.MaxObjects = 200
			}
			if input.MaxObjects < 1 || input.MaxObjects > protocol.MaxTimelineObjects {
				return nil, timelineOutput{}, fmt.Errorf("max_objects must be between 1 and %d", protocol.MaxTimelineObjects)
			}
			result, err := client.InspectTimeline(ctx, protocol.InspectTimelineParams(input))
			return nil, timelineOutput{Timeline: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "inspect_object", Description: "Inspect one object, including placement, sections, alias data, and effect states."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input inspectObjectInput) (*mcp.CallToolResult, objectOutput, error) {
			result, err := client.InspectObject(ctx, protocol.InspectObjectParams(input))
			return nil, objectOutput{Object: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "inspect_objects", Description: "Inspect up to one hundred objects in one bridge read section."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input inspectObjectsInput) (*mcp.CallToolResult, objectsOutput, error) {
			if len(input.ObjectIDs) < 1 || len(input.ObjectIDs) > protocol.MaxBatchOperations {
				return nil, objectsOutput{}, errors.New("object_ids must contain between 1 and 100 entries")
			}
			result, err := client.InspectObjects(ctx, protocol.InspectObjectsParams(input))
			return nil, objectsOutput{Objects: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "inspect_object_values", Description: "Inspect every effect item on an object, including raw values, track metadata, and values sampled at a frame."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input inspectObjectValuesInput) (*mcp.CallToolResult, objectValuesOutput, error) {
			if input.ObjectID == 0 || (input.Frame != nil && *input.Frame < 0) {
				return nil, objectValuesOutput{}, errors.New("object_id is required and frame must be non-negative")
			}
			result, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams(input))
			return nil, objectValuesOutput{Values: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "list_effects", Description: "List effect definitions registered in AviUtl2."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, effectsOutput, error) {
			result, err := client.ListEffects(ctx)
			return nil, effectsOutput{Effects: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "list_effect_items", Description: "List configurable item names and types for an effect."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input effectItemsInput) (*mcp.CallToolResult, effectItemsOutput, error) {
			if strings.TrimSpace(input.Effect) == "" {
				return nil, effectItemsOutput{}, errors.New("effect is required")
			}
			result, err := client.ListEffectItems(ctx, input.Effect)
			return nil, effectItemsOutput{Effect: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_selection", Description: "Get the focus object and all currently selected timeline objects."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, selectionOutput, error) {
			result, err := client.GetSelection(ctx)
			return nil, selectionOutput{Selection: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "preflight_media", Description: "Check whether AviUtl2 supports a media file and return its metadata without adding it."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input preflightMediaInput) (*mcp.CallToolResult, mediaOutput, error) {
			if strings.TrimSpace(input.File) == "" {
				return nil, mediaOutput{}, errors.New("file is required")
			}
			result, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams(input))
			return nil, mediaOutput{Media: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "add_text", Description: "Add a text object. Requires current session_id, generation, and scene_id to prevent editing stale context."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input addTextInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if input.Text == "" || input.Layer < 0 || input.Frame < 0 || input.Length < 1 {
				return nil, mutationOutput{}, errors.New("text is required and layer/frame/length must be valid")
			}
			if input.Size == 0 {
				input.Size = 34
			}
			if input.Color == "" {
				input.Color = "ffffff"
			}
			if input.Size <= 0 || !colorPattern.MatchString(input.Color) {
				return nil, mutationOutput{}, errors.New("size must be positive and color must be six hexadecimal digits")
			}
			params := protocol.AddTextParams{Text: input.Text, Layer: input.Layer, Frame: input.Frame, Length: input.Length, Size: input.Size, Color: strings.ToLower(input.Color)}
			result, err := client.AddText(ctx, params, input.expected())
			return nil, mutationOutput{Mutation: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "add_media", Description: "Add a local media file to the timeline using optimistic context checks."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input addMediaInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if input.File == "" || input.Layer < 0 || input.Frame < 0 || input.Length < 0 {
				return nil, mutationOutput{}, errors.New("file is required and layer/frame/length must be non-negative")
			}
			result, err := client.AddMedia(ctx, protocol.AddMediaParams{File: input.File, Layer: input.Layer, Frame: input.Frame, Length: input.Length}, input.expected())
			return nil, mutationOutput{Mutation: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_object", Description: "Move, rename, or update effect item values on an existing object in one Undo unit."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateObjectInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if input.ObjectID == 0 || (input.Layer == nil && input.Frame == nil && input.Name == nil && len(input.Properties) == 0) {
				return nil, mutationOutput{}, errors.New("object_id and at least one update are required")
			}
			if (input.Layer != nil && *input.Layer < 0) || (input.Frame != nil && *input.Frame < 0) {
				return nil, mutationOutput{}, errors.New("layer and frame must be non-negative")
			}
			params := protocol.UpdateObjectParams{ObjectID: input.ObjectID, Layer: input.Layer, Frame: input.Frame, Name: input.Name, Properties: input.Properties}
			result, err := client.UpdateObject(ctx, params, input.expected())
			return nil, mutationOutput{Mutation: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "delete_object", Description: "Delete an object in the expected AviUtl2 scene and generation."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input deleteObjectInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if input.ObjectID == 0 {
				return nil, mutationOutput{}, errors.New("object_id is required")
			}
			result, err := client.DeleteObject(ctx, input.ObjectID, input.expected())
			return nil, mutationOutput{Mutation: result}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "duplicate_objects", Description: "Duplicate objects with all effects and animation. Repetitions are created in one Undo unit."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input duplicateObjectsInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if input.Repeat == 0 {
				input.Repeat = 1
			}
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, mutationOutput{}, err
			}
			if input.Repeat < 1 || input.Repeat > 20 || len(objects)*input.Repeat > protocol.MaxBatchOperations {
				return nil, mutationOutput{}, errors.New("repeat must be between 1 and 20 and total copies must not exceed 100")
			}
			operations := make([]protocol.BatchOperation, 0, len(objects)*input.Repeat)
			for repetition := 1; repetition <= input.Repeat; repetition++ {
				for _, object := range objects {
					layer := object.Layer + input.LayerOffset*repetition
					frame := object.Start + input.FrameOffset*repetition
					if layer < 0 || frame < 0 {
						return nil, mutationOutput{}, errors.New("duplicate target layer and frame must be non-negative")
					}
					operations = append(operations, protocol.BatchOperation{
						Op: "duplicate_object", ObjectID: object.ID, Layer: &layer, Frame: &frame,
						Length: object.End - object.Start + 1,
					})
				}
			}
			result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: operations}, input.expected())
			return nil, mutationOutput{Mutation: result}, err
		})

	addEffectTool(server, client, "add_effect", "Add or replace an effect on an object.")
	addEffectTool(server, client, "delete_effect", "Delete an effect by zero-based index.")
	addEffectTool(server, client, "set_effect_state", "Enable, lock, or reorder an effect by zero-based index.")

	mcp.AddTool(server, &mcp.Tool{Name: "execute_batch", Description: "Preflight and execute up to 100 operations in one AviUtl2 edit section and Undo unit. Supports dry-run, timeout, progress, and result inspection."},
		func(ctx context.Context, request *mcp.CallToolRequest, input batchInput) (*mcp.CallToolResult, batchOutput, error) {
			started := time.Now()
			output := batchOutput{DryRun: input.DryRun, Operations: input.Operations}
			if err := input.mutationContext.validate(); err != nil {
				return nil, output, err
			}
			if len(input.Operations) == 0 || len(input.Operations) > protocol.MaxBatchOperations {
				return nil, output, fmt.Errorf("operations must contain between 1 and %d entries", protocol.MaxBatchOperations)
			}
			if input.TimeoutMS < 0 || input.TimeoutMS > 300000 {
				return nil, output, errors.New("timeout_ms must be between 0 and 300000")
			}
			if err := validateBatchOperations(input.Operations); err != nil {
				return nil, output, err
			}
			if err := preflightBatch(ctx, client, input.mutationContext, input.Operations); err != nil {
				return nil, output, err
			}
			output.Valid = true
			if input.DryRun {
				output.ElapsedMS = time.Since(started).Milliseconds()
				return nil, output, nil
			}
			if input.TimeoutMS > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(input.TimeoutMS)*time.Millisecond)
				defer cancel()
			}
			notifyProgress(ctx, request, 0, 1, "Executing AviUtl2 batch")
			result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: input.Operations}, input.expected())
			output.Mutation = &result
			if err == nil && input.ReturnObjects {
				output.Objects = inspectBatchResults(ctx, client, input.Operations, result)
			}
			output.ElapsedMS = time.Since(started).Milliseconds()
			notifyProgress(ctx, request, 1, 1, "AviUtl2 batch complete")
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "render_preview", Description: "Render a bounded PNG preview of the scene or one object at a frame."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input previewInput) (*mcp.CallToolResult, previewOutput, error) {
			if input.Frame < 0 {
				return nil, previewOutput{}, errors.New("frame must be non-negative")
			}
			if input.MaxWidth == 0 {
				input.MaxWidth = protocol.DefaultPreviewWidth
			}
			if input.MaxHeight == 0 {
				input.MaxHeight = protocol.DefaultPreviewWidth
			}
			if input.MaxWidth < 1 || input.MaxWidth > 800 || input.MaxHeight < 1 || input.MaxHeight > 800 {
				return nil, previewOutput{}, errors.New("preview dimensions must be between 1 and 800")
			}
			preview, err := client.RenderPreview(ctx, protocol.RenderPreviewParams(input))
			if err != nil {
				return nil, previewOutput{}, err
			}
			pngData, err := previewPNG(preview)
			if err != nil {
				return nil, previewOutput{}, err
			}
			content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: pngData, MIMEType: "image/png"}}}
			output := previewOutput{Frame: preview.Frame, Width: preview.Width, Height: preview.Height, SessionID: preview.SessionID, Generation: preview.Generation}
			return content, output, nil
		})

	addTimelineTools(server, client)
	addContactSheetTool(server, client)
	addQueryTools(server, client)
	addDiagnosticsTools(server, client, version)
	addAdvancedEditTools(server, client)
	addMediaTools(server, client)
	addVisualTools(server, client)
	addOrganizationTools(server, client)

	return server
}

func addEffectTool(server *mcp.Server, client *bridge.Client, name, description string) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description},
		func(ctx context.Context, _ *mcp.CallToolRequest, input effectMutationInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if input.ObjectID == 0 {
				return nil, mutationOutput{}, errors.New("object_id is required")
			}
			if name == "add_effect" && input.Effect == "" {
				return nil, mutationOutput{}, errors.New("effect is required")
			}
			if name != "add_effect" && input.EffectIndex < 0 {
				return nil, mutationOutput{}, errors.New("effect_index must be non-negative")
			}
			if name == "set_effect_state" && input.Index == nil && input.Enabled == nil && input.Locked == nil {
				return nil, mutationOutput{}, errors.New("set_effect_state requires index, enabled, or locked")
			}
			params := protocol.EffectMutationParams{ObjectID: input.ObjectID, Effect: input.Effect, EffectIndex: input.EffectIndex, Index: input.Index, Enabled: input.Enabled, Locked: input.Locked}
			result, err := client.MutateEffect(ctx, name, params, input.expected())
			return nil, mutationOutput{Mutation: result}, err
		})
}

func validateBatchOperations(operations []protocol.BatchOperation) error {
	for index, operation := range operations {
		prefix := fmt.Sprintf("operations[%d]", index)
		switch operation.Op {
		case "add_text":
			if operation.Text == "" || operation.Layer == nil || *operation.Layer < 0 || operation.Frame == nil || *operation.Frame < 0 || operation.Length < 1 {
				return fmt.Errorf("%s add_text requires text, non-negative layer/frame, and positive length", prefix)
			}
			if operation.Size < 0 || (operation.Color != "" && !colorPattern.MatchString(operation.Color)) {
				return fmt.Errorf("%s has an invalid size or color", prefix)
			}
		case "add_media":
			if strings.TrimSpace(operation.File) == "" || operation.Layer == nil || *operation.Layer < 0 || operation.Frame == nil || *operation.Frame < 0 || operation.Length < 0 {
				return fmt.Errorf("%s add_media requires a file and non-negative layer/frame/length", prefix)
			}
		case "duplicate_object":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.Layer == nil || *operation.Layer < 0 || operation.Frame == nil || *operation.Frame < 0 || operation.Length < 1 {
				return fmt.Errorf("%s duplicate_object requires non-negative layer/frame and positive length", prefix)
			}
		case "replace_media":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if strings.TrimSpace(operation.File) == "" || (operation.Item != "" && strings.TrimSpace(operation.Effect) == "") {
				return fmt.Errorf("%s requires file; item also requires effect", prefix)
			}
		case "update_object":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.Layer == nil && operation.Frame == nil && operation.Name == nil && len(operation.Properties) == 0 {
				return fmt.Errorf("%s requires at least one update", prefix)
			}
			if (operation.Layer != nil && *operation.Layer < 0) || (operation.Frame != nil && *operation.Frame < 0) {
				return fmt.Errorf("%s layer and frame must be non-negative", prefix)
			}
		case "delete_object":
			if operation.ResultRef != nil {
				return fmt.Errorf("%s cannot delete an object created in the same batch", prefix)
			}
			if operation.ObjectID == 0 {
				return fmt.Errorf("%s requires object_id", prefix)
			}
		case "create_section":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.Frame == nil || *operation.Frame < 0 {
				return fmt.Errorf("%s requires a non-negative frame", prefix)
			}
		case "delete_section":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.Section == nil || *operation.Section < 1 {
				return fmt.Errorf("%s requires a positive section index", prefix)
			}
		case "move_section":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.Section == nil || *operation.Section < 0 || operation.Frame == nil || *operation.Frame < 0 {
				return fmt.Errorf("%s requires non-negative section and frame", prefix)
			}
		case "set_layer_state":
			if operation.Layer == nil || *operation.Layer < 0 || (operation.Name == nil && operation.Enabled == nil && operation.Locked == nil) {
				return fmt.Errorf("%s requires a layer and at least one state update", prefix)
			}
		case "set_scene_settings":
			if operation.Name == nil && operation.Width == nil && operation.Height == nil && operation.Rate == nil && operation.Scale == nil && operation.SampleRate == nil {
				return fmt.Errorf("%s requires at least one scene update", prefix)
			}
			if (operation.Width == nil) != (operation.Height == nil) || (operation.Rate == nil) != (operation.Scale == nil) {
				return fmt.Errorf("%s requires width/height and rate/scale in pairs", prefix)
			}
			if (operation.Width != nil && (*operation.Width < 1 || *operation.Height < 1)) ||
				(operation.Rate != nil && (*operation.Rate < 1 || *operation.Scale < 1)) ||
				(operation.SampleRate != nil && *operation.SampleRate < 1) {
				return fmt.Errorf("%s scene numeric values must be positive", prefix)
			}
		case "set_marker", "clear_marker":
			if operation.Frame == nil || *operation.Frame < 0 {
				return fmt.Errorf("%s requires a non-negative frame", prefix)
			}
		case "set_grid_bpm":
			if operation.Tempo == nil || *operation.Tempo <= 0 || operation.Beat == nil || *operation.Beat < 1 {
				return fmt.Errorf("%s requires positive tempo and beat", prefix)
			}
		case "set_grid_bpm_list":
			if len(operation.BPMPoints) == 0 || len(operation.BPMPoints) > 100 {
				return fmt.Errorf("%s requires 1..100 bpm_points", prefix)
			}
			for _, point := range operation.BPMPoints {
				if point.Tempo <= 0 || point.Beat < 1 || point.Start < 0 {
					return fmt.Errorf("%s contains an invalid BPM point", prefix)
				}
			}
		case "move_marker":
			if operation.Frame == nil || *operation.Frame < 0 || operation.FrameTo == nil || *operation.FrameTo < 0 {
				return fmt.Errorf("%s requires non-negative frame and frame_to", prefix)
			}
		case "set_cursor", "set_display":
			if operation.Layer == nil || *operation.Layer < 0 || operation.Frame == nil || *operation.Frame < 0 {
				return fmt.Errorf("%s requires non-negative layer and frame", prefix)
			}
		case "set_selection_range":
			if operation.Start == nil || operation.End == nil ||
				!((*operation.Start == -1 && *operation.End == -1) || (*operation.Start >= 0 && *operation.End >= *operation.Start)) {
				return fmt.Errorf("%s requires an ordered range or -1/-1 to clear", prefix)
			}
		case "add_effect":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if strings.TrimSpace(operation.Effect) == "" {
				return fmt.Errorf("%s requires effect", prefix)
			}
		case "delete_effect":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.EffectIndex < 0 {
				return fmt.Errorf("%s effect_index must be non-negative", prefix)
			}
		case "set_effect_state":
			if err := validateBatchObjectReference(prefix, index, operation); err != nil {
				return err
			}
			if operation.EffectIndex < 0 || (operation.Index == nil && operation.Enabled == nil && operation.Locked == nil) {
				return fmt.Errorf("%s requires a valid effect_index and at least one state update", prefix)
			}
		default:
			return fmt.Errorf("%s has unknown op %q", prefix, operation.Op)
		}
		for propertyIndex, property := range operation.Properties {
			if strings.TrimSpace(property.Effect) == "" || strings.TrimSpace(property.Item) == "" {
				return fmt.Errorf("%s properties[%d] requires effect and item", prefix, propertyIndex)
			}
		}
	}
	return nil
}

func validateBatchObjectReference(prefix string, operationIndex int, operation protocol.BatchOperation) error {
	if operation.ResultRef == nil {
		if operation.ObjectID == 0 {
			return fmt.Errorf("%s requires object_id or result_ref", prefix)
		}
		return nil
	}
	if operation.ObjectID != 0 || *operation.ResultRef < 0 || *operation.ResultRef >= operationIndex {
		return fmt.Errorf("%s result_ref must exclusively reference an earlier operation", prefix)
	}
	return nil
}

func inspectObjects(ctx context.Context, client *bridge.Client, objectIDs []uint64, expected mutationContext) ([]protocol.Object, error) {
	for _, objectID := range objectIDs {
		if objectID == 0 {
			return nil, errors.New("object_ids must not contain zero")
		}
	}
	result, err := client.InspectObjects(ctx, protocol.InspectObjectsParams{ObjectIDs: objectIDs, IncludeEffects: true})
	if err != nil {
		return nil, err
	}
	if result.Context.SessionID != expected.SessionID || result.Context.Generation != expected.Generation || result.Context.SceneID != expected.SceneID {
		return nil, errors.New("AviUtl2 context changed while planning; call get_context and retry")
	}
	return result.Objects, nil
}

func previewPNG(preview protocol.PreviewResult) ([]byte, error) {
	img, err := previewImage(preview)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		return nil, fmt.Errorf("encode preview PNG: %w", err)
	}
	return output.Bytes(), nil
}

func previewImage(preview protocol.PreviewResult) (*image.NRGBA, error) {
	if preview.Width < 1 || preview.Height < 1 || preview.Width > 800 || preview.Height > 800 {
		return nil, errors.New("bridge returned invalid preview dimensions")
	}
	pixels, err := base64.StdEncoding.DecodeString(preview.RGBA)
	if err != nil {
		return nil, fmt.Errorf("decode preview pixels: %w", err)
	}
	want := preview.Width * preview.Height * 4
	if len(pixels) != want {
		return nil, fmt.Errorf("preview pixel length is %d, want %d", len(pixels), want)
	}
	return &image.NRGBA{Pix: pixels, Stride: preview.Width * 4, Rect: image.Rect(0, 0, preview.Width, preview.Height)}, nil
}
