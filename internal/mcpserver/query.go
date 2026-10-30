package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type findObjectsInput struct {
	NameContains  string  `json:"name_contains,omitempty"`
	Effect        string  `json:"effect,omitempty"`
	MediaContains string  `json:"media_contains,omitempty"`
	MediaType     string  `json:"media_type,omitempty" jsonschema:"video, image, or audio"`
	MinWidth      int     `json:"min_width,omitempty"`
	MinHeight     int     `json:"min_height,omitempty"`
	MinDuration   float64 `json:"min_duration,omitempty"`
	LayerStart    *int    `json:"layer_start,omitempty"`
	LayerEnd      *int    `json:"layer_end,omitempty"`
	FrameStart    *int    `json:"frame_start,omitempty"`
	FrameEnd      *int    `json:"frame_end,omitempty"`
	Selected      bool    `json:"selected,omitempty"`
	Focus         bool    `json:"focus,omitempty"`
	Limit         int     `json:"limit,omitempty" jsonschema:"default and maximum 100"`
}

type foundObject struct {
	Object    protocol.Object      `json:"object"`
	Media     []string             `json:"media,omitempty"`
	MediaInfo []protocol.MediaInfo `json:"media_info,omitempty"`
}

type findObjectsOutput struct {
	Context protocol.Context `json:"context"`
	Objects []foundObject    `json:"objects"`
	Limited bool             `json:"limited"`
}

type timelineAnalysisInput struct {
	LayerStart *int `json:"layer_start,omitempty"`
	LayerEnd   *int `json:"layer_end,omitempty"`
	FrameStart *int `json:"frame_start,omitempty"`
	FrameEnd   *int `json:"frame_end,omitempty"`
}

type inspectSceneInput struct {
	LayerStart   int  `json:"layer_start,omitempty"`
	LayerEnd     *int `json:"layer_end,omitempty"`
	IncludeEmpty bool `json:"include_empty,omitempty"`
	MaxObjects   int  `json:"max_objects,omitempty" jsonschema:"default and maximum 1000"`
}

type timelineGap struct {
	Layer int `json:"layer"`
	Start int `json:"start"`
	End   int `json:"end"`
}

type timelineOverlap struct {
	Layer     int      `json:"layer"`
	Start     int      `json:"start"`
	End       int      `json:"end"`
	ObjectIDs []uint64 `json:"object_ids"`
}

type layerSummary struct {
	Layer       int `json:"layer"`
	ObjectCount int `json:"object_count"`
	Start       int `json:"start"`
	End         int `json:"end"`
}

type timelineSummaryOutput struct {
	Context     protocol.Context `json:"context"`
	ObjectCount int              `json:"object_count"`
	Layers      []layerSummary   `json:"layers"`
	Start       int              `json:"start"`
	End         int              `json:"end"`
}

type gapsOutput struct {
	Context protocol.Context `json:"context"`
	Gaps    []timelineGap    `json:"gaps"`
}
type overlapsOutput struct {
	Context  protocol.Context  `json:"context"`
	Overlaps []timelineOverlap `json:"overlaps"`
}
type usedEffectsOutput struct {
	Context protocol.Context `json:"context"`
	Effects map[string]int   `json:"effects"`
}
type usedMediaOutput struct {
	Context protocol.Context    `json:"context"`
	Media   map[string][]uint64 `json:"media"`
}
type missingMedia struct {
	File      string   `json:"file"`
	ObjectIDs []uint64 `json:"object_ids"`
}
type missingMediaOutput struct {
	Context protocol.Context `json:"context"`
	Missing []missingMedia   `json:"missing"`
}
type outOfRangeOutput struct {
	Context protocol.Context  `json:"context"`
	Objects []protocol.Object `json:"objects"`
}

type analyzeTimelineInput struct {
	Operation  string `json:"operation" jsonschema:"summary, gaps, or overlaps"`
	LayerStart *int   `json:"layer_start,omitempty"`
	LayerEnd   *int   `json:"layer_end,omitempty"`
	FrameStart *int   `json:"frame_start,omitempty"`
	FrameEnd   *int   `json:"frame_end,omitempty"`
}

type analyzeTimelineOutput struct {
	Operation   string            `json:"operation"`
	Context     *protocol.Context `json:"context,omitempty"`
	ObjectCount int               `json:"object_count,omitempty"`
	Layers      []layerSummary    `json:"layers,omitempty"`
	Start       int               `json:"start,omitempty"`
	End         int               `json:"end,omitempty"`
	Gaps        []timelineGap     `json:"gaps,omitempty"`
	Overlaps    []timelineOverlap `json:"overlaps,omitempty"`
}

type auditSceneInput struct {
	Operation string `json:"operation" jsonschema:"effects, media, missing_media, or outside_scene"`
}

type auditSceneOutput struct {
	Operation string              `json:"operation"`
	Context   *protocol.Context   `json:"context,omitempty"`
	Effects   map[string]int      `json:"effects,omitempty"`
	Media     map[string][]uint64 `json:"media,omitempty"`
	Missing   []missingMedia      `json:"missing,omitempty"`
	Objects   []protocol.Object   `json:"objects,omitempty"`
}

func addQueryTools(server *mcp.Server, client *bridge.Client) {
	addTool(server, &mcp.Tool{Name: "inspect_scene", Description: "Get the whole scene in one call: context, per-layer object counts and frame ranges, layer colors, markers, and the BPM grid. Use this first; use inspect_timeline for individual objects."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input inspectSceneInput) (*mcp.CallToolResult, protocol.SceneOverview, error) {
			if input.LayerStart < 0 || (input.LayerEnd != nil && *input.LayerEnd < input.LayerStart) {
				return nil, protocol.SceneOverview{}, errors.New("layer range must be ordered and non-negative")
			}
			if input.MaxObjects == 0 {
				input.MaxObjects = protocol.MaxTimelineObjects
			}
			if input.MaxObjects < 1 || input.MaxObjects > protocol.MaxTimelineObjects {
				return nil, protocol.SceneOverview{}, fmt.Errorf("max_objects must be between 1 and %d", protocol.MaxTimelineObjects)
			}
			result, err := client.InspectScene(ctx, protocol.InspectSceneParams(input))
			if err != nil {
				return nil, protocol.SceneOverview{}, err
			}
			if result.Layers == nil {
				result.Layers = []protocol.SceneLayerSummary{}
			}
			if result.Markers == nil {
				result.Markers = []protocol.Marker{}
			}
			if result.BPM == nil {
				result.BPM = []protocol.BPMPoint{}
			}
			return nil, result, nil
		})

	addTool(server, &mcp.Tool{Name: "find_objects", Description: "Find objects by selection, focus, name, effect, media path, layer, or frame range."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input findObjectsInput) (*mcp.CallToolResult, findObjectsOutput, error) {
			if input.Limit == 0 {
				input.Limit = 100
			}
			if input.Limit < 1 || input.Limit > 100 {
				return nil, findObjectsOutput{}, errors.New("limit must be between 1 and 100")
			}
			if input.MediaType != "" && input.MediaType != "video" && input.MediaType != "image" && input.MediaType != "audio" {
				return nil, findObjectsOutput{}, errors.New("media_type must be video, image, or audio")
			}
			current, objects, err := readAllTimelineWithEffects(ctx, client)
			if err != nil {
				return nil, findObjectsOutput{}, err
			}
			if input.Selected || input.Focus {
				selection, err := client.GetSelection(ctx)
				if err != nil {
					return nil, findObjectsOutput{}, err
				}
				allowed := map[uint64]bool{}
				if input.Selected {
					for _, object := range selection.Objects {
						allowed[object.ID] = true
					}
				}
				if input.Focus && selection.FocusObjectID != nil {
					allowed[*selection.FocusObjectID] = true
				}
				filtered := objects[:0]
				for _, object := range objects {
					if allowed[object.ID] {
						filtered = append(filtered, object)
					}
				}
				objects = filtered
			}
			output := findObjectsOutput{Context: current, Objects: []foundObject{}}
			for _, object := range objects {
				if input.NameContains != "" && !strings.Contains(strings.ToLower(object.Name), strings.ToLower(input.NameContains)) {
					continue
				}
				if input.LayerStart != nil && object.Layer < *input.LayerStart {
					continue
				}
				if input.LayerEnd != nil && object.Layer > *input.LayerEnd {
					continue
				}
				if input.FrameStart != nil && object.End < *input.FrameStart {
					continue
				}
				if input.FrameEnd != nil && object.Start > *input.FrameEnd {
					continue
				}
				if input.Effect != "" && !objectHasEffect(object, input.Effect) {
					continue
				}
				found := foundObject{Object: object}
				if input.MediaContains != "" || input.MediaType != "" || input.MinWidth > 0 || input.MinHeight > 0 || input.MinDuration > 0 {
					found.Media, err = objectMedia(ctx, client, object.ID)
					if err != nil {
						return nil, findObjectsOutput{}, err
					}
					matched := false
					for _, file := range found.Media {
						if input.MediaContains != "" && !strings.Contains(strings.ToLower(file), strings.ToLower(input.MediaContains)) {
							continue
						}
						info, mediaErr := client.PreflightMedia(ctx, protocol.PreflightMediaParams{File: file, Strict: false})
						if mediaErr != nil {
							continue
						}
						if !matchesMediaInfo(info, input) {
							continue
						}
						found.MediaInfo = append(found.MediaInfo, info)
						matched = true
					}
					if !matched {
						continue
					}
				}
				if len(output.Objects) == input.Limit {
					output.Limited = true
					break
				}
				output.Objects = append(output.Objects, found)
			}
			return nil, output, nil
		})

	addTool(server, &mcp.Tool{Name: "analyze_timeline", Description: "Summarize object counts and occupied ranges per layer, list gaps, or list overlaps."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input analyzeTimelineInput) (*mcp.CallToolResult, analyzeTimelineOutput, error) {
			out := analyzeTimelineOutput{Operation: input.Operation}
			switch input.Operation {
			case "summary", "gaps", "overlaps":
			default:
				return nil, out, errors.New("operation must be summary, gaps, or overlaps")
			}
			current, objects, err := readFilteredTimeline(ctx, client, timelineAnalysisInput{
				LayerStart: input.LayerStart, LayerEnd: input.LayerEnd, FrameStart: input.FrameStart, FrameEnd: input.FrameEnd,
			})
			if err != nil {
				return nil, out, err
			}
			out.Context = &current
			switch input.Operation {
			case "summary":
				out.ObjectCount = len(objects)
				out.Start, out.End = -1, -1
				byLayer := groupObjectsByLayer(objects)
				keys := make([]int, 0, len(byLayer))
				for layer := range byLayer {
					keys = append(keys, layer)
				}
				sort.Ints(keys)
				for _, layer := range keys {
					values := byLayer[layer]
					start, end := values[0].Start, values[0].End
					for _, object := range values[1:] {
						start = min(start, object.Start)
						end = max(end, object.End)
					}
					out.Layers = append(out.Layers, layerSummary{Layer: layer, ObjectCount: len(values), Start: start, End: end})
					for _, object := range values {
						if out.Start < 0 || object.Start < out.Start {
							out.Start = object.Start
						}
						out.End = max(out.End, object.End)
					}
				}
			case "gaps":
				for layer, values := range groupObjectsByLayer(objects) {
					sortObjects(values)
					for index := 1; index < len(values); index++ {
						if values[index].Start > values[index-1].End+1 {
							out.Gaps = append(out.Gaps, timelineGap{Layer: layer, Start: values[index-1].End + 1, End: values[index].Start - 1})
						}
					}
				}
				sort.Slice(out.Gaps, func(i, j int) bool {
					if out.Gaps[i].Layer == out.Gaps[j].Layer {
						return out.Gaps[i].Start < out.Gaps[j].Start
					}
					return out.Gaps[i].Layer < out.Gaps[j].Layer
				})
			case "overlaps":
				for layer, values := range groupObjectsByLayer(objects) {
					sortObjects(values)
					for i := 0; i < len(values); i++ {
						for j := i + 1; j < len(values) && values[j].Start <= values[i].End; j++ {
							out.Overlaps = append(out.Overlaps, timelineOverlap{
								Layer: layer, Start: values[j].Start, End: min(values[i].End, values[j].End),
								ObjectIDs: []uint64{values[i].ID, values[j].ID},
							})
						}
					}
				}
			}
			return nil, out, nil
		})
	addTool(server, &mcp.Tool{Name: "audit_scene", Description: "Count used effects, list used media, find missing media, or find objects outside the scene frame range."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input auditSceneInput) (*mcp.CallToolResult, auditSceneOutput, error) {
			out := auditSceneOutput{Operation: input.Operation}
			switch input.Operation {
			case "effects":
				current, objects, err := readAllTimelineWithEffects(ctx, client)
				if err != nil {
					return nil, out, err
				}
				out.Context = &current
				out.Effects = map[string]int{}
				for _, object := range objects {
					for _, effect := range object.Effects {
						out.Effects[effect.Name]++
					}
				}
			case "media", "missing_media":
				current, objects, err := readAllTimeline(ctx, client)
				if err != nil {
					return nil, out, err
				}
				out.Context = &current
				media, err := collectUsedMedia(ctx, client, objects)
				if err != nil {
					return nil, out, err
				}
				if input.Operation == "media" {
					out.Media = media
					return nil, out, nil
				}
				for file, ids := range media {
					if _, err := os.Stat(file); err != nil {
						out.Missing = append(out.Missing, missingMedia{File: file, ObjectIDs: ids})
					}
				}
				sort.Slice(out.Missing, func(i, j int) bool { return out.Missing[i].File < out.Missing[j].File })
			case "outside_scene":
				current, objects, err := readAllTimeline(ctx, client)
				if err != nil {
					return nil, out, err
				}
				out.Context = &current
				for _, object := range objects {
					if object.Start < 0 || object.End > current.FrameMax {
						out.Objects = append(out.Objects, object)
					}
				}
			default:
				return nil, out, errors.New("operation must be effects, media, missing_media, or outside_scene")
			}
			return nil, out, nil
		})
}

func readAllTimeline(ctx context.Context, client *bridge.Client) (protocol.Context, []protocol.Object, error) {
	current, err := client.GetContext(ctx)
	if err != nil {
		return protocol.Context{}, nil, err
	}
	expected := mutationContext{SessionID: current.SessionID, Generation: current.Generation, SceneID: current.SceneID}
	return inspectTimelineSnapshot(ctx, client, expected, current.LayerMax, current.FrameMax, 0)
}

func readAllTimelineWithEffects(ctx context.Context, client *bridge.Client) (protocol.Context, []protocol.Object, error) {
	current, objects, err := readAllTimeline(ctx, client)
	if err != nil {
		return current, nil, err
	}
	detailed := make([]protocol.Object, 0, len(objects))
	for start := 0; start < len(objects); start += protocol.MaxBatchOperations {
		end := min(start+protocol.MaxBatchOperations, len(objects))
		ids := make([]uint64, 0, end-start)
		for _, object := range objects[start:end] {
			ids = append(ids, object.ID)
		}
		result, err := client.InspectObjects(ctx, protocol.InspectObjectsParams{ObjectIDs: ids, IncludeEffects: true})
		if err != nil {
			return current, nil, err
		}
		if result.Context.SessionID != current.SessionID || result.Context.Generation != current.Generation || result.Context.SceneID != current.SceneID {
			return current, nil, errors.New("AviUtl2 context changed while reading object details")
		}
		detailed = append(detailed, result.Objects...)
	}
	return current, detailed, nil
}

func readFilteredTimeline(ctx context.Context, client *bridge.Client, input timelineAnalysisInput) (protocol.Context, []protocol.Object, error) {
	current, objects, err := readAllTimeline(ctx, client)
	if err != nil {
		return current, nil, err
	}
	filtered := objects[:0]
	for _, object := range objects {
		if input.LayerStart != nil && object.Layer < *input.LayerStart {
			continue
		}
		if input.LayerEnd != nil && object.Layer > *input.LayerEnd {
			continue
		}
		if input.FrameStart != nil && object.End < *input.FrameStart {
			continue
		}
		if input.FrameEnd != nil && object.Start > *input.FrameEnd {
			continue
		}
		filtered = append(filtered, object)
	}
	return current, filtered, nil
}

func objectHasEffect(object protocol.Object, effect string) bool {
	for _, value := range object.Effects {
		if strings.EqualFold(value.Name, effect) {
			return true
		}
	}
	return false
}
func groupObjectsByLayer(objects []protocol.Object) map[int][]protocol.Object {
	result := map[int][]protocol.Object{}
	for _, o := range objects {
		result[o.Layer] = append(result[o.Layer], o)
	}
	return result
}
func sortObjects(objects []protocol.Object) {
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Start == objects[j].Start {
			return objects[i].End < objects[j].End
		}
		return objects[i].Start < objects[j].Start
	})
}

func objectMedia(ctx context.Context, client *bridge.Client, id uint64) ([]string, error) {
	f, r := false, true
	// Raw values are required for file paths; request them without track metadata or samples.
	result, err := client.InspectObjectValues(ctx, protocol.InspectObjectValuesParams{ObjectID: id, IncludeRawValues: &r, IncludeTrackInfo: &f, IncludeSampledValues: &f})
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, effect := range result.Effects {
		for _, item := range effect.Items {
			if item.Type == 6 && item.RawValue != "" {
				files = append(files, item.RawValue)
			}
		}
	}
	return files, nil
}
func collectUsedMedia(ctx context.Context, client *bridge.Client, objects []protocol.Object) (map[string][]uint64, error) {
	result := map[string][]uint64{}
	for _, o := range objects {
		files, err := objectMedia(ctx, client, o.ID)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			result[file] = append(result[file], o.ID)
		}
	}
	return result, nil
}

func matchesMediaInfo(info protocol.MediaInfo, input findObjectsInput) bool {
	if info.Width < input.MinWidth || info.Height < input.MinHeight || info.TotalTime < input.MinDuration {
		return false
	}
	switch input.MediaType {
	case "":
		return true
	case "video":
		return info.VideoTrackCount > 0 && info.TotalTime > 0
	case "image":
		return info.VideoTrackCount > 0 && info.TotalTime == 0
	case "audio":
		return info.AudioTrackCount > 0
	default:
		return false
	}
}
