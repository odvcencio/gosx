# Changelog

## v0.18.0

R/D series closeout: first-party WebGPU bundle rendering reaches R5 feature
parity, and the Windows desktop host reaches the D release gate.

### Render R1-R5

The new `render/bundle` pipeline is now the first-party WebGPU Scene3D backend
instead of a spike path. It covers lit and skinned meshes, instancing, CSM
shadow passes, material textures, block-compressed KTX2 upload paths, cubemap
IBL, compute particles, picking, bloom/tonemapping, FXAA 3.11, HDR intermediate
format selection, HDR10 presentation encoding when the surface supports it, and
device capability gating across `jsgpu`, `headless`, and stub backends.

The content pipeline gained KTX2 geometry metadata for layers/faces/depth,
cube-view texture registration, compressed-format support hooks, and cubemap
environment sampling in the lit shader. The post-FX path now splits HDR compose
from the final anti-aliasing pass, chooses RGB9E5/RGBA16F/RGB10A2-style formats
by device capability and memory budget, and preserves the headless test surface
for server-side validation.

Headless rendering is no longer limited to the early unlit path. The CPU
backend now covers deterministic lit shading approximation, depth-only shadow
passes, material texture sampling, compute-particle update/render, and a
scene3d-bench-style deterministic frame test. New examples exercise skinned
rendering and cubemap IBL in `examples/skinned-glb-spike` and
`examples/cubemap-ibl-spike`.

### Windows Desktop D Series

The Windows desktop host now has a real app surface around WebView2:
`gosx desktop dev` hot-reloads through the dev proxy, F12/devtools are gated by
CLI/runtime options, and the bridge uses typed request/response envelopes with
limits, errors, and streaming response support.

Shell and lifecycle integration landed for single-instance locking,
second-instance argument forwarding, deep-link registration, file associations,
per-user registry writes, lifecycle callbacks, `App.NewWindow`, and
`OnWindowCreated` / suspend / resume hooks. Native UI support now includes tray
icons, tray/context/menu-bar menus, shell notifications, file-drop callbacks,
per-monitor-v2 DPI awareness, AppUserModelID setup, and a minimal accessibility
surface. `gosx desktop --native-smoke` and `test/desktop/run.sh phase-5` cover
the manual Windows acceptance path.

Runtime resilience and release tooling now include `gosx build --offline`, a
versioned offline asset manifest, `CrashReporterOptions` for Go panic capture
plus Windows minidumps and optional user-consented upload, MSIX manifest/package
staging through `gosx build --msix`, signtool integration through `--sign`, and
AppInstaller feed generation through `--appinstaller`. The desktop API exposes
`App.UpdateCheck()` and `App.UpdateApply()` for AppInstaller-based updates.

### Release Gate

Linux and cross-compile validation passes across the renderer, desktop, CLI,
and examples. The remaining gate before cutting the public `v0.18.0` tag is the
manual Windows host smoke: install the MSIX, sign it with the real certificate,
publish a second AppInstaller package, and verify the update flow.

## v0.18.0-alpha.28

Scene3D no longer voluntarily loses WebGL contexts when a scene is hidden or offscreen.

The idle context release path was too aggressive for marketing/landing pages. It called `WEBGL_lose_context` after 30 seconds outside the renderable lifecycle, then tried to use the existing canvas as a 2D fallback while waiting for restore. Browsers generally do not allow switching an existing WebGL canvas to 2D, so production could report `webgl-context-lost-no-fallback` and leave the visual background black until the browser restored the context.

Hidden/offscreen Scene3D mounts now pause scheduled rendering without forcing context loss. Browser-driven `webglcontextlost`/`webglcontextrestored` handling remains in place for real GPU resets.

## v0.18.0-alpha.27

Scene3D voluntary WebGL restore fix.

`scheduleIdleContextRelease` now keeps the original `WEBGL_lose_context` extension object before calling `loseContext()` and `restoreVoluntarilyLostContext` uses that cached extension for `restoreContext()`. Chrome and Safari can refuse to reacquire `WEBGL_lose_context` from an already-lost canvas, which made prod report `webgl-voluntary-restore-requested requested=false`, left the renderer stubbed as `lost`, and forced the restore watchdog into `swapped=false`. Firefox tolerated the old reacquire path, which is why the failure skewed toward non-Firefox browsers.

## v0.18.0-alpha.26

Managed form submitter action fix.

GoSX managed form submissions now only honor `formaction`, `formmethod`, and `formtarget` when the submitter actually declares those attributes. Chromium exposes `button.formAction` as the current page URL even when no `formaction` override is present, which made managed POST forms submit to `/` instead of the form's own `action`. The navigation runtime now preserves native form semantics across browsers while still supporting explicit submitter overrides.

## v0.18.0-alpha.25

Scene3D live point-palette buffer fix.

Live Scene3D updates now apply large point buffer fields (`count`, `positions`, `sizes`, `colors`) immediately before building update-transition deltas. This keeps sparse palette-buffer swaps from being cloned into every transition frame while still allowing scalar fields such as opacity to tween normally. The patch also invalidates the paired cached GPU buffers through the existing transition-patch path so the next render uploads the new per-vertex colors.

Scene3D CSS transition diagnostics are now opt-in behind `window.__gosx_scene3d_css_debug === true`. The alpha.14 diagnostic logs were still emitted on production CSS resolves and could flood Chrome/Edge during live palette debugging.

## v0.18.0-alpha.24

Scene3D live palette/material swap fix.

`scenePlannerHashMaterial` in `15b-scene-planner.js` short-circuited to `key`-only when a material had a stable `key`, excluding `color`, `opacity`, `emissive`, `roughness`, `metalness`, `texture`, and `blendMode` from the hash. Downstream, `scenePreparedSignature` consumed that hash to detect whether the prepared scene could be reused, so CSS-var-resolved color rewrites on keyed materials (m31labs's `Material name="stars" color="var(--galaxy-star-color)"` pattern that drives the hourly/half-hourly palette shift) produced a new IR with new colors — but the signature matched the previous frame, `lastPrepared.passes` was reused with the previous bucket's baked colors, and the canvas never repainted. The mutation observer, CSS revision bump, fresh CSS resolve, and `scheduleRender` all fired correctly; the invalidation died at the signature check.

`scenePlannerHashMaterial` now hashes identity (`key`) AND resolved appearance (`color`, `opacity`, `emissive`, etc.) together, so palette-swap cache invalidation propagates through the prepared-scene signature into the pass list, forcing a fresh upload on the next frame.

## v0.18.0-alpha.23

Scene3D voluntary-restore watchdog.

Prod telemetry on Chrome caught a second restore-path failure mode distinct from the one alpha.22 fixed. After `scheduleIdleContextRelease` calls `ext.loseContext()` on a scene that has been off-viewport or backgrounded for 30 s, the canvas emits `webgl-context-lost` with `voluntary: true`. `restoreVoluntarilyLostContext` is supposed to pair with it by calling `ext.restoreContext()` on the next visibility/viewport transition, but Chrome does not always fire the matching `webglcontextrestored` event — the restore request is silently dropped and the stub renderer stays installed, leaving a permanently black canvas.

`restoreVoluntarilyLostContext` now arms a 2000 ms watchdog after calling `ext.restoreContext()`. If `webglcontextrestored` lands naturally, the watchdog is cancelled. If it doesn't, the watchdog force-invokes `restoreSceneWebGLRenderer` directly, bypassing the missing browser event and recovering the scene. Three new telemetry events track the lifecycle: `webgl-voluntary-restore-requested`, `webgl-voluntary-restore-watchdog`, `webgl-voluntary-restore-forced` (info or error depending on whether the forced swap succeeded).

`webgl-context-restored` now also reports `watchdogPending` so the server log can distinguish "browser fired on its own" from "watchdog was about to force-restore when the browser finally fired".

## v0.18.0-alpha.22

Scene3D WebGL context-restore root-cause fix.

Root cause isolated via the alpha.21 telemetry probe: after `webglcontextlost` the mount still held the live WebGL renderer, and any `scheduleRender` already queued before the loss fired a rAF callback that called `renderer.render(...)` on that stale renderer. As soon as the browser restored the context (same `gl` object, but all resources invalidated), every `gl.useProgram`, `gl.bindFramebuffer`, and `gl.drawElements` on those cached handles raised `GL_INVALID_OPERATION` (1282), the PBR/post-fx chain produced a silently blank frame, and the canvas stayed black even though `data-gosx-scene3d-renderer="webgl"` and `isContextLost` agreed everything was fine.

`onWebGLContextLost` now disposes the live renderer immediately and substitutes a no-op `sceneRendererLostStub` (`kind: "lost"`, `render()`/`dispose()` both empty) until `restoreSceneWebGLRenderer` installs a fresh renderer. Queued render callbacks between the two events land on the stub and harmlessly no-op, so no cached GL handles are touched across the loss/restore boundary. Local repro against Chrome + `WEBGL_lose_context.loseContext()` + `restoreContext()` now reports `glError: 0` post-restore (was 1282 in alpha.21).

The `render-canvas-blank` probe itself also switched from `gl.readPixels` to `canvas.toDataURL("image/png")` length check: on `preserveDrawingBuffer: false` contexts (the gosx default), `readPixels` sees zeros after the browser's composite clear even when the drawing buffer held real content — a false positive. `toDataURL` forces a sync snapshot from the pre-clear buffer. A uniform black 800x461 PNG compresses to ~400-900 bytes; any real scene lands at 8-30 KB, so a 1800-byte floor is used as the blank threshold.

## v0.18.0-alpha.21

Scene3D restore-path diagnostics.

The `render-empty` detector was missing the modern PBR drawing path: a bundle with `meshObjects` or `instancedMeshes` but no legacy `vertexCount`/`worldVertexCount`/`surfaces` fell through the early-return and produced a false-positive `render-empty` (or, conversely, stayed silent when the modern path silently produced a black canvas). The detector now counts `bundle.meshObjects.length` and `bundle.instancedMeshes.length` as real geometry.

When the bundle has geometry but the renderer may still be producing a blank canvas (framebuffer binding lost across a WebGL context-restore, stale PBR shadow attachments, etc.), gosx now schedules a one-frame-later `gl.readPixels` probe of a 32×32 center region. If every sampled pixel is `(0,0,0,0)` and `sceneState` still has drawable geometry, a new `render-canvas-blank` event fires with `lastSwapReason`, `rendererKind`, `glError`, and bundle inventory counts. The probe is opt-in via `window.__gosx_telemetry_config.probeCanvasBlank = true` so production pages do not readback every frame on every swap.

`restoreSceneWebGLRenderer` also emits `renderer-warmup` after `renderLatestSceneBundle`. The event carries the bundle inventory the fresh renderer just had to process — mesh/instance/point/light/label/sprite/surface counts plus `bundleHasPostFX` — so a silent post-restore blank frame can be narrowed to a specific resource class in the server slog.

## v0.18.0-alpha.20

Scene3D WebGL context-restore fix.

On `webglcontextrestored`, the restore path now renders `latestBundle` synchronously against the freshly created renderer before returning — forcing the new GL context's vertex/color/material buffers to populate on the same tick. Previously, `restoreSceneWebGLRenderer` only queued a deferred `scheduleRender`, so the new renderer's `passBuffers` stayed empty until the next animation frame actually fired. On pages where the animation loop was gated (tab hidden, off-viewport, or reduced-motion), the scene stayed black despite the mount reporting `data-gosx-scene3d-renderer="webgl"` and `isContextLost: false`. The test at `client/js/runtime.test.js` now forces a new `FakeWebGLContext` on restore and asserts `bufferData` + `drawArrays` land on the new GL object.

## v0.18.0-alpha.19

Scene3D rendering and physics integration release.

The WebGL2 PBR renderer now supports environment-map lighting inputs, Radiance `.hdr` parsing in a dedicated helper, CSM depth passes for directional shadows, texel-snapped cascade projections, and PCSS-style soft shadow sampling from `shadowSoftness`. Shadow maps, cascades, and environment maps now share a single texture-unit allocation path, so four-cascade CSM no longer collides with IBL sampler units. Shared budget helpers also downscale IBL/shadow resources together under the 26 MB default target and expose a half-float/LDR fallback decision for mobile GPUs.

The physics foundation now carries typed Scene3D physics declarations into canonical IR and then into `physics.WorldSpec`. Rigid bodies, colliders, static colliders, and distance constraints can be lowered into a runnable `physics.World`, while the existing `sim.Runner` path handles authoritative ticks and primitive input commands (`impulse`, `force`, `torque`) by body ID or index. Worlds also expose deterministic body/collider removal and closest-hit raycasts for sphere, plane, and oriented-box colliders.

Skinned GLB loading and playback plumbing is connected through the runtime: joint/weight attributes are extracted, skins remain in bind space, animation mixers update model instances, and joint matrices are uploaded through the skinned PBR shader path. Runtime coverage now asserts the WebGL2 skinned program uploads `u_hasSkin`, `u_jointMatrices[]`, `a_joints`, and `a_weights`.

The built-in video engine now owns the managed `<video>` element, shared `$video.*` signals, HLS.js runtime asset loading, source swaps, subtitle track normalization, and server-rendered fallback upgrades while still enforcing the one-video-per-page v1 constraint.

A new client-event telemetry pipeline ships bootstrap-side `window.__gosx_emit(level, category, message, fields)` plus automatic capture of uncaught errors and unhandled rejections. Events are batched, flushed every 2 s (or via `navigator.sendBeacon` on `visibilitychange=hidden`), and POSTed to `/_gosx/client-events` where a new server-side handler logs each event through a package-level `slog.Logger`. The handler enforces a 64 KB body cap, per-remote-addr rate limit, and a `GOSX_TELEMETRY=off` kill switch. Scene3D now emits structured events for `webgl-context-lost`, `webgl-context-restored` (with `swapped`), `renderer-swap`, and a new `render-empty` diagnostic that fires when a renderer swap produces an empty bundle despite the scene having geometry — the exact signature of a silent WebGL-restore regression.

## v0.18.0-alpha.18

GSX grammar and server app mounting release.

GSX text parsing now handles text that begins immediately after a child closing tag, such as `<p>a<span>b</span>c</p>`, by moving the `jsx_text` CST token onto the GoSX external scanner. The scanner now treats literal `>` and `}` as valid GSX text, so inputs such as `<p>a > b</p>` and `<p>Result: }</p>` lower into full IR text instead of silently truncating at the punctuation. Scanner external symbol lookups now use named constants tied to the grammar external order rather than raw indexes.

Server apps now expose `MountApp(prefix, child)` for composing child GoSX apps under a parent path. Non-root mounts strip the parent prefix before delegating, root mounts preserve the leading slash, and the child handler is built lazily so routes registered on the child after `MountApp` but before the first request are included.

## v0.18.0-alpha.17

Scene3D transition timing cache release.

CSS-driven Scene3D transitions now cache resolved transition timing on the mount element. The planner no longer has to carry the same timing-resolution fields through every scene planner record, while mount updates retain the resolved duration and easing needed for CSS variable animations.

## v0.18.0-alpha.16

Scene3D transition and capability release.

CSS-driven Scene3D updates now have a scene-wide default transition timing fallback. When a record does not declare its own transition, the planner searches material transitions first and then environment transition settings, caches the resolved timing on planner state, and uses that duration/easing for CSS variable animations.

Typed `scene.Props` graphs that include `ComputeParticles` now declare the `webgpu` engine capability automatically. File-route spread lookup also honors `GoSXSpreadProps()` for reserved transport fields such as `capabilities`, so `<Scene3D {...data.scene}>` uses the same computed capability contract as its serialized scene props. This fixes the docs homepage server-render failure where compute particles were validated against only the default `canvas webgl animation` capability set.

## v0.18.0-alpha.15

Scene3D material transition fallback release.

The CSS planner can now resolve transition timing from material records when an animated object, points layer, or instanced mesh does not carry its own transition metadata. Records can refer to materials by index or name, and the planner uses the material transition as the fallback timing source for CSS-backed scene updates.

Transition diagnostics now include fresh resolve logs with the CSS revision and previous-cache state, making it easier to trace whether a scene update came from a cached CSS bundle or a new resolution pass.

## v0.18.0-alpha.14

Scene3D transition diagnostics and CI format release.

Scene3D CSS variable transition debugging now emits planner diagnostics through `console.log`, making the messages visible in the browser and CI log paths that suppress `console.debug`. The transition logs were added around CSS bundle resolution to expose revision and cache behavior while chasing material-driven animation updates.

The repository formatter output was also backfilled across tracked generated Go and GSX files, unblocking the GitHub Actions format gate before the later Scene3D E2E fixes landed.

## v0.18.0-alpha.13

Editor release for Slack-style emoji shortcodes and picker suggestions.

The editor helper bar now includes an `emoji` command that inserts Markdown++ shortcode text instead of raw emoji, keeping authored content portable across renderers and storage backends. The hidden editor textarea opts into the shared emoji autocomplete runtime, so typing `:` plus a shortcode character opens a scrollable suggestion table.

Markdown rendering and the `/_gosx/emoji-codes.json` lookup now share a small compatibility alias layer for Slack-ish names such as `:simple_smile:`, `:slight_smile:`, `:thumbs_up:`, and `:red_heart:` while preserving the generated GitHub gemoji plus Unicode Emoji table as the canonical source.

## v0.18.0-alpha.12

Build and Scene3D runtime release for faster TinyGo output and CSS variable transitions.

`gosx build` now compiles the shared WASM runtime and islands-only WASM runtime concurrently when TinyGo is available. The build path factors the repeated compile flow through a shared helper and collects goroutine results before processing errors, cutting wall-clock time for the two TinyGo runtime artifacts.

Scene3D CSS-backed properties can now transition smoothly when CSS variables change. The browser planner tracks prior resolved values, parses transition timing from scene records, interpolates active numeric/color updates with easing, and keeps the scene dynamic while transitions are in flight so author-driven CSS state changes animate instead of jumping.

## v0.18.0-alpha.11

Patch release for Scene3D graph lowering and route panic diagnostics.

The Scene3D graph lowerer now initializes its anchors map before use, fixing a runtime panic path during graph construction. Route panic recovery also prints stack traces, making server-side render failures and panics easier to diagnose from process output.

## v0.18.0-alpha.10

Diagnostics release for server render failures and panics.

File page render errors now log with file path context, route handler panics log the route pattern and panic details, and page render recovery includes the request path. This keeps debugger and terminal output useful when a route fails before the response can carry enough context.

## v0.18.0-alpha.9

Scene3D compiler release for control-flow inside composable scene markup.

The route compiler now supports `<Each>`, `<For>`, `<If>`, `<Show>`, and `<When>` inside `<Scene3D>` composable children. Scene3D child lowering was split into smaller helpers for child-list traversal, loop expansion, conditional recursion, and composable node processing, so typed scene markup can use normal GSX control-flow constructs.

## v0.18.0-alpha.8

Scene authoring release for typed mesh spread props.

`scene.Mesh` now exposes `SpreadProps`, allowing typed mesh values to serialize directly into component attribute maps. This enables `<Each>` loops over typed mesh data in GSX templates without forcing a scene IR round trip before composable element rendering.

## v0.18.0-alpha.7

Hub and Scene3D resource-management release.

The hub now has a per-topic latch API for join replay. Servers can latch the latest topic payload, new subscribers receive the current value without waiting for the next publish, and replay is non-blocking with tests for overwrite behavior, topic isolation, and empty-topic no-op handling.

Scene3D now releases idle WebGL contexts after 30 seconds of inactivity. The runtime schedules context loss when a scene cannot render, restores rendering when activity resumes, clears timers on disposal, and marks the viewport dirty after restoration so the next frame redraws correctly.

## v0.18.0-alpha.6

Patch release for Scene3D CSS invalidation and Firefox/Chromium scroll stability.

Runtime style and attribute writes now skip no-op updates before touching the DOM. Scene3D also ignores environment changes that only alter visual viewport offsets, and its CSS observer filters GoSX-owned `--gosx-*` style churn while still reacting to author CSS variables such as `--galaxy-core-inner`.

This keeps CSS-driven Scene3D live transitions intact without letting root environment updates wake the planner during every scroll frame. On the m31labs.dev galaxy, Firefox now stays at the page scroll floor while the animated WebGL scene continues rendering normally.

## v0.18.0-alpha.5

Patch release for Firefox Scene3D scroll performance.

Scroll-driven Scene3D cameras now cache scroll position and scroll range from scroll/viewport invalidation handlers instead of reading scroll geometry inside every animated render frame. This removes a Firefox layout hot path that could surface as script-timeout warnings during sustained scrolling on particle-heavy scenes.

## v0.18.0-alpha.4

Patch release for Scene3D renderer state reporting.

The WebGL2 PBR renderer now reports `kind: "webgl"` like the legacy WebGL renderer. This keeps `data-gosx-scene3d-renderer` accurate on real PBR mounts and lets environment/capability recovery logic classify the active renderer correctly.

## v0.18.0-alpha.3

Patch release for Scene3D CSS performance and point-layer memory stability.

The planner now caches resolved Scene3D CSS values behind a mount/root CSS revision, so animated scenes do not call `getComputedStyle()` every frame when no CSS state changed. CSS transition windows still re-resolve at frame cadence while a real transition is active, preserving CSS-driven scene fades without keeping the resolver hot forever.

Point layers that inherit named `<Material>` values now reuse the material-applied point record while the source arrays and material values are unchanged. The cached CSS path also reuses resolved point records. This prevents WebGL from rebuilding large `_cachedPos` / `_cachedColors` typed arrays every frame on particle-heavy scenes such as the m31labs.dev galaxy.

The WebGPU availability probe now avoids a redundant `requestDevice returned null` warning when `requestAdapter()` already reported no adapter. Edge/Firefox fallback still lands on WebGL as before.

## v0.18.0-alpha.2

Patch release for Scene3D CSS-backed point materials.

Named `<Material>` records can now style `<Points>` layers without accidentally replacing point-specific rendering defaults. The `v0.18.0-alpha.1` material normalizer always assigned mesh-style defaults such as `blendMode`, then the point resolver applied those defaults back to particle layers. Point-heavy scenes that depended on additive blending, such as the m31labs.dev galaxy, could lose their glow/spark particle effect.

The point material resolver now only overrides `color`, `opacity`, `blendMode`, and `depthWrite` when the material explicitly specified those fields. Existing point-layer values remain intact otherwise. The regression test covers a CSS-var material applied to an additive point layer.

## v0.18.0-alpha.1

Scene3D now has a compiler-first authoring path and a shared render planner that both WebGL and WebGPU consume.

### Scene3D composable authoring and IR validation

`<Scene3D>` accepts composable children such as `<Camera>`, `<Environment>`, `<Material>`, `<Mesh>`, `<Points>`, light components, and `PostFX.*` components. The route compiler lowers them into the same runtime scene shape as prop-based scenes, so existing `<Scene3D {...props}>` users and new composable markup share one backend contract.

The Go and browser-side validators now agree on the SceneIR schema: node kind/payload pairing, material-index edge cases, non-negative counts, render-bundle discrimination, and capability gates are covered. Compiler lowering now fails loudly when a scene requests capabilities the target engine does not provide.

### CSS-stylable 3D scene state

Scene3D can resolve CSS custom properties in material, point, light, environment, and post-FX fields. Authors can write values like `color="var(--galaxy-core-inner)"`, `roughness="var(--scene-core-roughness, 0.4)"`, or `fogDensity="var(--galaxy-fog-density)"`; the planner batches computed-style reads, applies the resolved values to a cloned IR, and hands normal data to the renderers.

The runtime also emits scene-node sentinels for selector-driven styling and observes mount/root `class` and `style` mutations, media query changes, and CSS transition windows. That lets CSS class toggles, inherited `:root` variables, and `@property` transitions drive the WebGL/WebGPU canvas without reviving the older Scene3D Live event path.

### Shared planner, backend parity, and WebGPU buffer fixes

The new shared planner owns pass buckets, command sequencing, CSS-var resolution, scene-filter parsing, and buffer cache helpers. WebGL and WebGPU now expose parity command logs in tests, which locks both backends to the same prepared command sequence.

WebGPU point rendering now uses per-entry cached GPU buffers through `sceneCachedBuffer()` for point uniforms and particle storage data, avoiding the old shared-buffer churn on point-heavy scenes. Regression coverage guards against reintroducing the previous `pointsUniformBuffer` write-every-entry pattern.

### `@scene3d` and post-FX CSS hooks

The CSS package can extract `@scene3d` declarations and mirrors plain `scene-filter:` CSS into a custom property consumed by the planner. `scene-filter: bloom(...) vignette(...) color-grade(...)` maps into the scene post-processing chain, parallel to the browser's `filter:` shape but scoped to Scene3D.

## v0.17.24

TinyGo WASM runtime builds now ship a slim core runtime by default.

### `gosx build` — TinyGo core runtime drops below 225KB gzip

The TinyGo path now builds with `-no-debug` and `-panic=trap`, then uses a `gosx_tiny_runtime` build tag to keep optional browser exports out of the core island/engine runtime. The slim TinyGo runtime keeps the core exports used by GoSX pages:

- `__gosx_hydrate`, `__gosx_action`, `__gosx_dispose`
- shared-engine exports: `__gosx_hydrate_engine`, `__gosx_tick_engine`, `__gosx_render_engine`, `__gosx_engine_dispose`
- shared signal exports: `__gosx_set_shared_signal`, `__gosx_get_shared_signal`, `__gosx_set_input_batch`

The optional browser-side exports are still compiled into normal Go/WASM builds, but TinyGo omits them by default:

- `__gosx_text_layout`, `__gosx_text_layout_metrics`, `__gosx_text_layout_ranges` — the JS bootstrap already provides the public `window.__gosx_text_layout*` API and adopts a WASM implementation only when one is present.
- `__gosx_highlight` — used by the dashboard example when available, with plain-text fallback.
- `__gosx_crdt_*` — the first-party Go `crdt` package and hub sync path remain available; these direct browser exports no longer ride along on every TinyGo island page.

Set `GOSX_TINYGO_FULL_RUNTIME=1` during `gosx build` to keep the full TinyGo export set for applications that call those optional globals directly.

Measured release output:

| Runtime | Raw | gzip estimate |
|---|---:|---:|
| Standard Go WASM fallback | ~6 MB | ~2 MB |
| v0.17.23 TinyGo + `wasm-opt -Oz` | 1,417,044 bytes | 484 KB |
| v0.17.24 TinyGo full exports + `wasm-opt -Oz` | 1,090,196 bytes | 372 KB |
| v0.17.24 TinyGo core + `wasm-opt -Oz` | 654,211 bytes | 223 KB |

## v0.17.23

TinyGo-backed WASM runtime builds ship again.

### `gosx build` — TinyGo works from a pruned WASM module graph

Production builds already tried TinyGo before falling back to the standard Go WASM compiler, but the attempt never reached GoSX code on current developer machines. The installed TinyGo was already current (`0.40.1`), but TinyGo 0.40.1 only accepts Go 1.19 through Go 1.25. GoSX's root module now requires Go 1.26, and a few non-WASM tooling dependencies also require Go 1.26, so TinyGo exited during module loading and `gosx build` silently produced the full standard-Go runtime.

The build now constructs a temporary scratch module for the TinyGo attempt:

- derives the real `GOOS=js GOARCH=wasm` dependency closure with `go list -deps -json`
- copies only the GoSX packages in that closure into the scratch module
- writes a minimal `go.mod` with the external modules that the WASM runtime actually imports
- pins the scratch module directive to `go 1.25`
- preserves `go.sum` for checksum reuse

For the current runtime, that trims the external graph to `github.com/odvcencio/turboquant` and `github.com/rivo/uniseg` instead of asking TinyGo to load the full project graph.

### TinyGo toolchain fallback for Go 1.26 hosts

When the active `go` binary is too new for TinyGo, `gosx build` now retries the TinyGo compile against compatible installed Go SDKs. It checks `GOSX_TINYGO_GOROOT`, `$HOME/sdk/go1.*`, and `/usr/local/go`, filters to Go 1.19 through Go 1.25, picks the newest compatible root, and runs TinyGo with that root first on `PATH` plus `GOTOOLCHAIN=local`.

On the release machine, the build used Go 1.25.9 at `/home/draco/sdk/go1.25.9`, then applied `wasm-opt -Oz`.

Measured release output:

| Runtime | Raw | gzip estimate |
|---|---:|---:|
| Standard Go WASM fallback | ~6 MB | ~2 MB |
| TinyGo + `wasm-opt -Oz` | 1,417,044 bytes | 484 KB |

This keeps the standard Go fallback intact for environments without TinyGo or a compatible Go SDK, but makes the fast path work on the current GoSX toolchain.

## v0.17.22

Four loosely-related wins that all surfaced while investigating Scene3D performance on the m31labs.dev galaxy page:

1. A new `gosx/visual` package + `gosx visual` CLI for pixel-level regression testing
2. Scene3D critical-path bundle shrunk by 35KB via two new lazy sub-feature splits (GLTF loader, animation mixer)
3. First-render deferred so WebGL buffer upload doesn't block LCP
4. `gosx perf` reports now flag software-GPU environments so automated gates don't chase ghost regressions

### `visual` — new package for pixel-level regression testing

A dedicated `visual/` package and `gosx visual` CLI for catching unintended rendering changes. Uses the existing `perf.FindChrome` integration to auto-select between a remote `CHROME_WS_URL` (for in-cluster perf browsers) and a locally-launched Chrome, so the same API works for developer-machine runs, CI jobs, and scheduled in-cluster audits.

Three public entry points:

- **`Capture(ctx, url, CaptureOptions)`** — navigates with a viewport, waits for a selector + settle period, optionally runs a JS snippet to hide dynamic UI chrome, returns PNG bytes.
- **`Diff(baseline, current)`** — pixelmatch comparison with a side-by-side diagnostic image when dimensions differ. Mismatched pixels highlighted red on a grey-washed background for human review.
- **`Assert(ctx, url, AssertOptions)`** — capture + diff + baseline IO + threshold gate in one call. Writes a baseline when `Update=true`, returns `*AssertMismatch` (with diff path, current-capture path, mismatch count, diff percentage) when drift exceeds threshold.

`gosx visual [flags] <url>` exposes the full flag surface — `--update`, `--baseline`, `--threshold`, `-w/-h/--scale`, `--wait`, `--wait-selector`, `--selector`, `--eval`, `--timeout`, `--diff`, `--json`.

**Scene3D determinism note:** For visual regression against Scene3D pages, consumers should honor a `?__gosx_visual_seed=HASH` query param in their scene props loader and derive the RNG seed from it — see `app/galaxy.go` in the m31labs.dev reference app for a working pattern (multiple named seeds map to canonical frozen moments of the galaxy's time-based palette system). Rotation phase remains driven by requestAnimationFrame even when particles are seeded, so a small `--threshold 0.2` absorbs rotation drift between captures while still catching palette changes.

`github.com/orisano/pixelmatch` is promoted from transitive-only to a direct dependency so `visual/` can import it without relying on chromedp pulling it in.

### `scene3d` — lazy sub-feature bundles shrink main payload by 35KB

Two more subsystems extracted from `bootstrap-feature-scene3d.js` into on-demand chunks, following the same pattern as v0.17.16's WebGPU split. Pages that don't use these features now skip the parse cost entirely:

| Bundle | v0.17.21 | v0.17.22 | Loading |
|---|---|---|---|
| `bootstrap-feature-scene3d.js` | 540 KB | **505 KB** | eager |
| `bootstrap-feature-scene3d-gltf.js` | *(bundled)* | **24 KB** | lazy |
| `bootstrap-feature-scene3d-animation.js` | *(bundled)* | **13 KB** | lazy |

**GLTF loader** (`19-scene-gltf.js`) moved into `bootstrap-feature-scene3d-gltf.js` behind a new `ensureGLTFFeatureLoaded()` helper in `20-scene-mount.js`. `loadSceneModelAsset()` awaits the chunk the first time it encounters a `.glb`/`.gltf` URL. Pages with only programmatic geometry (points, lines, procedural meshes, particle systems, data viz — the majority of Scene3D consumers) never fetch the chunk. m31labs.dev's galaxy page confirms zero network traffic for GLTF.

**Animation mixer** (`19a-scene-animation.js`) moved into `bootstrap-feature-scene3d-animation.js` and exposed via `window.__gosx_ensure_scene3d_animation_loaded` so consumers can lazy-load the keyframe mixer / bone math before driving animations. Scenes with only transform spins or auto-rotation never fetch this chunk.

Each sub-feature publishes its API through a window global (`__gosx_scene3d_gltf_api`, `__gosx_scene3d_animation_api`) so the main bundle can bridge across IIFE boundaries without touching the original `sceneLoadGLTFModel` / `createSceneAnimationMixer` function bodies.

**Hashed URL hints for immutable caching** — the lazy loaders default to unhashed compat URLs (`/gosx/bootstrap-feature-scene3d-gltf.js`) which the server resolves through the manifest but which the browser can't cache immutably. New `data-gosx-scene3d-gltf-url` and `data-gosx-scene3d-animation-url` attributes on the main `<script defer data-gosx-script="feature-scene3d">` tag carry the hashed URLs (`...-gltf.HASH.js`). A new `resolveSceneSubFeatureURL()` helper in `20-scene-mount.js` reads the dataset and falls back to the unhashed URL only when the attribute isn't present (dev mode, manual integration). First lazy-load now hits `Cache-Control: max-age=31536000 immutable`.

**Plumbing** extends through `buildmanifest` (two new `HashedAsset` slots), `cmd/gosx/build.go` (hash + copy at `gosx build` time), `server/runtime_assets.go` (new compat cases), and `island/island.go` (path storage, `versionCompatRuntimePath` init, compat-hash lookup, allow-list entries, `SetBootstrapFeatureScene3D{GLTF,Animation}Path` setters wired into `ApplyBuildManifest`). One new `.js` file and one new test `HashedAsset` slot per sub-feature — the pattern is now cheap enough to repeat for future splits (PBR, shadows, labels, etc.).

**Cumulative scene3d critical-path savings since v0.17.8:** 658 KB → **505 KB**, **-153 KB** (-23%).

### `scene3d` — defer first `renderFrame` to yield LCP ahead of WebGL init

The mount factory previously called `renderFrame(0)` synchronously after `await sceneModelHydration`. On hardware GPUs that's a ~50-200ms block for vertex buffer upload + shader compile; on SwiftShader software WebGL (headless-shell, WSL2 without GPU passthrough) it balloons to 1-2 seconds of blocking work straddling LCP.

New `scheduleInitialRender()` uses the best-available scheduling primitive to push the first WebGL draw one frame later:

1. `scheduler.postTask({ priority: "user-visible" })` — Chrome 94+, Firefox 126+
2. `requestAnimationFrame` — universal, paints on next vsync
3. `setTimeout(0)` — last-resort task-queue defer

Total work is identical but LCP fires on the pre-existing CSS/DOM content one frame earlier, the site stays interactive during galaxy load, and headless visual-regression captures / CI perf profiles aren't dominated by SwiftShader shader-compile blocking that has nothing to do with real user experience.

### `perf` — software GPU detection + nil-safe WebGL info query

Two fixes to make `gosx perf` reliable in headless environments.

**Software GPU detection.** When `gosx perf` runs in headless Chrome without GPU passthrough (CI, cluster perf audits, WSL2 dev machines, headless-shell screenshot tooling), Chrome falls back to SwiftShader software rasterization. Scene3D frame timings, shader-compile blocking, and main-thread long tasks on that path are dominated by software-GPU latency and do NOT reflect real user experience. Previously `gosx perf` reported those numbers with no qualifier.

Two new helpers on `WebGLInfo`:

```go
func (*WebGLInfo) IsSoftwareRendered() bool
func (*WebGLInfo) SoftwareRendererName() string
```

Pattern-match the unmasked `GL_RENDERER` + `GL_VENDOR` strings against known software rasterizers (SwiftShader, Mesa llvmpipe/softpipe, Apple Software Renderer, Microsoft Basic Render Driver, generic "software rasterizer"). When detected, the perf report now emits a visible warning banner above the GPU Context section:

```
  ⚠  Software GPU detected (SwiftShader)
     Scene3D frame timings, shader-compile blocking, and main-thread
     long tasks below are software-emulated and do NOT reflect what real
     users on hardware GPUs experience. Run this profile against a browser
     with a real GPU for accurate Scene3D numbers.
```

**Nil-safety in `instrument.js queryWebGLInfo`.** The `vendor` and `renderer` fields were assigned directly from `ctx.getParameter()` without the `|| ""` fallback the other fields had, so lost or restricted contexts produced JSON `null` instead of empty strings. The Go-side software-GPU detection pattern-matches strings and needed empty strings, not nulls. Also added a fresh-canvas fallback probe: when the existing scene3d canvas has a lost/stale context (every `getParameter` returns null — happens when Scene3D has hit a shader-compile error path that left the original context unusable), we create a throwaway 1x1 canvas, grab a clean WebGL2 context from the browser/driver, and read vendor/renderer/etc. from that. Preserves diagnostics when the main scene context is unusable.

## v0.17.21

Scene3D SSR hot-path optimization + end-to-end benchmark coverage across `scene`, `route`, and `island`. Three tasks resolved in one release.

### `Props.GoSXSpreadProps` — **378 → 105 allocs (-72%)**

`Props.GoSXSpreadProps` is the public entry point the file-route renderer calls on every `<Scene3D {...data.galaxy}>` server render. The pre-v0.17.21 implementation called `Props.LegacyProps()` which recursively built a deep `map[string]any` tree via `SceneIR().legacyProps()` — about 350 interface-boxing allocations per render plus nested `map[string]any` for every object/model/light/sprite/label. On m31labs.dev's homepage galaxy scene that was the single biggest allocation site in the SSR path.

`MarshalJSON` already had the optimized path from v0.16.5: `legacyBaseProps()` for the shallow scalar props, plus `json.Marshal(SceneIR())` wrapped as `json.RawMessage` under the `"scene"` key. This release extracts that fast-path body into a new internal helper:

```go
func (p Props) spreadPropsFast() map[string]any {
    base := p.legacyBaseProps()
    sceneIR := p.SceneIR()
    if !sceneIR.isZero() {
        if sceneBytes, err := json.Marshal(sceneIR); err == nil {
            base["scene"] = json.RawMessage(sceneBytes)
        }
    }
    return base
}
```

and rewires three public methods onto it:

- **`GoSXSpreadProps`** — used to call `LegacyProps()` (378 allocs) then add `programRef` and `capabilities`. Now calls `spreadPropsFast()` and adds the same two extras. **105 allocs** on the 20-mesh mixed scene fixture.
- **`MarshalJSON`** — was inlining the fast-path body directly. Now delegates to `spreadPropsFast()` + `json.Marshal`. Same alloc count (128), zero semantic change, one less source of drift.
- **`RawPropsJSON`** — used to call `LegacyProps()` + `json.Marshal(values)` (two map walks, 378+ allocs). Now delegates to `MarshalJSON()` directly. **126 allocs**.

`LegacyProps` is preserved as-is because exported scene package tests still type-assert `legacy["scene"].(map[string]any)` to inspect the nested tree. It's effectively a test-only export now; none of the three hot-path methods use it anymore.

### `canonicalizeEnginePropValue` now passes `json.RawMessage` through unchanged

Downstream of `GoSXSpreadProps`, the route file-renderer calls `marshalEngineProps → canonicalizeEnginePropsMap → canonicalizeEnginePropValue` to normalize key casing (PascalCase vs camelCase) before marshaling the final `engine.Config.Props` bytes. `canonicalizeEnginePropValue` recurses through nested maps and slices — without a special case, a `json.RawMessage` (which is a `[]byte`) would be iterated byte by byte, boxed into individual `interface{}` values, and the whole spread optimization would evaporate (plus the output would be corrupt).

Added an early type-assertion at the top of `canonicalizeEnginePropValue`:

```go
if _, ok := value.(json.RawMessage); ok {
    return value
}
```

`json.Marshal` handles `json.RawMessage` natively — it splices the raw bytes directly into the output without re-marshaling — so pass-through is both correct and fast.

### Latent bug: `ModelIR.MarshalJSON` was missing

`ModelIR` embeds `ObjectIR` by value:

```go
type ModelIR struct {
    ObjectIR
    Src       string  `json:"src,omitempty"`
    ScaleX    float64 `json:"scaleX,omitempty"`
    // ... six more model-specific fields
}
```

Go's method promotion made `json.Marshal(modelIR)` dispatch to `ObjectIR.MarshalJSON` — which only emits the `ObjectIR` half via its `objectAlias` type-alias trick, silently dropping every `ModelIR`-local field. The old map-building path in `legacyModels` sidestepped this because it built the record manually; the new direct-marshal path hit it as soon as a typed scene with a `Model` node reached `spreadPropsFast`. `TestDefaultFileRendererSupportsTypedScenePropsSpread` caught it immediately.

Added a proper `ModelIR.MarshalJSON` using the same alias trick as `ObjectIR.MarshalJSON`:

```go
func (m ModelIR) MarshalJSON() ([]byte, error) {
    type objectAlias ObjectIR
    type modelWire struct {
        objectAlias
        Points    []linePointWire `json:"points,omitempty"`
        Src       string          `json:"src,omitempty"`
        ScaleX    float64         `json:"scaleX,omitempty"`
        ScaleY    float64         `json:"scaleY,omitempty"`
        ScaleZ    float64         `json:"scaleZ,omitempty"`
        Static    *bool           `json:"static,omitempty"`
        Animation string          `json:"animation,omitempty"`
        Loop      *bool           `json:"loop,omitempty"`
    }
    return json.Marshal(modelWire{
        objectAlias: objectAlias(m.ObjectIR),
        Points:      toLinePointsWire(m.Points),
        // ... rest of fields
    })
}
```

The shadow `Points` field on `modelWire` wins over the embedded `ObjectIR.Points` by Go's field resolution rules (shallower depth wins), so the canonical `linePointWire` wire shape is emitted.

### Benchmark coverage — three .dmj files updated

**`scene/bench.dmj`** gains five new benchmarks:

| benchmark | allocs | ns/op | context |
|---|---|---|---|
| `PropsSpreadPropsFast` | 103 | 99μs | Direct measure of the new helper |
| `PropsGosxSpreadProps` | 105 | 98μs | Public entry with cap + ref adds |
| `PropsRawJson` | 126 | 131μs | Engine-manifest emitter |
| `PropsMarshalGalaxy` | 383 | 588μs | 80-mesh galaxy fixture |
| `SceneIrGalaxy` | 172 | 86μs | Graph → SceneIR lowering isolated |

Plus a new `benchGalaxyScene` test helper — a production-shaped ~80-sphere fixture approximating m31labs.dev's homepage galaxy engine, used by the larger benches to catch accidental quadratic drift in the lowerer and marshal as scene complexity grows.

**`route/bench.dmj`** gains four engine-props end-to-end benchmarks (task #13 — the "engine/Scene3D end-to-end" piece):

| benchmark | allocs | ns/op | context |
|---|---|---|---|
| `EnginePropsSceneSpread` | 73 | 30μs | Just `marshalEngineProps` on a pre-spread map |
| `EnginePropsSceneEndToEnd` | 171 | 122μs | Full `GoSXSpreadProps + canonicalize + marshal` |
| `EnginePropsGalaxyEndToEnd` | 433 | 438μs | Same pipeline, 80-mesh scale |
| `SceneIrGalaxyLower` | 172 | 67μs | Isolated lowering via cross-package helper |

Galaxy scene pays roughly **5 allocs per mesh** through the full pipeline — linear in node count, which is the expected shape and the regression trip-wire for future refactors. Route bench helpers gain `benchScene3DProps` / `benchGalaxyScene3DProps` — inlined mirrors of the scene fixtures so the route package can benchmark the full cross-package SSR pipeline without adding exports that only tests would use.

**`island/bench.dmj`** gains four end-to-end coverage benches (task #12):

| benchmark | allocs | ns/op | context |
|---|---|---|---|
| `ToggleSsrRender` | 8 | 0.9μs | Fills the SSR coverage gap for the 4th fixture |
| `MultiIslandSsrRender` | 100 | 10.3μs | 10-island page SSR (typical prod page) |
| `FormTypingBurst` | 204 | 30μs | 5 rapid field dispatches in sequence |
| `CounterHydrationRoundTrip` | 70 | 11.7μs | NewIsland + 2 Dispatch + Reconcile |

The multi-island bench measures aggregate SSR cost at realistic page sizes — 10 allocs per island × 10 islands = 100 allocs total, which matches single-island measurements exactly and confirms there's no per-island overhead (manifest registration, program lookup, etc.) above what the counter SSR already pays. `FormTypingBurst` simulates the common pattern of typing into a form field (change-change-change-change-submit) so interaction-burst performance regressions would surface clearly.

## v0.17.20

Third fix in the WebGPU sub-chunk ordering chase. `s.async = false` on a dynamically-inserted `<script>` element is supposed to force ordered-deferred execution (i.e., run the script in document order, behind any parser-inserted `defer` scripts), but chromedp's headless Chromium — and likely several real builds — still raced the webgpu sub-chunk ahead of `bootstrap-feature-scene3d.js`. When the sub-chunk won the race, its IIFE's early-return guard fired:

```js
if (!window.__gosx_scene3d_api) {
  console.warn("[gosx] scene3d-webgpu chunk loaded without main scene3d bundle");
  return;
}
```

and `window.__gosx_scene3d_webgpu_api` was never published, so `sceneWebGPUAvailable()` stayed false for the life of the page and the mount silently stayed on WebGL.

Fix: gate the dynamic script insertion on `DOMContentLoaded` instead. By the time DCL fires, all parser-inserted defer scripts have already executed, so `__gosx_scene3d_api` is guaranteed to be in place when the sub-chunk runs. Fall through to immediate insertion if `readyState !== "loading"`, which covers pages where the inline script runs after DCL somehow.

```go
b.WriteString(`<script>if(navigator.gpu){var _w=function(){`)
b.WriteString(`var s=document.createElement('script');s.async=false;`)
b.WriteString(`s.dataset.gosxScript='feature-scene3d-webgpu';s.src=`)
b.WriteString(htmlJSStringLiteral(webgpuPath))
b.WriteString(`;document.head.appendChild(s);};`)
b.WriteString(`if(document.readyState==='loading'){`)
b.WriteString(`document.addEventListener('DOMContentLoaded',_w);`)
b.WriteString(`}else{_w();}}`)
b.WriteString("\x3c/script>")
```

The delay to sub-chunk load is bounded by DCL, which for a scene3d page is typically ~400ms after the main bundle completes — acceptable since the main bundle's scene mount code falls back to WebGL gracefully while waiting.

Verified end-to-end on m31labs.dev: the "scene3d-webgpu chunk loaded without main scene3d bundle" warning is gone. The only remaining console warning is the probe's diagnostic that SwiftShader can't create a device, which is exactly what the v0.17.17 probe is supposed to surface.

## v0.17.19

First attempt at the sub-chunk ordering fix: set `s.async = false` on the dynamically-inserted `<script>` element. Superseded by v0.17.20 once it became clear the flag isn't reliably honored in every Chromium build. Kept because the flag is still the right intent — it just isn't sufficient on its own, and combining it with a DCL gate produces a robustly-ordered load in every browser.

## v0.17.18

Two related fixes in the inline WebGPU sub-chunk loader, both shipped latent in v0.17.16 and only surfaced after a real scene3d page went into production:

### Inline `<script>` emitted literal `\x3c/script>`

The inline loader was written with a Go raw-string (backtick) literal:

```go
b.WriteString(`<script>if(navigator.gpu){var s=document.createElement('script');...}\x3c/script>`)
```

Raw-string literals don't process `\x` escapes, so the HTML contained the literal seven characters `\x3c/script>` instead of `</script>`. Browsers scanned the `<script>` body for `</script>` (which wasn't there), found the next real `</script>` in the page much later, and parsed the entire intervening text as script source. JavaScript then hit `}\x3c/script>...` and threw `SyntaxError`, silently dropping the whole IIFE — so the `if(navigator.gpu){...}` check never ran on any page since v0.17.16, and the WebGPU sub-chunk was never dynamically loaded.

Fix: split the string so the closing `</script>` comes from a double-quoted Go string where `\x3c` is processed correctly:

```go
b.WriteString(`<script>if(navigator.gpu){...};document.head.appendChild(s);}`)
b.WriteString("\x3c/script>")
```

### Probe used `powerPreference: "high-performance"`

The module-level adapter probe in `16z-scene-webgpu-probe.js` requested an adapter with `{ powerPreference: "high-performance" }`. On some headless / server Chromium backends (notably SwiftShader and certain Linux Mesa / ANGLE builds) that hint causes `requestAdapter()` to return null where the unbounded request succeeds — there's no discrete GPU to match the preference against and Chromium won't fall back to the integrated path automatically.

Dropped the hint. We don't have a discrete-vs-integrated selection need here; any working device is better than none. Also added `console.warn` diagnostics to the probe's `null`-adapter, `null`-device, and `catch` branches so probe failures surface in the `gosx perf` console-capture section instead of silently disabling WebGPU.

## v0.17.17

Fix for the "shader / context-loss symptoms when forcing the GPU renderer" bug report against v0.17.15 / v0.17.16.

### Root cause: canvas tainted before device was verified

`createSceneWebGPURenderer` in `16a-scene-webgpu.js` called `canvas.getContext("webgpu")` synchronously at factory construction time, **before** any adapter or device had been confirmed. Once a canvas has been bound to a `webgpu` context it can't be reused for `webgl2` / `webgl`, so any subsequent WebGL fallback fails.

The pre-v0.17.17 sequence that reproduced the bug:

1. Module-level probe in 16z: `requestAdapter()` resolves with a non-null adapter (SwiftShader, partial mobile GPU, broken ANGLE backend) — probe flips `_webgpuAdapterReady = true`
2. `sceneWebGPUAvailable()` returns true, scene mount calls `createSceneWebGPURenderer(canvas)`
3. Inside the factory: `canvas.getContext("webgpu")` taints the canvas
4. `startInit()` is called, async `adapter.requestDevice()` throws — device creation actually fails on this backend
5. `initFailed = true`, `render()` becomes a no-op
6. Mount has no working renderer and **no clean canvas to fall back to WebGL with** — the scene never renders, user sees "no shaders, context loss"

The pre-v0.17.17 probe only verified `requestAdapter()`; it never attempted `requestDevice()`. That's the gap — adapters are available on many backends where devices aren't.

### Fix: full-lifecycle probe + synchronous factory

**`16z-scene-webgpu-probe.js`** now chains `requestAdapter().then(a => a.requestDevice()).then(d => { ... })` and only flips `_webgpuAdapterReady = true` when the full chain succeeds. Partial implementations (adapter OK, device fails) are detected at probe time, so `sceneWebGPUAvailable()` returns false and the canvas is never touched. The probe caches the device and exposes it through `window.__gosx_scene3d_webgpu_probe()`:

```js
window.__gosx_scene3d_webgpu_probe = function() {
  return {
    adapter: _webgpuAdapterProbe,
    device: _webgpuDeviceProbe,
    ready: _webgpuAdapterReady,
  };
};
```

Device-loss after probe resolution also invalidates the probe (`device.lost.then(...)`), so a transient success followed by device death still lets the next mount fall through to WebGL.

**`createSceneWebGPURenderer`** in `16a-scene-webgpu.js` is now synchronous. It reuses the probed device instead of requesting a fresh one:

```js
var probe = _externalProbe();
if (!probe || !probe.ready || !probe.adapter || !probe.device) return null;
var adapter = probe.adapter;
var device = probe.device;
// Only NOW taint the canvas.
var gpuCtx = canvas.getContext("webgpu");
if (!gpuCtx) return null;
```

The previous two-stage `.then(requestAdapter).then(requestDevice)` setup inside `startInit` was collapsed into a synchronous `initGPUResources` IIFE wrapped in `try/catch` — if any step fails (shader compile, buffer allocation, texture creation) the factory returns null with a warning.

### Net effect

- **Headless Chromium / SwiftShader / CI**: probe catches the device-creation failure at page load, `sceneWebGPUAvailable()` stays false, mount uses WebGL with an **untouched canvas**, scene renders normally. No shader errors, no context loss.
- **Real Chrome desktop with working Vulkan/D3D12**: probe succeeds, factory reuses the probed device (no extra round-trip), WebGPU renderer works as before.
- **Any backend where adapter works but device creation fails**: same clean fallback to WebGL.

The bug-fix was validated against m31labs.dev in chromedp headless: 586+ frames rendered cleanly, no console errors, only the probe's own diagnostic warning (`WebGPU probe failed: requestDevice returned null`) surfacing — which is exactly the signal the fix is supposed to produce.

## v0.17.16

Two features shipped together: the second-pass WebGPU renderer code split, and a new `perf compare` subcommand for diffing two profile reports.

### Scene3D WebGPU renderer moved to a lazy sub-feature chunk

Coverage data from v0.17.14 on the m31labs.dev homepage showed that **only 26% of `bootstrap-feature-scene3d.js` (643 KB) was actually executed** on a WebGL-rendered galaxy page — about 475 KB of dead bytes. A big chunk of that was `16a-scene-webgpu.js` (108 KB) and `16b-scene-compute.js` (28 KB): the WebGPU renderer and compute-particle code sitting dead in the bundle on every page that uses WebGL.

This release splits them out into a new async bundle `bootstrap-feature-scene3d-webgpu.js` (120 KB raw / ~55 KB gzip) that loads only when `navigator.gpu` exists. The main `bootstrap-feature-scene3d.js` shrinks from **643 KB → 527 KB (-117 KB / -18%)** on every WebGL page — Safari, Firefox on most platforms, and any page with `ForceWebGL`.

Structural changes:

- **`10-runtime-scene-core.js`** extends `window.__gosx_scene3d_api` with the PBR/shadow/post-fx helpers the webgpu renderer needs (`scenePBRDepthSort`, `scenePBRObjectRenderPass`, `scenePBRProjectionMatrix`, `scenePBRViewMatrix`, `sceneShadowLightSpaceMatrix`, `sceneShadowComputeBounds`, `resolvePostFXFactor`, `resolveShadowSize`, `sceneColorRGBA`). These are function declarations in files 11-16 of the main scene3d bundle, hoisted into the IIFE scope, so the `__gosx_scene3d_api` literal in file 10 captures them via `typeof X === "function" ? X : undefined` guards.

- **`16z-scene-webgpu-probe.js`** (new, stays in main scene3d bundle) owns the `navigator.gpu.requestAdapter()` probe and the `sceneWebGPUAvailable()` / `createSceneWebGPURendererOrFallback()` stubs. The stubs dispatch to `window.__gosx_scene3d_webgpu_api.createRenderer(canvas)` if and only if the sub-chunk has loaded AND the adapter probe succeeded. (This file is reworked in v0.17.17 to also verify device creation.)

- **`26e-feature-scene3d-webgpu-prefix.js` / `26e-feature-scene3d-webgpu-suffix.js`** (new) wrap the sub-chunk as its own IIFE. The prefix destructures all shared helpers from `window.__gosx_scene3d_api`. The suffix publishes the renderer factory to `window.__gosx_scene3d_webgpu_api`.

- **`16a-scene-webgpu.js`** drops its inline adapter probe (now owned by 16z) and reads the shared probe via `_externalProbe()`.

- **`island/island.go` `RenderEntrypoints`** emits a gated inline loader right after the main scene3d script tag:

```html
<script>if(navigator.gpu){var s=document.createElement('script');s.defer=true;s.dataset.gosxScript='feature-scene3d-webgpu';s.src="...";document.head.appendChild(s);}</script>
```

Safari and Firefox-on-most-platforms skip the download entirely because `navigator.gpu` doesn't exist; Chromium browsers fetch the sub-chunk in parallel with the main scene3d chunk so the mount can pick WebGPU on the first render when available.

Build manifest, asset copy, runtime asset resolver, HTTP handler allow-list, and version-compat path whitelist all updated to include the new bundle (`bootstrap-feature-scene3d-webgpu.js`) alongside the main scene3d one.

*Note: v0.17.16 shipped with two latent bugs in the inline loader that weren't discovered until v0.17.18 — the `\x3c/script>` raw-string issue and the `powerPreference: "high-performance"` probe problem. Both fixed subsequently. The split itself was correct; only the gating script was broken.*

### `gosx perf compare` — side-by-side profile diff

New subcommand: `gosx perf compare baseline.json candidate.json`. Reads two `gosx perf --json` reports, diffs every tracked metric (TTFB, DCL, LCP, CLS, long-task count/total, TBT, scene p50/p95/p99, hub bytes, network bytes, JS coverage ratio/used/total), and prints a table with baseline/candidate/Δ%/status columns. Metrics that move the wrong way by more than `--threshold` (default 5%) are marked `⚠ regression`; improvements get `↓ improved` or `↑ improved` depending on direction. Anything under the threshold prints `~`.

Exit code is 1 if any metric regressed beyond threshold, making it CI-gateable. `--json` flag dumps the comparison as JSON.

New exports in `perf/compare.go`:

- `LoadReport(path)` — reads a JSON report, normalizes single-page reports so `Pages[0]` is always populated
- `CompareReports(baseline, candidate)` — produces a `Comparison{Metrics []ComparedMetric}`
- `FormatComparison(cmp, threshold)` — renders the side-by-side table
- `AnyRegression(cmp, threshold)` — boolean for CI gating
- `ComparedMetric.IsRegression(threshold)` — per-metric check respecting `Direction` (LowerBetter for timing metrics, HigherBetter for coverage ratio)

First in-anger use: diffing m31labs.dev before/after the WebGPU split + kinetic.js minification deploy showed **LCP -50%, TBT -60%, long-task total -52%, JS shipped -17%** on Pixel 7 @ 4× CPU throttle.

## v0.17.15

New feature in `perf`: heap snapshot capture via CDP `HeapProfiler.takeHeapSnapshot`.

- **`perf.TakeHeapSnapshot(d *Driver)`** and **`perf.TakeHeapSnapshotAfterGC(d *Driver)`** stream the snapshot through Chrome's `EventAddHeapSnapshotChunk` sequence, concatenate the JSON chunks, and return the assembled document as bytes ready to write to a `.heapsnapshot` file. Load the file in Chrome DevTools' Memory panel for retainer analysis, leak detection, and before/after comparisons. The `AfterGC` variant runs `HeapProfiler.collectGarbage` before capturing so the snapshot reflects live retention rather than ephemeral allocation churn.

- **`perf.QueryMemoryStats(d *Driver)`** reads `performance.memory` plus document DOM node count via a single JS eval and returns a lightweight `MemoryStats{JSHeapUsedMB, JSHeapTotalMB, JSHeapLimitMB, DOMNodeCount}`. Cheap enough to run between scenario interactions for delta checks without paying the full snapshot cost.

- **`gosx perf --heap-snapshot <path>`** CLI flag captures the final page state after all interactions have run, GC'd first. On m31labs.dev the homepage snapshot lands at ~5 MB — small enough to ship as a CI artifact for diffing.

Like v0.17.14's coverage capture, both `HeapProfiler.enable()` and `TakeHeapSnapshot` are called via `cdp.WithExecutor(d.ctx, chromedp.FromContext(d.ctx).Target)` because their Do methods return non-`error` values and don't satisfy the `chromedp.Action` interface.

## v0.17.14

JS block-level coverage capture: per-script used-vs-total byte breakdown from CDP `Profiler.startPreciseCoverage`. The measurement that tells you how much of each shipped bundle is actually executing on a given page.

### `perf.CaptureCoverage(d, during func() error) ([]CoverageEntry, error)`

Wraps a driver callback with `Debugger.enable` + `Profiler.enable` + `Profiler.startPreciseCoverage(WithCallCount(false), WithDetailed(true))`. After the callback runs, pulls coverage via `Profiler.takePreciseCoverage` and resolves each script's total source size via `Debugger.getScriptSource(scriptID)` — Chrome's coverage only reports executed byte ranges, not absolute sizes, and network `Content-Length` is unreliable for streamed scripts.

The coverage algorithm is worth documenting because the Chrome block format is easy to get wrong. With detailed block-level coverage enabled and call counts disabled, Chrome emits per-function `FunctionCoverage{Ranges []CoverageRange}` where `range.Count > 0` means executed and `range.Count == 0` means unreached. Count-0 ranges are non-overlapping leaves (if a block never ran, no child range was emitted inside it), so the correct `used` calculation is:

```go
unused := 0
for _, fn := range script.Functions {
  for _, r := range fn.Ranges {
    if r.Count == 0 {
      unused += int(r.EndOffset - r.StartOffset)
    }
  }
}
used := total - unused
```

The first version of this code summed `count > 0` ranges directly and produced "100% used" for every script (because outer function ranges always include their inner unused blocks). The `total - unused` formulation is correct for both partial functions and whole-function count=0 (never-called) cases.

Results sorted by unused bytes descending so the biggest split opportunities surface first. `gosx perf --coverage` emits a `JS Coverage` section in `FormatTable`:

```
  JS Coverage (used / total)
      …e/bootstrap-feature-scene3d.e10b9a8e2f70a20e.js  26.0%   167.4 KB / 643.1 KB
      …s/runtime/bootstrap-runtime.0ce4c5ebe39aaf9f.js  30.4%    56.1 KB / 184.5 KB
      /gosx/bootstrap-feature-engines.js                 8.7%     5.3 KB /  61.4 KB
      /kinetic.js                                       41.0%     9.5 KB /  23.1 KB
    Overall                 26.2%  (10 scripts, 261.2 KB used / 997.9 KB total)
```

### The first measurement immediately justified the tool

On the m31labs.dev homepage galaxy page, **only 26.2% of 998 KB of shipped JavaScript is actually executing** — 737 KB of dead code per page load. Top offenders:

- `bootstrap-feature-scene3d.js`: 26% used (476 KB dead) — primarily the WebGPU renderer + compute-particle code sitting unused on WebGL pages. Became the second-split target in v0.17.16.
- `bootstrap-runtime.js`: 30% used (128 KB dead) — baseline runtime.
- `bootstrap-feature-engines.js`: **8.7% used** (56 KB dead) — mostly engine-kind-specific mount paths and dispose code that doesn't run during a single page load measurement.
- `kinetic.js`: 41% used (13 KB dead) — custom typography animation library, became the minification target in the m31labs.dev deploy.

## v0.17.13

Three independent `perf` features that together close the "how do we reproduce mobile perf issues in the headless profiler" gap.

### Console + exception capture

**`perf.StartConsoleCapture(d)`** installs CDP `Runtime.consoleAPICalled` and `Runtime.exceptionThrown` listeners. Captures warnings, errors, asserts, and uncaught exceptions by default (info/log/debug filtered out as noise); `StartConsoleCaptureAll` keeps everything. Entries land in `PageReport.ConsoleEntries` and print in a new `Console` section of `FormatTable`:

```
  Console
    Counts                  exceptions:1  errors:1  warnings:1
      warn    this is a warning from the test page
      error   this is an error from the test page
      exception Error: uncaught explosion   at file:///tmp/test.html:9:26
```

Silent errors are one of the most common causes of "feature broken on mobile" bugs that don't surface in dev, so the capture is on by default in every `gosx perf` run — no opt-in flag.

### CPU throttling

**`perf.ApplyCPUThrottle(d, rate)`** wraps `Emulation.setCPUThrottlingRate`. `rate=1` is realtime, `rate=4` is the direct analogue of Chrome DevTools' "4× slowdown" preset (mid-range phone), `rate=6` is low-end. Must be called before `Navigate` for the throttle to cover the initial page load — done automatically by `RunScenario` when `Scenario.CPUThrottle > 1`.

### Mobile device emulation

**`perf.ApplyMobileEmulation(d, profile MobileProfile)`** sets viewport width/height, device scale factor, mobile flag, and user-agent override via `Emulation.setDeviceMetricsOverride` + `Emulation.setUserAgentOverride`. Built-in presets:

- `Pixel7` — 412×915 @ 2.625×, Chrome Android UA
- `iPhone14` — 390×844 @ 3×, Safari iOS UA

**`gosx perf --throttle 4 --mobile pixel7`** — the direct answer to "my site is slow on mobile and I can't reproduce it in desktop devtools".

First run against m31labs.dev at Pixel 7 @ 4× throttle: TBT jumped from 83ms desktop to **849ms** mobile, 24 long tasks, 254ms EventDispatch + 253ms RunMicrotasks at the top of the trace summary — exactly the signal needed to diagnose the reported mobile scroll jank, and the trigger for the WebGPU renderer split in v0.17.16.

## v0.17.12

CDP trace capture + hot-event summary. The piece of instrumentation that gives `gosx perf` a flame chart story.

### `perf.CaptureTrace(d, during)`

Wraps a driver callback with CDP `Tracing.start` / `Tracing.End` using `TransferModeReturnAsStream` + `StreamFormatJSON`, listens for `EventTracingComplete`, drains the returned `IO.StreamHandle` via `IO.Read` until EOF, and returns a Chrome DevTools-format JSON trace. Default category set matches DevTools' Performance panel:

```
devtools.timeline, v8.execute,
disabled-by-default-devtools.timeline,
disabled-by-default-devtools.timeline.frame,
disabled-by-default-devtools.timeline.stack,
disabled-by-default-v8.cpu_profiler,
disabled-by-default-v8.cpu_profiler.hires,
blink.user_timing, latencyInfo, loading, toplevel
```

`blink.user_timing` is the important one — that's where `gosx:ready`, `scene3d-render`, `gosx:island:hydrate:*`, and `gosx:dispatch:*` measures from `instrument.js` show up. Load the resulting `.trace.json` in chrome://tracing, Perfetto (ui.perfetto.dev), or Chrome DevTools' Performance panel (Load profile…).

`CaptureTraceWithCategories` is the custom-category variant. Chunks delivered via `EventDataCollected` are handled as a fallback for older Chrome builds that don't deliver the stream format.

### `perf.SummarizeTrace(trace, topN, minMs)` / `FormatTraceSummary`

Parses a captured trace and returns the top-N longest main-thread events matching an interesting-subset filter (`EvaluateScript`, `v8.compile`, `v8.parseOnBackground`, `CompileScript`, `FunctionCall`, `EventDispatch`, `RunMicrotasks`, `FireAnimationFrame`, `Layout`, `UpdateLayoutTree`, `Paint`, `ParseHTML`, `WebAssembly.Compile`, `WebAssembly.Instantiate`). The toplevel/`RunTask` shells are excluded because they double-count the real work they wrap.

`gosx perf --trace <path>` writes the `.trace.json` and also prints the summary as a `Trace Summary` section in the report:

```
  Trace Summary
    Saved to                /tmp/m31.trace.json
    Top main-thread events
      EventDispatch             276.9ms
      RunMicrotasks             276.8ms
      EvaluateScript             99.6ms  https://m31labs.dev/kinetic.js
      EventDispatch              55.0ms
      FunctionCall               47.9ms  …bootstrap-runtime.0ce4c5ebe39aaf9f.js
      v8.parseOnBackground       32.7ms  …bootstrap-feature-scene3d.e10b9a8e2f70a20e.js
      ...
```

The first measurement on m31labs.dev immediately validated the v0.16.3 Scene3D code-split (v0.17.1): `bootstrap-feature-scene3d.js` parse (33ms) lands on `v8.parseOnBackground` — a background thread — confirming the split moved the bulk of the parse cost off the main thread. The largest remaining main-thread tasks are `EventDispatch` / `RunMicrotasks` (WASM startup + hydration chain), not script parse. The summary also caught `kinetic.js` at 100ms EvaluateScript on mobile, which became the minification target.

## v0.17.11

Small but load-bearing fix in `perf/driver.go`: `Driver.Evaluate` now awaits Promise return values via `chromedp.Evaluate` + `runtime.EvaluateParams.WithAwaitPromise(true)`.

Previously any `(async () => { ... })()` expression in REPL eval or query helper code returned a pending Promise that JSON-serialized to `{}`, making sleep-based inspection useless. After the fix, REPL users can write:

```
eval (async () => { await new Promise(r=>setTimeout(r,4000)); return {adapter: !!await navigator.gpu.requestAdapter()}; })()
```

and actually get back the resolved value. Added `github.com/chromedp/cdproto` as a direct dependency (it was indirect before).

## v0.17.10

Filled a gap in the `scene3d-render` performance instrumentation: the WebGPU renderer was missing the `performance.mark` / `measure` pairs that the WebGL renderer has had since the bench overlay shipped.

Without the marks, `gosx perf` reported zero `Scene3D` frame stats on any page that actually ran on the WebGPU backend — the `sceneObserver` in `instrument.js` never saw any `scene3d-render` measures. `client/js/bootstrap-src/16a-scene-webgpu.js::render` now brackets the render body with the same opt-in mark pair, gated on `window.__gosx_scene3d_perf`:

```js
var perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
if (perfEnabled) performance.mark("scene3d-render-start");
// ... render body ...
if (perfEnabled) {
  performance.mark("scene3d-render-end");
  performance.measure("scene3d-render", "scene3d-render-start", "scene3d-render-end");
  performance.clearMarks("scene3d-render-start");
  performance.clearMarks("scene3d-render-end");
}
```

Early returns (no bundle data, zero-sized canvas) are now positioned before the `perfEnabled` check so they don't leave stale start marks around.

## v0.17.9

Extended runtime instrumentation pass — closes out the "batteries-included browser profiler" story with Core Web Vitals, long-task detection, GoSX runtime throughput counters, GPU tier introspection, and a network resource waterfall. The single biggest addition to `gosx perf` since v0.17.0.

### Core Web Vitals

Three `PerformanceObserver` subscriptions in `instrument.js` for the standard web-vitals triad:

- **Largest Contentful Paint** (`PerformanceObserver({type: "largest-contentful-paint"})`) — uses the latest entry since LCP can update multiple times. Rated good (<2500ms) / needs improvement (2500-4000ms) / poor (>4000ms) in the report.
- **Cumulative Layout Shift** (`{type: "layout-shift"}`) — accumulates non-user-input shifts. Rated <0.1 / 0.1-0.25 / >0.25.
- **First Input Delay** (`{type: "first-input"}`) — uses `processingStart - startTime`. Rated <100ms / 100-300ms / >300ms.

All three land in `PageReport` and print in a new `Core Web Vitals` section with the good/needs-improvement/poor rating annotation.

### Main-thread blocking

PerformanceObserver on `{type: "longtask"}` (the single most valuable signal for scroll jank diagnosis). Any main-thread task over 50ms is captured with `{name, duration, startTime}`. The report surfaces:

- Long task count
- Long task total (summed duration)
- **Total Blocking Time** — `sum(max(0, duration - 50))` over all long tasks, matching Lighthouse's TBT definition
- Top 5 longest tasks with their names and offsets

### Runtime throughput counters

Object.defineProperty traps on `__gosx_set_shared_signal` and `__gosx_get_shared_signal` count per-dispatch signal writes/reads, so a slow dispatch can be diagnosed as signal-bound (too many writes) or reconcile-bound (too many DOM diffs). Hub message counters (`hubMessageCount`, `hubMessageBytes`, `hubSendCount`) extended to track byte totals in addition to event counts.

### GPU tier detection

`window.__gosx_perf_webgl_info()` reports which GPU backend the engine is actually using plus what the browser *could* provide:

```
  GPU Context
    Tier                    webgl2 (best available: webgpu)
    Version                 WebGL 2.0 (OpenGL ES 3.0 Chromium)
    Vendor                  Google Inc. (Google)
    Renderer                ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device...))
    Max texture size        8192
    Extensions              29 extensions
    Browser supports        WebGPU, WebGL2, WebGL1
```

Reads `UNMASKED_VENDOR_WEBGL` / `UNMASKED_RENDERER_WEBGL` via `WEBGL_debug_renderer_info` when available. Checks `navigator.gpu`, `canvas.getContext("webgl2")`, and `canvas.getContext("webgl")` separately for a complete capability picture. If the tier actually in use differs from the best available, the report flags it (`webgl2 (best available: webgpu)` is a hint the engine is leaving capacity on the table).

The scene engine can opt-out of the auto-detection by setting `canvas.__gosx_scene_tier = "webgpu"` or `data-gosx-scene-tier="webgpu"` so the introspection doesn't need to probe the canvas itself.

### Network resource waterfall

New `PerformanceObserver({type: "resource"})` collects every resource timing entry. `gosx perf --waterfall` adds a `Resource Waterfall` section to the report showing per-resource transfer size, duration, start time, and connection type. Also tracks:

- `TotalBytesTransferred` — sum of `transferSize` across all resources
- `BlockingResourceMs` — duration of the longest render-blocking resource

Both surface in the default `Network` section; the waterfall flag is opt-in because the detail is verbose.

### Chrome launch flag

Added `--enable-unsafe-webgpu` to the chromedp allocator options so WebGPU probes can succeed on systems with a real GPU driver. Flag is harmless on WSL / headless CI where the driver isn't present — `requestAdapter()` just returns null as before — but enables WebGPU on real Chrome desktop for the newly-added tier detection.

## v0.17.8

Final scene3d split stabilization fix: remove a false `emit` export from the runtime API bridge.

`00-textlayout.js` was exporting `emit` as part of `window.__gosx_runtime_api`, but `emit` is actually a nested function inside `segmentBrowserWordRun` — not a top-level function in the IIFE scope. The export line captured `undefined`, which the scene3d chunk's prefix then destructured as `var emit = runtimeApi.emit || ...`. Harmless in isolation, but the fallback produced a `ReferenceError: emit is not defined` when the runtime crashed during the scene3d chunk evaluation phase on first load.

Fixed by removing `emit` from the runtime API export. Nothing in the scene3d chunk was actually using it — the false positive came from a grep-based extraction earlier in the split series.

## v0.17.7

Bridge the runtime API for scene3d chunk cross-IIFE access: introduces `window.__gosx_runtime_api` as the formal contract between the runtime bundle and the scene3d feature chunk. `00-textlayout.js` exports `setAttrValue`, `setStyleValue`, `gosxSubscribeSharedSignal`, `setSharedSignalValue`, `gosxTextLayoutRevision`, `normalizeTextLayoutOverflow`, `layoutBrowserText`, `applyTextLayoutPresentation`, and `onTextLayoutInvalidated` onto the namespace. The scene3d chunk's prefix (`26d-feature-scene3d-prefix.js`) destructures from it with fallbacks, so a missing runtime API degrades to a no-op rather than a hard reference error.

## v0.17.6

Corrected the Scene3D code split boundary after v0.17.5's partial re-enablement. The runtime bundle now includes file `10a-runtime-utils.js` (extracted from file 10), which contains the pure runtime utilities — `loadManifest`, `loadRuntime`, `fetchProgram`, `loadScriptTag`, `engineFrame`, `cancelEngineFrame`, `queueInputSignal`, `createInputProvider`, `capabilityList`, `sceneNumber`, `sceneBool`, `clearChildren`. These are the functions file 10's infrastructure needs without pulling in any scene-specific state.

The scene3d chunk contains the full file `10-runtime-scene-core.js` plus files 11-20 (scene math through scene mount). Files 11-20 depend heavily on symbols defined in file 10 (the `engineFactories` registry, `__gosx.engines` map, etc.), so the split point must be above file 10 — hence 10a carries just the standalone utilities and file 10 stays with the scene code.

## v0.17.5

Restore clean runtime sources and re-enable the Scene3D split after the v0.17.4 revert. The previous attempt left extra function declarations in the runtime bundle which caused "identifier already declared" errors when the scene3d chunk loaded on top. Reverted the runtime sources to a clean baseline (00 + 05 + 10a + 26) and re-ran the extraction so files 11-20 live exclusively in the scene3d chunk with no shadow declarations in the runtime.

## v0.17.4

Temporary revert of the Scene3D split on pages that use Scene3D engines: `usesSelectiveRuntimeBootstrap()` gains back a `!r.hasSceneEngines()` guard so scene3d pages fall back to the monolithic `bootstrap.js`. Runtime wasn't stable yet across the IIFE boundary and the revert bought time to fix the underlying issue — landed cleanly in v0.17.5 / v0.17.6 / v0.17.7 and the guard was removed in v0.17.9.

## v0.17.3

`<script defer>` instead of `<script async>` for the Scene3D feature chunk emit. The v0.17.1 version used `async` on the theory that the scene3d chunk was independent enough to run out of order, but scene3d's hydration depends on the main runtime bundle already being loaded. `defer` guarantees the parser-inserted script runs after the runtime and preserves document order, which was the right semantics all along.

## v0.17.2

`gosx build --prod` now copies `bootstrap-feature-scene3d.js` into the dist asset output. The v0.17.1 build generated the file via `client/js/build-bootstrap.mjs` but the prod asset-copy loop in `cmd/gosx/build.go` missed it, so deployments shipped a `<script src="...bootstrap-feature-scene3d.js">` tag referring to a 404 URL. Added the file to both the `manifest.Runtime.BootstrapFeatureScene3D` registration and the dist copy list.

## v0.17.1

**Scene3D bootstrap code splitting** — split the 894 KB monolithic `bootstrap.js` into a smaller `bootstrap-runtime.js` (blocking, ~185 KB) + `bootstrap-feature-scene3d.js` (async, ~640 KB) so pages that use Scene3D don't force users to parse the full runtime on the main thread.

The goal was to address reports of mobile Safari / Firefox scroll jank during page load: the main thread was pegged for 580+ ms parsing and executing the monolith before any user input could be processed. Splitting out the scene graph pipeline (files 11-20 of the bootstrap sources — scene math, geometry, materials, lighting, draw-plan, post-fx, WebGL/WebGPU renderers, compute, input, canvas, glTF, animations, mount) into a `<script defer>` chunk lets the main runtime load and hydrate islands while the scene3d chunk downloads and parses in the background.

Structural wiring:

- **`client/js/build-bootstrap.mjs`** now emits four bundles: `bootstrap.js` (monolith for pages without feature chunks), `bootstrap-lite.js`, `bootstrap-runtime.js` (runtime + islands + engines + hubs), and `bootstrap-feature-scene3d.js` (files 10–20, the scene graph pipeline).

- **`client/js/bootstrap-src/26d-feature-scene3d-prefix.js` / `...-suffix.js`** (new) wrap the scene3d chunk as its own IIFE. The prefix declares the symbols the IIFE needs from the runtime's scope (file 00's text layout state, file 10's registries).

- **`client/js/bootstrap-src/10-runtime-scene-core.js`** exposes scene utilities on `window.__gosx_scene3d_api` for future cross-IIFE access — the foundation the v0.17.16 WebGPU split builds on.

- **`island/island.go::RenderEntrypoints`** emits a `<script defer data-gosx-script="feature-scene3d" src="...">` tag right after the main bootstrap tag on any page that has a Scene3D engine. Pages without scene engines get nothing extra.

- **`island/island.go::usesSelectiveRuntimeBootstrap`** decides whether to emit the selective runtime or fall back to the monolith based on feature chunk presence + manifest state.

- **`buildmanifest/manifest.go`** extends `RuntimeAssets` with `BootstrapRuntime`, `BootstrapFeatureIslands`, `BootstrapFeatureEngines`, `BootstrapFeatureHubs`, and `BootstrapFeatureScene3D` fields; `cmd/gosx/build.go` hashes each file into the manifest and copies them to the dist runtime dir.

This release was the first of several — v0.17.2 through v0.17.8 all stabilized different corners of the split (asset copy, script attrs, IIFE scope boundaries, cross-chunk APIs). v0.17.9 finalized the split; v0.17.16 added a second-pass sub-feature for the WebGPU renderer.

## v0.17.0

Birth of **`gosx perf`** and **`gosx repl`** — a batteries-included, Go-native browser profiler and interactive runtime explorer shipped as subcommands of the main `gosx` CLI. The motivating need: existing browser instrumentation stories (Superpowers Chrome MCP, manual DevTools, Puppeteer wrappers) were awkward for testing GoSX-specific runtime behavior and didn't integrate with `.dmj` test files the rest of the framework uses for SSR benchmarks.

### Architecture

All new code lives under `perf/` as a standalone Go package, plus three CLI entry points in `cmd/gosx/`:

- **`perf.Driver`** — chromedp wrapper that manages Chrome allocator + context lifecycle, `Navigate`, `WaitReady`, `Evaluate`, and `Close`. Driver launches headless Chrome (or headed via `WithHeadless(false)`) via `chromedp.NewExecAllocator` + `chromedp.NewContext` with a configurable overall timeout.
- **`perf.FindChrome`** — cross-platform Chrome binary discovery that checks standard install locations on Linux/Mac/Windows plus the `CHROME_PATH` env override. Returns the first working binary or an error with diagnostic context.
- **`perf/instrument.js` + `perf.InjectDriver(d)`** — GoSX-aware instrumentation script injected via `Page.addScriptToEvaluateOnNewDocument` before any page scripts run. Installs Object.defineProperty traps on `__gosx_runtime_ready`, `__gosx_hydrate`, `__gosx_action`, and `__gosx_hydrate_engine` to bracket each call with `performance.mark` + `measure`. Separately listens for `gosx:ready` and `scene3d-render` PerformanceObserver entries, and wraps `WebSocket.prototype.onmessage` / `send` to count hub messages.
- **`perf.QueryPerformanceMeasures` / `QueryNavigationTiming` / `QueryHeapSize` / `QuerySceneFrames` / `QueryIslandHydrations` / `QueryDispatchLog` / `QueryHubMessages`** — typed CDP query helpers in `perf/query.go` that return structured `[]PerfEntry` slices for each kind of captured data.
- **`perf.Report`, `perf.PageReport`, `perf.SceneMetric`, `perf.IslandMetric`, `perf.InteractionMetric`, `perf.FrameStats`** — the metric data model (`perf/metrics.go`). `Report` holds multi-page scenarios with an embedded `PageReport` for single-page backward compat. `FrameStats` precomputes p50/p95/p99/max/mean from a frame-time slice.
- **`perf.CollectPageReport(d, url)`** — orchestrates all the query helpers into a single `PageReport` for the currently loaded page. Conditionally populates `Scene` metrics when `__gosx_perf.firstFrame` is set.
- **`perf.FormatTable(r)` / `FormatJSON(r)`** — output formatters. Table formatter is the default, JSON is opt-in via `--json`.
- **`perf.Interaction`, `perf.Scroll`, `perf.Click`, `perf.Type`** — user-interaction primitives that drive DOM via CDP.
- **`perf.Scenario` / `perf.RunScenario(s)`** — the top-level profiling session runner. Takes URLs, frame sample count, interaction list, timeout, and headless flag; returns a `*Report`. Orchestrates driver launch, instrumentation inject, per-URL navigate+collect, post-navigation frame wait, per-interaction dispatch measurement, and driver close.
- **`perf.Assertion` / `ParseAssertion` / `EvalAssertions`** — metric assertion engine. Assertions are string expressions like `dcl < 500` or `scene.p95 < 16` that the runner evaluates against a populated `Report`. Failed assertions produce human-readable error messages and exit the CLI with non-zero status — the foundation for CI gating.
- **`perf.Recorder` + `StartRecording` / `Stop`** — video recording via CDP `Page.startScreencast` / `stopScreencast`. Captures base64 frames, writes an MJPEG-ish sequence via ffmpeg or raw screenshots to a directory. Enabled via `--record <path>`.
- **`perf.RunREPL(d, url)`** — interactive command-line console for driving a live browser. Commands: `help`, `islands`, `engines`, `signals`, `dispatch <id> <handler>`, `scene`, `profile`, `scroll <px>`, `click <sel>`, `type <sel>:<text>`, `navigate <url>`, `record <path>`, `perf`, `eval <js>`, `heap`, `exit`. The `eval` command evaluates arbitrary JS against the page context — the killer feature for exploring GoSX's runtime internals without DevTools.
- **`perftest.GoSXPerf`** — a `testing.T`-integrated wrapper (`perf/perftest/perftest.go`) that `.dmj` files can call from a Go-compiled test. Wraps `RunScenario` and exposes the resulting report through a fluent API (`TTFBMs`, `LCPMs`, `SceneFrameBudget`, etc.). `.dmj` files can now mix SSR benchmarks with browser-side assertions against a real Chromium runtime.

### `cmd/gosx/perf.go` / `cmd/gosx/repl.go`

`gosx perf <url>` parses flags (`--frames`, `--click`, `--scroll`, `--type`, `--json`, `--timeout`, `--headless`, `--record`, `--assert`), builds a `perf.Scenario`, runs it, and prints the report. Multi-URL scenarios run sequentially against the same driver. `--assert` is repeatable and CI-gates on failure.

`gosx repl <url>` launches a browser in non-headless mode, injects the driver, navigates, waits for ready, and drops into `RunREPL`. Default URL is `http://localhost:3000` when invoked without an argument or with `--dev`.

### Dependencies added

`github.com/chromedp/chromedp` and `github.com/chromedp/cdproto` as direct deps. Goes into `go.mod` with the `@v0.15.1` pin for chromedp and the then-current pin for cdproto. The chromedp choice was deliberate: Go-native, no Node.js runtime, no puppeteer/playwright wrapper, integrates cleanly with the existing `testing.T` flow.

### Other optimization wins in the v0.17.0 batch

Two bundled allocation improvements landed in the same release:

- **`client/vm` resolveElementAttrs** — single-allocation rewrite, plus the first set of island behavior benchmarks in `.dmj` form (`client/vm/bench.dmj`, `island/bench.dmj`). Cut SSR attr allocation further on top of v0.16.6's 35→14 work.
- **`island/program` decodeStringTable** — eliminated the intermediate `[]byte` allocation per entry in the binary program format decoder. Strings are now sliced directly out of the caller's backing buffer.
- **`island` + `route` SSR attr aliasing** — `renderResolvedAttrs` returns `node.Attrs` directly when a node has no events, skipping a copy in the no-event case. Also fixed a nested-router bench that was inadvertently measuring the 404 path due to a mismatched child-pattern prefix; dropped alloc count from 46 → 18 once the route actually matched.

## v0.16.6

Sixth performance pass across five packages: `client/vm` (island runtime VM), `scene` (residual SceneIR allocs), `markdown` (builder-based render), `client/enginevm` (material key hashing), and `island` (SSR resolved-node renderer).

### `client/vm` — island dispatch cut 35 → 14 allocs (-60%)

The `client/vm` package evaluates island expressions and reconciles DOM trees on the server — every user interaction on a GoSX island (click, input, change) runs through it. Baselined via a new `client/vm/bench.dmj` against the `CounterProgram` fixture, then attacked from five angles:

1. **`parseEventData` shortcut for empty event payloads.** The vast majority of handler dispatches come with `"{}"` or `""` as the event data. Skipping `json.Unmarshal` on those saves a `reflect.MakeMapWithSize` plus `json.decodeState` setup per dispatch.

2. **`appendNodeRefs` append-into-slice pattern.** The old `resolveNodeRefs` returned `[]int{idx}` per non-fragment node and callers appended with `append(prev, refs...)`. Every resolved node paid a single-element slice allocation. The new `appendNodeRefs(tree, out, nodeID) []int` appends directly into the caller's buffer.

3. **Lazy-allocate `resolveElementAttrs` output slices.** Container elements like `<div>` holding interpolated text were paying two guaranteed `make()` calls per resolve. Now lazy-init both `resolved` and `events` on first use. Also fused with `materializeDOMAttrs` into a single walk so elements with zero events simply alias the resolved slice as `domAttrs` with no copy.

4. **Pre-size `tree.Nodes` in `EvalTree`.** The resolved tree's node slice grew via append from empty, doubling 3-4× for a typical island. Pre-sizing to `len(program.Nodes)` eliminates the cascade.

5. **`strconv` for `Value.String` scalar paths.** `fmt.Sprintf("%d", int)` replaced with `strconv.FormatInt`. Int values (counter values, array indices — by far the most common case) drop from 30 ns / 1 alloc to **2 ns / 0 allocs**. Same treatment for `childPath` in reconcile.go which runs per reconcile-node.

Bench results (`./client/vm`, `CounterProgram` fixture):

| benchmark | before | after |
|---|---|---|
| `ResolveInitialTreeCounter` | 43 allocs, 1967 ns | **29 allocs, 1363 ns** |
| `ResolveInitialTreeForm` | 74 allocs, 3528 ns | **42 allocs, 2492 ns** |
| `DispatchIncrementCounter` | 35 allocs, 1906 ns | **14 allocs, 1066 ns** |
| `ReconcileCounterAfterSet` | 27 allocs, 1299 ns | **13 allocs, 717 ns** |
| `ValueIntToString` | 30.9 ns, 1 alloc | **2.0 ns, 0 allocs** |

### `scene` — SceneIR 95 → 34 allocs (-64%)

Two fixes on the residual `PropsSceneIr` hot path left over from v0.16.5:

1. **`intString` replaced with `strconv.Itoa`.** The hand-rolled digit builder allocated a fresh `[]byte` per iteration via `append([]byte{digit}, digits...)`. `strconv.Itoa` uses a stack-allocated 20-byte scratch buffer and returns a single string with zero heap allocations for non-negative values. This function is called from every `scene-object-N` / `scene-points-N` / `scene-label-N` / `scene-light-N` auto-ID generator, so it was on a hot loop per scene.

2. **Pre-size `graphLowerer.{objects, lights, anchors}`** to `len(g.Nodes)`. The lowerer started with nil slices and grew via append.

| benchmark | before | after |
|---|---|---|
| `PropsSceneIr` | 95 allocs, 11864 ns, 48 KB | **34 allocs, 8014 ns, 28 KB** |
| `PropsMarshalJson` | 189 allocs | **128 allocs** |

### `markdown` — render cut 67–80% allocs via builder rewrite

`renderNode` used ~25 `fmt.Sprintf` calls, one per AST node type. Rewritten to write directly into a shared `*strings.Builder` via a new `renderNodeInto`. Internal recursion (`renderChildrenInto`, `renderTableInto`) passes the same builder through, so subtrees no longer allocate intermediate strings. Top-level `renderNode` retains its string-returning API by wrapping `renderNodeInto` for external callers.

| benchmark | before | after |
|---|---|---|
| `RenderShortDoc` | 49 allocs, 1660 ns | **16 allocs, 738 ns** |
| `RenderLongDoc` | 255 allocs, 11481 ns | **52 allocs, 4680 ns** |

(Parse is still ~22k allocs for a short doc; that's a separate rewrite for another pass.)

### `client/enginevm` — `renderMaterialKey` / `sceneRGBAString`

`renderMaterialKey` builds an 8-field composite cache key for every material profile during Scene3D engine dispatch, and `sceneRGBAString` stringifies scene stroke/fill colors. Both used `fmt.Sprintf`; both now use `strings.Builder` + `strconv` directly.

### `island` — SSR resolved-node renderer

`renderResolvedNode` is the server-side HTML path for island subtrees. Rewritten to write into a shared builder via `renderResolvedNodeInto`, matching the `markdown` and `client/vm` patterns. Per-attribute `fmt.Sprintf` replaced with direct `WriteByte`/`WriteString` calls. `childProgramPath` gets the same `strconv.Itoa` + concat treatment as the `client/vm` version, and `hydrate.EventSlot` field construction switches from `fmt.Sprintf` to string concatenation (Go lowers multi-string `+` to a single `runtime.concatstring3/4` call).

## v0.16.5

Two independent fixes: a large-scale allocation win in the Scene3D JSON marshal path, and a cluster of browser-specific scroll jank issues on Firefox and iOS Safari.

### `scene.Props.MarshalJSON` — **1020 → 189 allocations (-81%)**

`Props.MarshalJSON` used to build a `map[string]any` tree via `sceneIR.legacyProps()` — roughly 900 interface-boxing allocations per scene marshal across every object/model/light/etc. numeric setter, plus nested `map[string]any` allocations for every sub-entity in the scene.

Every IR type in the scene package (`ObjectIR`, `ModelIR`, `PointsIR`, `InstancedMeshIR`, `ComputeParticlesIR`, `AnimationClipIR`, `LabelIR`, `SpriteIR`, `LightIR`, `EnvironmentIR`) already has proper `json` tags on every field. So reflection-based `json.Marshal(sceneIR)` produces the same wire shape directly — no map tree required.

`Props.MarshalJSON` now:

1. Builds the small base-props map (`width`/`height`/`camera`/`compression`/`capabilities` — too conditional to express via static struct tags)
2. Marshals `SceneIR` directly via `json.Marshal(sceneIR)`
3. Wires the result in as a `json.RawMessage` under the `"scene"` key

Two shape-preserving details required:

- **`ObjectIR.MarshalJSON`** uses a `type alias` shadow trick to override its `Points` field with a `[]linePointWire` — a `Vector3` variant that always emits `x`/`y`/`z` even when zero. The legacy map-based path always emitted all three coordinates, so `Vec3(0,2,0)` rendered as `{"x":0,"y":2,"z":0}`; `Vector3`'s default `omitempty` tag would silently drop the zeroes and give `{"y":2}` instead. Preserving the legacy shape avoids breaking any JS consumer that reads three coordinates unconditionally.

- **Each `PostEffectIR` concrete type** (`TonemapIR`, `BloomIR`, `VignetteIR`, `ColorGradeIR`) now implements `json.Marshaler` directly. Needed because these structs have unnamed fields — the custom marshalers emit the same `{kind, ...}` shape `legacyProps` used to build.

Two new tests pin the wire shape:

- `scene/marshal_golden_test.go` captures the canonicalized bytes of `Props.MarshalJSON` for the `benchMixedScene` fixture into `testdata/props_marshal_golden.json`. Byte-for-byte reference for future refactors.
- `scene/marshal_direct_test.go` verifies that `json.Marshal(sceneIR)` and `json.Marshal(sceneIR.legacyProps())` produce semantically equal JSON — the load-bearing invariant for the new fast path.

Benchmark (`./scene`, 20-mesh mixed fixture with post-FX and lights):

| benchmark | before | after |
|---|---|---|
| `PropsMarshalJson` | 66411 ns, 108383 B, **1020 allocs** | 71230 ns, 85088 B, **189 allocs** |

Time is ~7% slower because reflection-based marshaling is a bit more expensive per field than the map walker, but the 831 eliminated short-lived objects per scene marshal is a far bigger win for long-running servers under GC pressure.

### Firefox + iOS Safari scroll jank / stale canvas state

Three fixes addressing reports that scrolling a page containing a Scene3D canvas would lag on Firefox and iOS Safari, with the canvas sometimes showing a previous frame's content after the scroll had already stopped:

1. **Passive `visualViewport` listeners** (`client/js/bootstrap-src/05-document-env.js`). The document-environment observer registered `resize` and `scroll` handlers on `visualViewport` without `{ passive: true }`. iOS Safari treats any non-passive touch-path listener as potentially `preventDefault`-ing and blocks the scroll thread until the handler completes, which stacks frame drops during rubber-band scrolls. Same applies to the `window` `resize`/`orientationchange`/`pageshow` listeners — all got the passive flag.

2. **Passive IME keyboard-height listener** (`client/js/bootstrap-src/10-runtime-scene-core.js`). The `visualViewport` resize handler that queues the `$input.keyboard_height` signal also ran as non-passive. The handler only writes a signal, so it's safe to mark passive.

3. **Defer forced-sync layout into RAF** (`client/js/bootstrap-src/20-scene-mount.js`). **This was the biggest root cause.** `scheduleRender()` called `sceneViewportFromMount()` and `applySceneViewport()` synchronously on every scroll event. Both read `mount.getBoundingClientRect()` and `canvas.getBoundingClientRect()` — and `applySceneViewport` does a _second_ pair of reads after writing `canvas.width` / `canvas.height` for the label-layer positioning. That's two forced synchronous layouts per scroll event.

   Firefox coalesces scroll events at roughly 30Hz during active touch-scroll, so the forced layouts stacked up and the browser had to reflow mid-scroll — visible as jank and a frame of stale canvas content after the scroll stopped. iOS Safari exhibited the same symptom for the same reason.

   Moved both viewport calls inside the `engineFrame()` callback. Inside RAF the browser is already in a read phase (style + layout already resolved), so rect reads are cheap and the subsequent canvas writes batch naturally into the following compositor pass. The existing `scheduledRenderHandle` coalescing still dedupes multiple scroll events into a single RAF.

Bootstrap bundles (`bootstrap.js`, `bootstrap-lite.js`, `bootstrap-runtime.js`) rebuilt via `client/js/build-bootstrap.mjs`.

## v0.16.4

Performance patch focused on `gosx.RenderHTML` — the single function every page render in the framework passes through — plus smaller wins in `hub`, `server/cache.go`, and `server.nextRequestID`.

### `RenderHTML` — single-allocation rendering

Three changes in `node.go`:

1. **Pre-grow the output `strings.Builder`.** Added an `estimateRenderSize` walker that sums tag, attribute, and text bytes for a rough forecast of the final HTML size. Pre-growing with that estimate eliminates the 3-5 internal `bytes` doublings `strings.Builder` would otherwise perform during a typical page render. The forecast can under-shoot slightly (HTML escape expansion isn't counted) — that's fine because the Builder still grows on demand.

2. **`El()` pre-sizes its children slice** to `len(args)`. The previous implementation appended one child at a time starting from a nil slice, forcing two or three doublings for any element with more than a handful of children.

3. **`El()` aliases the first `AttrList` directly** into `n.attrs` instead of copying entries one by one. `Attrs()` builds a fresh `AttrList` per call so there's no sharing concern.

4. **`Attrs()` pre-sizes its result slice** to `len(pairs)`.

The cumulative effect brings every `RenderHTML` call to **1 allocation** — the Builder's underlying byte slice — regardless of tree depth:

| benchmark | before | after |
|---|---|---|
| `RenderHtmlSimple` | 37 ns, 24 B, 2 allocs | **33 ns, 16 B, 1 alloc** |
| `RenderHtmlWithAttrs` | 279 ns, 504 B, 6 allocs | **189 ns, 192 B, 1 alloc** |
| `RenderHtmlNested` | 1156 ns, 1912 B, 8 allocs | **770 ns, 576 B, 1 alloc** |

The single remaining allocation is the Builder's backing array, which is unavoidable without a pool — and any pool would need to copy the output out, putting the allocation right back.

### Cascade wins downstream

Every package that calls `RenderHTML` picks up the improvement for free. Re-running the server and route benches shows:

| benchmark | before v0.16.4 | after v0.16.4 |
|---|---|---|
| `RenderDocumentSimple` | 11 allocs | **6 allocs** (-45%) |
| `RenderDocumentComplex` | 17 allocs | **6 allocs** (-65%) |
| `WriteHtmlSimple` | 14 allocs | **10 allocs** |
| `WriteHtmlComplex` | 18 allocs | **10 allocs** (-44%) |
| `RouterServeStatic` | 18 allocs | **16 allocs** |
| `RouterServeParam` | 26 allocs | **23 allocs** |
| `RouterServeNestedLayouts` | 54 allocs | **46 allocs** (-15%) |

### Smaller per-request helpers

- **`server.nextRequestID`** — the requestID middleware stamps every request with a `gosx-<nanos>-<seq>` string. The previous `fmt.Sprintf("gosx-%d-%d", …)` allocated a format-state scratch buffer per call. Replaced with a `strings.Builder` + two `strconv.Format*` calls.
- **`server/cache.go` auto-ETag** — same treatment: `fmt.Sprintf(W/"gosx-%s", hex)` → direct string concatenation. Runs on every cacheable GET that auto-derives an ETag.
- **`hub.Broadcast` / `hub.Send`** — the previous code called `json.Marshal` twice per send: once via `mustMarshal(data)` returning a `json.RawMessage`, then again on the wrapping `Message` envelope. Plus the `RawMessage` field assignment boxed an interface wrapper. Consolidated into a single `encodeMessage` helper that builds the wire format into one pre-sized byte buffer.

## v0.16.3

Server hot-path performance patch covering four packages: `island/program` (the binary IslandProgram wire format), `ir` (lowering and expression parsing), `server` (page rendering / write), and `route` (request dispatch). Every benchmarked path is faster, and per-request allocation pressure on real page renders is meaningfully lower.

### `island/program` — binary encode 55% faster, decode allocs cut by 87%

`EncodeBinary` was using `binary.Write(&buf, byteOrder, x)` per field, which boxes each integer into `interface{}` and walks the reflect-based path on every call. For a counter-sized program that came out to **149 allocations** per encode.

Replaced with direct `putUint16` / `WriteByte` helpers and back-patched section length prefixes in place — no more per-section `bytes.Buffer`. The decoder got the matching treatment: the io.Reader-based `binReader` was rewritten as an offset-indexed slice reader where sub-readers share the parent's backing buffer instead of copying section bytes.

| benchmark | before | after |
|---|---|---|
| `EncodeBinaryCounter` | 3068 ns, 149 allocs | **1764 ns, 11 allocs** |
| `EncodeBinaryForm` | 6962 ns, 287 allocs | **4322 ns, 16 allocs** |
| `DecodeBinaryCounter` | ~218 allocs | **807 ns, 29 allocs** |

### `ir` — Lower 53–73% fewer allocations on counter/form fixtures

Two big wins in the lowerer:

1. **`lowerer.text(n)` substrings instead of allocating.** The old version did `string(l.src[a:b])` per call, which forces a fresh byte allocation + copy every time the lowerer reads source text from a tree-sitter node — easily hundreds of times per island. Added a `srcStr string` field initialized once at the top of `Lower()`, and `text()` now slices into that. Go strings share their backing storage so each call is a 16-byte slice header copy with zero heap traffic.

2. **`precedingCommentLines` walks the source backwards** instead of building a full prefix string and `strings.Split`-ing every line of every previous declaration. Returns zero allocations when no comments precede the node.

Plus: pre-sized `extractAttrs` / `extractChildren` to the parent's child count, and pre-sized the expression lexer's token buffer to roughly `len(source)/3`. Single-character tokens reuse a package-level `[256]string` table to avoid `string(byte)` per call.

| benchmark | before | after |
|---|---|---|
| `LowerCounter` | 47 allocs | **22 allocs** (-53%) |
| `LowerForm` | 147 allocs | **39 allocs** (-73%) |
| `ParseExprComplex` | 21 allocs | **12 allocs** (-43%) |
| `ParseExprSimple` | 8 allocs | **5 allocs** |

### `server` — RenderDocument 34% faster, 45% fewer allocations

`renderDocumentWithContext`, `renderDocumentAttrValues`, and `renderDeferredChunk` were all using `fmt.Fprintf` / `fmt.Sprintf` per attribute or per chunk. Each call boxes both arguments and walks the format string.

Replaced with direct `strings.Builder.WriteString` / `WriteByte` writes, pre-sizing the builder via `Grow()` based on the actual lengths of the dynamic chunks (title, head HTML, body HTML, attrs). And `fmt.Fprint(w, html)` for streaming the response was replaced with `io.WriteString(w, html)` to skip the interface boxing path.

`NewPageState()` was eagerly allocating four things on every request: the headers map, the deferred-fragment registry, and the cache state struct — none of which most short responses ever touch. Switched to lazy initialization: `Header()`, `DeferredRegistry()`, and `CacheState()` create their fields on first access. Same for `pageRouteHandler` / `apiRouteHandler` which were also calling `ctx.cache = NewCacheState()` redundantly.

| benchmark | before | after |
|---|---|---|
| `RenderDocumentSimple` | 799 ns, 20 allocs | **525 ns, 11 allocs** |
| `RenderDocumentComplex` | 3227 ns, 26 allocs | **2791 ns, 17 allocs** |
| `RenderDeferredChunk` | 341 ns | **258 ns** |

### `route` — pattern parsing hoisted out of the request closure

`buildHandler` was calling `extractParams(req, pattern)` per request, which itself called `patternParamNames(pattern)` — a string parser that walked the pattern looking for `{name}` segments. Pattern is captured by closure so the param-name slice is fully determined at registration time. Hoisted it out:

```go
paramNames := patternParamNames(pattern)
return func(w, req) {
    ctx.Params = extractParamsByNames(req, paramNames)
    ...
}
```

`extractParamsByNames` now also returns `nil` (instead of an empty map) for routes that have no parameters at all — reads from a nil string map return the zero value, so handlers calling `ctx.Param("x")` don't notice the difference and we save the empty-map allocation on every parameterless route. Same for `newRouteContext`'s `Params` field.

| benchmark | before | after |
|---|---|---|
| `RouterServeStatic` | 963 ns, 20 allocs | **795 ns, 18 allocs** |
| `RouterServeParam` | 1486 ns, 27 allocs | **1218 ns, 26 allocs** |
| `RouterServeNestedLayouts` | 2400 ns, 55 allocs | **2225 ns, 54 allocs** |

### Benchmark coverage

Four new danmuji bench files document the hot paths and double as a regression guard:

- `island/program/bench.dmj` — binary encode/decode (counter, form), JSON encode (counter)
- `ir/bench.dmj` — Lower (counter, form), LowerIsland, ParseExpr (simple, complex)
- `server/bench.dmj` — renderDocument (simple, complex), WriteHTML (simple, complex), renderDeferredChunk
- `route/bench.dmj` — router serve (static, param, nested layouts), patternParamNames

## v0.16.2

Server-side performance patch focused on two Go hot paths that every request touches: Scene3D IR marshaling and HTML attribute rendering.

### HTML attribute rendering — **55% faster per-attribute**, 84% fewer allocations

`renderAttrHTML` in `node.go` was calling `fmt.Fprintf` per attribute, which boxes the two string arguments into `interface{}` values (2 allocs per call) plus allocates a format-state scratch buffer (1-2 more allocs). For a typical server page render with hundreds of attributes that was thousands of avoidable allocations per request.

Replaced with direct `b.WriteByte` / `b.WriteString` calls — the attribute format is fixed (` name="value"`) so there's no need for `fmt`'s dynamic format parsing.

Benchmarks on the new `gosx/node_bench.dmj` suite (20-mesh mixed scene):

| benchmark | before | after |
|---|---|---|
| `RenderHtmlSimple` | 50 ns, 56 B, 3 allocs | 37 ns, 24 B, 2 allocs |
| `RenderHtmlWithAttrs` | 614 ns, 816 B, 18 allocs | **279 ns, 504 B, 6 allocs** |
| `RenderHtmlNested` | 2115 ns, 2602 B, 50 allocs | **935 ns, 1912 B, 8 allocs** |

The nested page-subtree benchmark went from 50 allocations to 8 — every server page render gets ~2× faster on the HTML marshal phase. This is a universal win: it affects ALL pages, not just Scene3D.

### Scene3D IR marshaling — **27% faster**, 15% fewer allocations

`scene.Props.SceneIR()` used to allocate a `map[string]any` per mesh for geometry props and a second map per mesh for material props, only to immediately read those maps back into typed fields on `ObjectIR` via `applyGeometryProps` / `applyMaterialProps`. Pure waste — the typed data was already in hand.

Replaced with `applyGeometryToObjectIR` and `applyMaterialToObjectIR` helpers that type-switch over the concrete `Geometry` / `Material` types and set `ObjectIR` fields directly. Also dropped the defensive slice copies at the end of `Graph.SceneIR()` since the result is consumed immediately by `Props.SceneIR → legacyProps → MarshalJSON` with no mutation in between.

Additionally:
- `SceneIR.legacyProps()` now pre-sizes its output map to `make(map[string]any, 16)` instead of the default literal capacity, skipping 1-2 bucket grows per call.
- Transition and environment struct tags switched from `omitempty` (which doesn't work on nested struct fields) to Go 1.24's `omitzero` tag, backed by exported `IsZero()` methods. Pure correctness prep for a future direct-struct-marshal refactor — no behavior change in the current path.

Benchmarks (20-mesh mixed scene fixture):

| benchmark | before | after |
|---|---|---|
| `PropsSceneIR` | 21741 ns, 80 KB, 292 allocs | **9215 ns, 48 KB, 95 allocs** |
| `PropsLegacyProps` | 36918 ns, 109 KB, 665 allocs | 22221 ns, 78 KB, 439 allocs |
| `PropsMarshalJson` | 102535 ns, 142 KB, 1308 allocs | **65040 ns, 108 KB, 1020 allocs** |

`Props.SceneIR()` is 2.4× faster and allocates 67% fewer objects. The full `MarshalJSON` wire-encoding path is 37% faster end-to-end. A server rendering Scene3D pages at ~100 req/s saves ~3.7 ms of CPU per second on scene marshaling alone.

### Benchmark harness moved to danmuji

New benchmark suites in `node_bench.dmj`, `scene/bench.dmj`, and `signal/bench.dmj` authored in [danmuji](https://github.com/odvcencio/danmuji) `.dmj` format. Danmuji's `benchmark "name" { setup { … } measure { … } report_allocs }` blocks compile to plain `testing.B` benchmarks via `danmuji build`, so `go test -bench=.` runs them natively with no extra runner involved. The `.dmj` form is just a cleaner way to express setup + measure blocks without the boilerplate.

Signal benchmark coverage confirms `Signal.Set` / `Signal.Get` remain zero-allocation thanks to Go's escape analysis stack-allocating the per-call subscriber scratch slice.

## v0.16.1

Patch release following v0.16.0 with additional per-frame allocation eliminations caught in a second round of the perf sweep, plus the removal of two stray debug `console.log` statements that were shipping in production code paths.

### Performance

- **`sceneProjectPoint`** inlines the camera normalization, local transform, and inverse rotation. Previously allocated 5 intermediate objects per call (normalized camera ×2, local point arg, inverse-rotate result, final projected point). Now uses the out-param form of `sceneRenderCamera` with a module-level scratch and returns exactly 1 fresh object.
- **`sceneBoundsDepthMetrics`** replaces the `sceneBoundsCorners` + `sceneWorldPointDepth × 8` chain (16 allocations per call) with an inlined 8-corner bit-coded loop that reads `bounds.{min,max}{X,Y,Z}` directly and inlines the inverse-rotate math for the Z component only. Zero allocations in the corner loop.
- **`sceneWorldObjectDepth`** replaces the per-vertex `{x,y,z}` object + `sceneWorldPointDepth` chain (4+ allocations per vertex) with an inline camera-local Z computation using a module-level camera scratch. For a 1000-vertex mesh at 60fps that's 240k allocations per second eliminated.
- **`scenePlaneSurfaceCorners`** uses a module-level 4-element scratch array instead of allocating 4 fresh corner objects via the old `translateScenePoint` wrapper.
- **`translateScenePoint` allocating wrapper removed** — every call site now uses `translateScenePointInto` with hoisted scratches.
- **`sceneMaterialShaderData`** fast path returns the cached shaderData reference directly instead of element-by-element copying through `sceneNumber`. Callers verified read-only.
- **Stray `console.log` debug prints removed** from `appendSceneObjectToBundle` and the scene WebGL renderer wrapper — both ran every frame with `JSON.stringify` side effects and were pure log-spam.

The `/demos/scene3d-bench` composite "simulated frame" benchmark holds at ~2.2µs per frame with zero JS-side allocations on the steady-state path.

## v0.16.0

### Scene3D Thick Lines

**`scene.LinesGeometry.Width float64`**: Per-mesh line stroke width in CSS pixels. Zero (the default) keeps the legacy hairline rendering for backwards compatibility. Non-zero values activate the new thick-line draw path.

**WebGL thick-line pipeline**: a dedicated screen-space quad expansion shader built alongside the legacy `gl.LINES` path. Each segment `(A, B)` expands to 4 vertices (2 triangles) in the vertex shader with per-vertex offset perpendicular to the projected line direction. `gl.lineWidth()` is hairline-only on most WebGL drivers, so this is the only way to get configurable line thickness that works across hardware. Matches the projection math of the legacy renderer so thick lines align with existing geometry.

**Per-pass blend state**: the thick-line draw path honors the draw plan's opaque / alpha / additive pass separation. Each scene object's render pass is recorded in `bundle.worldLinePasses` at build time; the expansion function writes per-pass index ranges so the draw path issues up to three `drawElements` calls with the correct blend + depth state. Additive thick lines (e.g., glowing lightning) now composite correctly instead of rendering as alpha.

**`scene.Bloom.Scale` WebGPU parity**: the WebGPU backend now honors `Bloom.Scale` at parity with WebGL via a lazy ping-pong resize inside the bloom draw case.

**Canvas 2D fallback**: the world-projected canvas renderer reads per-segment width from a parallel `bundle.worldLineWidths` array instead of hardcoding 1.8px.

### Scene3D Render Path Performance

A sweep of the per-frame JS hot path. Validated end-to-end by a new microbenchmark harness (`client/js/runtime.bench.js`) and a live browser overlay at `/demos/scene3d-bench`.

- **Thick-line pooled scratch**: `resources.thickLineScratch` replaces the 8 fresh typed-array allocations per thick-line frame. Geometric growth (2× on miss), never shrinks, subarray-bounded uploads so small workloads don't push GPU buffers beyond their used slice.
- **`buildPBRDrawList` scratch arrays**: the PBR draw-list builder now reuses renderer-scoped arrays instead of allocating three plain arrays plus a result object every frame. Three call sites × 60 fps × 4 allocs/call = 720 allocs/sec eliminated.
- **Duplicate `sceneRenderCamera` call removed** from `applySceneWebGLUniforms` — it was resolving the same camera object twice per frame for no reason.
- **`sceneRenderCamera` out-param form**: the function now accepts an optional `out` scratch the caller owns. The PBR renderer uses it to mutate its own `_frameCam` in place instead of allocating a fresh result every frame. Backwards compatible — omit the out param to preserve legacy allocation behavior.
- **`translateScenePointInto` with hoisted scratches**: the alloc-free core of the scene-space transform now lives in `translateScenePointInto(out, px, py, pz, object, t)` with inlined rotation and motion math (no `sceneRotatePoint` or `sceneMotionOffset` intermediates). Two hot call sites — the line-geometry loop in `appendSceneObjectToBundle` and the triangle mesh loop in `appendSceneMeshObjectToBundle` — were restructured to use module-level scratches hoisted above their outer loops. Previously each allocated 4 or 7 intermediate objects per segment/triangle.
- **`scenePBRUploadLights` dirty tracking**: a content hash stamped on each uniforms struct skips the ~30 `gl.uniform*` calls per program per frame when lights + environment haven't changed. Three call sites (main + skinned + instanced programs) share a once-per-frame hoisted hash so the cost is paid once instead of three times.
- **Per-light / per-environment cached sub-hashes**: `normalizeSceneLight` and `normalizeSceneEnvironment` stamp `_lightHash` / `_envHash` at mutation time. `scenePBRLightsHash` combines those cached values instead of re-walking every field on every frame. `sceneApplyTransitionPatch` re-stamps duck-typed so transitions that mutate lights keep their hashes in sync. **Measured speedup: 27× on the warm path** (13µs → 479ns on a 3-light fixture).
- **`scenePBRUploadExposure` dirty tracking**: the same pattern with direct field caching instead of hashing — only 2 uniforms, so strict-equal comparison on cached primitives is simpler and collision-free.
- **Dead code removal**: `projectSceneObject`, `sceneWorldObjectSegments`, and `sceneMotionOffset` are dropped entirely — they had zero callers in the concatenated bundle.

### Runtime Asset Serving

- **Dev-mode manifest root fix**: `App.SetRuntimeRoot` now propagates into `island.SetManifestRoot`, so the HTML renderer's manifest lookup stays aligned with the file-serving root. Dev-mode setups where `runtimeRoot` points at the gosx source tree no longer produce silent 404s on hashed asset URLs.

### Browser Frame-Time Bench (`/demos/scene3d-bench`)

A new gosx-docs page that drives the Scene3D render pipeline under real WebGL and displays a live overlay with min/p50/p95/max/mean per-frame duration. Measured via `performance.mark("scene3d-render-start" / "scene3d-render-end")` gated behind `window.__gosx_scene3d_perf === true` — single truthy check when disabled, zero cost on production pages.

Five workloads selectable via `?workload=...`:
- **`static`** — 5 PBR meshes, no postfx, no animation. Baseline for the dirty-tracked fast path.
- **`pbr-heavy`** — 30 shiny spheres in a ring with shadows + full postfx chain.
- **`thick-lines`** — 12 thick bolts split across opaque / alpha / additive blend passes.
- **`particles`** — 2000-particle compute cloud with drift motion. Dynamic scene baseline.
- **`mixed`** — default. 15 PBR meshes + 6 additive lightning bolts + postfx + shadows.

### Scene3D Microbenchmark Harness

`client/js/runtime.bench.js` is a standalone Node harness that loads the bootstrap bundle in a VM context with `window.__gosx_bench_exports = true` and runs per-function microbenchmarks on the Scene3D hot path. No external deps, reports min/p50/p95/max/mean/std plus total iterations.

```bash
node client/js/runtime.bench.js                    # default 50k iterations
node client/js/runtime.bench.js -n 100000 -w 2000  # custom sample count
node client/js/runtime.bench.js --only Lights,Hash # substring filter
node client/js/runtime.bench.js --json             # machine-readable output
```

Composite "simulated frame" benchmark chains the hot-path calls a real render() makes on a steady-state static scene (hash + exposure × 3 + lights × 3 + thick-line expansion) and reports **2.29µs per frame** — 0.014% of a 60fps budget after the sweep.

### gts-suite `--exclude` (companion release)

The `gts` code-analysis CLI (separate repo, [github.com/odvcencio/gts-suite](https://github.com/odvcencio/gts-suite)) gained a `-X / --exclude` persistent flag so `gts analyze hotspot` can filter concatenated generated bundles like `client/js/bootstrap.js` without editing workspace `.gtsignore` files. This unblocked the gosx perf sweep by surfacing real source hotspots instead of build-output noise.

## v0.15.0

### Breaking Changes

**Default PostFX resolution cap**: `scene.PostFX` now enforces a default framebuffer resolution cap of 1080p (2,073,600 pixels) for the postfx offscreen pipeline. On displays exceeding this resolution, the postfx chain is downscaled uniformly and the result is linearly upsampled to the canvas. This protects against multi-hundred-megabyte framebuffer allocations on high-DPR displays. To restore v0.14.0 behavior (full-resolution postfx), set `MaxPixels: scene.PostFXMaxPixelsUnbounded` explicitly.

**Default shadow pixel cap**: `scene.Shadows` is a new top-level Props field that enforces a default per-shadow-map cap of 1024² (1,048,576 pixels). Directional and spot lights with `ShadowSize: 2048` or `4096` are silently scaled down to fit the cap on both WebGL and WebGPU backends. To restore v0.14.0 behavior (honor each light's full `ShadowSize` without a global cap), set `Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixelsUnbounded}` explicitly.

### PostFX Resolution Cap

**`scene.PostFX.MaxPixels int`**: Caps postfx offscreen framebuffers at a pixel count, uniformly scaling the pipeline when the canvas exceeds the cap. The zero value applies the safe 1080p default. Set to `scene.PostFXMaxPixelsUnbounded` to opt out of capping entirely.

**`scene.Bloom.Scale float32`**: Optional bloom-internal downscale factor applied on top of the main cap. The zero value preserves v0.14.0 behavior (0.5 half-res ping-pong buffers). Values outside (0, 1] are silently clamped to the default.

**Preset constants**: `scene.PostFXMaxPixels540p`, `PostFXMaxPixels720p`, `PostFXMaxPixels1080p`, `PostFXMaxPixels1440p`, and `PostFXMaxPixels4K` for common cap sizes. `PostFXMaxPixelsUnbounded` opts out entirely.

**WebGPU parity**: The WebGPU runtime (`16a-scene-webgpu.js`) honors `MaxPixels` at full parity with the WebGL runtime — the memory guarantee holds on both backends.

### Shadow Map Pixel Cap

**`scene.Shadows.MaxPixels int`**: Caps individual shadow maps at this many pixels. When a light's `ShadowSize` would exceed the cap, the pipeline scales it down uniformly (e.g., a light requesting `ShadowSize: 4096` with `MaxPixels: 1048576` gets a 1024 shadow map — memory drops from ~64 MB to ~4 MB per light).

**Preset constants**: `scene.ShadowMaxPixels512`, `ShadowMaxPixels1024`, `ShadowMaxPixels2048`, and `ShadowMaxPixels4096`. `ShadowMaxPixelsUnbounded` opts out entirely.

**WebGPU parity**: Both WebGL and WebGPU shadow pipelines honor the cap through a shared `resolveShadowSize` helper.

### Runtime Asset Serving

**Dev-mode runtime root fix**: `App.SetRuntimeRoot` now propagates the chosen root into `island.SetManifestRoot`, so the HTML renderer's manifest lookup stays aligned with the file-serving root. Previously, dev-mode setups where `runtimeRoot` pointed at the gosx source tree caused silent 404s on hashed asset URLs because the renderer read the manifest from the app CWD while the file server looked under the runtime root. The new `island.SetManifestRoot` / `ResetManifestRoot` pair is also available for tests that need fine-grained manifest-root control.

### Internal Refactors

**Shared post-processing helpers**: `resolvePostFXFactor` and the new `resolveShadowSize` are extracted to a shared `client/js/bootstrap-src/15a-scene-postfx-shared.js` module, replacing per-renderer duplication between the WebGL and WebGPU paths.

### Bug Fixes

**Point sprite sizing**: `gl_PointSize` now scales against the render target rather than the canvas, fixing a latent bug where high-DPR displays produced oversized stars when post-processing was active.

## v0.14.0

### `gosx/field` Module

New `field` package providing a 3D vector field type with trilinear sampling, standard operators (Advect, Curl, Divergence, Gradient, Blur, Resample), per-component scalar quantization at 4–8 bits with optional delta encoding, and `gosx/hub` integration for live field broadcast across WebSocket connections. Designed as the foundation for volumetric rendering, particle advection, fluid simulation, and any consumer that needs structured 3D data — independent of any renderer.

A 64³ scalar field at 6 bits packs to ~200 KB on the wire. A 64³ vec3 field packs to ~600 KB. Delta encoding shrinks subsequent updates further when the field is temporally coherent. The codec reuses the same per-component min/max scalar quantization pattern proven in `scene/compress.go`.

Hub streaming maintains a per-(hub, topic) subscriber registry inside the field package itself. `PublishField` does dual dispatch: the decoded `*Field` flows to local in-process subscribers via Go channels, while the JSON-encoded `Quantized` payload goes to connected WebSocket clients via `hub.Broadcast`. Successive publishes automatically delta-encode against the previous field for that topic.

### Scene3D PostFX Go API

`scene.PostFX` adds a Go-side post-processing effect chain on top of the existing JS-side post-processor. The previously-dormant FBO + ping-pong + ACES tone mapping + bloom infrastructure in the WebGL2 and WebGPU renderers is now driven by typed Go effects: `Tonemap`, `Bloom`, `Vignette`, `ColorGrade`. A typical chain looks like:

```go
scene.PostFX{Effects: []scene.PostEffect{
    scene.Bloom{Threshold: 0.65, Strength: 0.6, Radius: 8},
    scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.0},
}}
```

The new `PostEffect` interface is sealed via an unexported method so external packages cannot define their own effects without coordination with the renderer. Effect chains run in declaration order, ping-ponging through HDR offscreen targets before compositing to the canvas.

### `Environment.ToneMapping` Backwards-Compat Migration

Existing scenes that used `Environment.ToneMapping = "aces"` (and `Environment.Exposure`) keep working unchanged. The compile path now synthesizes a `Tonemap` PostEffect from those fields when `PostFX.Effects` does not already include one — the explicit Go API takes precedence when present, and the legacy fields keep working when it's absent. The synthesized effect routes through the new PostFX pipeline so the inline tonemap branches in the PBR fragment shader stay disabled.

## v0.13.0

### Authoritative Simulation Module

New `sim` package providing server-authoritative game simulation over gosx hubs. Games implement a four-method `Simulation` interface (Tick, Snapshot, Restore, State) and the `sim.Runner` handles everything else: fixed-rate tick loop, input collection from hub clients, state broadcast, 128-frame snapshot ring buffer for rollback, replay recording with full input logs, and spectator sync on join. One line to get tournament-grade netcode: `sim.New(hub, game, sim.Options{TickRate: 60})`.

### Server Gzip Middleware

`server.EnableGzip()` adds response compression with proper Hijacker/Flusher support so WebSocket upgrades and streaming still work. Pooled gzip writers, skips pre-compressed responses and WebSocket upgrades automatically.

### WASM Debug Stripping

Production builds now strip DWARF debug sections from WASM binaries via `wasm-opt --strip-debug --strip-producers`. Debug symbols were 45% of the binary (1.3 MB) and served no purpose in the browser. Result: 1032 KB → 526 KB gzipped, a 49% reduction. Dev builds retain debug symbols.

### WASM Data Section Externalization (experimental)

`wasmExternalizeData` post-processor splits the WASM data section (strings, type tables, reflection metadata) into a separate file for parallel loading. The browser can start compiling WASM code while the data section downloads independently.

## v0.12.0

### Scene3D Per-Component Compression

Position arrays are now deinterleaved before quantization so each axis gets its own min/max codebook. Previously, interleaved XYZ data shared one range, destroying precision on axes with smaller extents (e.g., a flat galaxy's Y axis spanning 10 units quantized against X/Z spanning 400 units). The client-side decompressor reinterleaves after dequantization. Controlled by the existing `Compression.BitWidth` setting — no API changes needed. The `positionStride` field is plumbed through the IR and legacy serialization path automatically.

### Particle Emitter Rotation

`ParticleEmitter` now accepts a `Rotation Euler` field. The spiral, disc, and sphere emitters apply the rotation to emitted particle positions so compute particle systems can match a tilted parent geometry. Both the WebGPU (WGSL) and CPU fallback paths apply the ZYX Euler rotation matrix. This fixes the horizontal-band artifact when a spiral emitter is used with a tilted galaxy scene.

### Editor Module

New `editor` package providing a rich text editing foundation: toolbar component with default markdown actions, undo/redo history with coalescing, `LocalDocument` text model, tree-sitter based syntax highlighting for markdown, input system with IME support, and signal-driven reactivity. Build-time factory registry for engine lockdown after init.

### Scene3D Improvements

Lighting, input, mount, glTF, and WebGPU rendering improvements across the Scene3D pipeline. Transition system additions for smooth state morphing.

## v0.11.0

### Selective Runtime Bootstrap

GoSX now ships a real selective bootstrap path for non-scene runtime pages instead of the previous full-vs-lite split.

**Selective runtime bundle**: Added `bootstrap-runtime.js` plus feature chunks for islands, engines, and hubs. Pages now load only the runtime features they declare:
- **Lite pages** keep `bootstrap-lite.js`
- **Islands / hubs / non-Scene3D engines** use `bootstrap-runtime.js` plus the matching feature chunk(s)
- **Scene3D pages** keep the full `bootstrap.js`

**Shared wasm runtime gating**: The page runtime now emits `runtime.wasm` and `wasm_exec.js` only when they are actually needed. Hub-only pages, managed video pages, and native JS engine pages no longer pay for the shared Go wasm bridge.

**Runtime asset pipeline**: Build manifests, compat asset serving, static export, and `gosx build` / `gosx export` / `gosx dev` now understand `bootstrap-runtime.js` and the `bootstrap-feature-*` assets, so selective bootstrap works in source builds, hashed build manifests, and static-export compatibility copies.

**Document/runtime contract cleanup**: Default `server.App` rendering now injects managed runtime head assets correctly before the document is finalized, and the document contract now distinguishes bootstrap from shared-runtime usage instead of treating every `full` bootstrap page as a shared wasm page.

### Dependency Wiring

**Published TurboQuant dependency**: Replaced the local `replace github.com/odvcencio/turboquant => ../turboquant` workflow with the published `github.com/odvcencio/turboquant v0.1.0`, so `cmd/gosx` build/export integration tests can resolve wasm runtime dependencies from clean starter apps.

## v0.10.0

### TurboQuant Vector Intelligence

**`quant` package**: Pure-Go implementation of the TurboQuant algorithm (arXiv:2504.19874). Compresses high-dimensional vectors to 1-8 bits per coordinate, within ~2.7x of information-theoretic optimum. MSE-optimal quantizer via random rotation + Lloyd-Max codebook. Inner-product-optimal quantizer via MSE + 1-bit QJL residual with unbiased estimation. Deterministic via `NewWithSeed`. Zero-alloc inner product in rotated domain. `PrepareQuery` amortizes O(d²) projection for search workloads. 53μs quantize, 52μs inner product at dim=384.

**`vecdb` package**: In-memory quantized vector search index. Zero indexing time (data-oblivious quantization). Add/Remove/Search with `PrepareQuery` + `InnerProductPrepared` scan and min-heap top-k selection. Thread-safe via `sync.RWMutex`. O(1) swap-and-pop removal. 45ms search over 1K vectors at dim=64.

**`embed` package**: Provider interface for external embedding APIs (OpenAI, Cohere, etc.) plus BPE tokenizer with vocabulary/merge loading, special token support, and auto-detection of Ġ space-prefix convention.

**`semantic` package**: Three AI-native primitives:
- **SemanticCache** — cache responses by vector similarity instead of exact URL. Similar queries share cached responses above a configurable threshold.
- **SemanticRouter** — match requests to handlers by embedding similarity. Route by meaning, not URL pattern. Primary consumer: AI agents calling APIs without needing schemas.
- **ContentIndex** — index page content for related-page discovery and semantic search.

### CRDT Vector Compression

**`VectorValue` type**: Store high-dimensional vectors in CRDT documents using TurboQuant compression. All replicas use a deterministic seed for byte-identical output, preventing spurious merge conflicts. 16x bandwidth reduction at 2-bit (96 bytes for 384-dim vs 1,536 raw). Participates in existing LWW merge semantics.

### Scene3D Compression Pipeline

**Scalar quantization for vertex transport**: Per-chunk min/max quantization compresses positions, sizes, transforms, and animation keyframes. Metadata is 8 bytes per chunk (min + max) instead of a rotation matrix.

**Client-side JS dequantizer**: Complete end-to-end pipeline — Go quantizes during IR lowering, JS dequantizes at scene init before the render loop. Base64 decode → unpack b-bit indices (1/2/4/8-bit fast paths) → scalar dequantize → vertex buffers.

**Progressive mesh loading**: `Compression{BitWidth: 4, Progressive: true}` ships 2-bit preview alongside 4-bit full resolution. Client renders preview immediately, upgrades to full after first paint via `requestIdleCallback`. No loading spinner.

**Quantization-based LOD**: `Compression{LOD: true}` stores both preview and full resolution. Per-frame, each object's camera distance determines which resolution renders. Objects beyond `LODThreshold` (default 20 units) use the preview. Crossing the threshold triggers buffer rebuild.

**Animation keyframe compression**: `AnimationClip` and `AnimationChannel` scene graph nodes with compressed `Times` and `Values` arrays. 32-joint skeleton at 60 keyframes, 4-bit: 92% compression (15KB vs 182KB). Progressive preview keyframes for instant playback.

## v0.9.0

### Bug Fixes

**Route handler error propagation**: Route handlers that encounter render errors no longer panic. Added `ctx.SetHandlerError(err)` to `RouteContext` and a check in `buildHandler` that dispatches through the error handler chain. The `defer recover()` safety net remains for unexpected panics, but normal render errors now flow through the error page system without crashing the process.

**CRDT API safety**: `Doc.Save()`, `Doc.Fork()`, and `Doc.Commit()` now return errors instead of panicking on encode failures. `Save()` returns `([]byte, error)`, `Fork()` returns `(*Doc, error)`, `Commit()` returns `(ChangeHash, error)`. Internal `commitPending` and `flushPendingForSnapshot` propagate errors. All callers in `crdt/`, `hub/`, and `client/bridge/` updated.

### Documentation Site Redesign

Complete ground-up rebuild of the gosx-docs example app. The old Paper & Ink site is gone. The new site is a maximalist, dual-mode experience that proves GoSX by being built with GoSX at full power.

**Design system**: Dual-mode token architecture (dark immersive + light editorial) built on the m31labs.dev visual language. Space Grotesk + Inter + JetBrains Mono font stack. Fluid clamp() typography and spacing. Glass morphism, chrome text gradient, gold accent family. WCAG 2.2 AA minimum with verified contrast ratios. Forced-colors override for Windows High Contrast Mode.

**Showroom homepage**: Full-viewport 3D hero scene (PBR meshes, 2000 GPU compute particles, orbit controls, ACES tone mapping). Three-statement pitch section. Eight capability showcase sections alternating dark/light. Proof point stat cards with TextBlock server-measured values. Scroll reveal with IntersectionObserver stagger.

**17 reference pages** where the docs ARE live demos:
- Light mode (11): Getting Started, Routing, Forms, Auth, Runtime, Images, Text Layout, Motion, Streaming, Compiler, Deployment
- Dark mode (5): Islands, Signals, Engines, 3D Engine (with inline PBR scene), Hubs & CRDT

**3 standalone demos**: Galaxy (2800 GPU compute particles), Geometry Zoo (PBR primitives with orbit controls), CMS Editor (block editor with publish action).

**Navigation**: Floating pill nav (translucent over dark, solid over light). Page-scoped TOC rail on reference pages. Full-screen overlay with focus trap on mobile.

**Accessibility**: Skip links (content + navigation + TOC). `prefers-reduced-motion` global kill switch with static 3D fallbacks. Focus-visible gold rings on all interactive elements. ARIA landmarks, live regions, descriptive labels. 44px minimum touch targets. 400% zoom reflow.

**Micro-interactions**: Link underline draw (left-to-right, gold). Card hover lift with chrome gradient border. Button press scale. Code block hover highlight. Page transitions (fade out 150ms, fade in 300ms). Glass panel tooltips with spring easing.

## v0.8.0

### Tier 2: Visual Quality

**HDR exposure control**: `Environment.Exposure` scales scene brightness before tone mapping. `Environment.ToneMapping` selects ACES filmic (default), Reinhard, or linear output. Postprocessing automatically disables built-in tone mapping.

**Spot lights**: `SpotLight` with cone angle, penumbra (soft edge), distance attenuation, and shadow casting. PBR shader computes inner/outer cone cutoff with smooth falloff.

**Hemisphere lights**: `HemisphereLight` with sky/ground color blend based on surface normal direction. Provides ambient lighting that varies with orientation.

**MSAA**: Canvas-level multisampling verified working for non-postprocessing scenes via the existing `antialias: true` context flag.

**5 light types** in the PBR shader: ambient, directional, point, spot, hemisphere.

## v0.7.0

### Tier 1: WebGPU, Instancing, GPU Compute Particles

**WebGPU backend** (2,447 lines): Full PBR renderer ported to WGSL — Cook-Torrance BRDF, shadow maps with comparison sampling, postprocessing chain, exponential fog. Points rendered as instanced billboard quads (WebGPU has no gl_PointSize). Async device initialization, pipeline caching by material+geometry signature, storage buffer light arrays, bind group architecture (per-frame / per-material / per-object).

**Instanced rendering**: `InstancedMesh` node draws N copies of one geometry in a single draw call. WebGL2 via `drawArraysInstanced` + `vertexAttribDivisor`, WebGPU via native instancing. Per-instance mat4 transforms from Go-declared positions/rotations/scales. Geometry generation for box, plane, sphere with normals/UVs/tangents. Geometry cache by kind+dimensions. Performance target: 10K instances at 60fps.

**GPU compute particles** (664 lines): `ComputeParticles` node declares a particle system that simulates entirely on GPU. WGSL compute shader with 4 emitter types (point, sphere, disc, spiral), 5 force types (gravity, wind, turbulence, orbit, drag), deterministic hash RNG, lifetime/respawn, color/size/opacity interpolation over lifetime. CPU fallback for WebGL2 (capped at 10K particles). Go API: one struct configures 100K particles at zero CPU cost.

**Auto backend selection**: WebGPU → WebGL2 PBR → WebGL2 legacy → Canvas 2D. Transparent to scene authors.

**Go API**: `InstancedMesh`, `ComputeParticles`, `ParticleEmitter`, `ParticleForce`, `ParticleMaterial` types with full IR lowering and render bundle transport.

## v0.6.0

### 3D Engine Phase 2-3

Production-grade 3D rendering platform built into GoSX's native Scene3D.

**Renderer**: PBR WebGL2 backend with Cook-Torrance BRDF, per-pixel lighting (8 lights), shadow maps (PCF), postprocessing (bloom, tone mapping, vignette, color grading), exponential fog. Draw-plan abstraction for future WebGPU backend. Split 5K LOC monolith into 10 focused modules.

**Points primitive**: GL_POINTS particle system with per-vertex size/color, size attenuation, additive blending, depth write control, pinwheel spin animation, scroll-driven camera.

**Asset pipeline**: Built-in glTF/GLB loader (meshes, PBR materials, textures, animations, skins). Animation mixer with keyframe interpolation, quaternion slerp, crossfading. Skeletal animation with vertex skinning (64 joints).

**Interaction**: Raycast scene picking (Moller-Trumbore ray-triangle with AABB broad phase) replacing bounds-based detection.

**Go API**: `StandardMaterial`, `CylinderGeometry`, `TorusGeometry`, `Points`, shadow fields, animation fields, `ScrollCameraStart`/`ScrollCameraEnd`, fog, `EffectComposer`.

**Performance**: Zero per-frame allocations in animation hot path, cached vertex data, pre-allocated scratch buffers, binary keyframe search, persistent GPU buffers.

**Galaxy demo**: 2,800-particle galaxy with spiral arms, scroll camera, fog — replaces three.js on m31labs.dev with 62% less JS over the wire (80KB gzipped savings).

## v0.3.2

- **fix(isr):** stale ISR pages now snapshot the existing artifact before background regeneration starts, so the first stale response cannot race ahead and serve the freshly regenerated HTML.
- **fix(test):** hardened hub websocket bootstrap tests to wait for the `__welcome` event without assuming it always arrives before every join broadcast once multiple clients connect.

## v0.3.1

- **fix(eval):** `nil == ""` now returns `true` in template expressions, so unbound variables like `flash.contact` compare equal to empty string instead of rendering conditional branches incorrectly.
- **release:** rolls the `v0.2.4` template-eval fix forward onto the `v0.3.x` line without backing out the native text layout, managed video, or managed motion work shipped in `v0.3.0`.

## v0.3.0

- **feat(text-layout):** `TextBlock` now supports explicit native server-measured layout with `mode="native"` / `TextBlockModeNative`, keeping pages off the bootstrap layer when final wrapped HTML should come from the server.
- **feat(video):** `server.Video`, `ctx.Video`, and `<Video />` now emit a real server `<video>` baseline with authored `<source>` and `<track>` children, and the managed runtime upgrades that baseline in place for HLS, sync, and shared media signals.
- **feat(motion):** added `server.Motion`, `ctx.Motion`, and the `.gsx` `<Motion />` builtin for bootstrap-managed DOM motion presets with `load`/`view` triggers, timing controls, and reduced-motion awareness.
- **docs:** refreshed the README and gosx-docs coverage for engines, video, motion, and native-vs-bootstrap text layout.
- **fix(ci):** restored `make fmt-check` on `main` by formatting `server/runtime_assets.go`.

## v0.2.4

- **fix(eval):** `nil == ""` now returns `true` in template expressions — unbound variables like `flash.contact` compare equal to empty string instead of being a distinct nil that renders `<If>` bodies incorrectly

## v0.2.3

- **fix(css):** Global selectors (`body`, `html`, `:root`, `*`, `*::before`, `*::after`, `::selection`, `::placeholder`) in sidecar CSS are no longer scoped — they pass through to the document directly
- **fix(server):** `/gosx/bootstrap.js` serves a minimal stub script when `gosx build` has not been run, instead of returning 404. The stub initializes the gosx runtime namespace and auto-mounts registered engines from the page manifest.

## v0.2.2

- **feat(route):** Added underscore param convention as Go-compatible alternative to bracket syntax
  - `_slug` directory → `{slug}` route parameter (single underscore = dynamic param)
  - `__path` directory → `{path...}` route parameter (double underscore = catch-all)
  - Valid Go package names, works with `go mod tidy`, `go build ./...`, and module imports
  - Legacy `[slug]` bracket syntax still supported for backward compatibility

## v0.2.1

- **fix(ir):** HTML entities (`&rarr;`, `&mdash;`, etc.) in `.gsx` text are now decoded to UTF-8 characters instead of being double-escaped
- **fix(ir):** `<script>` and `<style>` element content is now treated as raw text, preventing HTML escaping of `&&`, `<`, etc. inside inline scripts
- **fix(build):** `gosx build` no longer generates invalid Go import paths for `[slug]` dynamic route directories
- **fix(route):** Pattern conflicts between `__actions` and wildcard siblings now produce a clear diagnostic error instead of a raw panic

## v0.2.0

### Authentication

- added session-backed auth manager with `auth.New()`, `Require` middleware, and `Current()` user context
- added GitHub OAuth provider (`auth.GitHubProvider`) with automatic email fetching
- added Google OAuth provider (`auth.GoogleProvider`)
- added custom OAuth providers with `OAuthUserResolverFunc`
- added magic link passwordless auth with configurable resolver
- added WebAuthn/passkey auth with register and login flows
- added role-based access control via `RequireRole` middleware

### Engine System

- added Surface engine primitive for dedicated canvas/WebGL/WebGPU workloads
- added Scene3D engine with server-side render bundle generation
- added pixel-surface engine runtime with managed GPU framebuffers
- added 3D perspective rendering with world-space coordinates, frustum culling, and depth sorting
- added material system with presets (flat, glow, ghost, glass, matte) and blend modes
- added fluent `Builder` API for program construction
- added box, sphere, pyramid, and plane 3D primitives
- added scene label support with collision avoidance, occlusion, and priority layout
- added shared engine runtime with program-driven scenes
- added input providers with batched signal delivery (pointer, keyboard)

### Islands and Reactivity

- added shared signals across islands (prefixed with `$`)
- added `Each`/`For` rendering with resolved tree reconciliation
- added keyed reconciliation with auto-key stabilization
- added event field access in island handlers
- added string concatenation in VM expressions
- added raw props object access in island expressions

### File-Based Routing

- added directory-scoped modules (`DirModule`) with inherited middleware
- added file-route metadata sidecars (`page.server.go`)
- added sidecar CSS ownership and layer tracking
- added `route.config.json` for directory-scoped cache and header config
- added spread attribute support in file renderer
- added auto-resolved Go components from `Funcs`, `Values`, and dotted paths
- added not-found error handling for data loaders

### Server and Runtime

- added client-side page navigation with head/body swaps and scroll restoration
- added managed form submission via fetch with navigation runtime
- added ISR serving for prerendered bundles
- added static export (`gosx export`) with prerender pipeline
- added production build pipeline with hashed runtime assets and edge bundle artifacts
- added CSS layer architecture with ownership metadata and global styles
- added JSON request observer for structured logging
- added path traversal protection and runtime manifest caching
- added CSRF token injection in form fetch requests

### Text Layout

- added browser-backed text measurement and layout engine
- added grapheme-aware text layout with word segmentation
- added `TextBlock` primitive for framework-level text layout
- added server-side sizing hints, tab stops, soft hyphens, and CJK punctuation
- added `maxLines` and overflow support for text truncation
- added i18n and vertical writing mode support
- added text layout caching with font loading invalidation

### CRDT

- added document core, sync messages, and hub transport for real-time collaboration

### CLI

- added `gosx fmt` with `--check` mode
- added `gosx check` integration test for `.gsx` files
- added `gosx lsp` language server
- added `gosx init --template docs` scaffold

### Docs and Examples

- added gosx-docs reference app with CMS demo, Scene3D demo, auth flows, streaming, and ISR
- rewrote README with clearer structure and user focus
- renamed JSX terminology to native GSX throughout

### Internal

- modularized `bootstrap.js` with split source files and build step
- extracted helper functions across client, route, and VM packages for readability
- optimized WebGL state with cached blend/depth and static geometry buffers
- added capability-aware rendering and resilient WebGL fallback
- added `prefers-reduced-motion` and scroll handling
- added environment, document, and presentation observation APIs

## v0.1.0

- formalized the initial GoSX release line with a repo-level `gosx.Version`
- completed the zero-manual-wiring island path for compiled `.gsx` islands
- added build-manifest loading for hashed runtime and island assets
- hardened actions, hubs, server timeouts, HTML escaping, and build failure handling
- added repeatable repo tooling with `make test`, `make test-race`, `make test-wasm`, `make build-runtime`, and `make ci`
- added CI coverage for format checks, race tests, js/wasm runtime tests, CLI build, and WASM runtime build
- added js/wasm runtime tests that compile `.gsx` islands, hydrate them through `__gosx_hydrate`, dispatch via `__gosx_action`, and assert the client patch stream
