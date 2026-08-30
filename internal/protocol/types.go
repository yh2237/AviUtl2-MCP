package protocol

import "encoding/json"

const (
	Version             = 1
	DefaultAddress      = "127.0.0.1:28552"
	MaxMessageSize      = 4 << 20
	MaxBatchOperations  = 100
	MaxTimelineObjects  = 1000
	DefaultPreviewWidth = 640
)

type Request struct {
	ID      uint64           `json:"id"`
	Version int              `json:"version"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Context *ExpectedContext `json:"context,omitempty"`
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

type ExpectedContext struct {
	SessionID  string  `json:"session_id,omitempty"`
	Generation *uint64 `json:"generation,omitempty"`
	SceneID    *int    `json:"scene_id,omitempty"`
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
	EditState        int     `json:"edit_state"`
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

type Layer struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Enabled bool   `json:"enabled"`
	Locked  bool   `json:"locked"`
}

type Effect struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Locked  bool   `json:"locked"`
}

type Object struct {
	ID       uint64   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Layer    int      `json:"layer"`
	Start    int      `json:"start"`
	End      int      `json:"end"`
	Alias    string   `json:"alias,omitempty"`
	Effects  []Effect `json:"effects,omitempty"`
	Sections []int    `json:"sections,omitempty"`
}

type InspectTimelineParams struct {
	LayerStart     int  `json:"layer_start"`
	LayerEnd       int  `json:"layer_end"`
	FrameStart     int  `json:"frame_start"`
	FrameEnd       int  `json:"frame_end"`
	MaxObjects     int  `json:"max_objects,omitempty"`
	IncludeAlias   bool `json:"include_alias,omitempty"`
	IncludeEffects bool `json:"include_effects,omitempty"`
}

type TimelineResult struct {
	Context   Context  `json:"context"`
	Layers    []Layer  `json:"layers"`
	Objects   []Object `json:"objects"`
	Truncated bool     `json:"truncated"`
}

type InspectObjectParams struct {
	ObjectID       uint64 `json:"object_id"`
	IncludeAlias   bool   `json:"include_alias,omitempty"`
	IncludeEffects bool   `json:"include_effects,omitempty"`
}

type ObjectResult struct {
	Context Context `json:"context"`
	Object  Object  `json:"object"`
}

type InspectObjectsParams struct {
	ObjectIDs      []uint64 `json:"object_ids"`
	IncludeAlias   bool     `json:"include_alias,omitempty"`
	IncludeEffects bool     `json:"include_effects,omitempty"`
}

type ObjectsResult struct {
	Context Context  `json:"context"`
	Objects []Object `json:"objects"`
}

type EffectDefinition struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Flags int    `json:"flags"`
}

type EffectItem struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type ListEffectItemsParams struct {
	Effect string `json:"effect"`
}

type EffectItemsResult struct {
	Effect string       `json:"effect"`
	Items  []EffectItem `json:"items"`
}

type TrackInfo struct {
	Mode        string    `json:"mode,omitempty"`
	Parameters  []float64 `json:"parameters,omitempty"`
	Accelerate  bool      `json:"accelerate"`
	Decelerate  bool      `json:"decelerate"`
	TwoPoint    bool      `json:"two_point"`
	TimeControl bool      `json:"time_control"`
	GroupCount  int       `json:"group_count"`
	GroupIndex  int       `json:"group_index"`
	GroupName   string    `json:"group_name,omitempty"`
}

type ObjectItemValue struct {
	Name         string     `json:"name"`
	Type         int        `json:"type"`
	RawValue     string     `json:"raw_value,omitempty"`
	SampledValue *float64   `json:"sampled_value,omitempty"`
	Checked      *bool      `json:"checked,omitempty"`
	Track        *TrackInfo `json:"track,omitempty"`
}

type ObjectEffectValues struct {
	Index   int               `json:"index"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Locked  bool              `json:"locked"`
	Items   []ObjectItemValue `json:"items"`
}

type InspectObjectValuesParams struct {
	ObjectID uint64   `json:"object_id"`
	Frame    *float64 `json:"frame,omitempty"`
}

type ObjectValuesResult struct {
	Context Context              `json:"context"`
	Object  Object               `json:"object"`
	Frame   float64              `json:"frame"`
	Effects []ObjectEffectValues `json:"effects"`
}

type SelectionResult struct {
	Context            Context  `json:"context"`
	FocusObjectID      *uint64  `json:"focus_object_id,omitempty"`
	FocusObjectSection int      `json:"focus_object_section"`
	Objects            []Object `json:"objects"`
}

type PreflightMediaParams struct {
	File   string `json:"file"`
	Strict bool   `json:"strict"`
}

type MediaInfo struct {
	File            string  `json:"file"`
	Supported       bool    `json:"supported"`
	VideoTrackCount int     `json:"video_track_count"`
	AudioTrackCount int     `json:"audio_track_count"`
	TotalTime       float64 `json:"total_time"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
}

type PropertyUpdate struct {
	Effect string `json:"effect"`
	Item   string `json:"item"`
	Value  string `json:"value"`
}

type AddTextParams struct {
	Text   string  `json:"text"`
	Layer  int     `json:"layer"`
	Frame  int     `json:"frame"`
	Length int     `json:"length"`
	Size   float64 `json:"size"`
	Color  string  `json:"color"`
}

type AddMediaParams struct {
	File   string `json:"file"`
	Layer  int    `json:"layer"`
	Frame  int    `json:"frame"`
	Length int    `json:"length"`
}

type UpdateObjectParams struct {
	ObjectID   uint64           `json:"object_id"`
	Layer      *int             `json:"layer,omitempty"`
	Frame      *int             `json:"frame,omitempty"`
	Name       *string          `json:"name,omitempty"`
	Properties []PropertyUpdate `json:"properties,omitempty"`
}

type DeleteObjectParams struct {
	ObjectID uint64 `json:"object_id"`
}

type EffectMutationParams struct {
	ObjectID    uint64 `json:"object_id"`
	Effect      string `json:"effect,omitempty"`
	EffectIndex int    `json:"effect_index,omitempty"`
	Index       *int   `json:"index,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Locked      *bool  `json:"locked,omitempty"`
}

type BatchOperation struct {
	Op          string           `json:"op"`
	ObjectID    uint64           `json:"object_id,omitempty"`
	ResultRef   *int             `json:"result_ref,omitempty"`
	Text        string           `json:"text,omitempty"`
	File        string           `json:"file,omitempty"`
	Layer       *int             `json:"layer,omitempty"`
	Frame       *int             `json:"frame,omitempty"`
	Length      int              `json:"length,omitempty"`
	Size        float64          `json:"size,omitempty"`
	Color       string           `json:"color,omitempty"`
	Name        *string          `json:"name,omitempty"`
	Effect      string           `json:"effect,omitempty"`
	EffectIndex int              `json:"effect_index,omitempty"`
	Index       *int             `json:"index,omitempty"`
	Enabled     *bool            `json:"enabled,omitempty"`
	Locked      *bool            `json:"locked,omitempty"`
	Properties  []PropertyUpdate `json:"properties,omitempty"`
	Section     *int             `json:"section,omitempty"`
	FrameTo     *int             `json:"frame_to,omitempty"`
	Start       *int             `json:"start,omitempty"`
	End         *int             `json:"end,omitempty"`
	Width       *int             `json:"width,omitempty"`
	Height      *int             `json:"height,omitempty"`
	Rate        *int             `json:"rate,omitempty"`
	Scale       *int             `json:"scale,omitempty"`
	SampleRate  *int             `json:"sample_rate,omitempty"`
	Memo        *string          `json:"memo,omitempty"`
	Item        string           `json:"item,omitempty"`
}

type ExecuteBatchParams struct {
	Operations []BatchOperation `json:"operations"`
}

type OperationResult struct {
	Index    int     `json:"index"`
	Op       string  `json:"op"`
	ObjectID *uint64 `json:"object_id,omitempty"`
	Changed  bool    `json:"changed"`
}

type MutationResult struct {
	Context Context           `json:"context"`
	Results []OperationResult `json:"results"`
}

type RenderPreviewParams struct {
	Frame        int    `json:"frame"`
	MaxWidth     int    `json:"max_width"`
	MaxHeight    int    `json:"max_height"`
	ObjectID     uint64 `json:"object_id,omitempty"`
	ApplyEffects bool   `json:"apply_effects,omitempty"`
}

type PreviewResult struct {
	Frame      int    `json:"frame"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RGBA       string `json:"rgba_base64"`
	SessionID  string `json:"session_id"`
	Generation uint64 `json:"generation"`
}
