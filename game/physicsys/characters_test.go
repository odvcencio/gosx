package physicsys

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/physics"
)

// characterRuntime builds a runtime with a floor, stairs, a lift driven by a
// fixed-update system, a trigger and one character, all scripted by tick.
func characterRuntime(t *testing.T) (*game.Runtime, *CharacterSystem, *physics.Character, *[]string) {
	t.Helper()
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})
	world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 40, Height: 1, Depth: 40, Offset: physics.Vec3{Y: -0.5}})
	for i := 0; i < 4; i++ {
		h := 0.2 * float64(i+1)
		world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 0.4, Height: h, Depth: 3, Offset: physics.Vec3{X: 3 + 0.4*float64(i) + 0.2, Y: h / 2}})
	}
	lift := world.AddBody(physics.BodyConfig{ID: "lift", IsKinematic: true, Position: physics.Vec3{X: -4, Y: 0.3}})
	lift.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 2, Height: 0.2, Depth: 2})
	zone := world.AddBody(physics.BodyConfig{ID: "zone"})
	zone.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, IsTrigger: true, Width: 1, Height: 2, Depth: 3, Offset: physics.Vec3{X: 1.5, Y: 1}})
	hero := world.AddCharacter(physics.CharacterConfig{ID: "hero", Position: physics.Vec3{X: -4, Y: 2}, JumpSpeed: 5})

	var log []string
	liftSystem := game.Func("lift", game.PhaseFixedUpdate, func(ctx *game.Context) error {
		lift.Position = physics.Vec3{X: -4, Y: 0.3 + 0.5*math.Sin(float64(ctx.Tick)*0.02)}
		return nil
	})
	characters := Characters(func(ctx *game.Context, c *physics.Character) physics.CharacterInput {
		tick := float64(ctx.Tick)
		return physics.CharacterInput{
			Move: physics.Vec3{X: 2.5 * math.Cos(tick*0.004), Z: 0.3 * math.Sin(tick*0.03)},
			Jump: ctx.Tick%120 == 0,
		}
	}, AfterMove(func(ctx *game.Context, c *physics.Character, move physics.CharacterMove) {
		if move.Landed {
			log = append(log, "landed")
		}
	}))
	triggers := Triggers(TriggerHandlers{
		Enter: func(*game.Context, physics.TriggerEvent) { log = append(log, "enter") },
		Exit:  func(*game.Context, physics.TriggerEvent) { log = append(log, "exit") },
	})
	rt := game.New(game.Config{
		Physics:   world,
		FixedStep: time.Second / 60,
		Systems:   []game.System{liftSystem, characters, triggers},
	})
	return rt, characters, hero, &log
}

func stepFrames(t *testing.T, rt *game.Runtime, frames int) {
	t.Helper()
	for i := 0; i < frames; i++ {
		if _, err := rt.Step(time.Second / 60); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCharactersSystemDrivesCharactersAtTheFixedStep(t *testing.T) {
	rt, system, hero, log := characterRuntime(t)
	stepFrames(t, rt, 600)
	if hero.Position().X < 1 {
		t.Fatalf("hero at %+v; the input walks it toward the stairs", hero.Position())
	}
	if move, ok := system.LastMove(hero); !ok || move.Displacement == (physics.Vec3{}) {
		t.Fatalf("LastMove = %+v %v", move, ok)
	}
	landed, entered := 0, 0
	for _, entry := range *log {
		switch entry {
		case "landed":
			landed++
		case "enter":
			entered++
		}
	}
	if landed < 2 || entered == 0 {
		t.Fatalf("log = %v; expected landings from the scripted jumps and a trigger entry", *log)
	}
}

func TestCharactersSurviveRuntimeSnapshotRestore(t *testing.T) {
	original, _, _, originalLog := characterRuntime(t)
	stepFrames(t, original, 300)
	snapshot := original.Snapshot()
	logAtSnapshot := len(*originalLog)
	stepFrames(t, original, 300)
	want := string(original.State())

	restored, _, _, restoredLog := characterRuntime(t)
	// Put the copy somewhere else first, so the restore has to do the work.
	stepFrames(t, restored, 77)
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	*restoredLog = (*restoredLog)[:0]
	stepFrames(t, restored, 300)
	if got := string(restored.State()); got != want {
		t.Fatalf("restored runtime diverged:\n got %s\nwant %s", got, want)
	}
	tail := (*originalLog)[logAtSnapshot:]
	if len(tail) != len(*restoredLog) {
		t.Fatalf("event log after restore %v, original %v", *restoredLog, tail)
	}
	for i := range tail {
		if tail[i] != (*restoredLog)[i] {
			t.Fatalf("event log after restore %v, original %v", *restoredLog, tail)
		}
	}
}

func TestCharactersIgnoreFrameTiming(t *testing.T) {
	even, _, _, _ := characterRuntime(t)
	stepFrames(t, even, 240)
	uneven, _, _, _ := characterRuntime(t)
	steps := 0
	pattern := []int{3, 0, 1, 2, 4, 1, 0, 2}
	for i := 0; steps < 240; i++ {
		n := pattern[i%len(pattern)]
		if steps+n > 240 {
			n = 240 - steps
		}
		if _, err := uneven.Step(time.Duration(n)*time.Second/60 + time.Millisecond/10); err != nil {
			t.Fatal(err)
		}
		steps += n
	}
	// The runtime state also counts render frames, which differ on purpose;
	// the physics state must not.
	a, _ := json.Marshal(even.Physics().StateSnapshot())
	b, _ := json.Marshal(uneven.Physics().StateSnapshot())
	if string(a) != string(b) {
		t.Fatalf("uneven frames changed the character simulation:\n%s\n%s", a, b)
	}
}

func TestCharactersUpdateDoesNotAllocate(t *testing.T) {
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})
	world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 40, Height: 1, Depth: 40, Offset: physics.Vec3{Y: -0.5}})
	for i := 0; i < 4; i++ {
		world.AddCharacter(physics.CharacterConfig{Position: physics.Vec3{X: float64(i) * 2, Y: 1}})
	}
	system := Characters(func(*game.Context, *physics.Character) physics.CharacterInput {
		return physics.CharacterInput{Move: physics.Vec3{Z: 2}}
	})
	ctx := &game.Context{Physics: world, Phase: game.PhaseFixedUpdate}
	for i := 0; i < 60; i++ {
		_ = system.Update(ctx)
		world.StepFixed()
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = system.Update(ctx) }); allocs != 0 {
		t.Fatalf("Update allocates %.1f times per fixed step", allocs)
	}
}
