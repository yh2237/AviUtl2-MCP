package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type markerEdit struct {
	Op      string  `json:"op" jsonschema:"set, clear, or move"`
	Frame   int     `json:"frame"`
	FrameTo *int    `json:"frame_to,omitempty"`
	Memo    *string `json:"memo,omitempty"`
}

type markersInput struct {
	mutationContext
	Operation    string       `json:"operation" jsonschema:"list, edit, split_objects, or place_objects"`
	Markers      []markerEdit `json:"markers,omitempty" jsonschema:"entries for operation=edit"`
	MarkerFrames []int        `json:"marker_frames,omitempty" jsonschema:"explicit marker frames; defaults to every marker in the scene"`
	DryRun       bool         `json:"dry_run,omitempty"`
	targetSpec
}

type markersOutput struct {
	Operation  string                   `json:"operation"`
	Context    *protocol.Context        `json:"context,omitempty"`
	Markers    []protocol.Marker        `json:"markers,omitempty"`
	DryRun     bool                     `json:"dry_run,omitempty"`
	Operations []any                    `json:"operations,omitempty"`
	Mutation   *protocol.MutationResult `json:"mutation,omitempty"`
	Objects    []protocol.Object        `json:"objects,omitempty"`
}

type bpmInput struct {
	mutationContext
	Operation string              `json:"operation" jsonschema:"get, set, or set_list"`
	Tempo     float32             `json:"tempo,omitempty"`
	Beat      int                 `json:"beat,omitempty"`
	Offset    float32             `json:"offset,omitempty"`
	Points    []protocol.BPMPoint `json:"points,omitempty" jsonschema:"full variable-tempo grid for operation=set_list"`
	DryRun    bool                `json:"dry_run,omitempty"`
}

type bpmOutput struct {
	Operation  string                   `json:"operation"`
	Context    *protocol.Context        `json:"context,omitempty"`
	Points     []protocol.BPMPoint      `json:"points,omitempty"`
	DryRun     bool                     `json:"dry_run,omitempty"`
	Operations []any                    `json:"operations,omitempty"`
	Mutation   *protocol.MutationResult `json:"mutation,omitempty"`
}

type sceneSettingsInput struct {
	mutationContext
	Name       *string `json:"name,omitempty"`
	Width      *int    `json:"width,omitempty"`
	Height     *int    `json:"height,omitempty"`
	Rate       *int    `json:"rate,omitempty"`
	Scale      *int    `json:"scale,omitempty"`
	SampleRate *int    `json:"sample_rate,omitempty"`
	DryRun     bool    `json:"dry_run,omitempty"`
}

type findLayersInput struct {
	LayerFilter  string `json:"layer_filter,omitempty" jsonschema:"used, empty, or all; default used"`
	NameContains string `json:"name_contains,omitempty"`
	LayerStart   int    `json:"layer_start,omitempty"`
	LayerEnd     *int   `json:"layer_end,omitempty"`
}

type findLayersOutput struct {
	Context protocol.Context `json:"context"`
	Layers  []protocol.Layer `json:"layers"`
}

type editLayersInput struct {
	mutationContext
	Operation  string   `json:"operation" jsonschema:"name or color"`
	LayerStart int      `json:"layer_start"`
	LayerEnd   *int     `json:"layer_end,omitempty"`
	Prefix     string   `json:"prefix,omitempty" jsonschema:"prefix plus a one-based sequence for the name operation"`
	Color      string   `json:"color,omitempty" jsonschema:"color operation: RRGGBB, RRGGBBAA, or empty to restore the default"`
	Colors     []string `json:"colors,omitempty" jsonschema:"color operation: one entry per layer"`
	DryRun     bool     `json:"dry_run,omitempty"`
}

var layerColorPattern = regexp.MustCompile(`^([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// normalizeLayerColor accepts "#RRGGBB", "RRGGBBAA", or a keyword that clears the color.
func normalizeLayerColor(value string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "#")
	switch strings.ToLower(trimmed) {
	case "", "default", "none", "clear":
		return "", nil
	}
	if !layerColorPattern.MatchString(trimmed) {
		return "", fmt.Errorf("layer color %q must be six or eight hexadecimal digits, or empty/\"default\" to clear it", value)
	}
	return strings.ToLower(trimmed), nil
}

func planResult(operation string, plan operationPlanOutput) any {
	operations := make([]any, 0, len(plan.Operations))
	for _, operation := range plan.Operations {
		operations = append(operations, operation)
	}
	return map[string]any{
		"operation":  operation,
		"dry_run":    plan.DryRun,
		"operations": operations,
		"mutation":   plan.Mutation,
		"objects":    plan.Objects,
	}
}

func runMarkers(ctx context.Context, client *bridge.Client, input markersInput) (markersOutput, error) {
	out := markersOutput{Operation: input.Operation}
	switch input.Operation {
	case "list":
		result, err := client.GetMarkers(ctx)
		if err != nil {
			return out, err
		}
		out.Context = &result.Context
		out.Markers = result.Markers
		return out, nil
	case "edit":
		if err := input.mutationContext.validate(); err != nil {
			return out, err
		}
		ops := []protocol.BatchOperation{}
		for _, marker := range input.Markers {
			frame := marker.Frame
			switch marker.Op {
			case "set":
				ops = append(ops, protocol.BatchOperation{Op: "set_marker", Frame: &frame, Memo: marker.Memo})
			case "clear":
				ops = append(ops, protocol.BatchOperation{Op: "clear_marker", Frame: &frame})
			case "move":
				if marker.FrameTo == nil {
					return out, errors.New("move marker requires frame_to")
				}
				ops = append(ops, protocol.BatchOperation{Op: "move_marker", Frame: &frame, FrameTo: marker.FrameTo})
			default:
				return out, errors.New("marker op must be set, clear, or move")
			}
		}
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
		out.DryRun, out.Operations, out.Mutation, out.Objects = plan.DryRun, plan.Operations, plan.Mutation, plan.Objects
		return out, err
	case "split_objects":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return out, err
		}
		frames, err := resolveMarkerFrames(ctx, client, input.MarkerFrames)
		if err != nil {
			return out, err
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			for _, frame := range frames {
				if frame > o.Start && frame <= o.End && !containsInt(o.Sections, frame) {
					f := frame
					ops = append(ops, protocol.BatchOperation{Op: "create_section", ObjectID: o.ID, Frame: &f})
				}
			}
		}
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		out.DryRun, out.Operations, out.Mutation, out.Objects = plan.DryRun, plan.Operations, plan.Mutation, plan.Objects
		return out, err
	case "place_objects":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return out, err
		}
		frames, err := resolveMarkerFrames(ctx, client, input.MarkerFrames)
		if err != nil {
			return out, err
		}
		if len(frames) < len(objects) {
			return out, errors.New("not enough markers for target objects")
		}
		moves := []plannedMove{}
		for i, o := range objects {
			moves = append(moves, moveFor(o, o.Layer, frames[i]))
		}
		_, result, err := executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		out.DryRun, out.Operations, out.Mutation = result.DryRun, toAnyOperations(result.Operations), result.Mutation
		return out, err
	default:
		return out, fmt.Errorf("unknown operation %q; use list, edit, split_objects, or place_objects", input.Operation)
	}
}

func runBPM(ctx context.Context, client *bridge.Client, input bpmInput) (bpmOutput, error) {
	out := bpmOutput{Operation: input.Operation}
	switch input.Operation {
	case "get":
		result, err := client.GetBPMGrid(ctx)
		if err != nil {
			return out, err
		}
		out.Context = &result.Context
		out.Points = result.Points
		return out, nil
	case "set":
		tempo, beat, offset := input.Tempo, input.Beat, input.Offset
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext,
			[]protocol.BatchOperation{{Op: "set_grid_bpm", Tempo: &tempo, Beat: &beat, Offset: &offset}}, input.DryRun, false)
		out.DryRun, out.Operations, out.Mutation = plan.DryRun, plan.Operations, plan.Mutation
		return out, err
	case "set_list":
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext,
			[]protocol.BatchOperation{{Op: "set_grid_bpm_list", BPMPoints: input.Points}}, input.DryRun, false)
		out.DryRun, out.Operations, out.Mutation = plan.DryRun, plan.Operations, plan.Mutation
		return out, err
	default:
		return out, fmt.Errorf("unknown operation %q; use get, set, or set_list", input.Operation)
	}
}

func runFindLayers(ctx context.Context, client *bridge.Client, input findLayersInput) (findLayersOutput, error) {
	layerFilter := input.LayerFilter
	if layerFilter == "" {
		layerFilter = "used"
	}
	if layerFilter != "used" && layerFilter != "empty" && layerFilter != "all" {
		return findLayersOutput{}, errors.New("layer_filter must be used, empty, or all")
	}
	current, objects, err := readAllTimeline(ctx, client)
	if err != nil {
		return findLayersOutput{}, err
	}
	end := current.LayerMax
	if input.LayerEnd != nil {
		end = *input.LayerEnd
	}
	if input.LayerStart < 0 || end < input.LayerStart {
		return findLayersOutput{}, errors.New("invalid layer range")
	}
	used := map[int]bool{}
	for _, object := range objects {
		used[object.Layer] = true
	}
	layers := []protocol.Layer{}
	for first := input.LayerStart; first <= end; first += 100 {
		last := min(first+99, end)
		value, err := client.InspectTimeline(ctx, protocol.InspectTimelineParams{
			LayerStart: first, LayerEnd: last, FrameStart: 0, FrameEnd: max(0, current.FrameMax), MaxObjects: 1,
		})
		if err != nil {
			return findLayersOutput{}, err
		}
		for _, layer := range value.Layers {
			switch layerFilter {
			case "used":
				if !used[layer.Index] {
					continue
				}
			case "empty":
				if used[layer.Index] {
					continue
				}
			}
			if input.NameContains != "" && !strings.Contains(strings.ToLower(layer.Name), strings.ToLower(input.NameContains)) {
				continue
			}
			layers = append(layers, layer)
		}
	}
	return findLayersOutput{Context: current, Layers: layers}, nil
}

func runEditLayers(ctx context.Context, client *bridge.Client, input editLayersInput) (operationPlanOutput, error) {
	if err := input.mutationContext.validate(); err != nil {
		return operationPlanOutput{}, err
	}
	if input.LayerStart < 0 {
		return operationPlanOutput{}, errors.New("layer_start must be non-negative")
	}
	end := input.LayerStart
	if input.LayerEnd != nil {
		end = *input.LayerEnd
	}
	if end < input.LayerStart {
		return operationPlanOutput{}, errors.New("layer_end must not be smaller than layer_start")
	}
	count := end - input.LayerStart + 1
	if count > protocol.MaxBatchOperations {
		return operationPlanOutput{}, fmt.Errorf("layer range covers %d layers; edit at most %d per batch", count, protocol.MaxBatchOperations)
	}
	ops := make([]protocol.BatchOperation, 0, count)
	switch input.Operation {
	case "name":
		if input.Prefix == "" {
			return operationPlanOutput{}, errors.New("prefix is required for operation=name")
		}
		for offset := 0; offset < count; offset++ {
			layer := input.LayerStart + offset
			name := fmt.Sprintf("%s%d", input.Prefix, offset+1)
			ops = append(ops, protocol.BatchOperation{Op: "set_layer_state", Layer: &layer, Name: &name})
		}
	case "color":
		requested := make([]string, count)
		if len(input.Colors) > 0 {
			if len(input.Colors) != count {
				return operationPlanOutput{}, fmt.Errorf("colors must contain exactly one entry per layer (%d)", count)
			}
			copy(requested, input.Colors)
		} else {
			for index := range requested {
				requested[index] = input.Color
			}
		}
		for offset, raw := range requested {
			color, err := normalizeLayerColor(raw)
			if err != nil {
				return operationPlanOutput{}, err
			}
			layer := input.LayerStart + offset
			ops = append(ops, protocol.BatchOperation{Op: "set_layer_state", Layer: &layer, LayerColor: &color})
		}
	default:
		return operationPlanOutput{}, fmt.Errorf("unknown operation %q; use name or color", input.Operation)
	}
	_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
	return plan, err
}

func addOrganizationTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "markers", Description: "List markers, edit them in one Undo unit, split objects at marker frames, or place objects on markers."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input markersInput) (*mcp.CallToolResult, markersOutput, error) {
			result, err := runMarkers(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "bpm_grid", Description: "Read the BPM grid, set the primary tempo, or replace the full variable-tempo grid."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input bpmInput) (*mcp.CallToolResult, bpmOutput, error) {
			result, err := runBPM(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "find_layers", Description: "Find layers by occupancy and name in the current scene."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input findLayersInput) (*mcp.CallToolResult, findLayersOutput, error) {
			result, err := runFindLayers(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "edit_layers", Description: "Name or color a contiguous layer range. Colors need AviUtl2 2.1.13+; an empty color restores the default."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input editLayersInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			result, err := runEditLayers(ctx, client, input)
			return nil, result, err
		})
	addTool(server, &mcp.Tool{Name: "set_scene_settings", Description: "Update current scene name, dimensions, frame rate, or sample rate. AviUtl2 cannot Undo scene settings."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input sceneSettingsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			op := protocol.BatchOperation{Op: "set_scene_settings", Name: input.Name, Width: input.Width, Height: input.Height, Rate: input.Rate, Scale: input.Scale, SampleRate: input.SampleRate}
			return executeOperationPlan(ctx, client, input.mutationContext, []protocol.BatchOperation{op}, input.DryRun, false)
		})
}

func resolveMarkerFrames(ctx context.Context, client *bridge.Client, explicit []int) ([]int, error) {
	frames := append([]int(nil), explicit...)
	if len(frames) == 0 {
		markers, err := client.GetMarkers(ctx)
		if err != nil {
			return nil, err
		}
		for _, marker := range markers.Markers {
			frames = append(frames, marker.Frame)
		}
	}
	sort.Ints(frames)
	if len(frames) == 0 {
		return nil, errors.New("no marker frames")
	}
	return frames, nil
}
