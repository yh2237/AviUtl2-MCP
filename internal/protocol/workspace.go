package protocol

type ObjectFlags struct {
	EnableGroup         bool `json:"enable_group"`
	EnableCamera        bool `json:"enable_camera"`
	ClippingObject      bool `json:"clipping_object"`
	ClippingUpperObject bool `json:"clipping_upper_object"`
}

type ObjectFlagUpdates struct {
	EnableGroup         *bool `json:"enable_group,omitempty"`
	EnableCamera        *bool `json:"enable_camera,omitempty"`
	ClippingObject      *bool `json:"clipping_object,omitempty"`
	ClippingUpperObject *bool `json:"clipping_upper_object,omitempty"`
}

func (f ObjectFlagUpdates) Empty() bool {
	return f.EnableGroup == nil && f.EnableCamera == nil && f.ClippingObject == nil && f.ClippingUpperObject == nil
}

type Scene struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

type ScenesResult struct {
	Context Context `json:"context"`
	Scenes  []Scene `json:"scenes"`
}

// Omitted settings inherit the current scene. Background is RRGGBBAA.
type SceneSettings struct {
	Width      *int   `json:"width,omitempty"`
	Height     *int   `json:"height,omitempty"`
	Rate       *int   `json:"rate,omitempty"`
	Scale      *int   `json:"scale,omitempty"`
	SampleRate *int   `json:"sample_rate,omitempty"`
	Background string `json:"background,omitempty"`
}

type CreateSceneParams struct {
	SceneSettings
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
}

type SelectSceneParams struct {
	TargetSceneID *int   `json:"target_scene_id,omitempty"`
	Name          string `json:"name,omitempty"`
}

type CreateProjectParams struct {
	SceneSettings
	ShowConfirm bool `json:"show_confirm,omitempty"`
}

type ProjectFileParams struct {
	File        string `json:"file"`
	ShowConfirm bool   `json:"show_confirm,omitempty"`
}

type ProjectResult struct {
	Context Context `json:"context"`
	Action  string  `json:"action"`
	File    string  `json:"file,omitempty"`
}

type OutputFileParams struct {
	File            string `json:"file"`
	OutputPlugin    string `json:"output_plugin"`
	SaveProjectFile string `json:"save_project_file,omitempty"`
}

type OutputJob struct {
	ID              uint64 `json:"id"`
	SessionID       string `json:"session_id"`
	SceneID         int    `json:"scene_id"`
	File            string `json:"file"`
	OutputPlugin    string `json:"output_plugin"`
	State           string `json:"state"`
	Outcome         string `json:"outcome"`
	SaveProjectFile string `json:"save_project_file,omitempty"`
}

type OutputStatusResult struct {
	EditState int        `json:"edit_state"`
	Job       *OutputJob `json:"job,omitempty"`
}
