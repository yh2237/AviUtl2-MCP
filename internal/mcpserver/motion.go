package mcpserver

// The moving-track form below was measured on AviUtl2, not specified by the
// SDK as a portable serialization grammar. Keep raw-value copying available via
// set_track_values. This helper deliberately supports only single-section objects.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

// EDIT_HANDLE::EFFECT_ITEM_TYPE_NUMBER in plugin2.h.
const motionNumberItemType = 2

var motionModes = map[string]string{
	"linear":  "直線移動",
	"smooth":  "補間移動",
	"instant": "瞬間移動",
	"random":  "ランダム移動",
}

type motionTrack struct {
	Item   string  `json:"item" jsonschema:"numeric track item, e.g. X, Y, 透明度, 拡大率"`
	Effect string  `json:"effect,omitempty" jsonschema:"owning effect; default 標準描画; set_motion accepts occurrence suffixes such as ぼかし:1"`
	From   float64 `json:"from" jsonschema:"start value; constant requires from and to to be equal"`
	To     float64 `json:"to" jsonschema:"end value; random uses the endpoints as its range, not a guaranteed final value"`
	Mode   string  `json:"mode,omitempty" jsonschema:"linear, smooth, instant, random, or constant; default linear; host controls interpolation"`
}

func buildTrackValue(t motionTrack) (string, error) {
	if strings.TrimSpace(t.Item) == "" {
		return "", errors.New("track item is required")
	}
	if math.IsNaN(t.From) || math.IsInf(t.From, 0) || math.IsNaN(t.To) || math.IsInf(t.To, 0) {
		return "", fmt.Errorf("track %q values must be finite", t.Item)
	}
	from := strconv.FormatFloat(t.From, 'f', -1, 64)
	if t.Mode == "constant" {
		if t.From != t.To {
			return "", errors.New("constant requires equal from and to values")
		}
		return from, nil // scalar value: 移動無し, not 再生範囲
	}
	mode := t.Mode
	if mode == "" {
		mode = "linear"
	}
	local, ok := motionModes[mode]
	if !ok {
		return "", fmt.Errorf("unknown motion mode %q; use linear, smooth, instant, random, or constant", mode)
	}
	// Zero is the host's movement flags field, not an extra control point.
	return from + "," + strconv.FormatFloat(t.To, 'f', -1, 64) + "," + local + ",0", nil
}

func motionEffectParts(effect string) (string, int, error) {
	base := baseEffectName(effect)
	occurrence := 0
	if base != effect {
		occurrence, _ = strconv.Atoi(effect[len(base)+1:])
	}
	if strings.TrimSpace(base) == "" || occurrence < 0 {
		return "", 0, errors.New("effect must have a name and a non-negative occurrence index")
	}
	return base, occurrence, nil
}

func trackProperties(tracks []motionTrack) ([]protocol.PropertyUpdate, error) {
	if len(tracks) == 0 || len(tracks) > 100 {
		return nil, errors.New("between 1 and 100 tracks are required")
	}
	props := make([]protocol.PropertyUpdate, 0, len(tracks))
	seen := map[string]bool{}
	for _, track := range tracks {
		value, err := buildTrackValue(track)
		if err != nil {
			return nil, err
		}
		effect := track.Effect
		if effect == "" {
			effect = "標準描画"
		}
		base, occurrence, err := motionEffectParts(effect)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d\x00%s", base, occurrence, track.Item)
		if seen[key] {
			return nil, fmt.Errorf("duplicate motion track %q / %q", effect, track.Item)
		}
		seen[key] = true
		props = append(props, protocol.PropertyUpdate{Effect: effect, Item: track.Item, Value: value})
	}
	return props, nil
}

// Validate against the actual object, not just the global effect catalog. An
// item name can exist globally while its effect is absent from the target.
func preflightExistingMotion(ctx context.Context, client *bridge.Client, expected mutationContext, id uint64, props []protocol.PropertyUpdate) error {
	no := false
	values, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{
		ObjectID: id, IncludeRawValues: &no, IncludeSampledValues: &no,
	})
	if err != nil {
		return err
	}
	if values.Context.SessionID != expected.SessionID || values.Context.Generation != expected.Generation || values.Context.SceneID != expected.SceneID {
		return errors.New("AviUtl2 context changed before motion preflight")
	}
	if len(values.Object.Sections) > 1 {
		return errors.New("set_motion supports objects without intermediate sections; use set_track_values to preserve sectioned tracks")
	}
	for _, property := range props {
		base, occurrence, _ := motionEffectParts(property.Effect)
		var effect *protocol.ObjectEffectValues
		for i := range values.Effects {
			candidate := &values.Effects[i]
			if candidate.Name == base {
				if occurrence == 0 {
					effect = candidate
					break
				}
				occurrence--
			}
		}
		if effect == nil {
			return fmt.Errorf("object does not contain effect %q", property.Effect)
		}
		if effect.Locked {
			return fmt.Errorf("effect %q is locked", property.Effect)
		}
		found := false
		for _, item := range effect.Items {
			if item.Name == property.Item && item.Type == motionNumberItemType && item.Track != nil {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("effect %q has no numeric track %q", property.Effect, property.Item)
		}
	}
	return nil
}

// Before creation we can verify only effects that the creation path guarantees.
// Other effects must be added explicitly, then configured with set_motion.
func preflightNewMotion(ctx context.Context, client *bridge.Client, text bool, props []protocol.PropertyUpdate) error {
	definitions := map[string][]protocol.EffectItem{}
	for _, property := range props {
		base, occurrence, _ := motionEffectParts(property.Effect)
		if occurrence != 0 || (base != "標準描画" && !(text && base == "テキスト")) {
			return fmt.Errorf("add_motion cannot create effect %q; add the object and effect first, then use set_motion", property.Effect)
		}
		items, ok := definitions[base]
		if !ok {
			definition, err := client.ListEffectItems(ctx, base)
			if err != nil {
				return err
			}
			items = definition.Items
			definitions[base] = items
		}
		found := false
		for _, item := range items {
			if item.Name == property.Item && item.Type == motionNumberItemType {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("effect %q has no numeric track %q", property.Effect, property.Item)
		}
	}
	return nil
}

type setMotionInput struct {
	mutationContext
	ObjectID uint64        `json:"object_id"`
	Tracks   []motionTrack `json:"tracks"`
	DryRun   bool          `json:"dry_run,omitempty"`
}

type addMotionInput struct {
	mutationContext
	File   string        `json:"file,omitempty" jsonschema:"image or video file, mutually exclusive with text; audio-only is not supported"`
	Text   string        `json:"text,omitempty" jsonschema:"text content, mutually exclusive with file"`
	Layer  int           `json:"layer"`
	Frame  int           `json:"frame"`
	Length int           `json:"length"`
	Size   float64       `json:"size,omitempty"`
	Color  string        `json:"color,omitempty"`
	Tracks []motionTrack `json:"tracks"`
	DryRun bool          `json:"dry_run,omitempty"`
}

func addMotionTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "set_motion", Description: "Set typed numeric motion tracks on an existing object in one Undo unit. Rejects objects with intermediate sections. dry_run validates without editing."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input setMotionInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, operationPlanOutput{}, err
			}
			if input.ObjectID == 0 {
				return nil, operationPlanOutput{}, errors.New("object_id is required")
			}
			props, err := trackProperties(input.Tracks)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if err := preflightExistingMotion(ctx, client, input.mutationContext, input.ObjectID, props); err != nil {
				return nil, operationPlanOutput{}, err
			}
			ops := []protocol.BatchOperation{{Op: "update_object", ObjectID: input.ObjectID, Properties: props}}
			return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		})

	addTool(server, &mcp.Tool{Name: "add_motion", Description: "Create one image/video/text object with typed motion tracks in one Undo unit. Supports default 標準描画 tracks and numeric テキスト items for text. dry_run validates without creating objects."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input addMotionInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, operationPlanOutput{}, err
			}
			if (input.File == "") == (input.Text == "") {
				return nil, operationPlanOutput{}, errors.New("specify exactly one of file or text")
			}
			if input.Layer < 0 || input.Layer > math.MaxInt32 || input.Frame < 0 || input.Length < 1 || input.Length > math.MaxInt32 || input.Frame > math.MaxInt32-input.Length+1 {
				return nil, operationPlanOutput{}, errors.New("layer and frame must be non-negative and length positive, within the host frame range")
			}
			props, err := trackProperties(input.Tracks)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			if err := preflightNewMotion(ctx, client, input.Text != "", props); err != nil {
				return nil, operationPlanOutput{}, err
			}
			ops := []protocol.BatchOperation{}
			if input.File != "" {
				info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: input.File, Strict: true})
				if err != nil {
					return nil, operationPlanOutput{}, err
				}
				if !info.Supported || info.VideoTrackCount < 1 {
					return nil, operationPlanOutput{}, fmt.Errorf("add_motion requires supported image/video media: %s", input.File)
				}
				ops = append(ops, protocol.BatchOperation{Op: "add_media", File: input.File, Layer: &input.Layer, Frame: &input.Frame, Length: input.Length})
			} else {
				size, color := input.Size, input.Color
				if size == 0 {
					size = 34
				}
				if color == "" {
					color = "ffffff"
				}
				ops = append(ops, protocol.BatchOperation{Op: "add_text", Text: input.Text, Layer: &input.Layer, Frame: &input.Frame, Length: input.Length, Size: size, Color: color})
			}
			ref := 0
			ops = append(ops, protocol.BatchOperation{Op: "update_object", ResultRef: &ref, Properties: props})
			return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, true)
		})
}
