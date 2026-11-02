# Query the ocean surface

`scene.NewOceanQuery(ocean, "high", floor).Sample(x, z, seconds)` returns the water above a world XZ coordinate: position, shading normal and water-particle velocity. Use `"low"` for the renderer's four-wave quality; high uses six waves. `Evaluate` takes the undisplaced grid coordinate instead.

The browser module `client/runtime/scene3d/ocean-query.ts` exposes `window.__gosx_scene3d_ocean_query.create`, `sample`, `evaluate` and `bathymetry`. Load the shared `ocean-waves.ts` first. JS and Go accept the public Ocean zero-value defaults. For already normalized SceneIR properties, call `create(ocean, quality, floor, true)` to retain explicit zeros, such as stopped waves.

Both implementations use the same table in `scene/ocean_waves.json`. The Go implementation embeds it; `node scripts/generate-ocean-waves.mjs` writes the module used by the GPU uniform packer and the JS query. Coefficients and time round to float32, as the GPU uniform block does. Amplitude follows significant height: sum(a²) = Hs²/8. Gravity is 9.81 m/s².

The query includes horizontal Gerstner displacement, depth-dependent shoaling and the shader's nine-second run-up. Two fixed-point steps followed by up to eight Newton refinements invert the horizontal mapping. Normals use the shader's tangents; the shader omits derivatives of the shoaling and run-up terms. Velocity differentiates displacement in time at a material point; it is not an Eulerian current model. Pixel-scale capillary normal detail and foam are shading effects and are outside this surface query.

When the Ocean has a `Spectrum`, `NewOceanQuery` sums the ocean's `SwellWaves` instead of the Gerstner table. These are the 24 strongest bins of the spectrum's first two cascades. When `SwellWaves` is empty, as on an Ocean that was never normalized, `NewOceanQuery` computes them with `scene.OceanSwellWaves`, which clamps and wraps its inputs the way the SceneIR's normalization does. A raw Ocean and its normalized form therefore give the same samples; choppiness and the shore run-up amount are clamped the same way. A non-empty `SwellWaves` is used as it is, so after changing `Spectrum` or `WindDirection` of an Ocean taken from a SceneIR, clear `SwellWaves`. `Quality`, `WaveHeight`, `WaveLength` and `Speed` are ignored, time runs at 1x and is not rounded to float32, and choppiness scales the horizontal displacement. Shoaling and the shore run-up still apply. The browser module does not evaluate spectral oceans yet.

The query does not apply `Presentation`. When `Presentation` sets a drift or a `TimeScale`, the GPU surface samples the water at the position minus the integrated drift offset and evolves it at presentation time times `TimeScale`. `x` and `z` are therefore water-frame coordinates and `seconds` is wave time; apply the offset and the scale before the call if the query should follow the rendered water.

## Where the drawn water is

A host that moves a vessel's pose itself (an external-pose vessel, as Bon Voyage does) does its own buoyancy, and needs the offset and wave time the renderer drew with. In Go, take a reader once and read it each frame:

```go
water, err := ctx.Scene3DByID("sea").OceanWater() // after Ready
...
if frame, ok := water.Read(); ok {
	frame = frame.Advance(age)          // optional: carry the reading forward, age about one frame
	s := query.SampleWater(frame, x, z) // the swell waves' height at world (x, z)
}
```

`Read` returns a `scene.OceanWaterFrame`: `X` and `Z` (the drift the water has integrated), `Time` (wave time), `Spectral`, the rates `TimeScale`, `WaterDriftX` and `WaterDriftZ`, `Seq` (the frames the renderer submitted with an ocean), `Generation` (changes when the graph is rebuilt, and `Time`, `X` and `Z` restart) and `PageEpochMS`. `SampleWater` moves the point into the water frame, queries at `Time` and moves the result back. The values are exactly what the shader draws with on WebGPU and on WebGL2 (the page copies the graph's own `waterX`, `waterZ`, `simTime`, `timeScale`, `driftX` and `driftZ`). When the spectral graph is not running, because it is still building, failed, or the ocean has no spectrum, the frame is the Gerstner sea's: `Spectral` is false, `X` and `Z` are zero and `Time` is the scene clock, and `OceanQuery` applies `Ocean.Speed` itself.

The frame gives the phase and drift of the drawn sea. `SampleWater` approximates only its strongest waves. The query sums 24 bins of the first two tiles; the drawn sea sums about 12,000 bins on four. Measured against the full sum, the heave over a hull-sized footprint is off by about 93% of its signal in a calm sea, 82% in a moderate sea, 67% in a rough sea and 57% in a storm, and the slope at a point by 97% to 99%. Stay in phase with the drawn sea with the frame; do not expect `SampleWater` to reproduce its heights. Reading the drawn heights back from the GPU at host-given points is a later step.

Limits:

- It is a reading for drawing. Presentation never feeds a simulation: do not use it in a game rule, a save or a replay.
- `Read` itself allocates nothing in Go. It reads a `Float64Array` the page keeps in place; in an engine worker the page posts each frame by message, which allocates in the browser. The page records nothing until a reader is made, and only frames the renderer submitted count.
- Latency: the reading is the last drawn frame, so a pose that the next render draws sees a reading 1 frame old. In an engine worker add the message hop. Cap `Advance`'s age at about one frame; do not compare `PageEpochMS` with the worker's own clock. See `scene.OceanWaterFrame`.
- `Time` is not a monotonic clock: it restarts when `Generation` changes and stops while the scene is paused. Keep your own clock for a filter.
- After the ocean is removed or the renderer stops, `Read` keeps returning the last frame; a `Seq` that no longer changes means the ocean is gone.
- The page keeps one spectral ocean per page; two mounted spectral scenes are not told apart.
- A desktop-native engine host does not forward the request; the reader works on the main thread and in a Worker.

Pass a bathymetry callback that reproduces the GPU texture's level-zero bilinear R sample. JS `bathymetry(ImageData, ocean.bathymetry)` clamps to texel centres, filters R and then decodes linear or signed-sqrt height. A nil callback means deep water. A terrain heightfield is useful for collision but differs from the filtered texture; use the texture for visual parity in shallow water.

The query tests compare independent ports of the GLSL/WGSL vertex maths over a grid and several times to 0.0001 m. A shared 72-sample fixture checks Go/JS position, normal and velocity to 0.0000001. Actual GPU transcendental implementations and raster interpolation can introduce additional rounding.

## The drawn heights at your points

`OceanQuery.SampleWater` is an approximation of the drawn sea (see above). To read the heights the renderer computes, ask the page to read them back from the GPU at the points you choose, for example the 7 by 5 grid of a hull:

```go
water, _ := ctx.Scene3DByID("sea").OceanWater() // after Ready
water.SetProbe(points)                          // []scene.OceanProbePoint, world x and z, at most 64
defer water.Close()                             // stops the page probing
...
var r scene.OceanProbeReading                   // keep one; ReadProbe fills it in place
if water.ReadProbe(&r) && !r.Unavailable {
	h := r.Height[i]                            // world Y of the displaced sea above points[i], ocean level included
	nx, nz := r.NormalX[i], r.NormalZ[i]        // r.NormalY(i) is the third component
}
```

- **What it is.** For each point the height of the surface point that the renderer's displacement (choppiness included) moves to that x, z, with the normal's x and z. WebGPU runs a compute pass and WebGL2 draws the points into a float target; both sample the displacement and slope textures the ocean shader samples, over all cascades, with the same Newton inversion of the horizontal map. When the Gerstner sea is drawn (no spectrum, or a graph that is not running) the reading is the CPU query that mirrors the shader, for the tier the frame draws.
- **What it is not: the visible mesh.** The vertex shaders sample the textures at a level of detail that grows with the mesh spacing, so with distance from the camera. The reading is the level-zero field and does not depend on the camera. Against the mip-sampled mesh the difference is about 1 cm RMS at 20 m, 8 to 9 cm at 160 m and 11 to 12 cm at 215 m (three sigma 4 to 35 cm). This is the sea a ship rides, which is what a host that moves a hull wants; there is no option for the visible waterline.
- **Not in a reading.** A bathymetry's shoaling, shore run-up and a ship wake's height. A point with a NaN or infinite coordinate gives NaN. On WebGPU the host probe uses its own pass, not the graph's `ocean_probe` kernel: the kernel's heights match the drawn textures since Elio v0.6.0, but its normal is a 0.5 m central difference of height and measured up to 0.2 in a component away from the shader's.
- **Tags.** `Seq` and `Generation` are those of the water frame the points were drawn in. Compare `Seq` with the `Seq` of the frame you hold for the age in frames. `Request` is the number of the `SetProbe` call the reading answers.
- **Latency.** See `scene.OceanProbeReading` for the measured numbers (WebGL2 and WebGPU, 35 points every frame). The page never waits for the GPU: a pixel buffer behind a fence on WebGL2, a staging buffer mapped after the frame is submitted on WebGPU. Add one frame of message hop in an engine worker. The first ask compiles a shader or builds a pipeline, which can cost a frame and delays the first reading. A Gerstner reading has no delay.
- **No ask, no work.** Nothing is drawn, built, fenced or read until `SetProbe` is called with at least one point; `Close` (or a call with none) stops it, and so does the end of the engine in a worker.
- **Accuracy.** See the numbers in the pull request and `TestWaterProbeOnTheGPU` in `e2e`: both backends against a double-precision CPU evaluation of the very textures the shader samples, at the frame each reading was issued in, with drift, a non-zero level and a tier switch.
- **Unavailable.** When the page cannot read the sea back (the pass could not be built, a fence failed) the reading has `Unavailable` set and no heights. A lost WebGPU device does not set it: readings stop and their age grows. Never use an older reading as current.
- **One ocean mount per page.** The probe state, like the water frame's, is shared by mounts.
- **Drawing only.** Presentation never feeds a simulation: keep readings out of game rules, saves and replays.
- **Cost.** `SetProbe`, `ReadProbe` and `Close` do not allocate in Go. In an engine worker the page posts the answer each frame (about 2.7 KB while a host asks, 80 bytes otherwise).
