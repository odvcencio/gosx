// Package capability is the single source of truth for which Scene3D
// rendering backend can faithfully render a given feature set. Go computes a
// verdict; the JS runtime obeys it. See spec.m31labs-gosx.webgpu-honesty-gate.v0.1.
package capability

type Backend string

const (
	BackendWebGPU   Backend = "webgpu"
	BackendWebGL    Backend = "webgl"
	BackendCanvas2D Backend = "canvas2d"
)

var allBackends = []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D}

type Feature string

const (
	FeatureSkinning     Feature = "skinning"
	FeatureIBL          Feature = "ibl"
	FeatureGPUPicking   Feature = "gpu-picking"
	FeatureLineDashed   Feature = "line-dashed"
	FeatureCustomShader Feature = "custom-shader"
	FeatureComputeParts Feature = "compute-particles"
)

// Matrix records which backends implement each feature TODAY. A feature absent
// from the map is supported everywhere. Flip a cell when a renderer gains the
// feature; the drift guard (later task) ties this to renderer manifests.
// custom-shader is per-material (resolved via ShaderResolver), not a flat cell.
var Matrix = map[Feature]map[Backend]bool{
	FeatureSkinning:     {BackendWebGPU: false, BackendWebGL: true},
	FeatureIBL:          {BackendWebGPU: false, BackendWebGL: true},
	FeatureGPUPicking:   {BackendWebGPU: false, BackendWebGL: true},
	FeatureLineDashed:   {BackendWebGPU: false, BackendWebGL: true},
	FeatureComputeParts: {BackendWebGPU: true, BackendWebGL: false},
}

func supports(b Backend, f Feature) bool {
	row, ok := Matrix[f]
	if !ok {
		return true
	}
	return row[b]
}

type Policy struct{ Required map[Feature]bool }

func DefaultPolicy() Policy {
	return Policy{Required: map[Feature]bool{
		FeatureSkinning:   true,
		FeatureGPUPicking: true,
	}}
}

type BackendCaps struct {
	Capable  []Backend             `json:"capable"`
	Degraded map[Backend][]Feature `json:"degraded,omitempty"`
	Reasons  []CapReason           `json:"reasons,omitempty"`
}

type CapReason struct {
	Feature  Feature `json:"feature"`
	Excludes Backend `json:"excludes,omitempty"`
	Degrades Backend `json:"degrades,omitempty"`
}

// Verdict computes capable backends + per-backend degradations. required lists
// author-gated backends (empty = no gate).
func Verdict(features []Feature, required []Backend, pol Policy) BackendCaps {
	caps := BackendCaps{Degraded: map[Backend][]Feature{}}
	candidate := allBackends
	if len(required) > 0 {
		candidate = required
	}
	for _, b := range candidate {
		excluded := false
		for _, f := range features {
			if supports(b, f) {
				continue
			}
			if pol.Required[f] {
				excluded = true
				caps.Reasons = append(caps.Reasons, CapReason{Feature: f, Excludes: b})
				break
			}
			caps.Degraded[b] = append(caps.Degraded[b], f)
			caps.Reasons = append(caps.Reasons, CapReason{Feature: f, Degrades: b})
		}
		if !excluded {
			caps.Capable = append(caps.Capable, b)
		}
	}
	if len(caps.Degraded) == 0 {
		caps.Degraded = nil
	}
	return caps
}
