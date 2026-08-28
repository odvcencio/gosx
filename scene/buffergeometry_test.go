package scene

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func TestBufferGeometryLowersToInlineVertices(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID: "buf",
		Geometry: BufferGeometry{
			Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0},
			Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1},
			UVs:       []float64{0, 0, 1, 0, 0, 1},
		},
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.Kind != "gltf-mesh" {
		t.Fatalf("expected kind gltf-mesh, got %q", obj.Kind)
	}
	if obj.Vertices == nil {
		t.Fatalf("expected inline Vertices, got nil")
	}
	if obj.Vertices.Count != 3 {
		t.Fatalf("expected Count=3, got %d", obj.Vertices.Count)
	}
	if len(obj.Vertices.Positions) != 9 {
		t.Fatalf("expected 9 position floats, got %d", len(obj.Vertices.Positions))
	}
	// The serialized wire form must carry the vertices the runtime reads.
	b, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"vertices"`) || !strings.Contains(string(b), `"positions"`) {
		t.Fatalf("serialized scene missing inline vertices: %s", b)
	}
}

func TestBufferGeometryPreservesIndexedQuad(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID: "buf",
		Geometry: BufferGeometry{
			Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
			Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
			UVs:       []float64{0, 0, 1, 0, 0, 1, 1, 1},
			Indices:   []int{0, 1, 2, 0, 2, 3},
		},
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	ir := props.SceneIR()
	obj := ir.Objects[0]
	if obj.Vertices == nil || obj.Vertices.Count != 4 {
		t.Fatalf("expected 4 unique vertices, got %+v", obj.Vertices)
	}
	if len(obj.Vertices.Positions) != 12 {
		t.Fatalf("expected 12 position floats (unique vertices, not soup), got %d", len(obj.Vertices.Positions))
	}
	if len(obj.Vertices.Normals) != 12 || len(obj.Vertices.UVs) != 8 {
		t.Fatalf("expected unique normal and uv streams, got %d/%d", len(obj.Vertices.Normals), len(obj.Vertices.UVs))
	}
	if got := obj.Vertices.Indices; !slices.Equal(got, []uint32{0, 1, 2, 0, 2, 3}) {
		t.Fatalf("expected authored indices [0 1 2 0 2 3], got %v", got)
	}
	// The serialized wire form must keep the integer index stream lossless.
	b, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"indices":[0,1,2,0,2,3]`) {
		t.Fatalf("serialized scene missing the index stream: %s", b)
	}
}

func TestBufferGeometryIndexedSnapshotIsIndependentOfSourceSlices(t *testing.T) {
	positions := []float64{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0}
	normals := []float64{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1}
	uvs := []float64{0, 0, 1, 0, 0, 1, 1, 1}
	tangents := []float64{
		1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1,
		1, 0, 0, 1,
	}
	indices := []int{0, 1, 2, 0, 2, 3}
	g := BufferGeometry{
		Positions: positions,
		Normals:   normals,
		UVs:       uvs,
		Tangents:  tangents,
		Indices:   indices,
	}
	vertices := bufferGeometryVertices(g)
	if vertices == nil {
		t.Fatal("expected lowered vertices")
	}
	// Mutating the source slices after lowering must not mutate the snapshot.
	for i := range positions {
		positions[i] = -7
	}
	for i := range normals {
		normals[i] = -7
	}
	for i := range uvs {
		uvs[i] = -7
	}
	for i := range tangents {
		tangents[i] = -7
	}
	for i := range indices {
		indices[i] = 3
	}
	if vertices.Positions[0] != 0 || vertices.Positions[len(vertices.Positions)-1] != 0 {
		t.Fatal("source position mutation leaked into the IR snapshot")
	}
	if vertices.Normals[0] != 0 || vertices.UVs[0] != 0 || vertices.Tangents[0] != 1 {
		t.Fatal("source attribute mutation leaked into the IR snapshot")
	}
	if !slices.Equal(vertices.Indices, []uint32{0, 1, 2, 0, 2, 3}) {
		t.Fatalf("source index mutation leaked into the IR snapshot: %v", vertices.Indices)
	}
}

func TestBufferGeometryFailsClosedOnMalformedIndexStreams(t *testing.T) {
	cases := map[string][]int{
		"negative":      {0, -1, 2, 0, 2, 3},
		"out-of-range":  {0, 1, 4, 0, 2, 3},
		"not-triangles": {0, 1, 2, 3},
		"truncated":     {0, 1, 2, 0, 2},
	}
	for name, indices := range cases {
		g := BufferGeometry{
			Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
			Indices:   indices,
		}
		vertices := bufferGeometryVertices(g)
		if vertices != nil {
			t.Fatalf("%s: malformed index stream lowered to %+v, want nil (fail closed)", name, vertices)
		}
		props := Props{Graph: NewGraph(Mesh{ID: "buf", Geometry: g, Material: StandardMaterial{Color: "#ffffff"}})}
		b, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"indices"`) || strings.Contains(string(b), `"vertices":{`) {
			t.Fatalf("%s: malformed mesh reached the wire: %s", name, b)
		}
	}
}

func TestBufferGeometryUnindexedLoweringIsUnchanged(t *testing.T) {
	g := BufferGeometry{
		Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0},
		Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
	}
	vertices := bufferGeometryVertices(g)
	if vertices == nil || vertices.Count != 6 {
		t.Fatalf("unindexed lowering changed shape: %+v", vertices)
	}
	if len(vertices.Positions) != 18 || len(vertices.Normals) != 18 {
		t.Fatalf("unindexed streams must pass through untouched: %d/%d", len(vertices.Positions), len(vertices.Normals))
	}
	if vertices.Indices != nil {
		t.Fatalf("unindexed geometry must not grow an index stream: %v", vertices.Indices)
	}
}

func TestBufferGeometryCarriesExplicitRetainedSnapshotContract(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID: "retained",
		Geometry: BufferGeometry{
			Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0},
			Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1},
			UVs:       []float64{0, 0, 1, 0, 0, 1},
			Tangents:  []float64{1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1},
			Immutable: true,
			Revision:  7,
		},
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	vertices := props.SceneIR().Objects[0].Vertices
	if vertices == nil || !vertices.Immutable || vertices.Dynamic {
		t.Fatalf("unexpected retained contract: %+v", vertices)
	}
	if vertices.Revision == nil || *vertices.Revision != 7 {
		t.Fatalf("expected revision 7, got %+v", vertices.Revision)
	}
	if len(vertices.Tangents) != 12 {
		t.Fatalf("expected tangents to survive lowering, got %d", len(vertices.Tangents))
	}
}

func TestBufferGeometryDefaultsToRevisionlessMutableContract(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID:       "mutable",
		Geometry: BufferGeometry{Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0}},
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	vertices := props.SceneIR().Objects[0].Vertices
	if vertices == nil || vertices.Immutable || vertices.Revision != nil || vertices.Dynamic {
		t.Fatalf("default geometry must remain fail-closed: %+v", vertices)
	}
}

// A non-pickable buffer mesh must add no backend constraint: all backends stay
// capable (BufferGeometry must not gratuitously force WebGL).
func TestBufferGeometryKeepsWebGPUCapable(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID:       "buf",
		Geometry: BufferGeometry{Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0}},
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps")
	}
	got := backendSet(ir.BackendCaps.Capable)
	if !got[capability.BackendWebGPU] {
		t.Fatalf("expected WebGPU capable for a plain buffer mesh, got %v", ir.BackendCaps.Capable)
	}
}

// A pickable buffer mesh keeps WebGPU, exactly like a pickable parametric
// mesh — this is the kiln viewport's case. The honesty gate used to force it to
// WebGL2 because gpu-picking was WebGPU-false; both GPU backends now implement
// picking. See the FeatureGPUPicking comment in scene/capability/capability.go.
func TestBufferGeometryPickableKeepsWebGPU(t *testing.T) {
	pickable := true
	props := Props{Graph: NewGraph(Mesh{
		ID:       "buf",
		Geometry: BufferGeometry{Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0}},
		Material: StandardMaterial{Color: "#ffffff"},
		Pickable: &pickable,
	})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps")
	}
	got := backendSet(ir.BackendCaps.Capable)
	if !got[capability.BackendWebGPU] {
		t.Fatalf("expected WebGPU capable for a pickable buffer mesh, got %v", ir.BackendCaps.Capable)
	}
	if got[capability.BackendCanvas2D] {
		t.Fatalf("expected canvas2d excluded for gpu-picking, got %v", ir.BackendCaps.Capable)
	}
}
