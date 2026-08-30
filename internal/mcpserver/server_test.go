package mcpserver

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"testing"

	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

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
