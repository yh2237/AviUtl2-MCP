package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type targetSpec struct {
	ObjectIDs []uint64 `json:"object_ids,omitempty"`
	Selected  bool     `json:"selected,omitempty"`
	Focus     bool     `json:"focus,omitempty"`
}

type operationPlanOutput struct {
	DryRun     bool                      `json:"dry_run"`
	Operations []protocol.BatchOperation `json:"operations"`
	Mutation   *protocol.MutationResult  `json:"mutation,omitempty"`
	Objects    []protocol.Object         `json:"objects,omitempty"`
}

type trimObjectsInput struct {
	mutationContext
	targetSpec
	Start      *int `json:"start,omitempty"`
	End        *int `json:"end,omitempty"`
	StartDelta int  `json:"start_delta,omitempty"`
	EndDelta   int  `json:"end_delta,omitempty"`
	DryRun     bool `json:"dry_run,omitempty"`
}
type deleteObjectsInput struct {
	mutationContext
	targetSpec
	DryRun bool `json:"dry_run,omitempty"`
}
type splitObjectsInput struct {
	mutationContext
	targetSpec
	Frames []int `json:"frames"`
	DryRun bool  `json:"dry_run,omitempty"`
}
type sectionEdit struct {
	Op      string `json:"op" jsonschema:"create, delete, or move"`
	Section *int   `json:"section,omitempty"`
	Frame   *int   `json:"frame,omitempty"`
}
type editSectionsInput struct {
	mutationContext
	targetSpec
	Edits  []sectionEdit `json:"edits"`
	DryRun bool          `json:"dry_run,omitempty"`
}
type inspectSectionValuesInput struct {
	ObjectID             uint64   `json:"object_id"`
	Effect               string   `json:"effect,omitempty"`
	Items                []string `json:"items,omitempty"`
	IncludeRawValues     *bool    `json:"include_raw_values,omitempty"`
	IncludeTrackInfo     *bool    `json:"include_track_info,omitempty"`
	IncludeSampledValues *bool    `json:"include_sampled_values,omitempty"`
}
type sectionValuesOutput struct {
	Object   protocol.Object               `json:"object"`
	Sections []protocol.ObjectValuesResult `json:"sections"`
}
type rangeLayoutInput struct {
	mutationContext
	targetSpec
	Start  int  `json:"start"`
	End    int  `json:"end"`
	Gap    int  `json:"gap,omitempty"`
	DryRun bool `json:"dry_run,omitempty"`
}
type gapLayoutInput struct {
	mutationContext
	targetSpec
	Gap    int  `json:"gap,omitempty"`
	DryRun bool `json:"dry_run,omitempty"`
}
type moveToLayersInput struct {
	mutationContext
	targetSpec
	FirstLayer int    `json:"first_layer"`
	LayerStep  int    `json:"layer_step,omitempty"`
	GroupBy    string `json:"group_by,omitempty" jsonschema:"order, name, or effect"`
	DryRun     bool   `json:"dry_run,omitempty"`
}
type duplicatePatternInput struct {
	mutationContext
	targetSpec
	Rows      int  `json:"rows"`
	Columns   int  `json:"columns"`
	FrameStep int  `json:"frame_step"`
	LayerStep int  `json:"layer_step"`
	DryRun    bool `json:"dry_run,omitempty"`
}
type replaceTextInput struct {
	mutationContext
	targetSpec
	Find      string `json:"find"`
	Replace   string `json:"replace"`
	MatchCase bool   `json:"match_case,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}
type applyPropertiesInput struct {
	mutationContext
	SourceObjectID uint64 `json:"source_object_id"`
	targetSpec
	Effects          []string `json:"effects,omitempty"`
	Items            []string `json:"items,omitempty"`
	IncludeAnimation bool     `json:"include_animation,omitempty"`
	DryRun           bool     `json:"dry_run,omitempty"`
}
type trackUpdate struct {
	Effect   string `json:"effect"`
	Item     string `json:"item"`
	RawValue string `json:"raw_value" jsonschema:"A raw value previously read from AviUtl2"`
}
type setTracksInput struct {
	mutationContext
	targetSpec
	Tracks []trackUpdate `json:"tracks"`
	DryRun bool          `json:"dry_run,omitempty"`
}
type animationTemplateInput struct {
	mutationContext
	SourceObjectID uint64 `json:"source_object_id"`
	targetSpec
	Template string `json:"template" jsonschema:"fade, slide, or zoom"`
	DryRun   bool   `json:"dry_run,omitempty"`
}
type snapBPMInput struct {
	mutationContext
	targetSpec
	Division int  `json:"division,omitempty" jsonschema:"subdivisions per beat, default 1"`
	DryRun   bool `json:"dry_run,omitempty"`
}
type layerToolsInput struct {
	mutationContext
	LayerStart int    `json:"layer_start"`
	LayerEnd   int    `json:"layer_end"`
	Prefix     string `json:"prefix,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
}
type layerInfoOutput struct {
	Context    protocol.Context          `json:"context"`
	Empty      []int                     `json:"empty_layers"`
	Operations []protocol.BatchOperation `json:"operations,omitempty"`
	Mutation   *protocol.MutationResult  `json:"mutation,omitempty"`
}

func addAdvancedEditTools(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "delete_objects", Description: "Preflight and delete selected, focused, or explicit objects with dry-run support."}, func(ctx context.Context, _ *mcp.CallToolRequest, input deleteObjectsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		ops := make([]protocol.BatchOperation, 0, len(objects))
		for _, o := range objects {
			ops = append(ops, protocol.BatchOperation{Op: "delete_object", ObjectID: o.ID})
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "trim_objects", Description: "Trim object starts or ends by absolute frames or deltas."}, func(ctx context.Context, _ *mcp.CallToolRequest, input trimObjectsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if input.Start == nil && input.End == nil && input.StartDelta == 0 && input.EndDelta == 0 {
			return nil, operationPlanOutput{}, errors.New("a start/end value or delta is required")
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			start, end := o.Start+input.StartDelta, o.End+input.EndDelta
			if input.Start != nil {
				start = *input.Start
			}
			if input.End != nil {
				end = *input.End
			}
			if start < 0 || end < start {
				return nil, operationPlanOutput{}, fmt.Errorf("invalid trim range for object %d", o.ID)
			}
			if start != o.Start {
				s := 0
				f := start
				ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: o.ID, Section: &s, Frame: &f})
			}
			if end != o.End {
				s := len(o.Sections)
				f := end
				ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: o.ID, Section: &s, Frame: &f})
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "split_objects", Description: "Create intermediate sections at one or more absolute frames."}, func(ctx context.Context, _ *mcp.CallToolRequest, input splitObjectsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if len(input.Frames) == 0 {
			return nil, operationPlanOutput{}, errors.New("frames is required")
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			for _, v := range input.Frames {
				if v > o.Start && v <= o.End && !containsInt(o.Sections, v) {
					f := v
					ops = append(ops, protocol.BatchOperation{Op: "create_section", ObjectID: o.ID, Frame: &f})
				}
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "edit_sections", Description: "Create, delete, or move intermediate sections on target objects."}, func(ctx context.Context, _ *mcp.CallToolRequest, input editSectionsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if len(input.Edits) == 0 {
			return nil, operationPlanOutput{}, errors.New("edits is required")
		}
		ops := []protocol.BatchOperation{}
		for _, object := range objects {
			for _, edit := range input.Edits {
				switch edit.Op {
				case "create":
					if edit.Frame == nil {
						return nil, operationPlanOutput{}, errors.New("create requires frame")
					}
					ops = append(ops, protocol.BatchOperation{Op: "create_section", ObjectID: object.ID, Frame: edit.Frame})
				case "delete":
					if edit.Section == nil {
						return nil, operationPlanOutput{}, errors.New("delete requires section")
					}
					ops = append(ops, protocol.BatchOperation{Op: "delete_section", ObjectID: object.ID, Section: edit.Section})
				case "move":
					if edit.Section == nil || edit.Frame == nil {
						return nil, operationPlanOutput{}, errors.New("move requires section and frame")
					}
					ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: object.ID, Section: edit.Section, Frame: edit.Frame})
				default:
					return nil, operationPlanOutput{}, errors.New("section op must be create, delete, or move")
				}
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "inspect_section_values", Description: "Sample filtered settings and tracks at every section start on one object."}, func(ctx context.Context, _ *mcp.CallToolRequest, input inspectSectionValuesInput) (*mcp.CallToolResult, sectionValuesOutput, error) {
		objectResult, err := client.InspectObject(ctx, protocol.InspectObjectParams{ObjectID: input.ObjectID, IncludeEffects: true})
		if err != nil {
			return nil, sectionValuesOutput{}, err
		}
		values := []protocol.ObjectValuesResult{}
		for _, frame := range objectResult.Object.Sections {
			sample := float64(frame)
			result, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{ObjectID: input.ObjectID, Frame: &sample, Effect: input.Effect, Items: input.Items, IncludeRawValues: input.IncludeRawValues, IncludeTrackInfo: input.IncludeTrackInfo, IncludeSampledValues: input.IncludeSampledValues})
			if err != nil {
				return nil, sectionValuesOutput{}, err
			}
			values = append(values, result)
		}
		return nil, sectionValuesOutput{Object: objectResult.Object, Sections: values}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "sequence_objects", Description: "Place objects consecutively in target order using a fixed gap."}, func(ctx context.Context, _ *mcp.CallToolRequest, input gapLayoutInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 2)
		if err != nil {
			return nil, timelinePlanOutput{}, err
		}
		cursor := objects[0].Start
		moves := make([]plannedMove, 0, len(objects))
		for _, o := range objects {
			moves = append(moves, moveFor(o, o.Layer, cursor))
			cursor += o.End - o.Start + 1 + input.Gap
		}
		return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "pack_objects", Description: "Remove empty gaps between ordered objects without changing their lengths."}, func(ctx context.Context, _ *mcp.CallToolRequest, input gapLayoutInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
		return runPackedLayout(ctx, client, input, false)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "normalize_gaps", Description: "Set every gap between ordered objects to the requested frame count."}, func(ctx context.Context, _ *mcp.CallToolRequest, input gapLayoutInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
		return runPackedLayout(ctx, client, input, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fit_objects", Description: "Fit object positions and lengths proportionally into an absolute frame range."}, func(ctx context.Context, _ *mcp.CallToolRequest, input rangeLayoutInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if input.Start < 0 || input.End < input.Start {
			return nil, operationPlanOutput{}, errors.New("invalid fit range")
		}
		minStart, maxEnd := objects[0].Start, objects[0].End
		for _, o := range objects {
			minStart = min(minStart, o.Start)
			maxEnd = max(maxEnd, o.End)
		}
		sourceSpan := max(1, maxEnd-minStart+1)
		targetSpan := input.End - input.Start + 1
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			start := input.Start + (o.Start-minStart)*targetSpan/sourceSpan
			end := input.Start + (o.End-minStart+1)*targetSpan/sourceSpan - 1
			if end < start {
				end = start
			}
			layer := o.Layer
			ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: o.ID, Layer: &layer, Frame: &start})
			section := len(o.Sections)
			ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: o.ID, Section: &section, Frame: &end})
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "move_to_layers", Description: "Distribute objects onto layers by order, object name, or first effect."}, func(ctx context.Context, _ *mcp.CallToolRequest, input moveToLayersInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, timelinePlanOutput{}, err
		}
		if input.FirstLayer < 0 {
			return nil, timelinePlanOutput{}, errors.New("first_layer must be non-negative")
		}
		if input.LayerStep == 0 {
			input.LayerStep = 1
		}
		groups := map[string]int{}
		moves := []plannedMove{}
		for i, o := range objects {
			index := i
			if input.GroupBy != "" && input.GroupBy != "order" {
				key := o.Name
				if input.GroupBy == "effect" && len(o.Effects) > 0 {
					key = o.Effects[0].Name
				}
				if value, ok := groups[key]; ok {
					index = value
				} else {
					index = len(groups)
					groups[key] = index
				}
			}
			moves = append(moves, moveFor(o, input.FirstLayer+index*input.LayerStep, o.Start))
		}
		return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "duplicate_pattern", Description: "Duplicate objects into a frame/layer grid while preserving effects and animation."}, func(ctx context.Context, _ *mcp.CallToolRequest, input duplicatePatternInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if input.Rows < 1 || input.Columns < 1 || input.Rows*input.Columns <= 1 {
			return nil, operationPlanOutput{}, errors.New("rows and columns must be positive and create at least two cells")
		}
		ops := []protocol.BatchOperation{}
		for row := 0; row < input.Rows; row++ {
			for column := 0; column < input.Columns; column++ {
				if row == 0 && column == 0 {
					continue
				}
				for _, o := range objects {
					layer, frame := o.Layer+row*input.LayerStep, o.Start+column*input.FrameStep
					if layer < 0 || frame < 0 {
						return nil, operationPlanOutput{}, errors.New("pattern target is negative")
					}
					ops = append(ops, protocol.BatchOperation{Op: "duplicate_object", ObjectID: o.ID, Layer: &layer, Frame: &frame, Length: o.End - o.Start + 1})
				}
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "replace_text", Description: "Search and replace text values across selected objects."}, func(ctx context.Context, _ *mcp.CallToolRequest, input replaceTextInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if input.Find == "" {
			return nil, operationPlanOutput{}, errors.New("find is required")
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			value, ok, err := readRawItem(ctx, client, o.ID, "テキスト", "テキスト")
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if !ok {
				continue
			}
			next := replaceString(value, input.Find, input.Replace, input.MatchCase)
			if next != value {
				ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: o.ID, Properties: []protocol.PropertyUpdate{{Effect: "テキスト", Item: "テキスト", Value: next}}})
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "apply_properties", Description: "Copy filtered raw settings and animation data from one object to target objects."}, func(ctx context.Context, _ *mcp.CallToolRequest, input applyPropertiesInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		targets, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		f := false
		r := true
		values, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{ObjectID: input.SourceObjectID, Items: input.Items, IncludeRawValues: &r, IncludeTrackInfo: &f, IncludeSampledValues: &f})
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		properties := []protocol.PropertyUpdate{}
		for _, effect := range values.Effects {
			if len(input.Effects) > 0 && !containsFold(input.Effects, effect.Name) {
				continue
			}
			for _, item := range effect.Items {
				value := item.RawValue
				if !input.IncludeAnimation {
					value = firstTrackValue(value)
				}
				properties = append(properties, protocol.PropertyUpdate{Effect: effect.Name, Item: item.Name, Value: value})
			}
		}
		ops := []protocol.BatchOperation{}
		for _, o := range targets {
			if o.ID != input.SourceObjectID {
				ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: o.ID, Properties: properties})
			}
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "set_track_values", Description: "Apply raw track values previously read from AviUtl2 to multiple objects."}, func(ctx context.Context, _ *mcp.CallToolRequest, input setTracksInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if len(input.Tracks) == 0 {
			return nil, operationPlanOutput{}, errors.New("tracks is required")
		}
		properties := []protocol.PropertyUpdate{}
		for _, track := range input.Tracks {
			if track.Effect == "" || track.Item == "" || track.RawValue == "" {
				return nil, operationPlanOutput{}, errors.New("track effect, item, and raw_value are required")
			}
			properties = append(properties, protocol.PropertyUpdate{Effect: track.Effect, Item: track.Item, Value: track.RawValue})
		}
		ops := []protocol.BatchOperation{}
		for _, o := range objects {
			ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: o.ID, Properties: properties})
		}
		return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "apply_animation_template", Description: "Copy a verified fade, slide, or zoom track from a template object to targets."}, func(ctx context.Context, _ *mcp.CallToolRequest, input animationTemplateInput) (*mcp.CallToolResult, operationPlanOutput, error) {
		effect, item, err := templateTrack(input.Template)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		raw, found, err := readRawItem(ctx, client, input.SourceObjectID, effect, item)
		if err != nil {
			return nil, operationPlanOutput{}, err
		}
		if !found || raw == "" {
			return nil, operationPlanOutput{}, errors.New("template source does not contain the requested track")
		}
		return runRawTrackPlan(ctx, client, input.mutationContext, input.targetSpec, trackUpdate{Effect: effect, Item: item, RawValue: raw}, input.DryRun)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "snap_objects_to_bpm", Description: "Snap object starts to the nearest BPM-grid subdivision."}, func(ctx context.Context, _ *mcp.CallToolRequest, input snapBPMInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return nil, timelinePlanOutput{}, err
		}
		grid, err := client.GetBPMGrid(ctx)
		if err != nil {
			return nil, timelinePlanOutput{}, err
		}
		if len(grid.Points) == 0 {
			return nil, timelinePlanOutput{}, errors.New("BPM grid is empty")
		}
		if input.Division == 0 {
			input.Division = 1
		}
		if input.Division < 1 || input.Division > 16 {
			return nil, timelinePlanOutput{}, errors.New("division must be 1..16")
		}
		point := grid.Points[0]
		fps := float64(grid.Context.Rate) / float64(grid.Context.Scale)
		step := fps * 60 / float64(point.Tempo) / float64(input.Division)
		origin := (point.Start + float64(point.Offset)) * fps
		moves := []plannedMove{}
		for _, o := range objects {
			frame := int(math.Round(origin + math.Round((float64(o.Start)-origin)/step)*step))
			moves = append(moves, moveFor(o, o.Layer, max(0, frame)))
		}
		return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "find_empty_layers", Description: "Find empty layers in an inclusive range."}, func(ctx context.Context, _ *mcp.CallToolRequest, input layerToolsInput) (*mcp.CallToolResult, layerInfoOutput, error) {
		current, objects, err := readAllTimeline(ctx, client)
		if err != nil {
			return nil, layerInfoOutput{}, err
		}
		if input.LayerStart < 0 || input.LayerEnd < input.LayerStart {
			return nil, layerInfoOutput{}, errors.New("invalid layer range")
		}
		used := map[int]bool{}
		for _, o := range objects {
			used[o.Layer] = true
		}
		empty := []int{}
		for layer := input.LayerStart; layer <= input.LayerEnd; layer++ {
			if !used[layer] {
				empty = append(empty, layer)
			}
		}
		return nil, layerInfoOutput{Context: current, Empty: empty}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "name_layers", Description: "Name a contiguous layer range using a prefix and one-based sequence."}, func(ctx context.Context, _ *mcp.CallToolRequest, input layerToolsInput) (*mcp.CallToolResult, layerInfoOutput, error) {
		if err := input.mutationContext.validate(); err != nil {
			return nil, layerInfoOutput{}, err
		}
		if input.LayerStart < 0 || input.LayerEnd < input.LayerStart || input.Prefix == "" {
			return nil, layerInfoOutput{}, errors.New("valid range and prefix are required")
		}
		ops := []protocol.BatchOperation{}
		for layer := input.LayerStart; layer <= input.LayerEnd; layer++ {
			l := layer
			n := fmt.Sprintf("%s%d", input.Prefix, layer-input.LayerStart+1)
			ops = append(ops, protocol.BatchOperation{Op: "set_layer_state", Layer: &l, Name: &n})
		}
		if input.DryRun {
			return nil, layerInfoOutput{Operations: ops}, nil
		}
		result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: ops}, input.expected())
		return nil, layerInfoOutput{Context: result.Context, Operations: ops, Mutation: &result}, err
	})
}

func resolveTargetObjects(ctx context.Context, client *bridge.Client, expected mutationContext, target targetSpec, minimum int) ([]protocol.Object, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	ids := append([]uint64(nil), target.ObjectIDs...)
	if target.Selected || target.Focus {
		selection, err := client.GetSelection(ctx)
		if err != nil {
			return nil, err
		}
		if target.Selected {
			for _, o := range selection.Objects {
				ids = append(ids, o.ID)
			}
		}
		if target.Focus && selection.FocusObjectID != nil {
			ids = append(ids, *selection.FocusObjectID)
		}
	}
	ids = uniqueIDs(ids)
	if len(ids) < minimum || len(ids) > 100 {
		return nil, fmt.Errorf("target must resolve to between %d and 100 objects", minimum)
	}
	return inspectObjects(ctx, client, ids, expected)
}
func executeOperationPlan(ctx context.Context, client *bridge.Client, expected mutationContext, ops []protocol.BatchOperation, dryRun, returnObjects bool) (*mcp.CallToolResult, operationPlanOutput, error) {
	out := operationPlanOutput{DryRun: dryRun, Operations: ops}
	if len(ops) > 100 {
		return nil, out, errors.New("planned edit exceeds 100 operations")
	}
	if err := validateBatchOperations(ops); err != nil && len(ops) > 0 {
		return nil, out, err
	}
	if len(ops) > 0 {
		if err := preflightBatch(ctx, client, expected, ops); err != nil {
			return nil, out, err
		}
	}
	if dryRun || len(ops) == 0 {
		return nil, out, nil
	}
	result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: ops}, expected.expected())
	out.Mutation = &result
	if err == nil && returnObjects {
		ids := []uint64{}
		for _, r := range result.Results {
			if r.ObjectID != nil {
				ids = append(ids, *r.ObjectID)
			}
		}
		if len(ids) > 0 {
			values, e := client.InspectObjects(ctx, protocol.InspectObjectsParams{ObjectIDs: uniqueIDs(ids), IncludeEffects: true})
			if e == nil {
				out.Objects = values.Objects
			}
		}
	}
	return nil, out, err
}
func runPackedLayout(ctx context.Context, client *bridge.Client, input gapLayoutInput, useGap bool) (*mcp.CallToolResult, timelinePlanOutput, error) {
	objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 2)
	if err != nil {
		return nil, timelinePlanOutput{}, err
	}
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].Start < objects[j].Start })
	gap := 0
	if useGap {
		gap = input.Gap
	}
	cursor := objects[0].Start
	moves := []plannedMove{}
	for _, o := range objects {
		moves = append(moves, moveFor(o, o.Layer, cursor))
		cursor += o.End - o.Start + 1 + gap
	}
	return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
}
func runRawTrackPlan(ctx context.Context, client *bridge.Client, expected mutationContext, target targetSpec, track trackUpdate, dry bool) (*mcp.CallToolResult, operationPlanOutput, error) {
	objects, err := resolveTargetObjects(ctx, client, expected, target, 1)
	if err != nil {
		return nil, operationPlanOutput{}, err
	}
	properties := []protocol.PropertyUpdate{{Effect: track.Effect, Item: track.Item, Value: track.RawValue}}
	ops := []protocol.BatchOperation{}
	for _, o := range objects {
		ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: o.ID, Properties: properties})
	}
	return executeOperationPlan(ctx, client, expected, ops, dry, true)
}
func templateTrack(name string) (string, string, error) {
	switch name {
	case "fade":
		return "標準描画", "透明度", nil
	case "slide":
		return "標準描画", "X", nil
	case "zoom":
		return "標準描画", "拡大率", nil
	default:
		return "", "", errors.New("unknown animation template")
	}
}
func uniqueIDs(ids []uint64) []uint64 {
	seen := map[uint64]bool{}
	out := []uint64{}
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
func containsInt(values []int, target int) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}
func replaceString(value, find, replacement string, matchCase bool) string {
	if matchCase {
		return strings.ReplaceAll(value, find, replacement)
	}
	lowerValue, lowerFind := strings.ToLower(value), strings.ToLower(find)
	var b strings.Builder
	for {
		index := strings.Index(lowerValue, lowerFind)
		if index < 0 {
			b.WriteString(value)
			break
		}
		b.WriteString(value[:index])
		b.WriteString(replacement)
		value = value[index+len(find):]
		lowerValue = lowerValue[index+len(find):]
	}
	return b.String()
}
func firstTrackValue(value string) string {
	if index := strings.IndexByte(value, ','); index >= 0 {
		return value[:index]
	}
	return value
}
func readRawItem(ctx context.Context, client *bridge.Client, id uint64, effect, item string) (string, bool, error) {
	f, r := false, true
	result, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{ObjectID: id, Effect: effect, Items: []string{item}, IncludeRawValues: &r, IncludeTrackInfo: &f, IncludeSampledValues: &f})
	if err != nil {
		return "", false, err
	}
	for _, e := range result.Effects {
		for _, i := range e.Items {
			if i.Name == item {
				return i.RawValue, true, nil
			}
		}
	}
	return "", false, nil
}
