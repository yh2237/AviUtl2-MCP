package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

func notifyProgress(ctx context.Context, request *mcp.CallToolRequest, progress, total float64, message string) {
	if request == nil || request.Params == nil || request.Session == nil {
		return
	}
	token := request.Params.GetProgressToken()
	if token == nil {
		return
	}
	_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: progress, Total: total, Message: message})
}

func preflightBatch(ctx context.Context, client *bridge.Client, expected mutationContext, operations []protocol.BatchOperation) error {
	current, err := client.GetContext(ctx)
	if err != nil {
		return err
	}
	if current.SessionID != expected.SessionID || current.Generation != expected.Generation || current.SceneID != expected.SceneID {
		return errors.New("AviUtl2 context changed before batch preflight")
	}
	ids := []uint64{}
	for _, operation := range operations {
		if operation.ObjectID != 0 {
			ids = append(ids, operation.ObjectID)
		}
	}
	objects := map[uint64]protocol.Object{}
	if values := uniqueIDs(ids); len(values) > 0 {
		result, err := client.InspectObjects(ctx, protocol.InspectObjectsParams{ObjectIDs: values, IncludeEffects: true})
		if err != nil {
			return fmt.Errorf("object preflight: %w", err)
		}
		for _, object := range result.Objects {
			objects[object.ID] = object
		}
	}
	definitions := map[string]map[string]bool{}
	for index, operation := range operations {
		prefix := fmt.Sprintf("operations[%d]", index)
		if operation.File != "" && (operation.Op == "add_media" || operation.Op == "replace_media") {
			media, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: operation.File, Strict: true})
			if err != nil {
				return fmt.Errorf("%s media preflight: %w", prefix, err)
			}
			if !media.Supported {
				return fmt.Errorf("%s media is not supported: %s", prefix, operation.File)
			}
		}
		object, hasObject := objects[operation.ObjectID]
		switch operation.Op {
		case "create_section":
			if hasObject && (*operation.Frame <= object.Start || *operation.Frame > object.End) {
				return fmt.Errorf("%s section frame must be inside object %d", prefix, object.ID)
			}
		case "delete_section":
			if hasObject && *operation.Section >= len(object.Sections) {
				return fmt.Errorf("%s section does not exist", prefix)
			}
		case "move_section":
			if hasObject {
				section, frame := *operation.Section, *operation.Frame
				if section > len(object.Sections) {
					return fmt.Errorf("%s section is out of range", prefix)
				}
				if section > 0 && frame <= object.Sections[section-1] {
					return fmt.Errorf("%s section crosses the previous section", prefix)
				}
				if section < len(object.Sections)-1 && frame >= object.Sections[section+1] {
					return fmt.Errorf("%s section crosses the next section", prefix)
				}
				if section == len(object.Sections)-1 && frame >= object.End {
					return fmt.Errorf("%s section crosses the object end", prefix)
				}
			}
		case "delete_effect", "set_effect_state":
			if hasObject && operation.EffectIndex >= len(object.Effects) {
				return fmt.Errorf("%s effect_index is out of range", prefix)
			}
		}
		for _, property := range operation.Properties {
			base := baseEffectName(property.Effect)
			items, ok := definitions[base]
			if !ok {
				definition, err := client.ListEffectItems(ctx, base)
				if err != nil {
					return fmt.Errorf("%s effect %q: %w", prefix, base, err)
				}
				items = map[string]bool{}
				for _, item := range definition.Items {
					items[item.Name] = true
				}
				definitions[base] = items
			}
			if !items[property.Item] {
				return fmt.Errorf("%s effect %q has no item %q", prefix, base, property.Item)
			}
		}
		if operation.Op == "replace_media" && operation.Effect != "" && hasObject {
			if !objectHasEffect(object, baseEffectName(operation.Effect)) {
				return fmt.Errorf("%s object does not contain effect %q", prefix, operation.Effect)
			}
			if operation.Item != "" {
				base := baseEffectName(operation.Effect)
				items, ok := definitions[base]
				if !ok {
					definition, err := client.ListEffectItems(ctx, base)
					if err != nil {
						return err
					}
					items = map[string]bool{}
					for _, item := range definition.Items {
						items[item.Name] = true
					}
					definitions[base] = items
				}
				if !items[operation.Item] {
					return fmt.Errorf("%s effect has no item %q", prefix, operation.Item)
				}
			}
		}
	}
	return preflightPlacements(ctx, client, expected, operations)
}

type simulatedPlacement struct {
	key                         int64
	id                          uint64
	layer, start, end, sections int
}

func preflightPlacements(ctx context.Context, client *bridge.Client, expected mutationContext, operations []protocol.BatchOperation) error {
	needs := false
	for _, op := range operations {
		switch op.Op {
		case "add_text", "add_media", "duplicate_object", "update_object", "delete_object", "move_section":
			needs = true
		}
	}
	if !needs {
		return nil
	}
	_, objects, err := inspectTimelineSnapshot(ctx, client, expected, 0, 0, 0)
	if err != nil {
		return err
	}
	placements := map[int64]simulatedPlacement{}
	for _, o := range objects {
		placements[int64(o.ID)] = simulatedPlacement{key: int64(o.ID), id: o.ID, layer: o.Layer, start: o.Start, end: o.End, sections: len(o.Sections)}
	}
	refs := map[int]int64{}
	for index, op := range operations {
		key := int64(op.ObjectID)
		if op.ResultRef != nil {
			key = refs[*op.ResultRef]
		}
		switch op.Op {
		case "delete_object":
			delete(placements, key)
		case "add_text", "add_media":
			if op.Length > 0 {
				key = -int64(index + 1)
				p := simulatedPlacement{key: key, layer: *op.Layer, start: *op.Frame, end: *op.Frame + op.Length - 1}
				if err := checkPlacement(p, placements); err != nil {
					return fmt.Errorf("operations[%d]: %w", index, err)
				}
				placements[key] = p
				refs[index] = key
			}
		case "duplicate_object":
			key = -int64(index + 1)
			p := simulatedPlacement{key: key, layer: *op.Layer, start: *op.Frame, end: *op.Frame + op.Length - 1}
			if err := checkPlacement(p, placements); err != nil {
				return fmt.Errorf("operations[%d]: %w", index, err)
			}
			placements[key] = p
			refs[index] = key
		case "update_object":
			p, ok := placements[key]
			if !ok {
				continue
			}
			delete(placements, key)
			length := p.end - p.start
			if op.Layer != nil {
				p.layer = *op.Layer
			}
			if op.Frame != nil {
				p.start = *op.Frame
				p.end = p.start + length
			}
			if err := checkPlacement(p, placements); err != nil {
				return fmt.Errorf("operations[%d]: %w", index, err)
			}
			placements[key] = p
		case "create_section":
			p, ok := placements[key]
			if ok {
				p.sections++
				placements[key] = p
			}
		case "delete_section":
			p, ok := placements[key]
			if ok && p.sections > 1 {
				p.sections--
				placements[key] = p
			}
		case "move_section":
			p, ok := placements[key]
			if !ok {
				continue
			}
			if op.Section == nil || (*op.Section != 0 && *op.Section != p.sections) {
				continue
			}
			delete(placements, key)
			if *op.Section == 0 {
				p.start = *op.Frame
			} else {
				p.end = *op.Frame
			}
			if err := checkPlacement(p, placements); err != nil {
				return fmt.Errorf("operations[%d]: %w", index, err)
			}
			placements[key] = p
		}
	}
	return nil
}

func checkPlacement(candidate simulatedPlacement, placements map[int64]simulatedPlacement) error {
	if candidate.start < 0 || candidate.end < candidate.start {
		return errors.New("invalid simulated placement")
	}
	for _, other := range placements {
		if candidate.layer == other.layer && candidate.start <= other.end && other.start <= candidate.end {
			return fmt.Errorf("target overlaps object %d at layer %d frames %d..%d", other.id, candidate.layer, max(candidate.start, other.start), min(candidate.end, other.end))
		}
	}
	return nil
}

func inspectBatchResults(ctx context.Context, client *bridge.Client, operations []protocol.BatchOperation, result protocol.MutationResult) []protocol.Object {
	ids := []uint64{}
	for index, value := range result.Results {
		if value.ObjectID != nil && index < len(operations) && operations[index].Op != "delete_object" {
			ids = append(ids, *value.ObjectID)
		} else if index < len(operations) && operations[index].ObjectID != 0 && operations[index].Op != "delete_object" {
			ids = append(ids, operations[index].ObjectID)
		}
	}
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	values, err := client.InspectObjects(ctx, protocol.InspectObjectsParams{ObjectIDs: ids, IncludeEffects: true})
	if err != nil {
		return nil
	}
	return values.Objects
}

func baseEffectName(value string) string {
	if index := strings.LastIndexByte(value, ':'); index > 0 {
		if _, err := strconv.Atoi(value[index+1:]); err == nil {
			return value[:index]
		}
	}
	return value
}
