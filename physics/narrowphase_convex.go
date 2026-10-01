package physics

import "math"

// Narrowphase for the shapes that a support function describes: cylinder, cone
// and convex hull, plus the triangle mesh built from those. Overlap and
// penetration come from GJK and EPA; the plane cases stay analytic because a
// half-space has no support function.

const (
	// meshMaxManifolds bounds how many triangle contacts one mesh pair feeds
	// to the solver. The deepest contacts win.
	meshMaxManifolds = 4
	// meshCandidateBuffer sizes the stack buffer for BVH query results.
	meshCandidateBuffer = 64
	// planeParallelTolerance decides when a cap disc counts as parallel to a
	// plane, which is when the contact is a whole rim instead of one point.
	planeParallelTolerance = 1e-6
)

// isSupportShape reports whether the collider has a support function, which is
// every finite convex primitive.
func isSupportShape(c *Collider) bool {
	if c == nil {
		return false
	}
	switch c.Shape {
	case ShapeBox, ShapeSphere, ShapeCapsule, ShapeCylinder, ShapeCone, ShapeConvexHull:
		return true
	default:
		return false
	}
}

// cylinderParallelTolerance bounds how far two cylinder axes may tilt from
// parallel (or anti-parallel) before collideCylinderCylinder falls back to
// GJK. 1 - cos(angle) below this threshold means the axes agree to within
// about 0.08 degrees, tight enough that the parallel-axis shortcut below
// introduces no visible error even for a very tall cylinder.
const cylinderParallelTolerance = 1e-6

// cylinderAxialMarginFactor sets how deep into each cylinder's axial extent
// the overlap between the two axial ranges must reach before the side-contact
// formula below applies. Right at the boundary the true closest feature can
// be the rim instead of the round side, so the margin -- scaled by the
// smaller radius, which is the size of that rim region -- keeps the shortcut
// away from the case it cannot answer.
const cylinderAxialMarginFactor = 0.01

// cylinderRadialMarginFactor guards the other degenerate case: two axes close
// enough to coincident that the cross-section circles are nested rather than
// crossing. "Radial offset minus combined radius" is the two-circle
// penetration formula for crossing circles; it does not hold when one circle
// sits inside the other, where the true gap is close to the radius
// difference instead. Requiring the radial offset to clear |rA - rB| by a
// margin keeps the shortcut on the crossing side of that boundary.
const cylinderRadialMarginFactor = 0.01

// collideCylinderCylinder resolves the common case of two cylinders whose
// axes run parallel (or anti-parallel) and whose axial extents overlap by a
// safe margin: for an infinite parallel-cylinder pair the separation between
// the round sides is the same at every point along the shared direction, so
// as long as some of that direction lies inside both finite extents, the
// radial-offset-minus-combined-radius answer is exact, not an approximation,
// and there is no need to iterate toward it the way EPA does for a general
// pair. Any other configuration -- tilted axes, or axial ranges that do not
// overlap enough, which means a flat cap is the true closest feature -- falls
// back to collideConvexGJK, which handles every cylinder configuration
// correctly, just slower.
func collideCylinderCylinder(a, b *Collider) (ContactManifold, bool) {
	axis := a.WorldAxis()
	cos := axis.Dot(b.WorldAxis())
	if math.Abs(cos) < 1-cylinderParallelTolerance {
		return collideConvexGJK(a, b)
	}

	radiusA := math.Abs(a.Radius)
	radiusB := math.Abs(b.Radius)
	halfA := math.Abs(a.Height) * 0.5
	halfB := math.Abs(b.Height) * 0.5
	centerA := a.WorldCenter()
	centerB := b.WorldCenter()

	// perp is the (axis-direction-independent) offset between the two axis
	// lines. Because the axes are parallel, perp is the same at every axial
	// position, so any point in the overlap below gives the true separation.
	toB := centerB.Sub(centerA)
	axialB := toB.Dot(axis)
	perp := toB.Sub(axis.Mul(axialB))

	aMin, aMax := -halfA, halfA
	bMin, bMax := axialB-halfB, axialB+halfB
	overlap := minFloat(aMax, bMax) - maxFloat(aMin, bMin)
	margin := cylinderAxialMarginFactor * minFloat(radiusA, radiusB)
	if margin <= 0 {
		margin = epsilon
	}
	if overlap < margin {
		return collideConvexGJK(a, b)
	}

	radialMargin := cylinderRadialMarginFactor * minFloat(radiusA, radiusB)
	if radialMargin <= 0 {
		radialMargin = epsilon
	}
	nestedBound := math.Abs(radiusA-radiusB) + radialMargin
	distance2 := perp.Len2()
	if distance2 < nestedBound*nestedBound {
		// The cross-section circles are nested rather than crossing: "radial
		// offset minus combined radius" is not the two-circle penetration
		// formula there. GJK handles it correctly.
		return collideConvexGJK(a, b)
	}

	radii := radiusA + radiusB
	if distance2 > (radii+contactTolerance)*(radii+contactTolerance) {
		return ContactManifold{}, false
	}
	distance := math.Sqrt(distance2)
	penetration := maxFloat(radii-distance, 0)

	// The radial push is not the only way to separate two finite cylinders:
	// sliding one along the shared axis until the axial ranges stop
	// overlapping is a second, always-valid separating translation, of
	// length overlap. When the radial penetration is not comfortably the
	// smaller of the two, a corner near the rim could beat both, which is
	// exactly the case GJK's real support function already resolves
	// correctly. Requiring the radial push to be less than half the axial
	// one keeps the shortcut inside the regime -- shallow penetration, deep
	// axial overlap -- that is a real resting or rolling contact, and that a
	// two-circle MTV is provably the global minimum for.
	if penetration >= overlap*0.5 {
		return collideConvexGJK(a, b)
	}

	normal := perp.Div(distance)
	// Witness points sit at the middle of the shared axial band, which keeps
	// the reported contact point away from either rim.
	mid := maxFloat(aMin, bMin) + overlap*0.5
	pa := centerA.Add(axis.Mul(mid))
	pb := pa.Add(perp)
	contactPoint := pa.Add(pb).Mul(0.5)
	return makeContactManifold(a, b, normal, []ContactPoint{
		makeContactPoint(a, b, contactPoint, penetration),
	}), true
}

// collideConvexGJK builds a manifold from the GJK and EPA penetration of two
// support-mapped shapes.
//
// EPA gives one normal and one point. The face expansion pass then clips the
// two support faces against each other and returns the whole contact patch. A
// patch matters because contact impulses now carry angular terms: one point
// against a flat face lets the body rock about that point.
//
// A curved feature such as a sphere or a cylinder rim keeps the single EPA
// point, which is its true contact.
func collideConvexGJK(a, b *Collider) (ContactManifold, bool) {
	shapeA, okA := newConvexShape(a)
	shapeB, okB := newConvexShape(b)
	if !okA || !okB {
		return ContactManifold{}, false
	}
	simplex, overlap := gjkOverlap(shapeA, shapeB)
	if !overlap {
		return ContactManifold{}, false
	}
	result, ok := epaPenetration(shapeA, shapeB, simplex)
	if !ok || result.Normal.Len2() <= epsilon {
		return ContactManifold{}, false
	}

	var storage [4]ContactPoint
	if patch, expanded := expandConvexManifold(a, b, result.Normal, result.Depth, storage[:0]); expanded {
		return makeContactManifold(a, b, result.Normal, patch), true
	}
	return makeContactManifold(a, b, result.Normal, []ContactPoint{
		makeContactPoint(a, b, result.ContactPoint(), result.Depth),
	}), true
}

// collideConvexPlane collides any finite convex collider against a plane. The
// normal points from the convex shape toward the plane, matching the box and
// capsule plane cases.
func collideConvexPlane(convex, plane *Collider) (ContactManifold, bool) {
	if convex == nil || plane == nil || convex.Shape == ShapePlane {
		return ContactManifold{}, false
	}
	planeNormal, planeDistance := plane.Plane()

	var candidateStorage [8]Vec3
	candidates := candidateStorage[:0]
	switch convex.Shape {
	case ShapeCylinder:
		candidates = cylinderPlaneCandidates(convex, planeNormal, candidates)
	case ShapeCone:
		candidates = conePlaneCandidates(convex, planeNormal, candidates)
	case ShapeConvexHull:
		return collideHullPlane(convex, plane, planeNormal, planeDistance)
	default:
		return ContactManifold{}, false
	}

	var points [4]ContactPoint
	count := 0
	for _, candidate := range candidates {
		signed := planeNormal.Dot(candidate) - planeDistance
		if signed > contactTolerance {
			continue
		}
		point := makeContactPoint(convex, plane, candidate, maxFloat(-signed, 0))
		count = keepDeepestPoint(&points, count, point)
	}
	if count == 0 {
		return ContactManifold{}, false
	}
	contacts := make([]ContactPoint, count)
	copy(contacts, points[:count])
	return makeContactManifold(convex, plane, planeNormal.Neg(), contacts), true
}

// cylinderPlaneCandidates lists the cylinder points that can touch the plane
// first. Two rim points cover the general and the side-resting cases; four rim
// points cover a cap that lies flat on the plane.
func cylinderPlaneCandidates(cylinder *Collider, planeNormal Vec3, out []Vec3) []Vec3 {
	axis := cylinder.WorldAxis()
	bottom, top := cylinder.CylinderCapCenters()
	radius := math.Abs(cylinder.Radius)

	inward := planeNormal.Neg()
	perp := inward.Sub(axis.Mul(axis.Dot(inward)))
	if perp.Len2() > planeParallelTolerance {
		radial := perp.Normalize().Mul(radius)
		return append(out, bottom.Add(radial), top.Add(radial))
	}

	// The caps are parallel to the plane. Emit four points around the rim of
	// the cap that faces the plane.
	lower := bottom
	if planeNormal.Dot(top) < planeNormal.Dot(bottom) {
		lower = top
	}
	u, v := orthonormalBasis(axis)
	u = u.Mul(radius)
	v = v.Mul(radius)
	return append(out, lower.Add(u), lower.Sub(u), lower.Add(v), lower.Sub(v))
}

// conePlaneCandidates lists the apex plus the base rim points that can touch
// the plane first.
func conePlaneCandidates(cone *Collider, planeNormal Vec3, out []Vec3) []Vec3 {
	axis := cone.WorldAxis()
	apex, base := cone.ConeApexAndBase()
	radius := math.Abs(cone.Radius)

	out = append(out, apex)

	inward := planeNormal.Neg()
	perp := inward.Sub(axis.Mul(axis.Dot(inward)))
	if perp.Len2() > planeParallelTolerance {
		return append(out, base.Add(perp.Normalize().Mul(radius)))
	}
	u, v := orthonormalBasis(axis)
	u = u.Mul(radius)
	v = v.Mul(radius)
	return append(out, base.Add(u), base.Sub(u), base.Add(v), base.Sub(v))
}

func collideHullPlane(hull, plane *Collider, planeNormal Vec3, planeDistance float64) (ContactManifold, bool) {
	center := hull.WorldCenter()
	basis := mat3FromQuat(hull.WorldRotation())

	var points [4]ContactPoint
	count := 0
	for _, local := range hull.hull {
		world := center.Add(basis.mul(local))
		signed := planeNormal.Dot(world) - planeDistance
		if signed > contactTolerance {
			continue
		}
		point := makeContactPoint(hull, plane, world, maxFloat(-signed, 0))
		count = keepDeepestPoint(&points, count, point)
	}
	if count == 0 {
		return ContactManifold{}, false
	}
	contacts := make([]ContactPoint, count)
	copy(contacts, points[:count])
	return makeContactManifold(hull, plane, planeNormal.Neg(), contacts), true
}

// keepDeepestPoint stores point in the fixed manifold slot list, replacing the
// shallowest entry once the list is full.
func keepDeepestPoint(points *[4]ContactPoint, count int, point ContactPoint) int {
	if count < len(points) {
		points[count] = point
		return count + 1
	}
	shallowest := 0
	for i := 1; i < len(points); i++ {
		if points[i].Penetration < points[shallowest].Penetration {
			shallowest = i
		}
	}
	if point.Penetration > points[shallowest].Penetration {
		points[shallowest] = point
	}
	return count
}

// orthonormalBasis returns two unit vectors perpendicular to axis and to each
// other.
func orthonormalBasis(axis Vec3) (Vec3, Vec3) {
	reference := Vec3{X: 1}
	if math.Abs(axis.X) > 0.7 {
		reference = Vec3{Y: 1}
	}
	u := axis.Cross(reference)
	if u.Len2() <= epsilon {
		u = axis.Cross(Vec3{Z: 1})
	}
	u = u.Normalize()
	return u, axis.Cross(u).Normalize()
}

// collideMeshShape collides a triangle mesh against any other collider. The
// mesh always takes the A slot of the manifolds it returns; the caller flips
// them when the original pair order was reversed.
//
// Each contacting triangle produces its own manifold because one manifold
// carries one normal. The deepest meshMaxManifolds contacts survive, so a body
// wedged in a valley still receives an impulse from every distinct face it
// touches.
func collideMeshShape(mesh, other *Collider, out []ContactManifold) []ContactManifold {
	if mesh == nil || mesh.mesh == nil || other == nil {
		return out
	}
	if other.Shape == ShapePlane {
		if manifold, ok := collideMeshPlane(mesh, other); ok {
			return append(out, manifold)
		}
		return out
	}
	if other.Shape == ShapeTriangleMesh {
		// Mesh against mesh is not implemented. World.Diagnostics reports the
		// pair so the configuration fails loudly instead of passing through.
		return out
	}

	otherShape, ok := newConvexShape(other)
	if !ok {
		return out
	}
	box := other.AABB()
	if !box.IsFinite() {
		return out
	}

	var candidateStorage [meshCandidateBuffer]int
	candidates := mesh.mesh.queryAABB(mesh.localAABBFromWorld(box.Expand(contactTolerance)), candidateStorage[:0])
	if len(candidates) == 0 {
		return out
	}

	center := mesh.WorldCenter()
	basis := mat3FromQuat(mesh.WorldRotation())
	// A sphere against a triangle has a closed-form answer. Use it: a ball
	// rolling on mesh terrain is the most common mesh contact of all, and the
	// analytic form is both exact and about fifty times faster than GJK on a
	// curved surface.
	sphereRadius, sphereIsBall := 0.0, other.Shape == ShapeSphere
	if sphereIsBall {
		sphereRadius = math.Abs(other.Radius)
	}
	sphereCenter := other.WorldCenter()

	var best [meshMaxManifolds]epaResult
	bestCount := 0
	for _, index := range candidates {
		local := mesh.mesh.tris[index]
		worldA := center.Add(basis.mul(local.a))
		worldB := center.Add(basis.mul(local.b))
		worldC := center.Add(basis.mul(local.c))

		worldTri := meshTriangle{
			a: worldA, b: worldB, c: worldC,
			normal:       basis.mul(local.normal),
			edgeFiltered: local.edgeFiltered,
		}

		if sphereIsBall {
			result, ok := collideSphereTriangle(sphereCenter, sphereRadius, worldA, worldB, worldC)
			if ok {
				result.Normal = filterMeshContactNormal(worldTri, result.PointA, result.Normal)
				bestCount = keepDeepestResult(&best, bestCount, result)
			}
			continue
		}

		tri := triangleShape(worldA, worldB, worldC)
		simplex, overlap := gjkOverlap(tri, otherShape)
		if !overlap {
			continue
		}
		result, ok := epaPenetration(tri, otherShape, simplex)
		if !ok || result.Normal.Len2() <= epsilon {
			continue
		}
		result.Normal = filterMeshContactNormal(worldTri, result.PointA, result.Normal)
		bestCount = keepDeepestResult(&best, bestCount, result)
	}
	for i := 0; i < bestCount; i++ {
		out = append(out, makeContactManifold(mesh, other, best[i].Normal, []ContactPoint{
			makeContactPoint(mesh, other, best[i].ContactPoint(), best[i].Depth),
		}))
	}
	return out
}

const (
	// meshFaceNormalTolerance is the cosine below which a contact normal counts
	// as an edge normal rather than a face normal.
	meshFaceNormalTolerance = 0.999
	// meshEdgeSnapDistance2 is the squared distance within which a contact
	// counts as sitting on a triangle edge.
	meshEdgeSnapDistance2 = 1e-8
)

// filterMeshContactNormal replaces an internal edge normal with the face normal
// of the triangle that produced it.
//
// A mesh floor made of triangles has shared edges everywhere. A body that
// crosses one can receive the edge normal, which points sideways and throws the
// body off the surface. The mesh marks every edge whose neighbour makes a flat
// or a concave join at build time; a contact on such an edge pushes along the
// face instead. A convex ridge keeps its edge normal, because there the edge is
// the real surface.
func filterMeshContactNormal(tri meshTriangle, contactOnTriangle, normal Vec3) Vec3 {
	if tri.edgeFiltered == 0 || tri.normal.Len2() <= epsilon {
		return normal
	}
	face := tri.normal.Normalize()
	if face.Dot(normal) < 0 {
		face = face.Neg()
	}
	if normal.Dot(face) >= meshFaceNormalTolerance {
		return normal
	}
	for edge := 0; edge < 3; edge++ {
		start, end, filtered := tri.filteredEdgeEndpoints(edge)
		if !filtered {
			continue
		}
		closest, _ := closestPointOnSegment(start, end, contactOnTriangle)
		if closest.Sub(contactOnTriangle).Len2() <= meshEdgeSnapDistance2 {
			return face
		}
	}
	return normal
}

// collideSphereTriangle solves a ball against one triangle in closed form. The
// returned normal points from the triangle toward the sphere, which matches the
// A-to-B convention with the mesh in the A slot.
func collideSphereTriangle(center Vec3, radius float64, a, b, c Vec3) (epaResult, bool) {
	closest := closestPointOnTriangle(center, a, b, c)
	delta := center.Sub(closest)
	distance2 := delta.Len2()
	reach := radius + contactTolerance
	if distance2 > reach*reach {
		return epaResult{}, false
	}

	distance := math.Sqrt(distance2)
	var normal Vec3
	if distance > epsilon {
		normal = delta.Div(distance)
	} else {
		// The sphere centre sits on the triangle. Fall back to the face normal.
		normal = b.Sub(a).Cross(c.Sub(a)).Normalize()
		if normal.Len2() <= epsilon {
			return epaResult{}, false
		}
	}
	depth := maxFloat(radius-distance, 0)
	return epaResult{
		Normal: normal,
		Depth:  depth,
		PointA: closest,
		PointB: center.Sub(normal.Mul(radius)),
	}, true
}

// closestPointOnTriangle returns the point of triangle (a,b,c) nearest to p.
// This is Ericson's Voronoi-region formulation: test the three vertex regions,
// then the three edge regions, then fall through to the face interior.
func closestPointOnTriangle(p, a, b, c Vec3) Vec3 {
	ab := b.Sub(a)
	ac := c.Sub(a)
	ap := p.Sub(a)
	d1 := ab.Dot(ap)
	d2 := ac.Dot(ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}

	bp := p.Sub(b)
	d3 := ab.Dot(bp)
	d4 := ac.Dot(bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}

	vc := d1*d4 - d3*d2
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		denom := d1 - d3
		if denom <= epsilon {
			return a
		}
		return a.Add(ab.Mul(d1 / denom))
	}

	cp := p.Sub(c)
	d5 := ab.Dot(cp)
	d6 := ac.Dot(cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}

	vb := d5*d2 - d1*d6
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		denom := d2 - d6
		if denom <= epsilon {
			return a
		}
		return a.Add(ac.Mul(d2 / denom))
	}

	va := d3*d6 - d5*d4
	if va <= 0 && (d4-d3) >= 0 && (d5-d6) >= 0 {
		denom := (d4 - d3) + (d5 - d6)
		if denom <= epsilon {
			return b
		}
		return b.Add(c.Sub(b).Mul((d4 - d3) / denom))
	}

	denom := va + vb + vc
	if math.Abs(denom) <= epsilon {
		return a
	}
	return a.Add(ab.Mul(vb / denom)).Add(ac.Mul(vc / denom))
}

func keepDeepestResult(best *[meshMaxManifolds]epaResult, count int, result epaResult) int {
	if count < len(best) {
		best[count] = result
		return count + 1
	}
	shallowest := 0
	for i := 1; i < len(best); i++ {
		if best[i].Depth < best[shallowest].Depth {
			shallowest = i
		}
	}
	if result.Depth > best[shallowest].Depth {
		best[shallowest] = result
	}
	return count
}

// collideMeshPlane keeps the deepest four mesh vertices that sit below the
// plane. The BVH prunes whole subtrees that stay above the plane.
func collideMeshPlane(mesh, plane *Collider) (ContactManifold, bool) {
	planeNormal, planeDistance := plane.Plane()
	center := mesh.WorldCenter()
	basis := mat3FromQuat(mesh.WorldRotation())

	// Move the plane into mesh-local space so the BVH bounds can be tested
	// directly.
	localNormal := basis.mulT(planeNormal)
	localDistance := planeDistance - planeNormal.Dot(center)

	var points [4]ContactPoint
	count := 0
	var stack [meshMaxDepth * 2]int
	top := 0
	stack[top] = 0
	top++
	for top > 0 {
		top--
		index := stack[top]
		node := mesh.mesh.nodes[index]
		if boxMinAlong(node.bounds, localNormal) > localDistance+contactTolerance {
			continue
		}
		if node.leaf() {
			for i := node.start; i < node.start+node.count; i++ {
				tri := mesh.mesh.tris[i]
				for _, local := range [3]Vec3{tri.a, tri.b, tri.c} {
					signed := localNormal.Dot(local) - localDistance
					if signed > contactTolerance {
						continue
					}
					world := center.Add(basis.mul(local))
					point := makeContactPoint(mesh, plane, world, maxFloat(-signed, 0))
					count = keepDeepestPoint(&points, count, point)
				}
			}
			continue
		}
		if top+2 > len(stack) {
			continue
		}
		stack[top] = node.right
		top++
		stack[top] = index + 1
		top++
	}
	if count == 0 {
		return ContactManifold{}, false
	}
	contacts := make([]ContactPoint, count)
	copy(contacts, points[:count])
	return makeContactManifold(mesh, plane, planeNormal.Neg(), contacts), true
}

// boxMinAlong returns the smallest dot product of normal with any point of box.
func boxMinAlong(box AABB, normal Vec3) float64 {
	center := box.Center()
	half := box.Size().Mul(0.5)
	return normal.Dot(center) -
		(math.Abs(normal.X)*half.X + math.Abs(normal.Y)*half.Y + math.Abs(normal.Z)*half.Z)
}
