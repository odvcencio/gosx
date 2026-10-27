# GoSX Game Runtime

`game` is the first-class runtime layer for interactive simulations, games,
3D web apps, and scientific visualizations.

The package composes existing GoSX primitives instead of replacing them:

- `engine` still owns browser/native surfaces and capability gates.
- `scene` still owns Scene3D authoring and canonical IR.
- `island` still owns hydrated DOM islands and headless compute islands.
- `physics` still owns rigid-body simulation.
- `hub` and `sim` still own realtime transport and server-authoritative ticks.
- `game` owns fixed-step orchestration, ECS-style state, input actions, assets,
  and the bridge between those packages.

Browser engines that only present server snapshots can use `game/host` for
typed gamepad snapshots and animation-frame scheduling. This small leaf package
does not import `game.Runtime`, server hubs, HTTP/TLS, or the GoSX compiler.
`game/gamepad` and `game/loop` retain their existing adapter types as aliases,
so applications that use the input mapper or fixed-step driver remain compatible.

Minimal pattern:

```go
rt := game.New(game.Config{
    Profile: game.ScientificProfile(),
    Scene: func(ctx *game.Context) scene.Props {
        return scene.Props{
            Width:  960,
            Height: 540,
            Graph: scene.NewGraph(
                scene.Mesh{
                    ID:       "sample",
                    Geometry: scene.SphereGeometry{Radius: 1},
                    Material: scene.StandardMaterial{Color: "#77c6ff"},
                },
            ),
        }
    },
})
```

For full-stack web pages where 3D is a primary interface surface but not a
game, start from `game.Web3DProfile()`. It keeps Scene3D, pointer input,
keyboard inspection, fetch, and storage in the browser capability contract
without implying gamepad/audio requirements.

Use compute islands for the client-side controller tier around a Scene3D or game
surface: route data, hubs, and shared signals stay in the GoSX page runtime,
while the compute island runs headless island VM code without a DOM root.

For deterministic versus games, start from `game.FightingProfile()` and wire the
runtime through `game.NewRunner`. If the simulation state is already JSON,
`sim.Options{StateEncoding: sim.StateEncodingJSON}` streams it without base64
expansion.

Use the asset constructors to keep model/audio/texture manifests structured:

```go
assets := game.NewAssets()
if err := assets.RegisterAll(
    game.WithVariant(
        game.WithPreload(game.GLB("hero", "/assets/hero.glb")),
        game.VariantCapabilities(
            game.VariantCompression(game.Variant("/assets/hero.meshopt.glb"), "meshopt"),
            "webgl2", "meshopt",
        ),
    ),
    game.WithVariant(
        game.Texture("hero-albedo", "/assets/hero.png"),
        game.VariantCapabilities(
            game.VariantContentType(
                game.VariantCompression(game.Variant("/assets/hero-albedo.ktx2"), "ktx2"),
                "image/ktx2",
            ),
            "webgpu", "ktx2",
        ),
    ),
    game.Audio("hit", "/assets/hit.ogg"),
); err != nil {
    log.Fatal(err)
}
```

Use `assets.ManifestFor("webgpu", "ktx2")` or `assets.ResolveFor(...)` when
you want the server to collapse a manifest to the best representation for a
known capability tier. Otherwise the base `Manifest()` keeps variants so the
client/runtime can choose later.

Audio assets become a client audio manifest on `Runtime.EngineConfig()`, and
systems can emit playback events without hand-written browser glue:

```go
ctx.PlayAudio("hit", game.AudioPlayback{Bus: "sfx", Volume: 0.8})
ctx.PlayAudio("thud", game.AudioAt(game.Vec3{X: 2, Y: 1, Z: -4}, game.AudioPlayback{
    Bus:           "sfx",
    Volume:        0.9,
    RefDistance:   2,
    MaxDistance:   64,
    RolloffFactor: 0.75,
}))
```

For 60Hz/120Hz loops, prefer reusable query buffers in hot systems:

```go
var transforms []game.ComponentRef[game.Transform]
transforms = game.QueryInto(ctx.World, transforms[:0])
```

Use `rt.Mount(ctx, fallback)` from a server page state/runtime, or
`ctx.Engine(rt.EngineConfig(), fallback)` directly.

## Time control and multi-rate systems

`Clock` (and `Runtime`) can pause and scale game time:

```go
rt.SetPaused(true)       // frames still advance; no fixed steps, no game time
rt.SetTimeScale(2)       // double speed; 0 stops time; NaN/negative ignored
```

`Frame.Delta` is game time (clamped real delta times the scale, zero when
paused); `Frame.RealDelta` is the unscaled value; `Frame.Steps` counts fixed
steps since the clock started. Sub-nanosecond remainders of scaled deltas are
carried, so a scale of 0.3 does not drift. `Snapshot` saves the pause flag and
scale; older snapshots leave them alone. It also saves what the clock holds
between frames (the part of a step and the sub-nanosecond fraction not yet used),
so a restored runtime takes its next step on the same frame as one that never
stopped.

For a simulation that runs fast, use coarse steps and multi-rate systems, not
more substeps. One game year per real minute is a scale of 525,600. With
`FixedStep: time.Hour`, that is about 2.4 steps per frame at 60 fps:

```go
rt := game.New(game.Config{
    FixedStep: time.Hour, // MaxDelta defaults to at least FixedStep*MaxSubsteps
    Systems: []game.System{
        game.Func("weather", game.PhaseFixedUpdate, weather),            // every hour
        game.Every(24, game.Func("economy", game.PhaseFixedUpdate, econ)), // daily
        game.EveryInterval(30*24*time.Hour, game.Func("census", game.PhaseFixedUpdate, census)),
    },
})
rt.SetTimeScale(525600)
```

`Every(n, sys)` runs a system every n ticks (fixed steps for `PhaseFixedUpdate`,
frames otherwise) using `Context.Tick`, so the schedule is the same on every run.
On a run, `ctx.Delta` holds the game time gathered since the last run.
`EveryInterval(d, sys)` runs on accumulated game time instead. Frames cap at
`MaxSubsteps` fixed steps; at extreme scales raise `MaxSubsteps` or the step
size, or game time falls behind the scale.

`Snapshot` saves the game time each `Every` and `EveryInterval` wrapper has
gathered, and `Restore` gives it back to the wrapper with the same system name,
so a restored run fires on the same ticks with the same `ctx.Delta` as a run that
never stopped. A system added since the snapshot starts from zero. A wrapper
hidden inside your own `System` type is not found, so its time is not saved.

## Saving the world

`World` keeps its columns private, so register each persistent type under a
stable name with a `SaveCodec`:

```go
codec := game.NewSaveCodec()
_ = game.RegisterComponent[game.Transform](codec, "transform")        // JSON
_ = game.RegisterComponent[Treasury](codec, "treasury", game.ValueCodec[Treasury]{
    Marshal: encodeTreasury, Unmarshal: decodeTreasury,                // custom
})
_ = game.RegisterResource[Calendar](codec, "calendar")

rt := game.New(game.Config{Snapshot: codec.SnapshotCodec()})
save, err := codec.ExportWorld(rt.World()) // game.WorldSave, JSON-encodable
err = codec.ImportWorld(rt.World(), save)  // replaces the world; atomic on error
```

Only registered types are saved; entity IDs, names and the ID counter are kept.
Saves are byte-identical for the same world. `SkipUnknown` loads saves from a
newer build, and `IncludeWorldInState` adds the world to the per-tick `State`
(off by default because the world can be large). With `SnapshotCodec`,
`Runtime.Snapshot` and `Runtime.Restore` (and so `sim.Runner.Restore` and
spectator joins) carry the world.

`Restore` is the `sim.Simulation` method and has no result, so it ignores a
snapshot it cannot read without a word. Call `RestoreSnapshot` to get the error:

```go
if err := rt.RestoreSnapshot(saved); err != nil {
    return fmt.Errorf("load save: %w", err) // rt and its world are unchanged
}
```

It refuses an empty or malformed snapshot, a `version` newer than
`game.RuntimeStateVersion`, and a clock or world that does not fit, and it
changes nothing when it does. Snapshots now carry `"version": 2`, which adds the
clock remainder and the cadence times above. Version 1 snapshots, written before
the field existed, still load: they start the clock remainder from zero and leave
`Every` and `EveryInterval` wrappers as they are. `State`, which `sim.Runner`
broadcasts every tick, keeps the version 1 shape and stays small.
