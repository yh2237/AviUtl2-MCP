package mcpserver

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

func TestNewBuildsAllToolSchemas(t *testing.T) {
	client := bridge.NewClient("127.0.0.1:1", time.Millisecond)
	if server := New(client, "test"); server == nil {
		t.Fatal("New returned nil")
	}
}

func TestPreviewPNG(t *testing.T) {
	rgba := []byte{
		255, 0, 0, 255,
		0, 255, 0, 255,
	}
	data, err := previewPNG(protocol.PreviewResult{
		Width: 2, Height: 1, RGBA: base64.StdEncoding.EncodeToString(rgba),
	})
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != 2 || image.Bounds().Dy() != 1 {
		t.Fatalf("unexpected dimensions: %v", image.Bounds())
	}
}

func TestOrderMovesResolvesTransientDependency(t *testing.T) {
	moves := []plannedMove{
		moveFor(protocol.Object{ID: 1, Layer: 0, Start: 0, End: 9}, 0, 10),
		moveFor(protocol.Object{ID: 2, Layer: 0, Start: 10, End: 19}, 0, 20),
	}
	ordered, err := orderMoves(moves)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].object.ID != 2 || ordered[1].object.ID != 1 {
		t.Fatalf("unexpected move order: %d, %d", ordered[0].object.ID, ordered[1].object.ID)
	}
}

func TestOrderMovesRejectsCycle(t *testing.T) {
	moves := []plannedMove{
		moveFor(protocol.Object{ID: 1, Layer: 0, Start: 0, End: 9}, 0, 10),
		moveFor(protocol.Object{ID: 2, Layer: 0, Start: 10, End: 19}, 0, 0),
	}
	if _, err := orderMoves(moves); err == nil {
		t.Fatal("orderMoves accepted a cyclic swap")
	}
}

func TestComposeContactSheet(t *testing.T) {
	first := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	second := image.NewNRGBA(image.Rect(0, 0, 1, 2))
	data, width, height, cells, err := composeContactSheet([]*image.NRGBA{first, second}, []int{0, 30}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if width != 16 || height != 10 || len(cells) != 2 {
		t.Fatalf("unexpected sheet geometry: %dx%d, %d cells", width, height, len(cells))
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		t.Fatalf("unexpected PNG bounds: %v", decoded.Bounds())
	}
}

func TestPreviewPNGRejectsWrongPixelLength(t *testing.T) {
	_, err := previewPNG(protocol.PreviewResult{
		Width: 2, Height: 2, RGBA: base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4}),
	})
	if err == nil {
		t.Fatal("previewPNG accepted a short pixel buffer")
	}
}

func TestPreviewPNGRejectsOversizedDimensions(t *testing.T) {
	_, err := previewPNG(protocol.PreviewResult{Width: 801, Height: 1})
	if err == nil {
		t.Fatal("previewPNG accepted oversized dimensions")
	}
}

func TestValidateBatchOperationsAllowsEarlierResultReference(t *testing.T) {
	layer, frame, reference := 1, 12, 0
	operations := []protocol.BatchOperation{
		{Op: "add_text", Text: "hello", Layer: &layer, Frame: &frame, Length: 30},
		{Op: "add_effect", ResultRef: &reference, Effect: "ぼかし"},
	}
	if err := validateBatchOperations(operations); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBatchOperationsRejectsForwardReference(t *testing.T) {
	reference := 1
	err := validateBatchOperations([]protocol.BatchOperation{
		{Op: "add_effect", ResultRef: &reference, Effect: "ぼかし"},
		{Op: "delete_object", ObjectID: 1},
	})
	if err == nil {
		t.Fatal("validateBatchOperations accepted a forward reference")
	}
}
