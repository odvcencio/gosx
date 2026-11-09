# Scene3D temporal anti-aliasing, auto exposure and depth of field

## Temporal anti-aliasing

Add a `taa` post effect to smooth thin edges, foliage and specular sparkle that
FXAA cannot hold still. The renderer shifts the camera by a sub-pixel amount
each frame and blends each pixel with its history.

```go
PostFX: scene.PostFX{Effects: []scene.PostEffect{
    scene.Tonemap{Mode: scene.TonemapACES},
    scene.TAA{HistoryWeight: 0.9, ClampGamma: 1.25},
}},
```

| Backend | What TAA does |
|---|---|
| WebGPU | Resolves the linear HDR image before the post chain. Static surfaces reproject through depth; moving retained meshes and skinned meshes draw motion vectors. Blended particles and pixels near an ocean or pool surface keep less history, because they move without depth. |
| WebGL2 | Resolves inside the post chain. Static surfaces reproject through depth; moving retained meshes and skinned meshes draw motion vectors (skinned ones need 528 vertex uniform vectors, which most desktop GPUs have). Pixels near an ocean or pool surface keep less history. It has no particle mask and turns itself off over composited water. |

The first frame, and any frame after a reset, uses FXAA. History resets on a
resize, a projection change, a camera cut (more than 2 units or 60 degrees in
one frame), a change to the post effects, and a gap over 250 ms.

## Auto exposure

Set `Environment.AutoExposure` to adapt exposure to how bright the view is, the
way an eye adjusts when it walks from daylight into a cave.

```go
Environment: scene.Environment{
    AutoExposure: &scene.AutoExposure{
        MinExposure: 0.25, MaxExposure: 6, // bounds of the adapted multiplier
        Key:         0.18,                 // target average luminance (middle grey)
        SpeedUp:     3, SpeedDown: 1,      // adaptation rates per second
        Compensation: 0.5,                 // exposure stops added on top
    },
},
```

Zero fields use the defaults shown in the comments of `scene.AutoExposure`. A
camera jump of more than 2 units adapts at once instead of fading. The scene
must render through the post chain, so include at least one post effect such
as tone mapping.

### One color contract

Every surface writes linear light into the HDR scene target. Auto exposure
multiplies that target by the adapted value, tone mapping then applies the
authored `Exposure` and the curve, and the display transfer runs once at the
end. WebGPU measures with a compute pass and WebGL2 with a mip-chain reduction;
neither reads the value back to the CPU, so adaptation never stalls a frame.
WebGL2 needs float render targets (`EXT_color_buffer_float`); without them the
scene keeps its authored exposure.

## Depth of field

Add a `DOF` post effect to blur what sits in front of or behind a focus
distance, the way a camera lens does:

```go
props.PostFX = scene.PostFX{Effects: []scene.PostEffect{
    scene.DOF{FocusDistance: 6, Aperture: 0.05, MaxBlur: 12},
}}
```

| Field | Meaning |
|---|---|
| `FocusDistance` | Distance from the camera, in metres, that stays sharp (default 8). |
| `Aperture` | How fast blur grows away from the focus distance (default 0.04). |
| `MaxBlur` | Largest blur radius in pixels (default 8, at most 64). |

Each pixel's blur radius is `|depth - FocusDistance| * Aperture * MaxBlur`,
capped at `MaxBlur`, where depth is the distance from the camera. Both
backends gather 48 samples on a spiral out to that radius and keep a sample
only where its own blur reaches back to the pixel. A sharp subject therefore
keeps a hard edge against a blurred background, and the blurred background
does not smear onto it. WebGL2 and WebGPU run the same gather, so one setting
looks the same on both.
