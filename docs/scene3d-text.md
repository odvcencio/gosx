# Scene3D text

Use `scene.Text3D` for scores, chalkboard writing, and labels that belong on a
plane inside the scene. It accepts plain text and lowers to the existing HTML
texture surface, sharing perspective, depth, post-processing, font capture,
texture caching, and readable DOM fallback.

```go
score := scene.Text3D{
    ID: "score", Text: "Partners\n12", Color: "#f0eee6",
    Font: "48px monospace", Align: "center",
    Position: scene.Vec3(0, 1.2, -2),
    Rotation: scene.Euler{X: -math.Pi / 2},
    Width: 2, Height: 0.5,
}
props := scene.Props{
    Camera: scene.PerspectiveCamera{
        Position: scene.Vec3(0, 1.2, 5), FOV: 60,
    },
    Graph: scene.NewGraph(score),
}
```

The plane starts in XZ, matching GoSX's HTML texture surfaces. A rotation of
`-math.Pi/2` around X makes it upright in XY. Positions and `Width`/`Height` use
scene units. Groups apply translation and rotation; surface dimensions describe
the world-space plane. `Target` uses the existing surface anchoring contract.

`Font` is a CSS font shorthand and defaults to `48px sans-serif`. Use a managed
font family from your page's font styles for a handwritten appearance. The
existing texture manager waits for font faces, captures their declarations,
and refreshes the texture when they load. Font asset URLs use the page's normal
managed paths, including embedded asset prefixes.

Text is escaped, including its accessible label. Newlines and wrapping are
preserved; content outside the plane is clipped. `Align` accepts `left`,
`center`, and `right`. `LineHeight` is a unitless multiplier, defaulting to 1.2.
The surface consumes no pointer events. The renderer retains a readable DOM
mirror and reports the existing texture fallback if rasterization is unavailable.

Update scores through existing scene diffs:

```go
previous := props.Graph.SceneIR()
score.Text = "Partners\n13"
next := scene.NewGraph(score).SceneIR()
commands := scene.DiffCommands(previous, next)
```

Send those commands through your existing authenticated response and apply them
with `window.__gosx.scene3d.dispatchCommands(target, commands)`. The texture
manager rasterizes changed content and reuses static content between updates.
Use stable IDs so updates replace the intended surface.

The default backing texture is 512 by 128 CSS pixels with a 262,144-pixel cap;
the manager can increase raster resolution for device pixel ratio within that
cap. `TextureWidth`, `TextureHeight`, and `MaxTexturePixels` override it. Each
dimension is at most 2,048; the cap is at most 4,194,304 pixels, and must cover
the base texture. Text is limited to 4,096 characters. `Validate()` and
`Surface()` return authoring errors; graph lowering omits invalid nodes.

This API adds no browser runtime bytes or feature chunk. It uses the existing
HTML texture renderer already carried by a Scene3D route. Glyphs are rasterized
onto a flat plane; native hosts need their existing HTML texture support or DOM
fallback to display it.
