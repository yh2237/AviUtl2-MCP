package protocol

import "encoding/json"

const (
	Version        = 1
	DefaultAddress = "127.0.0.1:28552"
	MaxMessageSize = 4 << 20
)

type Request struct {
	ID      uint64          `json:"id"`
	Version int             `json:"version"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID      uint64          `json:"id"`
	Version int             `json:"version"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Retryable bool            `json:"retryable"`
	Details   json.RawMessage `json:"details,omitempty"`
}

type PingResult struct {
	Pong       bool   `json:"pong"`
	SessionID  string `json:"session_id"`
	Generation uint64 `json:"generation"`
}

type Context struct {
	SessionID        string  `json:"session_id"`
	Generation       uint64  `json:"generation"`
	SceneID          int     `json:"scene_id"`
	SceneName        string  `json:"scene_name,omitempty"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Rate             int     `json:"rate"`
	Scale            int     `json:"scale"`
	SampleRate       int     `json:"sample_rate"`
	Frame            int     `json:"frame"`
	Layer            int     `json:"layer"`
	FrameMax         int     `json:"frame_max"`
	LayerMax         int     `json:"layer_max"`
	DisplayFrame     int     `json:"display_frame_start"`
	DisplayLayer     int     `json:"display_layer_start"`
	DisplayFrameNum  int     `json:"display_frame_num"`
	DisplayLayerNum  int     `json:"display_layer_num"`
	SelectRangeStart int     `json:"select_range_start"`
	SelectRangeEnd   int     `json:"select_range_end"`
	GridBPMTempo     float32 `json:"grid_bpm_tempo"`
	GridBPMBeat      int     `json:"grid_bpm_beat"`
	GridBPMOffset    float32 `json:"grid_bpm_offset"`
}
