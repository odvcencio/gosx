package camera

import (
	"math"
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene"
)

const dt60 = 1.0 / 60

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestSmoothDampStableAtAnyStep(t *testing.T) {
	for _, step := range []float64{0.0001, 1.0 / 240, 1.0 / 60, 1.0 / 15, 0.25, 1, 10, 1e6} {
		x, v := 0.0, 0.0
		prev := 0.0
		for i := 0; i < 400000 && x != 10; i++ {
			x, v = SmoothDamp(x, 10, v, 0.3, step)
			if math.IsNaN(x) || math.IsNaN(v) || x > 10 || x < prev-1e-12 {
				t.Fatalf("step %v: x=%v v=%v prev=%v (overshoot or reversal)", step, x, v, prev)
			}
			prev = x
		}
		if !near(x, 10, 1e-6) {
			t.Fatalf("step %v: did not converge, x=%v", step, x)
		}
	}
}

func TestSmoothDampFrameRateIndependent(t *testing.T) {
	run := func(step float64) float64 {
		x, v := 0.0, 0.0
		for t := 0.0; t < 0.5-1e-9; t += step {
			x, v = SmoothDamp(x, 1, v, 0.4, step)
		}
		return x
	}
	a, b := run(1.0/240), run(1.0/30)
	if !near(a, b, 0.02) {
		t.Fatalf("240 Hz %.4f vs 30 Hz %.4f", a, b)
	}
}

func TestSmoothDampSnapAndNoop(t *testing.T) {
	if x, v := SmoothDamp(3, 7, 2, 0, 0.1); x != 7 || v != 0 {
		t.Fatal(x, v)
	}
	if x, v := SmoothDamp(3, 7, 2, 1, 0); x != 3 || v != 2 {
		t.Fatal(x, v)
	}
	// Moving away from the target with velocity still converges.
	x, v := 0.0, -50.0
	for i := 0; i < 600; i++ {
		x, v = SmoothDamp(x, 1, v, 0.2, dt60)
	}
	if !near(x, 1, 1e-3) {
		t.Fatal(x)
	}
}

func TestFollowTrailsAndSettles(t *testing.T) {
	f := &Follow{Yaw: 0.5, Pitch: 0.4, Distance: 8, SmoothTime: 0.25, Offset: Vec3{0, 1, 0}}
	f.SetTarget(Vec3{0, 0, 0})
	p0 := f.Update(dt60)
	if !near(p0.Target.Y, 1, 1e-9) || !near(p0.Position.Sub(p0.Target).Len(), 8, 1e-9) {
		t.Fatalf("initial pose %+v", p0)
	}
	f.SetTarget(Vec3{10, 0, 0})
	p1 := f.Update(dt60)
	if p1.Target.X <= 0 || p1.Target.X >= 1 {
		t.Fatalf("camera should trail the subject, focus x=%v", p1.Target.X)
	}
	var p Pose
	for i := 0; i < 600; i++ {
		f.SetTarget(Vec3{10, 0, 0})
		p = f.Update(dt60)
		if p.Target.X > 10+1e-9 {
			t.Fatal("overshoot")
		}
	}
	if !near(p.Target.X, 10, 1e-3) || !near(p.Position.Sub(p.Target).Len(), 8, 1e-9) {
		t.Fatalf("settle %+v", p)
	}
	f.SetTarget(Vec3{-50, 0, 0})
	f.Snap()
	if p := f.Update(dt60); !near(p.Target.X, -50, 1e-9) {
		t.Fatal("snap")
	}
	f.SetTarget(Vec3{math.NaN(), 0, 0}) // ignored
	if p := f.Update(dt60); math.IsNaN(p.Target.X) {
		t.Fatal("NaN leaked")
	}
}

func TestFollowLookAheadAndDeadzone(t *testing.T) {
	f := &Follow{Distance: 5, SmoothTime: 0.01, LookAhead: 0.5}
	x := 0.0
	var p Pose
	for i := 0; i < 240; i++ {
		x += 6 * dt60 // 6 units per second
		f.SetTarget(Vec3{x, 0, 0})
		p = f.Update(dt60)
	}
	if lead := p.Target.X - x; !near(lead, 3, 0.4) {
		t.Fatalf("lead %v, want about 3", lead)
	}
	g := &Follow{Distance: 5, SmoothTime: 0.05, Deadzone: 1}
	g.SetTarget(Vec3{})
	g.Update(dt60)
	g.SetTarget(Vec3{0.8, 0, 0})
	for i := 0; i < 120; i++ {
		p = g.Update(dt60)
	}
	if p.Target.X != 0 {
		t.Fatalf("inside the deadzone the camera must not move: %v", p.Target.X)
	}
}

// project returns a point's normalized screen position (-1..1) for a pose.
func project(p Pose, aspect float64, w Vec3) (x, y, depth float64) {
	f := p.Forward()
	right := f.Cross(Vec3{0, 1, 0}).Norm()
	up := right.Cross(f)
	rel := w.Sub(p.Position)
	d := rel.Dot(f)
	t := math.Tan(p.FOV * math.Pi / 360)
	return rel.Dot(right) / (d * t * aspect), rel.Dot(up) / (d * t), d
}

func TestFrameFitsAllPointsWithMargin(t *testing.T) {
	pts := []Vec3{{-6, 0, 1}, {5, 2, -3}, {0, 0.5, 4}, {2, 6, 0}}
	for _, margin := range []float64{0, 0.1, 0.25} {
		f := &Frame{Yaw: 0.6, Pitch: 0.5, FOV: 50, Aspect: 1.6, Margin: margin}
		f.SetPoints(pts...)
		f.Snap()
		p := f.Update(dt60)
		limit := 1 - 2*margin
		var maxExt float64
		for _, w := range pts {
			x, y, d := project(p, 1.6, w)
			if d <= 0 {
				t.Fatalf("point behind camera")
			}
			maxExt = math.Max(maxExt, math.Max(math.Abs(x), math.Abs(y)))
		}
		if maxExt > limit+1e-9 || !near(maxExt, limit, 1e-6) {
			t.Errorf("margin %v: extent %.6f, want exactly %.6f (tight fit)", margin, maxExt, limit)
		}
	}
}

func TestFrameLimitsAndMoves(t *testing.T) {
	f := &Frame{Yaw: 0, Pitch: 0.3, FOV: 60, Aspect: 1.77, MinDistance: 12, MaxDistance: 20, SmoothTime: 0.2}
	f.SetPoints(Vec3{0, 0, 0})
	f.Snap()
	p := f.Update(dt60)
	if d := p.Position.Sub(p.Target).Len(); !near(d, 12, 1e-9) {
		t.Fatalf("min distance %v", d)
	}
	f.SetPoints(Vec3{-500, 0, 0}, Vec3{500, 0, 0})
	for i := 0; i < 600; i++ {
		p = f.Update(dt60)
	}
	if d := p.Position.Sub(p.Target).Len(); !near(d, 20, 1e-6) {
		t.Fatalf("max distance %v", d)
	}
	// A moving point pulls the framing smoothly.
	f.SetPoints(Vec3{0, 0, 0}, Vec3{3, 0, 0})
	before := p.Target.X
	p = f.Update(dt60)
	if p.Target.X == before || math.Abs(p.Target.X-before) > 5 {
		t.Fatalf("framing should ease: %v -> %v", before, p.Target.X)
	}
	// Straight-down view does not produce NaN.
	g := &Frame{Pitch: math.Pi / 2, FOV: 60}
	g.SetPoints(Vec3{-3, 0, -3}, Vec3{3, 0, 3})
	q := g.Update(dt60)
	if !q.Valid() {
		t.Fatalf("top-down pose invalid %+v", q)
	}
	// No points: still valid.
	h := &Frame{}
	if !h.Update(dt60).Valid() {
		t.Fatal("empty frame invalid")
	}
}

func TestPoseRotationMatchesRuntimeConvention(t *testing.T) {
	p := Pose{Position: Vec3{3, 4, 5}, Target: Vec3{-1, 1, -2}, FOV: 70}
	cam := p.Perspective(0.1, 100)
	ray, err := scene.CameraRay(scene.IRCamera{Kind: "perspective", X: cam.Position.X, Y: cam.Position.Y, Z: cam.Position.Z,
		RotationX: cam.Rotation.X, RotationY: cam.Rotation.Y, RotationZ: cam.Rotation.Z, FOV: cam.FOV}, 400, 300, 800, 600)
	if err != nil {
		t.Fatal(err)
	}
	f := p.Forward()
	if !near(ray.Direction.X, f.X, 1e-9) || !near(ray.Direction.Y, f.Y, 1e-9) || !near(ray.Direction.Z, f.Z, 1e-9) {
		t.Fatalf("center ray %+v vs forward %+v", ray.Direction, f)
	}
	if cam.TransitionMS != 0 || cam.Near != 0.1 || cam.FOV != 70 {
		t.Fatalf("%+v", cam)
	}
	if (Pose{}).Valid() || !p.Valid() {
		t.Fatal("Valid")
	}
}

func TestShakeTraumaGatingAndDeterminism(t *testing.T) {
	scale := 1.0
	run := func() (offs []Vec3) {
		s := NewShake(ShakeConfig{Seed: 7}, func() float64 { return scale })
		s.AddTrauma(0.6)
		s.AddTrauma(0.9) // clamps to 1
		if s.Trauma() != 1 {
			t.Fatal("trauma clamp", s.Trauma())
		}
		for i := 0; i < 120; i++ {
			o, _ := s.Update(dt60)
			offs = append(offs, o)
		}
		return
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("shake is not deterministic")
	}
	if a[0] == (Vec3{}) || a[len(a)-1] != (Vec3{}) {
		t.Fatalf("shake should start strong and end at rest: %v ... %v", a[0], a[len(a)-1])
	}
	mag := func(v Vec3) float64 { return v.Len() }
	var early, late float64
	for i := 0; i < 10; i++ {
		early = math.Max(early, mag(a[i]))
		late = math.Max(late, mag(a[60+i]))
	}
	if !(early > late) {
		t.Fatalf("shake should decay: %v vs %v", early, late)
	}
	// Reduced motion / slider at 0: no output and hits are discarded.
	scale = 0
	s := NewShake(ShakeConfig{}, func() float64 { return scale })
	s.AddTrauma(1)
	if o, r := s.Update(dt60); o != (Vec3{}) || r != 0 || s.Trauma() != 0 {
		t.Fatal("shake leaked while gated")
	}
	scale = 1
	if o, _ := s.Update(dt60); o != (Vec3{}) {
		t.Fatal("hit during gate must not replay")
	}
	// Half scale halves the amplitude.
	full := NewShake(ShakeConfig{Seed: 1}, nil)
	half := NewShake(ShakeConfig{Seed: 1}, func() float64 { return 0.5 })
	full.AddTrauma(0.8)
	half.AddTrauma(0.8)
	of, _ := full.Update(dt60)
	oh, _ := half.Update(dt60)
	if !near(of.X*0.5, oh.X, 1e-12) {
		t.Fatalf("scale: %v %v", of.X, oh.X)
	}
	// Non-finite input is ignored.
	full.Clear()
	full.AddTrauma(math.NaN())
	if full.Trauma() != 0 {
		t.Fatal("NaN trauma")
	}
}

func staticAt(x float64) Static {
	return Static{Position: Vec3{x, 0, 10}, Target: Vec3{x, 0, 0}, FOV: 60}
}

func TestDirectorHandoffHasNoPop(t *testing.T) {
	d := NewDirector(DirectorConfig{}, staticAt(0))
	d.Update(dt60)
	d.Take(staticAt(100), Blend{Duration: 1})
	prev := d.Pose().Position.X
	var maxStep float64
	for i := 0; i < 90; i++ {
		x := d.Update(dt60).Position.X
		maxStep = math.Max(maxStep, math.Abs(x-prev))
		if x < prev-1e-12 {
			t.Fatal("blend went backwards")
		}
		prev = x
	}
	if !near(prev, 100, 1e-9) {
		t.Fatalf("blend end %v", prev)
	}
	if maxStep > 6 { // smoothstep peak speed is 1.5*100 per second
		t.Fatalf("pop of %v in one frame", maxStep)
	}
	// Handing back blends from the pose on screen to the live base.
	d.Release(Blend{Duration: 0.5})
	first := d.Update(dt60).Position.X
	if first < 99 {
		t.Fatalf("release popped to %v", first)
	}
	for i := 0; i < 40; i++ {
		d.Update(dt60)
	}
	if !near(d.Pose().Position.X, 0, 1e-9) || d.Busy() {
		t.Fatalf("not back at base: %v busy=%v", d.Pose().Position.X, d.Busy())
	}
}

func TestDirectorInterruptedBlendStartsFromScreen(t *testing.T) {
	d := NewDirector(DirectorConfig{}, staticAt(0))
	d.Update(dt60)
	d.Take(staticAt(100), Blend{Duration: 1})
	for i := 0; i < 30; i++ {
		d.Update(dt60)
	}
	mid := d.Pose().Position.X
	d.Release(Blend{Duration: 1})
	next := d.Update(dt60).Position.X
	if math.Abs(next-mid) > 5 {
		t.Fatalf("interrupted blend jumped from %v to %v", mid, next)
	}
}

func TestSequenceTimingEventsAndReturn(t *testing.T) {
	var events []Event
	d := NewDirector(DirectorConfig{OnEvent: func(e Event) { events = append(events, e) }}, staticAt(0))
	d.Bookmark("a", Pose{Position: Vec3{50, 0, 10}, Target: Vec3{50, 0, 0}})
	d.Bookmark("b", Pose{Position: Vec3{80, 0, 10}, Target: Vec3{80, 0, 0}})
	d.Update(dt60)
	ok := d.Play(Sequence{Name: "intro", Steps: []Step{
		{Shot: "a", Blend: Blend{Duration: 1}, Hold: 2},
		{Shot: "b", Blend: Blend{Duration: 0.5}, Hold: 1},
	}, Return: Blend{Duration: 1}})
	if !ok || d.Playing() != "intro" || !d.Busy() {
		t.Fatal("play")
	}
	stepAt := map[int]float64{}
	elapsed := 0.0
	for elapsed < 8 {
		d.Update(dt60)
		elapsed += dt60
		for _, e := range events {
			if e.Kind == EventSequenceStep {
				if _, seen := stepAt[e.Index]; !seen {
					stepAt[e.Index] = elapsed
				}
			}
		}
	}
	// Step 1 starts after 1 s blend + 2 s hold (frame rounding allowed).
	if !near(stepAt[1], 3.0, 0.05) {
		t.Fatalf("step 1 began at %v, want 3.0 (%v)", stepAt[1], stepAt)
	}
	if d.Playing() != "" || d.Busy() || !near(d.Pose().Position.X, 0, 1e-9) {
		t.Fatalf("did not return to base: playing=%q busy=%v x=%v", d.Playing(), d.Busy(), d.Pose().Position.X)
	}
	last := events[len(events)-1]
	hasEnd := false
	for _, e := range events {
		if e.Kind == EventSequenceEnd && e.Sequence == "intro" {
			hasEnd = true
		}
	}
	if !hasEnd || last.Kind != EventHandoffDone {
		t.Fatalf("events %+v", events)
	}
}

func TestSequenceInterruptAndLocked(t *testing.T) {
	var events []Event
	d := NewDirector(DirectorConfig{OnEvent: func(e Event) { events = append(events, e) }}, staticAt(0))
	d.Bookmark("a", Pose{Position: Vec3{50, 0, 10}, Target: Vec3{50, 0, 0}})
	d.Update(dt60)
	d.Play(Sequence{Name: "tour", Loop: true, Steps: []Step{{Shot: "a", Hold: 100}}, Return: Blend{Duration: 0.5}})
	for i := 0; i < 30; i++ {
		d.Update(dt60)
	}
	if !d.Interrupt() || d.Playing() != "" {
		t.Fatal("interrupt")
	}
	if events[len(events)-1].Kind != EventSequenceInterrupted {
		t.Fatalf("%+v", events)
	}
	for i := 0; i < 60; i++ {
		d.Update(dt60)
	}
	if d.Busy() {
		t.Fatal("should be back at base")
	}
	if d.Interrupt() {
		t.Fatal("nothing to interrupt")
	}
	d.Play(Sequence{Name: "cutscene", Locked: true, Steps: []Step{{Shot: "a", Hold: 100}}})
	if d.Interrupt() || d.Playing() != "cutscene" {
		t.Fatal("locked sequence was interrupted")
	}
	d.Stop()
	if d.Playing() != "" {
		t.Fatal("stop")
	}
}

func TestSequenceUntilAndPathSource(t *testing.T) {
	path := &Path{Poses: []Pose{
		{Position: Vec3{0, 0, 10}, Target: Vec3{}},
		{Position: Vec3{10, 0, 10}, Target: Vec3{10, 0, 0}},
		{Position: Vec3{20, 5, 10}, Target: Vec3{20, 0, 0}},
	}, Duration: 2}
	d := NewDirector(DirectorConfig{}, staticAt(0))
	d.Update(dt60)
	d.Play(Sequence{Name: "fly", Steps: []Step{{Source: path, Until: path.Done}}})
	for i := 0; i < 60; i++ {
		d.Update(dt60)
	}
	if d.Playing() != "fly" {
		t.Fatal("ended before path finished")
	}
	if x := d.Pose().Position.X; x < 5 || x > 15 {
		t.Fatalf("path midpoint x=%v", x)
	}
	for i := 0; i < 90; i++ {
		d.Update(dt60)
	}
	if d.Playing() != "" {
		t.Fatal("path done, sequence should end")
	}
	if !near(path.Update(0).Position.X, 20, 1e-9) {
		t.Fatal("path end pose")
	}
}

func TestReducedMotion(t *testing.T) {
	reduced := true
	d := NewDirector(DirectorConfig{ReducedMotion: func() bool { return reduced }}, staticAt(0))
	d.Bookmark("a", Pose{Position: Vec3{50, 0, 10}, Target: Vec3{50, 0, 0}})
	d.Update(dt60)
	// Handoffs cut.
	d.Take(staticAt(100), Blend{Duration: 2})
	if x := d.Update(dt60).Position.X; x != 100 {
		t.Fatalf("reduced motion should cut, got %v", x)
	}
	d.Release(Blend{})
	d.Update(dt60)
	// Skip policy.
	if d.Play(Sequence{Name: "intro", Steps: []Step{{Shot: "a", Hold: 5}}}) {
		t.Fatal("ReducedSkip should skip")
	}
	if d.Busy() {
		t.Fatal("busy after skip")
	}
	// Cuts policy keeps holds but never blends.
	if !d.Play(Sequence{Name: "story", Reduced: ReducedCuts, Steps: []Step{{Shot: "a", Blend: Blend{Duration: 3}, Hold: 1}}}) {
		t.Fatal("ReducedCuts should play")
	}
	if x := d.Update(dt60).Position.X; x != 50 {
		t.Fatalf("cut to shot, got %v", x)
	}
	// Motion turning reduced mid-sequence skips it.
	reduced = false
	d.Stop()
	d.Update(dt60)
	d.Play(Sequence{Name: "intro", Steps: []Step{{Shot: "a", Hold: 5}}})
	reduced = true
	d.Update(dt60)
	if d.Playing() != "" {
		t.Fatal("sequence should stop when motion becomes reduced")
	}
}

func TestDirectorShakeGatedByReducedMotion(t *testing.T) {
	reduced := false
	d := NewDirector(DirectorConfig{
		ReducedMotion: func() bool { return reduced },
		ShakeScale: func() float64 {
			if reduced {
				return 0
			}
			return 1
		},
	}, staticAt(0))
	base := d.Update(dt60)
	d.AddTrauma(1)
	shaken := d.Update(dt60)
	if shaken == base {
		t.Fatal("no shake")
	}
	if !near(shaken.Position.Sub(shaken.Target).Len(), 10, 1e-9) {
		t.Fatal("shake must translate position and target together")
	}
	reduced = true
	if got := d.Update(dt60); got != base {
		t.Fatalf("shake under reduced motion: %+v", got)
	}
}

func TestDirectorDeterministic(t *testing.T) {
	run := func() []Pose {
		d := NewDirector(DirectorConfig{Shake: ShakeConfig{Seed: 3}}, &Follow{Distance: 6, SmoothTime: 0.2})
		d.Bookmark("a", Pose{Position: Vec3{5, 5, 5}, Target: Vec3{}})
		var out []Pose
		for i := 0; i < 300; i++ {
			switch i {
			case 20:
				d.Play(Sequence{Name: "s", Steps: []Step{{Shot: "a", Blend: Blend{Duration: 1}, Hold: 1}}, Return: Blend{Duration: 1}})
			case 100:
				d.AddTrauma(0.7)
			}
			out = append(out, d.Update(dt60))
		}
		return out
	}
	if !reflect.DeepEqual(run(), run()) {
		t.Fatal("runs differ")
	}
}

func TestUpdateDoesNotAllocate(t *testing.T) {
	f := &Follow{Distance: 6, SmoothTime: 0.2}
	d := NewDirector(DirectorConfig{}, f)
	d.Update(dt60)
	d.AddTrauma(0.5)
	n := testing.AllocsPerRun(200, func() {
		f.SetTarget(Vec3{1, 2, 3})
		d.Update(dt60)
	})
	if n != 0 {
		t.Fatalf("allocs per frame = %v", n)
	}
}
