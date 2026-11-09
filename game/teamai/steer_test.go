package teamai

import (
	"math"
	"testing"
)

func closeTo(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestSeekFleeArrive(t *testing.T) {
	if v := Seek(V2(0, 0), V2(3, 4), 10); !near(v, V2(6, 8)) {
		t.Errorf("Seek = %v, want (6,8)", v)
	}
	if v := Flee(V2(0, 0), V2(3, 4), 5); !near(v, V2(-3, -4)) {
		t.Errorf("Flee = %v, want (-3,-4)", v)
	}
	if v := Seek(V2(1, 1), V2(1, 1), 5); !v.IsZero() {
		t.Errorf("Seek on target = %v, want zero", v)
	}
	// 1 unit out with deceleration 4: the stopping speed is sqrt(2*4*1).
	if v := Arrive(V2(0, 0), V2(1, 0), 10, 4, 0); !closeTo(v.Len(), math.Sqrt(8), 1e-12) {
		t.Errorf("Arrive speed = %v, want %v", v.Len(), math.Sqrt(8))
	}
	if v := Arrive(V2(0, 0), V2(100, 0), 10, 4, 0); !near(v, V2(10, 0)) {
		t.Errorf("Arrive far away = %v, want full speed (10,0)", v)
	}
	if v := Arrive(V2(0, 0), V2(0.1, 0), 10, 4, 0.25); !v.IsZero() {
		t.Errorf("Arrive inside tol = %v, want zero", v)
	}
}

func TestArriveStopsOnTarget(t *testing.T) {
	b := Body{MaxSpeed: 8, MaxAccel: 6}
	target := V2(20, 0)
	for i := 0; i < 300; i++ {
		Integrate(&b, Arrive(b.Pos, target, b.MaxSpeed, b.MaxAccel, 0.05), testDT)
	}
	if d := b.Pos.Dist(target); d > 0.1 {
		t.Fatalf("stopped %v from the target at %v", d, b.Pos)
	}
	if s := b.Vel.Len(); s > 0.5 {
		t.Fatalf("still moving at %v", s)
	}
}

func TestLimitAccel(t *testing.T) {
	v := LimitAccel(V2(0, 0), V2(10, 0), 30, testDT)
	if !near(v, V2(1, 0)) {
		t.Fatalf("LimitAccel = %v, want (1,0): 30 units/s^2 for 1/30 s", v)
	}
	if v := LimitAccel(V2(1, 1), V2(5, 5), 0, testDT); !near(v, V2(5, 5)) {
		t.Fatalf("no limit = %v", v)
	}
}

func TestReach(t *testing.T) {
	cases := []struct{ v0, max, accel, t, want float64 }{
		{0, 8, 4, 1, 2},
		{0, 8, 4, 3, 16}, // 2 s ramp covers 8, then 1 s at 8
		{8, 8, 4, 2, 16},
		{3, 5, 0, 2, 10},
		{0, 5, 4, 0, 0},
	}
	for _, c := range cases {
		if got := Reach(c.v0, c.max, c.accel, c.t); !closeTo(got, c.want, 1e-12) {
			t.Errorf("Reach(%v,%v,%v,%v) = %v, want %v", c.v0, c.max, c.accel, c.t, got, c.want)
		}
	}
}

func TestInterceptClosedForm(t *testing.T) {
	// Stationary target 10 away at speed 5: 2 s.
	in := Intercept(V2(0, 0), 5, 5, 0, V2(10, 0), V2(0, 0), 10)
	if !in.OK || !closeTo(in.Time, 2, 1e-12) || !near(in.Point, V2(10, 0)) {
		t.Fatalf("stationary: %+v", in)
	}
	// Crossing target: |(10, 3t)| = 5t gives t = 2.5 at (10, 7.5).
	in = Intercept(V2(0, 0), 5, 5, 0, V2(10, 0), V2(0, 3), 10)
	if !in.OK || !closeTo(in.Time, 2.5, 1e-12) || !near(in.Point, V2(10, 7.5)) {
		t.Fatalf("crossing: %+v", in)
	}
	// A faster target running away cannot be caught.
	in = Intercept(V2(0, 0), 5, 5, 0, V2(10, 0), V2(6, 0), 10)
	if in.OK {
		t.Fatalf("faster target caught: %+v", in)
	}
	if v := Pursue(V2(0, 0), V2(0, 0), 5, 0, V2(10, 0), V2(6, 0), 10); !near(v, V2(5, 0)) {
		t.Fatalf("Pursue fallback = %v, want straight at the target", v)
	}
	// The horizon bounds the search.
	if in = Intercept(V2(0, 0), 5, 5, 0, V2(100, 0), V2(0, 0), 10); in.OK {
		t.Fatalf("intercept beyond the horizon: %+v", in)
	}
}

func TestInterceptWithAcceleration(t *testing.T) {
	// From rest, 5 units/s^2 to 5 units/s: 2.5 units in the first second,
	// then 5 per second, so a stationary target 10 away takes 2.5 s.
	in := Intercept(V2(0, 0), 0, 5, 5, V2(10, 0), V2(0, 0), 5)
	if !in.OK || !closeTo(in.Time, 2.5, 1e-6) {
		t.Fatalf("accelerating: %+v", in)
	}
}

// TestInterceptIsEarliest checks seeded random cases: at the returned time the
// pursuer just reaches the target, and slightly earlier it cannot.
func TestInterceptIsEarliest(t *testing.T) {
	r := NewRand(7)
	checked := 0
	for i := 0; i < 2000; i++ {
		pos := V2(r.Range(-30, 30), r.Range(-30, 30))
		tpos := V2(r.Range(-30, 30), r.Range(-30, 30))
		tvel := V2(r.Range(-6, 6), r.Range(-6, 6))
		v0, maxSpeed, accel := r.Range(0, 4), r.Range(4, 9), r.Range(0, 8)
		in := Intercept(pos, v0, maxSpeed, accel, tpos, tvel, 6)
		if !in.OK {
			continue
		}
		checked++
		gap := Reach(v0, maxSpeed, accel, in.Time) - tpos.AddScaled(tvel, in.Time).Dist(pos)
		if gap < -1e-6 {
			t.Fatalf("case %d: pursuer is %v short at t=%v", i, -gap, in.Time)
		}
		if in.Time > 0.01 {
			early := in.Time - 0.01
			if g := Reach(v0, maxSpeed, accel, early) - tpos.AddScaled(tvel, early).Dist(pos); g > 1e-6 {
				t.Fatalf("case %d: reachable %v earlier than t=%v", i, g, in.Time)
			}
		}
	}
	if checked < 500 {
		t.Fatalf("only %d of 2000 cases intercepted; the generator is off", checked)
	}
}

func TestCutoff(t *testing.T) {
	bounds := Rect{Min: V2(-20, -50), Max: V2(20, 50)}
	// The target runs for the +X edge at 5 units/s and leaves at t = 4.
	in := Cutoff(V2(10, -10), 0, 6, 0, V2(0, 0), V2(5, 0), bounds, 0, 10)
	if !in.OK || in.Time > 4 || in.Point.X > 20 {
		t.Fatalf("reachable cutoff: %+v", in)
	}
	early := Cutoff(V2(10, -10), 0, 6, 0, V2(0, 0), V2(5, 0), bounds, 0.5, 10)
	if !early.OK || early.Time <= in.Time {
		t.Fatalf("a 0.5 s margin must meet the target later on its path: %+v vs %+v", early, in)
	}
	if got := Reach(0, 6, 0, early.Time-0.5); got < early.Point.Dist(V2(10, -10))-1e-6 {
		t.Fatalf("margin cutoff not reachable 0.5 s early")
	}
	// Too far away: the best angle is the point where the target leaves.
	far := Cutoff(V2(-20, -40), 0, 3, 0, V2(0, 0), V2(5, 0), bounds, 0, 10)
	if far.OK || !near(far.Point, V2(20, 0)) || !closeTo(far.Time, 4, 1e-12) {
		t.Fatalf("unreachable cutoff: %+v", far)
	}
	if want := V2(40, 40).Normalize(); !near(far.Dir, want) {
		t.Fatalf("cutoff angle %v, want %v", far.Dir, want)
	}
}

func TestSeparation(t *testing.T) {
	others := []Body{
		{Pos: V2(0, 0)},
		{Pos: V2(1, 0)},
		{Pos: V2(-1, 0)},
		{Pos: V2(0, 0.5), Team: 1},
		{Pos: V2(0, -0.5), Flags: FlagInactive},
	}
	// Symmetric neighbours on the same team cancel.
	if p := Separation(V2(0, 0), 0, others, 2, 0); !near(p, V2(0, 0)) {
		t.Fatalf("symmetric push = %v", p)
	}
	// All teams: the team-1 body above pushes down by (2-0.5)/2.
	if p := Separation(V2(0, 0), 0, others, 2, -1); !near(p, V2(0, -0.75)) {
		t.Fatalf("all-team push = %v, want (0,-0.75)", p)
	}
	// Coincident bodies split by index.
	two := []Body{{Pos: V2(3, 3)}, {Pos: V2(3, 3)}}
	if a, b := Separation(two[0].Pos, 0, two, 1, 0), Separation(two[1].Pos, 1, two, 1, 0); a.X != -1 || b.X != 1 {
		t.Fatalf("coincident pushes %v and %v, want -X and +X", a, b)
	}
}

func TestGuardPointAndShadow(t *testing.T) {
	zone, protect := V2(0, 10), V2(0, 20)
	if p, engaged := GuardPoint(zone, 5, V2(30, 0), protect, 1); engaged || !near(p, zone) {
		t.Fatalf("threat outside: %v %v", p, engaged)
	}
	if p, engaged := GuardPoint(zone, 5, V2(0, 8), protect, 1); !engaged || !near(p, V2(0, 9)) {
		t.Fatalf("threat inside: %v %v, want (0,9)", p, engaged)
	}

	b := Body{MaxSpeed: 9, MaxAccel: 20}
	target, tvel := V2(5, 0), V2(2, 1)
	offset := V2(0, -2)
	for i := 0; i < 90; i++ {
		Integrate(&b, Shadow(b.Pos, target, tvel, offset, b.MaxSpeed, 3), testDT)
		target = target.AddScaled(tvel, testDT)
	}
	if d := b.Pos.Dist(target.Add(offset)); d > 0.15 {
		t.Fatalf("shadow error %v after 3 s", d)
	}
}

func TestApproachAngle(t *testing.T) {
	cases := []struct {
		dir, ideal Vec2
		want       float64
	}{
		{V2(1, 0), V2(1, 0), 0},
		{V2(1, 0), V2(0, 1), math.Pi / 2},
		{V2(0, 1), V2(1, 0), math.Pi / 2},
		{V2(1, 0), V2(-1, 0), math.Pi},
		{V2(0, 0), V2(1, 0), 0},
	}
	for _, c := range cases {
		if got := ApproachAngle(c.dir, c.ideal); !closeTo(got, c.want, 1e-12) {
			t.Errorf("ApproachAngle(%v,%v) = %v, want %v", c.dir, c.ideal, got, c.want)
		}
	}
}

func TestSteeringDoesNotAllocate(t *testing.T) {
	bodies := make([]Body, 22)
	for i := range bodies {
		bodies[i] = Body{Pos: V2(float64(i), float64(i%3)), Team: i % 2}
	}
	bounds := Rect{Min: V2(-20, -50), Max: V2(20, 50)}
	var sink Vec2
	allocs := testing.AllocsPerRun(200, func() {
		in := Intercept(V2(0, 0), 1, 6, 5, V2(10, 3), V2(-1, 2), 5)
		cut := Cutoff(V2(0, 0), 1, 6, 5, V2(10, 3), V2(-1, 2), bounds, 0.2, 5)
		sink = in.Point.Add(cut.Point).Add(Separation(V2(3, 1), 3, bodies, 2, -1))
		sink = sink.Add(Pursue(V2(0, 0), V2(1, 0), 6, 5, V2(10, 3), V2(-1, 2), 5))
	})
	if allocs != 0 {
		t.Fatalf("steering allocated %.1f times per call", allocs)
	}
	_ = sink
}

func BenchmarkIntercept(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Intercept(V2(0, 0), 1, 6, 5, V2(10, 3), V2(-1, 2), 5)
	}
}

func BenchmarkInterceptClosedForm(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Intercept(V2(0, 0), 6, 6, 0, V2(10, 3), V2(-1, 2), 5)
	}
}
