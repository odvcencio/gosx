package preview

import (
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/gosx/engine"
)

// This file keeps the native preview honest about what it can draw.
//
// The CPU rasterizer builds geometry for a fixed set of primitive kinds and
// reads exactly one directional light. Any other kind produces no pixels. A
// silent drop is worse than a missing feature, because the author receives a
// PNG and a success exit code for a scene that never rendered. Every drop below
// becomes a diagnostic that names the record and the reason.

// rasterizableKinds lists the canonical primitive names that the CPU
// rasterizer can build. TestRasterizableKindsRenderPixels renders one mesh per
// entry and fails when an entry produces no pixels, so this table cannot drift
// away from the renderer without a test failure.
var rasterizableKinds = []string{
	"box",
	"cone",
	"cube",
	"cylinder",
	"plane",
	"pyramid",
	"sphere",
	"torus",
}

// kindAliases maps every accepted spelling onto its canonical primitive name.
// The renderer accepts both the short compatibility names that the browser
// bridge emits and the Go type names that the typed scene package emits.
var kindAliases = map[string]string{
	"box":              "box",
	"boxgeometry":      "box",
	"cone":             "cone",
	"conegeometry":     "cone",
	"cube":             "cube",
	"cubegeometry":     "cube",
	"cylinder":         "cylinder",
	"cylindergeometry": "cylinder",
	"plane":            "plane",
	"planegeometry":    "plane",
	"pyramid":          "pyramid",
	"pyramidgeometry":  "pyramid",
	"quad":             "plane",
	"quadgeometry":     "plane",
	"sphere":           "sphere",
	"spheregeometry":   "sphere",
	"torus":            "torus",
	"torusgeometry":    "torus",
	"uvsphere":         "sphere",
	"uvspheregeometry": "sphere",
}

// knownUnsupportedKinds explains the authored kinds that the typed scene
// package can emit but the CPU rasterizer cannot build. Naming them keeps the
// diagnostic specific instead of suggesting a spelling fix that will not help.
// Each entry states the exact blocker, so the next reader knows what closing
// the gap needs.
var knownUnsupportedKinds = map[string]string{
	"gltf-mesh": "the CPU rasterizer draws named primitives and does not read uploaded vertex buffers",
	"lines":     "the CPU rasterizer draws only the bundle.lit, bundle.unlit, bundle.shadow, and particle pipelines, and skips the bundle.worldLine line-list pipeline",
	"torusknot": "render/bundle builds no torus-knot primitive, so no backend draws this kind natively",
}

// CanRasterizeKind reports whether the CPU preview builds geometry for a
// primitive kind. Tools use it to decide what a native preview can show.
func CanRasterizeKind(kind string) bool {
	_, ok := kindAliases[normalizeKind(kind)]
	return ok
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
	if _, ok := kindAliases[normalized]; ok {
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
	var out []engine.RenderDiagnostic
	directionalUsed := false
	for _, light := range lights {
		kind := normalizeKind(light.Kind)
		if kind == "directional" {
			if !directionalUsed {
				directionalUsed = true
				continue
			}
			out = append(out, unsupported("light", light.ID,
				"native preview reads only the first directional light, so this light does not change the frame"))
			continue
		}
		out = append(out, unsupported("light", light.ID, fmt.Sprintf(
			"native preview ignores light kind %q; the CPU rasterizer reads one directional light plus the environment ambient term", light.Kind)))
	}
	return out
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
