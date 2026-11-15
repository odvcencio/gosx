# Scene3D projected decals

A projected decal paints a texture onto whatever surface its box covers: a floor, a wall, a slope, a curved hull.
Bullet holes, footprints, scorch marks, blood, paint and ground markers all use the same mechanism.

Decals run as a post effect. The pass reads the scene depth buffer, finds the surface under each pixel of the decal's
box, and paints the decal there. It works on WebGPU and on the WebGL2 fallback, and it draws nothing for scenes that do
not declare it: the code lives in its own chunk, which the page downloads only when a scene lists `Decals` (or
`MotionBlur`) in its post chain.

Do not confuse it with the older `scene.Decal` node, which is a flat alpha-blended quad placed by hand. A
`ProjectedDecal` follows the surface it lands on.

## Declare decals

```go
props := scene.Props{
    PostFX: scene.PostFX{Effects: []scene.PostEffect{
        scene.Decals{
            Materials: []scene.DecalMaterial{{
                Name:      "holes",
                Albedo:    "/img/bullet-holes.png", // an atlas; alpha is coverage
                Columns:   4,
                Rows:      2,
                NormalMap: "/img/bullet-holes-n.png",
                Blend:     "multiply",
            }},
            Items: []scene.ProjectedDecal{{
                Material: "holes",
                Position: scene.Vec3(2, 0.01, -3),
                Normal:   scene.Vec3(0, 1, 0), // the surface normal
                Size:     scene.Vec3(0.4, 0.4, 0.5), // width, height, projection depth
                Frame:    3,
            }},
            Pools: []string{"impacts"}, // run-time decals, see below
        },
        scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1},
    }},
}
```

Put `Decals` before `Tonemap`, `Bloom` and the other effects that should see the decals.

A decal is a box. Its local +Z axis points along `Normal`, away from the surface, and the box reaches `Size.Z` through
the surface along that axis. Choose a depth a little larger than the unevenness you want to cover: a deeper box
reaches onto neighbouring surfaces, a shallower one misses bumps.

### Materials

| Field | Meaning |
|---|---|
| `Albedo` | Atlas image URL. Alpha is the coverage. |
| `Columns`, `Rows` | Atlas grid, default 1 x 1. `Frame` picks a cell, row by row from the top left. |
| `NormalMap`, `RoughnessMap`, `EmissiveMap` | Optional, same atlas layout. The roughness map reads its green channel. |
| `Color` | Multiplies the albedo (`#rrggbb`). |
| `Roughness` | Constant, or the scale of the map. Default 0.8. |
| `EmissiveColor`, `EmissiveIntensity` | Light the decal gives off, ignoring scene lights. |
| `NormalStrength` | Scales the normal map. Default 1. |
| `AngleFadeStart`, `AngleFadeEnd` | Degrees between the decal's facing and the surface normal. Opaque up to the start, gone at the end. Defaults 65 and 88. |
| `EdgeFade` | Fraction of the box that fades at each edge and face. Default 0.1. Use 0.001 for hard edges. |
| `Blend` | `"lit"` (default) or `"multiply"`. |

Albedo and emissive images load as sRGB color. Normal and roughness images load as linear data. The page generates
mipmaps when it loads each image.

**Lit** decals paint their own color, shaded by the scene's ambient and first directional light, with a sun highlight
that follows roughness. They do not receive shadows. **Multiply** decals multiply the color already on screen, so
shadows and every light in the scene still show through. Use multiply for dirt, scorch and holes; use lit for paint,
signs and glowing marks. Multiply ignores emissive.

### What the pass does not have

- The depth buffer holds no normals, so the pass rebuilds them from depth differences. Thin features and depth
  discontinuities (the edge of an object against a far wall) get an approximate normal. The angle fade hides most of
  those pixels.
- Decals do not write into the depth buffer or the lighting inputs of other passes. Effects after `Decals` see the
  painted color; reflections and screen-space effects before it do not.
- One draw per run of consecutive decals that share a material. Sort order is layer, then spawn order. Keep material
  switches low by grouping decals on a layer.
- A texture that finishes loading does not schedule a frame by itself. Scenes that render every frame (games) pick it
  up on the next frame.

## Run-time decals: `DecalPool`

`DecalPool` holds many short-lived decals in fixed storage. Spawning does not allocate once the pool exists.

```go
impacts := scene.NewDecalPool(scene.DecalPoolConfig{
    Name:     "impacts", // matches Decals.Pools
    Capacity: 256,       // oldest decal is recycled when full
    Life:     20,        // seconds; 0 keeps decals until recycled
    FadeOut:  3,         // seconds spent fading before the end of life
})

// When a shot lands:
impacts.Spawn(scene.DecalSpawn{
    Material: 0,                    // index into Decals.Materials
    Position: hit.Point,
    Normal:   hit.Normal,
    Rotation: rand.Float64() * 6.28, // spin about the normal
    Size:     scene.Vec3(0.3, 0.3, 0.5),
    Frame:    rand.Intn(8),
})

// Once per frame:
impacts.Update(dt)
impacts.Publish() // js/wasm: copies the records to the renderer when they changed
```

`Publish` is a no-op outside a js/wasm build. It writes a typed array under
`window.__gosx_scene3d_decal_pools[name]`; the renderer reads it each frame and re-sorts only when the pool's revision
changes. A page that does not use Go can fill the same object: `{data: Float32Array, count, revision}` with
`scene.DecalRecordStride` (20) floats per decal. `DecalPool.Records()` returns the same layout for a worker or a
test.

A full pool recycles the oldest decal immediately, without a fade. Size the pool so recycling happens far from the
camera or after the fade.

Engine workers have no `window`; publish from the main thread.

## Quality and cost

The pass draws only decals inside the view and within `MaxDistance` (default 120 units) of the camera. Each visible
decal costs the pixels its box covers on screen, not the pixels of its texture, so many small decals on screen are
cheap and a few huge boxes are not. Frame measurements for this branch are in the pull request description.
