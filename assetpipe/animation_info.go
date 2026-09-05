package assetpipe

import (
	"math"
	"sort"
)

// AnimationClipInfo inventories an imported glTF clip without decoding buffers.
// Index is its stable identity, even when names are empty or duplicated.
// Time bounds are reported only when every referenced sampler has valid scalar
// accessor bounds. They are authored metadata, not verified playback samples.
type AnimationClipInfo struct {
	Index          int      `json:"index"`
	Name           string   `json:"name,omitempty"`
	Channels       int      `json:"channels"`
	TargetPaths    []string `json:"targetPaths,omitempty"`
	Interpolations []string `json:"interpolations,omitempty"`
	StartSeconds   *float64 `json:"startSeconds,omitempty"`
	EndSeconds     *float64 `json:"endSeconds,omitempty"`
	Duration       *float64 `json:"durationSeconds,omitempty"`
	TimingSource   string   `json:"timingSource,omitempty"`
}

type gltfAnimationProbe struct {
	Name     string `json:"name"`
	Channels []struct {
		Sampler *int `json:"sampler"`
		Target  struct {
			Path string `json:"path"`
		} `json:"target"`
	} `json:"channels"`
	Samplers []struct {
		Input         *int   `json:"input"`
		Interpolation string `json:"interpolation"`
	} `json:"samplers"`
}

type gltfAnimationAccessorProbe struct {
	Type          string    `json:"type"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Min           []float64 `json:"min"`
	Max           []float64 `json:"max"`
}

func inspectAnimationClips(animations []gltfAnimationProbe, accessors []gltfAnimationAccessorProbe) []AnimationClipInfo {
	clips := make([]AnimationClipInfo, 0, len(animations))
	for index, animation := range animations {
		clip := AnimationClipInfo{Index: index, Name: animation.Name, Channels: len(animation.Channels)}
		paths, interpolations := map[string]bool{}, map[string]bool{}
		start, end := math.Inf(1), math.Inf(-1)
		complete := len(animation.Channels) > 0
		for _, channel := range animation.Channels {
			if channel.Target.Path != "" {
				paths[channel.Target.Path] = true
			}
			if channel.Sampler == nil || *channel.Sampler < 0 || *channel.Sampler >= len(animation.Samplers) {
				complete = false
				continue
			}
			sampler := animation.Samplers[*channel.Sampler]
			interpolation := sampler.Interpolation
			if interpolation == "" {
				interpolation = "LINEAR"
			}
			interpolations[interpolation] = true
			if sampler.Input == nil || *sampler.Input < 0 || *sampler.Input >= len(accessors) {
				complete = false
				continue
			}
			accessor := accessors[*sampler.Input]
			if accessor.Type != "SCALAR" || accessor.ComponentType != 5126 || accessor.Count <= 0 ||
				len(accessor.Min) != 1 || len(accessor.Max) != 1 || accessor.Min[0] < 0 ||
				!finiteAnimationTime(accessor.Min[0]) || !finiteAnimationTime(accessor.Max[0]) || accessor.Max[0] < accessor.Min[0] {
				complete = false
				continue
			}
			start = math.Min(start, accessor.Min[0])
			end = math.Max(end, accessor.Max[0])
		}
		clip.TargetPaths, clip.Interpolations = animationInfoKeys(paths), animationInfoKeys(interpolations)
		if complete {
			duration := end - start
			clip.StartSeconds, clip.EndSeconds, clip.Duration = &start, &end, &duration
			clip.TimingSource = "accessor-bounds"
		}
		clips = append(clips, clip)
	}
	return clips
}

func finiteAnimationTime(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func animationInfoKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
