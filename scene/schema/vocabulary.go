package schema

import (
	"fmt"
	"sort"
	"strings"
)

// This file closes the widest authoring gap in the native loop: a misspelled
// kind used to pass every check. The browser runtime coerces an unknown
// geometry kind to a cube and an unknown material kind to flat, and the CPU
// preview draws nothing at all. The author saw a green exit code either way.
//
// Every check below names the record, quotes the value the author wrote, and
// suggests the nearest legal spelling when one exists.

// geometryKinds is the closed set of mesh kinds the Scene3D runtimes accept.
// Unlike materials, geometry has no runtime registry, so anything outside this
// set is a mistake rather than an extension.
var geometryKinds = []string{
	"box",
	"cone",
	"cube",
	"cylinder",
	"gltf-mesh",
	"lines",
	"plane",
	"pyramid",
	"sphere",
	"torus",
	"torusknot",
}

// geometryKindAliases maps accepted spellings onto their canonical kind.
var geometryKindAliases = map[string]string{
	"torus-knot": "torusknot",
}

// materialKinds is the built-in material vocabulary. A project may add more
// through registerSceneMaterialProfile, so an unknown value is a warning by
// default and an error only under strict validation.
var materialKinds = []string{
	"custom",
	"flat",
	"ghost",
	"glass",
	"glow",
	"line-basic",
	"line-dashed",
	"matte",
	"standard",
}

// lightKinds is the closed set of light kinds the Scene3D IR carries.
var lightKinds = []string{
	"ambient",
	"directional",
	"hemisphere",
	"light-probe",
	"point",
	"rect-area",
	"spot",
}

// blendModes is the closed set of material blend modes.
var blendModes = []string{
	"additive",
	"alpha",
	"opaque",
}

// GeometryKinds returns the accepted mesh kinds in sorted order.
func GeometryKinds() []string { return sortedCopy(geometryKinds) }

// MaterialKinds returns the built-in material kinds in sorted order.
func MaterialKinds() []string { return sortedCopy(materialKinds) }

// LightKinds returns the accepted light kinds in sorted order.
func LightKinds() []string { return sortedCopy(lightKinds) }

// validateGeometryKind reports a mesh kind that no Scene3D runtime can draw.
func validateGeometryKind(report *Report, kind, id, path string) {
	normalized := canonicalKind(kind, geometryKindAliases)
	if normalized == "" || contains(geometryKinds, normalized) {
		return
	}
	report.add(Error, "scene.geometry.unknown_kind",
		unknownKindMessage("Geometry kind", kind, normalized, geometryKinds,
			"no Scene3D runtime can draw it; the browser runtime substitutes a cube and the native preview draws nothing"),
		path+".kind", id, kindData(kind, normalized, geometryKinds))
}

// validateMaterialKind reports a material kind outside the built-in set. It
// stays a warning by default because a project may register extra profiles.
func validateMaterialKind(report *Report, kind, id, path string, strict bool) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" || contains(materialKinds, normalized) {
		return
	}
	severity := Warn
	if strict {
		severity = Error
	}
	report.add(severity, "scene.material.unknown_kind",
		unknownKindMessage("Material kind", kind, normalized, materialKinds,
			"the browser runtime falls back to flat unless a material profile with this name is registered"),
		path+".materialKind", id, kindData(kind, normalized, materialKinds))
}

// validateLightKind reports a light kind that no Scene3D runtime lights with.
func validateLightKind(report *Report, kind, id, path string) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" || contains(lightKinds, normalized) {
		return
	}
	report.add(Error, "scene.light.unknown_kind",
		unknownKindMessage("Light kind", kind, normalized, lightKinds,
			"no Scene3D runtime lights the scene with it"),
		path+".kind", id, kindData(kind, normalized, lightKinds))
}

// validateBlendMode reports a blend mode outside the closed set.
func validateBlendMode(report *Report, mode, id, path string) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" || contains(blendModes, normalized) {
		return
	}
	report.add(Error, "scene.material.unknown_blend_mode",
		unknownKindMessage("Blend mode", mode, normalized, blendModes, "it is not one of the supported modes"),
		path+".blendMode", id, kindData(mode, normalized, blendModes))
}

func unknownKindMessage(label, written, normalized string, vocabulary []string, consequence string) string {
	message := fmt.Sprintf("%s %q is not recognized; %s", label, written, consequence)
	if suggestion := nearestValue(normalized, vocabulary); suggestion != "" {
		return message + fmt.Sprintf(". Did you mean %q?", suggestion)
	}
	return message + ". Accepted values are " + strings.Join(sortedCopy(vocabulary), ", ")
}

func kindData(written, normalized string, vocabulary []string) map[string]any {
	data := map[string]any{"value": written, "accepted": sortedCopy(vocabulary)}
	if suggestion := nearestValue(normalized, vocabulary); suggestion != "" {
		data["suggestion"] = suggestion
	}
	return data
}

// canonicalKind lowercases a kind and resolves any accepted alias.
func canonicalKind(kind string, aliases map[string]string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if canonical, ok := aliases[normalized]; ok {
		return canonical
	}
	return normalized
}

// nearestValue returns the closest vocabulary entry when the author probably
// misspelled one. It returns an empty string when nothing is close, so an
// unrelated value never receives a misleading suggestion.
func nearestValue(value string, vocabulary []string) string {
	best, bestDistance := "", 0
	for _, candidate := range sortedCopy(vocabulary) {
		distance := editDistance(value, candidate)
		if best == "" || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	limit := len(value) / 3
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
			current[j] = minOf(previous[j]+1, minOf(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func minOf(a, b int) int {
	if a < b {
		return a
	}
	return b
}
