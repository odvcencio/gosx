# Scene3D v1 support contract

This document defines the narrow Scene3D v1 boundary. It is a convergence
contract, not a claim of generic Three.js parity and not a claim that every
row is complete today. The executable source of truth is
`scene/harness/testdata/v1-corpus.json`; CI validates that manifest and every
evidence anchor with:

```sh
go test ./scene/harness -run '^TestV1CorpusContract' -count=1
```

The manifest uses two states. `targetBackends` names the backend closure target;
only an `enforced` row is a support claim, and every such target must be covered
by typed test evidence with an exact CI job, step, and command owner.

- `enforced`: automated evidence exists in a normal CI lane.
- `blocked`: the v1 requirement still needs implementation or executable
  end-to-end proof and therefore carries no CI-owned evidence claim. Scene3D
  v1 is not complete while any row is blocked.

## v1 boundary

### Material detail layers

`StandardMaterial.Detail` and `Model.Detail` add world-space albedo, normal, and
roughness maps on WebGL2/WebGPU. Model detail preserves each glTF primitive's
base material. Nil detail emits nothing and retains the existing shader.
PNG/JPEG/KTX2 use the existing loaders; missing, pending, or failed channels are
neutral. Canvas and preview renderers show the base material.

Browser defaults: 2 metres per tile, normal scale 1, albedo mix 0.6, roughness mix
0.5, slope blend 30–45 degrees, fade 8–14 metres, stochastic tiling enabled.
Go omits zero numeric settings; prop-bag scenes can express explicit zero mixes.
Ground uses world XZ; Steep uses triplanar `abs(baseNormal)^4` weights and signed
axis bases. Nil Steep uses Ground everywhere. `Triplanar` overrides both layers.
Three hashed patches receive random offsets/quarter turns and normalized cubic
barycentric blending. Explicit unrotated gradients prevent mip seams. Use
tileable natural detail maps; the sampler does not synthesize missing edges.
Albedo applies clamped `base * mix(1, 2*detail, AlbedoMix)`; roughness blends from
base to the detail map's red channel; UDN normal blending preserves the base map.

`1 - smoothstep(FadeStart, FadeEnd, cameraDistance)` controls all channels.
Gradients precede the fade branch: zero detail samples beyond FadeEnd or when
quality disables detail. Programs/pipelines append `-detail` to the existing
PBR variant; camera/quality changes update uniforms without recompilation.

Packed 512×512 mipmapped arrays cost one sampler and about 5.33 MiB per atlas,
plus cached source textures. All maps present: at most 6 samples for Ground,
18 for Steep, 24 during their blend (36 if Ground is also triplanar). Axis-aligned
surfaces cost 6; missing channels reduce samples; disabling stochastic divides
bounds by three. Oblique surfaces exceed the 12-sample target to preserve all
projections and patches. Packing resamples larger maps and runs when textures
change. Detail instances use the existing per-mesh GPU culling fallback.

Adaptive survival disables detail by default; `qualityProfiles.*.detail` can
override it. `QualityRung.Detail` overrides off at rung zero/on above it.
`node scripts/validate-scene-detail-shaders.mjs` validates GLSL/WGSL without a
browser. These tests cover contracts and shader validity, not visual acceptance.

Scene3D v1 targets common browser product viewers, configurators, simulation
dashboards, and interactive scenes. Its supported boundary is:

- WebGPU and WebGL2 consume the same SceneIR semantics. Capability fallback is
  allowed, but a backend must not silently render a scene it cannot represent.
- Typed scene graphs provide deterministic nesting and full translation,
  rotation, and scale semantics on leaves and groups, including matching
  raycast/pick behavior.
- Uncompressed glTF/GLB 2.0 supports single and multiple buffers, data URIs,
  same-origin external buffers, embedded images, bounded and sparse accessors,
  ordinary primitives, existing skin/morph support, and imported animation.
- Imported animation supports `LINEAR`, `STEP`, and `CUBICSPLINE` for
  translation, rotation, scale, and morph weights. The existing mixer
  play/stop/fade/loop/weight surface is the v1 action boundary.
- The currently tested standard material/texture subset is supported.
  Meshopt- and Draco-compressed geometry fails closed with named errors.
  `KHR_texture_basisu` is not silently advertised: renderer KTX2 block upload
  and glTF Basis import are separate browser capabilities. S4 owns asset-pipe
  normalization and this S0 row makes no asset-pipeline support claim.
- Desktop orbit/fly/first-person controls, pointer lock, picking, object drag,
  and transform-gizmo commits are the v1 interaction boundary.
- Supported browser post effects preserve declaration order. Native/headless
  custom-post behavior remains blocked until S5 provides backend-specific
  degradation and telemetry evidence; it is not claimed as browser parity.
- Hub updates either emit complete commands or reject a remount-required diff
  atomically. Generic hydration must not discard Scene3D command output.
- A corpus route must publish measured p95/p99 frame evidence and stay inside
  the existing JavaScript, network, WASM, and performance budgets.

Set `scene.Props.RenderBeforeModels` to `scene.Bool(true)` to draw the first
frame without waiting for glTF or other model assets. Sky, water, lights, and
non-model nodes render while models load; hydration keeps its existing commit
behavior and schedules a render with reason `models` when it settles, including
failure. Transition priming and progressive-model setup still follow hydration.
Disposal or mount replacement prevents late hydration from rendering the old
scene. A nil prop emits no `renderBeforeModels` key and preserves existing wire
bytes; nil or false keeps the wait for models. The mount reports the selected
startup path as `before-models` or `after-models` in
`data-gosx-scene3d-first-frame`.

## Opt-in walking

Set `Props.Controls: scene.ControlFirstPerson` and `Props.Walk: &scene.Walk{}`
for grounded browser navigation. Omitting Walk keeps the existing free camera.
`scene.NewWalkGround(minX, minZ, sizeX, sizeZ, cols, rows, heights)` encodes a
row-major heightfield (columns along +X, rows along +Z); invalid dimensions or
sample counts panic. Sampling is bilinear and clamps at grid edges. Without a
heightfield, the ground is the Y=0 plane.

The browser defaults to a 1.7m eye height, 0.35m body radius, 1.6m/s walking,
2.2× sprint, a 38° slope limit, 0.3m steps, 0.03m head bob, and 2.2 radians per
1000px of look movement. Ground following starts on movement and eases from
the authored pose over about 0.15s. Collision queries use the body's circle at
feet height against cylinders, rotated boxes, and sphere slices. Bounded travel
steps prevent tunneling; rejected movement slides along each horizontal axis.
An uphill probe distinguishes short ledges from continuous steep slopes. Bounds
limit the feet position; water defaults to a 0.55m maximum depth. These authored
collision shapes are independent of visible scene geometry.

Click for pointer lock, use WASD or up/down arrows to move, left/right arrows to
turn, PageUp/PageDown to pitch, Shift to sprint, and Home to reset. Keyboard input
requires canvas focus or pointer lock. Touch uses a left-side joystick and
right-side drag; connected gamepads use the two sticks. Set `Gamepad` to false
to disable pads, `HeadBob` to zero to disable bob, and `Hint` to `"none"` to hide
the hint. Reduced motion always disables bob. The joystick and hint have
`gosx-scene3d-walk-*` classes for styling.

A button with `data-gosx-scene3d-reset="mount-id"` restores the start pose; an empty
value targets the only Scene3D mount on the page. The mounted handle exposes
`resetCamera()`, and `window.__gosx.scene3d.resetCamera(mountId)` is available
once walking loads. Camera output signals and telemetry use the existing control
path. The controls frame loop stops when idle, except while a gamepad is connected.
`bootstrap-feature-scene3d-walk.js` is advertised only when a scene carries walk
props and fetched only for first-person walking. Evidence lives in
`scene/walk_test.go`, `island/island_test.go`, and `client/js/scene3d-walk.test.js`.

## Executable corpus status

| Corpus ID | Current state | Closure needed |
| --- | --- | --- |
| `gltf-single-buffer-textured` | blocked | Add one end-to-end textured GLB case, not only isolated loader tests. |
| `gltf-multi-buffer-external` | enforced | Indexed data-URI and same-origin external buffers are tested. |
| `glb-bin-plus-external` | enforced | GLB BIN buffer 0 plus external buffer 1 is tested. |
| `gltf-sparse-embedded-image` | enforced | Sparse overlays and embedded bufferView images are tested. |
| `gltf-meshopt-rejection` | enforced | Meshopt input fails closed with a named error. |
| `gltf-draco-rejection` | enforced | Draco input fails closed with a named error. |
| `gltf-basis-ktx2-policy` | enforced | Basis import degradation and renderer KTX2 upload are separate browser facts; S4 owns asset-pipe policy. |
| `gltf-cubic-trs-morph` | enforced | Analytic fixture covers TRS, morph weights, clamps, seeks, native WebGL2 canvas pixels, and production WebGPU renderer pixels from the exact proof-private target. Actual WebGPU canvas presentation remains part of the release-pinned hardware completion obligation. |
| `nested-group-scale-pick` | blocked | Browser renderer proof and native unit coverage exist; require renderer-consumed native normals, winding/cull, and pick evidence before certification. |
| `desktop-controls-picking` | enforced | Orbit/first-person controls and picking stay inside the v1 boundary. |
| `desktop-gizmo-commit` | blocked | Add one end-to-end fly/pointer-lock/object-drag/gizmo commit proof. |
| `ordered-post-custom-uniforms` | blocked | Add one cross-backend order/uniform-patch proof before certifying this row. |
| `native-preview-degradation` | blocked | S5 must add backend-specific `CustomPost` degradation and telemetry evidence before this can be certified. |
| `hub-command-diff` | enforced | Diffable fields produce a lossless command stream. |
| `hub-remount-atomic-reject` | enforced | Remount fields reject before mutation or watcher notification. |
| `scene-p95-budget-route` | blocked | Add a dedicated corpus route and measured p95/p99 evidence. |
| `generic-adapter-command-envelope` | enforced | ABI 3 returns a strict versioned initial-command envelope; real WebGL2/WebGPU WASM mounts prove stale suppression, targeted hub application, and atomic remount rejection. |

## Explicit non-goals

The following are outside the Scene3D v1 contract unless a later decision adds
their dependency, byte, and certification cost:

- generic Three.js feature parity or generic performance superiority;
- WebXR/XR;
- runtime meshopt, Draco, or BasisLZ transcoders (build-time normalization is
  permitted as a separate feature);
- IK, retargeting, broad animation state machines, CSG, NURBS, Studio, or a
  modeling kernel;
- advanced TAA, SSR, or motion-blur pipelines;
- full native visual parity or macOS/Linux native renderer parity;
- general mobile touch/pinch/gamepad Scene3D controls;
- SH light probes, custom vertex attributes, and point shadows as v1 completion
  requirements;
- alpha-mask-aware shadow-caster silhouettes as a v1 completion requirement.
  Alpha cutoff is supported on visible PBR surfaces, but the current depth-only
  directional and spot shadow passes cast the closed mesh silhouette.

## Completion rule

Scene3D v1 is ready only when the manifest has no `blocked` rows, the named CI
lanes are green, budget changes are backed by measurements, and real WebGPU
hardware evidence exists for the release-pinned corpus. Passing the manifest
shape test alone proves that the contract is coherent; it does not certify the
blocked cases.

## Frame completion timing

`window.__gosx_scene3d_debug.inspect(surfaceID).frameTiming` gives a snapshot
for the selected surface. Get IDs from `listSurfaces()`. Reading the snapshot
does not consume the sample used by adaptive quality.

- `gpuMS` is elapsed GPU time for the complete frame. WebGPU uses standard
  timestamp writes before and after the frame encoder. WebGL uses
  `EXT_disjoint_timer_query_webgl2` around the full render call. Both include
  water, world geometry, and post effects. They exclude display scanout and
  asset uploads that occur outside the frame.
- `source` is `gpu-timestamp`, `webgl-timer`, or `none`. `scope` is `frame`.
  `frameSeq` identifies the measured frame; asynchronous readback can lag the
  current frame. `atMS` is the time when the result was read.
- `status` is `measured`, `pending`, `unavailable`, `stale`, `disjoint`,
  `failed`, or `disposed`. Only `measured` has a numeric `gpuMS`. A result is
  stale one second after readback. Missing hardware timers do not produce a
  CPU estimate in `gpuMS`.
- `cpuSubmitMS` measures the CPU render call. `frameIntervalMS` measures the
  time between render calls. `submitAtMS` identifies the latest CPU sample.
  These values are separate from GPU time and can refer to a later frame.

Each backend uses at most three frame queries. A full ring skips a timing
sample without delaying rendering. Readback never waits inside the render
call. WebGL discards all pending results after a disjoint event. A device or
context loss invalidates GPU samples. Renderer replacement starts a new ring.

### HDR post processing

When a WebGPU scene has post effects, the scene, auxiliary, bloom, and MSAA color targets use `rgba16float`. The canvas keeps its preferred presentation format. The final blit converts the last post target to that format. Resize and post-effect changes rebuild targets and pipelines with matching formats.

The tone-map effect applies the same display transfer as WebGL. Linear, ACES, and Reinhard modes apply gamma 2.2 after the curve. Filmic already includes its output response and gets no second transfer. Put bloom before tone mapping to select radiance above one. An explicit tone-map effect remains required; an identity or custom-only chain does not gain an implicit curve. Custom shader color conventions remain the author’s responsibility.

Set `scene.Bloom{Mode: "mip"}` to opt into mip-chain bloom on WebGL2 and WebGPU. The prefilter clamps linear RGB to 64, uses the maximum color channel for brightness, and applies a soft knee of half the threshold. `Scale` remains the prefilter resolution factor, defaulting to 0.5. Up to six levels halve each dimension, stopping before the short side would fall below eight pixels; a smaller initial target still works as one level. Each reduction uses 13 bilinear taps (outer offsets of two source texels and inner diagonals of one), with normalized weights. Upsampling adds a 3×3 tent with weights `[1, 2, 1] × [1, 2, 1] / 16` into separate scratch targets. The tent radius is `Radius / 5`, clamped to 0.25–2 source texels, with a default of one. The scene receives the resulting bloom multiplied by `Strength` in linear HDR before the following tone map. Resize and disposal release all levels and scratch targets. Empty and unknown modes retain the existing algorithm and emit no mode key from Go.

### Browser sky

WebGPU and WebGL2 draw `Environment.Sky` behind the scene. Gradient stops are sRGB colors, blended in linear light by the world-space view direction. Camera translation does not move the sky. A sky by itself does not replace default environment lighting. Nil sky keeps the existing clear color.

Environment mode uses the IBL radiance cube when it is ready, then the legacy environment image. `EnvRotation` turns the sky around world Y. `Sky.Intensity` scales linear radiance; zero means one. `Sky.Blur` selects the available radiance mip range. WebGL2 generates mips for legacy images. WebGPU legacy images currently have one mip and stay sharp. Pending, failed, or absent maps use `HorizonColor`. The mount reports `data-gosx-scene3d-sky`: `none`, `gradient`, `environment-cube`, `environment-map`, `environment-pending`, or `environment-unavailable`. A shader setup failure reports `unavailable` on WebGL2. Canvas2D retains its flat background fallback.

Physical mode (`Sky{Mode: "physical"}`) draws an analytic daylight sky: Rayleigh and Mie single scattering after Preetham, Shirley and Smits (1999) in the real-time form of Hoffman and Preetham (2002), with a sun disk. `SunDirection` points toward the sun; `scene.SunDirectionFromAngles(elevation, azimuth)` builds it in degrees, with azimuth 0 facing -Z. Give the key `DirectionalLight` the opposite direction so shadows agree with the drawn sun. `Turbidity` (1-20, default 10), `Rayleigh` (0-8, default 2), `MieCoefficient` (0-0.1, default 0.005), `MieDirectionalG` (0-0.999, default 0.8) and `SunDiskRadius` (degrees, default 0.53; negative hides the disk) shape it; zero means the default. The output suits the ACES tone mapper at an exposure near 0.5. The server fills any unset gradient stop from the same model, so Canvas2D shows matching colors. `Sky.PhysicalRadiance` evaluates the model in Go; with `ibl.CubeFromRadiance` it bakes IBL that matches the drawn sky. The mount reports `data-gosx-scene3d-sky="physical"`.

### Open ocean

`Environment.Ocean` draws an open sea that reaches the horizon on WebGPU and WebGL2. One draw of a camera-centred polar grid (no vertex buffers) carries six Gerstner waves sized from `WaveHeight` (significant height, meters), `WaveLength`, `WindDirection` (degrees; 0 travels toward +Z) and `Choppiness`. Per-pixel capillary waves fade out before they alias. The surface reflects the scene sky (the physical model when `Sky.Mode` is `physical`) with Fresnel weighting, adds a GGX sun glint, subsurface tint on crests (`ScatterColor`), whitecaps where the swell compresses, and fades into the sky at the horizon. With `Bathymetry` (a grayscale heightmap and its world extent) the water knows its depth: shallows turn `ShallowColor` and transparent over the terrain, waves shoal, foam lace rings the shore and every rock, a breaker line and a 9 s run-up surge (`Surf`) move up the beach. `Encoding: "signed-sqrt"` stores heights with fine steps near sea level; linear is the default. Low-end hardware gets four waves and a quarter of the grid. The ocean draws after opaque geometry with premultiplied alpha and writes depth; its horizon ignores the camera far plane. The mount reports `data-gosx-scene3d-ocean`: `none`, `surface`, `shore`, `bathymetry-pending`, `bathymetry-failed` or `unavailable`. Canvas2D draws no ocean. Opt-in geometry reflections are described below.

Sky draws share the scene target and post chain. They do not write depth. A water scene with a sky uses the world composite even when it has no imported models.

### Ocean geometry reflections

`Ocean.Reflections: &scene.OceanReflections{Mode: "ssr+planar", Resolution: 0.5, Strength: 1}` enables reflections on WebGL2 and WebGPU. Modes are `ssr`, `planar`, and `ssr+planar`; nil or an empty mode keeps the previous pipeline. Resolution defaults to 0.5 (range 0.125–1); strength defaults to 1. The ocean samples an opaque colour/depth capture with its filtered wave normal, marches at most 20 steps, refines a crossing with five binary steps, and rejects hits outside a depth thickness tolerance. Edge and roughness fades blend misses into a mirrored, sea-plane-clipped PBR render, then the physical sky. Visible custom shaders reflect through SSR; the planar fallback currently draws built-in PBR mesh objects. The sun path combines the existing GGX glint with an anisotropic unresolved wave-slope distribution.

Balanced quality drops planar first and uses ten SSR steps. Survival quality releases reflection targets and uses the original sky reflection. QualityLadder selects survival at its first rung, balanced at intermediate rungs and full at its last rung. Camera changes update matrices and uniforms without compiling shader variants. Estimated desktop cost at 1440p: 0.8–1.8 ms for half-resolution captures, ocean SSR and a modest opaque fallback; geometry complexity affects planar cost. These are planning estimates, not hardware measurements. Canvas2D omits geometry reflections.

### Living physical sky

`Sky.Clouds: &scene.SkyClouds{Coverage: 0.45, Altitude: 1500, Scale: 3000, Speed: 8, Direction: 90, Opacity: 0.85}` adds a curved procedural deck to physical sky mode on both GPU backends. Coverage is 0–1 (zero clears the deck); altitude and scale are metres; speed is metres/second; direction uses the ocean's degree convention. Browser defaults are 0.45 / 1500 / 3000 / 8 / 0 / 0.85. Go preserves zero markers for altitude, scale, speed and opacity. Noise stays in world coordinates and drifts smoothly, with derivative filtering. Beer attenuation, a powder term and a sun-facing silver lining use the physical sky's ambient and attenuated sun colour. Blue hour keeps ambient sky light. The ocean evaluates the same deck for its sky reflection.

Full quality uses five noise octaves; balanced uses three; survival removes the deck and disables its ocean contribution without changing the authored shader variant. Nil Clouds adds no uniforms, buffers or cloud shader code. Estimated 1440p cost including ocean reflection: 0.3–0.7 ms on a desktop GPU, unmeasured. Birds are omitted to keep attention and budget on the coastal light.

### Atmospheric light and grade

`Environment.Haze: &scene.Haze{Density: 0.0015, HeightFalloff: 0.04, SunScatter: 0.25}` replaces flat fog with a depth-reconstructed, height-dependent extinction integral and physical horizon in-scattering. Density is inverse metres; height falloff is inverse metres above sea level; sun scatter controls the forward lobe. The browser supplies these defaults. Nil leaves existing fog unchanged. Balanced retains height haze; survival uses existing fog density, or haze density as a cheap flat fallback.

`scene.GodRays{Intensity: 0.18, Decay: 0.96, Density: 0.9, Samples: 32}` adds physical-sun-coloured radial scattering on WebGL2 and WebGPU, with opaque depth occlusion and stronger scattering at low sun. The half-resolution pass uses 8–64 samples (default 32); balanced caps it at 12 samples and quarter resolution. Survival removes shafts and releases their target. Below-horizon or behind-eye sunlight contributes zero. Haze and shafts share a composite before bloom.

Use `scene.Tonemap{Mode: scene.TonemapAgX, Exposure: 0.7}` for a fitted AgX log-exposure sigmoid with a toe, shoulder and highlight desaturation, followed by one sRGB transfer. Put `scene.Grain{Intensity: 0.015}` after tonemap and before FXAA. Grain uses a fixed pixel pattern; grading and haze dither without frame seeds. Survival removes grain and retains tonemap. New Go zero fields are omitted so browser defaults apply; nil haze and absent effects preserve existing wire data and shader variants. JS can explicitly set zero intensity to disable an effect.

A restrained coastal chain is GodRays, mip Bloom (`Threshold: 1.2, Strength: 0.12, Radius: 3, Scale: 0.5`), AgX, Grain and FXAA. Keeping bloom above diffuse daylight and its strength low avoids broad halos. Estimated additional 1440p desktop cost for shafts, haze and grade: 0.4–0.9 ms, unmeasured. Combined atmosphere estimates are 1.5–3.4 ms; planar geometry can dominate. Hardware frame-time and Chrome/Edge/Firefox visual acceptance remain required.

### Controlling the Scene3D animation clock

Mounted Scene3D handles expose `getAnimationClock()` and
`setAnimationClock({ timeSeconds, paused })`. Use the handle registered on the
mount as `mount.__gosxScene3DHandle`, after command readiness.

```js
handle.setAnimationClock({ timeSeconds: 1.25 }); // seek and pause
handle.setAnimationClock({ timeSeconds: 0.5 });  // seek backward
handle.setAnimationClock({ timeSeconds: 0.5, paused: false }); // resume
```

Time must be a finite number from zero to 86,400 seconds. `paused` defaults to
true. Invalid arguments leave state unchanged; disposed surfaces reject writes.
The setter schedules a render and resets the wall-clock baseline, so resuming
does not include time spent paused. `getAnimationClock()` returns `timeSeconds`,
`paused`. Existing animation controls reflect this state.

This is an absolute clock for declarative motion, animation clips, spin/drift,
material programs and shader time. Reduced-motion preferences still govern
declarative movement. Stateful water/particle simulations and event-driven glTF
mixers retain their own state; exact reproduction of those requires explicit
pose/state replay. The API does not claim to rewind their simulation history.

A host can keep the native clock paused and drive it from one presentation
playhead. Wait for the scheduled render to settle before capturing its pixels.
