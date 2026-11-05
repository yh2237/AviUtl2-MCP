package mcpserver

// Motion support via AviUtl2-native track values.
//
// Real measurements against the AviUtl2 host show set_object_item_value accepts
// a single-segment track string in this exact grammar:
//
//	value1,value2,MODE,extra
//
// where value1 is the value at object start, value2 the value at object end,
// MODE is one of the localized movement-mode names (直線移動=linear,
// 補間移動=smooth interpolation, 瞬間移動=instant, ランダム移動=random,
// 再生範囲=constant/hold). The trailing token is accepted but measurably does
// NOT alter the evaluated curve (sampled X at t=10..90% is identical for
// 0.00 vs 1.00 under 直線移動 and 補間移動), so we always write 0.00.
// Note: 補間移動 between two endpoints is an asymmetric biased ease-in curve
// (~31% displacement at t=50%), not a symmetric ease-in-out; motion-design
// slow-in/slow-out is best composed by chaining short segments with uneven
// spacing (one object per segment, as the AviUtl2 track bar edits anyway).
// The alternate order (value,frame,MODE,value) parses and was measured to jump
// to the trailing value at that frame. These strings can also be written
// straight into .aup2 files; the host evaluates them on load. Multi-segment
// concatenation is NOT evaluated (only the first segment), so
// one object == one motion segment.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

var motionModes = map[string]string{
	"linear":   "直線移動",
	"smooth":   "補間移動",
	"instant":  "瞬間移動",
	"random":   "ランダム移動",
	"constant": "再生範囲",
}

type motionTrack struct {
	Item   string  `json:"item" jsonschema:"track bar item name, e.g. X, Y, 透明度, 拡大率 (use effect for non-drawing effects)"`
	Effect string  `json:"effect,omitempty" jsonschema:"owning effect; default 標準描画"`
	From   float64 `json:"from" jsonschema:"value at the object start frame"`
	To     float64 `json:"to" jsonschema:"value at the object end frame"`
	Mode   string  `json:"mode,omitempty" jsonschema:"linear, smooth, instant, random, or constant; default linear"`
}

func buildTrackValue(t motionTrack) (string, error) {
	if t.Item == "" {
		return "", errors.New("track item is required")
	}
	if math.IsNaN(t.From) || math.IsInf(t.From, 0) || math.IsNaN(t.To) || math.IsInf(t.To, 0) {
		return "", fmt.Errorf("track %q values must be finite", t.Item)
	}
	mode := t.Mode
	if mode == "" {
		mode = "linear"
	}
	local, ok := motionModes[mode]
	if !ok {
		return "", fmt.Errorf("unknown motion mode %q; use linear, smooth, instant, random, or constant", mode)
	}
	if t.Effect == "" {
		t.Effect = "標準描画"
	}
	extra := "0.00" // trailing token: accepted by the host, no measurable curve effect
	value := strconv.FormatFloat(t.From, 'f', 2, 64) + "," + strconv.FormatFloat(t.To, 'f', 2, 64) + "," + local + "," + extra
	return value, nil
}

func trackProperties(tracks []motionTrack) ([]protocol.PropertyUpdate, error) {
	if len(tracks) == 0 {
		return nil, errors.New("at least one track is required")
	}
	props := make([]protocol.PropertyUpdate, 0, len(tracks))
	for _, track := range tracks {
		value, err := buildTrackValue(track)
		if err != nil {
			return nil, err
		}
		effect := track.Effect
		if effect == "" {
			effect = "標準描画"
		}
		props = append(props, protocol.PropertyUpdate{Effect: effect, Item: track.Item, Value: value})
	}
	return props, nil
}

type setMotionInput struct {
	mutationContext
	ObjectID uint64        `json:"object_id"`
	Tracks   []motionTrack `json:"tracks"`
	DryRun   bool          `json:"dry_run,omitempty"`
}

type addMotionInput struct {
	mutationContext
	File   string  `json:"file,omitempty" jsonschema:"media file to add (mutually exclusive with text)"`
	Text   string  `json:"text,omitempty" jsonschema:"text content to add (mutually exclusive with file)"`
	Layer  int     `json:"layer"`
	Frame  int     `json:"frame"`
	Length int     `json:"length"`
	Size   float64 `json:"size,omitempty"`
	Color  string  `json:"color,omitempty"`
	Tracks []motionTrack `json:"tracks"`
	DryRun bool            `json:"dry_run,omitempty"`
}

func addMotionTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "set_motion", Description: "Attach an AviUtl2-native motion track (linear/smooth/instant ramp) to an existing object item in one Undo unit. One object = one motion segment."},
		func(ctx context.Context, request *mcp.CallToolRequest, input setMotionInput) (*mcp.CallToolResult, operationPlanOutput, error) {
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
			ops := []protocol.BatchOperation{{Op: "update_object", ObjectID: input.ObjectID, Properties: props}}
			return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
		})

	addTool(server, &mcp.Tool{Name: "add_motion", Description: "Create one media/text object carrying AviUtl2-native motion tracks over its full length (a single moving element, directly editable in the AviUtl2 track bar)."},
		func(ctx context.Context, request *mcp.CallToolRequest, input addMotionInput) (*mcp.CallToolResult, operationPlanOutput, error) {
			if err := input.mutationContext.validate(); err != nil {
				return nil, operationPlanOutput{}, err
			}
			if (input.File == "") == (input.Text == "") {
				return nil, operationPlanOutput{}, errors.New("specify exactly one of file or text")
			}
			if input.Layer < 0 || input.Frame < 0 || input.Length < 1 {
				return nil, operationPlanOutput{}, errors.New("layer, frame, and a positive length are required")
			}
			props, err := trackProperties(input.Tracks)
			if err != nil {
				return nil, operationPlanOutput{}, err
			}
			ops := []protocol.BatchOperation{}
			if input.File != "" {
				info, err := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: input.File, Strict: true})
				if err != nil {
					return nil, operationPlanOutput{}, err
				}
				if !info.Supported {
					return nil, operationPlanOutput{}, fmt.Errorf("media is unsupported: %s", input.File)
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
			return executeOperationPlan(ctx, client, input.mutationContext, ops, input.DryRun, false)
		})
}
