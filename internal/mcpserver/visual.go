package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type contactSheetRequest struct {
	Source       string `json:"source" jsonschema:"frames, range, or collision"`
	Frames       []int  `json:"frames,omitempty" jsonschema:"frames source: one to sixteen zero-based frames"`
	Start        int    `json:"start,omitempty" jsonschema:"range source: first frame"`
	End          int    `json:"end,omitempty" jsonschema:"range source: last frame"`
	Count        int    `json:"count,omitempty" jsonschema:"range source: samples, default 8 and maximum 16"`
	Columns      int    `json:"columns,omitempty" jsonschema:"one to four columns"`
	CellWidth    int    `json:"cell_width,omitempty"`
	CellHeight   int    `json:"cell_height,omitempty"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
	LabelFrames  bool   `json:"label_frames,omitempty"`
	LayerStart   *int   `json:"layer_start,omitempty" jsonschema:"collision source: layer filter"`
	LayerEnd     *int   `json:"layer_end,omitempty"`
	FrameStart   *int   `json:"frame_start,omitempty"`
	FrameEnd     *int   `json:"frame_end,omitempty"`
}

type compareRequest struct {
	Operation    string `json:"operation" jsonschema:"compare, capture, or compare_capture"`
	BeforeFrame  int    `json:"before_frame,omitempty" jsonschema:"compare operation: first frame"`
	AfterFrame   int    `json:"after_frame,omitempty" jsonschema:"compare operation: second frame"`
	Frame        int    `json:"frame,omitempty" jsonschema:"frame to snapshot for capture, or the after-edit frame for compare_capture"`
	SnapshotID   string `json:"snapshot_id,omitempty" jsonschema:"snapshot id from the capture operation"`
	MaxWidth     int    `json:"max_width,omitempty" jsonschema:"maximum width for capture, default 640"`
	MaxHeight    int    `json:"max_height,omitempty"`
	CellWidth    int    `json:"cell_width,omitempty"`
	CellHeight   int    `json:"cell_height,omitempty"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
}

type compareOutput struct {
	Operation  string             `json:"operation"`
	SnapshotID string             `json:"snapshot_id,omitempty"`
	Frame      int                `json:"frame,omitempty"`
	Width      int                `json:"width"`
	Height     int                `json:"height"`
	SessionID  string             `json:"session_id"`
	Generation uint64             `json:"generation"`
	Cells      []contactSheetCell `json:"cells,omitempty"`
}

type storedPreview struct {
	image      *image.NRGBA
	frame      int
	sessionID  string
	generation uint64
}

type previewStore struct {
	sync.Mutex
	next   uint64
	values map[string]storedPreview
	order  []string
}

func (s *previewStore) put(snapshot storedPreview) string {
	s.Lock()
	defer s.Unlock()
	s.next++
	id := fmt.Sprintf("preview-%d", s.next)
	s.values[id] = snapshot
	s.order = append(s.order, id)
	if len(s.order) > 8 {
		delete(s.values, s.order[0])
		s.order = s.order[1:]
	}
	return id
}

func (s *previewStore) get(id string) (storedPreview, bool) {
	s.Lock()
	defer s.Unlock()
	value, ok := s.values[id]
	return value, ok
}

func collisionFrames(ctx context.Context, client *bridge.Client, input contactSheetRequest) ([]int, error) {
	_, objects, err := readFilteredTimeline(ctx, client, timelineAnalysisInput{
		LayerStart: input.LayerStart, LayerEnd: input.LayerEnd, FrameStart: input.FrameStart, FrameEnd: input.FrameEnd,
	})
	if err != nil {
		return nil, err
	}
	frames := []int{}
	seen := map[int]bool{}
	for _, values := range groupObjectsByLayer(objects) {
		sortObjects(values)
		for i := 0; i < len(values); i++ {
			for j := i + 1; j < len(values) && values[j].Start <= values[i].End; j++ {
				if !seen[values[j].Start] {
					seen[values[j].Start] = true
					frames = append(frames, values[j].Start)
				}
			}
		}
	}
	if len(frames) == 0 {
		return nil, errors.New("no collisions found")
	}
	sort.Ints(frames)
	if len(frames) > 16 {
		frames = frames[:16]
	}
	return frames, nil
}

func addVisualTools(server *mcp.Server, client *bridge.Client) {
	store := &previewStore{values: map[string]storedPreview{}}

	addTool(server, &mcp.Tool{Name: "render_contact_sheet", Description: "Render up to sixteen frames into one PNG. source=frames for explicit frames, range to sample a span, collision to review overlaps."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input contactSheetRequest) (*mcp.CallToolResult, contactSheetOutput, error) {
			sheet := contactSheetInput{
				Frames: input.Frames, Columns: input.Columns, CellWidth: input.CellWidth, CellHeight: input.CellHeight,
				ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects, LabelFrames: input.LabelFrames,
			}
			switch input.Source {
			case "frames":
			case "range":
				if input.Start < 0 || input.End < input.Start {
					return nil, contactSheetOutput{}, errors.New("invalid frame range")
				}
				count := input.Count
				if count == 0 {
					count = 8
				}
				if count < 1 || count > 16 {
					return nil, contactSheetOutput{}, errors.New("count must be 1..16")
				}
				sheet.Frames = make([]int, count)
				for index := range sheet.Frames {
					if count == 1 {
						sheet.Frames[index] = input.Start
					} else {
						sheet.Frames[index] = input.Start + (input.End-input.Start)*index/(count-1)
					}
				}
				sheet.LabelFrames = true
			case "collision":
				frames, err := collisionFrames(ctx, client, input)
				if err != nil {
					return nil, contactSheetOutput{}, err
				}
				sheet.Frames = frames
				sheet.Columns = 4
				sheet.LabelFrames = true
			default:
				return nil, contactSheetOutput{}, fmt.Errorf("unknown source %q; use frames, range, or collision", input.Source)
			}
			result, output, err := renderContactSheet(ctx, client, sheet)
			output.Operation = input.Source
			return result, output, err
		})

	addTool(server, &mcp.Tool{Name: "compare_frames", Description: "Capture a before-edit snapshot, compare two frames, or compare a snapshot with a fresh render."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input compareRequest) (*mcp.CallToolResult, compareOutput, error) {
			out := compareOutput{Operation: input.Operation}
			switch input.Operation {
			case "compare":
				if input.BeforeFrame < 0 || input.AfterFrame < 0 {
					return nil, out, errors.New("frames must be non-negative")
				}
				result, sheet, err := renderContactSheet(ctx, client, contactSheetInput{
					Frames: []int{input.BeforeFrame, input.AfterFrame}, Columns: 2,
					CellWidth: input.CellWidth, CellHeight: input.CellHeight,
					ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects, LabelFrames: true,
				})
				out.Width, out.Height, out.SessionID, out.Generation, out.Cells = sheet.Width, sheet.Height, sheet.SessionID, sheet.Generation, sheet.Cells
				return result, out, err
			case "capture":
				if input.Frame < 0 {
					return nil, out, errors.New("frame must be non-negative")
				}
				if input.MaxWidth == 0 {
					input.MaxWidth = 640
				}
				if input.MaxHeight == 0 {
					input.MaxHeight = 640
				}
				preview, err := client.RenderPreview(ctx, protocol.RenderPreviewParams{
					Frame: input.Frame, MaxWidth: input.MaxWidth, MaxHeight: input.MaxHeight,
					ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects,
				})
				if err != nil {
					return nil, out, err
				}
				img, err := previewImage(preview)
				if err != nil {
					return nil, out, err
				}
				data, err := previewPNG(preview)
				if err != nil {
					return nil, out, err
				}
				id := store.put(storedPreview{image: img, frame: input.Frame, sessionID: preview.SessionID, generation: preview.Generation})
				out.SnapshotID, out.Frame = id, input.Frame
				out.Width, out.Height, out.SessionID, out.Generation = preview.Width, preview.Height, preview.SessionID, preview.Generation
				content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: "image/png"}}}
				return content, out, nil
			case "compare_capture":
				before, ok := store.get(input.SnapshotID)
				if !ok {
					return nil, out, errors.New("snapshot_id is unknown or expired")
				}
				preview, err := client.RenderPreview(ctx, protocol.RenderPreviewParams{
					Frame: input.Frame, MaxWidth: before.image.Bounds().Dx(), MaxHeight: before.image.Bounds().Dy(),
					ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects,
				})
				if err != nil {
					return nil, out, err
				}
				if preview.SessionID != before.sessionID {
					return nil, out, errors.New("AviUtl2 session changed since snapshot")
				}
				after, err := previewImage(preview)
				if err != nil {
					return nil, out, err
				}
				data, width, height, cells, err := composeContactSheetLabeled([]*image.NRGBA{before.image, after}, []int{before.frame, input.Frame}, 2, true)
				if err != nil {
					return nil, out, err
				}
				out.Frame, out.SnapshotID = input.Frame, input.SnapshotID
				out.Width, out.Height, out.SessionID, out.Generation, out.Cells = width, height, preview.SessionID, preview.Generation, cells
				content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: "image/png"}}}
				return content, out, nil
			default:
				return nil, out, fmt.Errorf("unknown operation %q; use compare, capture, or compare_capture", input.Operation)
			}
		})
}
