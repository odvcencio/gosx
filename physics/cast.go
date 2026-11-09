package physics

import "math"

// QueryShape is the moving or probing volume of a shape cast or an overlap
// query. Build one with SphereShape, CapsuleShape or BoxShape. The sizes follow
// ColliderConfig: a capsule's Height is the distance between its cap centres,
// and a box's Width, Height and Depth are full edge lengths.
type QueryShape struct {
	Shape  ColliderShape
	Radius float64
	Width  float64
	Height float64
	Depth  float64
}

// SphereShape returns a sphere query shape.
func SphereShape(radius float64) QueryShape {
	return QueryShape{Shape: ShapeSphere, Radius: math.Abs(radius)}
}

// CapsuleShape returns a capsule query shape around its local +Y axis. height
// is the distance between the two cap centres, so the capsule spans
// height + 2*radius end to end.
func CapsuleShape(radius, height float64) QueryShape {
	return QueryShape{Shape: ShapeCapsule, Radius: math.Abs(radius), Height: math.Abs(height)}
}

// BoxShape returns a box query shape with the given full edge lengths.
func BoxShape(width, height, depth float64) QueryShape {
	return QueryShape{Shape: ShapeBox, Width: math.Abs(width), Height: math.Abs(height), Depth: math.Abs(depth)}
}

// valid reports whether the shape describes a volume a query can use.
func (q QueryShape) valid() bool {
	switch q.Shape {
	case ShapeSphere, ShapeCapsule:
		return q.Radius > 0
	case ShapeBox:
		return q.Width > 0 && q.Height > 0 && q.Depth > 0
	default:
		return false
	}
}

// core returns the shape's support description without its rounding radius,
// placed at center, plus that radius.
func (q QueryShape) core(center Vec3, rotation Quat) (convexShape, float64) {
	shape := convexShape{kind: q.Shape, center: center, basis: mat3FromQuat(rotation)}
	switch q.Shape {
	case ShapeSphere:
		shape.bound = q.Radius
		return shape, q.Radius
	case ShapeCapsule:
		shape.halfH = q.Height * 0.5
		shape.bound = q.Radius + shape.halfH
		return shape, q.Radius
	default:
		shape.half = Vec3{X: q.Width * 0.5, Y: q.Height * 0.5, Z: q.Depth * 0.5}
		shape.bound = shape.half.Len()
		return shape, 0
	}
}

// bounds returns the world box of the shape at a pose.
func (q QueryShape) bounds(center Vec3, rotation Quat) AABB {
	switch q.Shape {
	case ShapeSphere:
		return AABBFromCenterHalfExtents(center, Vec3{X: q.Radius, Y: q.Radius, Z: q.Radius})
	case ShapeCapsule:
		axis := rotation.Rotate(Vec3{Y: q.Height * 0.5})
		return AABBFromCenterHalfExtents(center, Vec3{
			X: math.Abs(axis.X) + q.Radius,
			Y: math.Abs(axis.Y) + q.Radius,
			Z: math.Abs(axis.Z) + q.Radius,
		})
	default:
		return obbAABB(center, rotation, Vec3{X: q.Width * 0.5, Y: q.Height * 0.5, Z: q.Depth * 0.5})
	}
}

// innerRadius is the radius of a ball that always fits inside the shape. The
// generic cast steps by it, so no surface can slip between two samples.
func (q QueryShape) innerRadius() float64 {
	switch q.Shape {
	case ShapeSphere, ShapeCapsule:
		return q.Radius
	default:
		return math.Min(q.Width, math.Min(q.Height, q.Depth)) * 0.5
	}
}

// configureProbe turns a scratch collider into the query shape at a pose, so
// the narrowphase can test it against any collider.
func (q QueryShape) configureProbe(probe *Collider, center Vec3, rotation Quat) {
	*probe = Collider{
		Shape:    q.Shape,
		Offset:   center,
		Rotation: rotation,
		Radius:   q.Radius,
		Width:    q.Width,
		Height:   q.Height,
		Depth:    q.Depth,
		Normal:   Vec3{Y: 1},
		layer:    LayerAll,
		mask:     LayerAll,
	}
}

// ShapeCastHit is the first contact of a swept shape with a collider.
type ShapeCastHit struct {
	Collider *Collider
	Body     *RigidBody
	// Point is the contact point on the hit collider's surface.
	Point Vec3
	// Normal is the unit surface normal at Point. It points from the hit
	// collider back toward the swept shape.
	Normal Vec3
	// Fraction is the share of the cast's maxDistance the shape travelled
	// before it touched, from 0 to 1. Distance is the same in metres.
	Fraction float64
	Distance float64
	// StartOverlap reports that the shape already touched or overlapped the
	// collider at the cast origin. Distance is then 0 and Normal is the
	// direction that separates the two.
	StartOverlap bool
}

// castQuery is one shape cast in flight. The world reuses its buffers between
// casts, so a steady-state cast allocates nothing.
type castQuery struct {
	shape       QueryShape
	core        convexShape
	margin      float64
	origin      Vec3
	rotation    Quat
	direction   Vec3
	maxDistance float64
	translation Vec3
	bounds      AABB

	tris     *[]int
	contacts *[]ContactManifold
	probe    *Collider
}

// prepareCast fills q for a cast and reports false when the inputs cannot
// describe one.
func prepareCast(q *castQuery, shape QueryShape, origin Vec3, rotation Quat, direction Vec3, maxDistance float64) bool {
	if !shape.valid() || !(maxDistance > 0) || math.IsInf(maxDistance, 0) {
		return false
	}
	dir := direction.Normalize()
	if dir.Len2() <= epsilon {
		return false
	}
	if rotation == (Quat{}) {
		rotation = IdentityQuat()
	}
	rotation = rotation.Normalize()
	q.shape = shape
	q.core, q.margin = shape.core(origin, rotation)
	q.origin = origin
	q.rotation = rotation
	q.direction = dir
	q.maxDistance = maxDistance
	q.translation = dir.Mul(maxDistance)
	start := shape.bounds(origin, rotation)
	end := AABB{Min: start.Min.Add(q.translation), Max: start.Max.Add(q.translation)}
	q.bounds = start.Union(end).Expand(castLooseTolerance)
	return true
}

// ShapeCast sweeps shape from origin along direction for at most maxDistance
// metres and returns the first collider it touches. rotation orients the
// shape; the zero Quat means identity. maxDistance must be positive and
// finite.
//
// Every collider shape can be hit. Planes count as solid below their surface.
// When two colliders are hit within 1e-9 m of each other, the one with the
// lower collider index wins, so the answer never depends on the order the
// broadphase finds them in.
//
// A shape that starts touching or overlapping a collider reports it at
// Distance 0 with StartOverlap set, unless the cast direction leads out of it,
// in which case that collider is ignored. That rule is what lets a shape slide
// away from a surface it rests on.
//
// The query reads poses as they are now. Colliders attached to bodies are
// always current; a body-less collider moved by editing its fields is seen
// after the next step. Queries share scratch buffers with the world, so do not
// run them concurrently with each other or with Step.
func (w *World) ShapeCast(shape QueryShape, origin Vec3, rotation Quat, direction Vec3, maxDistance float64, filter QueryFilter) (ShapeCastHit, bool) {
	if w == nil {
		return ShapeCastHit{}, false
	}
	q := &w.castScratch
	if !prepareCast(q, shape, origin, rotation, direction, maxDistance) {
		return ShapeCastHit{}, false
	}
	q.tris, q.contacts, q.probe = &w.queryTris, &w.queryContacts, &w.queryProbe
	candidates := w.gatherQuery(q.bounds, &filter)
	var best ShapeCastHit
	found := false
	limit := 1.0
	for _, collider := range candidates {
		hit, ok := castCollider(collider, q, limit)
		if !ok {
			continue
		}
		// Candidates arrive in collider index order, so keeping the earlier
		// of two hits within 1e-9 m of each other is the index tie-break.
		if found && hit.Distance >= best.Distance-epsilon {
			continue
		}
		best, found = hit, true
		limit = hit.Fraction
	}
	return best, found
}

// SphereCast sweeps a sphere. See ShapeCast.
func (w *World) SphereCast(origin Vec3, radius float64, direction Vec3, maxDistance float64, filter QueryFilter) (ShapeCastHit, bool) {
	return w.ShapeCast(SphereShape(radius), origin, IdentityQuat(), direction, maxDistance, filter)
}

// CapsuleCast sweeps a capsule whose axis is rotation applied to +Y. height is
// the distance between the cap centres. See ShapeCast.
func (w *World) CapsuleCast(origin Vec3, rotation Quat, radius, height float64, direction Vec3, maxDistance float64, filter QueryFilter) (ShapeCastHit, bool) {
	return w.ShapeCast(CapsuleShape(radius, height), origin, rotation, direction, maxDistance, filter)
}

// BoxCast sweeps a box with full edge lengths size. See ShapeCast.
func (w *World) BoxCast(origin Vec3, rotation Quat, size Vec3, direction Vec3, maxDistance float64, filter QueryFilter) (ShapeCastHit, bool) {
	return w.ShapeCast(BoxShape(size.X, size.Y, size.Z), origin, rotation, direction, maxDistance, filter)
}

// ShapeCast sweeps shape against this collider alone. It ignores layers,
// trigger status and every QueryFilter rule; see World.ShapeCast for the rest.
func (c *Collider) ShapeCast(shape QueryShape, origin Vec3, rotation Quat, direction Vec3, maxDistance float64) (ShapeCastHit, bool) {
	if c == nil || c.invalid != nil {
		return ShapeCastHit{}, false
	}
	var q castQuery
	if !prepareCast(&q, shape, origin, rotation, direction, maxDistance) {
		return ShapeCastHit{}, false
	}
	var trisStorage [meshCandidateBuffer]int
	var contactStorage [meshMaxManifolds]ContactManifold
	tris := trisStorage[:0]
	contacts := contactStorage[:0]
	var probe Collider
	q.tris, q.contacts, q.probe = &tris, &contacts, &probe
	return castCollider(c, &q, 1)
}

// castCollider sweeps the query against one collider and reports a hit at a
// fraction no greater than limit.
func castCollider(target *Collider, q *castQuery, limit float64) (ShapeCastHit, bool) {
	var hit ShapeCastHit
	var ok bool
	switch {
	case target.Shape == ShapePlane:
		hit, ok = castPlane(target, q, limit)
	case target.Shape == ShapeTriangleMesh:
		hit, ok = castMesh(target, q, limit)
	case isSupportShape(target):
		hit, ok = castConvex(target, q, limit)
	default:
		hit, ok = castGeneric(target, q, limit)
	}
	if !ok {
		return ShapeCastHit{}, false
	}
	hit.Collider = target
	hit.Body = target.Body
	hit.Distance = float64(hit.Fraction * q.maxDistance)
	return hit, true
}

// targetCore builds a collider's support shape without its rounding radius
// and returns that radius separately.
func targetCore(c *Collider) (convexShape, float64, bool) {
	shape, ok := newConvexShape(c)
	if !ok {
		return convexShape{}, 0, false
	}
	margin := shape.margin
	shape.margin = 0
	return shape, margin, true
}

func castConvex(target *Collider, q *castQuery, limit float64) (ShapeCastHit, bool) {
	core, targetMargin, ok := targetCore(target)
	if !ok {
		return ShapeCastHit{}, false
	}
	result, ok := gjkCast(&q.core, &core, q.margin+targetMargin, q.translation, limit)
	if !ok {
		return ShapeCastHit{}, false
	}
	if result.lambda == 0 {
		return startOverlapHit(q, &core, targetMargin, result)
	}
	return ShapeCastHit{
		Point:    result.pointB.Add(result.normal.Mul(targetMargin)),
		Normal:   result.normal,
		Fraction: result.lambda,
	}, true
}

// startOverlapHit reports a cast that touches its target at the origin. A
// cast that leads out of the target is not a hit.
func startOverlapHit(q *castQuery, core *convexShape, targetMargin float64, result castResult) (ShapeCastHit, bool) {
	normal := result.normal
	point := result.pointB.Add(normal.Mul(targetMargin))
	if result.overlap {
		// The cores intersect, so the gap gives no direction. Ask EPA for
		// the shallowest way out instead.
		moving := q.core
		moving.margin = q.margin
		full := *core
		full.margin = targetMargin
		if simplex, overlap := gjkOverlap(moving, full); overlap {
			if pen, ok := epaPenetration(moving, full, simplex); ok && pen.Normal.Len2() > epsilon {
				normal = pen.Normal.Neg()
				point = pen.PointB
			}
		}
	}
	if normal.Dot(q.direction) >= 0 {
		return ShapeCastHit{}, false
	}
	return ShapeCastHit{Point: point, Normal: normal, StartOverlap: true}, true
}

// castPlane sweeps against a half-space: everything below the plane surface is
// solid.
func castPlane(target *Collider, q *castQuery, limit float64) (ShapeCastHit, bool) {
	normal, offset := target.Plane()
	deepest := q.core.support(normal.Neg())
	gap := normal.Dot(deepest) - offset - q.margin
	approach := normal.Dot(q.direction)
	if gap <= castTolerance {
		if approach >= 0 {
			return ShapeCastHit{}, false
		}
		surface := deepest.Sub(normal.Mul(normal.Dot(deepest) - offset))
		return ShapeCastHit{Point: surface, Normal: normal, StartOverlap: true}, true
	}
	if approach >= 0 {
		return ShapeCastHit{}, false
	}
	distance := gap / -approach
	fraction := distance / q.maxDistance
	if fraction > limit {
		return ShapeCastHit{}, false
	}
	contact := deepest.Add(q.direction.Mul(distance)).Sub(normal.Mul(q.margin))
	return ShapeCastHit{Point: contact, Normal: normal, Fraction: fraction}, true
}

// castMesh sweeps against every triangle of a mesh whose bounds the sweep
// reaches. The sweep runs in mesh-local space, so only the moving shape is
// transformed, not each triangle.
func castMesh(target *Collider, q *castQuery, limit float64) (ShapeCastHit, bool) {
	mesh := target.mesh
	if mesh == nil {
		return ShapeCastHit{}, false
	}
	center := target.WorldCenter()
	rotation := target.WorldRotation()
	basis := mat3FromQuat(rotation)

	local := q.core
	local.center = basis.mulT(q.origin.Sub(center))
	local.basis = mat3FromQuat(rotation.Conjugate().Mul(q.rotation))
	translation := basis.mulT(q.translation)
	direction := basis.mulT(q.direction)

	*q.tris = mesh.queryAABB(target.localAABBFromWorld(q.bounds), (*q.tris)[:0])
	var best castResult
	var bestTri meshTriangle
	found := false
	for _, index := range *q.tris {
		tri := mesh.tris[index]
		shape := triangleShape(tri.a, tri.b, tri.c)
		result, ok := gjkCast(&local, &shape, q.margin, translation, limit)
		if !ok {
			continue
		}
		if found && result.lambda >= best.lambda {
			continue
		}
		if result.lambda == 0 && startLeaves(&local, &shape, q.margin, result, direction) {
			continue
		}
		best, bestTri, found = result, tri, true
		limit = result.lambda
	}
	if !found {
		return ShapeCastHit{}, false
	}
	normal := filterMeshContactNormal(bestTri, best.pointB, best.normal)
	return ShapeCastHit{
		Point:        center.Add(basis.mul(best.pointB)),
		Normal:       basis.mul(normal).Normalize(),
		Fraction:     best.lambda,
		StartOverlap: best.lambda == 0,
	}, true
}

// startLeaves reports whether a cast that touches a triangle at its origin
// moves out of it, in which case the triangle does not block the cast.
func startLeaves(moving, tri *convexShape, margin float64, result castResult, direction Vec3) bool {
	normal := result.normal
	if result.overlap {
		full := *moving
		full.margin = margin
		if simplex, overlap := gjkOverlap(*tri, full); overlap {
			if pen, ok := epaPenetration(*tri, full, simplex); ok && pen.Normal.Len2() > epsilon {
				normal = pen.Normal
			}
		}
	}
	return normal.Dot(direction) >= 0
}

// castGeneric sweeps against a collider that has no dedicated cast path, using
// only the narrowphase. It samples the sweep at steps no longer than the
// shape's inner radius, so a surface the swept centre line crosses cannot slip
// between two samples, then bisects the first touching interval.
//
// Every collider the narrowphase can collide is castable this way, which is
// how shapes added after this file (a heightfield, for one) work with shape
// casts on the day they land.
func castGeneric(target *Collider, q *castQuery, limit float64) (ShapeCastHit, bool) {
	probe := q.probe
	touches := func(distance float64) (ContactManifold, bool) {
		q.shape.configureProbe(probe, q.origin.Add(q.direction.Mul(distance)), q.rotation)
		*q.contacts = CollideAll(probe, target, (*q.contacts)[:0])
		var deepest ContactManifold
		found := false
		for i := range *q.contacts {
			m := &(*q.contacts)[i]
			if m.PointCount == 0 {
				continue
			}
			if !found || deepestPoint(m) > deepestPoint(&deepest) {
				deepest, found = *m, true
			}
		}
		return deepest, found
	}

	if m, ok := touches(0); ok {
		normal := m.Normal.Neg()
		if normal.Dot(q.direction) >= 0 {
			return ShapeCastHit{}, false
		}
		return ShapeCastHit{Point: m.Points[0].Point, Normal: normal, StartOverlap: true}, true
	}
	reach := float64(limit * q.maxDistance)
	step := q.shape.innerRadius()
	if step <= 0 {
		step = reach
	}
	low := 0.0
	for low < reach {
		high := math.Min(low+step, reach)
		m, ok := touches(high)
		if !ok {
			low = high
			continue
		}
		// Bisect [low, high]: low never touches, high always does.
		for i := 0; i < 24 && high-low > castTolerance; i++ {
			mid := (low + high) * 0.5
			if mm, hit := touches(mid); hit {
				high, m = mid, mm
			} else {
				low = mid
			}
		}
		normal := m.Normal.Neg()
		fraction := low / q.maxDistance
		return ShapeCastHit{Point: m.Points[0].Point, Normal: normal, Fraction: fraction}, true
	}
	return ShapeCastHit{}, false
}

func deepestPoint(m *ContactManifold) float64 {
	deepest := math.Inf(-1)
	for i := 0; i < m.PointCount; i++ {
		deepest = math.Max(deepest, m.Points[i].Penetration)
	}
	return deepest
}
