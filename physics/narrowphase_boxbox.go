package physics

import "math"

// Box-Box (OBB-OBB) narrowphase via Separating Axis Theorem with Sutherland-
// Hodgman face clipping. Falls back to closest-point edge contacts when the
// minimum-penetration axis is one of the 9 edge-cross-product axes.

type boxBoxAxis struct {
	axis  Vec3
	depth float64
	kind  int     // 0..2 = A face, 3..5 = B face, 6..14 = edge cross product
	sign  float64 // axis * sign points from A toward B
	valid bool
}

func collideBoxBox(a, b *Collider) (ContactManifold, bool) {
	halfA := a.halfExtents()
	halfB := b.halfExtents()
	if halfA.X <= 0 || halfA.Y <= 0 || halfA.Z <= 0 ||
		halfB.X <= 0 || halfB.Y <= 0 || halfB.Z <= 0 {
		return ContactManifold{}, false
	}

	centerA := a.WorldCenter()
	centerB := b.WorldCenter()
	rotA := a.WorldRotation()
	rotB := b.WorldRotation()

	axesA := [3]Vec3{
		rotA.Rotate(Vec3{X: 1}),
		rotA.Rotate(Vec3{Y: 1}),
		rotA.Rotate(Vec3{Z: 1}),
	}
	axesB := [3]Vec3{
		rotB.Rotate(Vec3{X: 1}),
		rotB.Rotate(Vec3{Y: 1}),
		rotB.Rotate(Vec3{Z: 1}),
	}
	hA := [3]float64{halfA.X, halfA.Y, halfA.Z}
	hB := [3]float64{halfB.X, halfB.Y, halfB.Z}
	t := centerB.Sub(centerA)

	// AbsR[i][j] = |axesA[i] . axesB[j]| with epsilon to keep parallel-edge SAT stable.
	var AbsR [3][3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			AbsR[i][j] = math.Abs(axesA[i].Dot(axesB[j])) + 1e-9
		}
	}

	best := boxBoxAxis{depth: math.Inf(1)}

	tryAxis := func(axis Vec3, ra, rb, dist float64, kind int, faceBias float64) bool {
		gap := math.Abs(dist) - (ra + rb)
		if gap > 0 {
			return false
		}
		depth := -gap
		// Face axes are biased to be preferred over near-tied edge axes; this
		// reduces solver flicker on stacking when face/edge depths are equal.
		if !best.valid || depth+faceBias < best.depth {
			s := 1.0
			if dist < 0 {
				s = -1
			}
			best = boxBoxAxis{axis: axis, depth: depth, kind: kind, sign: s, valid: true}
		}
		return true
	}

	const edgePenalty = 1e-4

	// 3 A-face axes
	for i := 0; i < 3; i++ {
		ra := hA[i]
		rb := hB[0]*AbsR[i][0] + hB[1]*AbsR[i][1] + hB[2]*AbsR[i][2]
		if !tryAxis(axesA[i], ra, rb, t.Dot(axesA[i]), i, 0) {
			return ContactManifold{}, false
		}
	}
	// 3 B-face axes
	for j := 0; j < 3; j++ {
		ra := hA[0]*AbsR[0][j] + hA[1]*AbsR[1][j] + hA[2]*AbsR[2][j]
		rb := hB[j]
		if !tryAxis(axesB[j], ra, rb, t.Dot(axesB[j]), 3+j, 0) {
			return ContactManifold{}, false
		}
	}
	// 9 edge cross-product axes. Ericson's simplified ra/rb formulas below are
	// derived for the un-normalized axis A_i × B_j, so we keep the raw axis for
	// the SAT comparison and only normalize when we record a best axis.
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			axis := axesA[i].Cross(axesB[j])
			axisLen2 := axis.Len2()
			if axisLen2 < 1e-6 {
				continue // parallel edges; covered by face axes
			}
			i1 := (i + 1) % 3
			i2 := (i + 2) % 3
			j1 := (j + 1) % 3
			j2 := (j + 2) % 3
			ra := hA[i1]*AbsR[i2][j] + hA[i2]*AbsR[i1][j]
			rb := hB[j1]*AbsR[i][j2] + hB[j2]*AbsR[i][j1]
			// All quantities here are scaled by |axis|; rescale to a normalized
			// depth so tryAxis's best-tracking stays apples-to-apples with the
			// face axes above.
			axisLen := math.Sqrt(axisLen2)
			normAxis := axis.Div(axisLen)
			distN := t.Dot(normAxis)
			raN := ra / axisLen
			rbN := rb / axisLen
			if !tryAxis(normAxis, raN, rbN, distN, 6+i*3+j, edgePenalty) {
				return ContactManifold{}, false
			}
		}
	}

	if !best.valid {
		return ContactManifold{}, false
	}

	normal := best.axis.Mul(best.sign).Normalize()
	if normal.Len2() <= epsilon {
		return ContactManifold{}, false
	}

	var contacts []ContactPoint
	switch {
	case best.kind < 3:
		contacts = clipBoxFace(a, b, centerA, axesA, hA, best.kind, normal,
			centerB, axesB, hB)
	case best.kind < 6:
		// B is reference; reference outward normal points B→A (= -normal).
		contacts = clipBoxFace(b, a, centerB, axesB, hB, best.kind-3, normal.Neg(),
			centerA, axesA, hA)
		// Contacts have refCol=b, incCol=a. Caller's manifold expects (a,b)
		// with normal pointing A→B, so we keep the world point and just need
		// LocalA/LocalB to belong to the correct bodies.
		contacts = swapContactPair(contacts, a, b)
	default:
		i := (best.kind - 6) / 3
		j := (best.kind - 6) % 3
		contacts = boxBoxEdgeContact(a, b, centerA, axesA, hA, i,
			centerB, axesB, hB, j, best.depth)
	}

	if len(contacts) == 0 {
		return ContactManifold{}, false
	}
	return makeContactManifold(a, b, normal, contacts), true
}

// clipBoxFace clips the incident face against the four side planes of the
// reference face and returns up to 4 contact points whose world positions lie
// at or below the reference plane.
//
// refOutward is the outward world-space normal of the chosen reference face.
// Contacts are emitted with LocalA from refCol's body and LocalB from incCol's body.
func clipBoxFace(refCol, incCol *Collider,
	refCenter Vec3, refAxes [3]Vec3, refHalf [3]float64, refAxisIdx int, refOutward Vec3,
	incCenter Vec3, incAxes [3]Vec3, incHalf [3]float64) []ContactPoint {

	refSide := 1.0
	if refAxes[refAxisIdx].Dot(refOutward) < 0 {
		refSide = -1
	}
	refFaceCenter := refCenter.Add(refAxes[refAxisIdx].Mul(refSide * refHalf[refAxisIdx]))
	refFaceNormal := refAxes[refAxisIdx].Mul(refSide).Normalize()
	refDist := refFaceNormal.Dot(refFaceCenter)

	u := (refAxisIdx + 1) % 3
	v := (refAxisIdx + 2) % 3
	uAxis := refAxes[u]
	vAxis := refAxes[v]
	uCenter := uAxis.Dot(refFaceCenter)
	vCenter := vAxis.Dot(refFaceCenter)

	// Pick the incident face: axis k whose outward direction is most antiparallel
	// to refFaceNormal.
	incAxisIdx := 0
	maxAbs := -1.0
	for k := 0; k < 3; k++ {
		d := math.Abs(incAxes[k].Dot(refFaceNormal))
		if d > maxAbs {
			maxAbs = d
			incAxisIdx = k
		}
	}
	incSide := -1.0
	if incAxes[incAxisIdx].Dot(refFaceNormal) < 0 {
		incSide = 1
	}
	incFaceCenter := incCenter.Add(incAxes[incAxisIdx].Mul(incSide * incHalf[incAxisIdx]))
	iu := (incAxisIdx + 1) % 3
	iv := (incAxisIdx + 2) % 3
	uIn := incAxes[iu].Mul(incHalf[iu])
	vIn := incAxes[iv].Mul(incHalf[iv])
	poly := []Vec3{
		incFaceCenter.Add(uIn).Add(vIn),
		incFaceCenter.Sub(uIn).Add(vIn),
		incFaceCenter.Sub(uIn).Sub(vIn),
		incFaceCenter.Add(uIn).Sub(vIn),
	}

	// Sutherland-Hodgman clip against the 4 side planes of the reference face.
	// Each side plane keeps points with (uAxis . p) <= uCenter+halfU, etc.
	poly = clipHalfSpace(poly, uAxis, uCenter+refHalf[u])
	poly = clipHalfSpace(poly, uAxis.Neg(), -(uCenter - refHalf[u]))
	poly = clipHalfSpace(poly, vAxis, vCenter+refHalf[v])
	poly = clipHalfSpace(poly, vAxis.Neg(), -(vCenter - refHalf[v]))
	if len(poly) == 0 {
		return nil
	}

	contacts := make([]ContactPoint, 0, 4)
	for _, p := range poly {
		signed := refFaceNormal.Dot(p) - refDist
		if signed > contactTolerance {
			continue
		}
		penetration := -signed
		contacts = append(contacts, makeContactPoint(refCol, incCol, p, penetration))
		if len(contacts) >= 4 {
			break
		}
	}
	if len(contacts) > 4 {
		contacts = contacts[:4]
	}
	return contacts
}

// clipHalfSpace returns the polygon intersected with the half-space n.p <= dist.
func clipHalfSpace(poly []Vec3, normal Vec3, dist float64) []Vec3 {
	if len(poly) == 0 {
		return poly
	}
	out := make([]Vec3, 0, len(poly)+1)
	prev := poly[len(poly)-1]
	prevDist := normal.Dot(prev) - dist
	for _, curr := range poly {
		currDist := normal.Dot(curr) - dist
		switch {
		case currDist <= 0 && prevDist <= 0:
			out = append(out, curr)
		case currDist <= 0 && prevDist > 0:
			t := prevDist / (prevDist - currDist)
			out = append(out, prev.Add(curr.Sub(prev).Mul(t)))
			out = append(out, curr)
		case currDist > 0 && prevDist <= 0:
			t := prevDist / (prevDist - currDist)
			out = append(out, prev.Add(curr.Sub(prev).Mul(t)))
		}
		prev = curr
		prevDist = currDist
	}
	return out
}

// swapContactPair rebuilds a contact list so that LocalA/LocalB are in the
// frames of the supplied (a, b) colliders. Used when the SAT picks B as the
// reference box: we initially produce contacts with refCol=b/incCol=a, but the
// caller emits the manifold as (a, b).
func swapContactPair(contacts []ContactPoint, a, b *Collider) []ContactPoint {
	out := make([]ContactPoint, len(contacts))
	for i, c := range contacts {
		out[i] = ContactPoint{
			Point:       c.Point,
			LocalA:      localPoint(a, c.Point),
			LocalB:      localPoint(b, c.Point),
			Penetration: c.Penetration,
		}
	}
	return out
}

// boxBoxEdgeContact returns a single contact at the closest point between two
// near-skew edges from boxes A and B. The edge axes are A.axesA[edgeA] and
// B.axesB[edgeB]; the edge midpoint is offset toward the opposing box along
// the two non-edge axes.
func boxBoxEdgeContact(a, b *Collider,
	centerA Vec3, axesA [3]Vec3, halfA [3]float64, edgeA int,
	centerB Vec3, axesB [3]Vec3, halfB [3]float64, edgeB int,
	depth float64) []ContactPoint {

	dirA := axesA[edgeA]
	dirB := axesB[edgeB]
	uA := (edgeA + 1) % 3
	vA := (edgeA + 2) % 3
	uB := (edgeB + 1) % 3
	vB := (edgeB + 2) % 3

	tA := centerB.Sub(centerA)
	signA := func(d float64) float64 {
		if d < 0 {
			return -1
		}
		return 1
	}
	edgeAOrigin := centerA.
		Add(axesA[uA].Mul(signA(tA.Dot(axesA[uA])) * halfA[uA])).
		Add(axesA[vA].Mul(signA(tA.Dot(axesA[vA])) * halfA[vA]))

	tB := centerA.Sub(centerB)
	edgeBOrigin := centerB.
		Add(axesB[uB].Mul(signA(tB.Dot(axesB[uB])) * halfB[uB])).
		Add(axesB[vB].Mul(signA(tB.Dot(axesB[vB])) * halfB[vB]))

	// Closest point between two segments parameterized as Pa(s) = A0 + s*dirA,
	// Pb(t) = B0 + t*dirB, with s in [-halfA,halfA], t in [-halfB,halfB].
	r := edgeAOrigin.Sub(edgeBOrigin)
	aDotA := dirA.Dot(dirA)
	bDotB := dirB.Dot(dirB)
	bDotA := dirA.Dot(dirB)
	rDotA := dirA.Dot(r)
	rDotB := dirB.Dot(r)
	denom := aDotA*bDotB - bDotA*bDotA

	var s, tParam float64
	if denom > 1e-12 {
		s = (bDotA*rDotB - rDotA*bDotB) / denom
	}
	tParam = (bDotA*s + rDotB) / bDotB
	s = clampFloat(s, -halfA[edgeA], halfA[edgeA])
	tParam = clampFloat(tParam, -halfB[edgeB], halfB[edgeB])

	pointA := edgeAOrigin.Add(dirA.Mul(s))
	pointB := edgeBOrigin.Add(dirB.Mul(tParam))
	contact := pointA.Add(pointB).Mul(0.5)
	return []ContactPoint{makeContactPoint(a, b, contact, depth)}
}
