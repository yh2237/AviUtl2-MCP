package mcpserver

import (
	"context"
	"errors"
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

func addQueryTools(server *mcp.Server, client *bridge.Client) {
	mcp.AddTool(server, &mcp.Tool{Name: "find_objects", Description: "Find objects by selection, focus, name, effect, media path, layer, or frame range."},
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

	mcp.AddTool(server, &mcp.Tool{Name: "summarize_timeline", Description: "Summarize object counts and occupied ranges per layer."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input timelineAnalysisInput) (*mcp.CallToolResult, timelineSummaryOutput, error) {
			current, objects, err := readFilteredTimeline(ctx, client, input)
			if err != nil {
				return nil, timelineSummaryOutput{}, err
			}
			out := timelineSummaryOutput{Context: current, ObjectCount: len(objects), Layers: []layerSummary{}, Start: -1, End: -1}
			byLayer := map[int][]protocol.Object{}
			for _, object := range objects {
				byLayer[object.Layer] = append(byLayer[object.Layer], object)
				if out.Start < 0 || object.Start < out.Start {
					out.Start = object.Start
				}
				if object.End > out.End {
					out.End = object.End
				}
			}
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
			}
			return nil, out, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "find_gaps", Description: "Find empty frame ranges between objects on each layer."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input timelineAnalysisInput) (*mcp.CallToolResult, gapsOutput, error) {
			current, objects, err := readFilteredTimeline(ctx, client, input)
			if err != nil {
				return nil, gapsOutput{}, err
			}
			byLayer := groupObjectsByLayer(objects)
			gaps := []timelineGap{}
			for layer, values := range byLayer {
				sortObjects(values)
				for i := 1; i < len(values); i++ {
					if values[i].Start > values[i-1].End+1 {
						gaps = append(gaps, timelineGap{Layer: layer, Start: values[i-1].End + 1, End: values[i].Start - 1})
					}
				}
			}
			sort.Slice(gaps, func(i, j int) bool {
				if gaps[i].Layer == gaps[j].Layer {
					return gaps[i].Start < gaps[j].Start
				}
				return gaps[i].Layer < gaps[j].Layer
			})
			return nil, gapsOutput{Context: current, Gaps: gaps}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "find_overlaps", Description: "Find overlapping objects on the same layer."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input timelineAnalysisInput) (*mcp.CallToolResult, overlapsOutput, error) {
			current, objects, err := readFilteredTimeline(ctx, client, input)
			if err != nil {
				return nil, overlapsOutput{}, err
			}
			values := []timelineOverlap{}
			byLayer := groupObjectsByLayer(objects)
			for layer, layerObjects := range byLayer {
				sortObjects(layerObjects)
				for i := 0; i < len(layerObjects); i++ {
					for j := i + 1; j < len(layerObjects) && layerObjects[j].Start <= layerObjects[i].End; j++ {
						values = append(values, timelineOverlap{Layer: layer, Start: layerObjects[j].Start, End: min(layerObjects[i].End, layerObjects[j].End), ObjectIDs: []uint64{layerObjects[i].ID, layerObjects[j].ID}})
					}
				}
			}
			return nil, overlapsOutput{Context: current, Overlaps: values}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "list_used_effects", Description: "Count effect usage in the current scene."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, usedEffectsOutput, error) {
			current, objects, err := readAllTimelineWithEffects(ctx, client)
			if err != nil {
				return nil, usedEffectsOutput{}, err
			}
			values := map[string]int{}
			for _, object := range objects {
				for _, effect := range object.Effects {
					values[effect.Name]++
				}
			}
			return nil, usedEffectsOutput{Context: current, Effects: values}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "list_used_media", Description: "List media paths and the objects that reference them."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, usedMediaOutput, error) {
			current, objects, err := readAllTimeline(ctx, client)
			if err != nil {
				return nil, usedMediaOutput{}, err
			}
			values, err := collectUsedMedia(ctx, client, objects)
			return nil, usedMediaOutput{Context: current, Media: values}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "find_missing_media", Description: "Find referenced media files that do not exist on the MCP server machine."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, missingMediaOutput, error) {
			current, objects, err := readAllTimeline(ctx, client)
			if err != nil {
				return nil, missingMediaOutput{}, err
			}
			used, err := collectUsedMedia(ctx, client, objects)
			if err != nil {
				return nil, missingMediaOutput{}, err
			}
			missing := []missingMedia{}
			for file, ids := range used {
				if _, err := os.Stat(file); err != nil && os.IsNotExist(err) {
					missing = append(missing, missingMedia{File: file, ObjectIDs: ids})
				}
			}
			sort.Slice(missing, func(i, j int) bool { return missing[i].File < missing[j].File })
			return nil, missingMediaOutput{Context: current, Missing: missing}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "find_out_of_scene_objects", Description: "Find objects outside an explicit intended frame or layer range."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input timelineAnalysisInput) (*mcp.CallToolResult, outOfRangeOutput, error) {
			current, objects, err := readAllTimeline(ctx, client)
			if err != nil {
				return nil, outOfRangeOutput{}, err
			}
			out := []protocol.Object{}
			for _, o := range objects {
				if (input.LayerStart != nil && o.Layer < *input.LayerStart) || (input.LayerEnd != nil && o.Layer > *input.LayerEnd) || (input.FrameStart != nil && o.Start < *input.FrameStart) || (input.FrameEnd != nil && o.End > *input.FrameEnd) {
					out = append(out, o)
				}
			}
			return nil, outOfRangeOutput{Context: current, Objects: out}, nil
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
