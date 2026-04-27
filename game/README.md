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
assets.MustRegisterAll(
    game.WithPreload(game.GLB("hero", "/assets/hero.glb")),
    game.Texture("hero-albedo", "/assets/hero.png"),
    game.Audio("hit", "/assets/hit.ogg"),
)
```

Audio assets become a client audio manifest on `Runtime.EngineConfig()`, and
systems can emit playback events without hand-written browser glue:

```go
ctx.PlayAudio("hit", game.AudioPlayback{Bus: "sfx", Volume: 0.8})
```

For 60Hz/120Hz loops, prefer reusable query buffers in hot systems:

```go
var transforms []game.ComponentRef[game.Transform]
transforms = game.QueryInto(ctx.World, transforms[:0])
```

Use `rt.Mount(ctx, fallback)` from a server page state/runtime, or
`ctx.Engine(rt.EngineConfig(), fallback)` directly.
