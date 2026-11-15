package teamai

import (
	"strings"
	"testing"
)

const testDT = 1.0 / 30

type stubEnv struct {
	landmarks []Vec2
	choose    func(d *Decision, waited uint32) int
	marks     []int16
}

func (e *stubEnv) landmarkPos(id LandmarkID) Vec2 { return e.landmarks[id] }
func (e *stubEnv) decide(d *Decision, waited uint32) int {
	if e.choose != nil {
		if i := e.choose(d, waited); i >= 0 {
			return i
		}
	}
	if elapsed(waited, testDT) >= d.window {
		return decideExpired
	}
	return decideWait
}
func (e *stubEnv) marked(m int16) { e.marks = append(e.marks, m) }

type pathRun struct {
	trace []Vec2
	vels  []Vec2
	done  int // tick the path reported done, or -1
}

// runPath starts p at the body's position and integrates the commanded
// velocity for up to ticks ticks.
func runPath(p *Path, b *Body, fr *Frame, sign float64, env *stubEnv, ticks int) pathRun {
	var st pathState
	st.beginPath(b.Pos, fr, sign)
	r := pathRun{done: -1}
	for i := 0; i < ticks; i++ {
		out := p.step(&st, b, env, testDT, 0.25)
		st.ticks++
		st.segTicks++
		if out.done {
			r.done = i
			break
		}
		b.Vel = out.vel
		b.Pos = b.Pos.AddScaled(out.vel, testDT)
		r.trace = append(r.trace, b.Pos)
		r.vels = append(r.vels, out.vel)
	}
	return r
}

func pathBook(t testing.TB, segs ...SegmentSpec) (*Book, *Path) {
	t.Helper()
	s := drillSpec()
	s.Landmarks = []LandmarkSpec{{Name: "flag", Host: true}}
	s.Paths = []PathSpec{{Name: "p", Segments: segs}}
	reg := NewRegistry()
	never := PredicateFunc(func(*Query) bool { return false })
	_ = reg.Predicate("open", never)
	_ = reg.Predicate("closed", never)
	b, err := Compile(s, reg)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return b, b.Path(0)
}

func vp(x, y float64) *Vec2 { return &Vec2{X: x, Y: y} }
func fp(v float64) *float64 { return &v }

func TestPathLineReachesEndOnTime(t *testing.T) {
	_, p := pathBook(t, SegmentSpec{Kind: "line", To: vp(0, 10)})
	b := &Body{MaxSpeed: 5}
	r := runPath(p, b, &Frame{}, 1, &stubEnv{}, 200)
	// 10 units at 5 units/s is 60 ticks; the path completes within the
	// 0.25 arrival radius, a tick or two early.
	if r.done < 56 || r.done > 61 {
		t.Fatalf("done at tick %d, want about 60", r.done)
	}
	if end := r.trace[len(r.trace)-1]; end.Dist(V2(0, 10)) > 0.3 {
		t.Fatalf("ended at %v, want near (0,10)", end)
	}
}

func TestPathMirrorsAndRotates(t *testing.T) {
	_, p := pathBook(t, SegmentSpec{Kind: "line", To: vp(3, 4)})
	cases := []struct {
		name  string
		frame Frame
		sign  float64
		want  Vec2
	}{
		{"as authored", Frame{}, 1, V2(3, 4)},
		{"mirrored by the slot side", Frame{}, -1, V2(-3, 4)},
		{"frame facing +X", Frame{Forward: V2(1, 0)}, 1, V2(4, -3)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &Body{MaxSpeed: 6}
			r := runPath(p, b, &c.frame, c.sign, &stubEnv{}, 200)
			if r.done < 0 {
				t.Fatal("path did not finish")
			}
			if end := r.trace[len(r.trace)-1]; end.Dist(c.want) > 0.3 {
				t.Fatalf("ended at %v, want near %v", end, c.want)
			}
		})
	}
}

func TestPathToAndBy(t *testing.T) {
	_, p := pathBook(t,
		SegmentSpec{Kind: "line", To: vp(0, 5)},
		SegmentSpec{Kind: "line", By: vp(4, 0)},
		SegmentSpec{Kind: "line", To: vp(0, 0)},
	)
	b := &Body{Pos: V2(10, 10), MaxSpeed: 6}
	var st pathState
	st.beginPath(b.Pos, &Frame{}, 1)
	env := &stubEnv{}
	var corners []Vec2
	for i := 0; i < 400; i++ {
		seg := st.seg
		out := p.step(&st, b, env, testDT, 0.25)
		if st.seg != seg {
			corners = append(corners, st.start)
		}
		if out.done {
			break
		}
		st.ticks++
		st.segTicks++
		b.Pos = b.Pos.AddScaled(out.vel, testDT)
	}
	want := []Vec2{V2(10, 15), V2(14, 15), V2(10, 10)}
	if len(corners) != len(want) {
		t.Fatalf("corners %v, want %v", corners, want)
	}
	for i := range want {
		if !near(corners[i], want[i]) {
			t.Fatalf("corner %d = %v, want %v (planned points do not drift)", i, corners[i], want[i])
		}
	}
}

func TestPathCurveFollowsArc(t *testing.T) {
	_, p := pathBook(t, SegmentSpec{Kind: "curve", By: vp(5, 5), Control: vp(0, 5)})
	seg := &p.segs[0]
	length := seg.lens[curveSamples]
	// B(t) = (5t^2, 10t - 5t^2), so the length is the integral over [0,1] of
	// 10*sqrt(2t^2 - 2t + 1), which is 8.1163. Chords run a little short.
	if length < 8.10 || length > 8.1163 {
		t.Fatalf("arc length %v, want just under 8.1163", length)
	}
	b := &Body{MaxSpeed: 4}
	r := runPath(p, b, &Frame{}, 1, &stubEnv{}, 400)
	if r.done < 0 {
		t.Fatal("curve did not finish")
	}
	ticks := float64(r.done)
	if want := length / 4 / testDT; ticks < want-6 || ticks > want+3 {
		t.Fatalf("curve took %v ticks, want about %v", ticks, want)
	}
	var st pathState
	st.beginPath(V2(0, 0), &Frame{}, 1)
	for i, pt := range r.trace {
		at := p.projectCurve(seg, &st, pt)
		on := curveAt(seg, at)
		if d := on.Dist(pt); d > 0.35 {
			t.Fatalf("tick %d: %v is %v from the curve", i, pt, d)
		}
	}
}

func TestPathHoldAndNotBefore(t *testing.T) {
	_, p := pathBook(t,
		SegmentSpec{Kind: "hold", Duration: 1},
		SegmentSpec{Kind: "line", By: vp(0, 3), NotBefore: 1.5},
	)
	b := &Body{MaxSpeed: 5}
	r := runPath(p, b, &Frame{}, 1, &stubEnv{}, 200)
	// Hold for 1 s (30 ticks), then the line waits until 1.5 s (45 ticks).
	for i := 0; i < 45; i++ {
		if !r.vels[i].IsZero() {
			t.Fatalf("tick %d moved with %v before 1.5 s", i, r.vels[i])
		}
	}
	if r.vels[45].IsZero() {
		t.Fatal("tick 45 did not start the line")
	}
}

func TestPathSpeedProfileAndDuration(t *testing.T) {
	_, p := pathBook(t, SegmentSpec{Kind: "line", To: vp(0, 20), Speed: 0.2, EndSpeed: fp(1)})
	b := &Body{MaxSpeed: 10}
	r := runPath(p, b, &Frame{}, 1, &stubEnv{}, 400)
	first, last := r.vels[0].Len(), r.vels[len(r.vels)-1].Len()
	if first < 1.9 || first > 2.2 {
		t.Fatalf("start speed %v, want about 2 (0.2 of 10)", first)
	}
	if last < 9.5 {
		t.Fatalf("end speed %v, want near 10", last)
	}

	_, p = pathBook(t, SegmentSpec{Kind: "line", To: vp(0, 10), Duration: 4})
	b = &Body{MaxSpeed: 10}
	r = runPath(p, b, &Frame{}, 1, &stubEnv{}, 400)
	if v := r.vels[10].Len(); v < 2.49 || v > 2.51 {
		t.Fatalf("speed %v, want 2.5 (10 units in 4 s)", v)
	}
}

func TestPathMarksAndDecisions(t *testing.T) {
	b, p := pathBook(t,
		SegmentSpec{Kind: "line", To: vp(0, 4), Mark: "stem"},
		SegmentSpec{Kind: "decide", DecisionSpec: DecisionSpec{
			Window:   0.5,
			Branches: []BranchSpec{{When: "open", Goto: "out"}, {When: "closed", Goto: "in"}},
		}},
		SegmentSpec{Label: "straight", Kind: "line", By: vp(0, 6), Mark: "deep", Next: "end"},
		SegmentSpec{Label: "out", Kind: "line", By: vp(5, 0), Mark: "out", Next: "end"},
		SegmentSpec{Label: "in", Kind: "line", By: vp(-5, 0), Mark: "in", Next: "end"},
		SegmentSpec{Label: "end", Kind: "hold", Duration: 0.1},
	)
	stem, _ := b.Mark("stem")
	out, _ := b.Mark("out")
	deep, _ := b.Mark("deep")

	// A branch that fires at once.
	env := &stubEnv{choose: func(d *Decision, waited uint32) int { return 0 }}
	r := runPath(p, &Body{MaxSpeed: 5}, &Frame{}, 1, env, 300)
	if r.done < 0 || len(env.marks) != 2 || env.marks[0] != int16(stem) || env.marks[1] != int16(out) {
		t.Fatalf("marks %v (done %d), want stem then out", env.marks, r.done)
	}

	// No branch fires: the agent coasts through the 0.5 s window, then takes
	// the next segment.
	var waits []uint32
	env = &stubEnv{choose: func(d *Decision, waited uint32) int { waits = append(waits, waited); return -1 }}
	r = runPath(p, &Body{MaxSpeed: 5}, &Frame{}, 1, env, 300)
	if len(env.marks) != 2 || env.marks[1] != int16(deep) {
		t.Fatalf("marks %v, want stem then deep", env.marks)
	}
	if len(waits) < 14 || len(waits) > 16 {
		t.Fatalf("decision tested %d times, want about 15 (0.5 s at 30 Hz)", len(waits))
	}
	for i, w := range waits {
		if w != uint32(i) {
			t.Fatalf("waited %v, want 0, 1, 2, ...", waits)
		}
	}
}

func TestPathLandmarkTrack(t *testing.T) {
	_, p := pathBook(t, SegmentSpec{Kind: "line", Landmark: "flag", Offset: V2(0, -1), Track: true})
	env := &stubEnv{landmarks: []Vec2{V2(0, 10)}}
	b := &Body{MaxSpeed: 5}
	var st pathState
	st.beginPath(b.Pos, &Frame{}, 1)
	done := false
	for i := 0; i < 400 && !done; i++ {
		env.landmarks[0] = env.landmarks[0].Add(V2(0.05, 0)) // the flag drifts +X
		out := p.step(&st, b, env, testDT, 0.25)
		done = out.done
		st.ticks++
		st.segTicks++
		b.Pos = b.Pos.AddScaled(out.vel, testDT)
	}
	if !done {
		t.Fatal("did not reach the moving landmark")
	}
	if d := b.Pos.Dist(env.landmarks[0].Add(V2(0, -1))); d > 0.5 {
		t.Fatalf("ended %v from the moving landmark", d)
	}
}

func TestPathDecisionLoopIsBounded(t *testing.T) {
	_, p := pathBook(t,
		SegmentSpec{Label: "spin", Kind: "decide", DecisionSpec: DecisionSpec{
			Branches: []BranchSpec{{Goto: "spin"}},
		}},
	)
	env := &stubEnv{choose: func(d *Decision, waited uint32) int { return 0 }}
	var st pathState
	st.beginPath(V2(0, 0), &Frame{}, 1)
	out := p.step(&st, &Body{MaxSpeed: 5}, env, testDT, 0.25)
	if out.done {
		t.Fatal("a decision loop ended the path")
	}
}

func TestPathValidation(t *testing.T) {
	cases := []struct {
		name string
		seg  SegmentSpec
		want string
	}{
		{"line needs one target", SegmentSpec{Kind: "line", To: vp(1, 1), By: vp(1, 1)},
			"paths[0](p).segments[0]: a line needs exactly one of to, by or landmark"},
		{"curve needs control", SegmentSpec{Kind: "curve", By: vp(1, 1)},
			"a curve needs by and control"},
		{"hold needs duration", SegmentSpec{Kind: "hold"},
			"paths[0](p).segments[0].duration: a hold needs a duration"},
		{"unknown kind", SegmentSpec{Kind: "zigzag"},
			`paths[0](p).segments[0].kind: kind "zigzag"`},
		{"missing kind", SegmentSpec{},
			"paths[0](p).segments[0].kind: missing kind"},
		{"unknown landmark", SegmentSpec{Kind: "line", Landmark: "moon"},
			`paths[0](p).segments[0].landmark: unknown landmark "moon"`},
		{"unknown next", SegmentSpec{Kind: "line", To: vp(1, 1), Next: "nowhere"},
			`paths[0](p).segments[0].next: unknown label "nowhere"`},
		{"unknown goto", SegmentSpec{Kind: "decide", DecisionSpec: DecisionSpec{Branches: []BranchSpec{{Goto: "x"}}}},
			`paths[0](p).segments[0].branches[0](x).goto: unknown label "x"`},
		{"bad mode", SegmentSpec{Kind: "decide", DecisionSpec: DecisionSpec{Mode: "random", Branches: []BranchSpec{{Goto: "a"}}}, Label: "a"},
			`paths[0](p).segments[0](a).mode: mode "random"`},
		{"speed out of range", SegmentSpec{Kind: "line", To: vp(1, 1), Speed: 3},
			"paths[0](p).segments[0].speed: 3: want a fraction"},
		{"branches on a line", SegmentSpec{Kind: "line", To: vp(1, 1), DecisionSpec: DecisionSpec{Window: 1}},
			"apply to a decide segment only"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := drillSpec()
			s.Landmarks = []LandmarkSpec{{Name: "flag", Host: true}}
			s.Paths = []PathSpec{{Name: "p", Segments: []SegmentSpec{c.seg}}}
			_, err := Compile(s, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestPathTooManyMarks(t *testing.T) {
	s := drillSpec()
	var segs []SegmentSpec
	for i := 0; i < maxMarks+1; i++ {
		segs = append(segs, SegmentSpec{Kind: "hold", Duration: 1, Mark: "m" + itoa(i)})
	}
	s.Paths = []PathSpec{{Name: "p", Segments: segs}}
	_, err := Compile(s, nil)
	if err == nil || !strings.Contains(err.Error(), "paths[0](p).segments[32].mark: more than 32 distinct marks") {
		t.Fatalf("error %v", err)
	}
}
