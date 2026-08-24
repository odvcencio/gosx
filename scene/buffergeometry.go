package scene

// BufferGeometry is raw triangle-mesh geometry: flat vertex buffers produced by
// a mesh generator (CSG, NURBS tessellation, glTF import) rather than a
// parametric primitive. Positions and Normals are flat xyz triples; UVs are
// flat uv pairs. Indices, when present, reference vertices; lowering keeps the
// unique vertex streams and the authored triangle index list intact so the
// browser can upload an element buffer and draw indexed (see MeshVertices).
//
// A Mesh using BufferGeometry lowers to a "gltf-mesh" scene object carrying its
// vertices inline, so it flows through SceneIR and the WebGPU honesty gate just
// like any parametric object — including pickable/picking backend gating.
type BufferGeometry struct {
	Positions []float64
	Normals   []float64
	UVs       []float64
	Tangents  []float64
	Indices   []int

	// Immutable opts this geometry into renderer-side retained GPU buffers.
	// Authors must treat every attribute slice as immutable for a given
	// Revision. To publish changed data, replace the slices and increment
	// Revision. Revisionless or Dynamic geometry deliberately takes the
	// conservative CPU-baked path.
	Immutable bool
	Revision  uint64
	Dynamic   bool
}

func (BufferGeometry) sceneGeometry() {}

// legacyGeometry satisfies the Geometry interface. BufferGeometry's vertex data
// is carried through ObjectIR.Vertices by applyGeometryToObjectIR rather than
// the legacy geometry-prop map, so only the kind is reported here.
func (BufferGeometry) legacyGeometry() (string, map[string]any) {
	return "gltf-mesh", nil
}

// MeshVertices carries inline vertex buffers for a BufferGeometry mesh in the
// wire shape the browser runtime consumes (item.vertices). When Indices is
// empty the streams are a flat, non-indexed triangle list exactly as before.
// When Indices is present, Positions/Normals/UVs/Tangents hold UNIQUE vertices
// (Positions are xyz triples, UVs uv pairs), Count is the unique position
// vertex count, and Indices is the authored triangle list over those vertices —
// every entry in [0, Count), length divisible by three. The browser runtime
// uploads Indices as a Uint32Array element buffer and draws indexed.
type MeshVertices struct {
	Positions []float64 `json:"positions,omitempty"`
	Normals   []float64 `json:"normals,omitempty"`
	UVs       []float64 `json:"uvs,omitempty"`
	Tangents  []float64 `json:"tangents,omitempty"`
	Indices   []uint32  `json:"indices,omitempty"`
	Count     int       `json:"count"`
	Immutable bool      `json:"immutable,omitempty"`
	Revision  *uint64   `json:"revision,omitempty"`
	Dynamic   bool      `json:"dynamic,omitempty"`
}

// bufferGeometryVertices lowers a BufferGeometry into inline MeshVertices.
// Unindexed geometry keeps its historical flat-triangle-list shape unchanged.
// Indexed geometry preserves its unique vertex streams plus its authored
// triangle indices instead of expanding them into triangle soup, so the
// browser can upload an element buffer and issue indexed draws. Malformed
// indexed geometry fails closed: nil is returned so no partial mesh is ever
// serialized or drawn. Returns nil for empty geometry so the object simply
// carries no vertices.
func bufferGeometryVertices(g BufferGeometry) *MeshVertices {
	count := len(g.Positions) / 3
	if count == 0 {
		return nil
	}
	out := &MeshVertices{
		Count:     count,
		Positions: append([]float64(nil), g.Positions...),
		Immutable: g.Immutable,
		Dynamic:   g.Dynamic,
	}
	if g.Immutable || g.Revision != 0 {
		revision := g.Revision
		out.Revision = &revision
	}
	if len(g.Normals) > 0 {
		out.Normals = append([]float64(nil), g.Normals...)
	}
	if len(g.UVs) > 0 {
		out.UVs = append([]float64(nil), g.UVs...)
	}
	if len(g.Tangents) > 0 {
		out.Tangents = append([]float64(nil), g.Tangents...)
	}
	if len(g.Indices) > 0 {
		if !validBufferTriangleIndices(g.Indices, count) {
			// Fail closed: malformed indices must not reach the wire as a
			// partial mesh or an out-of-range GPU fetch. Dropping the object's
			// vertices leaves nothing to serialize or draw.
			return nil
		}
		indices := make([]uint32, len(g.Indices))
		for i, idx := range g.Indices {
			indices[i] = uint32(idx)
		}
		out.Indices = indices
	}
	return out
}

// validBufferTriangleIndices reports whether an authored index stream forms a
// drawable triangle list over count unique vertices: non-empty, divisible by
// three, and every index within [0, count).
func validBufferTriangleIndices(indices []int, count int) bool {
	if len(indices) < 3 || len(indices)%3 != 0 {
		return false
	}
	for _, idx := range indices {
		if idx < 0 || idx >= count {
			return false
		}
	}
	return true
}
