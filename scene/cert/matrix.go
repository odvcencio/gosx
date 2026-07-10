package cert

import (
	"encoding/json"
	"sort"
)

type Status string

const (
	Complete      Status = "complete"
	Partial       Status = "partial"
	Fallback      Status = "fallback"
	Unsupported   Status = "unsupported"
	NotApplicable Status = "notApplicable"
)

type Dimension string

const (
	TypedAuthoring Dimension = "typedAuthoring"
	GSXAuthoring   Dimension = "gsxAuthoring"
	SceneIR        Dimension = "sceneIR"
	RenderBundle   Dimension = "renderBundle"
	WebGPU         Dimension = "webgpu"
	WebGL          Dimension = "webgl"
	CanvasFallback Dimension = "canvasFallback"
	Headless       Dimension = "headless"
	Diff           Dimension = "diff"
	Picking        Dimension = "picking"
	Signals        Dimension = "signals"
	Assets         Dimension = "assets"
	PerfBudget     Dimension = "perfBudget"
	Docs           Dimension = "docs"
	Tests          Dimension = "tests"
	A11yFallback   Dimension = "a11yFallback"
	Motion         Dimension = "motion"
)

var AllDimensions = []Dimension{
	TypedAuthoring,
	GSXAuthoring,
	SceneIR,
	RenderBundle,
	WebGPU,
	WebGL,
	CanvasFallback,
	Headless,
	Diff,
	Picking,
	Signals,
	Assets,
	PerfBudget,
	Docs,
	Tests,
	A11yFallback,
	Motion,
}

type Entry struct {
	Feature     string               `json:"feature"`
	Category    string               `json:"category"`
	Dimensions  map[Dimension]Status `json:"dimensions"`
	Reasons     map[Dimension]string `json:"reasons,omitempty"`
	NextActions map[Dimension]string `json:"nextActions,omitempty"`
	Examples    []string             `json:"examples,omitempty"`
	TestTargets []string             `json:"testTargets,omitempty"`
}

type Report struct {
	Schema  string  `json:"schema"`
	Entries []Entry `json:"entries"`
	Summary Summary `json:"summary"`
}

type Summary struct {
	Features       int                          `json:"features"`
	StatusCounts   map[Status]int               `json:"statusCounts"`
	ByDimension    map[Dimension]map[Status]int `json:"byDimension"`
	StrictFailures []StrictFailure              `json:"strictFailures,omitempty"`
}

type StrictFailure struct {
	Feature   string    `json:"feature"`
	Dimension Dimension `json:"dimension"`
	Status    Status    `json:"status"`
	Required  string    `json:"required"`
}

type ValidationProblem struct {
	Feature   string    `json:"feature"`
	Dimension Dimension `json:"dimension,omitempty"`
	Message   string    `json:"message"`
}

const Schema = "gosx.scene3d.cert.v1"

type seed struct {
	feature     string
	category    string
	profile     string
	overrides   map[Dimension]Status
	examples    []string
	testTargets []string
}

func Matrix() []Entry {
	entries := make([]Entry, 0, len(seeds))
	for _, s := range seeds {
		entries = append(entries, buildEntry(s))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category == entries[j].Category {
			return entries[i].Feature < entries[j].Feature
		}
		return entries[i].Category < entries[j].Category
	})
	return entries
}

func BuildReport() Report {
	entries := Matrix()
	return Report{
		Schema:  Schema,
		Entries: entries,
		Summary: Summarize(entries),
	}
}

func MarshalJSON() ([]byte, error) {
	return json.MarshalIndent(BuildReport(), "", "  ")
}

func Summarize(entries []Entry) Summary {
	summary := Summary{
		Features:     len(entries),
		StatusCounts: make(map[Status]int),
		ByDimension:  make(map[Dimension]map[Status]int),
	}
	for _, d := range AllDimensions {
		summary.ByDimension[d] = make(map[Status]int)
	}
	for _, entry := range entries {
		for _, d := range AllDimensions {
			status := entry.Dimensions[d]
			summary.StatusCounts[status]++
			summary.ByDimension[d][status]++
		}
	}
	summary.StrictFailures = StrictFailures(entries)
	return summary
}

func Validate(entries []Entry) []ValidationProblem {
	var problems []ValidationProblem
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Feature == "" {
			problems = append(problems, ValidationProblem{Message: "feature name is required"})
			continue
		}
		if seen[entry.Feature] {
			problems = append(problems, ValidationProblem{Feature: entry.Feature, Message: "duplicate feature entry"})
		}
		seen[entry.Feature] = true
		if entry.Category == "" {
			problems = append(problems, ValidationProblem{Feature: entry.Feature, Message: "category is required"})
		}
		for _, d := range AllDimensions {
			status, ok := entry.Dimensions[d]
			if !ok || status == "" {
				problems = append(problems, ValidationProblem{Feature: entry.Feature, Dimension: d, Message: "dimension status is required"})
				continue
			}
			if status != Complete {
				if entry.Reasons[d] == "" {
					problems = append(problems, ValidationProblem{Feature: entry.Feature, Dimension: d, Message: "non-complete dimension requires a reason"})
				}
				if entry.NextActions[d] == "" {
					problems = append(problems, ValidationProblem{Feature: entry.Feature, Dimension: d, Message: "non-complete dimension requires a next action"})
				}
			}
		}
	}
	return problems
}

func StrictFailures(entries []Entry) []StrictFailure {
	byFeature := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byFeature[entry.Feature] = entry
	}
	var failures []StrictFailure
	requireComplete := func(features []string, dims []Dimension, required string) {
		for _, feature := range features {
			entry, ok := byFeature[feature]
			if !ok {
				failures = append(failures, StrictFailure{Feature: feature, Required: required})
				continue
			}
			for _, d := range dims {
				status := entry.Dimensions[d]
				if status != Complete {
					failures = append(failures, StrictFailure{Feature: feature, Dimension: d, Status: status, Required: required})
				}
			}
		}
	}
	requireComplete([]string{"cube", "box", "plane", "pyramid", "sphere", "cylinder", "cone", "torus"}, []Dimension{
		TypedAuthoring, SceneIR, RenderBundle, WebGPU, Tests,
	}, "built-in primitives must be typed, serialized, bundled, WebGPU-native, and tested")
	requireComplete([]string{"HTML overlay"}, []Dimension{SceneIR, RenderBundle, A11yFallback}, "DOM HTML overlays must preserve IR, bundle metadata, and accessible fallback semantics")
	requireComplete([]string{"HTML texture surface"}, []Dimension{SceneIR, RenderBundle}, "HTML texture surfaces must preserve IR and render bundle metadata")
	for _, feature := range []string{"HTML texture surface", "structured pick result"} {
		entry, ok := byFeature[feature]
		if !ok {
			failures = append(failures, StrictFailure{Feature: feature, Required: "feature must be present in certification matrix"})
			continue
		}
		if entry.Dimensions[WebGPU] == Unsupported {
			failures = append(failures, StrictFailure{Feature: feature, Dimension: WebGPU, Status: Unsupported, Required: "WebGPU may be partial, fallback, or complete, but must not be silently unsupported"})
		}
	}
	if entry, ok := byFeature["custom WGSL"]; ok {
		if entry.Dimensions[Docs] == Complete && entry.Dimensions[WebGPU] != Complete {
			failures = append(failures, StrictFailure{Feature: "custom WGSL", Dimension: Docs, Status: entry.Dimensions[Docs], Required: "docs must not imply complete custom WGSL before WebGPU support is complete"})
		}
	}
	// Phase 1 motion floor: the canonical evaluator + native↔WASM parity gate are
	// proven for skeletal animation. Skinned mesh is the primary feature that exercises
	// the motion evaluator; it must carry an explicit Motion status — unsupported is
	// not permitted now that the evaluator contract exists.
	for _, feature := range []string{"skinned mesh", "GLB model"} {
		entry, ok := byFeature[feature]
		if !ok {
			failures = append(failures, StrictFailure{Feature: feature, Required: "feature must be present in certification matrix"})
			continue
		}
		if entry.Dimensions[Motion] == Unsupported {
			failures = append(failures, StrictFailure{Feature: feature, Dimension: Motion, Status: Unsupported, Required: "Motion may be partial or complete, but must not be silently unsupported after Phase 1 evaluator delivery"})
		}
	}
	return failures
}

func buildEntry(s seed) Entry {
	dims := profile(s.profile)
	for d, status := range s.overrides {
		dims[d] = status
	}
	entry := Entry{
		Feature:     s.feature,
		Category:    s.category,
		Dimensions:  dims,
		Reasons:     make(map[Dimension]string),
		NextActions: make(map[Dimension]string),
		Examples:    append([]string(nil), s.examples...),
		TestTargets: append([]string(nil), s.testTargets...),
	}
	for _, d := range AllDimensions {
		status := dims[d]
		if status == Complete {
			continue
		}
		entry.Reasons[d] = defaultReason(status, d)
		entry.NextActions[d] = defaultNextAction(status, d)
	}
	return entry
}

func profile(name string) map[Dimension]Status {
	dims := map[Dimension]Status{}
	for _, d := range AllDimensions {
		dims[d] = Partial
	}
	switch name {
	case "primitive":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, WebGPU, WebGL, Headless, Diff, Picking, Tests} {
			dims[d] = Complete
		}
		dims[CanvasFallback] = Fallback
		dims[Signals] = Partial
		dims[Assets] = NotApplicable
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[A11yFallback] = NotApplicable
		dims[Motion] = NotApplicable
	case "nativePartial":
		for _, d := range []Dimension{TypedAuthoring, SceneIR, RenderBundle, WebGL, Diff} {
			dims[d] = Complete
		}
		dims[WebGPU] = Partial
		dims[CanvasFallback] = Fallback
		dims[Headless] = Partial
		dims[A11yFallback] = NotApplicable
		dims[Motion] = NotApplicable
	case "htmlDOM":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, WebGL, CanvasFallback, Diff, Picking, Signals, Docs, Tests, A11yFallback} {
			dims[d] = Complete
		}
		dims[WebGPU] = NotApplicable
		dims[Headless] = Fallback
		dims[Assets] = NotApplicable
		dims[PerfBudget] = Partial
		// DOM motion (CSS transitions, WAAPI) is outside Phase 1 evaluator scope.
		dims[Motion] = NotApplicable
	case "htmlTexture":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, Diff, Picking, Signals} {
			dims[d] = Complete
		}
		dims[WebGPU] = Partial
		dims[WebGL] = Fallback
		dims[CanvasFallback] = Fallback
		dims[Headless] = Fallback
		dims[Assets] = Partial
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[Tests] = Partial
		dims[A11yFallback] = Complete
		dims[Motion] = NotApplicable
	case "asset":
		for _, d := range []Dimension{SceneIR, RenderBundle, Assets} {
			dims[d] = Complete
		}
		dims[TypedAuthoring] = Partial
		dims[GSXAuthoring] = Partial
		dims[WebGPU] = Partial
		dims[WebGL] = Partial
		dims[CanvasFallback] = Fallback
		dims[Headless] = Partial
		dims[Diff] = Partial
		dims[Picking] = NotApplicable
		dims[Signals] = Partial
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[Tests] = Partial
		dims[A11yFallback] = NotApplicable
		// Asset pipeline itself does not exercise the motion evaluator contract.
		dims[Motion] = NotApplicable
	case "runtime":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, WebGL, Diff, Signals} {
			dims[d] = Complete
		}
		dims[WebGPU] = Partial
		dims[CanvasFallback] = Fallback
		dims[Headless] = Partial
		dims[Assets] = NotApplicable
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[Tests] = Partial
		dims[A11yFallback] = NotApplicable
		dims[Motion] = NotApplicable
	case "postfx":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, WebGPU, WebGL, Diff} {
			dims[d] = Complete
		}
		dims[CanvasFallback] = Fallback
		dims[Headless] = Partial
		dims[Picking] = NotApplicable
		dims[Signals] = Partial
		dims[Assets] = NotApplicable
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[Tests] = Partial
		dims[A11yFallback] = NotApplicable
		dims[Motion] = NotApplicable
	case "material":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, WebGPU, WebGL, Diff} {
			dims[d] = Complete
		}
		dims[CanvasFallback] = Fallback
		dims[Headless] = Partial
		dims[Picking] = NotApplicable
		dims[Signals] = Partial
		dims[Assets] = Partial
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[Tests] = Partial
		dims[A11yFallback] = NotApplicable
		dims[Motion] = NotApplicable
	case "lighting":
		for _, d := range []Dimension{TypedAuthoring, GSXAuthoring, SceneIR, RenderBundle, WebGPU, WebGL, Diff} {
			dims[d] = Complete
		}
		dims[CanvasFallback] = Fallback
		dims[Headless] = Partial
		dims[Picking] = NotApplicable
		dims[Signals] = Partial
		dims[Assets] = NotApplicable
		dims[PerfBudget] = Partial
		dims[Docs] = Partial
		dims[Tests] = Partial
		dims[A11yFallback] = NotApplicable
		dims[Motion] = NotApplicable
	default:
		for _, d := range AllDimensions {
			dims[d] = Partial
		}
	}
	return dims
}

func defaultReason(status Status, d Dimension) string {
	switch status {
	case Partial:
		return "implemented in the current contract but not yet fully certified across examples, diagnostics, budgets, and backend parity"
	case Fallback:
		return "feature has an explicit fallback path for this dimension instead of equivalent native behavior"
	case Unsupported:
		return "feature is not supported for this dimension in the current public contract"
	case NotApplicable:
		return "dimension does not apply to this feature"
	default:
		return "status requires review"
	}
}

func defaultNextAction(status Status, d Dimension) string {
	switch status {
	case Partial:
		switch d {
		case Docs:
			return "publish production-shape documentation and link a canonical proof app"
		case Tests:
			return "add unit, semantic, or browser coverage for the certified behavior"
		case PerfBudget:
			return "connect the feature to scene budget accounting and inspector telemetry"
		default:
			return "finish backend parity, diagnostics, and certification tests for this dimension"
		}
	case Fallback:
		return "keep fallback visible in diagnostics and promote to native behavior when practical"
	case Unsupported:
		return "either implement support or keep documentation explicit that this dimension is unavailable"
	case NotApplicable:
		return "no action required while the feature contract remains unchanged"
	default:
		return "review certification status"
	}
}

func overrides(values ...any) map[Dimension]Status {
	out := map[Dimension]Status{}
	for i := 0; i+1 < len(values); i += 2 {
		d, ok := values[i].(Dimension)
		if !ok {
			continue
		}
		status, ok := values[i+1].(Status)
		if !ok {
			continue
		}
		out[d] = status
	}
	return out
}

var seeds = []seed{
	{feature: "cube", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "box", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "plane", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "pyramid", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "sphere", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "cylinder", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "cone", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "torus", category: "geometry", profile: "primitive", examples: []string{"/demos/scene3d/primitives"}, testTargets: []string{"./render/bundle"}},
	{feature: "lines", category: "geometry", profile: "nativePartial"},
	{feature: "thick lines", category: "geometry", profile: "nativePartial"},
	{feature: "dashed lines", category: "geometry", profile: "nativePartial", overrides: overrides(WebGPU, Partial, WebGL, Complete)},
	{feature: "decals", category: "geometry", profile: "nativePartial", overrides: overrides(WebGPU, Partial, Tests, Partial)},
	{feature: "sprites", category: "geometry", profile: "nativePartial", overrides: overrides(WebGPU, Complete, WebGL, Complete, Picking, Complete)},
	{feature: "labels", category: "geometry", profile: "htmlDOM"},
	{feature: "HTML overlay", category: "geometry", profile: "htmlDOM"},
	{feature: "HTML portal", category: "geometry", profile: "htmlDOM"},
	{feature: "HTML texture surface", category: "geometry", profile: "htmlTexture"},
	{feature: "GLB model", category: "geometry", profile: "asset", overrides: overrides(TypedAuthoring, Complete, GSXAuthoring, Complete, WebGPU, Complete, WebGL, Complete, Picking, Complete, Tests, Complete, Motion, Partial)},
	{feature: "instanced mesh", category: "geometry", profile: "primitive", overrides: overrides(Assets, NotApplicable, PerfBudget, Complete)},
	{feature: "instanced GLB mesh", category: "geometry", profile: "asset", overrides: overrides(WebGPU, Complete, WebGL, Complete, Picking, Complete)},
	{feature: "skinned mesh", category: "geometry", profile: "asset", overrides: overrides(WebGPU, Complete, WebGL, Complete, Tests, Partial, Motion, Partial)},
	{feature: "helper grid", category: "geometry", profile: "nativePartial"},
	{feature: "helper axes", category: "geometry", profile: "nativePartial"},
	{feature: "helper box", category: "geometry", profile: "nativePartial"},
	{feature: "skeleton helper", category: "geometry", profile: "nativePartial"},
	{feature: "transform controls", category: "geometry", profile: "runtime", overrides: overrides(WebGPU, Partial, Picking, Partial)},

	{feature: "flat", category: "materials", profile: "material", overrides: overrides(Tests, Complete)},
	{feature: "matte", category: "materials", profile: "material"},
	{feature: "standard PBR", category: "materials", profile: "material", overrides: overrides(Tests, Complete)},
	{feature: "glass", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "glow", category: "materials", profile: "material"},
	{feature: "ghost", category: "materials", profile: "material"},
	{feature: "line basic", category: "materials", profile: "material"},
	{feature: "line dashed", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial)},
	{feature: "custom GLSL", category: "materials", profile: "material", overrides: overrides(WebGPU, NotApplicable, WebGL, Partial, Docs, Partial)},
	// Motion=partial: TargetMaterial authoring (MaterialAnims), lowering
	// (materialMotionTracks → MaterialMotionProgram), and the JS customUniforms
	// apply seam (window.__gosx_motion_wasm) exist. Animated-uniform pixel render
	// is browser-unverified; native/bundle material path is still static-cached.
	{feature: "custom WGSL", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, NotApplicable, Docs, Partial, Tests, Partial, Motion, Partial)},
	{feature: "named materials", category: "materials", profile: "material"},
	{feature: "CSS variable material fields", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial, Tests, Partial)},
	{feature: "texture maps", category: "materials", profile: "material", overrides: overrides(Assets, Complete, Tests, Complete)},
	{feature: "normal maps", category: "materials", profile: "material", overrides: overrides(Assets, Complete)},
	{feature: "emissive maps", category: "materials", profile: "material", overrides: overrides(Assets, Complete)},
	{feature: "roughness/metalness maps", category: "materials", profile: "material", overrides: overrides(Assets, Complete)},
	{feature: "clearcoat", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "sheen", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "transmission", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "iridescence", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "anisotropy", category: "materials", profile: "material", overrides: overrides(WebGPU, Partial, WebGL, Partial)},

	{feature: "ambient", category: "lighting", profile: "lighting", overrides: overrides(Tests, Complete)},
	{feature: "directional", category: "lighting", profile: "lighting", overrides: overrides(Tests, Complete)},
	{feature: "point", category: "lighting", profile: "lighting"},
	{feature: "spot", category: "lighting", profile: "lighting"},
	{feature: "hemisphere", category: "lighting", profile: "lighting", overrides: overrides(WebGPU, Partial)},
	{feature: "rect area", category: "lighting", profile: "lighting", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "light probe", category: "lighting", profile: "lighting", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "shadows", category: "lighting", profile: "lighting", overrides: overrides(Tests, Complete, PerfBudget, Partial)},
	{feature: "cascaded shadows", category: "lighting", profile: "lighting", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "shadow memory cap", category: "lighting", profile: "lighting", overrides: overrides(PerfBudget, Complete, Tests, Partial)},

	{feature: "orbit controls", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Complete, Tests, Complete)},
	{feature: "first-person controls", category: "runtime", profile: "runtime"},
	{feature: "fly controls", category: "runtime", profile: "runtime"},
	{feature: "drag-to-rotate", category: "runtime", profile: "runtime"},
	{feature: "pointer lock", category: "runtime", profile: "runtime", overrides: overrides(A11yFallback, Partial)},
	{feature: "camera get/set handle", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Complete)},
	{feature: "camera event stream", category: "runtime", profile: "runtime"},
	{feature: "object picking", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Complete, WebGL, Complete, Picking, Complete, Tests, Complete)},
	{feature: "instance picking", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Complete, WebGL, Complete, Picking, Complete)},
	{feature: "UV picking", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Partial, WebGL, Complete, Picking, Complete)},
	{feature: "structured pick result", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Partial, WebGL, Complete, Picking, Complete, Tests, Partial)},
	{feature: "scene diff commands", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Complete, WebGL, Complete, Tests, Complete)},
	{feature: "HTML event routing", category: "runtime", profile: "runtime", overrides: overrides(WebGPU, Partial, A11yFallback, Complete)},
	{feature: "shared scene signals", category: "runtime", profile: "runtime", overrides: overrides(Signals, Complete)},

	{feature: "bloom", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Complete)},
	// FXAA (E1+E2, spec.feralsurge.v0.2): the "postfx" profile default marks
	// WebGPU/WebGL Complete, which was a lie until this landed — render/bundle
	// (native WebGPU, render/bundle/postfx.go) always ran a fixed FXAA 3.11
	// pass, but BOTH browser renderers had zero FXAA of their own. Now real:
	// scene.FXAA + scene.GameplayPostFX() (scene/postfx.go) lower through
	// FXAAIR (scene/postfx_ir.go) to a chain-end pass in 16-scene-webgl.js
	// (SCENE_POST_FXAA_SOURCE, GLSL) and 16a-scene-webgpu.js
	// (WGSL_POST_FXAA_FRAGMENT), proven by client/js/runtime.test.js's
	// "Scene3D FXAA is wired as the chain-end postfx pass..." source-pattern
	// test plus scene/postfx_test.go. Tests Complete like its bloom/tonemap
	// siblings, backed by the same class of artifact (source-pattern JS test
	// + Go unit test), not a real-GPU pixel proof.
	{feature: "FXAA", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Complete)},
	{feature: "tonemap", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Complete)},
	{feature: "SSAO", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Partial)},
	{feature: "DOF", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Partial)},
	{feature: "vignette", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Partial)},
	{feature: "color grade", category: "post-fx", profile: "postfx", overrides: overrides(Tests, Partial)},

	{feature: "GLB", category: "asset pipeline", profile: "asset", overrides: overrides(WebGPU, Complete, WebGL, Complete, Tests, Complete, Motion, Partial)},
	{feature: "glTF", category: "asset pipeline", profile: "asset", overrides: overrides(Motion, Partial)},
	{feature: "raster textures", category: "asset pipeline", profile: "asset", overrides: overrides(WebGPU, Complete, WebGL, Complete, Tests, Complete)},
	{feature: "KTX2", category: "asset pipeline", profile: "asset", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "HDR/EXR", category: "asset pipeline", profile: "asset", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "IBL prefiltering", category: "asset pipeline", profile: "asset", overrides: overrides(WebGPU, Partial, WebGL, Partial)},
	{feature: "mesh compression plan", category: "asset pipeline", profile: "asset", overrides: overrides(Assets, Complete, WebGPU, Partial, WebGL, Partial)},
	{feature: "texture transcode plan", category: "asset pipeline", profile: "asset", overrides: overrides(Assets, Complete, WebGPU, Partial, WebGL, Partial)},
	{feature: "LOD stack", category: "asset pipeline", profile: "asset", overrides: overrides(WebGPU, Partial, WebGL, Partial, Diff, Partial)},
	{feature: "HTML texture manifest", category: "asset pipeline", profile: "asset", overrides: overrides(SceneIR, Complete, RenderBundle, Complete, Assets, Partial, A11yFallback, Complete)},
	{feature: "upload budget", category: "asset pipeline", profile: "asset", overrides: overrides(PerfBudget, Partial, Tests, Partial)},
}
