package teamai

// PathID indexes Book paths. NoPath is -1.
type PathID int16

// NoPath marks the absence of a path.
const NoPath PathID = -1

// SegmentKind is the kind of a path segment.
type SegmentKind uint8

const (
	// SegmentLine runs straight to a point or landmark.
	SegmentLine SegmentKind = iota + 1
	// SegmentCurve runs a quadratic Bezier curve.
	SegmentCurve
	// SegmentHold stands at the segment start for a duration.
	SegmentHold
	// SegmentDecide picks the next segment by rule.
	SegmentDecide
)

func (k SegmentKind) String() string {
	switch k {
	case SegmentLine:
		return "line"
	case SegmentCurve:
		return "curve"
	case SegmentHold:
		return "hold"
	case SegmentDecide:
		return "decide"
	}
	return "unknown"
}

// curveSamples is the number of chords a curve is split into for arc length,
// projection and progression.
const curveSamples = 16

// maxSegments bounds the segments of one path and the steps of one assignment.
const maxSegments = 64

// maxTransitions bounds the zero-time transitions (decisions, completed
// segments, jumps) one agent takes in one tick, so a loop of decisions in the
// data cannot hang a tick.
const maxTransitions = 8

// Path is a compiled sequence of timed segments. Points are local to the
// agent's frame when the path starts, with lateral offsets signed by the
// agent's slot side, so a path mirrors with its slot.
type Path struct {
	name string
	segs []segment
}

type segment struct {
	kind  SegmentKind
	label string
	// line: to (from the path origin) or by (from the segment start), or a
	// landmark plus offset. curve: by and control, from the segment start.
	to       Vec2
	relative bool
	landmark LandmarkID
	track    bool
	control  Vec2
	// curve chords in local axes from the segment start, unsigned:
	// poly[0] is (0,0); lens[i] is the arc length to poly[i].
	poly []Vec2
	lens []float64

	speed     float64 // fraction of MaxSpeed at the start
	endSpeed  float64 // fraction at the end
	duration  float64 // seconds: hold time, or time to run the segment
	notBefore float64 // seconds since the path started
	mark      int16   // mark set on completion, or -1
	next      int16   // segment after this one; -1 ends the path
	decision  Decision
}

// Name returns the path's name.
func (p *Path) Name() string { return p.name }

// NumSegments returns the number of segments.
func (p *Path) NumSegments() int { return len(p.segs) }

// SegmentKind returns the kind of segment i.
func (p *Path) SegmentKind(i int) SegmentKind { return p.segs[i].kind }

// SegmentLabel returns the label of segment i, or "".
func (p *Path) SegmentLabel(i int) string { return p.segs[i].label }

// pathState is the progress of one agent along a path. It is part of the
// executor's snapshot.
type pathState struct {
	seg      int16
	begun    bool
	ticks    uint32 // ticks since the path started
	segTicks uint32 // ticks since the segment started
	origin   Vec2   // agent position when the path started
	fwd      Vec2   // frame forward when the path started
	sign     float64
	start    Vec2 // planned segment start
	end      Vec2 // planned segment end (line)
	progress float64
	coast    Vec2 // last commanded velocity, kept through decisions
}

// pathEnv is what a running path needs from the executor.
type pathEnv interface {
	landmarkPos(id LandmarkID) Vec2
	// decide returns the branch to take at d, decideWait to keep waiting,
	// or decideExpired when the window is over and d takes its default.
	// waited is the number of ticks already spent at d.
	decide(d *Decision, waited uint32) int
	marked(mark int16)
}

// pathOutcome is one tick of path progress.
type pathOutcome struct {
	vel  Vec2
	done bool
}

func (st *pathState) beginPath(origin Vec2, fr *Frame, sign float64) {
	*st = pathState{origin: origin, fwd: fr.Fwd(), sign: sign, start: origin}
}

// local converts a local path offset to a world offset.
func (st *pathState) local(v Vec2) Vec2 {
	x := float64(v.X * st.sign)
	right := Vec2{X: st.fwd.Y, Y: -st.fwd.X}
	return Vec2{
		X: float64(right.X*x) + float64(st.fwd.X*v.Y),
		Y: float64(right.Y*x) + float64(st.fwd.Y*v.Y),
	}
}

func (st *pathState) enter(seg int16) {
	st.seg = seg
	st.begun = false
	st.segTicks = 0
	st.progress = 0
}

// endOf returns the planned end of a line segment.
func (p *Path) endOf(s *segment, st *pathState, env pathEnv) Vec2 {
	switch {
	case s.landmark >= 0:
		return env.landmarkPos(s.landmark).Add(st.local(s.to))
	case s.relative:
		return st.start.Add(st.local(s.to))
	default:
		return st.origin.Add(st.local(s.to))
	}
}

// step advances one agent along p by one tick. b is the agent's body, dt the
// fixed step in seconds and tol the arrival radius.
func (p *Path) step(st *pathState, b *Body, env pathEnv, dt, tol float64) pathOutcome {
	for n := 0; n < maxTransitions; n++ {
		if st.seg < 0 || int(st.seg) >= len(p.segs) {
			return pathOutcome{done: true}
		}
		s := &p.segs[st.seg]
		if !st.begun {
			if s.notBefore > 0 && elapsed(st.ticks, dt) < s.notBefore {
				return pathOutcome{vel: Arrive(b.Pos, st.start, b.MaxSpeed, b.MaxAccel, tol)}
			}
			st.begun = true
			st.segTicks = 0
			if s.kind == SegmentLine {
				st.end = p.endOf(s, st, env)
			}
		}
		switch s.kind {
		case SegmentHold:
			if elapsed(st.segTicks, dt) >= s.duration {
				p.complete(st, s, env, st.start)
				continue
			}
			return pathOutcome{vel: Arrive(b.Pos, st.start, b.MaxSpeed, b.MaxAccel, tol)}

		case SegmentDecide:
			switch i := env.decide(&s.decision, st.segTicks); {
			case i >= 0:
				st.enter(s.decision.branches[i].target)
				continue
			case i == decideExpired:
				st.enter(s.decision.otherwise)
				continue
			}
			return pathOutcome{vel: st.coast}

		case SegmentLine:
			if s.track && s.landmark >= 0 {
				st.end = p.endOf(s, st, env)
			}
			d := st.end.Sub(st.start)
			length := d.Len()
			var proj float64
			if length > epsilon {
				proj = b.Pos.Sub(st.start).Dot(d) / length
			}
			if length-proj <= tol || b.Pos.Dist(st.end) <= tol {
				p.complete(st, s, env, st.end)
				continue
			}
			st.progress = clamp(proj, 0, length)
			v := segSpeed(s, b, st.progress, length)
			look := lookAhead(v, dt, tol)
			carrot := Lerp(st.start, st.end, clamp((st.progress+look)/length, 0, 1))
			vel := Seek(b.Pos, carrot, v)
			st.coast = vel
			return pathOutcome{vel: vel}

		case SegmentCurve:
			length := s.lens[curveSamples]
			proj := p.projectCurve(s, st, b.Pos)
			endW := st.start.Add(st.local(s.poly[curveSamples]))
			if length-proj <= tol || b.Pos.Dist(endW) <= tol {
				p.complete(st, s, env, endW)
				continue
			}
			st.progress = proj
			v := segSpeed(s, b, proj, length)
			look := lookAhead(v, dt, tol)
			carrot := st.start.Add(st.local(curveAt(s, proj+look)))
			vel := Seek(b.Pos, carrot, v)
			st.coast = vel
			return pathOutcome{vel: vel}
		}
		return pathOutcome{done: true}
	}
	return pathOutcome{vel: st.coast}
}

// complete finishes segment s at the planned point end and moves on.
func (p *Path) complete(st *pathState, s *segment, env pathEnv, end Vec2) {
	if s.mark >= 0 {
		env.marked(s.mark)
	}
	st.start = end
	st.enter(s.next)
}

// lookAhead returns how far ahead of the agent's projection the path's
// steering target sits: three ticks of travel, at least tol.
func lookAhead(v, dt, tol float64) float64 {
	look := float64(float64(v*dt) * 3)
	if look < tol {
		return tol
	}
	return look
}

// seconds converts ticks to seconds.
func seconds(ticks uint32, dt float64) float64 { return float64(float64(ticks) * dt) }

// elapsed converts ticks to seconds for comparison with a duration, with a
// guard against 1/30-style steps landing a hair short of a whole second.
func elapsed(ticks uint32, dt float64) float64 {
	t := float64(float64(ticks) * dt)
	return t + float64(dt*1e-6)
}

// segSpeed returns the speed profile of s at distance at of length, in units
// per second.
func segSpeed(s *segment, b *Body, at, length float64) float64 {
	if s.duration > 0 {
		v := length / s.duration
		if v > b.MaxSpeed {
			return b.MaxSpeed
		}
		return v
	}
	f := s.speed
	if length > epsilon {
		f = s.speed + float64((s.endSpeed-s.speed)*clamp(at/length, 0, 1))
	}
	const floor = 0.05 // keep creeping toward a point the profile slows to zero at
	if f < floor {
		f = floor
	}
	return float64(f * b.MaxSpeed)
}

// projectCurve returns the arc length of the point on curve s closest to pos.
func (p *Path) projectCurve(s *segment, st *pathState, pos Vec2) float64 {
	best := 0.0
	bestD := farAway
	for i := 0; i < curveSamples; i++ {
		a := st.start.Add(st.local(s.poly[i]))
		c := st.start.Add(st.local(s.poly[i+1]))
		d := c.Sub(a)
		l2 := d.Len2()
		t := 0.0
		if l2 > epsilon {
			t = clamp(pos.Sub(a).Dot(d)/l2, 0, 1)
		}
		q := a.AddScaled(d, t)
		if dd := pos.Sub(q).Len2(); dd < bestD {
			bestD = dd
			best = s.lens[i] + float64((s.lens[i+1]-s.lens[i])*t)
		}
	}
	return best
}

// farAway is larger than any squared distance in a game world.
const farAway = 1e308

// curveAt returns the local point at arc length at along curve s.
func curveAt(s *segment, at float64) Vec2 {
	if at <= 0 {
		return s.poly[0]
	}
	if at >= s.lens[curveSamples] {
		return s.poly[curveSamples]
	}
	for i := 0; i < curveSamples; i++ {
		if at <= s.lens[i+1] {
			span := s.lens[i+1] - s.lens[i]
			t := 0.0
			if span > epsilon {
				t = (at - s.lens[i]) / span
			}
			return Lerp(s.poly[i], s.poly[i+1], t)
		}
	}
	return s.poly[curveSamples]
}

// bezierPoly samples the quadratic Bezier (0,0) -> control -> end.
func bezierPoly(control, end Vec2) ([]Vec2, []float64) {
	poly := make([]Vec2, curveSamples+1)
	lens := make([]float64, curveSamples+1)
	for i := 0; i <= curveSamples; i++ {
		t := float64(i) / curveSamples
		u := 1 - t
		// B(t) = 2ut*control + t^2*end (the start is the origin).
		a := float64(2 * float64(u*t))
		c := float64(t * t)
		poly[i] = Vec2{
			X: float64(a*control.X) + float64(c*end.X),
			Y: float64(a*control.Y) + float64(c*end.Y),
		}
		if i > 0 {
			lens[i] = lens[i-1] + poly[i].Dist(poly[i-1])
		}
	}
	return poly, lens
}
