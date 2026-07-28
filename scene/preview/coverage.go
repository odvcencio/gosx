package preview

import (
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/capability"
	"m31labs.dev/gosx/scene/geom"
)

// This file keeps the native preview honest about what it can draw.
//
// The CPU rasterizer builds geometry for a fixed set of primitive kinds and
// reads exactly one directional light. Any other kind produces no pixels. A
// silent drop is worse than a missing feature, because the author receives a
// PNG and a success exit code for a scene that never rendered. Every drop below
// becomes a diagnostic that names the record and the reason.

// rasterizableKinds lists the canonical primitive names that the CPU
// rasterizer can build. Every entry is a package scene/geom generator name,
// because render/bundle builds its primitives with geom.Build and draws
// whatever that returns.
//
// Two tests hold this table to the renderer. TestRasterizableKindsRenderPixels
// renders one mesh per entry and fails when an entry produces no pixels.
// TestGeomGeneratorsAreAllListedAsRasterizable fails when package scene/geom
// gains a generator that this table does not name. The second test exists
// because this table once omitted the torus knot for months: render/bundle drew
// the knot correctly while the preview told the author it drew nothing.
var rasterizableKinds = []string{
	geom.KindBox,
	geom.KindCone,
	geom.KindCube,
	geom.KindCylinder,
	geom.KindPlane,
	geom.KindPyramid,
	geom.KindSphere,
	geom.KindTorus,
	geom.KindTorusKnot,
}

// knownUnsupportedKinds explains the authored kinds that the typed scene
// package can emit but the CPU rasterizer cannot build. Naming them keeps the
// diagnostic specific instead of suggesting a spelling fix that will not help.
// Each entry states the exact blocker, so the next reader knows what closing
// the gap needs.
var knownUnsupportedKinds = map[string]string{
	"gltf-mesh": "engine.RenderInstancedMesh carries no vertex buffer field, so inline mesh vertices never reach any native backend; " +
		"this covers every scene.BufferGeometry generator, which is CircleGeometry, RingGeometry, ShapeGeometry, ExtrudeGeometry, " +
		"PolygonGeometry, PolyhedronGeometry and the four named solids built on it",
	"lines": "the CPU rasterizer draws only the bundle.lit, bundle.unlit, bundle.shadow, and particle pipelines, and skips the bundle.worldLine line-list pipeline",
}

// CanRasterizeKind reports whether the CPU preview builds geometry for a
// primitive kind. Tools use it to decide what a native preview can show.
//
// The answer comes from geom.NormalizeKind, the same function render/bundle
// calls before it builds. A separate spelling table here would drift from the
// renderer, and a drifted table reports a working geometry as unsupported.
func CanRasterizeKind(kind string) bool {
	return geom.NormalizeKind(kind) != ""
}

// RasterizableKinds returns the canonical primitive names the CPU preview
// draws, in sorted order.
func RasterizableKinds() []string {
	out := append([]string(nil), rasterizableKinds...)
	sort.Strings(out)
	return out
}

func normalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

// geometryDiagnostic returns an honest warning when the CPU rasterizer drops a
// mesh, and reports ok=false when the mesh draws normally.
func geometryDiagnostic(kind, id string) (engine.RenderDiagnostic, bool) {
	normalized := normalizeKind(kind)
	if CanRasterizeKind(kind) {
		return engine.RenderDiagnostic{}, false
	}
	var message string
	switch {
	case normalized == "":
		message = "native preview drew nothing because this record has no geometry kind"
	default:
		message = fmt.Sprintf("native preview cannot rasterize geometry kind %q, so this record draws no pixels", kind)
		if reason, ok := knownUnsupportedKinds[normalized]; ok {
			message += "; " + reason
		} else if suggestion := nearestKind(normalized); suggestion != "" {
			message += fmt.Sprintf("; did you mean %q?", suggestion)
		} else {
			message += "; supported kinds are " + strings.Join(RasterizableKinds(), ", ")
		}
	}
	return unsupported("geometry", id, message), true
}

// lightDiagnostics reports every authored light that the CPU rasterizer does
// not read. The rasterizer resolves one directional light plus the environment
// ambient term; it ignores all other kinds and every later directional light.
func lightDiagnostics(lights []engine.RenderLight) []engine.RenderDiagnostic {
	// The CPU rasterizer used to read ONE directional light, so this function
	// reported every other light and every later directional light. It now runs
	// the same runtime light loop litWGSL runs, shading ambient, directional,
	// point, spot and hemisphere with no count cap. Reporting those would tell an
	// author their lighting does nothing while it is lighting the scene.
	//
	// Rect-area is the one kind that still drops, and the reason is structural
	// rather than unfinished work: engine.RenderLight carries no width and no
	// height, so the rectangle the polygon form factor integrates over does not
	// exist on this path. Adding those two fields is the prerequisite; see the
	// rect-area-light row in render/bundle/lit_drift_test.go.
	// Match BOTH spellings. The typed API lowers scene.RectAreaLight to
	// "rect-area" (scene/scene.go), while a hand-written IR document may say
	// "area". Matching one spelling silently reports nothing for the other, and
	// a diagnostic that never fires is indistinguishable from a closed gap.
	var out []engine.RenderDiagnostic
	for _, light := range lights {
		switch normalizeKind(light.Kind) {
		case "rect-area", "rectarea", "area":
		default:
			continue
		}
		out = append(out, unsupported("light", light.ID,
			"native preview does not shade a rect-area light; engine.RenderLight carries no width and no "+
				"height, so the rectangle the form factor integrates over does not exist on this path"))
	}
	return out
}

// unsupportedRecordDiagnostics reports the whole-record channels that a native
// frame carries and never draws. Each one used to disappear without a word, so
// the author received a PNG, a success exit code, and no sign that a whole
// authoring feature produced nothing.
//
// Every entry names the blocker, because a reader needs to know whether to wait
// for the backend or to change the scene.
func unsupportedRecordDiagnostics(ir scene.SceneIR) []engine.RenderDiagnostic {
	var out []engine.RenderDiagnostic
	for _, points := range ir.Points {
		out = append(out, unsupported("points", points.ID,
			"render/bundle reads no RenderBundle.Points field, so this point cloud reaches no pipeline and draws nothing; "+
				"closing this needs a point or billboard pipeline in render/bundle"))
	}
	for _, particles := range ir.ComputeParticles {
		out = append(out, unsupported("compute-particles", particles.ID,
			"a preview renders one frame on a fresh renderer, and a particle spawns with age zero and fades in over the first "+
				"fifteen percent of its life, so a single frame shows nothing; "+
				"closing this needs the preview to advance particle state before the captured frame"))
	}
	if diagnostic, ok := environmentDiagnostic(ir.Environment); ok {
		out = append(out, diagnostic)
	}
	if diagnostic, ok := environmentMapBackendDiagnostic(ir.Environment); ok {
		out = append(out, diagnostic)
	}
	return out
}

// environmentMapBackendDiagnostic warns that the poster shows an environment map
// the preferred browser backend will not draw.
//
// WHY A DIAGNOSTIC FROM THIS PACKAGE NAMES ANOTHER BACKEND. Every other
// diagnostic here carries Backend "headless", because every other one reports
// what THIS device drops. This one is the opposite case and it is the more
// dangerous one: the CPU rasterizer DRAWS the environment map, so the author
// receives a poster that proves the field works, and then sees nothing on the
// backend most viewers get. Silence here is what makes the poster misleading.
//
// The verdict comes from scene/capability, so this function states no opinion of
// its own. It fires only while Matrix[environment-map][webgpu] is false, and it
// stops on its own the day the WebGPU renderer reads the image.
func environmentMapBackendDiagnostic(env scene.EnvironmentIR) (engine.RenderDiagnostic, bool) {
	if strings.TrimSpace(env.EnvMap) == "" {
		return engine.RenderDiagnostic{}, false
	}
	if capability.Supports(capability.BackendWebGPU, capability.FeatureEnvironmentMap) {
		return engine.RenderDiagnostic{}, false
	}
	return engine.RenderDiagnostic{
		Severity: "warning",
		Code:     "scene.preview.environment_map_backend_gap",
		Backend:  string(capability.BackendWebGPU),
		Message: "this poster shades the authored envMap, and so does the WebGL2 browser renderer, but the WebGPU " +
			"browser renderer reads no environment map at all: envMap, envIntensity and envRotation appear zero " +
			"times in 16a-scene-webgpu.js. WebGPU is the preferred backend, so most viewers see a scene without " +
			"the reflection this image shows. Do not size the lighting from this poster alone",
	}, true
}

// environmentDiagnostic names the environment fields the frame carries and the
// CPU rasterizer never reads.
//
// The rasterizer reads the ambient, sky and ground terms, samples an environment
// cubemap for image-based lighting, and now runs the present pass — so envMap,
// envIntensity, envRotation, exposure and toneMapping all left this list. Each
// entry was correct when it was written and became a lie as the CPU path grew;
// keeping one tells an author their authored value does nothing while it is
// shading, which invites deleting work that functions.
//
// Fog alone remains, and it is not a post-effect gap. Neither native copy has a
// fog term at all: litWGSL carries none, and the browser applies fog inside its
// material shader rather than a pass. So closing fog is a change to the material
// on both sides, not another pass on this device.
//
// This function reports only what THIS device drops. It says nothing about the
// browser, and dropping envMap from the list above created a second problem it
// cannot see: the poster now shades a term the WebGPU browser renderer ignores.
// environmentMapBackendDiagnostic below reports that half.
func environmentDiagnostic(env scene.EnvironmentIR) (engine.RenderDiagnostic, bool) {
	var ignored []string
	if strings.TrimSpace(env.FogColor) != "" || env.FogDensity != 0 {
		ignored = append(ignored, "fogColor", "fogDensity")
	}
	if len(ignored) == 0 {
		return engine.RenderDiagnostic{}, false
	}
	sort.Strings(ignored)
	return engine.RenderDiagnostic{
		Severity: "info",
		Code:     "scene.preview.environment_fields_ignored",
		Backend:  "headless",
		Message: "native preview shades the ambient, sky, ground and environment-map terms and runs the present pass; " +
			"fog lives in the browser material shader and has no native counterpart, so these authored fields did not " +
			"change the frame: " +
			strings.Join(ignored, ", "),
	}, true
}

// nearestKind returns the closest supported kind when the author probably made
// a spelling mistake. It returns an empty string when nothing is close enough,
// so an unrelated kind never gets a misleading suggestion.
func nearestKind(kind string) string {
	best, bestDistance := "", 0
	for _, candidate := range RasterizableKinds() {
		distance := editDistance(kind, candidate)
		if best == "" || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	limit := len(kind) / 3
	if limit < 1 {
		limit = 1
	}
	if bestDistance > limit {
		return ""
	}
	return best
}

// editDistance returns the Levenshtein distance between two short strings.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = minInt(previous[j]+1, minInt(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
