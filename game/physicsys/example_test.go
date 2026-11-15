package physicsys_test

import (
	"fmt"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/physicsys"
	"m31labs.dev/gosx/physics"
)

// A character walks across a floor, up four stairs and through a pickup
// trigger, then jumps. The runtime drives it at the fixed step.
func Example() {
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})

	// Level geometry: body-less static colliders.
	world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 40, Height: 1, Depth: 40, Offset: physics.Vec3{Y: -0.5}})
	stairX := []float64{4, 4.4, 4.8, 5.2}
	for i, rise := range []float64{0.2, 0.4, 0.6, 0.8} {
		world.AddCollider(physics.ColliderConfig{
			Shape: physics.ShapeBox, Width: 0.4, Height: rise, Depth: 3,
			Offset: physics.Vec3{X: stairX[i], Y: rise / 2},
		})
	}
	world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 12, Height: 0.8, Depth: 3, Offset: physics.Vec3{X: 11, Y: 0.4}})

	// A pickup: a trigger on its own layer.
	const pickupLayer = 1 << 4
	coin := world.AddBody(physics.BodyConfig{ID: "coin", Position: physics.Vec3{X: 6.5, Y: 1.4}})
	coin.AddCollider(physics.ColliderConfig{Shape: physics.ShapeSphere, Radius: 0.4, IsTrigger: true, Layer: pickupLayer})

	hero := world.AddCharacter(physics.CharacterConfig{
		ID:        "hero",
		Position:  physics.Vec3{Y: 1},
		JumpSpeed: physics.JumpSpeedForHeight(1.0, 9.81),
	})

	walk := physicsys.Characters(func(ctx *game.Context, c *physics.Character) physics.CharacterInput {
		// Walk +X at 3 m/s; jump once, on tick 200.
		return physics.CharacterInput{Move: physics.Vec3{X: 3}, Jump: ctx.Tick == 200}
	}, physicsys.AfterMove(func(ctx *game.Context, c *physics.Character, move physics.CharacterMove) {
		if move.Jumped {
			fmt.Printf("tick %d: jumped at x=%.2f\n", ctx.Tick, c.Position().X)
		}
		if move.Landed {
			fmt.Printf("tick %d: landed at y=%.2f\n", ctx.Tick, c.FootPosition().Y)
		}
	}))
	pickups := physicsys.Triggers(physicsys.TriggerHandlers{
		Enter: func(ctx *game.Context, e physics.TriggerEvent) {
			fmt.Printf("tick %d: %s picked up %s\n", ctx.Tick, e.Other.Body.ID, e.Trigger.Body.ID)
			world.RemoveBody(e.Trigger.Body)
		},
	})

	rt := game.New(game.Config{
		Physics:   world,
		FixedStep: time.Second / 60,
		Systems:   []game.System{walk, pickups},
	})
	for frame := 0; frame < 300; frame++ {
		rt.Step(time.Second / 60)
	}
	fmt.Printf("hero at x=%.2f y=%.2f grounded=%v\n", hero.Position().X, hero.FootPosition().Y, hero.Grounded())
	// Output:
	// tick 8: landed at y=0.02
	// tick 118: hero picked up coin
	// tick 200: jumped at x=9.86
	// tick 253: landed at y=0.82
	// hero at x=14.86 y=0.82 grounded=true
}
