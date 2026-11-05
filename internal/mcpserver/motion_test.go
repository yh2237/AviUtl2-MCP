package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

// These tests check our serialization contract, not the host's interpolation.
// Host-version-specific sampled-value/track-info measurements are separate.
func TestBuildTrackValue(t *testing.T) {
	cases := []struct {
		name string
		in   motionTrack
		want string
	}{
		{"linear default", motionTrack{Item: "X", From: -300, To: 300}, "-300,300,直線移動,0"},
		{"smooth", motionTrack{Item: "Y", From: -100, To: 100, Mode: "smooth"}, "-100,100,補間移動,0"},
		{"instant", motionTrack{Item: "X", From: 0, To: 400, Mode: "instant"}, "0,400,瞬間移動,0"},
		{"random", motionTrack{Item: "X", From: 1, To: 2, Mode: "random"}, "1,2,ランダム移動,0"},
		{"constant is scalar", motionTrack{Item: "X", From: 5, To: 5, Mode: "constant"}, "5"},
		{"constant zero", motionTrack{Item: "X", Mode: "constant"}, "0"},
		{"precision", motionTrack{Item: "拡大率", From: 100.001, To: 100.004}, "100.001,100.004,直線移動,0"},
		{"custom effect", motionTrack{Item: "X", Effect: "映像再生", From: 1, To: 2}, "1,2,直線移動,0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildTrackValue(c.in)
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestBuildTrackValueErrors(t *testing.T) {
	for _, track := range []motionTrack{
		{Item: ""}, {Item: " \t"},
		{Item: "X", Mode: "warp"},
		{Item: "X", From: 0, To: 1, Mode: "constant"},
		{Item: "X", From: math.NaN()},
		{Item: "X", From: math.Inf(-1)},
		{Item: "X", To: math.Inf(1)},
	} {
		if _, err := buildTrackValue(track); err == nil {
			t.Errorf("accepted invalid track: %+v", track)
		}
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
	for _, tracks := range [][]motionTrack{
		nil,
		make([]motionTrack, 101),
		{{Item: "X"}, {Item: "X", Effect: "標準描画:0"}},
		{{Item: "X", Effect: "ぼかし:-1"}},
	} {
		if _, err := trackProperties(tracks); err == nil {
			t.Errorf("accepted invalid tracks: %+v", tracks)
		}
	}
	if _, err := trackProperties([]motionTrack{{Item: "範囲", Effect: "ぼかし"}, {Item: "範囲", Effect: "ぼかし:1"}}); err != nil {
		t.Fatalf("different effect occurrences must remain distinct: %v", err)
	}
	tracks := make([]motionTrack, 100)
	for i := range tracks {
		tracks[i].Item = fmt.Sprintf("item%d", i)
	}
	if _, err := trackProperties(tracks); err != nil {
		t.Fatalf("100 distinct tracks should be accepted: %v", err)
	}
}

// A framed fake bridge records the full request, including expected context.
// It never connects to or mutates a live AviUtl2 instance.
type motionBridgeFixture struct {
	context protocol.Context
	values  protocol.ObjectValuesResult
	objects []protocol.Object
	media   protocol.MediaInfo
	items   map[string][]protocol.EffectItem
	mu      sync.Mutex
	calls   []protocol.Request
}

func newMotionBridgeFixture() *motionBridgeFixture {
	current := protocol.Context{SessionID: "motion-session", Generation: 3, SceneID: 0, LayerMax: 2, FrameMax: 100}
	object := protocol.Object{ID: 7, Layer: 2, Start: 10, End: 19, Sections: []int{10}}
	return &motionBridgeFixture{
		context: current,
		objects: []protocol.Object{object},
		values: protocol.ObjectValuesResult{Context: current, Object: object, Effects: []protocol.ObjectEffectValues{{
			Name: "標準描画", Enabled: true,
			Items: []protocol.ObjectItemValue{{Name: "X", Type: 2, Track: &protocol.TrackInfo{}}, {Name: "Y", Type: 2, Track: &protocol.TrackInfo{}}},
		}}},
		media: protocol.MediaInfo{Supported: true, VideoTrackCount: 1},
		items: map[string][]protocol.EffectItem{
			"標準描画": {{Name: "X", Type: 2}, {Name: "Y", Type: 2}},
			"テキスト": {{Name: "サイズ", Type: 2}, {Name: "テキスト", Type: 6}},
		},
	}
}

func (f *motionBridgeFixture) requests() []protocol.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Request(nil), f.calls...)
}

func (f *motionBridgeFixture) start(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				for {
					payload, err := protocol.ReadFrame(conn)
					if err != nil {
						return
					}
					var request protocol.Request
					if json.Unmarshal(payload, &request) != nil {
						return
					}
					f.mu.Lock()
					f.calls = append(f.calls, request)
					f.mu.Unlock()
					result, callErr := f.answer(request)
					response := protocol.Response{ID: request.ID, Version: protocol.Version}
					if callErr != nil {
						response.Error = &protocol.Error{Code: "HOST_ERROR", Message: callErr.Error()}
					} else {
						response.Result, _ = json.Marshal(result)
					}
					encoded, _ := json.Marshal(response)
					if protocol.WriteFrame(conn, encoded) != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func (f *motionBridgeFixture) answer(request protocol.Request) (any, error) {
	switch request.Method {
	case "get_context":
		return f.context, nil
	case "inspect_object_values":
		return f.values, nil
	case "inspect_timeline":
		return protocol.TimelineResult{Context: f.context, Objects: f.objects}, nil
	case "inspect_objects":
		var params protocol.InspectObjectsParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, err
		}
		objects := []protocol.Object{}
		for _, id := range params.ObjectIDs {
			if id == 42 {
				objects = append(objects, protocol.Object{ID: 42, Layer: 1, Start: 30, End: 39})
			}
			for _, object := range f.objects {
				if object.ID == id {
					objects = append(objects, object)
				}
			}
		}
		return protocol.ObjectsResult{Context: f.context, Objects: objects}, nil
	case "list_effect_items":
		var params protocol.ListEffectItemsParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, err
		}
		return protocol.EffectItemsResult{Effect: params.Effect, Items: f.items[params.Effect]}, nil
	case "preflight_media":
		return f.media, nil
	case "execute_batch":
		var params protocol.ExecuteBatchParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, err
		}
		results := []protocol.OperationResult{}
		id := uint64(7)
		for index, op := range params.Operations {
			if op.Op == "add_text" || op.Op == "add_media" {
				id = 42
			}
			results = append(results, protocol.OperationResult{Index: index, Op: op.Op, ObjectID: ptr(id), Changed: true})
		}
		return protocol.MutationResult{Context: f.context, Results: results}, nil
	}
	return nil, fmt.Errorf("unexpected method %s", request.Method)
}

func motionToolArgs(add, dry bool) map[string]any {
	args := map[string]any{
		"session_id": "motion-session", "generation": 3, "scene_id": 0,
		"tracks": []motionTrack{{Item: "X", From: 100.001, To: 100.004}}, "dry_run": dry,
	}
	if add {
		args["text"], args["layer"], args["frame"], args["length"] = "hello", 1, 30, 10
	} else {
		args["object_id"] = 7
	}
	return args
}

func callMotionTool(t *testing.T, fixture *motionBridgeFixture, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	session := connectTestSession(t, fixture.start(t))
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertMotionReadOnly(t *testing.T, fixture *motionBridgeFixture) {
	t.Helper()
	for _, request := range fixture.requests() {
		switch request.Method {
		case "get_context", "inspect_object_values", "inspect_objects", "inspect_timeline", "list_effect_items", "preflight_media":
		default:
			t.Errorf("validation sent mutating/unexpected RPC %q", request.Method)
		}
	}
}

func motionResultPlan(t *testing.T, result *mcp.CallToolResult) operationPlanOutput {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool failed: %+v", result.Content)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var plan operationPlanOutput
	if err := json.Unmarshal(encoded, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestMotionToolsDryRunNeverMutate(t *testing.T) {
	for _, name := range []string{"set_motion", "add_motion"} {
		t.Run(name, func(t *testing.T) {
			fixture := newMotionBridgeFixture()
			plan := motionResultPlan(t, callMotionTool(t, fixture, name, motionToolArgs(name == "add_motion", true)))
			assertMotionReadOnly(t, fixture)
			wantOps := 1
			if name == "add_motion" {
				wantOps = 2
			}
			if !plan.DryRun || plan.Mutation != nil || len(plan.Operations) != wantOps {
				t.Fatalf("unexpected dry-run plan: %+v", plan)
			}
		})
	}
}

func TestSetMotionRejectsUnsafeTargetsBeforeMutation(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*motionBridgeFixture, map[string]any)
	}{
		{"stale object context", "context changed", func(f *motionBridgeFixture, _ map[string]any) { f.values.Context.Generation++ }},
		{"stale batch context", "context changed", func(f *motionBridgeFixture, _ map[string]any) { f.context.Generation++ }},
		{"missing effect", "does not contain effect", func(f *motionBridgeFixture, _ map[string]any) { f.values.Effects = nil }},
		{"non-number", "no numeric track", func(f *motionBridgeFixture, _ map[string]any) { f.values.Effects[0].Items[0].Type = 6 }},
		{"missing track metadata", "no numeric track", func(f *motionBridgeFixture, _ map[string]any) { f.values.Effects[0].Items[0].Track = nil }},
		{"locked effect", "locked", func(f *motionBridgeFixture, _ map[string]any) { f.values.Effects[0].Locked = true }},
		{"sections", "intermediate sections", func(f *motionBridgeFixture, _ map[string]any) { f.values.Object.Sections = []int{10, 15} }},
		{"duplicate", "duplicate motion track", func(_ *motionBridgeFixture, args map[string]any) {
			args["tracks"] = []motionTrack{{Item: "X"}, {Item: "X", Effect: "標準描画:0"}}
		}},
		{"missing occurrence", "does not contain effect", func(_ *motionBridgeFixture, args map[string]any) {
			args["tracks"] = []motionTrack{{Item: "X", Effect: "標準描画:1"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture, args := newMotionBridgeFixture(), motionToolArgs(false, false)
			c.change(fixture, args)
			result := callMotionTool(t, fixture, "set_motion", args)
			encoded, _ := json.Marshal(result.Content)
			if !result.IsError || !strings.Contains(string(encoded), c.want) {
				t.Fatalf("want rejection containing %q; got %s", c.want, encoded)
			}
			assertMotionReadOnly(t, fixture)
		})
	}
}

func TestAddMotionSingleBatchReferencesCreatedObject(t *testing.T) {
	for _, media := range []bool{false, true} {
		t.Run(fmt.Sprintf("media=%v", media), func(t *testing.T) {
			fixture, args := newMotionBridgeFixture(), motionToolArgs(true, false)
			if media {
				delete(args, "text")
				args["file"] = `C:\media\clip.mp4`
			}
			plan := motionResultPlan(t, callMotionTool(t, fixture, "add_motion", args))
			if plan.Mutation == nil || len(plan.Objects) != 1 || plan.Objects[0].ID != 42 {
				t.Fatalf("missing mutation/read-back: %+v", plan)
			}
			batches, preflights := 0, 0
			for _, request := range fixture.requests() {
				if request.Method == "preflight_media" {
					preflights++
					var params protocol.PreflightMediaParams
					if err := json.Unmarshal(request.Params, &params); err != nil || !params.Strict || params.File != args["file"] {
						t.Fatalf("wrong media preflight: %s, %v", request.Params, err)
					}
				}
				if request.Method != "execute_batch" {
					continue
				}
				batches++
				if request.Context == nil || request.Context.SessionID != "motion-session" || request.Context.Generation == nil || *request.Context.Generation != 3 || request.Context.SceneID == nil || *request.Context.SceneID != 0 {
					t.Fatalf("batch lost expected context: %+v", request.Context)
				}
				var params protocol.ExecuteBatchParams
				if err := json.Unmarshal(request.Params, &params); err != nil {
					t.Fatal(err)
				}
				wantOp := "add_text"
				if media {
					wantOp = "add_media"
				}
				if len(params.Operations) != 2 || params.Operations[0].Op != wantOp {
					t.Fatalf("unexpected creation plan: %+v", params)
				}
				update := params.Operations[1]
				if update.Op != "update_object" || update.ObjectID != 0 || update.ResultRef == nil || *update.ResultRef != 0 || len(update.Properties) != 1 || update.Properties[0].Value != "100.001,100.004,直線移動,0" {
					t.Fatalf("invalid created-object reference/track: %+v", update)
				}
				var raw struct {
					Operations []map[string]json.RawMessage `json:"operations"`
				}
				_ = json.Unmarshal(request.Params, &raw)
				if string(raw.Operations[1]["result_ref"]) != "0" || raw.Operations[1]["object_id"] != nil {
					t.Fatalf("zero result_ref must survive omitempty: %s", request.Params)
				}
			}
			if batches != 1 || (media && preflights == 0) || (!media && preflights != 0) {
				t.Fatalf("batch/preflight counts: %d/%d", batches, preflights)
			}
		})
	}
}

func TestSetMotionUsesRequestedEffectOccurrence(t *testing.T) {
	fixture := newMotionBridgeFixture()
	fixture.values.Effects = []protocol.ObjectEffectValues{
		{Name: "ぼかし", Locked: true},
		{Name: "ぼかし", Items: []protocol.ObjectItemValue{{Name: "範囲", Type: 2, Track: &protocol.TrackInfo{}}}},
	}
	fixture.items["ぼかし"] = []protocol.EffectItem{{Name: "範囲", Type: 2}}
	args := motionToolArgs(false, false)
	args["tracks"] = []motionTrack{{Effect: "ぼかし:1", Item: "範囲", From: 5, To: 5, Mode: "constant"}}
	plan := motionResultPlan(t, callMotionTool(t, fixture, "set_motion", args))
	if plan.Mutation == nil || len(plan.Objects) != 1 || plan.Objects[0].ID != 7 {
		t.Fatalf("missing mutation/read-back: %+v", plan)
	}
	batches := 0
	for _, request := range fixture.requests() {
		if request.Method != "execute_batch" {
			continue
		}
		batches++
		var params protocol.ExecuteBatchParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatal(err)
		}
		if len(params.Operations) != 1 {
			t.Fatalf("unexpected plan: %+v", params)
		}
		op := params.Operations[0]
		if op.ObjectID != 7 || op.ResultRef != nil || len(op.Properties) != 1 || op.Properties[0].Effect != "ぼかし:1" || op.Properties[0].Value != "5" {
			t.Fatalf("wrong target occurrence/constant serialization: %+v", op)
		}
	}
	if batches != 1 {
		t.Fatalf("want one batch, got %d", batches)
	}
}

func TestAddMotionAllowsNumericTextItems(t *testing.T) {
	fixture, args := newMotionBridgeFixture(), motionToolArgs(true, true)
	args["tracks"] = []motionTrack{{Effect: "テキスト:0", Item: "サイズ", From: 12, To: 36}}
	plan := motionResultPlan(t, callMotionTool(t, fixture, "add_motion", args))
	if len(plan.Operations) != 2 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	assertMotionReadOnly(t, fixture)
}

func TestAddMotionRejectsUnsafeCreationBeforeMutation(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*motionBridgeFixture, map[string]any)
	}{
		{"absent effect", "cannot create effect", func(_ *motionBridgeFixture, a map[string]any) {
			a["tracks"] = []motionTrack{{Effect: "ぼかし", Item: "範囲"}}
		}},
		{"non-number", "no numeric track", func(_ *motionBridgeFixture, a map[string]any) {
			a["tracks"] = []motionTrack{{Effect: "テキスト", Item: "テキスト"}}
		}},
		{"audio only", "supported image/video", func(f *motionBridgeFixture, a map[string]any) {
			delete(a, "text")
			a["file"] = `C:\media\song.wav`
			f.media.VideoTrackCount = 0
			f.media.AudioTrackCount = 1
		}},
		{"unsupported media", "supported image/video", func(f *motionBridgeFixture, a map[string]any) {
			delete(a, "text")
			a["file"] = `C:\media\bad.mp4`
			f.media.Supported = false
		}},
		{"text and media", "exactly one", func(_ *motionBridgeFixture, a map[string]any) { a["file"] = `C:\media\clip.mp4` }},
		{"frame overflow", "host frame range", func(_ *motionBridgeFixture, a map[string]any) { a["frame"] = math.MaxInt32 }},
		{"length overflow", "host frame range", func(_ *motionBridgeFixture, a map[string]any) { a["length"], a["frame"] = int64(math.MaxInt32)+1, 0 }},
		{"layer overflow", "host frame range", func(_ *motionBridgeFixture, a map[string]any) { a["layer"] = int64(math.MaxInt32) + 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture, args := newMotionBridgeFixture(), motionToolArgs(true, false)
			c.change(fixture, args)
			result := callMotionTool(t, fixture, "add_motion", args)
			encoded, _ := json.Marshal(result.Content)
			if !result.IsError || !strings.Contains(string(encoded), c.want) {
				t.Fatalf("want rejection containing %q; got %s", c.want, encoded)
			}
			assertMotionReadOnly(t, fixture)
		})
	}
}
