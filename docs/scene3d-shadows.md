# Scene3D shadows

A directional light can cast shadows across a large scene by splitting the
camera view into up to four cascades. Near cascades spend their texels on the
area close to the camera, far cascades cover the distance, and the renderer
blends across each seam so no line shows where one cascade hands off to the
next.

```go
scene.DirectionalLight{
    ID:             "sun",
    Direction:      scene.Vec3(-0.4, -1, -0.3),
    CastShadow:     true,
    ShadowSize:     2048, // texels per side, per cascade
    ShadowCascades: 4,    // 2..4 splits the view; 0 or 1 keeps one map
    ShadowDistance: 80,   // cascades end 80 units from the camera
    ShadowSoftness: 0.2,  // 0 is a crisp edge; larger values widen the penumbra
    ShadowBias:     0.004,
}
```

## Fields

| Field | Effect |
|---|---|
| `ShadowCascades` | 2 to 4 cascades for a directional light seen through a perspective camera. Spot lights and orthographic cameras always use one map. |
| `ShadowDistance` | How far from the camera the cascades reach, in world units. Zero uses the camera's far plane. Set it to the size of the play area: a 1,000-unit far plane with no limit spreads four cascades thinly. Shadows fade out over the last part of this distance. |
| `ShadowSize` | Texels per side of each cascade, 256 to 4096. `Props.ShadowMaxPixels` caps the total across all of a light's cascades. |
| `ShadowSoftness` | 0 gives a crisp, filtered edge. Above 0, both backends search for blockers and widen the filter with the distance between caster and receiver (percentage-closer soft shadows), so contact points stay sharp. |
| `ShadowBias` | Depth offset that prevents surface acne. Raise it if lit surfaces show stripes; lower it if shadows detach from their casters. Tune it with one shadow map: each cascade scales it to its own texel size, so the same value works at any `ShadowCascades` and `ShadowDistance`. A negative value pulls the comparison toward back faces; keep it small, because thin parts then shadow themselves. |

## Stable edges

WebGPU fits each cascade to the bounding sphere of its slice of the view, so
turning the camera does not change the cascade's size, and it snaps the cascade
to the shadow-map texel grid. A slowly moving camera therefore moves a shadow
edge in whole texels, without crawling or shimmering. WebGL2 also snaps to the
texel grid.

## Backends

Both backends render up to two shadow-casting lights (directional or spot) with
up to four cascades each. WebGPU stores each light's cascades in one depth
texture array; WebGL2 does the same with a 2D depth array texture. Custom
Selena materials can read the same cascades; the binding and uniform layout is
documented at the top of `client/runtime/scene3d/webgpu-shadows.ts`.

On WebGPU, a frame with no shadow-casting light draws with a lit shader that
has no shadow sampling code. Software renderers such as SwiftShader run both
sides of a branch, so a scene without shadows would otherwise still pay for
them on every pixel.
