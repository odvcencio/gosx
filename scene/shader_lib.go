package scene

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// shaderLibThreshold is the minimum byte length of a shader source string that
// qualifies for hoisting into shaderLib. Strings shorter than this are never
// hoisted even if they repeat.
const shaderLibThreshold = 1024

// shaderLibRefSuffix is appended to a field name to form the ref field name.
// e.g. "computeWGSL" → "computeWGSLRef".
const shaderLibRefSuffix = "Ref"

// shaderLibIDPrefix is the prefix for content-hash shader lib IDs.
const shaderLibIDPrefix = "sl:"

// shaderLibFields is the registry of {collection key, field name} pairs that
// are candidates for hoisting. The collection key is the top-level scene JSON
// key (e.g. "computeParticles"); the field name is the key within each item.
//
// To add a new field: append an entry here — no other marshal-side code changes
// are needed.
var shaderLibFields = []shaderLibFieldDesc{
	{collection: "computeParticles", field: "computeWGSL"},
	// WaterSystem Elio/Selena GPU pipeline fields.
	{collection: "waterSystems", field: "seedWGSL"},
	{collection: "waterSystems", field: "dropWGSL"},
	{collection: "waterSystems", field: "displacementWGSL"},
	{collection: "waterSystems", field: "simulationWGSL"},
	{collection: "waterSystems", field: "normalWGSL"},
	{collection: "waterSystems", field: "causticsWGSL"},
	{collection: "waterSystems", field: "poolVertexWGSL"},
	{collection: "waterSystems", field: "poolFragmentWGSL"},
	{collection: "waterSystems", field: "surfaceVertexWGSL"},
	{collection: "waterSystems", field: "surfaceFragmentWGSL"},
	{collection: "waterSystems", field: "surfaceBelowFragmentWGSL"},
	{collection: "waterSystems", field: "objectShadowWGSL"},
	{collection: "waterSystems", field: "objectMeshShadowVertexWGSL"},
	{collection: "waterSystems", field: "objectMeshShadowFragmentWGSL"},
	{collection: "objects", field: "customVertex"},
	{collection: "objects", field: "customFragment"},
	{collection: "objects", field: "customVertexWGSL"},
	{collection: "objects", field: "customFragmentWGSL"},
	{collection: "models", field: "customVertex"},
	{collection: "models", field: "customFragment"},
	{collection: "models", field: "customVertexWGSL"},
	{collection: "models", field: "customFragmentWGSL"},
	// Points authored-material fields.
	{collection: "points", field: "customVertex"},
	{collection: "points", field: "customFragment"},
	{collection: "points", field: "customVertexWGSL"},
	{collection: "points", field: "customFragmentWGSL"},
	// ComputeParticles render-pass authored material fields.
	{collection: "computeParticles", field: "renderVertex"},
	{collection: "computeParticles", field: "renderFragment"},
	{collection: "computeParticles", field: "renderVertexWGSL"},
	{collection: "computeParticles", field: "renderFragmentWGSL"},
	// CustomPost post-effect authored shader fields.
	{collection: "postEffects", field: "fragmentWGSL"},
	{collection: "postEffects", field: "vertexWGSL"},
	{collection: "postEffects", field: "fragmentGLSL"},
	{collection: "postEffects", field: "vertexGLSL"},
	// Named material profile authored shader fields (from composable <Material>
	// elements — same envelope as objects/points so one .sel shader can serve
	// all ~21 galaxy profiles via a single shaderLib entry after dedup).
	{collection: "materials", field: "customVertex"},
	{collection: "materials", field: "customFragment"},
	{collection: "materials", field: "customVertexWGSL"},
	{collection: "materials", field: "customFragmentWGSL"},
	// InstancedMesh Elio GPU cull kernel — hoisted when ≥2 meshes share the
	// same kernel source. Mirror entry exists in JS SHADER_LIB_FIELDS (10-runtime-scene-core.js).
	{collection: "instancedMeshes", field: "cullKernelWGSL"},
}

// shaderLibFieldDesc identifies a single hoistable shader field by its
// containing collection and its field name.
type shaderLibFieldDesc struct {
	collection string // e.g. "computeParticles"
	field      string // e.g. "computeWGSL"
}

// shaderLibID returns the canonical lib ID for source s: first 16 hex
// chars of SHA-256, prefixed "sl:".
func shaderLibID(s string) string {
	h := sha256.Sum256([]byte(s))
	return shaderLibIDPrefix + fmt.Sprintf("%x", h[:8])
}

// applyShaderLib walks the scene wire map (as produced by SceneIR.legacyProps
// or SceneIR JSON marshal), collects qualifying shader strings, and — when any
// string appears more than once — hoists duplicates into a top-level
// "shaderLib" map and replaces the inline field with a sibling "*Ref" field.
//
// Policy: hoist only when the string appears ≥2 times across ALL hoistable
// fields in the scene. Strings that appear exactly once are left inline; this
// avoids inflating single-system scenes with an unnecessary lib map entry.
//
// The function mutates props in-place and returns it unchanged if no hoisting
// occurs (no allocation, no copy). A nil or empty scene map is a no-op.
func applyShaderLib(props map[string]any) map[string]any {
	if len(props) == 0 {
		return props
	}

	// Pass 1: count occurrences of each qualifying string value.
	counts := map[string]int{} // source → count
	for _, desc := range shaderLibFields {
		sceneIRNodeIterator(props, desc.collection, func(node map[string]any) {
			src, ok := node[desc.field].(string)
			if !ok || len(src) < shaderLibThreshold {
				return
			}
			counts[src]++
		})
	}

	// Identify duplicates (count ≥ 2).
	lib := map[string]string{} // id → source (deduped)
	for src, cnt := range counts {
		if cnt >= 2 {
			id := shaderLibID(src)
			lib[id] = src
		}
	}
	if len(lib) == 0 {
		return props // nothing to hoist
	}

	// Build a reverse index: source → id, for fast lookup during replacement.
	sourceToID := make(map[string]string, len(lib))
	for id, src := range lib {
		sourceToID[src] = id
	}

	// Pass 2: replace duplicate inline fields with *Ref fields.
	for _, desc := range shaderLibFields {
		sceneIRNodeIterator(props, desc.collection, func(node map[string]any) {
			src, ok := node[desc.field].(string)
			if !ok {
				return
			}
			id, isDup := sourceToID[src]
			if !isDup {
				return
			}
			refKey := desc.field + shaderLibRefSuffix
			node[refKey] = id
			delete(node, desc.field)
		})
	}

	// Add or merge the top-level shaderLib map. Existing entries can be present
	// when a deduped SceneIR is re-marshaled after a Go round trip.
	libMap := make(map[string]string, len(lib))
	if existing, ok := props["shaderLib"].(map[string]string); ok {
		for id, src := range existing {
			libMap[id] = src
		}
	} else if existingAny, ok := props["shaderLib"].(map[string]any); ok {
		for id, value := range existingAny {
			if src, ok := value.(string); ok {
				libMap[id] = src
			}
		}
	}
	for id, src := range lib {
		libMap[id] = src
	}
	props["shaderLib"] = libMap

	return props
}

// inflateShaderLib reverses applyShaderLib on a scene wire map (used on the Go
// server side when consuming scene bundles that may have been deduped). It
// walks the same field registry, replaces each "*Ref" field whose value exists
// in scene["shaderLib"] with the inflated source string, and removes the
// "shaderLib" key when done. Missing lib entries are silently ignored so that
// partial or future-authored payloads do not crash.
//
// If scene has no "shaderLib" key this is a no-op.
func inflateShaderLib(scene map[string]any) {
	lib, ok := scene["shaderLib"].(map[string]string)
	if !ok {
		// Try map[string]any (round-tripped through JSON.Unmarshal).
		libAny, ok2 := scene["shaderLib"].(map[string]any)
		if !ok2 || len(libAny) == 0 {
			return
		}
		lib = make(map[string]string, len(libAny))
		for k, v := range libAny {
			if s, ok := v.(string); ok {
				lib[k] = s
			}
		}
	}
	if len(lib) == 0 {
		return
	}

	for _, desc := range shaderLibFields {
		sceneIRNodeIterator(scene, desc.collection, func(node map[string]any) {
			refKey := desc.field + shaderLibRefSuffix
			id, ok := node[refKey].(string)
			if !ok {
				return
			}
			if src, found := lib[id]; found {
				node[desc.field] = src
			}
			delete(node, refKey)
		})
	}
	delete(scene, "shaderLib")
}

// sceneIRNodeIterator calls fn for each item in scene[collection], converting
// the collection to a normalized []any form if needed. It handles both
// []any (from JSON unmarshal) and []map[string]any (from legacyProps()).
func sceneIRNodeIterator(scene map[string]any, collection string, fn func(node map[string]any)) {
	v, ok := scene[collection]
	if !ok {
		return
	}
	switch items := v.(type) {
	case []any:
		for _, item := range items {
			if node, ok := item.(map[string]any); ok {
				fn(node)
			}
		}
	case []map[string]any:
		for _, node := range items {
			fn(node)
		}
	}
}

// sortedShaderLibKeys returns the sorted keys of the shaderLib map. Used by
// tests to assert deterministic output.
func sortedShaderLibKeys(lib map[string]string) []string {
	keys := make([]string, 0, len(lib))
	for k := range lib {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
