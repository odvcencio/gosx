package physicsys

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/physics"
)

// triggerWorld drops a ball through a trigger slab.
func triggerWorld() *physics.World {
	w := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})
	zone := w.AddBody(physics.BodyConfig{ID: "zone"})
	zone.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, IsTrigger: true, Width: 2, Height: 0.5, Depth: 2})
	ball := w.AddBody(physics.BodyConfig{ID: "ball", Mass: 1, Position: physics.Vec3{Y: 1.2}})
	ball.AddCollider(physics.ColliderConfig{Shape: physics.ShapeSphere, Radius: 0.25})
	return w
}

// runTriggers steps a runtime through frames of the given lengths and returns
// the handler log plus the emitted game events.
func runTriggers(t *testing.T, frames []time.Duration) ([]string, []string) {
	t.Helper()
	world := triggerWorld()
	var log []string
	record := func(ctx *game.Context, e physics.TriggerEvent) {
		if ctx.Phase != game.PhaseFixedUpdate {
			t.Fatalf("handler ran in phase %s", ctx.Phase)
		}
		if ctx.Tick != e.Step {
			t.Fatalf("handler tick %d, event step %d", ctx.Tick, e.Step)
		}
		log = append(log, fmt.Sprintf("%d:%s:%s", ctx.Tick, e.Phase, e.Other.Body.ID))
	}
	rt := game.New(game.Config{
		Physics:   world,
		FixedStep: time.Second / 60,
		Systems: []game.System{Triggers(TriggerHandlers{
			Enter: record, Stay: record, Exit: record, Emit: true,
		})},
	})
	var emitted []string
	for _, frame := range frames {
		if _, err := rt.Step(frame); err != nil {
			t.Fatal(err)
		}
		for _, event := range rt.Events() {
			data := event.Data.(physics.TriggerEvent)
			emitted = append(emitted, fmt.Sprintf("%d:%s:%s", data.Step, event.Type, event.Target))
		}
	}
	return log, emitted
}

func TestTriggersDeliverEveryStepWithItsContext(t *testing.T) {
	frames := make([]time.Duration, 90)
	for i := range frames {
		frames[i] = time.Second / 60
	}
	log, emitted := runTriggers(t, frames)
	joined := strings.Join(log, " ")
	if strings.Count(joined, ":enter:ball") != 1 || strings.Count(joined, ":exit:ball") != 1 || !strings.Contains(joined, ":stay:ball") {
		t.Fatalf("handler log = %v", log)
	}
	if len(emitted) != 2 || !strings.Contains(emitted[0], EventTriggerEnter+":zone") || !strings.Contains(emitted[1], EventTriggerExit+":zone") {
		t.Fatalf("emitted events = %v, want one enter and one exit targeting the zone", emitted)
	}
}

func TestTriggersDoNotDependOnFrameTiming(t *testing.T) {
	even := make([]time.Duration, 90)
	for i := range even {
		even[i] = time.Second / 60
	}
	// The same 90 fixed steps, delivered in uneven frames of 0 to 3 steps.
	var uneven []time.Duration
	pattern := []int{3, 0, 1, 2, 3, 1, 0, 2}
	steps := 0
	for i := 0; steps < 90; i++ {
		n := pattern[i%len(pattern)]
		if steps+n > 90 {
			n = 90 - steps
		}
		uneven = append(uneven, time.Duration(n)*time.Second/60+time.Millisecond/10)
		steps += n
	}
	want, _ := runTriggers(t, even)
	got, _ := runTriggers(t, uneven)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("uneven frames changed the trigger events:\n got %v\nwant %v", got, want)
	}
}

func TestTriggersFollowAReplacedWorld(t *testing.T) {
	first := triggerWorld()
	second := triggerWorld()
	enters := 0
	system := Triggers(TriggerHandlers{Enter: func(*game.Context, physics.TriggerEvent) { enters++ }})
	ctx := &game.Context{Physics: first}
	_ = system.Update(ctx)
	ctx.Physics = second
	_ = system.Update(ctx)
	for i := 0; i < 40; i++ {
		first.StepFixed()
		second.StepFixed()
	}
	if enters != 1 {
		t.Fatalf("enters = %d, want 1 (only the current world delivers)", enters)
	}
	system.Close()
	for i := 0; i < 40; i++ {
		second.StepFixed()
	}
	if enters != 1 {
		t.Fatal("Close must stop delivery")
	}
}
