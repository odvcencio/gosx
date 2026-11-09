package physics

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// triggerLog records events as compact strings so tests can compare whole
// sequences.
type triggerLog []string

func (l *triggerLog) record(e TriggerEvent) {
	*l = append(*l, fmt.Sprintf("%d:%s:%d/%d", e.Step, e.Phase, e.Trigger.index, e.Other.index))
}

func (l triggerLog) phases() string {
	parts := make([]string, len(l))
	for i, entry := range l {
		parts[i] = strings.Split(entry, ":")[1]
	}
	return strings.Join(parts, ",")
}

// fallThroughTrigger drops a ball through a static trigger box.
func fallThroughTrigger() (*World, *Collider, *RigidBody) {
	w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})
	zone := w.AddCollider(ColliderConfig{Shape: ShapeBox, IsTrigger: true, Width: 2, Height: 0.5, Depth: 2})
	ball := w.AddBody(BodyConfig{ID: "ball", Mass: 1, Position: Vec3{Y: 1.2}})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.25})
	return w, zone, ball
}

func TestTriggerEnterStayExitInOrder(t *testing.T) {
	w, zone, ball := fallThroughTrigger()
	var log triggerLog
	w.SetTriggerHandler(log.record)
	for i := 0; i < 90; i++ {
		w.StepFixed()
		// The handler and TriggerEvents agree on every step.
		events := w.TriggerEvents()
		if len(events) > 0 && fmt.Sprintf("%d:%s", events[0].Step, events[0].Phase) != strings.Join(strings.Split(log[len(log)-1], ":")[:2], ":") {
			t.Fatalf("step %d: TriggerEvents %v disagrees with the handler log %v", i, events, log)
		}
	}
	phases := log.phases()
	if !strings.HasPrefix(phases, "enter,stay") || !strings.HasSuffix(phases, "stay,exit") || strings.Count(phases, "enter") != 1 || strings.Count(phases, "exit") != 1 {
		t.Fatalf("event phases = %s", phases)
	}
	first := TriggerEvent{}
	w2, zone2, _ := fallThroughTrigger()
	for i := 0; i < 90 && first.Phase == 0; i++ {
		w2.StepFixed()
		if events := w2.TriggerEvents(); len(events) > 0 {
			first = events[0]
		}
	}
	if first.Trigger != zone2 || first.Other.Body.ID != "ball" {
		t.Fatalf("first event names trigger %v other %v", first.Trigger, first.Other)
	}
	if other, ok := first.Involves(zone2); !ok || other != first.Other {
		t.Fatal("Involves must return the other side")
	}
	_ = zone
	_ = ball
}

func TestTriggerSeesKinematicBodiesAndKinematicTriggersSeeStatics(t *testing.T) {
	w := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60})
	zone := w.AddCollider(ColliderConfig{Shape: ShapeBox, IsTrigger: true, Width: 1, Height: 1, Depth: 1})
	mover := w.AddBody(BodyConfig{ID: "mover", IsKinematic: true, Position: Vec3{X: 5}})
	mover.AddCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.3, Height: 1})
	wall := w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1, Offset: Vec3{X: -5}})
	carrier := w.AddBody(BodyConfig{ID: "carrier", IsKinematic: true, Position: Vec3{X: -8}})
	sensor := carrier.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5, IsTrigger: true})

	var log triggerLog
	w.SetTriggerHandler(log.record)
	w.StepFixed()
	if len(log) != 0 {
		t.Fatalf("nothing touches yet, got %v", log)
	}
	mover.Position = Vec3{}
	carrier.Position = Vec3{X: -5.6}
	w.StepFixed()
	want := []string{
		fmt.Sprintf("2:enter:%d/%d", zone.index, mover.colliders[0].index),
		fmt.Sprintf("2:enter:%d/%d", sensor.index, wall.index),
	}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("events %v, want %v (sorted by trigger index)", log, want)
	}
	mover.Position = Vec3{X: 5}
	w.StepFixed()
	if last := log[len(log)-2]; last != fmt.Sprintf("3:exit:%d/%d", zone.index, mover.colliders[0].index) {
		t.Fatalf("exit event = %v", log)
	}
}

func TestTriggerPairsHonorLayers(t *testing.T) {
	w, zone, _ := fallThroughTrigger()
	zone.SetCollisionFilter(1<<3, 1<<4)
	var log triggerLog
	w.SetTriggerHandler(log.record)
	for i := 0; i < 90; i++ {
		w.StepFixed()
	}
	if len(log) != 0 {
		t.Fatalf("a trigger that masks out the ball reported %v", log)
	}
}

func TestTriggerPairOfTriggersNamesTheLowerIndex(t *testing.T) {
	w := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60})
	a := w.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1, IsTrigger: true})
	body := w.AddBody(BodyConfig{Mass: 1})
	b := body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1, IsTrigger: true})
	w.StepFixed()
	events := w.TriggerEvents()
	if len(events) != 1 || events[0].Trigger != a || events[0].Other != b || events[0].Phase != TriggerEnter {
		t.Fatalf("events = %+v", events)
	}
}

func TestTriggerMeshReportsOnePairPerCollider(t *testing.T) {
	w := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60})
	config := terrainMeshConfig(4, 4, func(x, z float64) float64 { return 0 })
	config.IsTrigger = true
	mesh := w.AddCollider(config)
	ball := w.AddBody(BodyConfig{Mass: 1})
	// The ball touches several floor triangles at once.
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1.5})
	w.StepFixed()
	events := w.TriggerEvents()
	if len(events) != 1 || events[0].Trigger != mesh {
		t.Fatalf("events = %+v", events)
	}
}

func TestTriggerRemovalEndsOverlapSilently(t *testing.T) {
	w := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60})
	w.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1, IsTrigger: true})
	pickup := w.AddBody(BodyConfig{Mass: 1})
	pickup.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.2})
	var log triggerLog
	removed := false
	w.SetTriggerHandler(func(e TriggerEvent) {
		log.record(e)
		// A handler may remove what it touched.
		if e.Phase == TriggerEnter && !removed {
			removed = w.RemoveBody(pickup)
		}
	})
	w.StepFixed()
	w.StepFixed()
	if !removed || log.phases() != "enter" {
		t.Fatalf("removed=%v events=%v; removal must end the overlap without an exit", removed, log)
	}
	if got := w.TriggerOverlaps(nil); len(got) != 0 {
		t.Fatalf("overlaps after removal = %v", got)
	}
}

func TestTriggerOverlapsSurviveSnapshotRestore(t *testing.T) {
	original, _, _ := fallThroughTrigger()
	var want triggerLog
	original.SetTriggerHandler(want.record)
	var snapshot []byte
	for i := 0; i < 90; i++ {
		original.StepFixed()
		if i == 28 {
			if len(original.TriggerOverlaps(nil)) == 0 {
				t.Fatal("the ball should be inside the trigger at step 29")
			}
			snapshot = original.Snapshot()
		}
	}

	restored, _, _ := fallThroughTrigger()
	// Advance the copy to the same step count, then load the snapshot.
	for i := 0; i <= 28; i++ {
		restored.StepFixed()
	}
	var state WorldState
	if err := json.Unmarshal(snapshot, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Triggers) == 0 {
		t.Fatal("snapshot lost the trigger overlap")
	}
	restored.ApplyState(WorldState{Bodies: state.Bodies})
	if got := restored.TriggerOverlaps(nil); len(got) != 0 {
		t.Fatalf("a state without trigger overlaps must clear them, got %v", got)
	}
	restored.Restore(snapshot)
	var got triggerLog
	restored.SetTriggerHandler(got.record)
	for i := 29; i < 90; i++ {
		restored.StepFixed()
	}
	tail := want[len(want)-len(got):]
	if fmt.Sprint(got) != fmt.Sprint(tail) || !strings.HasPrefix(got.phases(), "stay") {
		t.Fatalf("restored events %v, want %v", got, tail)
	}
}

func TestTriggerUpdateDoesNotAllocateInSteadyState(t *testing.T) {
	w, _, ball := fallThroughTrigger()
	ball.Position = Vec3{}
	w.SetGravity(Vec3{})
	w.StepFixed()
	w.StepFixed()
	if allocs := testing.AllocsPerRun(200, w.updateTriggers); allocs != 0 {
		t.Fatalf("updateTriggers allocates %.1f times per step", allocs)
	}
	if events := w.TriggerEvents(); len(events) != 1 || events[0].Phase != TriggerStay {
		t.Fatalf("steady overlap should be one stay event, got %+v", events)
	}
}
