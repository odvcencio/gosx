package scene

import (
	"math"
	"testing"
)

func tubeStraightPath() []Vector3 {
	return []Vector3{{X: 0, Y: -1}, {X: 0, Y: 0}, {X: 0, Y: 1}}
}

func TestTubeGeometryProducesAnIndexedBufferGeometry(t *testing.T) {
	geometry := TubeGeometry(tubeStraightPath(), TubeGeometryOptions{Radius: 0.5, RadialSegments: 6})

	if len(geometry.Indices) == 0 {
		t.Fatal("expected an indexed BufferGeometry")
	}
	wantVertices := (3) * (6 + 1) // one ring per open path point
	if len(geometry.Positions) != wantVertices*3 {
		t.Fatalf("got %d position floats (%d vertices), want %d vertices",
			len(geometry.Positions), len(geometry.Positions)/3, wantVertices)
	}
	if len(geometry.Normals) != wantVertices*3 {
		t.Fatalf("expected normals for every vertex, got %d floats", len(geometry.Normals))
	}
	if len(geometry.UVs) != wantVertices*2 {
		t.Fatalf("expected UVs for every vertex, got %d floats", len(geometry.UVs))
	}
	for _, index := range geometry.Indices {
		if index < 0 || index >= wantVertices {
			t.Fatalf("index %d escapes %d vertices", index, wantVertices)
		}
	}
	for i := 0; i < len(geometry.Positions); i++ {
		if math.IsNaN(geometry.Positions[i]) || math.IsInf(geometry.Positions[i], 0) {
			t.Fatalf("position float %d is non-finite", i)
		}
	}
}

func TestTubeGeometryClosedCountsIncludeTheSeamRing(t *testing.T) {
	path := []Vector3{{Z: -1}, {X: 1}, {Z: 1}, {X: -1}}
	geometry := TubeGeometry(path, TubeGeometryOptions{Radius: 0.25, RadialSegments: 5, Closed: true})

	// Four path points plus the duplicated seam ring.
	wantVertices := (4 + 1) * (5 + 1)
	if got := len(geometry.Positions) / 3; got != wantVertices {
		t.Fatalf("vertex count = %d, want %d", got, wantVertices)
	}
	// The closing edge draws quads too: 4 segments * 5 radial steps.
	if got := len(geometry.Indices) / 6; got != 20 {
		t.Fatalf("triangle count = %d, want 20", got)
	}
}

func TestTubeGeometryLoweringExpansionReportsTheDrawnVertexCount(t *testing.T) {
	geometry := TubeGeometry(tubeStraightPath(), TubeGeometryOptions{Radius: 0.5, RadialSegments: 8})

	vertices := bufferGeometryVertices(geometry)
	if vertices == nil {
		t.Fatal("expected lowered inline vertices")
	}
	// Indexed shared vertices expand to three per triangle:
	// 2 path segments * 8 radial segments = 16 quads = 96 drawn vertices.
	if vertices.Count != 2*8*6 {
		t.Fatalf("drawn vertex count = %d, want %d", vertices.Count, 2*8*6)
	}
	if len(vertices.Positions) != vertices.Count*3 {
		t.Fatalf("expanded positions carry %d floats for count %d", len(vertices.Positions), vertices.Count)
	}
	if len(vertices.Normals) == 0 || len(vertices.UVs) == 0 {
		t.Fatal("the wire payload must carry normals and UVs")
	}

	props := Props{Graph: NewGraph(Mesh{
		ID:       "tube",
		Geometry: geometry,
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
	if obj.Vertices == nil || obj.Vertices.Count != vertices.Count {
		t.Fatalf("lowered object carries %+v, want count %d", obj.Vertices, vertices.Count)
	}
}

func TestTubeGeometryRejectsInvalidPathsWithAnEmptyGeometry(t *testing.T) {
	cases := []struct {
		name string
		path []Vector3
		opts TubeGeometryOptions
	}{
		{"one point", []Vector3{{}}, TubeGeometryOptions{}},
		{"duplicate points", []Vector3{{}, {}}, TubeGeometryOptions{}},
		{"non-finite coordinate", []Vector3{{X: math.NaN()}, {X: 1}}, TubeGeometryOptions{}},
		{"closed with two points", []Vector3{{}, {X: 1}}, TubeGeometryOptions{Closed: true}},
		{"nil path", nil, TubeGeometryOptions{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			geometry := TubeGeometry(tc.path, tc.opts)
			if len(geometry.Positions) != 0 || len(geometry.Normals) != 0 ||
				len(geometry.UVs) != 0 || len(geometry.Indices) != 0 {
				t.Fatalf("expected a zero-value BufferGeometry, got %+v", geometry)
			}
		})
	}
}

func TestTubeGeometryIsDeterministic(t *testing.T) {
	first := TubeGeometry(tubeStraightPath(), TubeGeometryOptions{Radius: 0.75, RadialSegments: 10, Closed: false})
	second := TubeGeometry(tubeStraightPath(), TubeGeometryOptions{Radius: 0.75, RadialSegments: 10, Closed: false})
	if len(first.Positions) != len(second.Positions) || len(first.Indices) != len(second.Indices) {
		t.Fatal("repeated calls produced differently sized buffers")
	}
	for i := range first.Positions {
		if first.Positions[i] != second.Positions[i] {
			t.Fatalf("positions differ at float %d", i)
		}
	}
}
