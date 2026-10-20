package scene

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"math"
	"math/rand"
	"testing"

	"m31labs.dev/gosx/scene/geom"
)

// This file checks the BufferGeometry generators against references that share
// no code with the intersection routines. Every reference states what the solid
// is, in its own terms, and the test compares the raycaster against it.
//
// The flat generators get an analytic reference: intersect the ray with the
// plane, then ask a 2D question. The solid generators get a marching reference,
// the same construction raycast_exact_test.go uses.

// genDrawnIndexCount returns how many vertex fetches the browser performs for a
// generated geometry: len(Indices) when indexed (the runtime dereferences the
// authored index stream), else every unique vertex.
func genDrawnIndexCount(g BufferGeometry) int {
	if len(g.Indices) > 0 {
		return len(g.Indices)
	}
	return len(g.Positions) / 3
}

// genBounds returns the axis-aligned box that holds a generated geometry.
func genBounds(g BufferGeometry) (lo, hi Vector3) {
	lo = Vec3(math.Inf(1), math.Inf(1), math.Inf(1))
	hi = Vec3(math.Inf(-1), math.Inf(-1), math.Inf(-1))
	for i := 0; i+3 <= len(g.Positions); i += 3 {
		lo.X = math.Min(lo.X, g.Positions[i])
		lo.Y = math.Min(lo.Y, g.Positions[i+1])
		lo.Z = math.Min(lo.Z, g.Positions[i+2])
		hi.X = math.Max(hi.X, g.Positions[i])
		hi.Y = math.Max(hi.Y, g.Positions[i+1])
		hi.Z = math.Max(hi.Z, g.Positions[i+2])
	}
	return lo, hi
}

// genRaycast fires one ray at one geometry through the public graph walk and
// through the accelerator, and fails when the two disagree. Both must report the
// mesh-triangle method, because these geometries are triangles and nothing else.
func genRaycast(t *testing.T, geometry Geometry, ray Ray) (RayHit, bool) {
	t.Helper()
	graph := NewGraph(Mesh{ID: "subject", Geometry: geometry})
	hit, ok := RaycastGraph(graph, ray)
	fast, fastOK := NewSceneAccelerator(graph).Raycast(ray)
	if ok != fastOK {
		t.Fatalf("the graph walk and the accelerator disagree on whether the ray hits")
	}
	if ok {
		if hit.Kind != "gltf-mesh" || hit.Method != "mesh-triangle" {
			t.Fatalf("kind/method = %q/%q, want gltf-mesh/mesh-triangle", hit.Kind, hit.Method)
		}
		if fast != hit {
			t.Fatalf("the accelerator reported %#v, the graph walk %#v", fast, hit)
		}
	}
	return hit, ok
}

// genFlatReference answers where a ray meets the plane y=elevation, and whether
// the 2D point passes the caller's own test. It shares nothing with the triangle
// solver.
func genFlatReference(ray Ray, elevation float64, inside func(x, z float64) bool) (float64, bool) {
	if math.Abs(ray.Direction.Y) < 1e-12 {
		return 0, false
	}
	distance := (elevation - ray.Origin.Y) / ray.Direction.Y
	if distance < 0 {
		return 0, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, distance))
	if !inside(point.X, point.Z) {
		return 0, false
	}
	return distance, true
}

// genPointInPolygon answers the even-odd crossing test for one ring list. It is
// the classic ray-crossing count, and it knows nothing about triangles.
func genPointInPolygon(rings [][]float64, x, z float64) bool {
	inside := false
	for _, ring := range rings {
		count := len(ring) / 2
		for i := 0; i < count; i++ {
			j := (i + 1) % count
			xi, zi := ring[i*2], ring[i*2+1]
			xj, zj := ring[j*2], ring[j*2+1]
			if (zi > z) == (zj > z) {
				continue
			}
			crossing := xi + (z-zi)/(zj-zi)*(xj-xi)
			if x < crossing {
				inside = !inside
			}
		}
	}
	return inside
}

// genRandomRays returns rays aimed through a box around the origin, so the mix
// holds hits and misses.
func genRandomRays(seed int64, count int, spread, reach float64) []Ray {
	random := rand.New(rand.NewSource(seed))
	rays := make([]Ray, 0, count)
	for len(rays) < count {
		origin := Vec3(
			random.Float64()*2*reach-reach,
			random.Float64()*2*reach-reach,
			random.Float64()*2*reach-reach,
		)
		target := Vec3(
			random.Float64()*2*spread-spread,
			random.Float64()*2*spread-spread,
			random.Float64()*2*spread-spread,
		)
		direction := normalizeVector(subVectors(target, origin))
		if direction == (Vector3{}) {
			continue
		}
		rays = append(rays, Ray{Origin: origin, Direction: direction})
	}
	return rays
}

// TestGeneratedGeometryVertexCounts pins the drawn vertex count of every new
// generator: len(Indices) for indexed geometry (the runtime dereferences it),
// else every vertex. A memory report and a wire budget both read this number.
func TestGeneratedGeometryVertexCounts(t *testing.T) {
	cases := []struct {
		name     string
		geometry BufferGeometry
		vertices int
	}{
		{"tetrahedron", TetrahedronGeometry(1, 0), 12},
		{"tetrahedron detail 1", TetrahedronGeometry(1, 1), 48},
		{"octahedron", OctahedronGeometry(1, 0), 24},
		{"icosahedron", IcosahedronGeometry(1, 0), 60},
		{"icosahedron detail 2", IcosahedronGeometry(1, 2), 540},
		{"dodecahedron", DodecahedronGeometry(1, 0), 108},
		{"circle 32", CircleGeometry(1, 32, 0, 0), 96},
		{"circle 8", CircleGeometry(1, 8, 0, 0), 24},
		{"ring 32", RingGeometry(0.5, 1, 32, 1, 0, 0), 192},
		{"ring 32 x 2", RingGeometry(0.5, 1, 32, 2, 0, 0), 384},
		{"shape square", ShapeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, 0), 6},
		{"extrude square", ExtrudeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, ExtrudeOptions{Depth: 1}), 36},
	}
	for _, testCase := range cases {
		if got := genDrawnIndexCount(testCase.geometry); got != testCase.vertices {
			t.Fatalf("%s: %d drawn indices/vertices, want %d", testCase.name, got, testCase.vertices)
		}
		if len(testCase.geometry.Normals) != len(testCase.geometry.Positions) {
			t.Fatalf("%s: normals and positions have different lengths", testCase.name)
		}
		if len(testCase.geometry.UVs) != len(testCase.geometry.Positions)/3*2 {
			t.Fatalf("%s: the uv stream does not match the vertex count", testCase.name)
		}
	}
}

// TestGeneratedGeometryBounds pins the extent of every new generator.
//
// A polyhedron does not fill its own sphere on every axis: a tetrahedron only
// reaches radius/sqrt(3) along Y, because none of its four corners sits on an
// axis. The reference derives the true extent from the base hull, so the test
// states the real shape instead of an assumed cube.
func TestGeneratedGeometryBounds(t *testing.T) {
	flat := []struct {
		name     string
		geometry BufferGeometry
		lo, hi   Vector3
	}{
		{"circle", CircleGeometry(2, 64, 0, 0), Vec3(-2, 0, -2), Vec3(2, 0, 2)},
		{"ring", RingGeometry(0.5, 2, 64, 1, 0, 0), Vec3(-2, 0, -2), Vec3(2, 0, 2)},
		{"shape", ShapeGeometry(Shape{Outline: []float64{-1, -2, 3, -2, 3, 4, -1, 4}}, 1.5), Vec3(-1, 1.5, -2), Vec3(3, 1.5, 4)},
		{
			"extrude",
			ExtrudeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, ExtrudeOptions{Depth: 3}),
			Vec3(-1, 0, -1), Vec3(1, 3, 1),
		},
	}
	for _, testCase := range flat {
		lo, hi := genBounds(testCase.geometry)
		assertVectorNear(t, testCase.name+" low", lo, testCase.lo)
		assertVectorNear(t, testCase.name+" high", hi, testCase.hi)
	}

	const radius = 2.0
	hulls := map[string]struct {
		hull  func() ([]float64, []int)
		build func(float64, int) BufferGeometry
	}{
		"tetrahedron":  {geom.TetrahedronHull, TetrahedronGeometry},
		"octahedron":   {geom.OctahedronHull, OctahedronGeometry},
		"icosahedron":  {geom.IcosahedronHull, IcosahedronGeometry},
		"dodecahedron": {geom.DodecahedronHull, DodecahedronGeometry},
	}
	for name, testCase := range hulls {
		// The reference reads the hull, normalizes each point, and keeps the
		// widest component per axis. That is the extent of the detail-0 solid.
		vertices, _ := testCase.hull()
		want := Vector3{}
		for i := 0; i+3 <= len(vertices); i += 3 {
			point := normalizeVector(Vec3(vertices[i], vertices[i+1], vertices[i+2]))
			want.X = math.Max(want.X, math.Abs(point.X)*radius)
			want.Y = math.Max(want.Y, math.Abs(point.Y)*radius)
			want.Z = math.Max(want.Z, math.Abs(point.Z)*radius)
		}
		lo, hi := genBounds(testCase.build(radius, 0))
		assertVectorNear(t, name+" high", hi, want)
		assertVectorNear(t, name+" low", lo, scaleVector(want, -1))

		// A subdivided solid grows toward the sphere and never past it.
		subdivided, subdividedHigh := genBounds(testCase.build(radius, 2))
		for _, value := range []float64{subdividedHigh.X, subdividedHigh.Y, subdividedHigh.Z} {
			if value > radius+1e-9 || value < radius*0.9 {
				t.Fatalf("%s at detail 2: a high bound is %v, want close to %v", name, value, radius)
			}
		}
		assertVectorNear(t, name+" detail 2 low", subdivided, scaleVector(subdividedHigh, -1))
	}

	// A bounding radius must never understate a generated mesh, or the broad
	// phase drops real hits.
	for name, geometry := range map[string]BufferGeometry{
		"icosahedron": IcosahedronGeometry(2, 2),
		"ring":        RingGeometry(0.5, 2, 32, 1, 0, 0),
		"extrude":     ExtrudeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, ExtrudeOptions{Depth: 3}),
	} {
		radius, strokes := geometryBounds(geometry)
		if strokes != 0 {
			t.Fatalf("%s: reported %v pick radii; a triangle mesh needs none", name, strokes)
		}
		for i := 0; i+3 <= len(geometry.Positions); i += 3 {
			x, y, z := geometry.Positions[i], geometry.Positions[i+1], geometry.Positions[i+2]
			if math.Sqrt(x*x+y*y+z*z) > radius+1e-9 {
				t.Fatalf("%s: vertex %d sits outside the declared bounding radius %v", name, i/3, radius)
			}
		}
	}
}

func assertVectorNear(t *testing.T, label string, got, want Vector3) {
	t.Helper()
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

// TestPolyhedronRaycastMatchesTheHullPlanes checks the four named solids against
// a reference built from the base hull, not from the generated triangles.
//
// A convex solid on the origin is the set of points that stay on the inner side
// of every face plane. The reference marches the ray and asks that question at
// each step, then refines the crossing by bisection. The raycaster must agree to
// well inside the marching step.
func TestPolyhedronRaycastMatchesTheHullPlanes(t *testing.T) {
	const radius = 1.5
	solids := []struct {
		name     string
		hull     func() ([]float64, []int)
		geometry BufferGeometry
	}{
		{"tetrahedron", geom.TetrahedronHull, TetrahedronGeometry(radius, 0)},
		{"octahedron", geom.OctahedronHull, OctahedronGeometry(radius, 0)},
		{"icosahedron", geom.IcosahedronHull, IcosahedronGeometry(radius, 0)},
		{"dodecahedron", geom.DodecahedronHull, DodecahedronGeometry(radius, 0)},
	}
	for _, solid := range solids {
		t.Run(solid.name, func(t *testing.T) {
			// Build the face planes from the hull. Every hull point is pushed onto
			// the sphere first, exactly as the generator's contract states.
			vertices, indices := solid.hull()
			point := func(index int) Vector3 {
				base := index * 3
				raw := Vec3(vertices[base], vertices[base+1], vertices[base+2])
				return scaleVector(normalizeVector(raw), radius)
			}
			type plane struct {
				normal Vector3
				offset float64
			}
			planes := make([]plane, 0, len(indices)/3)
			for face := 0; face+3 <= len(indices); face += 3 {
				a, c, d := point(indices[face]), point(indices[face+1]), point(indices[face+2])
				normal := normalizeVector(crossVector(subVectors(c, a), subVectors(d, a)))
				planes = append(planes, plane{normal: normal, offset: dotVector(normal, a)})
			}
			inside := func(p Vector3) bool {
				for _, face := range planes {
					if dotVector(face.normal, p) > face.offset {
						return false
					}
				}
				return true
			}

			hits, misses, thin := 0, 0, 0
			for index, ray := range genRandomRays(2001, 500, radius, radius*3) {
				if inside(ray.Origin) {
					continue
				}
				hit, ok := genRaycast(t, solid.geometry, ray)
				low, high, reachable := marchRange(ray, radius*1.001)
				if !reachable {
					if ok {
						t.Fatalf("ray %d hit a solid its own bounding sphere cannot reach", index)
					}
					misses++
					continue
				}
				want, wantOK := firstInsideDistance(ray, low, high, inside)
				switch {
				case ok && wantOK:
					if math.Abs(hit.Distance-want) > 2*marchStep {
						t.Fatalf("ray %d: distance %v, want %v", index, hit.Distance, want)
					}
					hits++
				case ok != wantOK:
					// A chord thinner than the marching step is invisible to the
					// reference. Count those instead of failing on them.
					thin++
					if thin > 20 {
						t.Fatalf("ray %d: raycaster says %v, reference says %v; too many disagreements",
							index, ok, wantOK)
					}
				default:
					misses++
				}
			}
			if hits < 40 {
				t.Fatalf("weak trial mix: only %d hits", hits)
			}
			if misses < 40 {
				t.Fatalf("weak trial mix: only %d misses", misses)
			}
		})
	}
}

// TestPolyhedronDetailStaysBetweenTheTwoSpheres checks a subdivided solid. Every
// hit must land between the sphere through the face centers and the sphere
// through the vertices, whatever the detail.
func TestPolyhedronDetailStaysBetweenTheTwoSpheres(t *testing.T) {
	const radius = 2.0
	geometry := IcosahedronGeometry(radius, 3)
	// The inscribed radius of a detail-3 icosahedron is close to the sphere, so
	// a loose lower bound still proves the solid did not collapse.
	hits := 0
	for index, ray := range genRandomRays(4242, 400, radius*0.6, radius*3) {
		hit, ok := genRaycast(t, geometry, ray)
		if !ok {
			continue
		}
		distance := vectorLength(hit.Point)
		if distance > radius+1e-9 {
			t.Fatalf("ray %d hit %v from the origin, past the sphere %v", index, distance, radius)
		}
		if distance < radius*0.9 {
			t.Fatalf("ray %d hit %v from the origin, well inside the solid", index, distance)
		}
		hits++
	}
	if hits < 100 {
		t.Fatalf("weak trial mix: only %d hits", hits)
	}
}

// TestCircleRaycastMatchesThePlaneAndRadius checks the disc against an analytic
// reference: meet the plane, then ask whether the point is inside the sector.
func TestCircleRaycastMatchesThePlaneAndRadius(t *testing.T) {
	const (
		radius      = 1.5
		thetaStart  = 0.4
		thetaLength = 2.1
	)
	cases := map[string]struct {
		geometry BufferGeometry
		inside   func(x, z float64) bool
	}{
		"full": {
			CircleGeometry(radius, 128, 0, 0),
			func(x, z float64) bool { return math.Hypot(x, z) <= radius },
		},
		"sector": {
			CircleGeometry(radius, 128, thetaStart, thetaLength),
			func(x, z float64) bool {
				if math.Hypot(x, z) > radius {
					return false
				}
				angle := math.Atan2(z, x) - thetaStart
				for angle < 0 {
					angle += 2 * math.Pi
				}
				return angle <= thetaLength
			},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			hits, misses, edge := 0, 0, 0
			for index, ray := range genRandomRays(777, 1200, radius*1.1, radius*3) {
				hit, ok := genRaycast(t, testCase.geometry, ray)
				want, wantOK := genFlatReference(ray, 0, testCase.inside)
				if ok != wantOK {
					// The disc is a polygon, not a true circle, so a ray near the
					// rim can differ. Count those and require the count to stay
					// small against the chord error of 128 segments.
					if crossing, near := genFlatReference(ray, 0, func(x, z float64) bool { return true }); near {
						point := addVectors(ray.Origin, scaleVector(ray.Direction, crossing))
						if math.Abs(math.Hypot(point.X, point.Z)-radius) < 0.01 {
							edge++
							continue
						}
						if name == "sector" {
							angle := math.Atan2(point.Z, point.X) - thetaStart
							for angle < 0 {
								angle += 2 * math.Pi
							}
							if math.Abs(angle) < 0.05 || math.Abs(angle-thetaLength) < 0.05 {
								edge++
								continue
							}
						}
					}
					t.Fatalf("ray %d: raycaster says %v, the plane reference says %v", index, ok, wantOK)
				}
				if !ok {
					misses++
					continue
				}
				if math.Abs(hit.Distance-want) > 1e-9 {
					t.Fatalf("ray %d: distance %v, want the plane crossing %v", index, hit.Distance, want)
				}
				if math.Abs(hit.Point.Y) > 1e-9 {
					t.Fatalf("ray %d hit at y=%v, want the XZ plane", index, hit.Point.Y)
				}
				hits++
			}
			if hits < 50 || misses < 50 {
				t.Fatalf("weak trial mix: %d hits, %d misses", hits, misses)
			}
			if edge > 80 {
				t.Fatalf("%d rim disagreements; the disc no longer follows its own radius", edge)
			}
		})
	}
}

// TestRingRaycastMatchesTheAnnulus checks that the hole is really a hole. A ring
// that filled its center would pass a vertex count test and a bounds test.
func TestRingRaycastMatchesTheAnnulus(t *testing.T) {
	const (
		inner = 0.6
		outer = 1.6
	)
	geometry := RingGeometry(inner, outer, 128, 2, 0, 0)
	inside := func(x, z float64) bool {
		distance := math.Hypot(x, z)
		return distance >= inner && distance <= outer
	}
	hits, misses, edge, throughHole := 0, 0, 0, 0
	for index, ray := range genRandomRays(31337, 700, outer*1.3, outer*3) {
		hit, ok := genRaycast(t, geometry, ray)
		want, wantOK := genFlatReference(ray, 0, inside)
		if ok != wantOK {
			crossing, crosses := genFlatReference(ray, 0, func(x, z float64) bool { return true })
			if crosses {
				point := addVectors(ray.Origin, scaleVector(ray.Direction, crossing))
				distance := math.Hypot(point.X, point.Z)
				if math.Abs(distance-inner) < 0.01 || math.Abs(distance-outer) < 0.01 {
					edge++
					continue
				}
			}
			t.Fatalf("ray %d: raycaster says %v, the annulus reference says %v", index, ok, wantOK)
		}
		if !ok {
			misses++
			continue
		}
		if math.Abs(hit.Distance-want) > 1e-9 {
			t.Fatalf("ray %d: distance %v, want the plane crossing %v", index, hit.Distance, want)
		}
		hits++
	}
	// Aim straight down the axis. The hole must let every one of these through.
	for step := 0; step < 20; step++ {
		offset := float64(step) / 20 * inner * 0.9
		ray := Ray{Origin: Vec3(offset, 4, 0), Direction: Vec3(0, -1, 0)}
		if _, ok := genRaycast(t, geometry, ray); ok {
			t.Fatalf("a ray through the hole at x=%v hit the ring", offset)
		}
		throughHole++
	}
	if hits < 50 || misses < 50 || throughHole != 20 {
		t.Fatalf("weak trial mix: %d hits, %d misses, %d hole rays", hits, misses, throughHole)
	}
}

// TestShapeRaycastMatchesThePolygon checks a shape with a hole against a
// point-in-polygon reference that counts ray crossings.
func TestShapeRaycastMatchesThePolygon(t *testing.T) {
	rings := [][]float64{
		{-2, -2, 2, -2, 2, 2, -2, 2},
		{-0.8, -0.8, 0.8, -0.8, 0.8, 0.8, -0.8, 0.8},
	}
	shape := Shape{Outline: rings[0], Holes: [][]float64{rings[1]}}
	geometry := ShapeGeometry(shape, 0.25)
	hits, misses := 0, 0
	for index, ray := range genRandomRays(9090, 700, 3, 6) {
		hit, ok := genRaycast(t, geometry, ray)
		want, wantOK := genFlatReference(ray, 0.25, func(x, z float64) bool {
			return genPointInPolygon(rings, x, z)
		})
		if ok != wantOK {
			// A ray that meets the plane within a hair of an edge can land on
			// either side of it. Allow that band and nothing else.
			crossing, crosses := genFlatReference(ray, 0.25, func(x, z float64) bool { return true })
			if crosses {
				point := addVectors(ray.Origin, scaleVector(ray.Direction, crossing))
				near := false
				for _, ring := range rings {
					for i := 0; i+2 <= len(ring); i += 2 {
						if math.Abs(math.Abs(point.X)-math.Abs(ring[i])) < 1e-6 ||
							math.Abs(math.Abs(point.Z)-math.Abs(ring[i+1])) < 1e-6 {
							near = true
						}
					}
				}
				if near {
					continue
				}
			}
			t.Fatalf("ray %d: raycaster says %v, the polygon reference says %v", index, ok, wantOK)
		}
		if !ok {
			misses++
			continue
		}
		if math.Abs(hit.Distance-want) > 1e-9 {
			t.Fatalf("ray %d: distance %v, want the plane crossing %v", index, hit.Distance, want)
		}
		hits++
	}
	if hits < 50 || misses < 50 {
		t.Fatalf("weak trial mix: %d hits, %d misses", hits, misses)
	}
}

// TestExtrudeRaycastMatchesThePrism checks the solid against a marching
// reference. The reference states the prism in its own terms: a point is inside
// when its height is in range and its 2D point is in the polygon.
func TestExtrudeRaycastMatchesThePrism(t *testing.T) {
	const depth = 1.5
	rings := [][]float64{
		{-1.5, -1, 1.5, -1, 1.5, 1, -1.5, 1},
		{-0.5, -0.4, 0.5, -0.4, 0.5, 0.4, -0.5, 0.4},
	}
	shape := Shape{Outline: rings[0], Holes: [][]float64{rings[1]}}
	geometry := ExtrudeGeometry(shape, ExtrudeOptions{Depth: depth})
	inside := func(p Vector3) bool {
		if p.Y < 0 || p.Y > depth {
			return false
		}
		return genPointInPolygon(rings, p.X, p.Z)
	}
	// The prism is not centered on the origin, so bound it by the sphere that
	// holds every corner.
	bound := math.Sqrt(1.5*1.5+1*1+depth*depth) + 0.01
	hits, misses, thin := 0, 0, 0
	for index, ray := range genRandomRays(5150, 700, 2, 5) {
		if inside(ray.Origin) {
			continue
		}
		hit, ok := genRaycast(t, geometry, ray)
		low, high, reachable := marchRange(ray, bound)
		if !reachable {
			if ok {
				t.Fatalf("ray %d hit a solid its own bounding sphere cannot reach", index)
			}
			misses++
			continue
		}
		want, wantOK := firstInsideDistance(ray, low, high, inside)
		switch {
		case ok && wantOK:
			if math.Abs(hit.Distance-want) > 2*marchStep {
				t.Fatalf("ray %d: distance %v, want %v", index, hit.Distance, want)
			}
			hits++
		case ok != wantOK:
			thin++
			if thin > 25 {
				t.Fatalf("ray %d: raycaster says %v, reference says %v; too many disagreements",
					index, ok, wantOK)
			}
		default:
			misses++
		}
	}
	if hits < 60 || misses < 60 {
		t.Fatalf("weak trial mix: %d hits, %d misses", hits, misses)
	}
}

// TestGeneratedGeometryLowersToAMeshWithVertices proves the whole route works:
// a generator makes a BufferGeometry, the lowerer turns it into a gltf-mesh
// object, and the object carries its own vertices. A generator whose vertices
// never reached the wire would draw nothing in the browser.
func TestGeneratedGeometryLowersToAMeshWithVertices(t *testing.T) {
	cases := map[string]BufferGeometry{
		"tetrahedron":  TetrahedronGeometry(1, 0),
		"octahedron":   OctahedronGeometry(1, 0),
		"icosahedron":  IcosahedronGeometry(1, 1),
		"dodecahedron": DodecahedronGeometry(1, 0),
		"circle":       CircleGeometry(1, 16, 0, 0),
		"ring":         RingGeometry(0.5, 1, 16, 1, 0, 0),
		"shape":        ShapeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, 0),
		"extrude":      ExtrudeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, ExtrudeOptions{Depth: 1}),
	}
	for name, geometry := range cases {
		ir := Props{Graph: NewGraph(Mesh{ID: "subject", Geometry: geometry})}.SceneIR()
		if len(ir.Objects) != 1 {
			t.Fatalf("%s: %d objects, want 1", name, len(ir.Objects))
		}
		object := ir.Objects[0]
		if object.Kind != "gltf-mesh" {
			t.Fatalf("%s: kind %q, want gltf-mesh", name, object.Kind)
		}
		if object.Vertices == nil {
			t.Fatalf("%s: the object carries no vertices, so the browser would draw nothing", name)
		}
		if got, want := object.Vertices.Count, len(geometry.Positions)/3; got != want {
			t.Fatalf("%s: %d wire vertices, want %d unique", name, got, want)
		}
		if got, want := len(object.Vertices.Indices), len(geometry.Indices); got != want {
			t.Fatalf("%s: wire index stream of %d does not match the %d authored indices", name, got, want)
		}
		if len(object.Vertices.Positions) != object.Vertices.Count*3 {
			t.Fatalf("%s: the position stream does not match the vertex count", name)
		}
		if len(object.Vertices.Normals) != object.Vertices.Count*3 {
			t.Fatalf("%s: the normal stream does not match the vertex count", name)
		}
		if len(object.Vertices.UVs) != object.Vertices.Count*2 {
			t.Fatalf("%s: the uv stream does not match the vertex count", name)
		}
	}
}

// TestTessellateAnswersForEverySurfaceGeometry proves the shared tessellator
// covers every geometry that owns a surface, and refuses the ones that do not.
//
// A missing case here is exactly the defect this work closed: the native
// renderer knew eight kinds, did not know the ninth, and dropped its draw.
func TestTessellateAnswersForEverySurfaceGeometry(t *testing.T) {
	surfaces := map[string]Geometry{
		"cube":      CubeGeometry{},
		"box":       BoxGeometry{Width: 2, Height: 1, Depth: 3},
		"plane":     PlaneGeometry{},
		"pyramid":   PyramidGeometry{},
		"sphere":    SphereGeometry{},
		"cylinder":  CylinderGeometry{RadiusTop: 1, RadiusBottom: 1, Height: 2},
		"cone":      CylinderGeometry{RadiusBottom: 1, Height: 2},
		"torus":     TorusGeometry{},
		"torusknot": TorusKnotGeometry{},
		"buffer":    IcosahedronGeometry(1, 0),
	}
	for name, geometry := range surfaces {
		mesh, ok := Tessellate(geometry)
		if !ok {
			t.Fatalf("%s: Tessellate refused a geometry that owns a surface", name)
		}
		if mesh.TriangleCount() == 0 {
			t.Fatalf("%s: Tessellate returned no triangles", name)
		}
		if len(mesh.Normals) != len(mesh.Positions) {
			t.Fatalf("%s: the normal stream does not match the position stream", name)
		}
		if len(mesh.UVs) != mesh.VertexCount()*2 {
			t.Fatalf("%s: the uv stream does not match the vertex count", name)
		}
	}
	surfaceless := map[string]Geometry{
		"nil":            nil,
		"lines":          LinesGeometry{Points: []Vector3{Vec3(0, 0, 0), Vec3(1, 0, 0)}, Segments: [][2]int{{0, 1}}},
		"empty buffer":   BufferGeometry{},
		"nil pointer":    (*BufferGeometry)(nil),
		"empty polygon":  PolygonGeometry(nil, nil, 0),
		"degenerate rng": RingGeometry(2, 1, 32, 1, 0, 0),
	}
	for name, geometry := range surfaceless {
		if _, ok := Tessellate(geometry); ok {
			t.Fatalf("%s: Tessellate answered for a geometry with no surface", name)
		}
	}
}

// TestTessellateMatchesTheDrawnKnot proves the picker and the renderer read one
// tessellation. The knot soup is built through the same call, so a change to one
// consumer cannot leave the other behind.
func TestTessellateMatchesTheDrawnKnot(t *testing.T) {
	geometry := TorusKnotGeometry{Radius: 1, Tube: 0.2, RadialSegments: 6, TubularSegments: 24}
	mesh, ok := Tessellate(geometry)
	if !ok {
		t.Fatal("Tessellate refused a torus knot")
	}
	soup := torusKnotSoup(geometry)
	if got, want := soup.triangleCount(), mesh.TriangleCount(); got != want {
		t.Fatalf("the picker holds %d triangles, the tessellator %d", got, want)
	}
	for i := range mesh.Positions {
		if soup.positions[i] != mesh.Positions[i] {
			t.Fatalf("position %d differs between the picker and the tessellator", i)
		}
	}
	for i := range mesh.Indices {
		if soup.indices[i] != mesh.Indices[i] {
			t.Fatalf("index %d differs between the picker and the tessellator", i)
		}
	}
}

// TestGeneratedGeometryWireCost measures what each generator costs on the wire,
// the same way a size budget does: marshal the scene IR, then compress it.
//
// The numbers are a record, not a limit. Bundle bytes are free for these
// generators, because the browser needs no code for them. Wire bytes are not.
func TestGeneratedGeometryWireCost(t *testing.T) {
	cases := []struct {
		name     string
		geometry BufferGeometry
	}{
		{"TetrahedronGeometry(1, 0)", TetrahedronGeometry(1, 0)},
		{"OctahedronGeometry(1, 0)", OctahedronGeometry(1, 0)},
		{"IcosahedronGeometry(1, 0)", IcosahedronGeometry(1, 0)},
		{"IcosahedronGeometry(1, 2)", IcosahedronGeometry(1, 2)},
		{"DodecahedronGeometry(1, 0)", DodecahedronGeometry(1, 0)},
		{"CircleGeometry(1, 32)", CircleGeometry(1, 32, 0, 0)},
		{"RingGeometry(0.5, 1, 32, 1)", RingGeometry(0.5, 1, 32, 1, 0, 0)},
		{"ShapeGeometry(square)", ShapeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, 0)},
		{
			"ExtrudeGeometry(square, depth 1)",
			ExtrudeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, ExtrudeOptions{Depth: 1}),
		},
		{
			"ExtrudeGeometry(square, bevel)",
			ExtrudeGeometry(Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}},
				ExtrudeOptions{Depth: 1, Bevel: true, BevelSize: 0.1, BevelThickness: 0.1, BevelSegments: 3}),
		},
	}
	empty := genWireBytes(t, Props{Graph: NewGraph(Mesh{ID: "subject", Geometry: CubeGeometry{}})})
	for _, testCase := range cases {
		props := Props{Graph: NewGraph(Mesh{ID: "subject", Geometry: testCase.geometry})}
		raw, gzipped := genWireBytesBoth(t, props)
		t.Logf("%-34s vertices=%4d raw=%7d B gzip-9=%6d B (delta over a cube: %+d B)",
			testCase.name, genDrawnIndexCount(testCase.geometry), raw, gzipped, gzipped-empty)
		if gzipped <= empty {
			t.Fatalf("%s costs no more than a parametric cube; the vertices did not reach the wire",
				testCase.name)
		}
	}
}

func genWireBytes(t *testing.T, props Props) int {
	t.Helper()
	_, gzipped := genWireBytesBoth(t, props)
	return gzipped
}

func genWireBytesBoth(t *testing.T, props Props) (raw, gzipped int) {
	t.Helper()
	payload, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatalf("marshal the scene IR: %v", err)
	}
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		t.Fatalf("open the compressor: %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the compressor: %v", err)
	}
	return len(payload), buffer.Len()
}
