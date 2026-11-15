# Characters, queries and triggers

The `physics` package gives a 3D action game the pieces it builds gameplay on:

- **Shape casts and overlap queries**: sweep a sphere, capsule or box through the world and get the first hit, or list everything a shape touches.
- **Collision layers**: decide which colliders meet, in the simulation and in every query.
- **Triggers**: enter, stay and exit events for sensor volumes such as pickups, checkpoints and damage zones.
- **A character controller**: a capsule that walks, climbs stairs, respects a slope limit, sticks to the ground, jumps, rides moving platforms and pushes crates.
- **Runtime systems** in `game/physicsys` that run the characters and deliver trigger events at the fixed step.

Everything here is deterministic and lives in the world's snapshot. A server and a browser that run the same inputs get the same characters and the same trigger events, bit for bit, on amd64, arm64 and WebAssembly.

## A worked example

This is `game/physicsys/example_test.go`. It compiles, runs under `go test`, and the output below is checked.

```go
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
```

```text
tick 8: landed at y=0.02
tick 118: hero picked up coin
tick 200: jumped at x=9.86
tick 253: landed at y=0.82
hero at x=14.86 y=0.82 grounded=true
```

The hero drops onto the floor, walks up four 0.2 m stairs without jumping, collects the coin, jumps, and lands on the 0.8 m platform. Its feet rest 0.02 m above every surface: that gap is the controller's skin width.

## Shape casts

A shape cast sweeps a shape along a straight line and returns the first collider it touches.

```go
filter := physics.QueryFilter{ExcludeBody: hero.Body()}
hit, ok := world.SphereCast(muzzle, 0.1, aim, 40, filter)
if ok {
	// hit.Collider, hit.Body, hit.Point, hit.Normal, hit.Distance, hit.Fraction
}
```

| Call | Shape |
|---|---|
| `World.SphereCast(origin, radius, direction, maxDistance, filter)` | sphere |
| `World.CapsuleCast(origin, rotation, radius, height, direction, maxDistance, filter)` | capsule around `rotation` applied to +Y; `height` is the distance between the cap centres, as in `ColliderConfig` |
| `World.BoxCast(origin, rotation, size, direction, maxDistance, filter)` | box with full edge lengths `size` |
| `World.ShapeCast(shape, origin, rotation, direction, maxDistance, filter)` | any `QueryShape`: `SphereShape`, `CapsuleShape`, `BoxShape` |
| `Collider.ShapeCast(shape, origin, rotation, direction, maxDistance)` | against one collider, ignoring filters |

What a hit means:

- `Point` lies on the hit collider's surface and `Normal` points from that surface back toward the cast. `Fraction` is the share of `maxDistance` travelled, from 0 to 1, and `Distance` is the same in metres.
- Every collider shape can be hit: sphere, box, capsule, cylinder, cone, convex hull, triangle mesh and plane. A plane is solid below its surface. Any other collider the narrowphase supports, such as a heightfield, is hit through a generic path that samples the sweep and bisects the first touch.
- `maxDistance` must be positive and finite.
- A shape that already touches or overlaps a collider at the origin reports it with `StartOverlap` set and `Distance` 0, unless the cast moves out of it. A shape can always slide away from a surface it rests on.
- Two hits within 1e-9 m of each other go to the lower collider index, so the answer never depends on the order the world finds colliders in.

The sweep is van den Bergen's GJK ray cast against the configuration-space obstacle. Spheres and capsules enter as a point and a segment plus their radius, which keeps round shapes exact. `TestShapeCastRandomizedAgainstReference` compares 300 random casts against an independent brute-force reference (exact closest points where they exist, a separate boolean GJK otherwise) and requires agreement within 0.5 mm. `FuzzShapeCast` runs the same check under `go test -fuzz`.

## Overlap queries

```go
nearby = world.OverlapSphere(blast, 4, physics.QueryFilter{}, nearby[:0])
for _, c := range nearby {
	// every collider the sphere touches, in collider index order
}
```

`Overlap`, `OverlapSphere`, `OverlapCapsule` and `OverlapBox` append to the slice you pass and return it. Pass a reused slice and the query allocates nothing. `Collider.Overlaps` tests one collider.

## Filtering queries

`QueryFilter` decides what a raycast, cast or overlap can report. The zero value hits every solid collider and skips triggers.

| Field | Effect |
|---|---|
| `Mask` | layers the query hits; zero means all |
| `Layer` | when set, skip colliders whose `Mask` does not include this layer, so the query sees what a collider on `Layer` would collide with |
| `IncludeTriggers` | also report trigger colliders |
| `ExcludeBody` | skip every collider on this body (your own character) |
| `ExcludeCollider` | skip one collider |

`World.RaycastFiltered` is the filtered ray cast. `World.Raycast` keeps its old behaviour and hits everything, triggers included.

Queries see bodies where they are now. A kinematic platform you moved this frame is found at its new position without a step. A body-less collider you edit in place (its `Offset`, say) is seen after the next step. Queries share scratch buffers with the world, so do not run them on two goroutines at once, or while `Step` runs.

## Collision layers

Each collider belongs to the layers in its `Layer` bits and collides with the layers in its `Mask` bits. Two colliders meet only when each one's layer is in the other's mask:

```go
const (
	layerWorld  = physics.LayerDefault
	layerPlayer = 1 << 1
	layerEnemy  = 1 << 2
	layerBullet = 1 << 3
)
// A barrier that stops enemies and lets players and bullets through.
world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Width: 1, Height: 3, Depth: 8,
	Layer: layerWorld, Mask: layerEnemy})
```

Zero `Layer` means `LayerDefault` and zero `Mask` means `LayerAll`, so a config that sets neither collides with everything, as before. `Collider.SetCollisionFilter(layer, mask)` changes them later with the same defaults. The broadphase drops a pair that fails the rule before any narrowphase work, `Collide` and `CollideAll` return no manifold for it, and continuous collision and triggers ignore it.

## Triggers

A collider with `IsTrigger` set never pushes anything. Each step the world compares the trigger pairs that touch with the previous step's and reports:

| Phase | Meaning |
|---|---|
| `TriggerEnter` | the pair touches now and did not last step |
| `TriggerStay` | the pair touched last step and still does |
| `TriggerExit` | the pair touched last step and does not now |

Events come sorted by trigger collider index, then by the other collider's index. A trigger pairs with dynamic bodies and kinematic bodies (characters, moving platforms), and a trigger that rides a kinematic body also pairs with static geometry. Two static colliders never pair. Removing a collider ends its overlaps without an exit event.

You can read the events three ways:

- `World.TriggerEvents()` returns the last step's events. The slice is reused by the next step.
- `World.SetTriggerHandler(fn)` calls `fn` for each event at the end of the step that produced it. The handler may add or remove bodies; the change applies from the next step.
- `physicsys.Triggers(handlers)` is a fixed-update system that installs that handler and passes your handlers the fixed step's `game.Context`. With `Emit` set, enter and exit also become `game.Event`s of type `physics.trigger.enter` and `physics.trigger.exit`, which `Runtime.Events` lists after the frame.

Delivery depends only on fixed steps, never on how render frames split them.

## The character controller

`World.AddCharacter` creates a kinematic capsule body and returns its `*Character`. Call `Character.Step(dt, input)` once per fixed step, before the world steps; `physicsys.Characters` does that for you.

Each step does this, in order:

1. **Carry.** If the character stood on a moving body last step, it moves with that body and turns with it.
2. **Recover.** If static or kinematic geometry moved into the capsule, push the capsule out.
3. **Jump and gravity.** Start a jump if one is requested and allowed, or add gravity while airborne.
4. **Walk.** Sweep the horizontal move along the ground, climbing ledges up to the step height, following walkable slopes at full speed, and sliding along walls and steep slopes.
5. **Fall or snap.** Move vertically; while walking, look `SnapDistance` below the feet for ground and stay on it. That is what keeps a character on stairs and down slopes.
6. **Push.** Dynamic bodies the capsule ran into get an impulse; a dynamic body it stands on feels its weight.

### Configuration

Every zero value picks the default in the table.

| Field | Default | Meaning |
|---|---|---|
| `Radius` | 0.35 m | capsule radius |
| `TotalHeight` | 1.8 m | full height, cap to cap |
| `SkinWidth` | 0.02 m | gap kept between the capsule and what it touches |
| `StepHeight` | 0.3 m | tallest ledge climbed without a jump; negative disables |
| `MaxSlope` | 45° (π/4) | steepest walkable slope; steeper surfaces act as walls and the character slides down them |
| `SnapDistance` | `StepHeight` | how far below the feet a walking character looks for ground; negative disables |
| `MaxSlideIterations` | 4 (most 8) | cast-and-slide passes per move |
| `Gravity` | world gravity | per-character gravity; only the part along `Up` acts |
| `JumpSpeed` | 0 (no jumping) | launch speed; `JumpSpeedForHeight(h, g)` converts a height |
| `CoyoteTime` | 0 s | a jump still starts this long after walking off a ledge |
| `JumpBufferTime` | 0 s | a jump requested this long before landing fires on landing |
| `MaxFallSpeed` | 0 (no cap) | terminal fall speed |
| `Mass` | 80 kg | used only for pushing and weighing down dynamic bodies |
| `DisablePush` | false | treat dynamic bodies as walls |
| `Up` | +Y | the direction the character stands along |
| `Layer`, `Mask` | defaults | the capsule's collision filter, also used by its own casts |

### Input and results

`CharacterInput.Move` is the desired horizontal velocity in metres per second. `Jump` requests a jump this step: pass the button's just-pressed state, not its held state, or the character jumps again on every landing.

`Step` returns a `CharacterMove`:

| Field | Meaning |
|---|---|
| `Collisions` | `CollidedBelow`, `CollidedSides`, `CollidedAbove` flags |
| `Displacement` | how far the capsule moved, carry included |
| `Carried`, `CarriedRotation` | what the ground body moved and turned the character through; apply `CarriedRotation` to the facing to turn with a rotating platform |
| `Stepped`, `Jumped`, `Landed` | events for animation and sound |
| `Pushed` | dynamic bodies pushed this step |

Read state with `Position` (capsule centre), `FootPosition`, `Velocity`, `Grounded`, `GroundNormal` and `GroundCollider`. `SetVelocity` with an upward part lifts a grounded character off the ground, which is how you apply a launch pad or a knockback. `Teleport` moves it without sweeping. `Move(displacement)` is a plain sweep-and-slide with no gravity, stepping or snapping, for scripted motion.

### Moving platforms

A platform is a kinematic body your code moves each fixed step. List the system that moves it before `physicsys.Characters`: the character reads the platform's new pose, moves with it, and keeps its place on it. A jump off a moving platform keeps the platform's velocity in the air.

### Behaviour at the edges

- A ledge no taller than `StepHeight`, measured from the ground under the feet, is climbed; a taller one is a wall. The tests check 0.28 m (climbed) against 0.32 m and 0.34 m (blocked) with a 0.3 m step.
- At a drop, the capsule leaves the ground once its centre is about one radius past the edge.
- In an inner corner the character stops and stays still.
- The controller never steps onto a dynamic body; it pushes it instead.

## Running from game.Runtime

```go
rt := game.New(game.Config{
	Physics: world,
	Systems: []game.System{
		inputSystem,     // read the player's intent
		platformSystem,  // move kinematic platforms
		physicsys.Characters(intent, physicsys.AfterMove(onMove)),
		physicsys.Triggers(physicsys.TriggerHandlers{Enter: onEnter, Emit: true}),
	},
})
```

The runtime runs every fixed-update system and then steps the world, once per fixed step. `CharacterSystem.LastMove(c)` returns a character's move from the latest fixed step, for systems that run later in the frame. The input function must read the player's intent from the context (`ctx.Input`, `ctx.NetworkInputs` or game state), never from wall-clock time or render-frame values.

## Snapshots and netplay

`World.StateSnapshot` carries every character's state (`WorldState.Characters`) and the touching trigger pairs (`WorldState.Triggers`) next to the body states, and `World.ApplyState` restores them. `game.Runtime`'s default snapshot includes the physics state, so `Runtime.Snapshot` and `Runtime.RestoreSnapshot`, and therefore `game/netsync` prediction and reconciliation, carry characters and triggers with no extra code. After a restore, a trigger the character was inside reports `TriggerStay`, not a second `TriggerEnter`.

The tests restore a snapshot into a separately built world that ran a different history first, and require the next 300 steps to match the original bit for bit, for the bare world (`TestCharacterSnapshotRestoreIsExact`) and for a full `game.Runtime` (`TestCharactersSurviveRuntimeSnapshotRestore`).

One limit: the snapshot does not hold the solver's warm-start contact cache. Characters, kinematic bodies and triggers restore exactly either way. Dynamic bodies resting on each other restore exactly only with `WorldConfig.DisableWarmStart` set; with warm starting on, a restored stack can differ in the last bits, which a rollback client sees as a small correction.

## Determinism

The package is marked `//gosx:deterministic`: every float product that feeds an addition is rounded explicitly, so arm64's fused multiply-add cannot change results (see [deterministic-simulation.md](deterministic-simulation.md)). `TestCharacterDeterminismGolden` runs four characters over mesh terrain, stairs, ramps, a lift, a spinning platform, pushable crates and triggers for 1,000 steps and hashes every body, character and trigger event each step. The digest matches on linux/amd64 (GOAMD64 v1 and v3), darwin/arm64 and js/wasm.

Your own scene and input code needs the same care when a server and a client must agree: use `det.Sin` and the other `det` functions, and write `float64(a*b) + c`, not `a*b + c`.

## Cost

Measured with `go test -bench` on an Apple M4 Pro (darwin/arm64), median of three runs, with 0 allocations per operation for each:

| Operation | Time |
|---|---|
| `Character.Step`, walking on mesh terrain | 4.2 µs |
| `Character.Step`, pushing into a wall | 2.3 µs |
| capsule cast down onto mesh terrain | 1.6 µs |
| box cast across a level of 150 boxes | 1.6 µs |
| capsule overlap | 1.8 µs |
| filtered 30 m raycast | 2.5 µs |
| sphere cast across the level | 8.4 µs |

The level for the query numbers is a 32 x 32 cell mesh terrain, 150 static boxes, 40 dynamic props, 4 kinematic platforms and 8 triggers (`queryBenchWorld` in `physics/query_bench_test.go`). One whole fixed step of the determinism scene (four characters, the world step, triggers) takes 31 µs (`BenchmarkGoldenSceneStep`).
