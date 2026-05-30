package scene

import (
	"encoding/json"
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

func TestBufferGeometryExpandsIndices(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID: "buf",
		Geometry: BufferGeometry{
			Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
			Indices:   []int{0, 1, 2, 0, 2, 3},
		},
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	ir := props.SceneIR()
	obj := ir.Objects[0]
	if obj.Vertices == nil || obj.Vertices.Count != 6 {
		t.Fatalf("expected 6 expanded vertices, got %+v", obj.Vertices)
	}
	if len(obj.Vertices.Positions) != 18 {
		t.Fatalf("expected 18 position floats after expand, got %d", len(obj.Vertices.Positions))
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

// A pickable buffer mesh forces WebGL via the honesty gate, exactly like a
// pickable parametric mesh — this is the kiln viewport's case.
func TestBufferGeometryPickableForcesWebGL(t *testing.T) {
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
	if len(ir.BackendCaps.Capable) != 1 || ir.BackendCaps.Capable[0] != capability.BackendWebGL {
		t.Fatalf("expected Capable==[webgl], got %v", ir.BackendCaps.Capable)
	}
}
