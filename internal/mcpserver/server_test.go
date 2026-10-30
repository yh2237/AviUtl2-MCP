package mcpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
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

func TestReplaceStringCaseInsensitive(t *testing.T) {
	got := replaceString("Hello HELLO hello", "hello", "AviUtl2", false)
	if got != "AviUtl2 AviUtl2 AviUtl2" {
		t.Fatalf("unexpected replacement: %q", got)
	}
}

func TestTemplateTrackUsesCopyableAviUtl2Items(t *testing.T) {
	tests := map[string]string{
		"fade":  "透明度",
		"slide": "X",
		"zoom":  "拡大率",
	}
	for template, wantItem := range tests {
		effect, item, err := templateTrack(template)
		if err != nil {
			t.Fatalf("templateTrack(%q): %v", template, err)
		}
		if effect != "標準描画" || item != wantItem {
			t.Fatalf("templateTrack(%q) = %q/%q", template, effect, item)
		}
	}
	if _, _, err := templateTrack("unknown"); err == nil {
		t.Fatal("templateTrack accepted an unknown template")
	}
}

func TestMatchesMediaInfo(t *testing.T) {
	video := protocol.MediaInfo{Width: 1920, Height: 1080, TotalTime: 12.5, VideoTrackCount: 1, AudioTrackCount: 1}
	if !matchesMediaInfo(video, findObjectsInput{MediaType: "video", MinWidth: 1280, MinDuration: 10}) {
		t.Fatal("video did not match valid constraints")
	}
	if matchesMediaInfo(video, findObjectsInput{MediaType: "image"}) {
		t.Fatal("timed video matched image filter")
	}
	image := protocol.MediaInfo{Width: 800, Height: 600, VideoTrackCount: 1}
	if !matchesMediaInfo(image, findObjectsInput{MediaType: "image"}) {
		t.Fatal("still image did not match image filter")
	}
}

func TestBaseEffectName(t *testing.T) {
	if got := baseEffectName("標準描画:2"); got != "標準描画" {
		t.Fatalf("unexpected base effect: %q", got)
	}
	if got := baseEffectName("カスタム:名前"); got != "カスタム:名前" {
		t.Fatalf("non-index suffix was removed: %q", got)
	}
}

func TestNormalizeLayerColor(t *testing.T) {
	tests := map[string]string{
		"#1A2B3C":  "1a2b3c",
		"1a2b3c":   "1a2b3c",
		"11223344": "11223344",
		"":         "",
		"default":  "",
		"clear":    "",
		" none ":   "",
	}
	for input, want := range tests {
		got, err := normalizeLayerColor(input)
		if err != nil {
			t.Fatalf("normalizeLayerColor(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeLayerColor(%q) = %q, want %q", input, got, want)
		}
	}
	for _, invalid := range []string{"red", "12345", "1234567", "gggggg"} {
		if _, err := normalizeLayerColor(invalid); err == nil {
			t.Fatalf("normalizeLayerColor(%q) accepted an invalid color", invalid)
		}
	}
}

func TestValidateBatchOperationsAcceptsLayerColor(t *testing.T) {
	layer, color, clear := 3, "ff8800", ""
	if err := validateBatchOperations([]protocol.BatchOperation{{Op: "set_layer_state", Layer: &layer, LayerColor: &color}}); err != nil {
		t.Fatal(err)
	}
	if err := validateBatchOperations([]protocol.BatchOperation{{Op: "set_layer_state", Layer: &layer, LayerColor: &clear}}); err != nil {
		t.Fatalf("clearing a layer color was rejected: %v", err)
	}
	if err := validateBatchOperations([]protocol.BatchOperation{{Op: "set_layer_state", Layer: &layer}}); err == nil {
		t.Fatal("validateBatchOperations accepted a layer update with no state")
	}
	invalid := "nope"
	if err := validateBatchOperations([]protocol.BatchOperation{{Op: "set_layer_state", Layer: &layer, LayerColor: &invalid}}); err == nil {
		t.Fatal("validateBatchOperations accepted an invalid layer color")
	}
}

func TestLayerColorWireFormat(t *testing.T) {
	layer, clear, color := 2, "", "ff8800"
	encoded, err := json.Marshal(protocol.BatchOperation{Op: "set_layer_state", Layer: &layer, LayerColor: &clear})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"layer_color":""`) {
		t.Fatalf("clearing a layer color must keep an empty layer_color: %s", encoded)
	}
	encoded, err = json.Marshal(protocol.BatchOperation{Op: "set_layer_state", Layer: &layer, LayerColor: &color})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"layer_color":"ff8800"`) {
		t.Fatalf("unexpected layer color payload: %s", encoded)
	}
	encoded, err = json.Marshal(protocol.BatchOperation{Op: "set_layer_state", Layer: &layer, Name: &color})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "layer_color") {
		t.Fatalf("layer_color must stay absent when unset: %s", encoded)
	}
}

func TestLimitationsReportsOutdatedHost(t *testing.T) {
	values := limitations(protocol.DiagnosticsResult{
		HostVersionText: "2.1.11a",
		Capabilities: protocol.Capabilities{
			LayerColor: protocol.Capability{Supported: false, RequiresVersionText: "2.1.13"},
		},
	})
	found := false
	for _, value := range values {
		if strings.Contains(value, "2.1.13") && strings.Contains(value, "2.1.11a") {
			found = true
		}
	}
	if !found {
		t.Fatalf("limitations did not report the outdated host: %v", values)
	}
	supported := limitations(protocol.DiagnosticsResult{
		Capabilities: protocol.Capabilities{LayerColor: protocol.Capability{Supported: true}},
	})
	if len(supported) != 4 {
		t.Fatalf("supported host should not report extra limitations: %v", supported)
	}
}
