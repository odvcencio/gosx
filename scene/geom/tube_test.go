package geom

import (
	"math"
	"reflect"
	"testing"
)

// straightYPath runs from (0,-1,0) to (0,1,0), the easiest frame to reason
// about: every tangent is +Y.
func straightYPath() []float64 {
	return []float64{0, -1, 0, 0, 0, 0, 0, 1, 0}
}

func vecAt(flat []float64, i, stride int) (a, b, c float64) {
	base := i * stride
	return flat[base], flat[base+1], flat[base+2]
}

// pairAt reads one two-component value, such as a UV pair.
func pairAt(flat []float64, i int) (u, v float64) {
	return flat[i*2], flat[i*2+1]
}

func TestTubeVertexCountTable(t *testing.T) {
	cases := []struct {
		name           string
		points         int
		radialSegments int
		closed         bool
		want           int
	}{
		{"open default", 4, 0, false, 4 * 9},
		{"open explicit", 3, 12, false, 3 * 13},
		{"closed default", 5, 0, true, 6 * 9},
		{"closed minimum", 3, 3, true, 4 * 4},
		{"open below minimum clamps", 2, 1, false, 2 * 4},
		{"closed below minimum clamps", 3, 0, true, 4 * 9},
		{"above maximum clamps", 4, 1000, false, 4 * 129},
		{"too few open points", 1, 8, false, 0},
		{"zero open points", 0, 8, false, 0},
		{"two closed points", 2, 8, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TubeVertexCount(tc.points, tc.radialSegments, tc.closed); got != tc.want {
				t.Fatalf("TubeVertexCount(%d, %d, %v) = %d, want %d", tc.points, tc.radialSegments, tc.closed, got, tc.want)
			}
		})
	}
}

func TestTubeVertexAndIndexCountsMatchTheContract(t *testing.T) {
	cases := []struct {
		name           string
		points         int
		radialSegments int
		closed         bool
	}{
		{"open", 4, 8, false},
		{"closed", 4, 8, true},
		{"open minimum radial", 5, 3, false},
		{"closed minimum radial", 3, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := make([]float64, tc.points*3)
			for i := range path {
				path[i] = float64(i) * 0.25 // arbitrary distinct finite values
			}
			mesh := Tube(path, 1, tc.radialSegments, tc.closed, AllAttributes)
			if mesh == nil {
				t.Fatal("expected a mesh")
			}
			wantVertices := TubeVertexCount(tc.points, tc.radialSegments, tc.closed)
			if got := mesh.VertexCount(); got != wantVertices {
				t.Fatalf("vertex count = %d, want %d", got, wantVertices)
			}
			steps := tc.points - 1
			if tc.closed {
				steps = tc.points
			}
			wantQuads := steps * tc.radialSegments
			if got := mesh.TriangleCount(); got != 2*wantQuads {
				t.Fatalf("triangle count = %d, want %d", got, 2*wantQuads)
			}
			if len(mesh.Positions) != wantVertices*3 || len(mesh.Normals) != wantVertices*3 || len(mesh.UVs) != wantVertices*2 {
				t.Fatalf("stream lengths do not match %d vertices", wantVertices)
			}
			for _, index := range mesh.Indices {
				if index < 0 || index >= wantVertices {
					t.Fatalf("index %d escapes %d vertices", index, wantVertices)
				}
			}
		})
	}
}

func TestStraightYTubeHoldsRadiusBoundsNormalsAndUVs(t *testing.T) {
	const radius = 0.5
	const radial = 8
	mesh := Tube(straightYPath(), radius, radial, false, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("expected a mesh")
	}
	vertices := mesh.VertexCount()

	// Bounds: the body is a cylinder of the requested radius between y=-1 and y=1.
	maxRadial := 0.0
	minY, maxY := math.Inf(1), math.Inf(-1)
	for v := 0; v < vertices; v++ {
		x, y, z := vecAt(mesh.Positions, v, 3)
		maxRadial = math.Max(maxRadial, math.Hypot(x, z))
		minY = math.Min(minY, y)
		maxY = math.Max(maxY, y)
		if !isFiniteVec(vec3{x, y, z}) {
			t.Fatalf("vertex %d carries a non-finite coordinate", v)
		}
	}
	if math.Abs(maxRadial-radius) > 1e-12 {
		t.Fatalf("max radial extent = %v, want %v", maxRadial, radius)
	}
	if minY != -1 || maxY != 1 {
		t.Fatalf("y range = [%v, %v], want [-1, 1]", minY, maxY)
	}

	// Normals: unit length, finite, perpendicular to +Y, and pointing away
	// from the axis at the same height.
	for v := 0; v < vertices; v++ {
		nx, ny, nz := vecAt(mesh.Normals, v, 3)
		length := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if math.Abs(length-1) > 1e-12 {
			t.Fatalf("normal %d has length %v", v, length)
		}
		if math.Abs(ny) > 1e-12 {
			t.Fatalf("normal %d tilts off the cross section: ny=%v", v, ny)
		}
		px, py, pz := vecAt(mesh.Positions, v, 3)
		if nx*px+nz*pz <= 0 {
			t.Fatalf("normal %d does not point outward from its position (%v,%v,%v)", v, px, py, pz)
		}
	}

	// Winding: every triangle faces outward from the centerline.
	for i := 0; i+2 < len(mesh.Indices); i += 3 {
		a, b, c := mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]
		ax, ay, az := vecAt(mesh.Positions, a, 3)
		bx, by, bz := vecAt(mesh.Positions, b, 3)
		cx, cy, cz := vecAt(mesh.Positions, c, 3)
		u := vec3{bx - ax, by - ay, bz - az}
		w := vec3{cx - ax, cy - ay, cz - az}
		n := crossVec(u, w)
		centroid := vec3{(ax + bx + cx) / 3, (ay + by + cy) / 3, (az + bz + cz) / 3}
		axisPoint := vec3{X: 0, Y: centroid.Y, Z: 0}
		outward := subVec(centroid, axisPoint)
		if dotVec(n, outward) <= 0 {
			t.Fatalf("triangle %d is not wound outward: normal=(%v,%v,%v)", i/3, n.X, n.Y, n.Z)
		}
	}

	// UVs: U wraps 0..1 around each ring; V spans the centerline endpoints.
	stride := radial + 1
	for i := 0; i < vertices/stride; i++ {
		u0, _ := pairAt(mesh.UVs, i*stride)
		u1, _ := pairAt(mesh.UVs, i*stride+radial)
		if u0 != 0 {
			t.Fatalf("ring %d starts at U=%v, want 0", i, u0)
		}
		if u1 != 1 {
			t.Fatalf("ring %d ends at U=%v, want the distinct seam value 1", i, u1)
		}
	}
	_, v0 := pairAt(mesh.UVs, 0)
	_, vLast := pairAt(mesh.UVs, vertices-1)
	if v0 != 0 {
		t.Fatalf("first V = %v, want 0", v0)
	}
	if vLast != 1 {
		t.Fatalf("last V = %v, want 1", vLast)
	}
}

func TestBentPathKeepsFiniteOrthonormalFramesWithoutFlips(t *testing.T) {
	// The path turns through two axes and ends climbing straight up, where a
	// fixed world-up cross product would collapse or flip.
	path := []float64{
		0, 0, 0,
		3, 0, 0,
		3, 0, 3,
		0, 0, 3,
		0, 4, 3,
	}
	mesh := Tube(path, 0.4, 16, false, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("expected a mesh")
	}
	stride := 17
	points := len(path) / 3
	prevNormal := vec3{}
	for i := 0; i < mesh.VertexCount()/stride; i++ {
		// Mirror the generator's tangent choice: one-sided at the endpoints,
		// centered in between.
		var tangent vec3
		switch {
		case i == 0:
			tangent = subVec(vec3{path[3], path[4], path[5]}, vec3{path[0], path[1], path[2]})
		case i == points-1:
			tangent = subVec(
				vec3{path[(points-1)*3], path[(points-1)*3+1], path[(points-1)*3+2]},
				vec3{path[(points-2)*3], path[(points-2)*3+1], path[(points-2)*3+2]},
			)
		default:
			tangent = subVec(
				vec3{path[(i+1)*3], path[(i+1)*3+1], path[(i+1)*3+2]},
				vec3{path[(i-1)*3], path[(i-1)*3+1], path[(i-1)*3+2]},
			)
		}
		tangent = normalize(tangent)
		if !isFiniteVec(tangent) {
			t.Fatalf("frame %d has a non-finite tangent", i)
		}
		for j := 0; j < stride; j++ {
			v := i*stride + j
			nx, ny, nz := vecAt(mesh.Normals, v, 3)
			normal := vec3{nx, ny, nz}
			if !isFiniteVec(normal) {
				t.Fatalf("frame %d vertex %d carries a non-finite normal", i, v)
			}
			if math.Abs(lengthSquared(normal)-1) > 1e-9 {
				t.Fatalf("normal at frame %d vertex %d is not unit: |n|^2=%v", i, v, lengthSquared(normal))
			}
			if math.Abs(dotVec(normal, tangent)) > 1e-9 {
				t.Fatalf("normal at frame %d is not perpendicular to the tangent: dot=%v", i, dotVec(normal, tangent))
			}
			if j == 0 {
				if i > 0 && dotVec(normal, prevNormal) < 0 {
					t.Fatalf("frame %d flips against frame %d: dot=%v", i, i-1, dotVec(normal, prevNormal))
				}
				prevNormal = normal
			}
		}
	}
}

func TestClosedTubeDuplicatesTheFirstRingAtVOne(t *testing.T) {
	// A closed square loop in the XZ plane.
	path := []float64{
		0, 0, 0,
		2, 0, 0,
		2, 0, 2,
		0, 0, 2,
	}
	const radius = 0.25
	const radial = 6
	mesh := Tube(path, radius, radial, true, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("expected a mesh")
	}
	stride := radial + 1
	firstRing := 0
	lastRing := mesh.VertexCount() - stride
	if lastRing != 4*stride {
		t.Fatalf("closed seam ring starts at %d, want %d", lastRing, 4*stride)
	}
	for j := 0; j < stride; j++ {
		a, b := lastRing+j, firstRing+j
		px, py, pz := vecAt(mesh.Positions, a, 3)
		qx, qy, qz := vecAt(mesh.Positions, b, 3)
		if px != qx || py != qy || pz != qz {
			t.Fatalf("seam vertex %d sits at (%v,%v,%v), duplicate of (%v,%v,%v)", j, px, py, pz, qx, qy, qz)
		}
		nx, ny, nz := vecAt(mesh.Normals, a, 3)
		mx, my, mz := vecAt(mesh.Normals, b, 3)
		if nx != mx || ny != my || nz != mz {
			t.Fatalf("seam normal %d is (%v,%v,%v), duplicate of (%v,%v,%v)", j, nx, ny, nz, mx, my, mz)
		}
		_, vSeam := pairAt(mesh.UVs, a)
		_, vFirst := pairAt(mesh.UVs, b)
		if vFirst != 0 {
			t.Fatalf("first ring V = %v, want 0", vFirst)
		}
		if vSeam != 1 {
			t.Fatalf("seam ring V = %v, want 1", vSeam)
		}
	}

	// The closing edge keeps the same outward winding as every other edge:
	// every triangle must face away from its own stretch of the centerline.
	for i := 0; i+2 < len(mesh.Indices); i += 3 {
		a, b, c := mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]
		ax, ay, az := vecAt(mesh.Positions, a, 3)
		bx, by, bz := vecAt(mesh.Positions, b, 3)
		cx, cy, cz := vecAt(mesh.Positions, c, 3)
		n := crossVec(
			vec3{bx - ax, by - ay, bz - az},
			vec3{cx - ax, cy - ay, cz - az},
		)
		centroid := vec3{(ax + bx + cx) / 3, (ay + by + cy) / 3, (az + bz + cz) / 3}
		closest := closestOnLoop(path, centroid)
		if dotVec(n, subVec(centroid, closest)) <= 0 {
			t.Fatalf("triangle %d is not wound outward across the closing edge", i/3)
		}
	}
}

// closestOnLoop returns the point of the closed XZ-plane loop nearest to p.
func closestOnLoop(loop []float64, p vec3) vec3 {
	points := len(loop) / 3
	best := vec3{}
	bestDist := math.Inf(1)
	for i := 0; i < points; i++ {
		j := (i + 1) % points
		a := vec3{loop[i*3], loop[i*3+1], loop[i*3+2]}
		b := vec3{loop[j*3], loop[j*3+1], loop[j*3+2]}
		ab := subVec(b, a)
		t := dotVec(subVec(p, a), ab) / lengthSquared(ab)
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		candidate := addVec(a, scaleVec(ab, t))
		if d := lengthSquared(subVec(p, candidate)); d < bestDist {
			bestDist = d
			best = candidate
		}
	}
	return best
}

func TestTubeRejectsInvalidPaths(t *testing.T) {
	cases := []struct {
		name   string
		path   []float64
		closed bool
	}{
		{"length not divisible by three", []float64{0, 0, 0, 1, 0}, false},
		{"NaN coordinate", []float64{math.NaN(), 0, 0, 1, 0, 0}, false},
		{"Inf coordinate", []float64{0, math.Inf(1), 0, 1, 0, 0}, false},
		{"one open point", []float64{0, 0, 0}, false},
		{"empty open path", nil, false},
		{"two closed points", []float64{0, 0, 0, 1, 0, 0}, true},
		{"consecutive duplicates", []float64{0, 0, 0, 0, 0, 0, 1, 0, 0}, false},
		{"closed wrap duplicates first", []float64{0, 0, 0, 1, 0, 0, 0, 0, 0}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if mesh := Tube(tc.path, 1, 8, tc.closed, AllAttributes); mesh != nil {
				t.Fatalf("expected nil for %s, got %+v", tc.name, mesh)
			}
		})
	}
}

func TestTubeDefaultsRadiusAndClampsRadialSegments(t *testing.T) {
	openPath := straightYPath()
	defaulted := Tube(openPath, 0, 0, false, AllAttributes)
	explicit := Tube(openPath, 1, 8, false, AllAttributes)
	if defaulted == nil || explicit == nil {
		t.Fatal("expected meshes")
	}
	if !reflect.DeepEqual(defaulted, explicit) {
		t.Fatal("radius 0 and radial segments 0 must resolve to radius 1 and 8")
	}

	negative := Tube(openPath, -2, -4, false, AllAttributes)
	if negative == nil || !reflect.DeepEqual(negative, explicit) {
		t.Fatal("non-positive radius and segment counts must fall back to the defaults")
	}

	clampedLow := Tube(openPath, 1, 1, false, AllAttributes)
	wantLow := Tube(openPath, 1, 3, false, AllAttributes)
	if clampedLow == nil || !reflect.DeepEqual(clampedLow, wantLow) {
		t.Fatal("radial segments below the minimum must clamp to 3")
	}

	clampedHigh := Tube(openPath, 1, 500, false, AllAttributes)
	wantHigh := Tube(openPath, 1, 128, false, AllAttributes)
	if clampedHigh == nil || !reflect.DeepEqual(clampedHigh, wantHigh) {
		t.Fatal("radial segments above the maximum must clamp to 128")
	}
}

func TestPositionsOnlyOmitsNormalsAndUVs(t *testing.T) {
	mesh := Tube(straightYPath(), 1, 8, false, PositionsOnly)
	if mesh == nil {
		t.Fatal("expected a mesh")
	}
	if mesh.Normals != nil {
		t.Fatalf("PositionsOnly must leave Normals nil, got %d floats", len(mesh.Normals))
	}
	if mesh.UVs != nil {
		t.Fatalf("PositionsOnly must leave UVs nil, got %d floats", len(mesh.UVs))
	}
	if mesh.Colors != nil {
		t.Fatalf("PositionsOnly must leave Colors nil, got %d floats", len(mesh.Colors))
	}
	if mesh.VertexCount() != TubeVertexCount(3, 8, false) {
		t.Fatalf("vertex count = %d", mesh.VertexCount())
	}
	if len(mesh.Indices) == 0 {
		t.Fatal("positions-only output still needs indices")
	}
}

func TestClosedTubePinsAnIrregular3DSeamExactly(t *testing.T) {
	path := []float64{
		0, 0, 0,
		2, 0.5, 0,
		2, 1, 3,
		-1, 2, 2,
		-2, 0, 0.5,
	}
	const radial = 7
	mesh := Tube(path, 0.4, radial, true, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("expected a mesh")
	}
	stride := radial + 1
	lastRing := mesh.VertexCount() - stride
	for j := 0; j < stride; j++ {
		first, last := j, lastRing+j
		for component := 0; component < 3; component++ {
			if mesh.Positions[first*3+component] != mesh.Positions[last*3+component] {
				t.Fatalf("seam position %d component %d differs", j, component)
			}
			if mesh.Normals[first*3+component] != mesh.Normals[last*3+component] {
				t.Fatalf("seam normal %d component %d differs", j, component)
			}
		}
	}
}

func TestTubeIsDeeplyDeterministic(t *testing.T) {
	path := []float64{
		0, 0, 0,
		2, 1, 0,
		2, 1, 3,
		0, 0, 3,
	}
	first := Tube(path, 0.75, 10, true, AllAttributes)
	second := Tube(path, 0.75, 10, true, AllAttributes)
	if first == nil || second == nil {
		t.Fatal("expected meshes")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated calls must produce identical buffers")
	}
}
