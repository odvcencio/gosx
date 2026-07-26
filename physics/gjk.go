package physics

import (
	"math"
	"sync"
)

// GJK plus EPA over support-mapped convex shapes. GJK answers "do these two
// volumes overlap"; EPA turns an overlapping simplex into a penetration normal,
// a depth and one witness point on each surface.
//
// Sign convention: the Minkowski difference is supportA(d) - supportB(-d), so
// the EPA normal points from A toward B. That matches the manifold normal that
// makeContactManifold expects.

// EPA limits. A polytope with V vertices has 2V-4 faces, so the face budget
// bounds the vertex budget. Flat shapes such as boxes, hulls and triangles
// converge exactly in a handful of expansions. A curved surface converges
// toward an inscribed polytope, so the budget sets the depth accuracy: better
// than 0.3% of the depth for the worst pair of two spheres, and better than
// 1e-6 for anything with a flat contact feature.
//
// The buffers live in a pool rather than on the stack. Declaring arrays this
// large in the frame would make Go clear about 12 kB on every narrowphase call.
const (
	gjkMaxIterations = 32
	epaMaxIterations = 96
	epaMaxVerts      = 128
	epaMaxFaces      = 256
	epaMaxHorizon    = 64
	// epaTolerance is the absolute floor on the remaining gap.
	epaTolerance = 1e-9
	// epaRelativeTolerance stops the expansion once the remaining gap falls
	// below this fraction of the smaller shape's bounding radius.
	//
	// Scaling by the shape size and not by the penetration depth matters: a
	// resting contact has a depth near the solver's slop, and a depth-relative
	// target would demand an absurd absolute accuracy on the most common
	// contact of all. Taking the smaller of the two radii keeps a large floor
	// triangle from loosening the tolerance for a small ball rolling on it.
	//
	// A flat contact feature closes the gap to zero on the first probe, so
	// boxes, hulls, triangles and cylinder caps stay exact. Only a curved
	// surface stops early.
	epaRelativeTolerance = 1e-4
	// epaMonotoneSlack absorbs rounding when comparing face distances against
	// the previous best, and when deciding that a face is coplanar with a new
	// vertex.
	epaMonotoneSlack = 1e-12
)

// minkowskiPoint is one vertex of the Minkowski difference together with the
// two surface points that produced it.
type minkowskiPoint struct {
	diff Vec3
	onA  Vec3
	onB  Vec3
}

func minkowskiSupport(a, b convexShape, dir Vec3) minkowskiPoint {
	pa := a.support(dir)
	pb := b.support(dir.Neg())
	return minkowskiPoint{diff: pa.Sub(pb), onA: pa, onB: pb}
}

type gjkSimplex struct {
	points [4]minkowskiPoint
	count  int
}

// gjkOverlap reports whether the two shapes intersect. On a true result the
// simplex holds a tetrahedron that encloses the origin, ready for EPA.
func gjkOverlap(a, b convexShape) (gjkSimplex, bool) {
	var simplex gjkSimplex

	dir := b.center.Sub(a.center)
	if dir.Len2() <= epsilon {
		dir = Vec3{X: 1}
	}
	simplex.points[0] = minkowskiSupport(a, b, dir)
	simplex.count = 1

	dir = simplex.points[0].diff.Neg()
	if dir.Len2() <= epsilon {
		// The first support point is the origin. The shapes touch with zero
		// depth, which EPA cannot expand from.
		return simplex, false
	}

	for i := 0; i < gjkMaxIterations; i++ {
		next := minkowskiSupport(a, b, dir)
		if next.diff.Dot(dir) < 0 {
			return simplex, false
		}
		simplex.points[simplex.count] = next
		simplex.count++

		contains, newDir := gjkReduce(&simplex)
		if contains {
			return simplex, true
		}
		if newDir.Len2() <= epsilon {
			return simplex, false
		}
		dir = newDir
	}
	return simplex, false
}

// gjkReduce drops the simplex features that cannot contain the origin and
// returns the next search direction. It reports true when the simplex is a
// tetrahedron that encloses the origin.
func gjkReduce(s *gjkSimplex) (bool, Vec3) {
	switch s.count {
	case 2:
		return false, gjkReduceLine(s)
	case 3:
		return false, gjkReduceTriangle(s)
	case 4:
		return gjkReduceTetrahedron(s)
	}
	return false, s.points[0].diff.Neg()
}

func gjkReduceLine(s *gjkSimplex) Vec3 {
	newest := s.points[1]
	other := s.points[0]
	ao := newest.diff.Neg()
	ab := other.diff.Sub(newest.diff)
	if ab.Dot(ao) > 0 {
		dir := ab.Cross(ao).Cross(ab)
		if dir.Len2() <= epsilon {
			dir = anyPerpendicular(ab)
		}
		return dir
	}
	s.points[0] = newest
	s.count = 1
	return ao
}

func gjkReduceTriangle(s *gjkSimplex) Vec3 {
	a := s.points[2]
	b := s.points[1]
	c := s.points[0]
	ao := a.diff.Neg()
	ab := b.diff.Sub(a.diff)
	ac := c.diff.Sub(a.diff)
	abc := ab.Cross(ac)

	if abc.Cross(ac).Dot(ao) > 0 {
		if ac.Dot(ao) > 0 {
			s.points[0] = c
			s.points[1] = a
			s.count = 2
			dir := ac.Cross(ao).Cross(ac)
			if dir.Len2() <= epsilon {
				dir = anyPerpendicular(ac)
			}
			return dir
		}
		return gjkReduceTriangleEdgeAB(s, a, b, ab, ao)
	}
	if ab.Cross(abc).Dot(ao) > 0 {
		return gjkReduceTriangleEdgeAB(s, a, b, ab, ao)
	}

	if abc.Len2() <= epsilon {
		// Degenerate triangle. Fall back to the edge case.
		return gjkReduceTriangleEdgeAB(s, a, b, ab, ao)
	}
	if abc.Dot(ao) > 0 {
		s.points[0] = c
		s.points[1] = b
		s.points[2] = a
		s.count = 3
		return abc
	}
	s.points[0] = b
	s.points[1] = c
	s.points[2] = a
	s.count = 3
	return abc.Neg()
}

func gjkReduceTriangleEdgeAB(s *gjkSimplex, a, b minkowskiPoint, ab, ao Vec3) Vec3 {
	if ab.Dot(ao) > 0 {
		s.points[0] = b
		s.points[1] = a
		s.count = 2
		dir := ab.Cross(ao).Cross(ab)
		if dir.Len2() <= epsilon {
			dir = anyPerpendicular(ab)
		}
		return dir
	}
	s.points[0] = a
	s.count = 1
	return ao
}

func gjkReduceTetrahedron(s *gjkSimplex) (bool, Vec3) {
	a := s.points[3]
	b := s.points[2]
	c := s.points[1]
	d := s.points[0]

	// Each face is oriented away from the vertex it does not contain, so the
	// test works whatever winding the simplex arrived in.
	if faceSeparatesOrigin(a.diff, b.diff, c.diff, d.diff) {
		s.points[0] = c
		s.points[1] = b
		s.points[2] = a
		s.count = 3
		return false, gjkReduceTriangle(s)
	}
	if faceSeparatesOrigin(a.diff, c.diff, d.diff, b.diff) {
		s.points[0] = d
		s.points[1] = c
		s.points[2] = a
		s.count = 3
		return false, gjkReduceTriangle(s)
	}
	if faceSeparatesOrigin(a.diff, d.diff, b.diff, c.diff) {
		s.points[0] = b
		s.points[1] = d
		s.points[2] = a
		s.count = 3
		return false, gjkReduceTriangle(s)
	}
	return true, Vec3{}
}

// faceSeparatesOrigin reports whether the origin sits on the outward side of
// triangle (p0,p1,p2), where outward means away from inner.
func faceSeparatesOrigin(p0, p1, p2, inner Vec3) bool {
	normal := p1.Sub(p0).Cross(p2.Sub(p0))
	if normal.Len2() <= 1e-24 {
		return false
	}
	if normal.Dot(inner.Sub(p0)) > 0 {
		normal = normal.Neg()
	}
	return normal.Dot(p0.Neg()) > 0
}

func anyPerpendicular(v Vec3) Vec3 {
	axis := Vec3{X: 1}
	if math.Abs(v.X) > math.Abs(v.Y) {
		axis = Vec3{Y: 1}
	}
	perp := v.Cross(axis)
	if perp.Len2() <= epsilon {
		perp = v.Cross(Vec3{Z: 1})
	}
	return perp
}

// epaResult carries the penetration data of one overlapping pair.
type epaResult struct {
	// Normal is a unit vector that points from A toward B.
	Normal Vec3
	// Depth is the penetration distance along Normal.
	Depth float64
	// PointA and PointB are the witness points on the two surfaces.
	PointA Vec3
	PointB Vec3
}

// ContactPoint returns the midpoint between the two witness points, which is
// the convention the other narrowphase routines use.
func (r epaResult) ContactPoint() Vec3 {
	return r.PointA.Add(r.PointB).Mul(0.5)
}

type epaFace struct {
	a      int
	b      int
	c      int
	normal Vec3
	dist   float64
}

type epaEdge struct {
	from int
	to   int
}

// epaWorkspace holds the reusable polytope buffers of one EPA run.
type epaWorkspace struct {
	verts   []minkowskiPoint
	faces   []epaFace
	horizon []epaEdge
}

var epaPool = sync.Pool{
	New: func() any {
		return &epaWorkspace{
			verts:   make([]minkowskiPoint, 0, epaMaxVerts),
			faces:   make([]epaFace, 0, epaMaxFaces),
			horizon: make([]epaEdge, 0, epaMaxHorizon),
		}
	},
}

// epaPenetration expands the GJK tetrahedron until it touches the boundary of
// the Minkowski difference, then reports the penetration normal, depth and
// witness points.
func epaPenetration(a, b convexShape, simplex gjkSimplex) (epaResult, bool) {
	if simplex.count != 4 {
		return epaResult{}, false
	}

	workspace := epaPool.Get().(*epaWorkspace)
	defer epaPool.Put(workspace)

	verts := workspace.verts[:0]
	verts = append(verts, simplex.points[0], simplex.points[1], simplex.points[2], simplex.points[3])

	tolerance := epaGapTolerance(a, b)

	faces := workspace.faces[:0]
	tetra := [4][4]int{
		{1, 2, 3, 0},
		{0, 2, 3, 1},
		{0, 1, 3, 2},
		{0, 1, 2, 3},
	}
	for _, face := range tetra {
		f, ok := makeSeedFace(verts, face[0], face[1], face[2], verts[face[3]].diff)
		if !ok {
			return epaResult{}, false
		}
		faces = append(faces, f)
	}

	// While the polytope stays closed and holds the origin, the closest-face
	// distance never decreases and never drops below zero. Recording the best
	// face and stopping the moment either invariant breaks means a numerically
	// broken polytope reports a slightly conservative depth instead of a bogus
	// one. A zero depth would be the worst outcome: the solver would accept the
	// contact and then correct nothing.
	var best epaFace
	haveBest := false

	for iteration := 0; iteration < epaMaxIterations; iteration++ {
		index := closestFace(faces)
		if index < 0 {
			break
		}
		face := faces[index]
		// A closest face at distance zero is legitimate: the origin can sit on
		// the seed tetrahedron's boundary, and the expansion then moves it
		// inward. Only a clearly negative distance means the polytope broke.
		if face.dist < -epaMonotoneSlack || (haveBest && face.dist < best.dist-epaMonotoneSlack) {
			break
		}
		best = face
		haveBest = true

		next := minkowskiSupport(a, b, face.normal)
		if next.diff.Dot(face.normal)-face.dist < tolerance {
			break
		}
		if len(verts) >= epaMaxVerts || len(faces)+epaMaxHorizon > epaMaxFaces {
			// Out of room. The current closest face is the best answer we have.
			break
		}
		if vertexAlreadyPresent(verts, next.diff) {
			break
		}

		newIndex := len(verts)
		verts = append(verts, next)

		horizon := workspace.horizon[:0]
		kept := faces[:0]
		overflow := false
		for _, candidate := range faces {
			// Treat a face that the new vertex is coplanar with as visible.
			// Keeping such a face leaves a zero-area sliver, and the sliver makes
			// the polytope non-convex a few expansions later.
			if candidate.normal.Dot(next.diff)-candidate.dist > -epaMonotoneSlack {
				if len(horizon)+3 > epaMaxHorizon {
					overflow = true
					break
				}
				horizon = addHorizonEdge(horizon, candidate.a, candidate.b)
				horizon = addHorizonEdge(horizon, candidate.b, candidate.c)
				horizon = addHorizonEdge(horizon, candidate.c, candidate.a)
				continue
			}
			kept = append(kept, candidate)
		}
		if overflow {
			break
		}
		faces = kept
		degenerate := false
		for _, edge := range horizon {
			// The horizon edges keep the winding of the faces they came from,
			// so the new faces inherit the outward orientation of the seed
			// tetrahedron. Re-deriving the orientation here would break edge
			// cancellation on the next expansion.
			f, ok := makeWoundFace(verts, edge.from, edge.to, newIndex)
			if !ok {
				// Skipping a face would leave a hole, and a holed polytope no
				// longer bounds the origin.
				degenerate = true
				break
			}
			faces = append(faces, f)
		}
		if degenerate || len(faces) == 0 {
			break
		}
	}

	if !haveBest {
		return epaResult{}, false
	}
	return extractEPAResult(verts, best), true
}

// makeSeedFace builds one face of the starting tetrahedron with its normal
// turned away from the opposite vertex. Doing this for all four faces gives the
// polytope a single consistent outward winding.
func makeSeedFace(verts []minkowskiPoint, ia, ib, ic int, inner Vec3) (epaFace, bool) {
	normal, ok := faceNormal(verts, ia, ib, ic)
	if !ok {
		return epaFace{}, false
	}
	p0 := verts[ia].diff
	if normal.Dot(inner.Sub(p0)) > 0 {
		normal = normal.Neg()
		ib, ic = ic, ib
	}
	return epaFace{a: ia, b: ib, c: ic, normal: normal, dist: normal.Dot(p0)}, true
}

// makeWoundFace builds a face and trusts the given winding to already point
// outward.
func makeWoundFace(verts []minkowskiPoint, ia, ib, ic int) (epaFace, bool) {
	normal, ok := faceNormal(verts, ia, ib, ic)
	if !ok {
		return epaFace{}, false
	}
	return epaFace{a: ia, b: ib, c: ic, normal: normal, dist: normal.Dot(verts[ia].diff)}, true
}

func faceNormal(verts []minkowskiPoint, ia, ib, ic int) (Vec3, bool) {
	p0 := verts[ia].diff
	normal := verts[ib].diff.Sub(p0).Cross(verts[ic].diff.Sub(p0))
	length2 := normal.Len2()
	if length2 <= 1e-24 {
		return Vec3{}, false
	}
	return normal.Div(math.Sqrt(length2)), true
}

// epaGapTolerance returns the gap below which the expansion stops.
func epaGapTolerance(a, b convexShape) float64 {
	scale := math.Min(a.bound, b.bound)
	relative := epaRelativeTolerance * scale
	if relative > epaTolerance {
		return relative
	}
	return epaTolerance
}

func closestFace(faces []epaFace) int {
	best := -1
	bestDist := math.Inf(1)
	for i, face := range faces {
		if face.dist < bestDist {
			bestDist = face.dist
			best = i
		}
	}
	return best
}

// addHorizonEdge keeps only the edges on the silhouette. An edge shared by two
// removed faces appears twice with opposite orientation and cancels.
func addHorizonEdge(horizon []epaEdge, from, to int) []epaEdge {
	for i, edge := range horizon {
		if edge.from == to && edge.to == from {
			horizon = append(horizon[:i], horizon[i+1:]...)
			return horizon
		}
	}
	return append(horizon, epaEdge{from: from, to: to})
}

func vertexAlreadyPresent(verts []minkowskiPoint, p Vec3) bool {
	for _, v := range verts {
		if v.diff.Sub(p).Len2() <= 1e-20 {
			return true
		}
	}
	return false
}

// extractEPAResult reads the penetration off the closest face. The origin is
// projected onto the face plane, and the barycentric weights of that projection
// blend the stored surface points into one witness point per shape.
func extractEPAResult(verts []minkowskiPoint, face epaFace) epaResult {
	projected := face.normal.Mul(face.dist)
	u, v, w := barycentric(verts[face.a].diff, verts[face.b].diff, verts[face.c].diff, projected)
	pointA := verts[face.a].onA.Mul(u).
		Add(verts[face.b].onA.Mul(v)).
		Add(verts[face.c].onA.Mul(w))
	pointB := verts[face.a].onB.Mul(u).
		Add(verts[face.b].onB.Mul(v)).
		Add(verts[face.c].onB.Mul(w))
	return epaResult{
		Normal: face.normal,
		Depth:  maxFloat(face.dist, 0),
		PointA: pointA,
		PointB: pointB,
	}
}

// barycentric returns clamped, normalized weights of q inside triangle
// (p0,p1,p2).
func barycentric(p0, p1, p2, q Vec3) (float64, float64, float64) {
	v0 := p1.Sub(p0)
	v1 := p2.Sub(p0)
	v2 := q.Sub(p0)
	d00 := v0.Dot(v0)
	d01 := v0.Dot(v1)
	d11 := v1.Dot(v1)
	d20 := v2.Dot(v0)
	d21 := v2.Dot(v1)
	denom := d00*d11 - d01*d01
	if math.Abs(denom) <= 1e-24 {
		return 1, 0, 0
	}
	v := (d11*d20 - d01*d21) / denom
	w := (d00*d21 - d01*d20) / denom
	u := 1 - v - w
	u = clampFloat(u, 0, 1)
	v = clampFloat(v, 0, 1)
	w = clampFloat(w, 0, 1)
	sum := u + v + w
	if sum <= epsilon {
		return 1, 0, 0
	}
	return u / sum, v / sum, w / sum
}
