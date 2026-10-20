package scene

import (
	"math"
	"testing"
)

// TestPolygonGeometryWindingAgreesWithTheDeclaredNormal is the regression test
// for a polygon that declared a normal its own triangles contradicted.
//
// PolygonGeometry writes (0, 1, 0) on every vertex. Package scene/earcut always
// emits counter-clockwise triangles in the flat input plane, and a
// counter-clockwise loop in (x, z) faces -Y, so every polygon shipped with
// triangles wound against the normal they carried.
//
// Compare the geometric normal of each triangle against the shaded normals of
// its own three vertices and require a positive dot product. Do not measure area
// instead: the two existing area tests take an absolute value, so both windings
// pass them. Do not look at a render either — the browser culls no back faces
// and the ray tester hits both sides, so nothing else here can fail.
func TestPolygonGeometryWindingAgreesWithTheDeclaredNormal(t *testing.T) {
	cases := []struct {
		name    string
		polygon []float64
		holes   [][]float64
	}{
		{
			// Clockwise in (x, z).
			name:    "clockwise ring",
			polygon: []float64{0, 0, 0, 4, 4, 4, 4, 0},
		},
		{
			// Counter-clockwise in (x, z). Both directions must produce an
			// upward face, because the author's ring direction is not a
			// statement about which way the polygon faces.
			name:    "counter-clockwise ring",
			polygon: []float64{0, 0, 4, 0, 4, 4, 0, 4},
		},
		{
			name:    "ring with a hole",
			polygon: []float64{0, 0, 10, 0, 10, 10, 0, 10},
			holes:   [][]float64{{2, 2, 8, 2, 8, 8, 2, 8}},
		},
		{
			name:    "concave ring",
			polygon: []float64{0, 0, 6, 0, 6, 6, 3, 3, 0, 6},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			geometry := PolygonGeometry(testCase.polygon, testCase.holes, 1.5)
			if len(geometry.Indices) < 3 {
				t.Fatalf("triangulation is empty: %d indices", len(geometry.Indices))
			}
			for i := 0; i+3 <= len(geometry.Indices); i += 3 {
				a, b, c := geometry.Indices[i], geometry.Indices[i+1], geometry.Indices[i+2]
				gx, gy, gz, ok := polygonTriangleNormal(geometry, a, b, c)
				if !ok {
					t.Fatalf("triangle %d is degenerate", i/3)
				}
				sx, sy, sz := polygonShadedNormal(geometry, a, b, c)
				dot := gx*sx + gy*sy + gz*sz
				if dot <= 0 {
					t.Fatalf("triangle %d is wound against its own normals (dot %.4f): geometric (%.3f, %.3f, %.3f), shaded (%.3f, %.3f, %.3f)",
						i/3, dot, gx, gy, gz, sx, sy, sz)
				}
			}
		})
	}
}

// TestPolygonGeometryEarcutAlwaysWindsCounterClockwise pins the earcut property
// the fix rests on. If earcut ever starts to follow the author's ring direction,
// orientPolygonUp measures the direction itself, so the fix still holds — but
// this test records what the raw output does today.
func TestPolygonGeometryEarcutAlwaysWindsCounterClockwise(t *testing.T) {
	clockwise := []float64{0, 0, 0, 4, 4, 4, 4, 0}
	counterClockwise := []float64{0, 0, 4, 0, 4, 4, 0, 4}
	for _, ring := range [][]float64{clockwise, counterClockwise} {
		signed := signedRingArea(ring)
		geometry := PolygonGeometry(ring, nil, 0)
		total := 0.0
		for i := 0; i+3 <= len(geometry.Indices); i += 3 {
			a, b, c := geometry.Indices[i], geometry.Indices[i+1], geometry.Indices[i+2]
			ax, az := geometry.Positions[a*3], geometry.Positions[a*3+2]
			bx, bz := geometry.Positions[b*3], geometry.Positions[b*3+2]
			cx, cz := geometry.Positions[c*3], geometry.Positions[c*3+2]
			total += ((bx-ax)*(cz-az) - (bz-az)*(cx-ax)) / 2
		}
		// After orientPolygonUp the emitted triangles run clockwise in (x, z),
		// which is the +Y face, whatever the author's ring direction was.
		if total >= 0 {
			t.Fatalf("ring with signed area %.1f produced triangles of signed area %.1f, want negative (a +Y face)", signed, total)
		}
	}
}

// TestPolygonGeometryWindingSurvivesLowering proves the corrected order reaches
// the browser. There is no triangulator in the client: PolygonGeometry lowers to
// kind "gltf-mesh" and bufferGeometryVertices keeps the authored index list over
// the unique vertex streams, so the browser draws whatever order Go emitted —
// both on the CPU pick path and when the runtime dereferences the indices while
// baking wire segments or triangle soup.
func TestPolygonGeometryWindingSurvivesLowering(t *testing.T) {
	props := Props{Graph: NewGraph(Mesh{
		ID:       "polygon-floor",
		Geometry: PolygonGeometry([]float64{0, 0, 4, 0, 4, 4, 0, 4}, nil, 0),
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	vertices := props.SceneIR().Objects[0].Vertices
	if vertices == nil {
		t.Fatal("expected lowered vertices")
	}
	soup := BufferGeometry{Positions: vertices.Positions, Normals: vertices.Normals}
	for i := 0; i+2 < len(vertices.Indices); i += 3 {
		a := int(vertices.Indices[i])
		b := int(vertices.Indices[i+1])
		c := int(vertices.Indices[i+2])
		gx, gy, gz, ok := polygonTriangleNormal(soup, a, b, c)
		if !ok {
			t.Fatalf("lowered triangle %d is degenerate", i/3)
		}
		sx, sy, sz := polygonShadedNormal(soup, a, b, c)
		if dot := gx*sx + gy*sy + gz*sz; dot <= 0 {
			t.Fatalf("lowered triangle %d is wound against its own normals (dot %.4f)", i/3, dot)
		}
	}
}

func polygonTriangleNormal(geometry BufferGeometry, a, b, c int) (float64, float64, float64, bool) {
	ax, ay, az := geometry.Positions[a*3], geometry.Positions[a*3+1], geometry.Positions[a*3+2]
	bx, by, bz := geometry.Positions[b*3], geometry.Positions[b*3+1], geometry.Positions[b*3+2]
	cx, cy, cz := geometry.Positions[c*3], geometry.Positions[c*3+1], geometry.Positions[c*3+2]
	e0x, e0y, e0z := bx-ax, by-ay, bz-az
	e1x, e1y, e1z := cx-ax, cy-ay, cz-az
	nx := e0y*e1z - e0z*e1y
	ny := e0z*e1x - e0x*e1z
	nz := e0x*e1y - e0y*e1x
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	if length < 1e-12 {
		return 0, 0, 0, false
	}
	return nx / length, ny / length, nz / length, true
}

func polygonShadedNormal(geometry BufferGeometry, a, b, c int) (float64, float64, float64) {
	nx := geometry.Normals[a*3] + geometry.Normals[b*3] + geometry.Normals[c*3]
	ny := geometry.Normals[a*3+1] + geometry.Normals[b*3+1] + geometry.Normals[c*3+1]
	nz := geometry.Normals[a*3+2] + geometry.Normals[b*3+2] + geometry.Normals[c*3+2]
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	if length == 0 {
		return 0, 0, 0
	}
	return nx / length, ny / length, nz / length
}

func signedRingArea(ring []float64) float64 {
	total := 0.0
	for i := 0; i+1 < len(ring); i += 2 {
		j := (i + 2) % len(ring)
		total += ring[i]*ring[j+1] - ring[j]*ring[i+1]
	}
	return total / 2
}
