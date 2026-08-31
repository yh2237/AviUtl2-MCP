package mcpserver

import (
	"context"
	"errors"
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
type editMarkersInput struct {
	mutationContext
	Markers []markerEdit `json:"markers"`
	DryRun  bool         `json:"dry_run,omitempty"`
}
type markerObjectsInput struct {
	mutationContext
	targetSpec
	MarkerFrames []int `json:"marker_frames,omitempty"`
	DryRun       bool  `json:"dry_run,omitempty"`
}
type setBPMInput struct {
	mutationContext
	Tempo  float32 `json:"tempo"`
	Beat   int     `json:"beat"`
	Offset float32 `json:"offset,omitempty"`
	DryRun bool    `json:"dry_run,omitempty"`
}
type setBPMListInput struct {
	mutationContext
	Points []protocol.BPMPoint `json:"points"`
	DryRun bool                `json:"dry_run,omitempty"`
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
	NameContains string `json:"name_contains,omitempty"`
	IncludeEmpty bool   `json:"include_empty,omitempty"`
	LayerStart   int    `json:"layer_start,omitempty"`
	LayerEnd     *int   `json:"layer_end,omitempty"`
}
type findLayersOutput struct {
	Context protocol.Context `json:"context"`
	Layers  []protocol.Layer `json:"layers"`
}

func addOrganizationTools(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "edit_markers", Description: "Set, clear, or move several markers in one Undo unit."}, func(ctx context.Context, _ *mcp.CallToolRequest, input editMarkersInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		if err := input.mutationContext.validate(); err != nil {
			return nil, operationPlanOutput{}, err
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
					return nil, operationPlanOutput{}, errors.New("move marker requires frame_to")
				}
				ops = append(ops, protocol.BatchOperation{Op: "move_marker", Frame: &frame, FrameTo: marker.FrameTo})
			default:
				return nil, operationPlanOutput{}, errors.New("marker op must be set, clear, or move")
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "split_at_markers", Description: "Create intermediate sections where markers fall inside target objects."}, func(ctx context.Context, _ *mcp.CallToolRequest, input markerObjectsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		frames, err := resolveMarkerFrames(ctx, client, input.MarkerFrames)
		if err != nil {
			return nil, operationPlanOutput{}, err
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
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "place_objects_at_markers", Description: "Move ordered objects to marker frames."}, func(ctx context.Context, _ *mcp.CallToolRequest, input markerObjectsInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, timelinePlanOutput{}, err
		}
		frames, err := resolveMarkerFrames(ctx, client, input.MarkerFrames)
		if err != nil {
			return nil, timelinePlanOutput{}, err
		}
		if len(frames) < len(objects) {
			return nil, timelinePlanOutput{}, errors.New("not enough markers for target objects")
		}
		moves := []plannedMove{}
		for i, o := range objects {
			moves = append(moves, moveFor(o, o.Layer, frames[i]))
		}
		return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "set_bpm_grid", Description: "Set the primary BPM grid tempo, beat, and offset."}, func(ctx context.Context, _ *mcp.CallToolRequest, input setBPMInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		tempo, beat, offset := input.Tempo, input.Beat, input.Offset
		return executeOperationPlan(ctx, client, input.mutationContext, []protocol.BatchOperation{{Op: "set_grid_bpm", Tempo: &tempo, Beat: &beat, Offset: &offset}}, input.DryRun, false)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "set_bpm_grid_list", Description: "Replace the complete variable-tempo BPM grid."}, func(ctx context.Context, _ *mcp.CallToolRequest, input setBPMListInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		return executeOperationPlan(ctx, client, input.mutationContext, []protocol.BatchOperation{{Op: "set_grid_bpm_list", BPMPoints: input.Points}}, input.DryRun, false)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "set_scene_settings", Description: "Update current scene name, dimensions, frame rate, or sample rate. AviUtl2 cannot Undo scene settings."}, func(ctx context.Context, _ *mcp.CallToolRequest, input sceneSettingsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		op := protocol.BatchOperation{Op: "set_scene_settings", Name: input.Name, Width: input.Width, Height: input.Height, Rate: input.Rate, Scale: input.Scale, SampleRate: input.SampleRate}
		return executeOperationPlan(ctx, client, input.mutationContext, []protocol.BatchOperation{op}, input.DryRun, false)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "find_layers", Description: "Find current-scene layers by name and occupancy."}, func(ctx context.Context, _ *mcp.CallToolRequest, input findLayersInput) (*mcp.CallToolResult, findLayersOutput, error) {
		current, objects, err := readAllTimeline(ctx, client)
		if err != nil {
			return nil, findLayersOutput{}, err
		}
		end := current.LayerMax
		if input.LayerEnd != nil {
			end = *input.LayerEnd
		}
		if input.LayerStart < 0 || end < input.LayerStart {
			return nil, findLayersOutput{}, errors.New("invalid layer range")
		}
		used := map[int]bool{}
		for _, o := range objects {
			used[o.Layer] = true
		}
		layers := []protocol.Layer{}
		for first := input.LayerStart; first <= end; first += 100 {
			last := min(first+99, end)
			value, err := client.InspectTimeline(ctx, protocol.InspectTimelineParams{LayerStart: first, LayerEnd: last, FrameStart: 0, FrameEnd: max(0, current.FrameMax), MaxObjects: 1})
			if err != nil {
				return nil, findLayersOutput{}, err
			}
			for _, layer := range value.Layers {
				if !input.IncludeEmpty && !used[layer.Index] {
					continue
				}
				if input.NameContains != "" && !strings.Contains(strings.ToLower(layer.Name), strings.ToLower(input.NameContains)) {
					continue
				}
				layers = append(layers, layer)
			}
		}
		return nil, findLayersOutput{Context: current, Layers: layers}, nil
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
