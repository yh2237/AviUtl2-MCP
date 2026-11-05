//go:build ignore

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type smoke struct {
	ctx        context.Context
	session    *mcp.ClientSession
	current    protocol.Context
	serverPath string
}

func (s *smoke) reconnect() error {
	if err := s.session.Close(); err != nil {
		return err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "aviutl2-live-smoke", Version: "dev"}, nil)
	session, err := client.Connect(s.ctx, &mcp.CommandTransport{Command: exec.Command(s.serverPath)}, nil)
	if err != nil {
		return err
	}
	s.session = session
	fmt.Println("PASS MCP process restart and reconnect")
	return nil
}

func (s *smoke) call(name string, args any, out any) error {
	result, err := s.session.CallTool(s.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if result.IsError {
		data, _ := json.Marshal(result.Content)
		return fmt.Errorf("%s: %s", name, data)
	}
	if out != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s decode: %w", name, err)
		}
	}
	fmt.Println("PASS", name)
	return nil
}

func (s *smoke) refresh() error {
	var out struct {
		Context protocol.Context `json:"context"`
	}
	if err := s.call("get_context", map[string]any{}, &out); err != nil {
		return err
	}
	s.current = out.Context
	return nil
}

func (s *smoke) args(values map[string]any) map[string]any {
	values["session_id"] = s.current.SessionID
	values["generation"] = s.current.Generation
	values["scene_id"] = s.current.SceneID
	return values
}

func (s *smoke) restore(file string) error {
	if err := s.refresh(); err != nil {
		return err
	}
	var out protocol.ProjectResult
	if err := s.call("project", s.args(map[string]any{"operation": "open", "file": file, "timeout_ms": 60000}), &out); err != nil {
		return err
	}
	s.current = out.Context
	fmt.Println("RESTORED", file)
	return nil
}

func (s *smoke) edits(dir, outputPlugin string) (returnErr error) {
	prefix := filepath.Join(dir, "aviutl2-live-"+time.Now().Format("20060102-150405"))
	backup := prefix + "-original.aup2"
	fixture := prefix + "-fixture.aup2"
	var saved protocol.ProjectResult
	if err := s.call("project", s.args(map[string]any{"operation": "save", "file": backup, "timeout_ms": 60000}), &saved); err != nil {
		return err
	}
	if info, err := os.Stat(backup); err != nil || info.Size() == 0 {
		return fmt.Errorf("original snapshot is missing: %v", err)
	}
	fmt.Println("ORIGINAL SNAPSHOT", backup)
	defer func() {
		if err := s.restore(backup); err != nil {
			returnErr = fmt.Errorf("test error: %v; restore failed: %w; original snapshot: %s", returnErr, err, backup)
		}
	}()
	var project protocol.ProjectResult
	if err := s.call("project", s.args(map[string]any{"operation": "create", "width": 320, "height": 180, "rate": 30, "scale": 1, "sample_rate": 48000, "background": "000000ff"}), &project); err != nil {
		return err
	}
	s.current = project.Context
	if s.current.Width != 320 || s.current.Height != 180 {
		return fmt.Errorf("project settings were not applied: %+v", s.current)
	}
	var mutation struct {
		Mutation protocol.MutationResult `json:"mutation"`
	}
	if err := s.call("add_text", s.args(map[string]any{"text": "MCP実機テスト", "layer": 0, "frame": 0, "length": 4, "size": 24}), &mutation); err != nil {
		return err
	}
	s.current = mutation.Mutation.Context
	if len(mutation.Mutation.Results) != 1 || mutation.Mutation.Results[0].ObjectID == nil {
		return fmt.Errorf("add_text did not return an object ID")
	}
	id := *mutation.Mutation.Results[0].ObjectID
	var object struct {
		Objects protocol.ObjectsResult `json:"objects"`
	}
	if err := s.call("inspect_object", map[string]any{"object_id": id, "include_effects": true}, &object); err != nil {
		return err
	}
	if len(object.Objects.Objects) != 1 || object.Objects.Objects[0].NativeID == "" || object.Objects.Objects[0].NativeID == "0" || len(object.Objects.Objects[0].Effects) == 0 || object.Objects.Objects[0].Effects[0].NativeID == "" {
		return fmt.Errorf("native object/effect IDs missing: %+v", object)
	}
	fmt.Printf("NATIVE IDs object=%s effect=%s\n", object.Objects.Objects[0].NativeID, object.Objects.Objects[0].Effects[0].NativeID)
	flags := s.args(map[string]any{"object_ids": []uint64{id}, "flags": map[string]any{"enable_camera": false, "clipping_object": true}, "dry_run": true})
	if err := s.call("set_object_flags", flags, nil); err != nil {
		return err
	}
	flags["dry_run"] = false
	if err := s.call("set_object_flags", flags, nil); err != nil {
		return err
	}
	if err := s.call("inspect_object", map[string]any{"object_id": id}, &object); err != nil {
		return err
	}
	if len(object.Objects.Objects) != 1 || object.Objects.Objects[0].Flags.EnableCamera || !object.Objects.Objects[0].Flags.ClippingObject {
		return fmt.Errorf("flags were not applied: %+v", object.Objects.Objects)
	}
	// Remove clipping again to make the preview/output visible.
	if err := s.call("set_object_flags", s.args(map[string]any{"object_ids": []uint64{id}, "flags": map[string]any{"clipping_object": false}}), nil); err != nil {
		return err
	}
	var overview protocol.SceneOverview
	if err := s.call("inspect_scene", map[string]any{"include_empty": true}, &overview); err != nil {
		return err
	}
	if overview.Context.SessionID != s.current.SessionID || overview.Scene.ObjectCount < 1 || len(overview.Layers) == 0 {
		return fmt.Errorf("inspect_scene returned an unexpected payload: %+v", overview.Scene)
	}
	fmt.Printf("SCENE OVERVIEW objects=%d layers=%d markers=%d bpm=%d\n",
		overview.Scene.ObjectCount, len(overview.Layers), len(overview.Markers), len(overview.BPM))
	var diagnostics struct {
		Bridge protocol.DiagnosticsResult `json:"bridge"`
	}
	if err := s.call("diagnose_connection", map[string]any{}, &diagnostics); err != nil {
		return err
	}
	if diagnostics.Bridge.Capabilities.LayerColor.Supported {
		if err := s.call("edit_layers", s.args(map[string]any{"operation": "color", "layer_start": 0, "layer_end": 0, "color": "#3366cc", "dry_run": true}), nil); err != nil {
			return err
		}
		if err := s.call("edit_layers", s.args(map[string]any{"operation": "color", "layer_start": 0, "layer_end": 0, "color": "3366ccff"}), nil); err != nil {
			return err
		}
		var layers struct {
			Layers []protocol.Layer `json:"layers"`
		}
		if err := s.call("find_layers", map[string]any{"layer_start": 0, "layer_filter": "all"}, &layers); err != nil {
			return err
		}
		if len(layers.Layers) == 0 || layers.Layers[0].Color == nil || *layers.Layers[0].Color != "3366ccff" {
			return fmt.Errorf("layer color was not applied: %+v", layers.Layers)
		}
		if err := s.call("edit_layers", s.args(map[string]any{"operation": "color", "layer_start": 0, "layer_end": 0, "color": ""}), nil); err != nil {
			return err
		}
		fmt.Println("PASS layer colors")
	} else {
		fmt.Printf("SKIP layer colors: AviUtl2 %s (needs %s or newer)\n",
			diagnostics.Bridge.HostVersionText, diagnostics.Bridge.Capabilities.LayerColor.RequiresVersionText)
	}
	if err := s.call("render_preview", map[string]any{"frame": 0, "max_width": 320, "max_height": 180}, nil); err != nil {
		return err
	}
	mainScene := s.current.SceneID
	var scenes protocol.ScenesResult
	if err := s.call("scenes", s.args(map[string]any{"operation": "create", "name": "MCP smoke sub", "label": "MCP test"}), &scenes); err != nil {
		return err
	}
	s.current = scenes.Context
	if s.current.Width != 320 || s.current.Height != 180 || s.current.Rate != 30 || len(scenes.Scenes) < 2 {
		return fmt.Errorf("scene inheritance/list failed: %+v", scenes)
	}
	stale, err := s.session.CallTool(s.ctx, &mcp.CallToolParams{Name: "inspect_object", Arguments: map[string]any{"object_id": id}})
	if err != nil || !stale.IsError {
		return fmt.Errorf("expired object ID was not rejected: %v", err)
	}
	fmt.Println("PASS expired object ID rejected")
	if err := s.call("scenes", s.args(map[string]any{"operation": "select", "target_scene_id": mainScene}), &scenes); err != nil {
		return err
	}
	s.current = scenes.Context
	if err := s.call("scenes", s.args(map[string]any{"operation": "select", "name": "MCP smoke sub"}), &scenes); err != nil {
		return err
	}
	s.current = scenes.Context
	if err := s.call("scenes", s.args(map[string]any{"operation": "select", "target_scene_id": mainScene}), &scenes); err != nil {
		return err
	}
	s.current = scenes.Context
	if err := s.call("project", s.args(map[string]any{"operation": "save", "file": fixture}), &project); err != nil {
		return err
	}
	if err := s.call("project", s.args(map[string]any{"operation": "open", "file": fixture}), &project); err != nil {
		return err
	}
	s.current = project.Context
	if err := s.call("scenes", map[string]any{"operation": "list"}, &scenes); err != nil {
		return err
	}
	if len(scenes.Scenes) < 2 {
		return fmt.Errorf("project round-trip lost scenes")
	}
	if outputPlugin != "" {
		output := prefix + "-output.wav"
		if outputPlugin == "PNGファイル出力" {
			output = prefix + "-output.png"
		}
		var outputResult struct {
			Status *protocol.OutputStatusResult `json:"status"`
		}
		if err := s.call("output", s.args(map[string]any{"operation": "start", "file": output, "output_plugin": outputPlugin, "save_project_file": prefix + "-before-output.aup2"}), &outputResult); err != nil {
			return err
		}
		if outputResult.Status == nil || outputResult.Status.Job == nil {
			return fmt.Errorf("output job missing")
		}
		jobID := outputResult.Status.Job.ID
		if err := s.reconnect(); err != nil {
			return err
		}
		if err := s.call("output", map[string]any{"operation": "status"}, &outputResult); err != nil {
			return err
		}
		if outputResult.Status == nil || outputResult.Status.Job == nil || outputResult.Status.Job.ID != jobID {
			return fmt.Errorf("output job lost after reconnect")
		}
		deadline := time.Now().Add(60 * time.Second)
		for outputResult.Status.Job.State != "ended" && time.Now().Before(deadline) {
			time.Sleep(300 * time.Millisecond)
			if err := s.call("output", map[string]any{"operation": "status", "job_id": jobID}, &outputResult); err != nil {
				return err
			}
		}
		if outputResult.Status.Job.State != "ended" {
			return fmt.Errorf("output did not end: %+v", outputResult.Status)
		}
		if info, err := os.Stat(output); err != nil || info.Size() == 0 {
			return fmt.Errorf("output file missing/empty: %v", err)
		}
		fmt.Println("OUTPUT FILE", output)
		if filepath.Ext(output) == ".wav" {
			data, err := os.ReadFile(output)
			if err != nil || len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
				return fmt.Errorf("output is not a valid RIFF/WAVE file: %v", err)
			}
			var sampleRate, dataBytes uint32
			for offset := 12; offset+8 <= len(data); {
				length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
				if length < 0 || length > len(data)-offset-8 {
					return fmt.Errorf("invalid WAV chunk length")
				}
				if string(data[offset:offset+4]) == "fmt " && length >= 16 {
					sampleRate = binary.LittleEndian.Uint32(data[offset+12 : offset+16])
				}
				if string(data[offset:offset+4]) == "data" {
					dataBytes = uint32(length)
				}
				offset += 8 + length + length%2
			}
			if sampleRate != 48000 || dataBytes == 0 {
				return fmt.Errorf("unexpected WAV data: rate=%d bytes=%d", sampleRate, dataBytes)
			}
			fmt.Printf("PASS WAV contents: sample_rate=%d data_bytes=%d total_bytes=%d\n", sampleRate, dataBytes, len(data))
		}
	}
	fmt.Println("EDIT SMOKE PASSED; restoring original snapshot")
	return nil
}

func run() error {
	serverPath := flag.String("server", `C:\ProgramData\aviutl2\Plugin\AviUtl2-MCP\aviutl2-mcp.exe`, "MCP server executable")
	edit := flag.Bool("edit", false, "exercise mutations on a temporary project, then restore original")
	dir := flag.String("work-dir", os.TempDir(), "existing directory for project snapshots and outputs")
	plugin := flag.String("output-plugin", "", "optional output plugin to exercise")
	restore := flag.String("restore", "", "restore a previous snapshot instead of running editing tests")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "aviutl2-live-smoke", Version: "dev"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(*serverPath)}, nil)
	if err != nil {
		return err
	}
	s := &smoke{ctx: ctx, session: session, serverPath: *serverPath}
	defer func() { s.session.Close() }()
	if err := s.call("ping", map[string]any{}, nil); err != nil {
		return err
	}
	if err := s.refresh(); err != nil {
		return err
	}
	data, _ := json.Marshal(s.current)
	fmt.Println("CONTEXT", string(data))
	var scenes protocol.ScenesResult
	if err := s.call("scenes", map[string]any{"operation": "list"}, &scenes); err != nil {
		return err
	}
	data, _ = json.Marshal(scenes.Scenes)
	fmt.Println("SCENES", string(data))
	var plugins struct {
		Plugins []protocol.ModuleInfo `json:"plugins"`
	}
	if err := s.call("output", map[string]any{"operation": "list_plugins"}, &plugins); err != nil {
		return err
	}
	data, _ = json.Marshal(plugins.Plugins)
	fmt.Println("OUTPUT PLUGINS", string(data))
	if *restore != "" {
		return s.restore(*restore)
	}
	if *edit {
		return s.edits(*dir, *plugin)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL", err)
		os.Exit(1)
	}
}
