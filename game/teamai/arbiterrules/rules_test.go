package arbiterrules

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/game/teamai"
)

const source = `
rule LaneOpen {
    when { nearest_opponent.dist > 6 and self.y < 30 }
    then Read { branch: "deep" }
}

rule Crowded {
    when { nearest_opponent.dist <= 6 }
    then Read { branch: "short" }
}

rule Late {
    when { signal.clock < 10 }
    then Read { branch: "short" }
}
`

func spec() *teamai.Spec {
	return &teamai.Spec{
		Format: teamai.Format, Version: teamai.Version,
		Roles:   []string{"runner"},
		Signals: []string{"clock"},
		Assignments: []teamai.AssignmentSpec{
			{Name: "read", Steps: []teamai.StepSpec{
				{Do: "decide", DecisionSpec: teamai.DecisionSpec{
					Window:   1,
					Branches: []teamai.BranchSpec{{When: "LaneOpen", Goto: "deep"}, {When: "Crowded", Goto: "short"}},
				}},
				{Label: "deep", Do: "hold", Duration: 5},
				{Label: "short", Do: "hold", Duration: 5},
			}},
			{Name: "pick", Steps: []teamai.StepSpec{
				{Do: "decide", DecisionSpec: teamai.DecisionSpec{
					Chooser:  "reads",
					Branches: []teamai.BranchSpec{{Goto: "deep"}, {Goto: "short"}},
				}},
				{Label: "deep", Do: "hold", Duration: 5},
				{Label: "short", Do: "hold", Duration: 5},
			}},
		},
	}
}

func world(oppY float64) *teamai.World {
	return &teamai.World{
		Teams: []teamai.Team{{Frame: teamai.Frame{Band: -1}}, {Frame: teamai.Frame{Band: -1}}},
		Agents: []teamai.Body{
			{Pos: teamai.V2(0, 5), MaxSpeed: 5},
			{Pos: teamai.V2(0, oppY), Team: 1, MaxSpeed: 5},
		},
		Signals: []float64{60},
	}
}

func compile(t testing.TB) (*teamai.Book, *Rules) {
	t.Helper()
	r, err := Compile([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	reg := teamai.NewRegistry()
	if err := r.Register(reg, ""); err != nil {
		t.Fatal(err)
	}
	ch, err := r.Chooser("Late", "LaneOpen", "Crowded")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Chooser("reads", ch); err != nil {
		t.Fatal(err)
	}
	b, err := teamai.Compile(spec(), reg)
	if err != nil {
		t.Fatal(err)
	}
	return b, r
}

func run(t testing.TB, b *teamai.Book, assignment string, w *teamai.World) int {
	t.Helper()
	ex, err := teamai.NewExecutor(b, teamai.Options{Step: 1.0 / 30})
	if err != nil {
		t.Fatal(err)
	}
	ex.Reset(2, 2)
	a, _ := b.AssignmentID(assignment)
	ex.Assign(0, a)
	ex.Step(w)
	return ex.Agent(0).Step
}

func TestRulesDriveDecisions(t *testing.T) {
	b, _ := compile(t)
	if got := run(t, b, "read", world(20)); got != 1 {
		t.Fatalf("open lane: step %d, want 1 (deep)", got)
	}
	if got := run(t, b, "read", world(8)); got != 2 {
		t.Fatalf("crowded: step %d, want 2 (short)", got)
	}
}

func TestChooserReadsBranchParam(t *testing.T) {
	b, _ := compile(t)
	if got := run(t, b, "pick", world(20)); got != 1 {
		t.Fatalf("chooser with an open lane: step %d, want 1 (deep)", got)
	}
	w := world(20)
	w.Signals[0] = 5 // Late is first in the chooser's order
	if got := run(t, b, "pick", w); got != 2 {
		t.Fatalf("chooser when late: step %d, want 2 (short)", got)
	}
}

func TestPredicateDoesNotAllocate(t *testing.T) {
	b, r := compile(t)
	p, err := r.Predicate("LaneOpen")
	if err != nil {
		t.Fatal(err)
	}
	ex, _ := teamai.NewExecutor(b, teamai.Options{Step: 1.0 / 30})
	ex.Reset(2, 2)
	a, _ := b.AssignmentID("read")
	ex.Assign(0, a)
	w := world(20)
	ex.Step(w)
	allocs := testing.AllocsPerRun(200, func() {
		ex.Assign(0, a)
		ex.Step(w)
	})
	t.Logf("decide step through Arbiter: %.1f allocs", allocs)
	if allocs != 0 {
		t.Fatalf("a decision through Arbiter allocated %.1f times", allocs)
	}
	_ = p
}

func TestUnknownFactFailsAtCompile(t *testing.T) {
	r, err := Compile([]byte(`rule Bad { when { self.altitude > 1 } then Read { branch: "x" } }`))
	if err != nil {
		t.Fatal(err)
	}
	reg := teamai.NewRegistry()
	if err := r.Register(reg, ""); err != nil {
		t.Fatal(err)
	}
	s := spec()
	s.Assignments = []teamai.AssignmentSpec{{Name: "x", Steps: []teamai.StepSpec{{Do: "hold", Until: "Bad"}}}}
	_, err = teamai.Compile(s, reg)
	if err == nil || !strings.Contains(err.Error(), `unknown property "altitude"`) {
		t.Fatalf("err = %v, want the unknown fact reported", err)
	}
}

func TestGovernanceIsRefused(t *testing.T) {
	_, err := Compile([]byte(`
rule Slow {
    rollout 50
    when { self.speed > 1 }
    then Read { branch: "x" }
}`))
	if err == nil || !strings.Contains(err.Error(), "uses a rollout") {
		t.Fatalf("err = %v, want rollouts refused", err)
	}
}

func BenchmarkArbiterPredicate(b *testing.B) {
	book, r := compile(b)
	p, _ := r.Predicate("LaneOpen")
	ex, _ := teamai.NewExecutor(book, teamai.Options{Step: 1.0 / 30})
	ex.Reset(2, 2)
	a, _ := book.AssignmentID("read")
	w := world(20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ex.Assign(0, a)
		ex.Step(w)
	}
	_ = p
}
