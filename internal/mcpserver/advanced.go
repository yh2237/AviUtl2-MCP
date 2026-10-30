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
	Operation  string                   `json:"operation,omitempty"`
	DryRun     bool                     `json:"dry_run"`
	Operations []any                    `json:"operations"`
	Mutation   *protocol.MutationResult `json:"mutation,omitempty"`
	Objects    []protocol.Object        `json:"objects,omitempty"`
}

// toAnyOperations erases the concrete operation type so the tool payload stays readable
// without tying every grouped tool to []protocol.BatchOperation.
func toAnyOperations(operations []protocol.BatchOperation) []any {
	values := make([]any, 0, len(operations))
	for _, operation := range operations {
		values = append(values, operation)
	}
	return values
}

type deleteObjectsInput struct {
	mutationContext
	targetSpec
	DryRun bool `json:"dry_run,omitempty"`
}

type arrangeInput struct {
	mutationContext
	targetSpec
	Operation   string `json:"operation" jsonschema:"align, distribute, stagger, sequence, pack, normalize_gaps, or snap_to_bpm"`
	Edge        string `json:"edge,omitempty" jsonschema:"align: start, center, or end"`
	TargetFrame *int   `json:"target_frame,omitempty" jsonschema:"align: defaults to the outer edge of the targets"`
	StartFrame  *int   `json:"start_frame,omitempty" jsonschema:"distribute/stagger: defaults to the first target start"`
	EndFrame    *int   `json:"end_frame,omitempty" jsonschema:"distribute: defaults to the last target start"`
	FrameStep   int    `json:"frame_step,omitempty" jsonschema:"stagger: frames between targets"`
	LayerStep   int    `json:"layer_step,omitempty" jsonschema:"stagger: layers between targets"`
	Gap         int    `json:"gap,omitempty" jsonschema:"sequence/normalize_gaps: empty frames between targets"`
	Division    int    `json:"division,omitempty" jsonschema:"snap_to_bpm: subdivisions per beat, default 1"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

type retimeInput struct {
	mutationContext
	targetSpec
	Operation  string `json:"operation" jsonschema:"shift, insert_time, trim, fit, or fit_media"`
	FrameDelta int    `json:"frame_delta,omitempty" jsonschema:"shift: frames to move"`
	LayerDelta int    `json:"layer_delta,omitempty" jsonschema:"shift: layers to move"`
	Frame      int    `json:"frame,omitempty" jsonschema:"insert_time: insertion frame"`
	Length     int    `json:"length,omitempty" jsonschema:"insert_time: empty frames to insert"`
	Start      *int   `json:"start,omitempty" jsonschema:"trim: absolute start frame"`
	End        *int   `json:"end,omitempty" jsonschema:"trim: absolute end frame"`
	StartDelta int    `json:"start_delta,omitempty" jsonschema:"trim: relative start shift"`
	EndDelta   int    `json:"end_delta,omitempty" jsonschema:"trim: relative end shift"`
	FitStart   int    `json:"fit_start,omitempty" jsonschema:"fit: target range start"`
	FitEnd     int    `json:"fit_end,omitempty" jsonschema:"fit: target range end"`
	LayerStart *int   `json:"layer_start,omitempty" jsonschema:"insert_time: first affected layer"`
	LayerEnd   *int   `json:"layer_end,omitempty" jsonschema:"insert_time: last affected layer"`
	DryRun     bool   `json:"dry_run,omitempty"`
}

type moveToLayersInput struct {
	mutationContext
	targetSpec
	FirstLayer int    `json:"first_layer"`
	LayerStep  int    `json:"layer_step,omitempty"`
	GroupBy    string `json:"group_by,omitempty" jsonschema:"order, name, or effect"`
	DryRun     bool   `json:"dry_run,omitempty"`
}

type sectionEdit struct {
	Op      string `json:"op" jsonschema:"create, delete, or move"`
	Frame   *int   `json:"frame,omitempty"`
	Section *int   `json:"section,omitempty"`
}

type editSectionsInput struct {
	mutationContext
	targetSpec
	Operation string        `json:"operation" jsonschema:"split, create, delete, or move"`
	Frames    []int         `json:"frames,omitempty" jsonschema:"split: absolute frames; must fall inside each target"`
	Edits     []sectionEdit `json:"edits,omitempty" jsonschema:"create/delete/move: one or more section edits"`
	DryRun    bool          `json:"dry_run,omitempty"`
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
	Template         string   `json:"template,omitempty" jsonschema:"fade, slide, or zoom; copies one verified track instead of every item"`
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

func runArrange(ctx context.Context, client *bridge.Client, input arrangeInput) (operationPlanOutput, error) {
	switch input.Operation {
	case "align":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 2)
		if err != nil {
			return operationPlanOutput{}, err
		}
		target, err := alignmentTarget(objects, input.Edge, input.TargetFrame)
		if err != nil {
			return operationPlanOutput{}, err
		}
		moves := make([]plannedMove, 0, len(objects))
		for _, object := range objects {
			length := object.End - object.Start
			start := target
			switch input.Edge {
			case "center":
				start = target - length/2
			case "end":
				start = target - length
			}
			moves = append(moves, moveFor(object, object.Layer, start))
		}
		return executeMovePlanOutput(ctx, client, input.mutationContext, moves, input.DryRun)
	case "distribute":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 3)
		if err != nil {
			return operationPlanOutput{}, err
		}
		sort.SliceStable(objects, func(i, j int) bool {
			if objects[i].Start == objects[j].Start {
				return objects[i].Layer < objects[j].Layer
			}
			return objects[i].Start < objects[j].Start
		})
		start, end := objects[0].Start, objects[len(objects)-1].Start
		if input.StartFrame != nil {
			start = *input.StartFrame
		}
		if input.EndFrame != nil {
			end = *input.EndFrame
		}
		if start < 0 || end < start {
			return operationPlanOutput{}, errors.New("distribution frame range must be ordered and non-negative")
		}
		moves := make([]plannedMove, 0, len(objects))
		for index, object := range objects {
			frame := start + (end-start)*index/(len(objects)-1)
			moves = append(moves, moveFor(object, object.Layer, frame))
		}
		return executeMovePlanOutput(ctx, client, input.mutationContext, moves, input.DryRun)
	case "stagger":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 2)
		if err != nil {
			return operationPlanOutput{}, err
		}
		if input.FrameStep == 0 && input.LayerStep == 0 {
			return operationPlanOutput{}, errors.New("frame_step or layer_step is required")
		}
		start := objects[0].Start
		if input.StartFrame != nil {
			start = *input.StartFrame
		}
		moves := make([]plannedMove, 0, len(objects))
		for index, object := range objects {
			moves = append(moves, moveFor(object, objects[0].Layer+input.LayerStep*index, start+input.FrameStep*index))
		}
		return executeMovePlanOutput(ctx, client, input.mutationContext, moves, input.DryRun)
	case "sequence":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 2)
		if err != nil {
			return operationPlanOutput{}, err
		}
		cursor := objects[0].Start
		moves := make([]plannedMove, 0, len(objects))
		for _, object := range objects {
			moves = append(moves, moveFor(object, object.Layer, cursor))
			cursor += object.End - object.Start + 1 + input.Gap
		}
		return executeMovePlanOutput(ctx, client, input.mutationContext, moves, input.DryRun)
	case "pack", "normalize_gaps":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 2)
		if err != nil {
			return operationPlanOutput{}, err
		}
		sort.SliceStable(objects, func(i, j int) bool { return objects[i].Start < objects[j].Start })
		gap := 0
		if input.Operation == "normalize_gaps" {
			gap = input.Gap
		}
		cursor := objects[0].Start
		moves := []plannedMove{}
		for _, object := range objects {
			moves = append(moves, moveFor(object, object.Layer, cursor))
			cursor += object.End - object.Start + 1 + gap
		}
		return executeMovePlanOutput(ctx, client, input.mutationContext, moves, input.DryRun)
	case "snap_to_bpm":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return operationPlanOutput{}, err
		}
		grid, err := client.GetBPMGrid(ctx)
		if err != nil {
			return operationPlanOutput{}, err
		}
		if len(grid.Points) == 0 {
			return operationPlanOutput{}, errors.New("BPM grid is empty")
		}
		division := input.Division
		if division == 0 {
			division = 1
		}
		if division < 1 || division > 16 {
			return operationPlanOutput{}, errors.New("division must be 1..16")
		}
		point := grid.Points[0]
		fps := float64(grid.Context.Rate) / float64(grid.Context.Scale)
		step := fps * 60 / float64(point.Tempo) / float64(division)
		origin := (point.Start + float64(point.Offset)) * fps
		moves := []plannedMove{}
		for _, object := range objects {
			frame := int(math.Round(origin + math.Round((float64(object.Start)-origin)/step)*step))
			moves = append(moves, moveFor(object, object.Layer, max(0, frame)))
		}
		return executeMovePlanOutput(ctx, client, input.mutationContext, moves, input.DryRun)
	default:
		return operationPlanOutput{}, fmt.Errorf("unknown operation %q; use align, distribute, stagger, sequence, pack, normalize_gaps, or snap_to_bpm", input.Operation)
	}
}

func runRetime(ctx context.Context, client *bridge.Client, input retimeInput) (operationPlanOutput, error) {
	switch input.Operation {
	case "shift":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return operationPlanOutput{}, err
		}
		if input.FrameDelta == 0 && input.LayerDelta == 0 {
			return operationPlanOutput{}, errors.New("frame_delta or layer_delta is required")
		}
		moves := make([]plannedMove, 0, len(objects))
		for _, object := range objects {
			moves = append(moves, moveFor(object, object.Layer+input.LayerDelta, object.Start+input.FrameDelta))
		}
		_, plan, err := executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		return convertMovePlan(plan), err
	case "insert_time":
		if err := input.mutationContext.validate(); err != nil {
			return operationPlanOutput{}, err
		}
		if input.Frame < 0 || input.Length < 1 {
			return operationPlanOutput{}, errors.New("frame must be non-negative and length must be positive")
		}
		current, objects, err := inspectTimelineSnapshot(ctx, client, input.mutationContext, 0, -1, 0)
		if err != nil {
			return operationPlanOutput{}, err
		}
		layerStart, layerEnd := 0, current.LayerMax
		if input.LayerStart != nil {
			layerStart = *input.LayerStart
		}
		if input.LayerEnd != nil {
			layerEnd = *input.LayerEnd
		}
		if layerStart < 0 || layerEnd < layerStart {
			return operationPlanOutput{}, errors.New("layer range must be ordered and non-negative")
		}
		moves := make([]plannedMove, 0)
		for _, object := range objects {
			if object.Layer >= layerStart && object.Layer <= layerEnd && object.Start >= input.Frame {
				moves = append(moves, moveFor(object, object.Layer, object.Start+input.Length))
			}
		}
		if len(moves) == 0 {
			return operationPlanOutput{DryRun: input.DryRun, Operations: []any{}}, nil
		}
		_, plan, err := executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		return convertMovePlan(plan), err
	case "trim":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return operationPlanOutput{}, err
		}
		if input.Start == nil && input.End == nil && input.StartDelta == 0 && input.EndDelta == 0 {
			return operationPlanOutput{}, errors.New("a start/end value or delta is required")
		}
		ops := []protocol.BatchOperation{}
		for _, object := range objects {
			start, end := object.Start+input.StartDelta, object.End+input.EndDelta
			if input.Start != nil {
				start = *input.Start
			}
			if input.End != nil {
				end = *input.End
			}
			if start < 0 || end < start {
				return operationPlanOutput{}, fmt.Errorf("invalid trim range for object %d", object.ID)
			}
			if start != object.Start {
				section, frame := 0, start
				ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: object.ID, Section: &section, Frame: &frame})
			}
			if end != object.End {
				section, frame := len(object.Sections), end
				ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: object.ID, Section: &section, Frame: &frame})
			}
		}
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		return plan, err
	case "fit":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return operationPlanOutput{}, err
		}
		if input.FitStart < 0 || input.FitEnd < input.FitStart {
			return operationPlanOutput{}, errors.New("invalid fit range")
		}
		minStart, maxEnd := objects[0].Start, objects[0].End
		for _, object := range objects {
			minStart = min(minStart, object.Start)
			maxEnd = max(maxEnd, object.End)
		}
		sourceSpan := max(1, maxEnd-minStart+1)
		targetSpan := input.FitEnd - input.FitStart + 1
		ops := []protocol.BatchOperation{}
		for _, object := range objects {
			start := input.FitStart + (object.Start-minStart)*targetSpan/sourceSpan
			end := input.FitStart + (object.End-minStart+1)*targetSpan/sourceSpan - 1
			if end < start {
				end = start
			}
			layer := object.Layer
			ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: object.ID, Layer: &layer, Frame: &start})
			section := len(object.Sections)
			ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: object.ID, Section: &section, Frame: &end})
		}
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		return plan, err
	case "fit_media":
		objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
		if err != nil {
			return operationPlanOutput{}, err
		}
		current, err := client.GetContext(ctx)
		if err != nil {
			return operationPlanOutput{}, err
		}
		ops := []protocol.BatchOperation{}
		for _, object := range objects {
			files, err := objectMedia(ctx, client, object.ID)
			if err != nil {
				return operationPlanOutput{}, err
			}
			if len(files) != 1 {
				continue
			}
			info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: files[0], Strict: true})
			if err != nil {
				return operationPlanOutput{}, err
			}
			if info.TotalTime <= 0 {
				continue
			}
			end := object.Start + max(1, int(math.Round(info.TotalTime*float64(current.Rate)/float64(current.Scale)))) - 1
			section := len(object.Sections)
			ops = append(ops, protocol.BatchOperation{Op: "move_section", ObjectID: object.ID, Section: &section, Frame: &end})
		}
		_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		return plan, err
	default:
		return operationPlanOutput{}, fmt.Errorf("unknown operation %q; use shift, insert_time, trim, fit, or fit_media", input.Operation)
	}
}

func executeMovePlanOutput(ctx context.Context, client *bridge.Client, expected mutationContext, moves []plannedMove, dryRun bool) (operationPlanOutput, error) {
	_, plan, err := executeMovePlan(ctx, client, expected, moves, dryRun)
	return convertMovePlan(plan), err
}

// convertMovePlan flattens the collision-aware move plan into the shared plan shape.
func convertMovePlan(plan timelinePlanOutput) operationPlanOutput {
	return operationPlanOutput{
		DryRun:     plan.DryRun,
		Operations: toAnyOperations(plan.Operations),
		Mutation:   plan.Mutation,
	}
}

func addAdvancedEditTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "delete_objects", Description: "Preflight and delete selected, focused, or explicit objects with dry-run support."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input deleteObjectsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			ops := make([]protocol.BatchOperation, 0, len(objects))
			for _, object := range objects {
				ops = append(ops, protocol.BatchOperation{Op: "delete_object", ObjectID: object.ID})
			}
			_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
			plan.Operation = "delete_objects"
			return nil, plan, err
		})
	addTool(server, &mcp.Tool{Name: "arrange_objects", Description: "Align, distribute, stagger, sequence, pack, normalize gaps, or snap targets to the BPM grid. Collisions are detected before editing."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input arrangeInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			plan, err := runArrange(ctx, client, input)
			plan.Operation = input.Operation
			return nil, plan, err
		})
	addTool(server, &mcp.Tool{Name: "retime_objects", Description: "Shift, insert time, trim, fit to a range, or fit media duration. Collisions are detected before editing."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input retimeInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			plan, err := runRetime(ctx, client, input)
			plan.Operation = input.Operation
			return nil, plan, err
		})
	addTool(server, &mcp.Tool{Name: "move_to_layers", Description: "Distribute objects onto layers by order, object name, or first effect."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input moveToLayersInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if input.FirstLayer < 0 {
				return nil, operationPlanOutput{}, errors.New("first_layer must be non-negative")
			}
			if input.LayerStep == 0 {
				input.LayerStep = 1
			}
			groups := map[string]int{}
			moves := []plannedMove{}
			for index, object := range objects {
				position := index
				if input.GroupBy != "" && input.GroupBy != "order" {
					key := object.Name
					if input.GroupBy == "effect" && len(object.Effects) > 0 {
						key = object.Effects[0].Name
					}
					if value, ok := groups[key]; ok {
						position = value
					} else {
						position = len(groups)
						groups[key] = position
					}
				}
				moves = append(moves, moveFor(object, input.FirstLayer+position*input.LayerStep, object.Start))
			}
			_, plan, err := executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
			out := convertMovePlan(plan)
			out.Operation = "move_to_layers"
			return nil, out, err
		})
	addTool(server, &mcp.Tool{Name: "edit_sections", Description: "Split objects at frames, or create, delete, and move intermediate sections in one Undo unit."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input editSectionsInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			ops := []protocol.BatchOperation{}
			switch input.Operation {
			case "split":
				if len(input.Frames) == 0 {
					return nil, operationPlanOutput{}, errors.New("frames is required for operation=split")
				}
				for _, object := range objects {
					for _, value := range input.Frames {
						if value > object.Start && value <= object.End && !containsInt(object.Sections, value) {
							frame := value
							ops = append(ops, protocol.BatchOperation{Op: "create_section", ObjectID: object.ID, Frame: &frame})
						}
					}
				}
			case "create", "delete", "move":
				if len(input.Edits) == 0 {
					return nil, operationPlanOutput{}, errors.New("edits is required")
				}
				for _, object := range objects {
					for _, edit := range input.Edits {
						editOp := edit.Op
						if editOp == "" {
							editOp = input.Operation
						}
						switch editOp {
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
			default:
				return nil, operationPlanOutput{}, fmt.Errorf("unknown operation %q; use split, create, delete, or move", input.Operation)
			}
			_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
			plan.Operation = input.Operation
			return nil, plan, err
		})
	addTool(server, &mcp.Tool{Name: "replace_text", Description: "Search and replace text values across target objects."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input replaceTextInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			objects, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if input.Find == "" {
				return nil, operationPlanOutput{}, errors.New("find is required")
			}
			ops := []protocol.BatchOperation{}
			for _, object := range objects {
				value, ok, err := readRawItem(ctx, client, object.ID, "テキスト", "テキスト")
				if err != nil {
					return nil, operationPlanOutput{}, err
				}
				if !ok {
					continue
				}
				next := replaceString(value, input.Find, input.Replace, input.MatchCase)
				if next != value {
					ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: object.ID, Properties: []protocol.PropertyUpdate{{Effect: "テキスト", Item: "テキスト", Value: next}}})
				}
			}
			_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
			plan.Operation = "replace_text"
			return nil, plan, err
		})
	addTool(server, &mcp.Tool{Name: "apply_properties", Description: "Copy raw settings from a source object to targets, or copy one verified fade/slide/zoom track with template."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input applyPropertiesInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			if input.Template != "" {
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
				_, plan, err := runRawTrackPlan(ctx, client, input.mutationContext, input.targetSpec, trackUpdate{Effect: effect, Item: item, RawValue: raw}, input.DryRun)
				plan.Operation = "apply_properties"
				return nil, plan, err
			}
			targets, err := resolveTargetObjects(ctx, client, input.mutationContext, input.targetSpec, 1)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			includeRaw, excludeTrack, excludeSample := true, false, false
			values, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{
				ObjectID: input.SourceObjectID, Items: input.Items,
				IncludeRawValues: &includeRaw, IncludeTrackInfo: &excludeTrack, IncludeSampledValues: &excludeSample,
			})
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
			for _, object := range targets {
				if object.ID != input.SourceObjectID {
					ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: object.ID, Properties: properties})
				}
			}
			_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
			plan.Operation = "apply_properties"
			return nil, plan, err
		})
	addTool(server, &mcp.Tool{Name: "set_track_values", Description: "Apply raw track values previously read from AviUtl2 to target objects."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input setTracksInput) (*mcp.CallToolResult, operationPlanOutput, error) {
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
			for _, object := range objects {
				ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: object.ID, Properties: properties})
			}
			_, plan, err := executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
			plan.Operation = "set_track_values"
			return nil, plan, err
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
			for _, object := range selection.Objects {
				ids = append(ids, object.ID)
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
	out := operationPlanOutput{DryRun: dryRun, Operations: toAnyOperations(ops)}
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
		for _, value := range result.Results {
			if value.ObjectID != nil {
				ids = append(ids, *value.ObjectID)
			}
		}
		if len(ids) > 0 {
			values, inspectErr := client.InspectObjects(ctx, protocol.InspectObjectsParams{ObjectIDs: uniqueIDs(ids), IncludeEffects: true})
			if inspectErr == nil {
				out.Objects = values.Objects
			}
		}
	}
	return nil, out, err
}

func runRawTrackPlan(ctx context.Context, client *bridge.Client, expected mutationContext, target targetSpec, track trackUpdate, dry bool) (*mcp.CallToolResult, operationPlanOutput, error) {
	objects, err := resolveTargetObjects(ctx, client, expected, target, 1)
	if err != nil {
		return nil, operationPlanOutput{}, err
	}
	properties := []protocol.PropertyUpdate{{Effect: track.Effect, Item: track.Item, Value: track.RawValue}}
	ops := []protocol.BatchOperation{}
	for _, object := range objects {
		ops = append(ops, protocol.BatchOperation{Op: "update_object", ObjectID: object.ID, Properties: properties})
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
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
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
	var builder strings.Builder
	for {
		index := strings.Index(lowerValue, lowerFind)
		if index < 0 {
			builder.WriteString(value)
			break
		}
		builder.WriteString(value[:index])
		builder.WriteString(replacement)
		value = value[index+len(find):]
		lowerValue = lowerValue[index+len(find):]
	}
	return builder.String()
}

func firstTrackValue(value string) string {
	if index := strings.IndexByte(value, ','); index >= 0 {
		return value[:index]
	}
	return value
}

func readRawItem(ctx context.Context, client *bridge.Client, id uint64, effect, item string) (string, bool, error) {
	includeTrack, includeSample := false, false
	includeRaw := true
	result, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{
		ObjectID: id, Effect: effect, Items: []string{item},
		IncludeRawValues: &includeRaw, IncludeTrackInfo: &includeTrack, IncludeSampledValues: &includeSample,
	})
	if err != nil {
		return "", false, err
	}
	for _, effectValues := range result.Effects {
		for _, itemValue := range effectValues.Items {
			if itemValue.Name == item {
				return itemValue.RawValue, true, nil
			}
		}
	}
	return "", false, nil
}
