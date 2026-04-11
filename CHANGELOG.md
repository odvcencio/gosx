# Changelog

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
