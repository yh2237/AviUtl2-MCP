package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type planCollision struct {
	ObjectID      uint64  `json:"object_id"`
	OtherObjectID *uint64 `json:"other_object_id,omitempty"`
	Layer         int     `json:"layer"`
	Start         int     `json:"start"`
	End           int     `json:"end"`
	Reason        string  `json:"reason"`
}

type timelinePlanOutput struct {
	DryRun     bool                      `json:"dry_run"`
	Blocked    bool                      `json:"blocked"`
	Operations []protocol.BatchOperation `json:"operations"`
	Collisions []planCollision           `json:"collisions,omitempty"`
	Mutation   *protocol.MutationResult  `json:"mutation,omitempty"`
}

type shiftObjectsInput struct {
	mutationContext
	ObjectIDs  []uint64 `json:"object_ids"`
	FrameDelta int      `json:"frame_delta,omitempty"`
	LayerDelta int      `json:"layer_delta,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
}

type alignObjectsInput struct {
	mutationContext
	ObjectIDs   []uint64 `json:"object_ids"`
	Edge        string   `json:"edge" jsonschema:"start, center, or end"`
	TargetFrame *int     `json:"target_frame,omitempty" jsonschema:"defaults to the outer edge of the selected objects"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

type distributeObjectsInput struct {
	mutationContext
	ObjectIDs  []uint64 `json:"object_ids"`
	StartFrame *int     `json:"start_frame,omitempty"`
	EndFrame   *int     `json:"end_frame,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
}

type insertTimeInput struct {
	mutationContext
	Frame      int  `json:"frame"`
	Length     int  `json:"length"`
	LayerStart *int `json:"layer_start,omitempty"`
	LayerEnd   *int `json:"layer_end,omitempty"`
	DryRun     bool `json:"dry_run,omitempty"`
}

type staggerObjectsInput struct {
	mutationContext
	ObjectIDs  []uint64 `json:"object_ids" jsonschema:"objects in placement order"`
	StartFrame *int     `json:"start_frame,omitempty" jsonschema:"defaults to the first object start"`
	FrameStep  int      `json:"frame_step"`
	LayerStep  int      `json:"layer_step,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
}

type replaceMediaInput struct {
	mutationContext
	ObjectID uint64 `json:"object_id"`
	File     string `json:"file"`
	Effect   string `json:"effect,omitempty"`
	Item     string `json:"item,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
}

type plannedMove struct {
	object protocol.Object
	layer  int
	start  int
	end    int
}

func addTimelineTools(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "shift_objects", Description: "Move several objects by frame/layer deltas after checking final and transient collisions."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input shiftObjectsInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
			objects, err := prepareObjects(ctx, client, input.mutationContext, input.ObjectIDs, 1)
			if err != nil {
				return nil, timelinePlanOutput{}, err
			}
			if input.FrameDelta == 0 && input.LayerDelta == 0 {
				return nil, timelinePlanOutput{}, errors.New("frame_delta or layer_delta is required")
			}
			moves := make([]plannedMove, 0, len(objects))
			for _, object := range objects {
				moves = append(moves, moveFor(object, object.Layer+input.LayerDelta, object.Start+input.FrameDelta))
			}
			return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "align_objects", Description: "Align object start, center, or end frames in one Undo unit."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input alignObjectsInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
			objects, err := prepareObjects(ctx, client, input.mutationContext, input.ObjectIDs, 2)
			if err != nil {
				return nil, timelinePlanOutput{}, err
			}
			target, err := alignmentTarget(objects, input.Edge, input.TargetFrame)
			if err != nil {
				return nil, timelinePlanOutput{}, err
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
			return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "distribute_objects", Description: "Distribute object start frames evenly between two anchors."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input distributeObjectsInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
			objects, err := prepareObjects(ctx, client, input.mutationContext, input.ObjectIDs, 3)
			if err != nil {
				return nil, timelinePlanOutput{}, err
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
				return nil, timelinePlanOutput{}, errors.New("distribution frame range must be ordered and non-negative")
			}
			moves := make([]plannedMove, 0, len(objects))
			for index, object := range objects {
				frame := start + (end-start)*index/(len(objects)-1)
				moves = append(moves, moveFor(object, object.Layer, frame))
			}
			return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "insert_time", Description: "Insert empty time by shifting objects beginning at or after a frame."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input insertTimeInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, timelinePlanOutput{}, err
			}
			if input.Frame < 0 || input.Length < 1 {
				return nil, timelinePlanOutput{}, errors.New("frame must be non-negative and length must be positive")
			}
			current, objects, err := inspectTimelineSnapshot(ctx, client, input.mutationContext, 0, -1, 0)
			if err != nil {
				return nil, timelinePlanOutput{}, err
			}
			layerStart, layerEnd := 0, current.LayerMax
			if input.LayerStart != nil {
				layerStart = *input.LayerStart
			}
			if input.LayerEnd != nil {
				layerEnd = *input.LayerEnd
			}
			if layerStart < 0 || layerEnd < layerStart {
				return nil, timelinePlanOutput{}, errors.New("layer range must be ordered and non-negative")
			}
			moves := make([]plannedMove, 0)
			for _, object := range objects {
				if object.Layer >= layerStart && object.Layer <= layerEnd && object.Start >= input.Frame {
					moves = append(moves, moveFor(object, object.Layer, object.Start+input.Length))
				}
			}
			if len(moves) == 0 {
				return nil, timelinePlanOutput{DryRun: input.DryRun, Operations: []protocol.BatchOperation{}}, nil
			}
			return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "stagger_objects", Description: "Place ordered objects at fixed frame and layer steps."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input staggerObjectsInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
			objects, err := prepareObjects(ctx, client, input.mutationContext, input.ObjectIDs, 2)
			if err != nil {
				return nil, timelinePlanOutput{}, err
			}
			if input.FrameStep == 0 && input.LayerStep == 0 {
				return nil, timelinePlanOutput{}, errors.New("frame_step or layer_step is required")
			}
			start := objects[0].Start
			if input.StartFrame != nil {
				start = *input.StartFrame
			}
			moves := make([]plannedMove, 0, len(objects))
			for index, object := range objects {
				moves = append(moves, moveFor(object, objects[0].Layer+input.LayerStep*index, start+input.FrameStep*index))
			}
			return executeMovePlan(ctx, client, input.mutationContext, moves, input.DryRun)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "replace_media", Description: "Replace an object's media file while preserving its placement, effects, and animation."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input replaceMediaInput) (*mcp.CallToolResult, timelinePlanOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, timelinePlanOutput{}, err
			}
			if input.ObjectID == 0 || input.File == "" || (input.Item != "" && input.Effect == "") {
				return nil, timelinePlanOutput{}, errors.New("object_id and file are required; item also requires effect")
			}
			media, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: input.File, Strict: true})
			if err != nil {
				return nil, timelinePlanOutput{}, err
			}
			if !media.Supported {
				return nil, timelinePlanOutput{}, errors.New("AviUtl2 does not support the replacement media file")
			}
			operation := protocol.BatchOperation{Op: "replace_media", ObjectID: input.ObjectID, File: input.File, Effect: input.Effect, Item: input.Item}
			output := timelinePlanOutput{DryRun: input.DryRun, Operations: []protocol.BatchOperation{operation}}
			if input.DryRun {
				return nil, output, nil
			}
			result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: output.Operations}, input.expected())
			output.Mutation = &result
			return nil, output, err
		})
}

func prepareObjects(ctx context.Context, client *bridge.Client, expected mutationContext, ids []uint64, minimum int) ([]protocol.Object, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	if len(ids) < minimum || len(ids) > protocol.MaxBatchOperations {
		return nil, fmt.Errorf("object_ids must contain between %d and %d entries", minimum, protocol.MaxBatchOperations)
	}
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("object_id %d is duplicated", id)
		}
		seen[id] = struct{}{}
	}
	return inspectObjects(ctx, client, ids, expected)
}

func alignmentTarget(objects []protocol.Object, edge string, explicit *int) (int, error) {
	if edge != "start" && edge != "center" && edge != "end" {
		return 0, errors.New("edge must be start, center, or end")
	}
	if explicit != nil {
		if *explicit < 0 {
			return 0, errors.New("target_frame must be non-negative")
		}
		return *explicit, nil
	}
	minimum, maximum := objects[0].Start, objects[0].End
	for _, object := range objects[1:] {
		minimum = min(minimum, object.Start)
		maximum = max(maximum, object.End)
	}
	switch edge {
	case "start":
		return minimum, nil
	case "center":
		return (minimum + maximum) / 2, nil
	default:
		return maximum, nil
	}
}

func moveFor(object protocol.Object, layer, start int) plannedMove {
	return plannedMove{object: object, layer: layer, start: start, end: start + object.End - object.Start}
}

func executeMovePlan(ctx context.Context, client *bridge.Client, expected mutationContext, moves []plannedMove, dryRun bool) (*mcp.CallToolResult, timelinePlanOutput, error) {
	filtered := moves[:0]
	for _, move := range moves {
		if move.layer < 0 || move.start < 0 {
			return nil, timelinePlanOutput{}, errors.New("planned layer and frame must be non-negative")
		}
		if move.layer != move.object.Layer || move.start != move.object.Start {
			filtered = append(filtered, move)
		}
	}
	moves = filtered
	if len(moves) > protocol.MaxBatchOperations {
		return nil, timelinePlanOutput{}, fmt.Errorf("planned edit contains %d moves; split it into batches of at most %d", len(moves), protocol.MaxBatchOperations)
	}
	collisions, err := detectCollisions(ctx, client, expected, moves)
	if err != nil {
		return nil, timelinePlanOutput{}, err
	}
	ordered, err := orderMoves(moves)
	if err != nil {
		return nil, timelinePlanOutput{}, err
	}
	operations := make([]protocol.BatchOperation, 0, len(ordered))
	for _, move := range ordered {
		layer, frame := move.layer, move.start
		operations = append(operations, protocol.BatchOperation{Op: "update_object", ObjectID: move.object.ID, Layer: &layer, Frame: &frame})
	}
	output := timelinePlanOutput{DryRun: dryRun, Blocked: len(collisions) > 0, Operations: operations, Collisions: collisions}
	if dryRun || output.Blocked {
		return nil, output, nil
	}
	if len(operations) == 0 {
		return nil, output, nil
	}
	result, err := client.ExecuteBatch(ctx, protocol.ExecuteBatchParams{Operations: operations}, expected.expected())
	output.Mutation = &result
	return nil, output, err
}

func detectCollisions(ctx context.Context, client *bridge.Client, expected mutationContext, moves []plannedMove) ([]planCollision, error) {
	collisions := make([]planCollision, 0)
	for left := 0; left < len(moves); left++ {
		for right := left + 1; right < len(moves); right++ {
			if overlaps(moves[left].layer, moves[left].start, moves[left].end, moves[right].layer, moves[right].start, moves[right].end) {
				other := moves[right].object.ID
				collisions = append(collisions, planCollision{ObjectID: moves[left].object.ID, OtherObjectID: &other, Layer: moves[left].layer, Start: moves[left].start, End: moves[left].end, Reason: "planned objects overlap"})
			}
		}
	}
	if len(moves) == 0 {
		return collisions, nil
	}
	maxLayer, maxFrame := 0, 0
	moving := make(map[uint64]struct{}, len(moves))
	for _, move := range moves {
		maxLayer = max(maxLayer, move.layer)
		maxFrame = max(maxFrame, move.end)
		moving[move.object.ID] = struct{}{}
	}
	_, existing, err := inspectTimelineSnapshot(ctx, client, expected, maxLayer, maxFrame, 0)
	if err != nil {
		return nil, err
	}
	for _, move := range moves {
		for _, object := range existing {
			if _, isMoving := moving[object.ID]; isMoving {
				continue
			}
			if overlaps(move.layer, move.start, move.end, object.Layer, object.Start, object.End) {
				other := object.ID
				collisions = append(collisions, planCollision{ObjectID: move.object.ID, OtherObjectID: &other, Layer: move.layer, Start: move.start, End: move.end, Reason: "target overlaps an existing object"})
			}
		}
	}
	return collisions, nil
}

func inspectTimelineSnapshot(ctx context.Context, client *bridge.Client, expected mutationContext, minimumLayer, minimumFrame, layerStart int) (protocol.Context, []protocol.Object, error) {
	current, err := client.GetContext(ctx)
	if err != nil {
		return protocol.Context{}, nil, err
	}
	if current.SessionID != expected.SessionID || current.Generation != expected.Generation || current.SceneID != expected.SceneID {
		return protocol.Context{}, nil, errors.New("AviUtl2 context changed while planning; call get_context and retry")
	}
	maxLayer := max(current.LayerMax, minimumLayer)
	maxFrame := max(max(current.FrameMax, minimumFrame), 0)
	objects := make([]protocol.Object, 0)
	for first := layerStart; first <= maxLayer; first += 100 {
		last := min(first+99, maxLayer)
		result, err := client.InspectTimeline(ctx, protocol.InspectTimelineParams{
			LayerStart: first, LayerEnd: last, FrameStart: 0, FrameEnd: maxFrame,
			MaxObjects: protocol.MaxTimelineObjects, IncludeEffects: false,
		})
		if err != nil {
			return protocol.Context{}, nil, err
		}
		if result.Context.SessionID != expected.SessionID || result.Context.Generation != expected.Generation || result.Context.SceneID != expected.SceneID {
			return protocol.Context{}, nil, errors.New("AviUtl2 context changed while reading the timeline")
		}
		if result.Truncated {
			return protocol.Context{}, nil, errors.New("timeline contains too many objects for safe collision detection")
		}
		objects = append(objects, result.Objects...)
	}
	return current, objects, nil
}

func orderMoves(moves []plannedMove) ([]plannedMove, error) {
	remaining := append([]plannedMove(nil), moves...)
	ordered := make([]plannedMove, 0, len(moves))
	for len(remaining) > 0 {
		selected := -1
		for index, candidate := range remaining {
			blocked := false
			for otherIndex, other := range remaining {
				if index == otherIndex {
					continue
				}
				if overlaps(candidate.layer, candidate.start, candidate.end, other.object.Layer, other.object.Start, other.object.End) {
					blocked = true
					break
				}
			}
			if !blocked {
				selected = index
				break
			}
		}
		if selected < 0 {
			return nil, errors.New("planned moves have a cyclic transient collision; split the operation or insert free space first")
		}
		ordered = append(ordered, remaining[selected])
		remaining = append(remaining[:selected], remaining[selected+1:]...)
	}
	return ordered, nil
}

func overlaps(leftLayer, leftStart, leftEnd, rightLayer, rightStart, rightEnd int) bool {
	return leftLayer == rightLayer && leftStart <= rightEnd && rightStart <= leftEnd
}
