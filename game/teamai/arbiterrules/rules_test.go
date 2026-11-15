package arbiterrules

import (
	_ "embed"
	"strings"
	"testing"

	"m31labs.dev/arbiter/gameai"

	"m31labs.dev/gosx/game/teamai"
)

// The bundles are built from testdata/*.arb by TestBundlesAreCurrent (native
// builds only); these tests load them, so they also run as js/wasm.
var (
	//go:embed testdata/reads.bundle
	readsBundle []byte
	//go:embed testdata/bad.bundle
	badBundle []byte
)

func spec() *teamai.Spec {
	decide := func(chooser string, branches ...teamai.BranchSpec) teamai.StepSpec {
		return teamai.StepSpec{Do: "decide", DecisionSpec: teamai.DecisionSpec{
			Window: 1, Chooser: chooser, Branches: branches,
		}}
	}
	holds := []teamai.StepSpec{
		{Label: "deep", Do: "hold", Duration: 5},
		{Label: "short", Do: "hold", Duration: 5},
	}
	return &teamai.Spec{
		Format: teamai.Format, Version: teamai.Version,
		Roles:   []string{"runner"},
		Signals: []string{"clock"},
		Assignments: []teamai.AssignmentSpec{
			{Name: "read", Steps: append([]teamai.StepSpec{
				decide("", teamai.BranchSpec{When: "LaneOpen", Goto: "deep"}, teamai.BranchSpec{When: "Crowded", Goto: "short"}),
			}, holds...)},
			{Name: "pick", Steps: append([]teamai.StepSpec{
				decide("reads", teamai.BranchSpec{Goto: "deep"}, teamai.BranchSpec{Goto: "short"}),
			}, holds...)},
			{Name: "best", Steps: append([]teamai.StepSpec{
				decide("best", teamai.BranchSpec{Goto: "deep"}, teamai.BranchSpec{Goto: "short"}),
			}, holds...)},
			{Name: "weighted", Steps: append([]teamai.StepSpec{
				decide("weighted", teamai.BranchSpec{Goto: "deep"}, teamai.BranchSpec{Goto: "short"}),
			}, holds...)},
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
	r, err := Load(readsBundle)
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
	best, err := r.Decision("pick", gameai.Best("score"))
	if err != nil {
		t.Fatal(err)
	}
	weighted, err := r.Decision("pick", gameai.Weighted("weight"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]teamai.Chooser{"reads": ch, "best": best, "weighted": weighted} {
		if err := reg.Chooser(name, c); err != nil {
			t.Fatal(err)
		}
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

func TestBestDecisionScores(t *testing.T) {
	b, _ := compile(t)
	// PickDeep scores the opponent distance, PickShort 8 minus it.
	if got := run(t, b, "best", world(25)); got != 1 {
		t.Fatalf("opponent 20 away: step %d, want 1 (deep scores 20 against -12)", got)
	}
	if got := run(t, b, "best", world(7)); got != 2 {
		t.Fatalf("opponent 2 away: step %d, want 2 (short scores 6 against 2)", got)
	}
}

func TestWeightedDecisionIsSeeded(t *testing.T) {
	b, _ := compile(t)
	counts := [3]int{}
	for seed := uint64(0); seed < 2000; seed++ {
		w := world(20)
		w.Seed = seed
		counts[run(t, b, "weighted", w)]++
	}
	// Weights 1 (deep) and 3 (short).
	if frac := float64(counts[2]) / 2000; frac < 0.71 || frac > 0.79 {
		t.Fatalf("short drawn %.3f of the time, want about 0.75 (%v)", frac, counts)
	}
	w := world(20)
	w.Seed = 42
	first := run(t, b, "weighted", w)
	if again := run(t, b, "weighted", w); again != first {
		t.Fatal("the same seed and tick drew differently")
	}
}

func TestDecisionsDoNotAllocate(t *testing.T) {
	b, _ := compile(t)
	ex, _ := teamai.NewExecutor(b, teamai.Options{Step: 1.0 / 30})
	ex.Reset(2, 2)
	w := world(20)
	for _, name := range []string{"read", "pick", "best", "weighted"} {
		a, _ := b.AssignmentID(name)
		ex.Assign(0, a)
		ex.Step(w)
		allocs := testing.AllocsPerRun(200, func() {
			ex.Assign(0, a)
			ex.Step(w)
		})
		if allocs != 0 {
			t.Errorf("%s: a decision through Arbiter allocated %.1f times", name, allocs)
		}
	}
}

func TestUnknownFactFailsAtCompile(t *testing.T) {
	r, err := Load(badBundle)
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

func TestDigestIsStable(t *testing.T) {
	a, _ := Load(readsBundle)
	b, _ := Load(readsBundle)
	if a.Digest() != b.Digest() || a.Program().DigestHex() == "" {
		t.Fatal("the same bundle gave different digests")
	}
}

func BenchmarkArbiterPredicate(b *testing.B) {
	book, _ := compile(b)
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
}

func BenchmarkArbiterBestDecision(b *testing.B) {
	book, _ := compile(b)
	ex, _ := teamai.NewExecutor(book, teamai.Options{Step: 1.0 / 30})
	ex.Reset(2, 2)
	a, _ := book.AssignmentID("best")
	w := world(20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ex.Assign(0, a)
		ex.Step(w)
	}
}
