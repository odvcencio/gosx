package teamai

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestStartPlanFillsSlotsAndMovesThere(t *testing.T) {
	d := newDrill(t, 4, 1)
	f := d.book.Formation(d.ex.Formation(0))
	for i := 0; i < 4; i++ {
		info := d.ex.Agent(i)
		if info.Slot < 0 || info.Kind != StepSlot {
			t.Fatalf("red agent %d: %+v, want a slot and a slot step", i, info)
		}
		if want := f.SlotSign(&d.w.Teams[0].Frame, info.Slot); info.Side != want {
			t.Fatalf("red agent %d side %v, want %v", i, info.Side, want)
		}
	}
	// Before the go signal (tick 15) every raider heads for its slot.
	slots := f.Resolve(&d.w.Teams[0].Frame, d.w.Field, nil)
	var before [4]float64
	for i := range before {
		before[i] = d.w.Agents[i].Pos.Dist(slots[d.ex.Agent(i).Slot])
	}
	for d.w.Tick < 15 {
		d.step()
	}
	for i := range before {
		after := d.w.Agents[i].Pos.Dist(slots[d.ex.Agent(i).Slot])
		// From rest at 12 units/s^2, 0.5 s covers about 1.5 units.
		if after > 0.5 && before[i]-after < 1.4 {
			t.Errorf("raider %d went from %.2f to %.2f from its slot in 0.5 s", i, before[i], after)
		}
		if k := d.ex.Agent(i).Kind; k != StepSlot {
			t.Errorf("raider %d left its slot step (%v) before the go signal", i, k)
		}
	}
}

func TestDrillRunsAndRaidersReachTheFlag(t *testing.T) {
	d := newDrill(t, 4, 1)
	breakMark, _ := d.book.Mark("break")
	sawBreak := false
	steps := map[StepKind]bool{}
	for i := 0; i < 900; i++ {
		d.step()
		for _, ev := range d.ex.Events() {
			if ev.Kind == EventMark && ev.Value == breakMark {
				sawBreak = true
			}
		}
		for a := range d.w.Agents {
			steps[d.ex.Agent(a).Kind] = true
		}
	}
	if !sawBreak {
		t.Error("no raider reached the break mark")
	}
	if d.grabs == 0 {
		t.Error("no raider reached the flag in 30 s")
	}
	for _, k := range []StepKind{StepSlot, StepPath, StepGuard, StepMove} {
		if !steps[k] {
			t.Errorf("no agent ran a %v step", k)
		}
	}
	t.Logf("grabs %d, tags %d", d.grabs, d.tags)
}

// miniBook builds a Book from a Spec with a small default skeleton.
func miniBook(t testing.TB, reg *Registry, edit func(s *Spec)) *Book {
	t.Helper()
	s := &Spec{
		Format: Format, Version: Version,
		Roles:     []string{"a", "b"},
		Objects:   []string{"ball"},
		Signals:   []string{"flag"},
		Landmarks: []LandmarkSpec{{Name: "spot", Host: true}, {Name: "goal", At: V2(0, 30)}},
		Formations: []FormationSpec{{Name: "line", Slots: []SlotSpec{
			{Name: "s0", Role: "a", At: V2(-2, 0)}, {Name: "s1", Role: "a", At: V2(2, 0)},
		}}},
		Rules: []RuleSpec{{Name: "flag_up", Fact: "signal.flag", Op: ">=", Value: f64(1)}},
	}
	edit(s)
	b, err := Compile(s, reg)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return b
}

func oneAgentWorld(b *Book, agents ...Body) *World {
	return &World{
		Teams:     []Team{{Frame: Frame{Band: -1}}, {Frame: Frame{Origin: V2(0, 30), Forward: V2(0, -1), Band: -1}}},
		Agents:    agents,
		Objects:   []Body{{Pos: V2(0, 20)}},
		Landmarks: []Vec2{V2(5, 5), {}},
		Signals:   []float64{0},
	}
}

func runTicks(ex *Executor, w *World, n int, each func(cmds []Command)) {
	for i := 0; i < n; i++ {
		cmds := ex.Step(w)
		if each != nil {
			each(cmds)
		}
		for j := range w.Agents {
			Integrate(&w.Agents[j], cmds[j].Vel, ex.dt)
		}
		w.Tick++
	}
}

func TestInterruptFiresOnRisingEdgeAfterReaction(t *testing.T) {
	b := miniBook(t, nil, func(s *Spec) {
		s.Assignments = []AssignmentSpec{{Name: "wait", Steps: []StepSpec{
			{Do: "hold", Until: "flag_up", Next: "wait2"},
			{Label: "alarm", Do: "hold", Duration: 10},
			{Label: "wait2", Do: "hold", Duration: 10},
		}, Interrupts: []InterruptSpec{{When: "flag_up", Goto: "alarm"}}}}
	})
	ex, _ := NewExecutor(b, Options{Step: testDT})
	w := oneAgentWorld(b, Body{MaxSpeed: 5, Reaction: 3})
	ex.Reset(1, 2)
	a, _ := b.AssignmentID("wait")
	ex.Assign(0, a)
	fires := 0
	firedAt := -1
	for tick := 0; tick < 40; tick++ {
		switch tick {
		case 5:
			w.Signals[0] = 1
		case 20:
			w.Signals[0] = 0
		case 25:
			w.Signals[0] = 1
		}
		ex.Step(w)
		for _, ev := range ex.Events() {
			if ev.Kind == EventInterrupt {
				fires++
				if firedAt < 0 {
					firedAt = tick
				}
			}
		}
		w.Tick++
	}
	// The rule turns true on tick 5. With Reaction 3 the interrupt waits out
	// ticks 5, 6 and 7 and fires on tick 8. (The hold step's own until sees
	// the rule at once and moves on to wait2 on tick 5; interrupts run
	// regardless of the step.) It fires again only after the rule goes false
	// on tick 20 and true on tick 25.
	if firedAt != 8 {
		t.Fatalf("first fire on tick %d, want 8", firedAt)
	}
	if fires != 2 {
		t.Fatalf("fired %d times, want 2 (one per rising edge)", fires)
	}
}

func TestPursueEndsOnContactAndEngageReportsOnce(t *testing.T) {
	b := miniBook(t, nil, func(s *Spec) {
		s.Assignments = []AssignmentSpec{
			{Name: "tackle", Steps: []StepSpec{
				{Do: "pursue", Target: "nearest_opponent", Radius: 1, Mark: "caught"},
				{Do: "engage", Target: "nearest_opponent", Radius: 1, Duration: 2},
			}},
			{Name: "flee", Steps: []StepSpec{{Do: "move", Landmark: "goal", Speed: 0.5}}},
		}
	})
	ex, _ := NewExecutor(b, Options{Step: testDT})
	w := oneAgentWorld(b,
		Body{Pos: V2(0, 0), MaxSpeed: 8, MaxAccel: 20, Team: 0},
		Body{Pos: V2(10, 5), MaxSpeed: 6, Team: 1},
	)
	ex.Reset(2, 2)
	tackle, _ := b.AssignmentID("tackle")
	flee, _ := b.AssignmentID("flee")
	ex.Assign(0, tackle)
	ex.Assign(1, flee)
	contacts, caught := 0, -1
	caughtMark, _ := b.Mark("caught")
	runTicks(ex, w, 200, func([]Command) {
		for _, ev := range ex.Events() {
			if ev.Agent != 0 {
				continue
			}
			if ev.Kind == EventContact {
				contacts++
				if ev.Target != Agent(1) {
					t.Errorf("contact with %+v, want agent 1", ev.Target)
				}
			}
			if ev.Kind == EventMark && ev.Value == caughtMark {
				caught = int(w.Tick)
			}
		}
	})
	if caught < 0 {
		t.Fatal("pursuer never caught the target")
	}
	if contacts != 1 {
		t.Fatalf("contact reported %d times, want once", contacts)
	}
	if d := w.Agents[0].Pos.Dist(w.Agents[1].Pos); d > 1.5 {
		t.Logf("after engage: distance %v", d)
	}
}

func TestGuardStandsBetweenThreatAndGoal(t *testing.T) {
	b := miniBook(t, nil, func(s *Spec) {
		s.Assignments = []AssignmentSpec{{Name: "zone", Steps: []StepSpec{
			{Do: "guard", Landmark: "goal", Radius: 8, Target: "nearest_opponent", Gap: 2},
		}}}
	})
	ex, _ := NewExecutor(b, Options{Step: testDT})
	w := oneAgentWorld(b,
		Body{Pos: V2(0, 25), MaxSpeed: 8},
		Body{Pos: V2(30, 0), Team: 1}, // outside the zone
	)
	ex.Reset(2, 2)
	z, _ := b.AssignmentID("zone")
	ex.Assign(0, z)
	runTicks(ex, w, 120, nil)
	if d := w.Agents[0].Pos.Dist(V2(0, 30)); d > 0.3 {
		t.Fatalf("guard is %v from the zone centre with no threat", d)
	}
	w.Agents[1].Pos = V2(0, 24) // inside, 6 short of the goal
	runTicks(ex, w, 120, nil)
	if d := w.Agents[0].Pos.Dist(V2(0, 26)); d > 0.3 {
		t.Fatalf("guard at %v, want (0,26): 2 units from the threat toward the goal", w.Agents[0].Pos)
	}
}

func TestWeightedDecisionIsSeededAndProportional(t *testing.T) {
	b := miniBook(t, nil, func(s *Spec) {
		s.Assignments = []AssignmentSpec{{Name: "pick", Steps: []StepSpec{
			{Do: "decide", DecisionSpec: DecisionSpec{Mode: "weighted", Branches: []BranchSpec{
				{Goto: "a", Weight: 1}, {Goto: "b", Weight: 3}, {When: "flag_up", Goto: "c", Weight: 100},
			}}},
			{Label: "a", Do: "hold", Duration: 1},
			{Label: "b", Do: "hold", Duration: 1},
			{Label: "c", Do: "hold", Duration: 1},
		}}}
	})
	pick, _ := b.AssignmentID("pick")
	count := [3]int{}
	first := -1
	for seed := uint64(0); seed < 4000; seed++ {
		ex, _ := NewExecutor(b, Options{Step: testDT})
		w := oneAgentWorld(b, Body{MaxSpeed: 1})
		w.Seed = seed
		ex.Reset(1, 2)
		ex.Assign(0, pick)
		ex.Step(w)
		for _, ev := range ex.Events() {
			if ev.Kind == EventBranch {
				count[ev.Value]++
				if seed == 0 {
					first = ev.Value
				}
			}
		}
	}
	if count[2] != 0 {
		t.Fatalf("a branch whose rule is false was drawn %d times", count[2])
	}
	if frac := float64(count[1]) / 4000; frac < 0.72 || frac > 0.78 {
		t.Fatalf("weight-3 branch drawn %.3f of the time, want about 0.75 (%v)", frac, count)
	}
	// Pinned: these counts and the first draw are the same on every platform.
	if count != [3]int{1009, 2991, 0} || first != 0 {
		t.Fatalf("draws %v first %d changed; they must be identical on every platform", count, first)
	}
}

func TestChooserAndRegistryPredicates(t *testing.T) {
	reg := NewRegistry()
	calls := 0
	if err := reg.Chooser("second", ChooserFunc(func(q *Query, d *Decision) int {
		calls++
		if d.NumBranches() != 2 || d.BranchLabel(1) != "y" {
			t.Errorf("chooser sees %d branches", d.NumBranches())
		}
		return 1
	})); err != nil {
		t.Fatal(err)
	}
	if err := reg.Predicate("ball_near", PredicateFunc(func(q *Query) bool {
		pos, _, ok := q.RefBody(Object(0))
		return ok && pos.Dist(q.Body().Pos) < 3
	})); err != nil {
		t.Fatal(err)
	}
	b := miniBook(t, reg, func(s *Spec) {
		s.Assignments = []AssignmentSpec{{Name: "c", Steps: []StepSpec{
			{Do: "decide", DecisionSpec: DecisionSpec{Chooser: "second", Branches: []BranchSpec{{Goto: "x"}, {Goto: "y"}}}},
			{Label: "x", Do: "hold", Duration: 5},
			{Label: "y", Do: "move", Landmark: "spot", Until: "ball_near"},
			{Do: "hold", Duration: 5},
		}}}
	})
	ex, _ := NewExecutor(b, Options{Step: testDT})
	w := oneAgentWorld(b, Body{MaxSpeed: 5})
	ex.Reset(1, 2)
	c, _ := b.AssignmentID("c")
	ex.Assign(0, c)
	ex.Step(w)
	if calls != 1 || ex.Agent(0).Step != 2 {
		t.Fatalf("chooser calls %d, step %d; want 1 call and step 2", calls, ex.Agent(0).Step)
	}
	w.Objects[0].Pos = w.Agents[0].Pos.Add(V2(1, 0))
	ex.Step(w)
	if ex.Agent(0).Step != 3 {
		t.Fatalf("until did not end the step: at step %d", ex.Agent(0).Step)
	}
	if reg.Predicate("ball_near", PredicateFunc(func(*Query) bool { return true })) == nil {
		t.Fatal("registering a name twice must fail")
	}
}

func TestFacts(t *testing.T) {
	b := miniBook(t, nil, func(s *Spec) {
		s.AgentSignals = []string{"stamina"}
		s.TeamSignals = []string{"score"}
		s.Assignments = []AssignmentSpec{{Name: "h", Steps: []StepSpec{
			{Do: "pursue", Target: "object:ball"},
		}}}
	})
	ex, _ := NewExecutor(b, Options{Step: testDT})
	w := oneAgentWorld(b,
		Body{Pos: V2(1, 2), Vel: V2(0, 1), MaxSpeed: 5},
		Body{Pos: V2(1, 6), Vel: V2(0, -2), Team: 1},
	)
	w.AgentSignals = []float64{0.7, 0.2}
	w.TeamSignals = []float64{3, -3}
	w.Tick = 42
	ex.Reset(2, 2)
	h, _ := b.AssignmentID("h")
	ex.Assign(0, h)
	ex.SetMemory(0, 2, 9)
	ex.SetSlot(0, 0, -1) // left side: lateral facts flip sign
	ex.Step(w)
	q := &ex.q
	q.reset(ex, w, 0)
	q.target = Object(0)
	cases := map[string]float64{
		"tick":                     42,
		"signal.flag":              0,
		"team.score":               3,
		"mem.2":                    9,
		"self.x":                   -1,
		"self.y":                   2,
		"self.stamina":             0.7,
		"self.speed":               1,
		"self.vy":                  1,
		"nearest_opponent.dist":    4,
		"nearest_opponent.dy":      4,
		"nearest_opponent.closing": 3,
		"nearest_opponent.stamina": 0.2,
		"nearest_teammate.exists":  0,
		"target.dist":              math.Hypot(1, 18),
		"target.x":                 0,
		"object.ball.y":            20,
		"landmark.spot.x":          -5,
		"landmark.goal.y":          30,
		"self.step_time":           elapsed(1, testDT),
	}
	for name, want := range cases {
		f, err := b.ParseFact(name)
		if err != nil {
			t.Errorf("ParseFact(%q): %v", name, err)
			continue
		}
		if got := q.Fact(f); !closeTo(got, want, 1e-9) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	f, _ := b.ParseFact("nearest_teammate.dist")
	if got := q.Fact(f); !math.IsNaN(got) {
		t.Errorf("missing subject = %v, want NaN", got)
	}
	for _, bad := range []string{"", "self", "self.altitude", "object.moon.x", "signal.nope", "mem.4", "tick.x", "landmark.spot"} {
		if _, err := b.ParseFact(bad); err == nil {
			t.Errorf("ParseFact(%q) succeeded", bad)
		}
	}
}

func TestCompareWithNaNIsFalse(t *testing.T) {
	for _, op := range []cmpOp{opLT, opLE, opGT, opGE, opEQ, opNE} {
		if compare(op, math.NaN(), 1) || compare(op, 1, math.NaN()) {
			t.Errorf("op %d with NaN is true", op)
		}
	}
}

func TestSnapshotRestoreReproducesTheRun(t *testing.T) {
	a := newDrill(t, 5, 3)
	for i := 0; i < 200; i++ {
		a.step()
	}
	snap := a.ex.AppendState(nil)
	if len(snap) != StateSize(a.ex.NumAgents(), 2) {
		t.Fatalf("state is %d bytes, StateSize says %d", len(snap), StateSize(a.ex.NumAgents(), 2))
	}
	worldCopy := cloneWorld(&a.w)
	carrier := a.carrier

	var want []uint64
	for i := 0; i < 300; i++ {
		a.step()
		want = append(want, a.hash())
	}

	b := newDrill(t, 5, 99) // different start, overwritten below
	if err := b.ex.LoadState(snap); err != nil {
		t.Fatal(err)
	}
	b.w = worldCopy
	b.carrier = carrier
	for i := 0; i < 300; i++ {
		b.step()
		if got := b.hash(); got != want[i] {
			t.Fatalf("restored run differs at tick %d after the snapshot", i)
		}
	}
}

func cloneWorld(w *World) World {
	c := *w
	c.Teams = append([]Team(nil), w.Teams...)
	c.Agents = append([]Body(nil), w.Agents...)
	c.Objects = append([]Body(nil), w.Objects...)
	c.Landmarks = append([]Vec2(nil), w.Landmarks...)
	c.Signals = append([]float64(nil), w.Signals...)
	c.TeamSignals = append([]float64(nil), w.TeamSignals...)
	c.AgentSignals = append([]float64(nil), w.AgentSignals...)
	return c
}

func TestLoadStateRejectsBadInputAndKeepsState(t *testing.T) {
	d := newDrill(t, 3, 1)
	for i := 0; i < 50; i++ {
		d.step()
	}
	good := d.ex.AppendState(nil)
	before := d.ex.Hash()
	bad := map[string][]byte{
		"empty":     nil,
		"truncated": good[:len(good)-1],
		"version":   append([]byte{9}, good[1:]...),
	}
	corrupt := append([]byte(nil), good...)
	corrupt[5+2*2] = 0x7f // first agent's assignment index
	bad["assignment index"] = corrupt
	nan := append([]byte(nil), good...)
	for k := 0; k < 8; k++ {
		nan[5+2*2+8+k] = 0xff // first agent's side: NaN bits
	}
	bad["NaN"] = nan
	for name, src := range bad {
		if err := d.ex.LoadState(src); !errors.Is(err, ErrState) {
			t.Errorf("%s: err = %v, want ErrState", name, err)
		}
		if d.ex.Hash() != before {
			t.Errorf("%s: a failed LoadState changed the executor", name)
		}
	}
}

func TestStepDoesNotAllocate(t *testing.T) {
	d := newDrill(t, 11, 5)
	for i := 0; i < 300; i++ { // warm the event buffer through the busy start
		d.step()
	}
	allocs := testing.AllocsPerRun(200, func() { d.step() })
	if allocs != 0 {
		t.Fatalf("a 22-agent tick allocated %.2f times", allocs)
	}
	snap := make([]byte, 0, StateSize(22, 2))
	allocs = testing.AllocsPerRun(100, func() {
		snap = d.ex.AppendState(snap[:0])
		if err := d.ex.LoadState(snap); err != nil {
			t.Fatal(err)
		}
		_ = d.ex.Hash()
	})
	if allocs != 0 {
		t.Fatalf("snapshot and restore allocated %.2f times", allocs)
	}
}

func BenchmarkStep22Agents(b *testing.B) {
	d := newDrill(b, 11, 5)
	for i := 0; i < 300; i++ {
		d.step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Keep the drill in its busy phase: rewind the tick count only.
		d.ex.Step(&d.w)
	}
}

func BenchmarkSnapshot22Agents(b *testing.B) {
	d := newDrill(b, 11, 5)
	for i := 0; i < 300; i++ {
		d.step()
	}
	snap := make([]byte, 0, StateSize(22, 2))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap = d.ex.AppendState(snap[:0])
		if err := d.ex.LoadState(snap); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAssignmentValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *Spec)
		want string
	}{
		{"unknown rule in until", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "hold", Until: "nope"}}}}
		}, `assignments[0](x).steps[0].until: unknown rule "nope"`},
		{"unknown do", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "dance"}}}}
		}, `assignments[0](x).steps[0].do: do "dance"`},
		{"field not allowed", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "hold", Duration: 1, Target: "nearest_opponent"}}}}
		}, `assignments[0](x).steps[0].target: a hold step does not take target`},
		{"bad target", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "pursue", Target: "object:moon"}}}}
		}, `assignments[0](x).steps[0].target: target "object:moon"`},
		{"engage needs radius", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "engage", Target: "nearest_opponent"}}}}
		}, `assignments[0](x).steps[0].radius: an engage step needs a contact radius`},
		{"unknown move slot", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "move", Slot: "s9"}}}}
		}, `assignments[0](x).steps[0].slot: no formation has a slot "s9"`},
		{"interrupt goto", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "hold", Duration: 1}},
				Interrupts: []InterruptSpec{{When: "flag_up", Goto: "nowhere"}}}}
		}, `assignments[0](x).interrupts[0](flag_up).goto: unknown label "nowhere"`},
		{"plan slot", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{{Do: "slot"}}}}
			s.Plans = []PlanSpec{{Name: "p", Formation: "line", Assign: []PlanAssignSpec{{Slot: "s7", Assignment: "x"}}}}
		}, `plans[0](p).assign[0](s7).slot: formation "line" has no slot "s7"`},
		{"plan assignment", func(s *Spec) {
			s.Plans = []PlanSpec{{Name: "p", Formation: "line", Assign: []PlanAssignSpec{{Role: "a", Assignment: "zz"}}}}
		}, `plans[0](p).assign[0](a).assignment: unknown assignment "zz"`},
		{"rule cycle", func(s *Spec) {
			s.Rules = append(s.Rules, RuleSpec{Name: "r1", Not: "r2"}, RuleSpec{Name: "r2", All: []string{"r1"}})
		}, `rule refers to itself`},
		{"rule two forms", func(s *Spec) {
			s.Rules = append(s.Rules, RuleSpec{Name: "r1", Fact: "tick", Op: ">", Value: f64(1), Not: "flag_up"})
		}, `rules[1](r1): a rule needs exactly one of`},
		{"rule bad fact", func(s *Spec) {
			s.Rules = append(s.Rules, RuleSpec{Name: "r1", Fact: "self.altitude", Op: ">", Value: f64(1)})
		}, `rules[1](r1).fact: fact "self.altitude": unknown property "altitude"`},
		{"rule bad op", func(s *Spec) {
			s.Rules = append(s.Rules, RuleSpec{Name: "r1", Fact: "tick", Op: "=~", Value: f64(1)})
		}, `rules[1](r1).op: op "=~"`},
		{"unknown chooser", func(s *Spec) {
			s.Assignments = []AssignmentSpec{{Name: "x", Steps: []StepSpec{
				{Do: "decide", DecisionSpec: DecisionSpec{Chooser: "oracle"}},
			}}}
		}, `assignments[0](x).steps[0].chooser: unknown chooser "oracle"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Spec{
				Format: Format, Version: Version, Roles: []string{"a"}, Signals: []string{"flag"},
				Formations: []FormationSpec{{Name: "line", Slots: []SlotSpec{{Name: "s0", Role: "a"}}}},
				Rules:      []RuleSpec{{Name: "flag_up", Fact: "signal.flag", Op: ">=", Value: f64(1)}},
			}
			c.edit(s)
			_, err := Compile(s, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v\nwant it to contain %q", err, c.want)
			}
		})
	}
}

// BenchmarkDrillTick22Agents is one whole host tick of the 22-agent drill:
// Executor.Step, event handling, contests and integration.
func BenchmarkDrillTick22Agents(b *testing.B) {
	d := newDrill(b, 11, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.step()
	}
}

// BenchmarkHash22Agents is the executor state hash used by determinism checks.
func BenchmarkHash22Agents(b *testing.B) {
	d := newDrill(b, 11, 5)
	for i := 0; i < 300; i++ {
		d.step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.ex.Hash()
	}
}
