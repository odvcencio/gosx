# Anchored DOM overlays

Use `scene.Overlay` to place GoSX content at a world position, or attach it to
an object that moves. Bubbles, seat cards, and interaction hints stay readable
above the canvas and use ordinary HTML, CSS, and accessibility attributes.
Give the Scene3D mount `position: relative` so projected coordinates use its
origin. The overlay supplies its placement styles; no projection CSS is needed.

```go
import (
    "m31labs.dev/gosx"
    "m31labs.dev/gosx/scene"
)

graph := scene.NewGraph(
    scene.Mesh{
        ID: "seat-south",
        Position: scene.Vec3(0, 0, 2),
        Geometry: scene.BoxGeometry{Width: .5, Height: .2, Depth: .5},
    },
    scene.Overlay{
        ID: "turn-bubble",
        Target: "seat-south",
        Position: scene.Vec3(0, .6, 0),
        Width: 1.8, Height: .5,
        OffsetY: -8,
        ClassName: "turn-bubble",
        Content: gosx.El("span",
            gosx.Attrs(gosx.Attr("role", "status"), gosx.Attr("aria-live", "polite")),
            gosx.Text("Your turn")),
    },
)
```

With `Target`, `Position` is a local offset. The runtime applies the object's
current translation, rotation, scale, spin, and drift every time the scene
renders, then projects through the current camera. Object update commands and
animation therefore move the overlay too. A missing or removed target hides
the content; recreating that ID makes it visible again. The runtime never
writes the projected position back into the authored offset.

Without `Target`, placement uses world coordinates and inherits a surrounding
`scene.Group` transform. `Width` and `Height` are world units (defaults: 2 and
.72), so apparent size changes with camera depth. `OffsetX` and `OffsetY` are
CSS pixels. Content is centered on the projected point. `ClassName` styles the
overlay container; content nodes may carry their own classes and attributes.

Set `Interactive: true` for links, buttons, and inputs to receive pointer
events. It defaults to false so decorative content passes gestures through to
the canvas. `Occlude: true` uses the existing scene overlay occlusion test;
`Priority` controls stacking. Behind-camera content is hidden. Hidden content
receives `aria-hidden`; author status or live-region semantics on the content.

Change `Content` by producing a new graph and applying `scene.DiffCommands`.
Content replacement uses the existing DOM overlay lifecycle. Compiled island
mounts have their own lifecycle; rendering an island placeholder as content
does not create an independent island mount.

`Validate` and `Surface` return errors for nonfinite placement, negative
dimensions, oversized identifiers, empty content, and HTML above 64 KiB.
Graph lowering omits invalid overlays. Normal GoSX nodes escape text and
attributes. `gosx.RawHTML` keeps its usual trusted-markup contract.

Existing `scene.HTML` retains its static target placement by default. Set
`FollowTarget: true` to opt that lower-level API into the same target following,
including texture or perspective surfaces. This adds one optional
`targetOffset` tuple to SceneIR and uses the existing lazy Scene3D runtime;
there is no separate scheduling loop or texture allocation for DOM overlays.
