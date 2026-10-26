# Powered vessels, decks and cutaway

Run a large powered ship in Go, show it in Scene3D, let people walk its decks,
and open it up like a dollhouse. Three opt-in pieces work together:

| Piece | Where | What it does |
|---|---|---|
| `vessel` | Go package, deterministic | Hull, engines, rudder, waves, decks, stairs and lifts |
| `Vessel.ExternalPose` | Scene3D | Go sends the pose; the browser runs no sailing physics |
| `Vessel.Decks`, `Vessel.Cutaway` | Scene3D | Walk many decks; hide or ghost what is above one |

A sailing `scene.Vessel` (wind, sails, helm) behaves as before. Nothing here
changes it unless you set the new fields.

## The ship model

`vessel` is marked `//gosx:deterministic`. It gives the same bits on js/wasm,
amd64 and arm64, so replays, ghost runs and shared worlds can use it. See
`docs/deterministic-simulation.md` for the rules it follows.

```go
sea := vessel.NewSea(vessel.SeaConfig{WaveHeight: 3, WaveLength: 70, WindDirection: 25})
ship := vessel.NewShip(vessel.Liner(), 0, 0, 0, sea)

// Every tick, with one constant dt:
ship.Step(vessel.Input{Power: 0.8, Heading: 1.2, Stabilizers: true}, 1.0/60)
pose := ship.Pose()          // X, Y, Z, Heading, Pitch, Roll
speed, watts := ship.Speed(), ship.PowerDrawn()
```

Inputs per tick: a power setting from -1 (full astern) to 1, a heading command in
radians, and optional stabilizers. Set `Input.RudderOverride` and `Input.Rudder`
to steer by hand. Outputs: pose, speed, thrust and power drawn.

Axes match the sailing vessel: heading 0 faces -Z, positive heading turns toward
-X, positive pitch lifts the bow, positive roll lifts the starboard side.

### Hull classes

`vessel.Expedition()`, `vessel.Liner()` and `vessel.Megaship()` return a
`HullClass`. Change any field to make your own.

| Class | Length | Beam | Decks | Mass | Power | Service speed |
|---|---|---|---|---|---|---|
| expedition | 125 m | 21 m | 7 | 8,400 t | 8 MW (pods) | 8.2 m/s (15.9 kn) |
| liner | 290 m | 36 m | 14 | 62,000 t | 48 MW (shafts) | 12.0 m/s (23.3 kn) |
| megaship | 360 m | 48 m | 20 | 120,000 t | 62 MW (pods) | 11.8 m/s (22.9 kn) |

### Power and speed

Resistance grows with the square of speed, so power grows with its cube:

```
R(u) = k*u*|u| + kLin*u
P(u) = R(u)*u / efficiency
k    = efficiency * InstalledPower / ServiceSpeed^3     kLin = 0.04*k*ServiceSpeed
```

Half power gives about 79% of top speed; a 10% speed cut saves about 27% of the
power. `HullClass.SteadySpeed(p)` and `PowerForSpeed(u)` give exact values.
Measured steady speeds (m/s) against the cube-root law:

| Power | expedition | liner | megaship |
|---|---|---|---|
| 0.25 | 5.06 (law 5.17) | 7.40 (law 7.56) | 7.28 (law 7.43) |
| 0.50 | 6.40 (law 6.51) | 9.37 (law 9.52) | 9.21 (law 9.37) |
| 1.00 | 8.09 (law 8.20) | 11.84 (law 12.00) | 11.64 (law 11.80) |

The small gap below the law is the linear term. The engines ramp toward the
setting (a megaship takes 75 s from idle to full power), and a ship accelerates
slowly because of its mass: a megaship at full power is under 2 m/s after 60 s.

### Turning and stopping

Steering is a first-order Nomoto model: at steady state the turning radius is
`Length / (SteerGain * rudder)`. A rudder or pod angle slews at a limited rate,
the autopilot uses half of the available angle, and the hull drifts outward,
loses speed to rudder drag and heels outward. `vessel.TurningCircle` and
`vessel.StoppingDistance` measure a class in calm water (dt 0.5 s):

| Class | Tactical diameter at 0.6 power | 180-degree turn | Crash stop from full power |
|---|---|---|---|
| expedition | 319 m (2.6 L) | 92 s | 597 m (4.8 L) in 110 s |
| liner | 946 m (3.3 L) | 181 s | 1,820 m (6.3 L) in 258 s |
| megaship | 1,138 m (3.2 L) | 220 s | 2,486 m (6.9 L) in 363 s |

These come from simplified formulas tuned to plausible numbers. They are not
validated against sea-trial data.

### Waves, roll and pitch

Each step samples the water at five points under the hull and drives heave,
pitch and roll through second-order springs with the class's natural periods
(roll 9 to 18 s). A long hull averages short waves out. Stabilizers add roll
damping and cut the wave and turn forcing; they need speed to work and draw
power (`StabilizerPower`).

`vessel.Sea` builds waves from the same six-component table as the Scene3D
ocean. It keeps only the vertical term, and it stays within 35% (rms) of
`scene.OceanQuery`. Any type with `Height(x, z, t float64) float64` works as a
`WaveField`; `vessel.WaveFunc` adapts a function. `scene.OceanQuery` uses the
platform `math` library, so wrapping it gives up bit-identical results.

### Saving and replaying

`ship.AppendState(dst)` writes the whole state as 128 bytes of float bits;
`ship.RestoreState(b)` loads it. A restored ship continues bit for bit.

### Determinism results

`vessel/testdata/parity-*.json` hold SHA-256 traces of three scripted 30-minute
voyages (one per class: power changes, course changes, hard-over rudder,
crash stop, stabilizers on and off, rough sea), the deck layouts and routes, and
the 60-second liner storm that the benchmark uses. `make test-det-parity` runs
them on amd64 (also with `GOAMD64=v3`), js/wasm and, when `qemu-aarch64` is
installed, linux/arm64. `make test-det-parity-arm64-remote` with
`PARITY_ARM64_HOST` set runs them on a real Apple Silicon Mac. All platforms
agree: the liner storm trace digest is
`cf4d145ea783422ca6b03ee8ec4eba4896b1ed103dce19c2853419343ee93907` on amd64,
amd64 `GOAMD64=v3`, js/wasm and darwin/arm64. `make lint-deterministic` checks
the package.

## Show a Go-driven ship

```go
scene.Props{
    Vessel: &scene.Vessel{
        NodeID: "liner", ExternalPose: true,
        Position: scene.Vec3(0, 0, 0), // used until the first frame arrives
        Length: 290, Beam: 36,
    },
    Walk: &scene.Walk{},
}
```

Each tick, send the pose:

```go
err := scene.DispatchVesselPose("scene-mount-id", scene.VesselPoseFrame{
    Vessels: []scene.VesselPose{{
        ID: "liner", Time: ship.State.Time,
        X: pose.X, Y: pose.Y, Z: pose.Z,
        Heading: float32(pose.Heading), Pitch: float32(pose.Pitch), Roll: float32(pose.Roll),
        Speed: float32(ship.Speed()),
    }},
})
```

The target is the mount element's ID. `DispatchVesselPose` returns an error until
the vessel has mounted, so ignore errors for the first ticks. In the browser, the
vessel stops integrating physics and shows the pose, blended between ticks. The
model parts, the walker, the wake, `InstancedGLBMesh{ParentNode: "liner"}`
crowds and `InstancedMesh{ParentNode: "liner"}` batches (windows, rails,
lifeboats) follow the pose as for a sailing vessel. Instance positions in a
parented batch are in the ship's own frame. There is no helm, navigation or
wind. Set `VesselPose.Snap` for the first pose and for teleports.

The frame (`GSV1`, little-endian) is `"GSV1"`, a `u16` ship count, then per ship
a flags byte, a `u16` ID length, the ID, `f64` time, `f64` x, y, z and `f32`
heading, pitch, roll, speed. One ship with a 5-byte ID takes 62 bytes. The
browser blends from the pose on screen to the newest tick over the measured gap
between ticks, and holds the last pose when a tick is late. It drops a tick whose
time is not newer, and cuts on `Snap` or a 500 m jump.

## Decks

```go
layout := vessel.StandardLayout(vessel.Liner()) // 14 hull-shaped decks, stairs and lifts
scene.Vessel{ /* ... */ Decks: scene.VesselDecksFromLayout(layout) }
```

A `vessel.Layout` has decks (`Walk` polygons minus `Holes`, at a height `Y`),
stair flights (`Ramps`) and portals. Coordinates are ship-local: X toward
starboard, Y up from the waterline origin, Z toward the stern. Build a deck from
a few numbers with `vessel.DeckPlan{Name, Y, Length, Beam, Voids, ...}.Build()`;
`StandardLayout` does that for every deck of a class, narrows the lowest and
highest decks, opens an atrium through the middle decks, and adds two stair
towers and three lift banks.

Queries (all in ship-local space, all deterministic):

| Call | Answer |
|---|---|
| `layout.DeckAt(x, y, z)` | Deck under a point: the highest deck at most `StepHeight` (0.5 m) above the feet that has the spot walkable, or -1 |
| `layout.FloorAt(x, y, z)` | Walking height, counting stair flights |
| `pose.ToWorld(local)`, `pose.ToLocal(world)` | Convert with roll, pitch and heading |
| `layout.DeckAtWorld(pose, world)` | The deck under a world point on a rolling ship |
| `layout.Route(from, x, z, to, x, z, opts)` | Quickest hops by stairs and lifts, with walking time and optional queueing |
| `layout.NewFlow()`, `flow.Admit(...)` | Capacity: stairs pass persons per second, a lift takes a car at a time |

A portal is a stack of stops at one spot. Stairs cost `PerLevel` seconds per
level (at most `MaxLevels` per trip); a lift costs `Dwell + PerLevel` per level.
On the liner a 13-level trip takes 197 s by lift and 301 s by chained stairs.

In the browser, `Vessel.Decks` replaces the single walk rectangle. A walker
stands on the highest deck at or just above their feet, measured in ship space,
so a rolling ship keeps people on the deck they chose. Stair flights are walkable
ramps with a hole in the deck above. Lifts are simulation only: move a player
between decks yourself (a game event) when they use one.

## Cutaway

```go
Vessel{ /* ... */ Decks: decks, Cutaway: &scene.VesselCutaway{Enabled: true, Deck: 8, Above: "fade"} }

// later, from Go:
scene.DispatchVesselCutaway("scene-mount-id", "liner", scene.VesselCutaway{
    Enabled: true, Deck: 5,
    Section: &scene.VesselSection{Axis: "x", Position: 0, Side: 1}, // slice off the starboard half
})
```

`Deck` is the highest deck shown, as an index into `Vessel.Decks` (or set `Height`
in ship metres). Everything above that deck's ceiling is cut: model parts are
hidden (`Above: "hide"`, the default) or ghosted with a screen-door pattern that
keeps `FadeAlpha` of the pixels (`"fade"`, default 0.15). `Section` slices the
hull along `x` (across the beam) or `z` (along its length); the cut is open, not
capped. Both planes are fixed to the ship, so they roll and pitch with it.

A crowd whose `ParentNode` is the vessel loses every agent on the cut side. The
renderer collapses those agents in the vertex shader for both the color and shadow
passes, so they cost no pixels and cast no shadows. The clipped model parts stop
casting shadows while the cutaway is on. In the browser pixel check, a ship cut to
deck 1 draws the same pixels, exactly, as a scene that omits everything above
deck 1, level or rolled, with and without shadows.

Limits:

- WebGL only. On WebGPU the ship stays whole and the mount sets
  `data-gosx-scene3d-cutaway="unsupported"`. The seam is documented at the top of
  `client/runtime/scene3d/vessel-cutaway.ts`: a renderer reads `object.cutaway`
  (clip planes for model parts) and `object.cutawayCrowd` (the first eight floats,
  for crowds), then adds its name to `BACKENDS`.
- Agents are culled by their origin, so an agent on a stair flight disappears as
  it crosses the cut.
- Detail-layer materials draw uncut in the crowd path.

## Cost

`client/js/scene3d-vessel-powered.bench.mjs` runs the liner with 2,900 parented
agents, 5% changing per 10 Hz tick, on poses that Go produced for a storm (max
roll 1.5 degrees, max pitch 1.0 degree). Node, no GPU, median of five runs:

| Cutaway | Wire bytes per tick | Decode and apply per tick | Ship, decks and walker queries per frame | Mean per frame (60 Hz) |
|---|---|---|---|---|
| off | 13,571 (62 + 13,509) | 0.73 ms | 0.030 ms | 0.66 ms |
| on | 13,571 | 0.68 ms | 0.032 ms | 0.62 ms |

The Go encoders write a 62-byte pose frame and a 13,514-byte partial motion
frame for the same shape. A cutaway command is 40 bytes of JSON. The first frame,
which restates all agents, takes 7 to 8 ms once.

Checks you can run:

```sh
go test ./vessel ./scene                                        # model, decks, wire
node --test client/js/scene3d-vessel-*.test.js                  # decoder, decks, cutaway
node client/js/scene3d-vessel-powered.bench.mjs                 # cost
SCENE3D_TOOLS_DIR=<dir> SCENE3D_BROWSER=<chromium> \
  node scripts/scene3d-vessel-cutaway.mjs [shadows]             # pixels in a real browser
```

Primitive `InstancedMesh.ParentNode` batches use the same named-parent lookup
in WebGL2, WebGPU, native previews and `AtTime` picking. An explicit parent
owns the batch coordinate space, including when the batch is declared inside
another group; that structural group is not baked a second time. Exact object
or model IDs take priority over imported part prefixes. A retained `Group` ID
can carry a batch directly. Missing parents temporarily leave it in world
space; strict SceneIR validation reports an unresolved primitive-batch parent.
