package scene

import (
	"math"
	"math/rand"
	"testing"
)

// This file checks the exact narrow-phase tests against references that share no
// code with them. Every reference marches the ray in small steps and asks a
// point-inside-the-solid question, then refines the crossing by bisection. A
// solver bug shows up as a wrong distance, a dropped hit, or a phantom hit.

// marchStep is the reference sampling step. It resolves every chord thicker than
// this, and the tests count the thinner ones instead of failing on them.
const marchStep = 1e-4

// marchRange returns the part of the ray that can reach a solid held inside a
// sphere of radius bound centered on the node origin. Marching only this chord
// keeps the reference fast and skips the empty run in front of the solid.
func marchRange(ray Ray, bound float64) (float64, float64, bool) {
	along := -dotVector(ray.Origin, ray.Direction)
	gap := bound*bound - (dotVector(ray.Origin, ray.Origin) - along*along)
	if gap <= 0 {
		return 0, 0, false
	}
	half := math.Sqrt(gap)
	low, high := along-half, along+half
	if high < 0 {
		return 0, 0, false
	}
	if low < 0 {
		low = 0
	}
	return low, high, true
}

// firstInsideDistance returns the first ray parameter inside [low, high] where
// inside reports true. It refines the crossing by bisection, so the answer is
// accurate to about 1e-12. A ray that starts inside the solid reports low, and
// the callers skip that case: every exact test reports the exit surface there,
// which is what the sphere and box tests already do.
func firstInsideDistance(ray Ray, low, high float64, inside func(Vector3) bool) (float64, bool) {
	pointAt := func(distance float64) Vector3 {
		return addVectors(ray.Origin, scaleVector(ray.Direction, distance))
	}
	previous := low
	for distance := low; distance <= high; distance += marchStep {
		if !inside(pointAt(distance)) {
			previous = distance
			continue
		}
		lowEdge, highEdge := previous, distance
		for i := 0; i < 60; i++ {
			middle := (lowEdge + highEdge) / 2
			if inside(pointAt(middle)) {
				highEdge = middle
			} else {
				lowEdge = middle
			}
		}
		return (lowEdge + highEdge) / 2, true
	}
	return 0, false
}

// torusInside reports whether a point sits inside the torus solid.
func torusInside(point Vector3, radius, tube float64) bool {
	planar := math.Hypot(point.X, point.Z) - radius
	return planar*planar+point.Y*point.Y <= tube*tube
}

// torusSurfaceResidual returns the quartic value at a point, scaled so that a
// point on the surface reads near zero whatever the torus size.
func torusSurfaceResidual(point Vector3, radius, tube float64) float64 {
	planar := math.Hypot(point.X, point.Z) - radius
	return (planar*planar + point.Y*point.Y - tube*tube) / (tube * tube)
}

func TestTorusRaycastMatchesNumericSolid(t *testing.T) {
	random := rand.New(rand.NewSource(23))
	hits, misses, grazes := 0, 0, 0
	for trial := 0; trial < 3000; trial++ {
		radius := 0.4 + random.Float64()*1.5
		tube := 0.05 + random.Float64()*0.3
		// Aim most rays through the torus envelope so the mix holds real hits.
		origin := Vec3(random.Float64()*8-4, random.Float64()*8-4, random.Float64()*8-4)
		reach := radius + tube
		target := Vec3(random.Float64()*2*reach-reach, random.Float64()*2*reach-reach, random.Float64()*2*reach-reach)
		ray := Ray{Origin: origin, Direction: normalizeVector(subVectors(target, origin))}
		if ray.Direction == (Vector3{}) || torusInside(ray.Origin, radius, tube) {
			continue
		}
		low, high, reachable := marchRange(ray, reach)
		if !reachable {
			if _, ok := intersectTorus(ray, radius, tube); ok {
				t.Fatalf("trial %d: hit outside the bounding sphere (radius %v tube %v ray %#v)", trial, radius, tube, ray)
			}
			misses++
			continue
		}
		want, wantOK := firstInsideDistance(ray, low, high, func(point Vector3) bool {
			return torusInside(point, radius, tube)
		})
		got, gotOK := intersectTorus(ray, radius, tube)

		if !gotOK && wantOK {
			t.Fatalf("trial %d: the solver dropped a hit at %v (radius %v tube %v ray %#v)",
				trial, want, radius, tube, ray)
		}
		if gotOK && !wantOK {
			// A chord thinner than the march step is invisible to the reference.
			// Require the reported point to sit on the surface.
			if residual := math.Abs(torusSurfaceResidual(got.Point, radius, tube)); residual > 1e-6 {
				t.Fatalf("trial %d: phantom hit at %v, surface residual %v (radius %v tube %v ray %#v)",
					trial, got.Distance, residual, radius, tube, ray)
			}
			grazes++
			continue
		}
		if !gotOK {
			misses++
			continue
		}
		hits++
		if math.Abs(got.Distance-want) > 1e-6 {
			t.Fatalf("trial %d: distance = %v, want %v (radius %v tube %v ray %#v)",
				trial, got.Distance, want, radius, tube, ray)
		}
		if residual := math.Abs(torusSurfaceResidual(got.Point, radius, tube)); residual > 1e-9 {
			t.Fatalf("trial %d: hit point off the surface by %v", trial, residual)
		}
		// The normal must point away from the nearest point of the center circle.
		planar := math.Hypot(got.Point.X, got.Point.Z)
		center := Vec3(got.Point.X*radius/planar, 0, got.Point.Z*radius/planar)
		outward := normalizeVector(subVectors(got.Point, center))
		if dotVector(outward, got.Normal) < 1-1e-6 {
			t.Fatalf("trial %d: normal %#v does not match the tube surface %#v", trial, got.Normal, outward)
		}
	}
	t.Logf("hits=%d misses=%d thin-chord grazes=%d", hits, misses, grazes)
	if hits < 200 || misses < 200 {
		t.Fatalf("weak trial mix: hits=%d misses=%d", hits, misses)
	}
	if grazes*50 > hits+misses {
		t.Fatalf("too many chords fell below the reference step: %d", grazes)
	}
}

func TestTorusRaycastRejectsTheHoleAndTheCorners(t *testing.T) {
	graph := NewGraph(Mesh{ID: "ring", Geometry: TorusGeometry{Radius: 1, Tube: 0.25}})
	// Straight down the axis of the hole. The bounding sphere reported this as a
	// hit; the quartic does not.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 5, 0), Direction: Vec3(0, -1, 0)}); ok {
		t.Fatalf("a ray through the hole must miss, got %#v", hit)
	}
	// Through the tube, from above the ring at x = 1.
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(1, 5, 0), Direction: Vec3(0, -1, 0)})
	if !ok {
		t.Fatal("a ray through the tube must hit")
	}
	if hit.Method != "analytic-torus" || hit.Kind != "torus" {
		t.Fatalf("expected the exact torus test, got %#v", hit)
	}
	if math.Abs(hit.Distance-4.75) > 1e-9 {
		t.Fatalf("expected the tube top at distance 4.75, got %v", hit.Distance)
	}
	if math.Abs(hit.Normal.Y-1) > 1e-9 {
		t.Fatalf("expected an upward normal on the tube top, got %#v", hit.Normal)
	}
	// The tube spans x in [0.75, 1.25], so a ray at x = 1.3 crosses the bounding
	// sphere and no surface.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(1.3, 5, 0), Direction: Vec3(0, -1, 0)}); ok {
		t.Fatalf("a ray outside the tube must miss, got %#v", hit)
	}
	// So does a ray between the hole and the tube.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.5, 5, 0), Direction: Vec3(0, -1, 0)}); ok {
		t.Fatalf("a ray inside the hole must miss, got %#v", hit)
	}
}

func TestTorusRaycastHandlesRaysInThePlaneOfTheRing(t *testing.T) {
	// A ray that lies in the plane of the ring meets the surface exactly where it
	// meets the bounding sphere, because the outer equator touches that sphere. A
	// solver that brackets its search at the sphere alone reads the crossing as
	// zero and reports a miss. A flat ring picked from the side is the common case,
	// so this test pins it.
	radius, tube := 1.0, 0.3
	graph := NewGraph(Mesh{ID: "ring", Geometry: TorusGeometry{Radius: radius, Tube: tube}})
	for _, offset := range []float64{0, 0.25, 0.5, 0.7, 1, 1.2, 1.29} {
		ray := Ray{Origin: Vec3(offset, 0, 6), Direction: Vec3(0, 0, -1)}
		hit, ok := RaycastGraph(graph, ray)
		if !ok {
			t.Fatalf("offset %v: a ray across the ring plane must hit the tube", offset)
		}
		// The hit must land on the outer surface, which sits where the planar radius
		// reaches radius + tube.
		if gap := math.Hypot(hit.Point.X, hit.Point.Z) - (radius + tube); math.Abs(gap) > 1e-9 {
			t.Fatalf("offset %v: hit %#v sits %v off the outer equator", offset, hit.Point, gap)
		}
		if hit.Method != "analytic-torus" {
			t.Fatalf("offset %v: expected the exact torus test, got %q", offset, hit.Method)
		}
	}
	// Past the outer equator the coplanar ray must miss.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(1.31, 0, 6), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("a coplanar ray outside the ring must miss, got %#v", hit)
	}
}

func TestTorusRaycastGrazesTheOuterEquator(t *testing.T) {
	// The broadphase sizes the torus as radius + tube. A ray that passes just
	// inside that reach must still hit, which pins the bound.
	geometry := TorusGeometry{Radius: 1, Tube: 0.25}
	radius, strokes := geometryBounds(geometry)
	if radius != 1.25 || strokes != 0 {
		t.Fatalf("torus bounds = %v, %v, want 1.25, 0", radius, strokes)
	}
	graph := NewGraph(Mesh{ID: "ring", Geometry: geometry})
	ray := Ray{Origin: Vec3(radius-1e-6, 0, 5), Direction: Vec3(0, 0, -1)}
	hit, ok := RaycastGraph(graph, ray)
	if !ok {
		t.Fatal("a ray grazing the outer equator must hit")
	}
	if hit.Method != "analytic-torus" {
		t.Fatalf("expected the exact torus test to run, got %q", hit.Method)
	}
	// The chord is short, so the hit lands close to the plane of the ring.
	if math.Abs(hit.Point.Z) > 0.01 {
		t.Fatalf("expected the grazing hit near z = 0, got %#v", hit.Point)
	}
	// A hair outside the bound must miss, and the broadphase may reject it.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(radius+1e-6, 0, 5), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("a ray outside the outer equator must miss, got %#v", hit)
	}
}

func TestTorusRaycastUsesRendererDefaults(t *testing.T) {
	// The runtime draws a zero-valued torus with radius 0.7 and tube 0.3. An exact
	// test against another size would not be exact.
	graph := NewGraph(Mesh{ID: "ring", Geometry: TorusGeometry{}})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.7, 3, 0), Direction: Vec3(0, -1, 0)})
	if !ok {
		t.Fatal("the default torus must hit above its center circle")
	}
	if math.Abs(hit.Distance-2.7) > 1e-9 {
		t.Fatalf("expected the tube top 0.3 above the origin, got %v", hit.Distance)
	}
}

func TestTorusRaycastExitsFromInside(t *testing.T) {
	// A ray that starts inside the tube must report the exit surface, which is
	// what the sphere and box tests do.
	hit, ok := intersectTorus(Ray{Origin: Vec3(1, 0, 0), Direction: Vec3(0, 1, 0)}, 1, 0.25)
	if !ok {
		t.Fatal("a ray from inside the tube must hit the surface")
	}
	if math.Abs(hit.Distance-0.25) > 1e-9 {
		t.Fatalf("expected the exit 0.25 above the center circle, got %v", hit.Distance)
	}
}

// pyramidInside reports whether a point sits inside the pyramid solid. It tests
// the five faces as half spaces, which shares no code with the triangle test.
func pyramidInside(point Vector3, width, height, depth float64) bool {
	halfWidth, halfHeight, halfDepth := width/2, height/2, depth/2
	if point.Y < -halfHeight || point.Y > halfHeight {
		return false
	}
	// The cross-section shrinks to a point at the apex.
	shrink := (halfHeight - point.Y) / (2 * halfHeight)
	return math.Abs(point.X) <= halfWidth*shrink && math.Abs(point.Z) <= halfDepth*shrink
}

func TestPyramidRaycastMatchesNumericSolid(t *testing.T) {
	random := rand.New(rand.NewSource(29))
	hits, misses, grazes := 0, 0, 0
	for trial := 0; trial < 3000; trial++ {
		width := 0.4 + random.Float64()*2
		height := 0.4 + random.Float64()*2
		depth := 0.4 + random.Float64()*2
		// Aim most rays through the pyramid envelope so the mix holds real hits.
		origin := Vec3(random.Float64()*6-3, random.Float64()*6-3, random.Float64()*6-3)
		target := Vec3(random.Float64()*width-width/2, random.Float64()*height-height/2, random.Float64()*depth-depth/2)
		ray := Ray{Origin: origin, Direction: normalizeVector(subVectors(target, origin))}
		if ray.Direction == (Vector3{}) || pyramidInside(ray.Origin, width, height, depth) {
			continue
		}
		bound, _ := geometryBounds(PyramidGeometry{Width: width, Height: height, Depth: depth})
		low, high, reachable := marchRange(ray, bound)
		if !reachable {
			if _, ok := intersectPyramid(ray, width, height, depth); ok {
				t.Fatalf("trial %d: hit outside the bounding sphere (size %v %v %v ray %#v)", trial, width, height, depth, ray)
			}
			misses++
			continue
		}
		want, wantOK := firstInsideDistance(ray, low, high, func(point Vector3) bool {
			return pyramidInside(point, width, height, depth)
		})
		got, gotOK := intersectPyramid(ray, width, height, depth)

		if !gotOK && wantOK {
			t.Fatalf("trial %d: the solver dropped a hit at %v (size %v %v %v ray %#v)",
				trial, want, width, height, depth, ray)
		}
		if gotOK && !wantOK {
			if !pyramidInside(got.Point, width*(1+1e-6), height*(1+1e-6), depth*(1+1e-6)) {
				t.Fatalf("trial %d: phantom hit at %#v (size %v %v %v ray %#v)",
					trial, got.Point, width, height, depth, ray)
			}
			grazes++
			continue
		}
		if !gotOK {
			misses++
			continue
		}
		hits++
		if math.Abs(got.Distance-want) > 1e-6 {
			t.Fatalf("trial %d: distance = %v, want %v (size %v %v %v ray %#v)",
				trial, got.Distance, want, width, height, depth, ray)
		}
		if length := vectorLength(got.Normal); math.Abs(length-1) > 1e-9 {
			t.Fatalf("trial %d: normal length = %v", trial, length)
		}
		if dotVector(got.Normal, ray.Direction) > 0 {
			t.Fatalf("trial %d: normal %#v faces away from the ray", trial, got.Normal)
		}
	}
	t.Logf("hits=%d misses=%d thin-chord grazes=%d", hits, misses, grazes)
	if hits < 200 || misses < 200 {
		t.Fatalf("weak trial mix: hits=%d misses=%d", hits, misses)
	}
	if grazes*50 > hits+misses {
		t.Fatalf("too many chords fell below the reference step: %d", grazes)
	}
}

func TestPyramidRaycastRejectsTheBoundingBoxCorner(t *testing.T) {
	graph := NewGraph(Mesh{ID: "spire", Geometry: PyramidGeometry{Width: 2, Height: 2, Depth: 2}})
	// This ray crosses the old bounding box near the top corner, well outside the
	// slanted face.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.9, 0.9, 5), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("expected a pyramid corner miss, got %#v", hit)
	}
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 3, 0), Direction: Vec3(0, -1, 0)})
	if !ok {
		t.Fatal("a ray down the apex must hit")
	}
	if hit.Method != "analytic-pyramid" || hit.Kind != "pyramid" {
		t.Fatalf("expected the exact pyramid test, got %#v", hit)
	}
	if math.Abs(hit.Distance-2) > 1e-9 {
		t.Fatalf("expected the apex at distance 2, got %v", hit.Distance)
	}
}

func TestPyramidRaycastGrazesABaseCorner(t *testing.T) {
	// The base corner is the farthest point of the solid, so it pins the
	// broadphase radius. A ray just inside it must still hit.
	geometry := PyramidGeometry{Width: 2, Height: 2, Depth: 2}
	graph := NewGraph(Mesh{ID: "spire", Geometry: geometry})
	radius, _ := geometryBounds(geometry)
	if math.Abs(radius-math.Sqrt(3)) > 1e-12 {
		t.Fatalf("pyramid bound = %v, want sqrt(3)", radius)
	}
	ray := Ray{Origin: Vec3(1-1e-7, -1+1e-7, 5), Direction: Vec3(0, 0, -1)}
	hit, ok := RaycastGraph(graph, ray)
	if !ok {
		t.Fatal("a ray grazing the base corner must hit")
	}
	if hit.Method != "analytic-pyramid" {
		t.Fatalf("expected the exact pyramid test to run, got %q", hit.Method)
	}
	if math.Abs(hit.Distance-4) > 1e-6 {
		t.Fatalf("expected the near base face at distance 4, got %v", hit.Distance)
	}
}

// segmentDistance returns the distance from a point to a segment.
func segmentDistance(point, from, to Vector3) float64 {
	return vectorLength(subVectors(point, closestPointOnSegment(point, from, to)))
}

func TestLinesRaycastMatchesClosestApproach(t *testing.T) {
	random := rand.New(rand.NewSource(31))
	points := []Vector3{
		Vec3(-1, 0, 0), Vec3(1, 0, 0), Vec3(1, 1.5, 0.5), Vec3(-0.5, 1, -1), Vec3(0, -1, 1),
	}
	geometry := LinesGeometry{
		Points:   points,
		Segments: [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}},
	}
	hits, misses, grazes := 0, 0, 0
	for trial := 0; trial < 2000; trial++ {
		threshold := 0.02 + random.Float64()*0.3
		// Aim most rays at the strokes so the mix holds real hits.
		origin := Vec3(random.Float64()*6-3, random.Float64()*6-3, random.Float64()*6-3)
		target := points[random.Intn(len(points))]
		target = addVectors(target, Vec3(random.Float64()-0.5, random.Float64()-0.5, random.Float64()-0.5))
		ray := Ray{Origin: origin, Direction: normalizeVector(subVectors(target, origin))}
		if ray.Direction == (Vector3{}) {
			continue
		}
		inside := func(point Vector3) bool {
			for _, pair := range geometry.Segments {
				if segmentDistance(point, points[pair[0]], points[pair[1]]) <= threshold {
					return true
				}
			}
			return false
		}
		if inside(ray.Origin) {
			continue
		}
		bound, strokes := geometryBounds(geometry)
		low, high, reachable := marchRange(ray, bound+strokes*threshold)
		if !reachable {
			if _, ok := intersectLines(ray, geometry, threshold); ok {
				t.Fatalf("trial %d: hit outside the bounding sphere (threshold %v ray %#v)", trial, threshold, ray)
			}
			misses++
			continue
		}
		want, wantOK := firstInsideDistance(ray, low, high, inside)
		got, gotOK := intersectLines(ray, geometry, threshold)

		if !gotOK && wantOK {
			t.Fatalf("trial %d: the solver dropped a stroke hit at %v (threshold %v ray %#v)",
				trial, want, threshold, ray)
		}
		if gotOK && !wantOK {
			if !inside(got.Point) {
				t.Fatalf("trial %d: phantom stroke hit at %#v (threshold %v)", trial, got.Point, threshold)
			}
			grazes++
			continue
		}
		if !gotOK {
			misses++
			continue
		}
		hits++
		if math.Abs(got.Distance-want) > 1e-6 {
			t.Fatalf("trial %d: distance = %v, want %v (threshold %v ray %#v)",
				trial, got.Distance, want, threshold, ray)
		}
	}
	t.Logf("hits=%d misses=%d thin-chord grazes=%d", hits, misses, grazes)
	if hits < 100 || misses < 100 {
		t.Fatalf("weak trial mix: hits=%d misses=%d", hits, misses)
	}
	if grazes*20 > hits+misses {
		t.Fatalf("too many chords fell below the reference step: %d", grazes)
	}
}

func TestLinesRaycastRejectsTheEmptyBoxInterior(t *testing.T) {
	// The four points frame a square with no diagonal. The old bounding box
	// reported the middle as a hit; the stroke test does not.
	graph := NewGraph(Mesh{ID: "frame", Geometry: LinesGeometry{
		Points:   []Vector3{Vec3(-1, -1, 0), Vec3(1, -1, 0), Vec3(1, 1, 0), Vec3(-1, 1, 0)},
		Segments: [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}},
	}})
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 5), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("a ray through the empty middle must miss, got %#v", hit)
	}
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, -1, 5), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("a ray down a stroke must hit")
	}
	if hit.Method != "line-threshold" || hit.Kind != "lines" {
		t.Fatalf("expected the stroke test, got %#v", hit)
	}
	if math.Abs(hit.Distance-(5-DefaultPointThreshold)) > 1e-9 {
		t.Fatalf("expected the hit on the stroke surface, got %v", hit.Distance)
	}
}

func TestLinesRaycastGrazesTheStrokeSurface(t *testing.T) {
	// The pick radius extends the bound by exactly one threshold. A ray that
	// passes just inside the stroke surface must hit, which pins that bound.
	geometry := LinesGeometry{Points: []Vector3{Vec3(-1, 0, 0), Vec3(1, 0, 0)}, Segments: [][2]int{{0, 1}}}
	radius, strokes := geometryBounds(geometry)
	if strokes != 1 {
		t.Fatalf("lines must report one stroke radius, got %v", strokes)
	}
	graph := NewGraph(Mesh{ID: "wire", Geometry: geometry, Scale: Vec3(2, 2, 2)})
	// The stroke sits 0.1 from the axis in world units, and the scale does not
	// change that, so a ray at y = 0.0999 hits and one at y = 0.1001 misses.
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0.0999, 5), Direction: Vec3(0, 0, -1)}); !ok {
		t.Fatalf("a ray inside the stroke must hit (bound radius %v)", radius)
	}
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0.1001, 5), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("a ray outside the stroke must miss, got %#v", hit)
	}
	// A wider threshold reaches further, and the broadphase must follow it.
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0.4, 5), Direction: Vec3(0, 0, -1)}, PointThreshold(0.5)); !ok {
		t.Fatal("a wider pick radius must reach a ray 0.4 units off the stroke")
	}
}

func TestLinesRaycastFallsBackToAConnectedPolyline(t *testing.T) {
	// sceneLineSegments in the runtime draws consecutive points when no index pair
	// survives, so the native test must pick the same strokes.
	graph := NewGraph(Mesh{ID: "path", Geometry: LinesGeometry{
		Points:   []Vector3{Vec3(-1, 0, 0), Vec3(1, 0, 0)},
		Segments: [][2]int{{0, 0}, {5, 9}},
	}})
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 3, 0), Direction: Vec3(0, -1, 0)}); !ok {
		t.Fatal("expected the fallback polyline to pick")
	}
}

func TestTorusKnotRaycastMatchesItsTessellation(t *testing.T) {
	geometry := TorusKnotGeometry{Radius: 1, Tube: 0.2, RadialSegments: 8, TubularSegments: 32}
	soup := torusKnotSoup(geometry)
	if soup.triangleCount() != 32*8*2 {
		t.Fatalf("triangle count = %d, want %d", soup.triangleCount(), 32*8*2)
	}
	if len(soup.tree.nodes) == 0 {
		t.Fatal("expected a triangle hierarchy over the tessellation")
	}
	// Every vertex must sit one tube radius from the knot curve, and inside the
	// bound the broadphase uses.
	radius, _ := geometryBounds(geometry)
	for index := 0; index+3 <= len(soup.positions); index += 3 {
		point := Vec3(soup.positions[index], soup.positions[index+1], soup.positions[index+2])
		if vectorLength(point) > radius+1e-9 {
			t.Fatalf("vertex %#v leaves the bound %v", point, radius)
		}
	}

	// The hierarchy must return exactly what a walk over the same triangles
	// returns, including the tie order.
	flat := triangleSoup{positions: soup.positions, indices: soup.indices}
	random := rand.New(rand.NewSource(37))
	hits := 0
	for trial := 0; trial < 800; trial++ {
		// Aim each ray at a point inside the knot envelope so the mix holds enough
		// hits to be worth comparing.
		origin := Vec3(random.Float64()*6-3, random.Float64()*6-3, random.Float64()*6-3)
		target := Vec3(random.Float64()*3-1.5, random.Float64()*3-1.5, random.Float64()*3-1.5)
		ray := Ray{Origin: origin, Direction: normalizeVector(subVectors(target, origin))}
		if ray.Direction == (Vector3{}) {
			continue
		}
		want, wantOK := flat.intersect(ray)
		got, gotOK := soup.intersect(ray)
		if wantOK != gotOK {
			t.Fatalf("trial %d: hierarchy hit = %v, walk hit = %v", trial, gotOK, wantOK)
		}
		if !wantOK {
			continue
		}
		hits++
		if got.Distance != want.Distance || got.Point != want.Point || got.Normal != want.Normal {
			t.Fatalf("trial %d: hierarchy hit %#v, walk hit %#v", trial, got, want)
		}
	}
	if hits < 100 {
		t.Fatalf("weak trial mix: only %d hits", hits)
	}
}

func TestTorusKnotRaycastRejectsTheBoundingSphere(t *testing.T) {
	geometry := TorusKnotGeometry{Radius: 1, Tube: 0.15, RadialSegments: 8, TubularSegments: 48}
	graph := NewGraph(Mesh{ID: "knot", Geometry: geometry})
	// The knot curve never reaches the origin, so a ray straight through the
	// center of the bounding sphere along Y must miss the tube.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 5), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("a ray through the empty middle must miss, got %#v", hit)
	}
	// A ray aimed at a sampled curve point must hit within one tube radius.
	center := torusKnotCurvePoint(1, 0.6)
	hit, ok := RaycastGraph(graph, Ray{Origin: addVectors(center, Vec3(0, 0, 5)), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("a ray aimed at the knot curve must hit the tube")
	}
	if hit.Method != "tessellated-triangle" || hit.Kind != "torusknot" {
		t.Fatalf("expected the tessellated triangle test, got %#v", hit)
	}
	// The tessellation chords both the curve and the cross-section, so the hit sits
	// a little inside the ideal tube surface. Eight radial segments give up to
	// tube*(1-cos(pi/8)) of that, and the curve chord adds the rest.
	if gap := math.Abs(hit.Distance - (5 - 0.15)); gap > 0.05 {
		t.Fatalf("expected the hit near the tube surface, got %v (gap %v)", hit.Distance, gap)
	}
}

// torusKnotCurvePoint samples the knot center curve the tessellation sweeps.
func torusKnotCurvePoint(radius, theta float64) Vector3 {
	sweep := radius * (2 + math.Cos(3*theta)) * 0.5
	return Vec3(sweep*math.Cos(2*theta), sweep*math.Sin(2*theta), radius*math.Sin(3*theta)*0.5)
}

func TestTorusKnotRaycastGrazesTheOutermostTube(t *testing.T) {
	// The bound is 1.5*radius + tube, reached where the curve crosses the XY
	// plane at its widest. A ray just inside that reach must hit.
	geometry := TorusKnotGeometry{Radius: 1, Tube: 0.2, RadialSegments: 16, TubularSegments: 64}
	radius, _ := geometryBounds(geometry)
	if math.Abs(radius-1.7) > 1e-12 {
		t.Fatalf("knot bound = %v, want 1.7", radius)
	}
	graph := NewGraph(Mesh{ID: "knot", Geometry: geometry})
	// theta = 0 puts the curve at (1.5, 0, 0). The tube reaches x = 1.7 there.
	ray := Ray{Origin: Vec3(1.68, 0, 5), Direction: Vec3(0, 0, -1)}
	hit, ok := RaycastGraph(graph, ray)
	if !ok {
		t.Fatal("a ray grazing the outermost tube must hit")
	}
	if hit.Method != "tessellated-triangle" {
		t.Fatalf("expected the tessellated triangle test to run, got %q", hit.Method)
	}
	if hit.Distance > 5 {
		t.Fatalf("expected the hit before the curve plane, got %v", hit.Distance)
	}
}

func TestTorusKnotSoupIsCachedByItsNumbers(t *testing.T) {
	first := torusKnotSoup(TorusKnotGeometry{Radius: 0.5, Tube: 0.1, RadialSegments: 6, TubularSegments: 20})
	second := torusKnotSoup(TorusKnotGeometry{Radius: 0.5, Tube: 0.1, RadialSegments: 6, TubularSegments: 20})
	if first != second {
		t.Fatal("the same knot numbers must reuse one tessellation")
	}
	other := torusKnotSoup(TorusKnotGeometry{Radius: 0.5, Tube: 0.1, RadialSegments: 6, TubularSegments: 21})
	if first == other {
		t.Fatal("different knot numbers must build their own tessellation")
	}
	// A zero field resolves to the renderer default, so both spellings share one
	// entry.
	if torusKnotSoup(TorusKnotGeometry{}) != torusKnotSoup(TorusKnotGeometry{Radius: 0.17, Tube: 0.045, RadialSegments: 16, TubularSegments: 128}) {
		t.Fatal("the default knot must resolve to one tessellation")
	}
}

func TestTorusKnotSoupIsSafeUnderConcurrentRays(t *testing.T) {
	// The tessellation cache is process wide, so many goroutines may reach it at
	// once. Run this under the race detector.
	graph := NewGraph(
		Mesh{ID: "knotA", Geometry: TorusKnotGeometry{Radius: 1, Tube: 0.2, RadialSegments: 6, TubularSegments: 18}},
		Mesh{ID: "knotB", Geometry: TorusKnotGeometry{Radius: 1, Tube: 0.2, RadialSegments: 7, TubularSegments: 19}},
	)
	done := make(chan int, 8)
	for worker := 0; worker < 8; worker++ {
		go func(seed int64) {
			random := rand.New(rand.NewSource(seed))
			hits := 0
			for i := 0; i < 200; i++ {
				origin := Vec3(random.Float64()*6-3, random.Float64()*6-3, random.Float64()*6-3)
				ray := Ray{Origin: origin, Direction: normalizeVector(scaleVector(origin, -1))}
				if _, ok := RaycastGraph(graph, ray); ok {
					hits++
				}
			}
			done <- hits
		}(int64(worker) + 1)
	}
	total := 0
	for worker := 0; worker < 8; worker++ {
		total += <-done
	}
	if total == 0 {
		t.Fatal("expected the concurrent rays to reach the knots")
	}
}

// quadBuffer builds a two-triangle square in the XY plane at z = 0.
func quadBuffer(half float64) BufferGeometry {
	return BufferGeometry{
		Positions: []float64{
			-half, -half, 0,
			half, -half, 0,
			half, half, 0,
			-half, half, 0,
		},
		Indices: []int{0, 1, 2, 0, 2, 3},
	}
}

func TestBufferGeometryRaycastHitsItsTriangles(t *testing.T) {
	graph := NewGraph(Mesh{ID: "panel", Geometry: quadBuffer(1)})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.5, 0.25, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected a triangle hit")
	}
	if hit.Method != "mesh-triangle" || hit.Kind != "gltf-mesh" {
		t.Fatalf("expected the triangle mesh test, got %#v", hit)
	}
	if math.Abs(hit.Distance-4) > 1e-9 {
		t.Fatalf("expected distance 4, got %v", hit.Distance)
	}
	if math.Abs(hit.Normal.Z-1) > 1e-9 {
		t.Fatalf("expected a normal facing the ray, got %#v", hit.Normal)
	}
	// Outside the square, inside the old unit-cube fallback: this must miss.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(1.5, 0, 4), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("a ray outside the triangles must miss, got %#v", hit)
	}
}

func TestBufferGeometryRaycastBoundsFollowTheVertices(t *testing.T) {
	// The old fallback bounded every unknown geometry with a unit cube, which
	// would drop hits on any larger mesh. The bound now reads the vertices.
	geometry := quadBuffer(4)
	radius, strokes := geometryBounds(geometry)
	if math.Abs(radius-math.Hypot(4, 4)) > 1e-12 || strokes != 0 {
		t.Fatalf("buffer bounds = %v, %v, want %v, 0", radius, strokes, math.Hypot(4, 4))
	}
	graph := NewGraph(Mesh{ID: "panel", Geometry: geometry})
	// A grazing ray a hair inside the far corner of the square.
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(3.999999, 3.999999, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("a ray grazing the far corner must hit")
	}
	if hit.Method != "mesh-triangle" {
		t.Fatalf("expected the triangle test to run, got %q", hit.Method)
	}
}

func TestBufferGeometryRaycastHonorsPointerGeometry(t *testing.T) {
	geometry := quadBuffer(1)
	graph := NewGraph(Mesh{ID: "panel", Geometry: &geometry})
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)}); !ok {
		t.Fatal("a pointer BufferGeometry must pick like the value form")
	}
}

func TestPolygonGeometryRaycastIsExact(t *testing.T) {
	// A square ring with a square hole. The hole must not pick.
	outer := []float64{-2, -2, 2, -2, 2, 2, -2, 2}
	hole := []float64{-0.5, -0.5, -0.5, 0.5, 0.5, 0.5, 0.5, -0.5}
	geometry := PolygonGeometry(outer, [][]float64{hole}, 0)
	graph := NewGraph(Mesh{ID: "deck", Geometry: geometry})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(1.5, 3, 1.5), Direction: Vec3(0, -1, 0)})
	if !ok {
		t.Fatal("expected a hit on the polygon surface")
	}
	if hit.Method != "mesh-triangle" {
		t.Fatalf("expected the triangle test, got %#v", hit)
	}
	if math.Abs(hit.Distance-3) > 1e-9 {
		t.Fatalf("expected distance 3 to the ground plane, got %v", hit.Distance)
	}
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 3, 0), Direction: Vec3(0, -1, 0)}); ok {
		t.Fatalf("a ray through the hole must miss, got %#v", hit)
	}
}

// gridBuffer builds a side x side grid of quads in the XZ plane, with a raised
// bump so the surface is not flat.
func gridBuffer(side int, span float64) BufferGeometry {
	step := span / float64(side)
	positions := make([]float64, 0, side*side*18)
	for x := 0; x < side; x++ {
		for z := 0; z < side; z++ {
			x0 := -span/2 + float64(x)*step
			z0 := -span/2 + float64(z)*step
			x1, z1 := x0+step, z0+step
			height := func(x, z float64) float64 { return 0.3 * math.Sin(x) * math.Cos(z) }
			positions = append(positions,
				x0, height(x0, z0), z0,
				x1, height(x1, z0), z0,
				x1, height(x1, z1), z1,
				x0, height(x0, z0), z0,
				x1, height(x1, z1), z1,
				x0, height(x0, z1), z1,
			)
		}
	}
	return BufferGeometry{Positions: positions}
}

// TestRaycastMethodManifest pins the method name every geometry reports and how
// exact that method is. RayHit.Method is the telemetry an agent reads to know how
// much to trust a hit, so the vocabulary may grow but may not shift meaning.
func TestRaycastMethodManifest(t *testing.T) {
	cases := []struct {
		name     string
		geometry Geometry
		kind     string
		method   string
		// exact records whether the method answers for the drawn surface with no
		// approximation of shape.
		exact bool
		// note explains any gap between the method and the drawn surface.
		note string
	}{
		{name: "sphere", geometry: SphereGeometry{Radius: 1}, kind: "sphere", method: "analytic-sphere", exact: true},
		{name: "cube", geometry: CubeGeometry{Size: 2}, kind: "cube", method: "analytic-aabb", exact: true},
		{name: "box", geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2}, kind: "box", method: "analytic-aabb", exact: true},
		{name: "plane", geometry: PlaneGeometry{Width: 2, Height: 2}, kind: "plane", method: "analytic-plane", exact: true},
		{
			name: "cylinder", geometry: CylinderGeometry{RadiusTop: 1, RadiusBottom: 1, Height: 2},
			kind: "cylinder", method: "analytic-frustum", exact: true,
		},
		{
			name: "torus", geometry: TorusGeometry{Radius: 1, Tube: 0.3}, kind: "torus", method: "analytic-torus", exact: true,
			note: "solves the quartic of the ideal surface, which the renderer approximates with segments",
		},
		{
			name: "pyramid", geometry: PyramidGeometry{Width: 2, Height: 2, Depth: 2},
			kind: "pyramid", method: "analytic-pyramid", exact: true,
		},
		{
			name: "lines", geometry: LinesGeometry{Points: []Vector3{Vec3(-1, 0, 0), Vec3(1, 0, 0)}, Segments: [][2]int{{0, 1}}},
			kind: "lines", method: "line-threshold", exact: false,
			note: "a stroke owns no world thickness, so the pick radius comes from RaycastOptions.PointThreshold",
		},
		{
			name: "torusknot", geometry: TorusKnotGeometry{Radius: 1, Tube: 0.3, RadialSegments: 8, TubularSegments: 32},
			kind: "torusknot", method: "tessellated-triangle", exact: false,
			note: "exact for the triangles the renderer draws, which chord the ideal swept tube",
		},
		{
			name: "buffer geometry", geometry: quadBuffer(1), kind: "gltf-mesh", method: "mesh-triangle", exact: true,
			note: "the authored triangles are the surface, so the triangle test is the whole answer",
		},
	}

	ray := Ray{Origin: Vec3(0, 0, 6), Direction: Vec3(0, 0, -1)}
	exactCount := 0
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Aim through the geometry. A torus and a knot hold no surface on their
			// own axis, so those two get an offset ray.
			probe := ray
			switch testCase.geometry.(type) {
			case TorusGeometry:
				probe.Origin = Vec3(1, 0, 6)
			case TorusKnotGeometry:
				probe.Origin = Vec3(1.5, 0, 6)
			}
			hit, ok := RaycastGraph(NewGraph(Mesh{ID: "subject", Geometry: testCase.geometry}), probe)
			if !ok {
				t.Fatalf("expected a hit to report a method (%s)", testCase.note)
			}
			if hit.Kind != testCase.kind || hit.Method != testCase.method {
				t.Fatalf("kind/method = %q/%q, want %q/%q", hit.Kind, hit.Method, testCase.kind, testCase.method)
			}
			// The accelerator must report the same method, or a trace would tell two
			// stories about the same scene.
			accel := NewSceneAccelerator(NewGraph(Mesh{ID: "subject", Geometry: testCase.geometry}))
			fast, ok := accel.Raycast(probe)
			if !ok || fast.Method != hit.Method || fast.Kind != hit.Kind {
				t.Fatalf("accelerator reported %#v, want kind %q method %q", fast, hit.Kind, hit.Method)
			}
		})
		if testCase.exact {
			exactCount++
		}
	}
	if exactCount != 8 {
		t.Fatalf("exact geometry count = %d; update the claim in README.md when this changes", exactCount)
	}
}

func TestBufferGeometryAcceleratorMatchesTheTriangleWalk(t *testing.T) {
	// The accelerator builds a triangle hierarchy; the graph walk tests the
	// triangles in order. Both must return the same triangle for every ray.
	geometry := gridBuffer(24, 8)
	graph := NewGraph(Mesh{ID: "terrain", Geometry: geometry, Rotation: Euler{Y: 0.4}, Scale: Vec3(1, 2, 1)})
	accel := NewSceneAccelerator(graph)
	random := rand.New(rand.NewSource(41))
	hits := 0
	for trial := 0; trial < 600; trial++ {
		// Aim at the surface span so the mix holds enough hits to compare.
		origin := Vec3(random.Float64()*12-6, random.Float64()*8-2, random.Float64()*12-6)
		target := Vec3(random.Float64()*8-4, random.Float64()*2-1, random.Float64()*8-4)
		ray := Ray{Origin: origin, Direction: normalizeVector(subVectors(target, origin))}
		if ray.Direction == (Vector3{}) {
			continue
		}
		want := TraceGraph(graph, ray)
		got := accel.Trace(ray)
		if len(want.Hits) != len(got.Hits) {
			t.Fatalf("trial %d: hit count = %d, want %d", trial, len(got.Hits), len(want.Hits))
		}
		for index := range want.Hits {
			if got.Hits[index] != want.Hits[index] {
				t.Fatalf("trial %d: hit %d = %#v, want %#v", trial, index, got.Hits[index], want.Hits[index])
			}
		}
		if len(want.Hits) > 0 {
			hits++
		}
	}
	if hits < 100 {
		t.Fatalf("weak trial mix: only %d hits", hits)
	}
}
