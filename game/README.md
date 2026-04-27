# GoSX Game Runtime

`game` is the first-class runtime layer for interactive simulations, games, and
scientific visualizations.

The package composes existing GoSX primitives instead of replacing them:

- `engine` still owns browser/native surfaces and capability gates.
- `scene` still owns Scene3D authoring and canonical IR.
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

Use `rt.Mount(ctx, fallback)` from a server page state/runtime, or
`ctx.Engine(rt.EngineConfig(), fallback)` directly.
