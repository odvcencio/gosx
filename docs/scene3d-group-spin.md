# Hierarchical spin

`Group.Spin` is angular velocity in radians per second, just like `Mesh.Spin`.
The whole assembly rotates around the group's local origin. Each child keeps
its own position, rotation, scale and optional spin.

```go
scene.Group{
    ID: "fan-rotor",
    Position: scene.Vec3(0, 2, 0),
    Spin: scene.Rotate(4, 0, 0),
    Children: []scene.Node{
        fanHub(),
        fanBlades(),
        fanSpinner(),
    },
}
```

Place off-center parts in the group's local coordinates. Their positions orbit
the shared pivot; their own `Mesh.Spin` remains rotation around their own origin.
Nested groups compose from child to parent, including nonuniform and negative
scale. Exact affine matrices retain shear instead of approximating it as Euler
angles and scale.

Animation uses scene-clock seconds. WebGL and WebGPU consume the same evaluated
parent matrices, and browser picking uses those rendered positions. Native graph
queries accept `scene.AtTime(seconds)`. An accelerator can sample a pose with
`UpdateAtTime(graph, seconds)` or an explicit `AtTime` query; sampling a different
time rebuilds its bounds. Queries without a time keep the accelerator's last pose.

Spinning groups get a retained transform record and a stable motion target ID.
`SceneIR.SpinMotionTimeline()` includes their spin tracks. `SetGroupsCommand`
replaces the retained hierarchy; existing transform commands can update a named
group without replacing child geometry. Ordinary motion bindings can target the
group's position, rotation or scale. Use explicit IDs for assemblies referenced
by timelines, picking or live updates.

A group with zero spin keeps the existing static lowering path, unless it is
inside a retained animated ancestor. Scenes with no animated groups add no
`groups` collection or hierarchy parent references. Scene-wide configuration nodes, such
as environment, water simulation and physics-world configuration, keep their
existing scene-wide semantics.
