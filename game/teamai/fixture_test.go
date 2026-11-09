package teamai

import (
	"encoding/binary"
	"math"
	"testing"
)

// The fixture is a toy two-team capture-the-flag drill. Team 0 (red) starts
// at y = 10 facing +Y and raids a flag at (0, 50). Team 1 (blue) starts at
// y = 50 facing -Y: a keeper guards the flag zone and chasers run the raiders
// down. Nothing in it belongs to a particular sport.

func f64(v float64) *float64 { return &v }

// ctfSpec returns the drill for perTeam agents a side (at least 3).
func ctfSpec(perTeam int) *Spec {
	s := &Spec{
		Format:  Format,
		Version: Version,
		Roles:   []string{"runner", "chaser", "keeper"},
		Bands:   []string{"left", "middle", "right"},
		Objects: []string{"flag"},
		Signals: []string{"go", "carried"},
		TeamSignals: []string{
			"score",
		},
		AgentSignals: []string{"stamina", "carrying"},
		Landmarks: []LandmarkSpec{
			{Name: "home", Host: true},
			{Name: "midfield", At: V2(0, 20)},
		},
		Rules: []RuleSpec{
			{Name: "go", Fact: "signal.go", Op: ">=", Value: f64(1)},
			{Name: "lane_open", Fact: "nearest_opponent.dist", Op: ">", Value: f64(6)},
			{Name: "threat_close", Fact: "nearest_opponent.dist", Op: "<", Value: f64(5)},
			{Name: "deep_enough", Fact: "self.y", Op: ">", Value: f64(25)},
			{Name: "rested", Fact: "self.stamina", Op: ">", Value: f64(0.5)},
			{Name: "press", All: []string{"go", "rested"}},
			{Name: "flag_loose", Fact: "object.flag.speed", Op: "<", Value: f64(0.01)},
			{Name: "grabbed", Fact: "mark.grab", Op: "==", Value: f64(1)},
			{Name: "flag_held", Fact: "signal.carried", Op: ">=", Value: f64(1)},
			{Name: "flag_free", Not: "flag_held"},
			{Name: "not_carrying", Fact: "self.carrying", Op: "<", Value: f64(1)},
			{Name: "escort_now", All: []string{"flag_held", "not_carrying"}},
		},
		Paths: []PathSpec{{
			Name: "post",
			Segments: []SegmentSpec{
				{Kind: "line", By: vp(0, 12), Mark: "break"},
				{Kind: "decide", DecisionSpec: DecisionSpec{
					Window:    0.3,
					Branches:  []BranchSpec{{When: "lane_open", Goto: "deep"}},
					Otherwise: "cut",
				}},
				{Label: "deep", Kind: "line", Landmark: "home", Track: true, Next: "end"},
				{Label: "cut", Kind: "curve", By: vp(-6, 6), Control: vp(0, 6), Speed: 0.8, EndSpeed: f64(1)},
				{Kind: "line", Landmark: "home"},
				{Label: "end", Kind: "hold", Duration: 0.2},
			},
		}},
		Assignments: []AssignmentSpec{
			{Name: "raid", Steps: []StepSpec{
				{Do: "slot", Until: "go"},
				{Do: "path", Path: "post"},
				{Label: "chase_flag", Do: "pursue", Target: "object:flag", Radius: 0.6, Mark: "grab"},
				{Label: "run_home", Do: "move", Landmark: "midfield", Speed: 0.9},
				{Do: "hold", Duration: 0.5, Next: "chase_flag"},
				{Label: "escort", Do: "mirror", Target: "object:flag", Offset: V2(2, -2), Until: "flag_free", Next: "chase_flag"},
			}, Interrupts: []InterruptSpec{{When: "escort_now", Goto: "escort"}}},
			{Name: "keep", Steps: []StepSpec{
				{Do: "guard", Landmark: "home", Radius: 10, Target: "nearest_opponent", Gap: 1.5},
			}},
			{Name: "chase", Steps: []StepSpec{
				{Do: "slot", Until: "press"},
				{Label: "pick", Do: "decide", DecisionSpec: DecisionSpec{
					Mode:     "weighted",
					Branches: []BranchSpec{{Goto: "cut", Weight: 1}, {Goto: "run", Weight: 2}},
				}},
				{Label: "cut", Do: "cutoff", Target: "nearest_opponent", Latch: true, Margin: 0.2, Radius: 1, Next: "tag"},
				{Label: "run", Do: "pursue", Target: "nearest_opponent", Radius: 1, Speed: 0.95},
				{Label: "tag", Do: "engage", Target: "nearest_opponent", Radius: 0.8, Duration: 1},
				{Do: "hold", Duration: 0.5},
				{Do: "slot", Next: "pick"},
			}, Interrupts: []InterruptSpec{{When: "threat_close", Goto: "tag"}}},
		},
	}
	red := FormationSpec{Name: "spread"}
	blue := FormationSpec{Name: "shell"}
	blue.Slots = append(blue.Slots, SlotSpec{Name: "keeper", Role: "keeper", At: V2(0, 3)})
	for i := 0; i < perTeam; i++ {
		lat := float64(4 * (i/2 + 1))
		side := SideStrong
		if i%2 == 1 {
			side = SideWeak
		}
		red.Slots = append(red.Slots, SlotSpec{Name: "r" + itoa(i), Role: "runner", At: V2(lat, -float64(i%3)), Side: side})
		if i < perTeam-1 {
			blue.Slots = append(blue.Slots, SlotSpec{Name: "c" + itoa(i), Role: "chaser", At: V2(lat, 12+float64(i%2)*4), Side: side, Clamp: true})
		}
	}
	s.Formations = []FormationSpec{red, blue}
	s.Plans = []PlanSpec{
		{Name: "red_raid", Formation: "spread", Assign: []PlanAssignSpec{{Role: "runner", Assignment: "raid"}}},
		{Name: "blue_shell", Formation: "shell", Assign: []PlanAssignSpec{
			{Slot: "keeper", Assignment: "keep"},
			{Role: "chaser", Assignment: "chase"},
		}},
	}
	return s
}

// drill is a running host for the fixture.
type drill struct {
	t       testing.TB
	book    *Book
	ex      *Executor
	w       World
	carrier int // agent holding the flag, or -1
	tags    int
	escapes int
	grabs   int
	contest ContestModel
}

func newDrill(t testing.TB, perTeam int, seed uint64) *drill {
	t.Helper()
	b, err := Compile(ctfSpec(perTeam), nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return newDrillBook(t, b, perTeam, seed)
}

// newDrillBook runs the drill from b, which must be ctfSpec(perTeam) compiled
// some way.
func newDrillBook(t testing.TB, b *Book, perTeam int, seed uint64) *drill {
	t.Helper()
	ex, err := NewExecutor(b, Options{Step: testDT, Separation: 1.2})
	if err != nil {
		t.Fatal(err)
	}
	d := &drill{t: t, book: b, ex: ex, carrier: -1}
	runner, _ := b.Role("runner")
	chaser, _ := b.Role("chaser")
	keeper, _ := b.Role("keeper")
	n := 2 * perTeam
	d.w = World{
		Seed:  seed,
		Field: Rect{Min: V2(-20, 0), Max: V2(20, 60)},
		Teams: []Team{
			{Frame: Frame{Origin: V2(0, 10), Band: 1}},
			{Frame: Frame{Origin: V2(0, 50), Forward: V2(0, -1), Band: 1}},
		},
		Agents:       make([]Body, n),
		Objects:      []Body{{Pos: V2(0, 50)}},
		Landmarks:    []Vec2{V2(0, 50), {}},
		Signals:      []float64{0, 0},
		TeamSignals:  []float64{0, 0},
		AgentSignals: make([]float64, 2*n),
	}
	r := NewRand(seed)
	for i := 0; i < perTeam; i++ {
		d.w.Agents[i] = Body{Team: 0, Role: runner, Pos: V2(r.Range(-15, 15), r.Range(2, 8)),
			MaxSpeed: 7 + r.Range(0, 1), MaxAccel: 12, Reaction: uint16(i % 3)}
		role := chaser
		if i == 0 {
			role = keeper
		}
		d.w.Agents[perTeam+i] = Body{Team: 1, Role: role, Pos: V2(r.Range(-15, 15), r.Range(52, 58)),
			MaxSpeed: 6.5 + r.Range(0, 1), MaxAccel: 10, Reaction: uint16(2 + i%2)}
	}
	for i := 0; i < n; i++ {
		d.w.AgentSignals[2*i] = 0.4 + float64(i%3)*0.2 // stamina
	}
	ex.Reset(n, 2)
	red, _ := b.PlanID("red_raid")
	blue, _ := b.PlanID("blue_shell")
	if _, err := ex.StartPlan(&d.w, 0, red); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.StartPlan(&d.w, 1, blue); err != nil {
		t.Fatal(err)
	}
	return d
}

// step runs one tick: AI, then the host rules of the drill, then movement.
func (d *drill) step() []Command {
	if d.w.Tick == 15 {
		d.w.Signals[0] = 1
	}
	cmds := d.ex.Step(&d.w)
	grab, _ := d.book.Mark("grab")
	for _, ev := range d.ex.Events() {
		switch {
		case ev.Kind == EventMark && ev.Value == grab && d.carrier < 0:
			d.carrier = ev.Agent
			d.grabs++
		case ev.Kind == EventContact && ev.Target.Kind == RefAgent && int(ev.Target.Index) == d.carrier:
			// A tag is a contest between the chaser and the carrier.
			ch, ca := &d.w.Agents[ev.Agent], &d.w.Agents[d.carrier]
			c := Contest{
				Attacker: 100 * d.w.AgentSignals[2*ev.Agent],
				Defender: 100 * d.w.AgentSignals[2*d.carrier],
				Angle:    ApproachAngle(ch.Vel, ca.Pos.Sub(ch.Pos)),
				RelSpeed: ch.Vel.Len() - ca.Vel.Len(),
			}
			if d.contest.Resolve(c, d.w.Seed, d.w.Tick, uint64(ev.Agent), uint64(d.carrier)).Result != Win {
				d.escapes++
				continue
			}
			d.carrier = -1
			d.tags++
			d.w.Objects[0].Pos = d.w.Landmarks[0]
			d.w.Objects[0].Vel = Vec2{}
		}
	}
	for i := range d.w.Agents {
		Integrate(&d.w.Agents[i], cmds[i].Vel, testDT)
		d.w.Agents[i].Pos = d.w.Field.Clamp(d.w.Agents[i].Pos)
	}
	if d.carrier >= 0 {
		c := &d.w.Agents[d.carrier]
		d.w.Objects[0].Pos, d.w.Objects[0].Vel = c.Pos, c.Vel
	}
	d.w.Signals[1] = 0
	for i := range d.w.Agents {
		d.w.AgentSignals[2*i+1] = 0
	}
	if d.carrier >= 0 {
		d.w.Signals[1] = 1
		d.w.AgentSignals[2*d.carrier+1] = 1
	}
	d.w.Tick++
	return cmds
}

// hash folds the executor hash and every body's bits into one value.
func (d *drill) hash() uint64 {
	h := d.ex.Hash()
	var b [8]byte
	mixIn := func(v uint64) {
		binary.LittleEndian.PutUint64(b[:], v)
		for _, c := range b {
			h ^= uint64(c)
			h *= 1099511628211
		}
	}
	for i := range d.w.Agents {
		a := &d.w.Agents[i]
		mixIn(math.Float64bits(a.Pos.X))
		mixIn(math.Float64bits(a.Pos.Y))
		mixIn(math.Float64bits(a.Vel.X))
		mixIn(math.Float64bits(a.Vel.Y))
	}
	mixIn(math.Float64bits(d.w.Objects[0].Pos.X))
	mixIn(math.Float64bits(d.w.Objects[0].Pos.Y))
	mixIn(uint64(d.carrier + 1))
	return h
}
