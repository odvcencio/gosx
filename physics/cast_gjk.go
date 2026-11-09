package physics

// Swept GJK for shape casts.
//
// A shape cast moves shape A along a straight line and asks where it first
// touches shape B. Write the configuration space obstacle as D = B - A, the set
// of every b - a. Shape A moved by lambda * r touches B exactly when lambda * r
// lies in D, so the cast is a ray cast from the origin along r against D. GJK
// answers that without ever building D: it only needs D's support function,
// which is B's support in a direction minus A's support in the opposite one.
//
// The loop below is the ray cast of van den Bergen ("Ray casting against
// general convex objects with application to continuous collision detection",
// 2004), with the margin handling of Box2D's b2ShapeCast. Each pass finds the
// supporting plane of D facing the current ray point x. When x sits farther
// than the margin in front of that plane, no point of the ray before the plane
// can touch, so lambda jumps to the plane. Either way the simplex then grows
// toward x. lambda only ever moves forward by a distance the
// plane proves empty, so it never passes the true time of impact: a cast that
// stops early is conservative.
//
// Spheres and capsules enter as their core (a point and a segment) plus a
// margin, the radius. The loop stops when x comes within the summed margin of
// the core obstacle. That keeps round shapes exact and quick, because GJK only
// ever iterates on flat features.

const (
	// castMaxIterations bounds the support calls of one cast. Flat pairs
	// converge in a handful; a cylinder or cone rim converges slowly, which is
	// what the cap is for.
	castMaxIterations = 48
	// castTolerance is the gap, in metres, at which a cast counts as touching.
	castTolerance = 1e-7
	// castLooseTolerance is the gap a cast that ran out of iterations may still
	// report as touching. Beyond it the cast reports a miss.
	castLooseTolerance = 1e-4
	// castTiny guards divisions and normalizations.
	castTiny = 1e-24
	// castNormalFloor is the shortest gap, in metres, whose direction is
	// trusted as the contact normal.
	castNormalFloor = 1e-6
)

// castVertex is one support point of the obstacle D together with the two
// shape points that produced it.
type castVertex struct {
	// p is b - a, a point of D. It is stored unshifted: the simplex point
	// relative to the ray is p - x, recomputed whenever x moves.
	p Vec3
	// a is the moving core's support point at the start pose.
	a Vec3
	// b is the target core's support point.
	b Vec3
}

// castSimplex is the GJK simplex of a cast plus the barycentric weights of
// the closest point the last reduce found.
type castSimplex struct {
	v [4]castVertex
	w [4]float64
	n int
}

// castResult is what a convex cast found.
type castResult struct {
	// lambda is the share of the translation travelled before contact.
	lambda float64
	// normal is a unit vector from the target toward the moving shape.
	normal Vec3
	// pointA and pointB are the closest points on the moving core (at the
	// start pose) and on the target core.
	pointA Vec3
	pointB Vec3
	// overlap reports that the cores themselves touch or intersect at lambda,
	// so normal could not come from the gap between them.
	overlap bool
}

// gjkCast sweeps the moving core along translation against the target core
// and reports the first lambda in [0, maxLambda] at which the cores come
// within margin of each other. Both shapes must have margin zero; the caller
// passes the summed radii as margin.
func gjkCast(moving, target *convexShape, margin float64, translation Vec3, maxLambda float64) (castResult, bool) {
	lambda := 0.0
	x := Vec3{}
	var planeNormal Vec3
	var s castSimplex

	// Seed the search with a real point of D, so |v| starts as an upper bound
	// on the gap. The shape centres would not do: a hull's origin need not lie
	// inside the hull.
	seed := translation.Normalize()
	if seed.Len2() == 0 {
		// A cast that does not move is an overlap test: start from the sides
		// of the two cores that face each other.
		seed = target.center.Sub(moving.center).Normalize()
		if seed.Len2() == 0 {
			seed = Vec3{X: 1}
		}
	}
	seedB := target.support(seed.Neg())
	seedA := moving.support(seed)
	v := seedA.Sub(seedB)
	lastA, lastB := seedA, seedB
	if v.Len2() <= castTiny {
		// The seed point is the origin itself: the cores touch at the start.
		return finishCast(&s, lambda, v, planeNormal, translation, lastA, lastB), true
	}

	converged := false
	for iter := 0; iter < castMaxIterations; iter++ {
		vlen := v.Len()
		if vlen-margin <= castTolerance {
			converged = true
			break
		}
		// Support with a unit direction. The support functions compare the
		// direction's parts against absolute thresholds, so a direction as
		// short as a converging v would lose a cylinder's rim, and a wrong
		// support plane would let lambda jump past the contact.
		dir := v.Div(vlen)
		pb := target.support(dir)
		pa := moving.support(dir.Neg())
		p := pb.Sub(pa)
		w := x.Sub(p)
		vw := v.Dot(w)
		reach := float64(margin * vlen)
		advanced := false
		if vw > reach {
			// The supporting plane facing x proves the stretch of ray up to
			// it empty. Jump there, or report a miss when the ray runs
			// parallel to the plane or away from it.
			vr := v.Dot(translation)
			if vr >= 0 {
				return castResult{}, false
			}
			lambda -= (vw - reach) / vr
			if lambda > maxLambda {
				return castResult{}, false
			}
			x = translation.Mul(lambda)
			planeNormal = v
			advanced = true
		}
		// The simplex survives the jump: its points are points of D, and
		// reduce measures them from the new x. Restarting it instead (as
		// Box2D does) zig-zags toward a curved surface one sliver at a time.
		if s.holds(p) {
			if !advanced {
				// No new support point and no jump: GJK has converged.
				converged = true
				break
			}
		} else {
			s.v[s.n] = castVertex{p: p, a: pa, b: pb}
			s.n++
			lastA, lastB = pa, pb
		}
		closest, inside := s.reduce(x)
		if inside {
			v = Vec3{}
			converged = true
			break
		}
		v = closest.Neg()
	}
	if !converged && v.Len()-margin > castLooseTolerance {
		return castResult{}, false
	}
	return finishCast(&s, lambda, v, planeNormal, translation, lastA, lastB), true
}

// finishCast reads the contact out of a converged cast.
//
// The normal is the gap direction v while the gap is long enough to have a
// direction. Rounded shapes always stop with a gap of about their margin, so
// they take this path. Flat shapes close the gap to nothing, and the direction
// of what is left is rounding noise; the normal is then the last supporting
// plane the ray advanced to, which is the contact normal of van den Bergen's
// algorithm.
func finishCast(s *castSimplex, lambda float64, v, planeNormal, translation, lastA, lastB Vec3) castResult {
	result := castResult{lambda: lambda, pointA: lastA, pointB: lastB}
	if s.n > 0 {
		result.pointA, result.pointB = s.witnesses()
	}
	gap := v.Len()
	result.overlap = gap <= castNormalFloor
	switch {
	case gap > castNormalFloor:
		result.normal = v.Div(gap)
	case planeNormal.Len2() > castTiny:
		result.normal = planeNormal.Div(planeNormal.Len())
	default:
		if length := translation.Len(); length > 0 {
			result.normal = translation.Div(-length)
		} else {
			result.normal = Vec3{Y: 1}
		}
	}
	return result
}

// holds reports whether p is already a vertex of the simplex.
func (s *castSimplex) holds(p Vec3) bool {
	for i := 0; i < s.n; i++ {
		if s.v[i].p.Sub(p).Len2() <= castTiny {
			return true
		}
	}
	return false
}

// witnesses blends the stored shape points with the last reduce weights.
func (s *castSimplex) witnesses() (Vec3, Vec3) {
	var a, b Vec3
	for i := 0; i < s.n; i++ {
		a = a.Add(s.v[i].a.Mul(s.w[i]))
		b = b.Add(s.v[i].b.Mul(s.w[i]))
	}
	return a, b
}

// reduce finds the point of the simplex hull nearest x, drops the vertices
// that do not support it, and stores the weights of the rest. It returns that
// point relative to x, and true when x lies inside a full tetrahedron.
func (s *castSimplex) reduce(x Vec3) (Vec3, bool) {
	switch s.n {
	case 1:
		s.w[0] = 1
		return s.v[0].p.Sub(x), false
	case 2:
		return s.reduceSegment(x), false
	case 3:
		return s.reduceTriangle(x), false
	default:
		return s.reduceTetrahedron(x)
	}
}

func (s *castSimplex) keep1(i int) Vec3 {
	s.v[0] = s.v[i]
	s.w[0] = 1
	s.n = 1
	return s.v[0].p
}

func (s *castSimplex) keep2(i, j int, wi, wj float64) {
	vi, vj := s.v[i], s.v[j]
	s.v[0], s.v[1] = vi, vj
	s.w[0], s.w[1] = wi, wj
	s.n = 2
}

func (s *castSimplex) reduceSegment(x Vec3) Vec3 {
	q0 := s.v[0].p.Sub(x)
	q1 := s.v[1].p.Sub(x)
	e := q1.Sub(q0)
	den := e.Dot(e)
	if den <= castTiny {
		return s.keep1(1).Sub(x)
	}
	t := -q0.Dot(e) / den
	if t <= 0 {
		return s.keep1(0).Sub(x)
	}
	if t >= 1 {
		return s.keep1(1).Sub(x)
	}
	s.w[0], s.w[1] = 1-t, t
	return q0.Add(e.Mul(t))
}

// reduceTriangle is Ericson's closest point on a triangle (Real-Time Collision
// Detection, 5.1.5) with the query point at x, keeping the barycentric
// weights.
func (s *castSimplex) reduceTriangle(x Vec3) Vec3 {
	a := s.v[0].p.Sub(x)
	b := s.v[1].p.Sub(x)
	c := s.v[2].p.Sub(x)
	ab := b.Sub(a)
	ac := c.Sub(a)

	d1 := -ab.Dot(a)
	d2 := -ac.Dot(a)
	if d1 <= 0 && d2 <= 0 {
		return s.keep1(0).Sub(x)
	}
	d3 := -ab.Dot(b)
	d4 := -ac.Dot(b)
	if d3 >= 0 && d4 <= d3 {
		return s.keep1(1).Sub(x)
	}
	vc := float64(d1*d4) - float64(d3*d2)
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		t := d1 / (d1 - d3)
		s.keep2(0, 1, 1-t, t)
		return a.Add(ab.Mul(t))
	}
	d5 := -ab.Dot(c)
	d6 := -ac.Dot(c)
	if d6 >= 0 && d5 <= d6 {
		return s.keep1(2).Sub(x)
	}
	vb := float64(d5*d2) - float64(d1*d6)
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		t := d2 / (d2 - d6)
		s.keep2(0, 2, 1-t, t)
		return a.Add(ac.Mul(t))
	}
	va := float64(d3*d6) - float64(d5*d4)
	if va <= 0 && (d4-d3) >= 0 && (d5-d6) >= 0 {
		t := (d4 - d3) / ((d4 - d3) + (d5 - d6))
		s.keep2(1, 2, 1-t, t)
		return b.Add(c.Sub(b).Mul(t))
	}
	denom := va + vb + vc
	if denom <= castTiny {
		// A degenerate triangle. Its longest edge holds the answer.
		return s.reduceDegenerateTriangle(x)
	}
	v := vb / denom
	w := vc / denom
	s.w[0], s.w[1], s.w[2] = 1-v-w, v, w
	return a.Add(ab.Mul(v)).Add(ac.Mul(w))
}

// reduceDegenerateTriangle handles three collinear points by reducing the two
// that span the most.
func (s *castSimplex) reduceDegenerateTriangle(x Vec3) Vec3 {
	best, bi, bj := -1.0, 0, 1
	pairs := [3][2]int{{0, 1}, {0, 2}, {1, 2}}
	for _, pair := range pairs {
		if d := s.v[pair[0]].p.Sub(s.v[pair[1]].p).Len2(); d > best {
			best, bi, bj = d, pair[0], pair[1]
		}
	}
	vi, vj := s.v[bi], s.v[bj]
	s.v[0], s.v[1] = vi, vj
	s.n = 2
	return s.reduceSegment(x)
}

// tetraFaces lists the four faces of a tetrahedron, each with the vertex it
// leaves out.
var tetraFaces = [4][4]int{
	{0, 1, 2, 3},
	{0, 1, 3, 2},
	{0, 2, 3, 1},
	{1, 2, 3, 0},
}

func (s *castSimplex) reduceTetrahedron(x Vec3) (Vec3, bool) {
	var best castSimplex
	bestDist := -1.0
	var bestPoint Vec3
	for _, face := range tetraFaces {
		a := s.v[face[0]].p
		b := s.v[face[1]].p
		c := s.v[face[2]].p
		d := s.v[face[3]].p
		normal := b.Sub(a).Cross(c.Sub(a))
		signX := x.Sub(a).Dot(normal)
		signD := d.Sub(a).Dot(normal)
		// x is outside this face when it sits on the other side from the
		// fourth vertex. A flat tetrahedron has no inside, so every face is
		// a candidate.
		flat := signD*signD <= float64(castTiny*normal.Len2())
		if !flat && float64(signX*signD) >= 0 {
			continue
		}
		tri := castSimplex{n: 3}
		tri.v[0], tri.v[1], tri.v[2] = s.v[face[0]], s.v[face[1]], s.v[face[2]]
		point := tri.reduceTriangle(x)
		if dist := point.Len2(); bestDist < 0 || dist < bestDist {
			best, bestDist, bestPoint = tri, dist, point
		}
	}
	if bestDist < 0 {
		return Vec3{}, true
	}
	*s = best
	return bestPoint, false
}
