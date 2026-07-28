package geom

import (
	"math"
	"testing"
)

// hullCase names one base hull and what it must contain.
type hullCase struct {
	name     string
	hull     func() ([]float64, []int)
	points   int
	faces    int
	build    func(float64, int, Attribute) *Mesh
	distinct int
}

func hullCases() []hullCase {
	return []hullCase{
		{"tetrahedron", TetrahedronHull, 4, 4, Tetrahedron, 4},
		{"octahedron", OctahedronHull, 6, 8, Octahedron, 6},
		{"icosahedron", IcosahedronHull, 12, 20, Icosahedron, 12},
		{"dodecahedron", DodecahedronHull, 20, 36, Dodecahedron, 20},
	}
}

// TestBaseHullsFaceOutward proves every base face is wound counter-clockwise as
// seen from outside.
//
// A hull is a convex solid centered on the origin, so the outward direction at
// any face is the direction of that face's own centroid. A face wound the other
// way would give a normal pointing back at the origin. This is the test that
// catches a typo in an index list; a vertex count test cannot.
func TestBaseHullsFaceOutward(t *testing.T) {
	for _, testCase := range hullCases() {
		t.Run(testCase.name, func(t *testing.T) {
			vertices, indices := testCase.hull()
			if got := len(vertices) / 3; got != testCase.points {
				t.Fatalf("hull holds %d points, want %d", got, testCase.points)
			}
			if got := len(indices) / 3; got != testCase.faces {
				t.Fatalf("hull holds %d faces, want %d", got, testCase.faces)
			}
			corner := func(index int) vec3 {
				base := index * 3
				if index < 0 || base+3 > len(vertices) {
					t.Fatalf("face names point %d, which the hull does not hold", index)
				}
				return vec3{vertices[base], vertices[base+1], vertices[base+2]}
			}
			used := map[int]bool{}
			for face := 0; face+3 <= len(indices); face += 3 {
				a, c, d := corner(indices[face]), corner(indices[face+1]), corner(indices[face+2])
				used[indices[face]] = true
				used[indices[face+1]] = true
				used[indices[face+2]] = true
				normal := triangleNormal(a, c, d)
				centroid := scaleVec(addVec(addVec(a, c), d), 1.0/3)
				if dot := dotVec(normal, centroid); dot <= 0 {
					t.Fatalf("face %d faces the origin (dot %.4f); it is wound the wrong way", face/3, dot)
				}
			}
			if len(used) != testCase.distinct {
				t.Fatalf("the faces use %d of the %d hull points; an unused point means a wrong index list",
					len(used), testCase.distinct)
			}
		})
	}
}

// TestPolyhedronVertexCounts pins the count at every detail level, so memory
// reporting cannot drift away from the real upload.
func TestPolyhedronVertexCounts(t *testing.T) {
	for _, testCase := range hullCases() {
		for detail := 0; detail <= 3; detail++ {
			mesh := testCase.build(1, detail, AllAttributes)
			if mesh == nil {
				t.Fatalf("%s detail %d: nil mesh", testCase.name, detail)
			}
			want := PolyhedronVertexCount(testCase.faces, detail)
			if got := mesh.VertexCount(); got != want {
				t.Fatalf("%s detail %d: %d vertices, want %d", testCase.name, detail, got, want)
			}
			if got, expect := mesh.TriangleCount(), testCase.faces*(detail+1)*(detail+1); got != expect {
				t.Fatalf("%s detail %d: %d triangles, want %d", testCase.name, detail, got, expect)
			}
			if got, expect := len(mesh.UVs), mesh.VertexCount()*2; got != expect {
				t.Fatalf("%s detail %d: %d uv numbers, want %d", testCase.name, detail, got, expect)
			}
		}
	}
	// A detail above the cap must clamp, not allocate without bound.
	capped := Icosahedron(1, 99, PositionsOnly)
	if got, want := capped.VertexCount(), PolyhedronVertexCount(20, 5); got != want {
		t.Fatalf("a runaway detail gave %d vertices, want the capped %d", got, want)
	}
}

// TestPolyhedronBoundsSitOnTheSphere proves every vertex lands on the requested
// sphere, and that the box bounds match it.
func TestPolyhedronBoundsSitOnTheSphere(t *testing.T) {
	const radius = 2.5
	for _, testCase := range hullCases() {
		for detail := 0; detail <= 2; detail++ {
			mesh := testCase.build(radius, detail, AllAttributes)
			for i := 0; i+3 <= len(mesh.Positions); i += 3 {
				x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
				if got := math.Sqrt(x*x + y*y + z*z); math.Abs(got-radius) > 1e-9 {
					t.Fatalf("%s detail %d: vertex %d sits %v from the origin, want %v",
						testCase.name, detail, i/3, got, radius)
				}
			}
			lo, hi := meshBounds(mesh)
			for _, value := range []float64{lo.X, lo.Y, lo.Z} {
				if value < -radius-1e-9 {
					t.Fatalf("%s detail %d: bounds reach %v, past -%v", testCase.name, detail, value, radius)
				}
			}
			for _, value := range []float64{hi.X, hi.Y, hi.Z} {
				if value > radius+1e-9 {
					t.Fatalf("%s detail %d: bounds reach %v, past %v", testCase.name, detail, value, radius)
				}
			}
		}
	}
}

// TestPolyhedronWindingAndNormals checks the subdivider keeps every sub-triangle
// facing out, and that the normal rule follows the detail level.
func TestPolyhedronWindingAndNormals(t *testing.T) {
	for _, testCase := range hullCases() {
		for detail := 0; detail <= 2; detail++ {
			mesh := testCase.build(1, detail, AllAttributes)
			assertWindingMatchesNormals(t, testCase.name, mesh, 0)
			assertFiniteUnitNormals(t, testCase.name, mesh)
			// Every face of a convex solid on the origin points away from the
			// origin, whatever the detail.
			for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
				p0, p1, p2 := meshTriangle(mesh, triangle)
				centroid := scaleVec(addVec(addVec(p0, p1), p2), 1.0/3)
				if dot := dotVec(triangleNormal(p0, p1, p2), centroid); dot <= 0 {
					t.Fatalf("%s detail %d: triangle %d faces inward", testCase.name, detail, triangle)
				}
			}
			if detail == 0 {
				continue
			}
			// Above detail zero the normal is the point's own direction, which
			// makes the subdivided solid shade as a sphere.
			for i := 0; i+3 <= len(mesh.Positions); i += 3 {
				want := normalize(vec3{mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]})
				got := vec3{mesh.Normals[i], mesh.Normals[i+1], mesh.Normals[i+2]}
				if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
					t.Fatalf("%s detail %d: vertex %d normal %v, want %v", testCase.name, detail, i/3, got, want)
				}
			}
		}
	}
}

// TestPolyhedronFlatFacesAtDetailZero proves a detail of zero keeps hard edges.
// A smooth normal there would round every facet away.
func TestPolyhedronFlatFacesAtDetailZero(t *testing.T) {
	mesh := Icosahedron(1, 0, AllAttributes)
	for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
		n0, n1, n2 := meshNormals(mesh, triangle)
		if n0 != n1 || n1 != n2 {
			t.Fatalf("triangle %d carries three different normals, so the facet is not flat", triangle)
		}
		p0, p1, p2 := meshTriangle(mesh, triangle)
		want := triangleNormal(p0, p1, p2)
		if math.Abs(dotVec(n0, want)-1) > 1e-9 {
			t.Fatalf("triangle %d normal %v does not match its own plane %v", triangle, n0, want)
		}
	}
}

// TestPolyhedronRejectsDegenerateInput proves a bad hull returns nil instead of
// an empty mesh that reads like a successful build.
func TestPolyhedronRejectsDegenerateInput(t *testing.T) {
	if got := Polyhedron(nil, nil, 1, 0, AllAttributes); got != nil {
		t.Fatal("an empty hull produced a mesh")
	}
	if got := Polyhedron([]float64{0, 0, 0}, []int{0, 0, 0}, 1, 0, AllAttributes); got != nil {
		t.Fatal("a hull with one point produced a mesh")
	}
	vertices, _ := OctahedronHull()
	if got := Polyhedron(vertices, []int{0, 1}, 1, 0, AllAttributes); got != nil {
		t.Fatal("an index list with no whole triangle produced a mesh")
	}
}

// TestPolyhedronSeamAndPoleUVsStayUsable proves the two UV repair passes ran.
//
// A triangle that straddles the azimuth wrap reads one corner near u=1 and the
// others near u=0, which stretches the whole texture across one face. A vertex
// at a pole has no azimuth at all, so its raw u is whatever atan2 returns for
// two zeros.
func TestPolyhedronSeamAndPoleUVsStayUsable(t *testing.T) {
	for _, testCase := range hullCases() {
		for detail := 0; detail <= 2; detail++ {
			mesh := testCase.build(1, detail, AllAttributes)
			for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
				base := triangle * 6
				u0, u1, u2 := mesh.UVs[base], mesh.UVs[base+2], mesh.UVs[base+4]
				high := math.Max(u0, math.Max(u1, u2))
				low := math.Min(u0, math.Min(u1, u2))
				if high > 0.9 && low < 0.1 {
					t.Fatalf("%s detail %d: triangle %d straddles the seam with u %v %v %v",
						testCase.name, detail, triangle, u0, u1, u2)
				}
				for k := 0; k < 3; k++ {
					v := mesh.UVs[base+k*2+1]
					if v < -1e-9 || v > 1+1e-9 {
						t.Fatalf("%s detail %d: triangle %d corner %d has v %v outside [0, 1]",
							testCase.name, detail, triangle, k, v)
					}
				}
				// A pole corner takes the average of the other two, because its
				// own azimuth is undefined.
				p := [3]vec3{}
				p[0], p[1], p[2] = meshTriangle(mesh, triangle)
				for k := 0; k < 3; k++ {
					if math.Hypot(p[k].X, p[k].Z) > 1e-9 {
						continue
					}
					want := (mesh.UVs[base+((k+1)%3)*2] + mesh.UVs[base+((k+2)%3)*2]) / 2
					if got := mesh.UVs[base+k*2]; math.Abs(got-want) > 1e-9 {
						t.Fatalf("%s detail %d: triangle %d pole corner has u %v, want the average %v",
							testCase.name, detail, triangle, got, want)
					}
				}
			}
		}
	}
}
