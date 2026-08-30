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
	Operations []protocol.BatchOperation `json:"operations" jsonschema:"ordered operations executed in one AviUtl2 edit section and Undo unit"`
}

type mutationOutput struct {
	Mutation protocol.MutationResult `json:"mutation"`
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

	addEffectTool(server, client, "add_effect", "Add or replace an effect on an object.")
	addEffectTool(server, client, "delete_effect", "Delete an effect by zero-based index.")
	addEffectTool(server, client, "set_effect_state", "Enable, lock, or reorder an effect by zero-based index.")

	mcp.AddTool(server, &mcp.Tool{Name: "execute_batch", Description: "Execute up to 100 operations in one AviUtl2 edit section and Undo unit. Earlier changes remain if a later operation fails."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input batchInput) (*mcp.CallToolResult, mutationOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, mutationOutput{}, err
			}
			if len(input.Operations) == 0 || len(input.Operations) > protocol.MaxBatchOperations {
				return nil, mutationOutput{}, fmt.Errorf("operations must contain between 1 and %d entries", protocol.MaxBatchOperations)
			}
			if err := validateBatchOperations(input.Operations); err != nil {
				return nil, mutationOutput{}, err
			}
			result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: input.Operations}, input.expected())
			return nil, mutationOutput{Mutation: result}, err
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

func previewPNG(preview protocol.PreviewResult) ([]byte, error) {
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
	img := &image.NRGBA{Pix: pixels, Stride: preview.Width * 4, Rect: image.Rect(0, 0, preview.Width, preview.Height)}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		return nil, fmt.Errorf("encode preview PNG: %w", err)
	}
	return output.Bytes(), nil
}
