package physics

import (
	"math"
	"slices"
)

// Query candidates.
//
// A query asks the world which colliders a box reaches, then runs the exact
// test on each. Two sources answer:
//
//   - Body-less colliders, which is where level geometry lives, come from the
//     broadphase's static grid. The step keeps that grid current, and a query
//     rebuilds it only when colliders were added or removed since.
//   - Every collider attached to a body comes from a list whose bounds are
//     recomputed when the body's pose changed since the last query. A body
//     moved by game code between steps (a kinematic platform, a character) is
//     therefore always seen where it is now, not where the last step left it.
//
// Candidates are returned sorted by collider index, so every query result is
// independent of the grid's internal order.

// queryEntry caches one body-attached collider's world bounds for the body
// pose they were computed at.
type queryEntry struct {
	collider *Collider
	position Vec3
	rotation Quat
	bounds   AABB
	valid    bool
}

// currentBounds returns the entry's bounds, recomputing them when the body
// moved.
func (e *queryEntry) currentBounds() AABB {
	body := e.collider.Body
	if body == nil {
		return e.collider.AABB()
	}
	if e.valid && body.Position == e.position && body.Rotation == e.rotation {
		return e.bounds
	}
	e.bounds = e.collider.AABB()
	e.position = body.Position
	e.rotation = body.Rotation
	e.valid = true
	return e.bounds
}

// gridQueryCollider reports whether a query reads the collider from the static
// grid: body-less and solid, which is exactly what the grid holds and what no
// step ever moves.
func gridQueryCollider(c *Collider) bool {
	return c.Body == nil && !c.IsTrigger
}

// prepareQueries brings the query sources up to date with the collider set.
func (w *World) prepareQueries() {
	if w.queryVersion != w.colliderVersion || w.broadphase.Built() == 0 {
		w.broadphase.Rebuild(w.colliders)
		w.queryVersion = w.colliderVersion
		w.queryBuilt = 0
	}
	if w.queryBuilt == w.broadphase.Built() {
		return
	}
	// Once per step: re-list the body-attached colliders. Their cached bounds
	// are dropped too, which also picks up a collider resized in place.
	clear(w.queryMovable)
	w.queryMovable = w.queryMovable[:0]
	for _, collider := range w.colliders {
		if collider == nil || gridQueryCollider(collider) {
			continue
		}
		w.queryMovable = append(w.queryMovable, queryEntry{collider: collider})
	}
	w.queryBuilt = w.broadphase.Built()
}

// gatherQuery returns every collider the filter accepts whose bounds overlap
// box, sorted by collider index. The slice is world scratch, reused by the
// next query.
func (w *World) gatherQuery(box AABB, filter *QueryFilter) []*Collider {
	w.prepareQueries()
	out := w.broadphase.QueryStaticAABB(box, w.queryOut[:0])
	kept := out[:0]
	for _, collider := range out {
		// The static grid also holds solid colliders on static and kinematic
		// bodies. The pose-tracked list owns those.
		if gridQueryCollider(collider) && filter.accepts(collider) {
			kept = append(kept, collider)
		}
	}
	out = kept
	for i := range w.queryMovable {
		entry := &w.queryMovable[i]
		if !filter.accepts(entry.collider) {
			continue
		}
		if bounds := entry.currentBounds(); bounds.IsFinite() && !bounds.Overlaps(box) {
			continue
		}
		out = append(out, entry.collider)
	}
	slices.SortFunc(out, compareColliderIndex)
	w.queryOut = out
	return out
}

func compareColliderIndex(a, b *Collider) int {
	return a.index - b.index
}

// RaycastFiltered returns the closest hit among the colliders the filter
// accepts. maxDistance <= 0 means unbounded. Distances within 1e-9 of each
// other count as a tie, which the lower collider index wins.
func (w *World) RaycastFiltered(ray Ray, maxDistance float64, filter QueryFilter) (RaycastHit, bool) {
	if w == nil {
		return RaycastHit{}, false
	}
	direction := ray.Direction.Normalize()
	if direction.Len2() <= epsilon {
		return RaycastHit{}, false
	}
	limit := raycastMaxDistance(maxDistance)
	box := AABB{
		Min: Vec3{X: math.Inf(-1), Y: math.Inf(-1), Z: math.Inf(-1)},
		Max: Vec3{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)},
	}
	if !math.IsInf(limit, 1) {
		end := ray.Origin.Add(direction.Mul(limit))
		box = AABB{Min: ray.Origin.Min(end), Max: ray.Origin.Max(end)}.Expand(epsilon)
	}
	best := RaycastHit{Distance: math.Inf(1)}
	found := false
	for _, collider := range w.gatherQuery(box, &filter) {
		hit, ok := collider.Raycast(ray, limit)
		if !ok {
			continue
		}
		if !found || hit.Distance < best.Distance-epsilon {
			best = hit
			found = true
		}
	}
	return best, found
}

// overlapQuery is one overlap test in flight.
type overlapQuery struct {
	shape    QueryShape
	full     convexShape
	core     convexShape
	margin   float64
	center   Vec3
	rotation Quat
	bounds   AABB

	tris     *[]int
	contacts *[]ContactManifold
	probe    *Collider
}

func prepareOverlap(q *overlapQuery, shape QueryShape, center Vec3, rotation Quat) bool {
	if !shape.valid() {
		return false
	}
	if rotation == (Quat{}) {
		rotation = IdentityQuat()
	}
	rotation = rotation.Normalize()
	q.shape = shape
	q.core, q.margin = shape.core(center, rotation)
	q.full = q.core
	q.full.margin = q.margin
	q.center = center
	q.rotation = rotation
	q.bounds = shape.bounds(center, rotation)
	return true
}

// Overlap appends to out every collider the filter accepts that the shape at
// center overlaps, in ascending collider index order, and returns the result.
// rotation orients the shape; the zero Quat means identity. Pass a reused
// slice to keep the query allocation-free.
func (w *World) Overlap(shape QueryShape, center Vec3, rotation Quat, filter QueryFilter, out []*Collider) []*Collider {
	if w == nil {
		return out
	}
	q := &w.overlapScratch
	if !prepareOverlap(q, shape, center, rotation) {
		return out
	}
	q.tris, q.contacts, q.probe = &w.queryTris, &w.queryContacts, &w.queryProbe
	for _, collider := range w.gatherQuery(q.bounds, &filter) {
		if overlapCollider(collider, q) {
			out = append(out, collider)
		}
	}
	return out
}

// OverlapSphere is Overlap with a sphere.
func (w *World) OverlapSphere(center Vec3, radius float64, filter QueryFilter, out []*Collider) []*Collider {
	return w.Overlap(SphereShape(radius), center, IdentityQuat(), filter, out)
}

// OverlapCapsule is Overlap with a capsule whose axis is rotation applied to
// +Y; height is the distance between the cap centres.
func (w *World) OverlapCapsule(center Vec3, rotation Quat, radius, height float64, filter QueryFilter, out []*Collider) []*Collider {
	return w.Overlap(CapsuleShape(radius, height), center, rotation, filter, out)
}

// OverlapBox is Overlap with a box of full edge lengths size.
func (w *World) OverlapBox(center Vec3, rotation Quat, size Vec3, filter QueryFilter, out []*Collider) []*Collider {
	return w.Overlap(BoxShape(size.X, size.Y, size.Z), center, rotation, filter, out)
}

// Overlaps reports whether shape at center overlaps this collider. It ignores
// layers and trigger status.
func (c *Collider) Overlaps(shape QueryShape, center Vec3, rotation Quat) bool {
	if c == nil || c.invalid != nil {
		return false
	}
	var q overlapQuery
	if !prepareOverlap(&q, shape, center, rotation) {
		return false
	}
	var trisStorage [meshCandidateBuffer]int
	var contactStorage [meshMaxManifolds]ContactManifold
	tris := trisStorage[:0]
	contacts := contactStorage[:0]
	var probe Collider
	q.tris, q.contacts, q.probe = &tris, &contacts, &probe
	return overlapCollider(c, &q)
}

func overlapCollider(target *Collider, q *overlapQuery) bool {
	switch {
	case target.Shape == ShapePlane:
		normal, offset := target.Plane()
		deepest := q.core.support(normal.Neg())
		return normal.Dot(deepest)-offset-q.margin <= 0
	case target.Shape == ShapeTriangleMesh:
		return overlapMesh(target, q)
	case isSupportShape(target):
		core, margin, ok := targetCore(target)
		if !ok {
			return false
		}
		return coresTouch(&q.core, &core, q.margin+margin)
	default:
		q.shape.configureProbe(q.probe, q.center, q.rotation)
		*q.contacts = CollideAll(q.probe, target, (*q.contacts)[:0])
		return len(*q.contacts) > 0
	}
}

// coresTouch reports whether two cores come within margin of each other. It
// is a swept GJK that does not move: the loop reports a miss as soon as a
// supporting plane separates the cores by more than margin, and a touch once
// the gap closes to margin. Unlike the boolean GJK of the narrowphase, it
// probes support functions with unit directions, so it does not miss a
// shallow overlap with a cylinder or cone rim.
func coresTouch(moving, target *convexShape, margin float64) bool {
	_, ok := gjkCast(moving, target, margin, Vec3{}, 0)
	return ok
}

func overlapMesh(target *Collider, q *overlapQuery) bool {
	mesh := target.mesh
	if mesh == nil {
		return false
	}
	center := target.WorldCenter()
	rotation := target.WorldRotation()
	basis := mat3FromQuat(rotation)
	local := q.core
	local.center = basis.mulT(q.center.Sub(center))
	local.basis = mat3FromQuat(rotation.Conjugate().Mul(q.rotation))
	*q.tris = mesh.queryAABB(target.localAABBFromWorld(q.bounds), (*q.tris)[:0])
	for _, index := range *q.tris {
		tri := mesh.tris[index]
		shape := triangleShape(tri.a, tri.b, tri.c)
		if coresTouch(&local, &shape, q.margin) {
			return true
		}
	}
	return false
}

// penetration is the deepest overlap of a query shape with one collider.
type penetration struct {
	// normal is a unit vector from the query shape toward the collider.
	normal Vec3
	depth  float64
}

// penetrateCollider measures how deep the query shape sits in a collider and
// in which direction it should move to leave. It reports false when the two
// do not overlap.
func penetrateCollider(target *Collider, q *overlapQuery) (penetration, bool) {
	switch {
	case target.Shape == ShapePlane:
		normal, offset := target.Plane()
		deepest := q.core.support(normal.Neg())
		gap := normal.Dot(deepest) - offset - q.margin
		if gap >= 0 {
			return penetration{}, false
		}
		return penetration{normal: normal.Neg(), depth: -gap}, true
	case target.Shape == ShapeTriangleMesh:
		return penetrateMesh(target, q)
	case isSupportShape(target):
		shape, ok := newConvexShape(target)
		if !ok {
			return penetration{}, false
		}
		simplex, overlap := gjkOverlap(q.full, shape)
		if !overlap {
			return penetration{}, false
		}
		result, ok := epaPenetration(q.full, shape, simplex)
		if !ok || result.Normal.Len2() <= epsilon {
			return penetration{}, false
		}
		return penetration{normal: result.Normal, depth: result.Depth}, true
	default:
		q.shape.configureProbe(q.probe, q.center, q.rotation)
		*q.contacts = CollideAll(q.probe, target, (*q.contacts)[:0])
		best := penetration{}
		found := false
		for i := range *q.contacts {
			m := &(*q.contacts)[i]
			if depth := deepestPoint(m); m.PointCount > 0 && (!found || depth > best.depth) {
				best, found = penetration{normal: m.Normal, depth: depth}, true
			}
		}
		return best, found && best.depth > 0
	}
}

func penetrateMesh(target *Collider, q *overlapQuery) (penetration, bool) {
	mesh := target.mesh
	if mesh == nil {
		return penetration{}, false
	}
	center := target.WorldCenter()
	rotation := target.WorldRotation()
	basis := mat3FromQuat(rotation)
	local := q.full
	local.center = basis.mulT(q.center.Sub(center))
	local.basis = mat3FromQuat(rotation.Conjugate().Mul(q.rotation))
	*q.tris = mesh.queryAABB(target.localAABBFromWorld(q.bounds), (*q.tris)[:0])
	best := penetration{}
	found := false
	for _, index := range *q.tris {
		tri := mesh.tris[index]
		shape := triangleShape(tri.a, tri.b, tri.c)
		simplex, overlap := gjkOverlap(shape, local)
		if !overlap {
			continue
		}
		result, ok := epaPenetration(shape, local, simplex)
		if !ok || result.Normal.Len2() <= epsilon {
			continue
		}
		if found && result.Depth <= best.depth {
			continue
		}
		// EPA's normal runs from the triangle toward the shape. Filter
		// internal edges, then turn it to run from the shape toward the mesh.
		normal := filterMeshContactNormal(tri, result.PointA, result.Normal)
		best = penetration{normal: basis.mul(normal).Normalize().Neg(), depth: result.Depth}
		found = true
	}
	return best, found
}
