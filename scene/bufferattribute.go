package scene

import (
	"math"
	"sort"
)

// BufferAttribute is one named per-vertex float attribute attached to a
// BufferGeometry: a flat float value stream plus its component count
// (ItemSize). ItemSize 1..4 correspond to float/vec2/vec3/vec4 in authored
// Selena shaders. Data holds exactly Count*ItemSize finite values for a
// geometry lowering to Count unique vertices.
//
// Custom attributes are part of the explicit retained snapshot contract: they
// are only lowered for Immutable, non-Dynamic geometry whose Revision pins the
// snapshot, because the CPU-baked mutable path cannot carry unnamed extra
// streams through world baking without a broader redesign. Anything else fails
// closed in bufferGeometryVertices rather than silently dropping the stream.
type BufferAttribute struct {
	Data     []float64
	ItemSize int
}

// MeshAttribute is the wire shape of one custom attribute on MeshVertices:
// exactly Count*ItemSize finite float values for the mesh's unique vertex
// count. It serializes compactly next to the built-in streams:
//
//	"attributes": { "heat": { "data": [...], "itemSize": 1 } }
type MeshAttribute struct {
	Data     []float64 `json:"data"`
	ItemSize int       `json:"itemSize"`
}

// bufferAttributeReservedNames lists the built-in vertex streams a custom
// attribute name must never shadow, in both their BufferGeometry field spellings
// and their common shader-semantic spellings. This set is the single canonical
// collision rule; tests assert against this exact variable.
var bufferAttributeReservedNames = map[string]struct{}{
	"position":  {},
	"positions": {},
	"normal":    {},
	"normals":   {},
	"uv":        {},
	"uvs":       {},
	"uv1":       {},
	"tangent":   {},
	"tangents":  {},
	"index":     {},
	"indices":   {},
	"skin":      {},
	"skinIndex": {},
	"joints":    {},
	"weights":   {},
}

// ValidBufferAttributeName is the canonical custom-attribute name rule shared
// by lowering and tests: a non-empty WGSL/GLSL-compatible shader identifier
// ([A-Za-z_][A-Za-z0-9_]*) that does not collide with a built-in position,
// normal, uv, tangent, index, or skin stream.
func ValidBufferAttributeName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			// Leading or continuation character: fine.
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	if _, reserved := bufferAttributeReservedNames[name]; reserved {
		return false
	}
	return true
}

// sortedBufferAttributeNames returns the map's keys in sorted order. Every
// pass over custom attributes goes through this so validation output,
// snapshots, and GPU slot assignment never depend on Go's randomized map
// iteration order.
func sortedBufferAttributeNames(attributes map[string]BufferAttribute) []string {
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validateBufferAttributes checks every custom stream against the unique
// vertex count before anything is serialized. All four failure classes —
// malformed names, built-in collisions, item sizes outside [1,4], and wrong-
// length or non-finite data — fail closed together so no malformed vertex
// payload can ever reach the wire or a partial GPU fetch. Names are visited in
// sorted order so the first rejection is deterministic regardless of caller
// map insertion order.
func validateBufferAttributes(attributes map[string]BufferAttribute, count int) bool {
	for _, name := range sortedBufferAttributeNames(attributes) {
		attr := attributes[name]
		if !ValidBufferAttributeName(name) {
			return false
		}
		if attr.ItemSize < 1 || attr.ItemSize > 4 {
			return false
		}
		if len(attr.Data) != count*attr.ItemSize {
			return false
		}
		for _, v := range attr.Data {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
	}
	return true
}

// lowerCustomAttributes copies validated custom streams into fresh slices so
// later caller mutation cannot alter the IR snapshot. Callers must run
// validateBufferAttributes first; names are inserted in sorted order purely as
// documentation of determinism — map insertion order never affects reads.
func lowerCustomAttributes(attributes map[string]BufferAttribute) map[string]MeshAttribute {
	if len(attributes) == 0 {
		return nil
	}
	out := make(map[string]MeshAttribute, len(attributes))
	for _, name := range sortedBufferAttributeNames(attributes) {
		attr := attributes[name]
		data := make([]float64, len(attr.Data))
		copy(data, attr.Data)
		out[name] = MeshAttribute{Data: data, ItemSize: attr.ItemSize}
	}
	return out
}
