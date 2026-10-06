# Browser controllers

Use `server.PageRuntime.Controller(controller.Config{...})` to bind browser input
to shared signals and app intents without an island or a lifecycle script.
Controllers are disposed with the page. They do not render UI.

## Project events into typed intents

`Projection.Value` supplies a Go value with the intent's JSON shape.
`Projection.Fields` maps top-level JSON fields to dot paths under `event`,
`inputs`, or `drag`. `Projection.When` requires scalar equality at
source paths. Missing fields suppress the output. No Go or JavaScript expressions
are evaluated by these paths.

```go
type PlayIntent struct {
    Kind     string `json:"kind"`
    Tile     string `json:"tile"`
    Revision int    `json:"revision"`
}

config := controller.Config{
    Root: "#game-ui",
    Inputs: []controller.Input{{Name: "view", Signal: "$view"}},
    Outputs: []controller.Output{{Name: "intent", Signal: "$intent", Event: "app:intent"}},
    Events: []controller.Event{{
        Type: "gosx:scene3d:input", Target: "#board", Output: "intent",
        Project: &controller.Projection{
            Value: PlayIntent{Kind: "play"},
            When: map[string]any{
                "event.detail.kind": "pick",
                "event.detail.input.type": "select",
            },
            Fields: map[string]string{
                "tile": "event.detail.input.targetID",
                "revision": "inputs.view.revision",
            },
        },
    }},
}
runtime.Controller(config) // runtime is a *server.PageRuntime
```

The shared signal receives `PlayIntent`'s shape directly. The bubbling
`app:intent` event receives the same value in `detail`; use either output or both.
An app's authenticated action or hub adapter can consume that event. The server
still validates the intent and revision. Event-only outputs may omit `Signal`.
Unprojected bindings retain the existing controller event envelope.

Event sources include `event.detail`, key/code/modifier fields, pointer ID and
client coordinates, and `event.target.dataset`, ID, name, value, checked, and
text. Controller inputs are keyed by `Input.Name` (or its signal name).
Projection paths read own properties and reject prototype access. Objects and
arrays are copied as JSON values.

Use the existing `game/gamepad` package for controller polling and pressed edges.

## Drag to DOM or scene targets

```go
controller.DragBinding{
    Source: "[data-tile]", Output: "intent", CancelOutput: "$dragCancel",
    Targets: []controller.DropTarget{{
        Target: "#board", Scene: true, HitIDs: []string{"left-end", "right-end"},
        RequestOutput: "$dropRay", ResultSignal: "$dropHit",
    }},
    Project: &controller.Projection{
        Value: PlayIntent{Kind: "play"},
        Fields: map[string]string{
            "tile": "drag.source.dataset.tile",
            "revision": "drag.inputs.view.revision",
        },
    },
}
```

Put bindings in `Config.Drags`. DOM targets use only `DropTarget.Target`.
Scene targets resolve the pointer against the current mounted canvas, viewport,
and camera. Without `RequestOutput` and `ResultSignal`, they use the renderer's
browser pick. Set both to use the existing native scene query APIs:

```go
// Inside an engine/wasm factory, return the subscription as its Handle.
subscription, err := wasm.SubscribeSignal[controller.PickRequest](ctx, "$dropRay",
    func(request controller.PickRequest) {
        hit, ok := scene.RaycastGraph(graph, request.Ray, scene.PickableOnly())
        result := controller.PickResult{RequestID: request.RequestID}
        if ok {
            result.Hit = &hit
        }
        _ = ctx.SetSignal("$dropHit", result)
    })
return subscription, err
// scene.NewSceneAccelerator(graph).Raycast(request.Ray) uses the same result type.
```

The Scene3D mount receives `gosx:scene3d:pick-request` and emits
`gosx:scene3d:input` with `detail.kind == "ray"` and a correlated ray/pick in
`detail.input`. `PickRequest.Ray` has the same JSON representation as `scene.Ray`.
`wasm.SubscribeSignal[T]` decodes subsequent browser writes into a Go value;
`ctx.SetSignal` writes JSON back into the browser signal runtime. Dispose the
subscription with the engine handle. Native signal constructors remain
request-local; the context methods provide the engine/VM bridge.

A native result must retain `RequestID`; a nil `Hit` is a miss. The controller
ignores stale and cancelled requests. Native results expire after `TimeoutMS`
(default 1000). The mount bridge is detached on replacement and disposal.

`drag.source` contains the source element's event target fields, including
`dataset`. `drag.inputs` snapshots controller inputs at pointer-down; use it for
revision checks. Successful drops include `drag.hit` (a scene hit or null),
`drag.target` (the matched selector), pointer ID and client coordinates.
`StartOutput`, `MoveOutput`, and `CancelOutput` publish optional phase records.
Taps below `ThresholdPX` (default 4) produce no drop. Secondary pointers are
ignored; cancellation, lost capture, blur, remount, and disposal release the
gesture. Give draggable sources `touch-action: none` for touch input.

## Own modal focus

```go
controller.FocusOwner{
    Target: "#pause-menu", OpenSignal: "$menuOpen",
    InitialFocus: "button", ReturnFocus: "#pause-button",
}
```

Put owners in `Config.Focus`. A true boolean `OpenSignal` opens the owner. The
runtime traps Tab in both directions, redirects outside focus, and makes
background branches inert. Escape sets the signal to false. Closing restores
`ReturnFocus`, or the element focused before opening, and the original inert and
tabindex state. Nested owners give the latest modal focus and return to the
previous one. Provide the modal markup and accessible dialog semantics in GoSX;
the controller owns interaction and disposal.

The input runtime is a separate hashed chunk, fetched only when one of these
contracts or storage is configured. Storage uses the same optional chunk to
keep the default bundle within its existing size budget. Ordinary event, key,
timer, and resource controllers retain their existing load path.
The `controllers` runtime exclusion role excludes both controller chunks.
