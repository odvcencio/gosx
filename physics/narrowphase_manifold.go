package physics

import "math"

// Manifold expansion for the GJK narrowphase.
//
// GJK with EPA returns one point and one normal. One point is enough to stop
// two shapes overlapping, but it cannot hold a flat face at rest: an angular
// contact impulse at a single point lets the body rock about that point. This
// file turns the EPA normal into a contact patch by clipping the two support
// faces against each other, the same way the box against box path does.
//
// A shape whose extreme feature along the normal is a single point, such as a
// sphere, keeps the single EPA contact. That is the physically correct answer.

const (
	// faceAngleTolerance decides when a face counts as perpendicular to the
	// contact normal. It is the cosine of about four degrees.
	faceAngleTolerance = 0.9976
	// faceFlatTolerance decides when a direction counts as parallel to a cap
	// disc, which is the sine of about four degrees.
	faceFlatTolerance = 0.07
	// hullFaceTolerance is the distance below the extreme support value that a
	// hull vertex may sit and still count as part of the support face.
	hullFaceTolerance = 1e-4
	// maxFacePoints bounds one support face.
	maxFacePoints = 8
)

// patchMergeDistance is the smallest gap between two contact points of one
// manifold. Clipping a face against an identical face lands several points on
// the same corner, and duplicate points give one corner several times the grip
// of the others.
const patchMergeDistance = 1e-5

// reduceContactPatch keeps at most four well spread points from a clipped
// polygon and appends them to out.
//
// The deepest point comes first, then farthest point sampling adds the points
// that are farthest from everything already kept. That rule is deterministic
// and it never returns two points on the same corner, so the four normal
// impulses of a resting box stay balanced from step to step.
func reduceContactPatch(src []ContactPoint, out *[4]ContactPoint) int {
	if len(src) == 0 {
		return 0
	}
	deepest := 0
	for i := 1; i < len(src); i++ {
		if src[i].Penetration > src[deepest].Penetration {
			deepest = i
		}
	}
	out[0] = src[deepest]
	count := 1

	for count < len(out) {
		best := -1
		bestSpread := patchMergeDistance * patchMergeDistance
		for i := range src {
			spread := math.Inf(1)
			for k := 0; k < count; k++ {
				if d := src[i].Point.Sub(out[k].Point).Len2(); d < spread {
					spread = d
				}
			}
			if spread > bestSpread {
				bestSpread = spread
				best = i
			}
		}
		if best < 0 {
			break
		}
		out[count] = src[best]
		count++
	}
	return count
}

// contactFace is the extreme feature of one collider along a world direction,
// ordered around its own centroid.
type contactFace struct {
	points [maxFacePoints]Vec3
	count  int
}

func (f *contactFace) add(p Vec3) {
	if f.count < len(f.points) {
		f.points[f.count] = p
		f.count++
	}
}

func (f contactFace) centroid() Vec3 {
	if f.count == 0 {
		return Vec3{}
	}
	sum := Vec3{}
	for i := 0; i < f.count; i++ {
		sum = sum.Add(f.points[i])
	}
	return sum.Div(float64(f.count))
}

// expandConvexManifold returns the contact patch of two convex colliders for a
// known contact normal and penetration depth. It reports false when the pair
// has no patch, which tells the caller to keep the single EPA point.
//
// normal points from a toward b, which is the manifold convention.
//
// The patch is accepted only when its deepest point agrees with the EPA depth.
// EPA is validated against two independent oracles, so treating it as the
// authority stops a clipping mistake from quietly weakening the solver.
func expandConvexManifold(a, b *Collider, normal Vec3, depth float64, points []ContactPoint) ([]ContactPoint, bool) {
	faceA := supportFace(a, normal)
	faceB := supportFace(b, normal.Neg())
	if faceA.count < 2 || faceB.count < 2 {
		return points, false
	}

	// The reference feature must be a real face. Two edges meet at one point,
	// and expanding that pair invents contacts that are not there.
	reference, incident := faceA, faceB
	refNormal := normal
	if faceB.count > faceA.count {
		reference, incident = faceB, faceA
		refNormal = normal.Neg()
	}
	if reference.count < 3 {
		return points, false
	}

	var clipped [maxFacePoints * 2]Vec3
	polygon := clipAgainstFace(reference, refNormal, incident, clipped[:0])
	if len(polygon) < 2 {
		return points, false
	}

	// Measure from the reference support plane, which is the plane through the
	// farthest reference point along the normal. The face centroid would sit
	// behind that plane whenever the face is not square to the normal, and the
	// patch would then understate every penetration.
	planeDistance := refNormal.Dot(reference.points[0])
	for i := 1; i < reference.count; i++ {
		if d := refNormal.Dot(reference.points[i]); d > planeDistance {
			planeDistance = d
		}
	}

	var candidateStorage [maxFacePoints * 2]ContactPoint
	candidates := candidateStorage[:0]
	deepest := 0.0
	for _, p := range polygon {
		separation := refNormal.Dot(p) - planeDistance
		if separation > contactTolerance {
			continue
		}
		penetration := -separation
		if penetration > deepest {
			deepest = penetration
		}
		// Place the contact midway between the two surfaces, which is where the
		// single-point EPA path puts it too.
		world := p.Add(refNormal.Mul(penetration * 0.5))
		candidates = append(candidates, makeContactPoint(a, b, world, penetration))
	}
	var kept [4]ContactPoint
	count := reduceContactPatch(candidates, &kept)
	if count < 2 {
		return points, false
	}

	// Reject a patch that disagrees with EPA. Feature selection uses angle
	// tolerances, so a near-edge case can clip away the true deepest point.
	if math.Abs(deepest-depth) > maxFloat(1e-4, depth*0.05) {
		return points, false
	}

	// Shift the whole patch so its deepest point carries the EPA depth exactly.
	// EPA is the validated authority on penetration; clipping only decides
	// where the patch lies and how the depth varies across it.
	// reduceContactPatch already put the deepest point in slot zero.
	shift := depth - deepest
	for i := 0; i < count; i++ {
		kept[i].Penetration = maxFloat(0, kept[i].Penetration+shift)
		points = append(points, kept[i])
	}
	return points, true
}

// clipAgainstFace clips the incident polygon against the side planes of the
// reference face and appends the result to out.
func clipAgainstFace(reference contactFace, refNormal Vec3, incident contactFace, out []Vec3) []Vec3 {
	for i := 0; i < incident.count; i++ {
		out = append(out, incident.points[i])
	}
	if reference.count == 2 {
		// A reference edge bounds the patch along its own direction only.
		edge := reference.points[1].Sub(reference.points[0])
		length := edge.Len()
		if length <= epsilon {
			return out
		}
		direction := edge.Div(length)
		out = clipHalfSpace(out, direction, direction.Dot(reference.points[1]))
		out = clipHalfSpace(out, direction.Neg(), -direction.Dot(reference.points[0]))
		return out
	}

	centre := reference.centroid()
	for i := 0; i < reference.count; i++ {
		start := reference.points[i]
		end := reference.points[(i+1)%reference.count]
		side := end.Sub(start).Cross(refNormal)
		if side.Len2() <= epsilon {
			continue
		}
		side = side.Normalize()
		if side.Dot(centre.Sub(start)) > 0 {
			side = side.Neg()
		}
		out = clipHalfSpace(out, side, side.Dot(start))
		if len(out) == 0 {
			return out
		}
	}
	return out
}

// supportFace returns the extreme feature of a collider along a world
// direction. A curved feature reports one point, an edge reports two, and a
// flat face reports three or more ordered around the centroid.
func supportFace(c *Collider, dir Vec3) contactFace {
	var face contactFace
	if c == nil || dir.Len2() <= epsilon {
		return face
	}
	direction := dir.Normalize()
	rotation := c.WorldRotation()
	centre := c.WorldCenter()

	switch c.Shape {
	case ShapeBox:
		boxSupportFace(c, centre, rotation, direction, &face)
	case ShapeConvexHull:
		hullSupportFace(c, centre, rotation, direction, &face)
	case ShapeCylinder:
		cylinderSupportFace(c, centre, direction, &face)
	case ShapeCone:
		coneSupportFace(c, direction, &face)
	case ShapeCapsule:
		capsuleSupportFace(c, direction, &face)
	}
	return face
}

// boxSupportFace returns the true extreme feature of a box along dir. An axis
// that is close to perpendicular to dir leaves the feature free along that
// axis, so the box reports a vertex, an edge or a face as the geometry demands.
func boxSupportFace(c *Collider, centre Vec3, rotation Quat, dir Vec3, face *contactFace) {
	half := c.halfExtents()
	axes := [3]Vec3{
		rotation.Rotate(Vec3{X: 1}),
		rotation.Rotate(Vec3{Y: 1}),
		rotation.Rotate(Vec3{Z: 1}),
	}
	extents := [3]float64{half.X, half.Y, half.Z}

	base := centre
	var free [3]int
	freeCount := 0
	for i := 0; i < 3; i++ {
		along := axes[i].Dot(dir)
		if math.Abs(along) <= faceFlatTolerance {
			free[freeCount] = i
			freeCount++
			continue
		}
		sign := 1.0
		if along < 0 {
			sign = -1
		}
		base = base.Add(axes[i].Mul(sign * extents[i]))
	}

	switch freeCount {
	case 0:
		face.add(base)
	case 1:
		offset := axes[free[0]].Mul(extents[free[0]])
		face.add(base.Sub(offset))
		face.add(base.Add(offset))
	case 2:
		u := axes[free[0]].Mul(extents[free[0]])
		v := axes[free[1]].Mul(extents[free[1]])
		face.add(base.Add(u).Add(v))
		face.add(base.Sub(u).Add(v))
		face.add(base.Sub(u).Sub(v))
		face.add(base.Add(u).Sub(v))
	}
}

func hullSupportFace(c *Collider, centre Vec3, rotation Quat, dir Vec3, face *contactFace) {
	if len(c.hull) == 0 {
		return
	}
	basis := mat3FromQuat(rotation)
	local := basis.mulT(dir)
	best := local.Dot(c.hull[0])
	for _, v := range c.hull[1:] {
		if d := local.Dot(v); d > best {
			best = d
		}
	}
	var collected [maxFacePoints]Vec3
	count := 0
	for _, v := range c.hull {
		if local.Dot(v) < best-hullFaceTolerance {
			continue
		}
		if count < len(collected) {
			collected[count] = centre.Add(basis.mul(v))
			count++
		}
	}
	orderFacePoints(collected[:count], dir, face)
}

func cylinderSupportFace(c *Collider, centre Vec3, dir Vec3, face *contactFace) {
	axis := c.WorldAxis()
	radius := math.Abs(c.Radius)
	half := math.Abs(c.Height) * 0.5
	along := axis.Dot(dir)

	if math.Abs(along) >= faceAngleTolerance {
		// A cap disc faces the other shape. Sample four rim points using a
		// basis built from the contact direction, so two facing cylinders
		// sample the same directions and their patches overlap.
		sign := 1.0
		if along < 0 {
			sign = -1
		}
		capCentre := centre.Add(axis.Mul(sign * half))
		u, v := orthonormalBasis(axis)
		u = u.Mul(radius)
		v = v.Mul(radius)
		face.add(capCentre.Add(u))
		face.add(capCentre.Add(v))
		face.add(capCentre.Sub(u))
		face.add(capCentre.Sub(v))
		return
	}
	if math.Abs(along) <= faceFlatTolerance {
		// The cylinder lies on its side. The extreme feature is the line where
		// the barrel touches, which runs from cap to cap.
		radial := dir.Sub(axis.Mul(along))
		if radial.Len2() <= epsilon {
			return
		}
		radial = radial.Normalize().Mul(radius)
		face.add(centre.Sub(axis.Mul(half)).Add(radial))
		face.add(centre.Add(axis.Mul(half)).Add(radial))
	}
}

func coneSupportFace(c *Collider, dir Vec3, face *contactFace) {
	axis := c.WorldAxis()
	apex, base := c.ConeApexAndBase()
	radius := math.Abs(c.Radius)
	along := axis.Dot(dir)

	if along <= -faceAngleTolerance {
		// The base disc faces the other shape.
		u, v := orthonormalBasis(axis)
		u = u.Mul(radius)
		v = v.Mul(radius)
		face.add(base.Add(u))
		face.add(base.Add(v))
		face.add(base.Sub(u))
		face.add(base.Sub(v))
		return
	}
	if along >= faceAngleTolerance {
		// The apex is a single point.
		return
	}
	radial := dir.Sub(axis.Mul(along))
	if radial.Len2() <= epsilon {
		return
	}
	radial = radial.Normalize().Mul(radius)
	face.add(apex)
	face.add(base.Add(radial))
}

func capsuleSupportFace(c *Collider, dir Vec3, face *contactFace) {
	axis := c.WorldAxis()
	along := axis.Dot(dir)
	if math.Abs(along) > faceFlatTolerance {
		// A hemisphere cap touches, which is a single point.
		return
	}
	p0, p1 := c.CapsuleAxisEndpoints()
	offset := dir.Mul(math.Abs(c.Radius))
	face.add(p0.Add(offset))
	face.add(p1.Add(offset))
}

// orderFacePoints sorts coplanar points around their centroid in the plane
// whose normal is dir, then copies them into face. Sutherland-Hodgman clipping
// needs an ordered polygon.
func orderFacePoints(points []Vec3, dir Vec3, face *contactFace) {
	if len(points) == 0 {
		return
	}
	if len(points) <= 2 {
		for _, p := range points {
			face.add(p)
		}
		return
	}
	centre := Vec3{}
	for _, p := range points {
		centre = centre.Add(p)
	}
	centre = centre.Div(float64(len(points)))
	u, v := orthonormalBasis(dir)

	var angles [maxFacePoints]float64
	for i, p := range points {
		d := p.Sub(centre)
		angles[i] = math.Atan2(v.Dot(d), u.Dot(d))
	}
	// Insertion sort. The list never holds more than maxFacePoints entries.
	for i := 1; i < len(points); i++ {
		angle := angles[i]
		point := points[i]
		j := i - 1
		for j >= 0 && angles[j] > angle {
			angles[j+1] = angles[j]
			points[j+1] = points[j]
			j--
		}
		angles[j+1] = angle
		points[j+1] = point
	}
	for _, p := range points {
		face.add(p)
	}
}
