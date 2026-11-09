package teamai

import (
	"errors"
	"strings"
	"testing"
)

func near(a, b Vec2) bool {
	const tol = 1e-9
	d := a.Sub(b)
	return d.X < tol && d.X > -tol && d.Y < tol && d.Y > -tol
}

func mustCompile(t testing.TB, s *Spec) *Book {
	t.Helper()
	b, err := Compile(s, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return b
}

func drillSpec() *Spec {
	return &Spec{
		Format:  Format,
		Version: Version,
		Roles:   []string{"runner", "guard", "keeper"},
		Bands:   []string{"left", "middle", "right"},
		Formations: []FormationSpec{{
			Name: "wedge",
			Slots: []SlotSpec{
				{Name: "point", Role: "runner", At: V2(0, -1)},
				{Name: "wing", Role: "runner", At: V2(4, -2), Side: SideStrong},
				{Name: "far", Role: "runner", At: V2(4, -2), Side: SideWeak},
				{Name: "tail", Role: "guard", From: "point", At: V2(0, -3), Alternates: []string{"runner"}},
				{Name: "wide", Role: "keeper", Lateral: "edge", At: V2(3, -1), Side: SideStrong,
					Bands: []BandOffsetSpec{{Band: "left", Offset: V2(1, 0)}}},
			},
		}},
	}
}

func TestFormationResolve(t *testing.T) {
	b := mustCompile(t, drillSpec())
	id, ok := b.FormationID("wedge")
	if !ok {
		t.Fatal("wedge not found")
	}
	f := b.Formation(id)
	field := Rect{Min: V2(0, 0), Max: V2(50, 100)}

	cases := []struct {
		name  string
		frame Frame
		want  []Vec2
	}{
		{
			name:  "facing +Y, strength right",
			frame: Frame{Origin: V2(20, 40), Band: -1},
			want:  []Vec2{V2(20, 39), V2(24, 38), V2(16, 38), V2(20, 36), V2(47, 39)},
		},
		{
			name:  "strength left swaps strong and weak",
			frame: Frame{Origin: V2(20, 40), Strength: -1, Band: -1},
			want:  []Vec2{V2(20, 39), V2(16, 38), V2(24, 38), V2(20, 36), V2(3, 39)},
		},
		{
			name:  "mirror flips everything",
			frame: Frame{Origin: V2(20, 40), Mirror: true, Band: -1},
			want:  []Vec2{V2(20, 39), V2(16, 38), V2(24, 38), V2(20, 36), V2(3, 39)},
		},
		{
			name:  "band offset applies in its band",
			frame: Frame{Origin: V2(20, 40), Band: 0},
			want:  []Vec2{V2(20, 39), V2(24, 38), V2(16, 38), V2(20, 36), V2(46, 39)},
		},
		{
			// Facing +X: Right() is -Y. Depth runs along +X.
			name:  "facing +X rotates the formation",
			frame: Frame{Origin: V2(20, 40), Forward: V2(1, 0), Band: -1},
			want:  []Vec2{V2(19, 40), V2(18, 36), V2(18, 44), V2(16, 40), V2(19, 3)},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := f.Resolve(&c.frame, field, nil)
			for i := range c.want {
				if !near(got[i], c.want[i]) {
					t.Errorf("slot %s = %v, want %v", f.Slot(i).Name, got[i], c.want[i])
				}
			}
		})
	}
}

func TestFormationEdgeWithoutFieldUsesOffset(t *testing.T) {
	b := mustCompile(t, drillSpec())
	f := b.Formation(0)
	got := f.Resolve(&Frame{Origin: V2(0, 0), Band: -1}, Rect{}, nil)
	if !near(got[4], V2(3, -1)) {
		t.Fatalf("edge slot without a field = %v, want (3,-1)", got[4])
	}
}

func TestFormationClamp(t *testing.T) {
	s := drillSpec()
	s.Formations[0].Slots[1].At = V2(40, -2)
	s.Formations[0].Slots[1].Clamp = true
	b := mustCompile(t, s)
	got := b.Formation(0).Resolve(&Frame{Origin: V2(20, 40), Band: -1}, Rect{Max: V2(50, 100)}, nil)
	if !near(got[1], V2(50, 38)) {
		t.Fatalf("clamped slot = %v, want (50,38)", got[1])
	}
}

func TestFormationResolveDoesNotAllocate(t *testing.T) {
	b := mustCompile(t, drillSpec())
	f := b.Formation(0)
	dst := make([]Vec2, f.NumSlots())
	fr := Frame{Origin: V2(20, 40), Band: 2}
	field := Rect{Max: V2(50, 100)}
	allocs := testing.AllocsPerRun(100, func() { dst = f.Resolve(&fr, field, dst) })
	if allocs != 0 {
		t.Fatalf("Resolve allocated %.1f times per call", allocs)
	}
}

func TestAssignByRole(t *testing.T) {
	b := mustCompile(t, drillSpec())
	f := b.Formation(0)
	runner, _ := b.Role("runner")
	guard, _ := b.Role("guard")
	keeper, _ := b.Role("keeper")
	agents := []Body{
		{Team: 1, Role: runner},                      // 0: other team
		{Team: 0, Role: runner},                      // 1
		{Team: 0, Role: keeper},                      // 2
		{Team: 0, Role: runner, Flags: FlagInactive}, // 3: out of play
		{Team: 0, Role: runner},                      // 4
		{Team: 0, Role: runner},                      // 5
		{Team: 0, Role: runner},                      // 6: no guard, fills tail
	}
	_ = guard
	dst := make([]int, f.NumSlots())
	used := make([]bool, len(agents))
	empty := AssignByRole(dst, f, agents, 0, used)
	want := []int{1, 4, 5, 6, 2}
	if empty != 0 {
		t.Fatalf("empty = %d, want 0", empty)
	}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("slot %s got agent %d, want %d (all %v)", f.Slot(i).Name, dst[i], want[i], dst)
		}
	}

	// Without the extra runner the tail stays empty.
	empty = AssignByRole(dst, f, agents[:6], 0, used)
	if empty != 1 || dst[3] != -1 {
		t.Fatalf("empty = %d, tail = %d; want 1 empty and tail -1", empty, dst[3])
	}
}

func TestAssignNearest(t *testing.T) {
	b := mustCompile(t, drillSpec())
	f := b.Formation(0)
	runner, _ := b.Role("runner")
	keeper, _ := b.Role("keeper")
	fr := Frame{Origin: V2(20, 40), Band: -1}
	slots := f.Resolve(&fr, Rect{Max: V2(50, 100)}, nil)
	agents := []Body{
		{Role: runner, Pos: V2(15, 38)}, // nearest to far (16,38)
		{Role: runner, Pos: V2(25, 38)}, // nearest to wing (24,38)
		{Role: runner, Pos: V2(20, 30)}, // tail
		{Role: runner, Pos: V2(20, 39)}, // point
		{Role: keeper, Pos: V2(45, 39)},
	}
	dst := make([]int, f.NumSlots())
	used := make([]bool, len(agents))
	AssignNearest(dst, f, agents, 0, used, slots)
	want := []int{3, 1, 0, 2, 4}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("slot %s got agent %d, want %d (all %v)", f.Slot(i).Name, dst[i], want[i], dst)
		}
	}
}

func TestFormationValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *Spec)
		want string
	}{
		{"unknown role", func(s *Spec) { s.Formations[0].Slots[1].Role = "striker" },
			`formations[0](wedge).slots[1](wing).role: unknown role "striker"`},
		{"unknown alternate", func(s *Spec) { s.Formations[0].Slots[3].Alternates = []string{"x"} },
			`formations[0](wedge).slots[3](tail).alternates[0]: unknown role "x"`},
		{"unknown from", func(s *Spec) { s.Formations[0].Slots[3].From = "nose" },
			`formations[0](wedge).slots[3](tail).from: unknown slot "nose"`},
		{"cycle", func(s *Spec) { s.Formations[0].Slots[0].From = "tail" },
			`slots placed from each other form a cycle`},
		{"duplicate slot", func(s *Spec) { s.Formations[0].Slots[2].Name = "wing" },
			`formations[0](wedge).slots[2](wing).name: duplicate slot name; first defined at slots[1]`},
		{"bad lateral", func(s *Spec) { s.Formations[0].Slots[0].Lateral = "middle" },
			`formations[0](wedge).slots[0](point).lateral: lateral "middle": want origin or edge`},
		{"unknown band", func(s *Spec) { s.Formations[0].Slots[4].Bands[0].Band = "hash" },
			`formations[0](wedge).slots[4](wide).bands[0](hash).band: unknown band "hash"`},
		{"bad role name", func(s *Spec) { s.Roles[1] = "has space" },
			`roles[1]: name "has space"`},
		{"duplicate role", func(s *Spec) { s.Roles[2] = "runner" },
			`roles[2](runner): duplicate name; first defined at roles[0]`},
		{"wrong format", func(s *Spec) { s.Format = "other" },
			`format: format "other": want "gosx.teamai"`},
		{"newer version", func(s *Spec) { s.Version = 9 },
			`version: version 9 is newer than this build reads (1)`},
		{"empty formation", func(s *Spec) { s.Formations[0].Slots = nil },
			`formations[0](wedge).slots: a formation needs at least one slot`},
		{"host landmark with at", func(s *Spec) {
			s.Landmarks = []LandmarkSpec{{Name: "goal", Host: true, At: V2(1, 1)}}
		}, `landmarks[0](goal).at: a host landmark takes its position from World.Landmarks`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := drillSpec()
			c.edit(s)
			_, err := Compile(s, nil)
			var se *SpecError
			if !errors.As(err, &se) {
				t.Fatalf("Compile error = %v, want a *SpecError", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

func TestLandmarkPos(t *testing.T) {
	s := drillSpec()
	s.Landmarks = []LandmarkSpec{
		{Name: "goal", Host: true},
		{Name: "deep", At: V2(5, 20), Side: SideStrong},
	}
	b := mustCompile(t, s)
	goal, _ := b.Landmark("goal")
	deep, _ := b.Landmark("deep")
	hosted := []Vec2{V2(25, 95), {}}
	fr := Frame{Origin: V2(20, 40), Strength: -1}
	if got := b.LandmarkPos(goal, &fr, hosted); !near(got, V2(25, 95)) {
		t.Errorf("host landmark = %v", got)
	}
	if got := b.LandmarkPos(deep, &fr, hosted); !near(got, V2(15, 60)) {
		t.Errorf("local landmark = %v, want (15,60)", got)
	}
}
