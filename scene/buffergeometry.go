package scene

// BufferGeometry is raw triangle-mesh geometry: flat vertex buffers produced by
// a mesh generator (CSG, NURBS tessellation, glTF import) rather than a
// parametric primitive. Positions and Normals are flat xyz triples; UVs are
// flat uv pairs. Indices, when present, reference vertices and are expanded
// into a flat (non-indexed) triangle list at lower time.
//
// A Mesh using BufferGeometry lowers to a "gltf-mesh" scene object carrying its
// vertices inline, so it flows through SceneIR and the WebGPU honesty gate just
// like any parametric object — including pickable/picking backend gating.
type BufferGeometry struct {
	Positions []float64
	Normals   []float64
	UVs       []float64
	Indices   []int
}

func (BufferGeometry) sceneGeometry() {}

// legacyGeometry satisfies the Geometry interface. BufferGeometry's vertex data
// is carried through ObjectIR.Vertices by applyGeometryToObjectIR rather than
// the legacy geometry-prop map, so only the kind is reported here.
func (BufferGeometry) legacyGeometry() (string, map[string]any) {
	return "gltf-mesh", nil
}

// MeshVertices carries inline vertex buffers for a BufferGeometry mesh in the
// wire shape the browser runtime consumes (item.vertices): a flat, non-indexed
// triangle list. Positions/Normals are xyz triples and UVs are uv pairs; Count
// is the vertex count (len(Positions)/3).
type MeshVertices struct {
	Positions []float64 `json:"positions,omitempty"`
	Normals   []float64 `json:"normals,omitempty"`
	UVs       []float64 `json:"uvs,omitempty"`
	Count     int       `json:"count"`
}

// bufferGeometryVertices flattens a BufferGeometry into inline MeshVertices.
// Indexed geometry is expanded into a non-indexed list because the runtime
// draws item.vertices as a flat triangle soup (count = len(positions)/3).
// Returns nil for empty geometry so the object simply carries no vertices.
func bufferGeometryVertices(g BufferGeometry) *MeshVertices {
	pos, nrm, uvs := g.Positions, g.Normals, g.UVs
	if len(g.Indices) > 0 {
		pos = expandBufferAttr(g.Positions, g.Indices, 3)
		nrm = expandBufferAttr(g.Normals, g.Indices, 3)
		uvs = expandBufferAttr(g.UVs, g.Indices, 2)
	}
	count := len(pos) / 3
	if count == 0 {
		return nil
	}
	out := &MeshVertices{Count: count, Positions: append([]float64(nil), pos...)}
	if len(nrm) > 0 {
		out.Normals = append([]float64(nil), nrm...)
	}
	if len(uvs) > 0 {
		out.UVs = append([]float64(nil), uvs...)
	}
	return out
}

// expandBufferAttr expands an indexed attribute (stride floats per vertex) into
// a flat per-index list. Out-of-range indices are skipped so malformed input
// can't panic the lowerer.
func expandBufferAttr(src []float64, indices []int, stride int) []float64 {
	if len(src) == 0 || stride <= 0 {
		return nil
	}
	out := make([]float64, 0, len(indices)*stride)
	for _, idx := range indices {
		base := idx * stride
		if idx < 0 || base+stride > len(src) {
			continue
		}
		out = append(out, src[base:base+stride]...)
	}
	return out
}
