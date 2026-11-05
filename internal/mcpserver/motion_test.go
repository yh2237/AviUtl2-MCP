package mcpserver

import (
	"math"
	"strings"
	"testing"
)

func TestBuildTrackValue(t *testing.T) {
	cases := []struct {
		name string
		in   motionTrack
		want string
	}{
		{"linear default", motionTrack{Item: "X", From: -300, To: 300}, "-300.00,300.00,直線移動,0.00"},
		{"smooth", motionTrack{Item: "Y", From: -100, To: 100, Mode: "smooth"}, "-100.00,100.00,補間移動,0.00"},
		{"instant", motionTrack{Item: "X", From: 0, To: 400, Mode: "instant"}, "0.00,400.00,瞬間移動,0.00"},
		{"constant", motionTrack{Item: "X", From: 5, To: 5, Mode: "constant"}, "5.00,5.00,再生範囲,0.00"},
		{"custom effect", motionTrack{Item: "X", Effect: "映像再生", From: 1, To: 2}, "1.00,2.00,直線移動,0.00"},
	}
	for _, c := range cases {
		got, err := buildTrackValue(c.in)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestBuildTrackValueErrors(t *testing.T) {
	if _, err := buildTrackValue(motionTrack{Item: "", From: 0, To: 1}); err == nil {
		t.Fatal("missing item must fail")
	}
	if _, err := buildTrackValue(motionTrack{Item: "X", From: 0, To: 1, Mode: "warp"}); err == nil {
		t.Fatal("unknown mode must fail")
	}
	if _, err := buildTrackValue(motionTrack{Item: "X", From: 0, To: math.Inf(1), Mode: "linear"}); err == nil {
		t.Fatal("non-finite value must fail")
	}
}

func TestTrackPropertiesEffectDefaulting(t *testing.T) {
	props, err := trackProperties([]motionTrack{{Item: "X", From: 0, To: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(props) != 1 || props[0].Effect != "標準描画" || props[0].Item != "X" {
		t.Fatalf("unexpected props: %+v", props)
	}
	if _, err := trackProperties(nil); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty tracks must fail, got %v", err)
	}
}
