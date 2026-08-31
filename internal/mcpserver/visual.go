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

type rangeContactSheetInput struct {
	Start        int    `json:"start"`
	End          int    `json:"end"`
	Count        int    `json:"count,omitempty" jsonschema:"default 8, maximum 16"`
	Columns      int    `json:"columns,omitempty"`
	CellWidth    int    `json:"cell_width,omitempty"`
	CellHeight   int    `json:"cell_height,omitempty"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
	LabelFrames  bool   `json:"label_frames,omitempty"`
}
type compareFramesInput struct {
	BeforeFrame  int    `json:"before_frame"`
	AfterFrame   int    `json:"after_frame"`
	CellWidth    int    `json:"cell_width,omitempty"`
	CellHeight   int    `json:"cell_height,omitempty"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
}
type collisionSheetInput struct {
	LayerStart *int `json:"layer_start,omitempty"`
	LayerEnd   *int `json:"layer_end,omitempty"`
	FrameStart *int `json:"frame_start,omitempty"`
	FrameEnd   *int `json:"frame_end,omitempty"`
	CellWidth  int  `json:"cell_width,omitempty"`
	CellHeight int  `json:"cell_height,omitempty"`
}
type capturePreviewInput struct {
	Frame        int    `json:"frame"`
	MaxWidth     int    `json:"max_width,omitempty"`
	MaxHeight    int    `json:"max_height,omitempty"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
}
type capturePreviewOutput struct {
	SnapshotID string `json:"snapshot_id"`
	Frame      int    `json:"frame"`
	SessionID  string `json:"session_id"`
	Generation uint64 `json:"generation"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}
type compareSnapshotInput struct {
	SnapshotID   string `json:"snapshot_id"`
	Frame        int    `json:"frame"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
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

func addVisualTools(server *mcp.Server, client *bridge.Client) {
	store := &previewStore{values: map[string]storedPreview{}}
	mcp.AddTool(server, &mcp.Tool{Name: "capture_preview_snapshot", Description: "Capture an in-memory before-edit preview for later visual comparison."}, func(ctx context.Context, _ *mcp.CallToolRequest, input capturePreviewInput) (*mcp.CallToolResult, capturePreviewOutput, error) {
		if input.Frame < 0 {
			return nil, capturePreviewOutput{}, errors.New("frame must be non-negative")
		}
		if input.MaxWidth == 0 {
			input.MaxWidth = 640
		}
		if input.MaxHeight == 0 {
			input.MaxHeight = 640
		}
		preview, err := client.RenderPreview(ctx, protocol.RenderPreviewParams{Frame: input.Frame, MaxWidth: input.MaxWidth, MaxHeight: input.MaxHeight, ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects})
		if err != nil {
			return nil, capturePreviewOutput{}, err
		}
		img, err := previewImage(preview)
		if err != nil {
			return nil, capturePreviewOutput{}, err
		}
		data, err := previewPNG(preview)
		if err != nil {
			return nil, capturePreviewOutput{}, err
		}
		store.Lock()
		store.next++
		id := fmt.Sprintf("preview-%d", store.next)
		store.values[id] = storedPreview{image: img, frame: input.Frame, sessionID: preview.SessionID, generation: preview.Generation}
		store.order = append(store.order, id)
		if len(store.order) > 8 {
			delete(store.values, store.order[0])
			store.order = store.order[1:]
		}
		store.Unlock()
		content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: "image/png"}}}
		return content, capturePreviewOutput{SnapshotID: id, Frame: input.Frame, SessionID: preview.SessionID, Generation: preview.Generation, Width: preview.Width, Height: preview.Height}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "render_snapshot_comparison", Description: "Compare a captured before-edit snapshot with a newly rendered after-edit frame."}, func(ctx context.Context, _ *mcp.CallToolRequest, input compareSnapshotInput) (*mcp.CallToolResult, contactSheetOutput, error) {
		store.Lock()
		before, ok := store.values[input.SnapshotID]
		store.Unlock()
		if !ok {
			return nil, contactSheetOutput{}, errors.New("snapshot_id is unknown or expired")
		}
		preview, err := client.RenderPreview(ctx, protocol.RenderPreviewParams{Frame: input.Frame, MaxWidth: before.image.Bounds().Dx(), MaxHeight: before.image.Bounds().Dy(), ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects})
		if err != nil {
			return nil, contactSheetOutput{}, err
		}
		if preview.SessionID != before.sessionID {
			return nil, contactSheetOutput{}, errors.New("AviUtl2 session changed since snapshot")
		}
		after, err := previewImage(preview)
		if err != nil {
			return nil, contactSheetOutput{}, err
		}
		data, width, height, cells, err := composeContactSheetLabeled([]*image.NRGBA{before.image, after}, []int{before.frame, input.Frame}, 2, true)
		if err != nil {
			return nil, contactSheetOutput{}, err
		}
		content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: "image/png"}}}
		return content, contactSheetOutput{Width: width, Height: height, SessionID: preview.SessionID, Generation: preview.Generation, Cells: cells}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "render_range_contact_sheet", Description: "Sample an inclusive frame range evenly into a labeled contact sheet."}, func(ctx context.Context, _ *mcp.CallToolRequest, input rangeContactSheetInput) (*mcp.CallToolResult, contactSheetOutput, error) {
		if input.Start < 0 || input.End < input.Start {
			return nil, contactSheetOutput{}, errors.New("invalid frame range")
		}
		if input.Count == 0 {
			input.Count = 8
		}
		if input.Count < 1 || input.Count > 16 {
			return nil, contactSheetOutput{}, errors.New("count must be 1..16")
		}
		frames := make([]int, input.Count)
		if input.Count == 1 {
			frames[0] = input.Start
		} else {
			for i := range frames {
				frames[i] = input.Start + (input.End-input.Start)*i/(input.Count-1)
			}
		}
		return renderContactSheet(ctx, client, contactSheetInput{Frames: frames, Columns: input.Columns, CellWidth: input.CellWidth, CellHeight: input.CellHeight, ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects, LabelFrames: input.LabelFrames})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "render_change_comparison", Description: "Render two frames side by side for before/after visual comparison."}, func(ctx context.Context, _ *mcp.CallToolRequest, input compareFramesInput) (*mcp.CallToolResult, contactSheetOutput, error) {
		if input.BeforeFrame < 0 || input.AfterFrame < 0 {
			return nil, contactSheetOutput{}, errors.New("frames must be non-negative")
		}
		return renderContactSheet(ctx, client, contactSheetInput{Frames: []int{input.BeforeFrame, input.AfterFrame}, Columns: 2, CellWidth: input.CellWidth, CellHeight: input.CellHeight, ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects, LabelFrames: true})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "render_collision_sheet", Description: "Find overlaps and render their frames as a labeled contact sheet."}, func(ctx context.Context, _ *mcp.CallToolRequest, input collisionSheetInput) (*mcp.CallToolResult, contactSheetOutput, error) {
		_, objects, err := readFilteredTimeline(ctx, client, timelineAnalysisInput{LayerStart: input.LayerStart, LayerEnd: input.LayerEnd, FrameStart: input.FrameStart, FrameEnd: input.FrameEnd})
		if err != nil {
			return nil, contactSheetOutput{}, err
		}
		frames := []int{}
		seen := map[int]bool{}
		for _, values := range groupObjectsByLayer(objects) {
			sortObjects(values)
			for i := 0; i < len(values); i++ {
				for j := i + 1; j < len(values) && values[j].Start <= values[i].End; j++ {
					frame := values[j].Start
					if !seen[frame] {
						seen[frame] = true
						frames = append(frames, frame)
					}
				}
			}
		}
		sort.Ints(frames)
		if len(frames) == 0 {
			return nil, contactSheetOutput{}, errors.New("no collisions found")
		}
		if len(frames) > 16 {
			frames = frames[:16]
		}
		return renderContactSheet(ctx, client, contactSheetInput{Frames: frames, Columns: 4, CellWidth: input.CellWidth, CellHeight: input.CellHeight, LabelFrames: true})
	})
}
