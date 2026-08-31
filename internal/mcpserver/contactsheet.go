package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type contactSheetInput struct {
	Frames       []int  `json:"frames" jsonschema:"one to sixteen zero-based frames"`
	Columns      int    `json:"columns,omitempty" jsonschema:"one to four columns, default 4"`
	CellWidth    int    `json:"cell_width,omitempty" jsonschema:"maximum width per preview, default 320 and maximum 600"`
	CellHeight   int    `json:"cell_height,omitempty" jsonschema:"maximum height per preview, default 320 and maximum 600"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
	LabelFrames  bool   `json:"label_frames,omitempty"`
}

type contactSheetCell struct {
	Frame  int `json:"frame"`
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type contactSheetOutput struct {
	Width      int                `json:"width"`
	Height     int                `json:"height"`
	SessionID  string             `json:"session_id"`
	Generation uint64             `json:"generation"`
	Cells      []contactSheetCell `json:"cells"`
}

func addContactSheetTool(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "render_contact_sheet", Description: "Render up to sixteen frames into one PNG for efficient visual review."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input contactSheetInput) (*mcp.CallToolResult, contactSheetOutput, error) {
			return renderContactSheet(ctx, client, input)
		})
}

func renderContactSheet(ctx context.Context, client *bridge.Client, input contactSheetInput) (*mcp.CallToolResult, contactSheetOutput, error) {
	if len(input.Frames) < 1 || len(input.Frames) > 16 {
		return nil, contactSheetOutput{}, errors.New("frames must contain between 1 and 16 entries")
	}
	if input.Columns == 0 {
		input.Columns = min(4, len(input.Frames))
	}
	if input.CellWidth == 0 {
		input.CellWidth = 320
	}
	if input.CellHeight == 0 {
		input.CellHeight = 320
	}
	if input.Columns < 1 || input.Columns > 4 || input.CellWidth < 1 || input.CellWidth > 600 || input.CellHeight < 1 || input.CellHeight > 600 {
		return nil, contactSheetOutput{}, errors.New("columns must be 1..4 and cell dimensions must be 1..600")
	}
	for _, frame := range input.Frames {
		if frame < 0 {
			return nil, contactSheetOutput{}, errors.New("frames must be non-negative")
		}
	}

	previews := make([]protocol.PreviewResult, 0, len(input.Frames))
	images := make([]*image.NRGBA, 0, len(input.Frames))
	for _, frame := range input.Frames {
		preview, err := client.RenderPreview(ctx, protocol.RenderPreviewParams{
			Frame: frame, MaxWidth: input.CellWidth, MaxHeight: input.CellHeight,
			ObjectID: input.ObjectID, ApplyEffects: input.ApplyEffects,
		})
		if err != nil {
			return nil, contactSheetOutput{}, err
		}
		if len(previews) > 0 && (preview.SessionID != previews[0].SessionID || preview.Generation != previews[0].Generation) {
			return nil, contactSheetOutput{}, errors.New("AviUtl2 context changed while rendering the contact sheet")
		}
		img, err := previewImage(preview)
		if err != nil {
			return nil, contactSheetOutput{}, err
		}
		previews = append(previews, preview)
		images = append(images, img)
	}

	pngData, width, height, cells, err := composeContactSheetLabeled(images, input.Frames, input.Columns, input.LabelFrames)
	if err != nil {
		return nil, contactSheetOutput{}, err
	}
	content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: pngData, MIMEType: "image/png"}}}
	output := contactSheetOutput{Width: width, Height: height, SessionID: previews[0].SessionID, Generation: previews[0].Generation, Cells: cells}
	return content, output, nil

}

func composeContactSheet(images []*image.NRGBA, frames []int, columns int) ([]byte, int, int, []contactSheetCell, error) {
	return composeContactSheetLabeled(images, frames, columns, false)
}

func composeContactSheetLabeled(images []*image.NRGBA, frames []int, columns int, labels bool) ([]byte, int, int, []contactSheetCell, error) {
	if len(images) == 0 || len(images) != len(frames) || columns < 1 {
		return nil, 0, 0, nil, errors.New("invalid contact sheet inputs")
	}
	const gap = 4
	cellWidth, cellHeight := 1, 1
	for _, img := range images {
		if img == nil {
			return nil, 0, 0, nil, errors.New("contact sheet contains a nil image")
		}
		cellWidth = max(cellWidth, img.Bounds().Dx())
		cellHeight = max(cellHeight, img.Bounds().Dy())
	}
	rows := (len(images) + columns - 1) / columns
	width := columns*cellWidth + (columns+1)*gap
	height := rows*cellHeight + (rows+1)*gap
	sheet := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(sheet, sheet.Bounds(), &image.Uniform{C: color.NRGBA{R: 32, G: 32, B: 32, A: 255}}, image.Point{}, draw.Src)
	cells := make([]contactSheetCell, 0, len(images))
	for index, img := range images {
		column, row := index%columns, index/columns
		x := gap + column*cellWidth + (cellWidth-img.Bounds().Dx())/2
		y := gap + row*cellHeight + (cellHeight-img.Bounds().Dy())/2
		destination := image.Rect(x, y, x+img.Bounds().Dx(), y+img.Bounds().Dy())
		draw.Draw(sheet, destination, img, img.Bounds().Min, draw.Src)
		if labels {
			drawFrameLabel(sheet, x+3, y+3, frames[index])
		}
		cells = append(cells, contactSheetCell{Frame: frames[index], X: x, Y: y, Width: img.Bounds().Dx(), Height: img.Bounds().Dy()})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, sheet); err != nil {
		return nil, 0, 0, nil, fmt.Errorf("encode contact sheet: %w", err)
	}
	return encoded.Bytes(), width, height, cells, nil
}

var tinyDigits = [10][7]byte{
	{0x7, 0x5, 0x5, 0x5, 0x5, 0x5, 0x7}, {0x2, 0x6, 0x2, 0x2, 0x2, 0x2, 0x7},
	{0x7, 0x1, 0x1, 0x7, 0x4, 0x4, 0x7}, {0x7, 0x1, 0x1, 0x7, 0x1, 0x1, 0x7},
	{0x5, 0x5, 0x5, 0x7, 0x1, 0x1, 0x1}, {0x7, 0x4, 0x4, 0x7, 0x1, 0x1, 0x7},
	{0x7, 0x4, 0x4, 0x7, 0x5, 0x5, 0x7}, {0x7, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1},
	{0x7, 0x5, 0x5, 0x7, 0x5, 0x5, 0x7}, {0x7, 0x5, 0x5, 0x7, 0x1, 0x1, 0x7},
}

func drawFrameLabel(img *image.NRGBA, x, y, frame int) {
	label := fmt.Sprintf("%d", frame)
	background := image.Rect(x-2, y-2, x+len(label)*4+2, y+9)
	draw.Draw(img, background, &image.Uniform{C: color.NRGBA{A: 190}}, image.Point{}, draw.Over)
	for index, ch := range label {
		digit := tinyDigits[int(ch-'0')]
		for row, bits := range digit {
			for column := 0; column < 3; column++ {
				if bits&(1<<(2-column)) != 0 {
					img.SetNRGBA(x+index*4+column, y+row, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
		}
	}
}
