# GoSX Video Engine

**Date:** 2026-03-29
**Status:** Draft

## Problem

Every GoSX application that needs video playback rewrites the same browser glue: HLS.js setup, `<video>` lifecycle, event listeners for position and buffering, subtitle fetching and cue evaluation, fullscreen wiring, and optional WebSocket sync with drift correction. This is framework work, not application work.

## Proposal Summary

1. Add `video` as a third engine kind alongside `worker` and `surface`.
2. Implement v1 as a framework-owned bootstrap factory. Authors should not write custom JS or a custom engine program to get a capable player.
3. Keep the public contract page-global and single-player in v1 through a fixed `$video.*` shared-signal namespace.
4. Use the existing shared-signal authoring model in islands for v1. Islands bind to names like `$video.position` via `signal.NewShared("video.position", ...)`; magical undeclared `$video.*` globals are not required.
5. Keep the shared signal store in the existing WASM bridge for v1. Even though the player itself is bootstrap-managed JS, pages with a video engine should still load the runtime bridge so the engine and islands have a single source of truth for `$video.*`.
6. Deliver this in phases: engine-kind plumbing, shared-signal bridge exposure, local playback, subtitles, sync, then packaging and docs.

## Goals

1. Add `video` as a first-class engine kind.
2. Provide a fixed `$video.*` signal contract that any island can bind to.
3. Handle HLS transport, subtitle management, fullscreen, and optional WebSocket sync inside the framework.
4. Ship HLS.js as a framework-owned asset that is lazy-loaded only when needed.
5. Let a GoSX video player be authored entirely in `.gsx` and ordinary Go props, with zero authored JavaScript.

## Non-Goals

- DASH / MPEG-DASH support
- DRM (Widevine, FairPlay)
- Picture-in-picture
- Chromecast / AirPlay casting
- Video recording / capture
- Multi-player pages
- Audio track selection in v1
- A new island authoring syntax for implicit `$video.*` globals

## Architecture

### Server and compiler shape

- `//gosx:engine video` is accepted by `ir/lower.go`.
- Add `engine.KindVideo = "video"` and `engine.CapVideo = "video"`.
- Add a shared helper for "mount-bearing engine kinds" and use it everywhere that currently hard-codes `"surface"`. `video` should share the same mount-resolution path as `surface`.
- Renderer and manifest entries should use the existing `Props` field for video configuration. A separate `VideoConfig` nested under `EngineEntry` is not needed in v1.
- Built-in video engines do not require `ProgramRef`, `JSRef`, or `JSExport`. The bootstrap should resolve the framework-owned factory by `entry.kind === "video"`, not by pretending video is an escape-hatch JS engine.
- Reject more than one `kind:"video"` engine entry per page. Runtime validation is enough for v1; compile-time validation can be added later.

### Runtime shape

- The bootstrap creates and owns the managed `<video>` element inside the engine mount.
- Any manifest that contains a video engine should be treated as needing the runtime bridge, because `$video.*` is a shared-signal contract, not a bootstrap-local store.
- The video engine should mount before islands hydrate, or at minimum seed default `$video.*` values before island hydration begins, so control islands render against deterministic initial state.
- The video engine owns transport, media events, fullscreen, subtitle loading, sync, and teardown. Islands own controls, overlays, and presentation.

### Why v1 still loads the runtime bridge

The current bootstrap can write shared signals into the WASM bridge, but it does not yet have a general way to observe island-written shared-signal changes from JS. Video needs both directions:

- engine -> islands for `$video.position`, `$video.playing`, `$video.activeCues`, etc.
- islands -> engine for `$video.command`, `$video.seek`, `$video.subtitleTrack`, etc.

The smallest coherent v1 is to keep the bridge store as the source of truth and expose JS-facing read and change-notification hooks from that bridge.

## Engine Kind

The `//gosx:engine video` directive declares a video engine. The compiler sets `EngineKind = "video"` and auto-injects capabilities `[CapVideo, CapFetch, CapAudio]`.

```go
case "video":
    comp.EngineKind = "video"
    comp.EngineCapabilities = append(comp.EngineCapabilities, "video", "fetch", "audio")
```

No `//gosx:capabilities` directive is required for video engines.

### Relationship to `surface`

`video` is mount-bearing like `surface`, but the factory is framework-owned:

- `surface` mounts a user-provided or program-driven engine factory.
- `video` mounts the built-in video factory.

Any path that currently says "if engine kind is `surface`, resolve a mount" should move to a helper such as `engineKindNeedsMount(kind)` and include `video`.

### One video engine per page

`$video.*` is global in v1. Multiple players on one page would collide, so the runtime should fail fast if it sees more than one video engine entry. If multi-player support becomes necessary later, move to instance-scoped names such as `$video.{engineID}.position`.

## Shared Signal Contract

The video engine owns the following shared-signal names. Islands bind to them through the existing shared-signal API, for example:

```go
playing := signal.NewShared("video.playing", false)
command := signal.NewShared("video.command", "")
```

### Outputs (engine writes, islands read)

| Signal | Type | Description |
|--------|------|-------------|
| `$video.position` | `float64` | Current playback time in seconds |
| `$video.duration` | `float64` | Total duration in seconds |
| `$video.playing` | `bool` | Whether playback is currently active |
| `$video.buffered` | `float64` | Seconds of buffer ahead of current position |
| `$video.stalled` | `bool` | True when playback has stopped because data is not ready |
| `$video.fullscreen` | `bool` | Fullscreen state |
| `$video.viewport` | `[2]int` | Player viewport `[width, height]` |
| `$video.ready` | `bool` | Enough media is ready to begin playback |
| `$video.muted` | `bool` | Current mute state |
| `$video.actualRate` | `float64` | Effective playback rate after drift correction |
| `$video.error` | `string` | User-visible error message, empty when healthy |
| `$video.syncConnected` | `bool` | True while the sync WebSocket is connected |
| `$video.subtitleTracks` | `[]TrackInfo` | Available subtitle tracks |
| `$video.subtitleStatus` | `string` | `"idle"`, `"loading"`, `"warming"`, `"ready"`, or `"error"` |
| `$video.activeCues` | `[]VideoCue` | Currently visible subtitle cues |

### Inputs (islands write, engine reads)

| Signal | Type | Description |
|--------|------|-------------|
| `$video.src` | `string` | Source URL. Writing a new value loads a new source |
| `$video.seek` | `float64` | Seek target in seconds |
| `$video.command` | `string` | `"play"`, `"pause"`, `"toggle"`, `"enter-fullscreen"`, `"exit-fullscreen"`, `"toggle-fullscreen"` |
| `$video.volume` | `float64` | Requested volume from `0.0` to `1.0` |
| `$video.mute` | `bool` | Requested mute state |
| `$video.rate` | `float64` | Requested playback rate |
| `$video.subtitleTrack` | `string` | Stable track ID, empty string = subtitles off |

### Contract notes

- Repeated writes should be treated as independent events in v1. The design should not require a reset-to-neutral trick for `command` or `seek`.
- In `"follow"` sync mode, the engine should ignore local writes to `$video.command`, `$video.seek`, and `$video.rate`. Volume, mute, and subtitle selection remain local preferences and should still work.
- The engine should seed sane defaults for all output signals as soon as it mounts, before the first media event arrives.

## Props

```go
type VideoEngineProps struct {
    Src            string      `json:"src,omitempty"`
    Sync           string      `json:"sync,omitempty"`
    SyncMode       string      `json:"sync_mode,omitempty"`     // "follow" or "lead"
    SyncStrategy   string      `json:"sync_strategy,omitempty"` // "nudge" (default) or "snap"
    SubtitleBase   string      `json:"subtitle_base,omitempty"`
    SubtitleTracks []TrackInfo `json:"subtitle_tracks,omitempty"`
}
```

Notes:

- `SubtitleTracks` is needed for non-HLS sources. The engine should always be the writer of `$video.subtitleTracks`; islands should not be responsible for populating that output.
- Standard `<video>`-style props such as `poster`, `autoplay`, `muted`, `loop`, `playsinline`, `preload`, and `crossorigin` should pass through as ordinary props and be applied to the managed element.
- `Src` is the initial source. After mount, `$video.src` is authoritative for source swaps.

## Shared-Signal Bridge Work

This is the biggest missing prerequisite in the current codebase.

### Required bridge behavior

1. JS must be able to read the current value of a shared signal when the video engine mounts.
2. JS must be notified when a shared signal changes so the video engine can react to `$video.command`, `$video.seek`, and friends.
3. JS must be able to keep using the existing `__gosx_set_shared_signal` / `__gosx_set_input_batch` path to publish engine outputs.

### Minimal v1 bridge API

- Add a runtime export such as `__gosx_get_shared_signal(name)` that returns the current value as JSON.
- Add a JS callback hook such as `window.__gosx_notify_shared_signal(name, valueJSON)` that the bridge invokes whenever a shared signal changes.
- Build a small bootstrap-side registry on top of that callback so the video engine can subscribe only to `$video.*` inputs and ignore its own output writes.
- Extend `manifestNeedsRuntimeBridge(manifest)` so any `kind:"video"` entry loads the runtime bridge.

### Ordering

Update bootstrap startup so video engines mount before islands hydrate. That prevents islands from winning the initial shared-signal seed with stale placeholder values and avoids first-frame control flicker.

## HLS.js Asset and Source Loading

HLS.js should be a framework-owned runtime asset.

### Packaging

- Vendor HLS.js in the framework source tree, for example at `client/js/vendor/hls.min.js`.
- Extend `buildmanifest.RuntimeAssets` with a dedicated hashed asset entry such as `VideoHLS`.
- Stage the compatibility copy to `/gosx/hls.min.js` for source-build and export flows, just like the other runtime assets.
- Expose the resolved path to bootstrap through the page runtime/document contract so bootstrap does not hard-code dev-only paths.

### Loading

- Lazy-load HLS.js only when the manifest contains a video engine and the current source needs HLS.js.
- Deduplicate loading through a shared promise.
- On Safari / iOS or any browser with native HLS support, skip HLS.js and set `video.src` directly.
- On source swap, tear down the previous HLS instance, clear transient state, and create a fresh attachment path.

## Sync Adapter

Sync is optional and activates only when `Sync` is non-empty.

### Modes

**`"follow"` (default):**

- Playback state comes from the server.
- Local `$video.command`, `$video.seek`, and `$video.rate` writes are ignored.
- Local volume, mute, subtitle selection, and fullscreen remain client-local.

**`"lead"`:**

- The local player is authoritative.
- Local control signals behave normally.
- The engine publishes periodic state snapshots over the socket.

### Protocol

Use a slightly richer message than the original draft so drift correction can account for transport delay:

```json
{
  "type": "sync",
  "mediaID": "episode-42",
  "position": 42.5,
  "playing": true,
  "rate": 1.0,
  "sentAtMS": 1711711712000
}
```

Notes:

- `mediaID` lets the engine ignore stale sync messages after a source change.
- `sentAtMS` lets the follower project the authoritative position forward by transport time when the stream is playing.

### Drift correction

**`"nudge"` strategy (default):**

- Compute authoritative target position from `position`, `playing`, `rate`, and `sentAtMS`.
- Sample drift every 500ms.
- Require several consecutive samples in the same direction before adjusting.
- Drift > 0.5s ahead: temporarily slow to `0.92x`.
- Drift > 0.5s behind: temporarily speed to `1.08x`.
- Drift within threshold: return to the requested rate.
- Emergency seek when drift exceeds 5s.

**`"snap"` strategy:**

- Seek directly when drift exceeds 1s.
- Simpler, more visible.

### Connection behavior

- `$video.syncConnected` should reflect socket liveness.
- In `"follow"` mode, playback continues from the last known state while disconnected; correction resumes on reconnect.
- In `"lead"` mode, do not buffer a backlog of deltas. On reconnect, send a fresh current snapshot instead.
- Reconnect with exponential backoff: 1s initial, 30s max.

## Subtitle Integration

The engine owns the subtitle pipeline. Islands only render the overlay.

### Track discovery

For HLS manifests with `#EXT-X-MEDIA:TYPE=SUBTITLES`, expose:

```go
type TrackInfo struct {
    ID       string `json:"id"`
    Language string `json:"language"`
    Title    string `json:"title"`
    Default  bool   `json:"default"`
    Forced   bool   `json:"forced"`
}
```

For non-HLS subtitle sources, seed the same shape from `SubtitleTracks` props and fetch VTT data from `SubtitleBase`.

### Track selection

- Use stable string IDs, not indices.
- `$video.subtitleTrack = ""` means subtitles off.
- For HLS-native tracks, map the selected ID back to the manifest track entry.
- For `SubtitleBase` sources, construct the VTT URL from the selected track ID.

### Warmup handling

If subtitle fetch returns HTTP 202 with `Retry-After`:

- set `$video.subtitleStatus = "warming"`
- retry every 1.5s
- cap retries at 60
- do not set `$video.error` for expected warmup

### Cue evaluation

On each `timeupdate`:

1. convert `currentTime` to milliseconds
2. binary-search the sorted cue list
3. compute the active set
4. update `$video.activeCues` only when the active set changes

### Cue shape

```go
type VideoCue struct {
    Text    string `json:"text"`               // sanitized markup subset, safe for innerHTML
    Align   string `json:"align,omitempty"`    // "start", "center", "end"
    Line    int    `json:"line,omitempty"`     // vertical position percentage
    FadeIn  int    `json:"fade_in,omitempty"`  // milliseconds
    FadeOut int    `json:"fade_out,omitempty"` // milliseconds
}
```

### Sanitization

The engine must not forward arbitrary subtitle HTML into island DOM. Preserve only a small whitelist of formatting tags / classes needed for subtitle styling and strip everything else.

## Bootstrap Integration

### Mount sequence

1. Bootstrap loads the manifest.
2. If any video engine exists, bootstrap loads the runtime bridge first.
3. Bootstrap validates that there is at most one video engine entry.
4. Bootstrap resolves the mount using the shared mount-bearing-kind helper.
5. Bootstrap creates the managed `<video>` element in the mount.
6. Bootstrap seeds default `$video.*` outputs.
7. Bootstrap wires shared-signal subscriptions for inputs.
8. Bootstrap loads HLS.js if needed and attaches the source.
9. Bootstrap attaches media, fullscreen, resize, and subtitle listeners.
10. Bootstrap opens the optional sync socket.
11. Islands hydrate after the video engine is mounted and the contract is seeded.

### Teardown

On dispose:

1. destroy the HLS instance
2. abort in-flight source and subtitle fetches
3. close the sync socket
4. remove event listeners and observers
5. detach shared-signal subscriptions
6. remove the managed `<video>` element and restore fallback content

## Implementation Plan

### Phase 1: Engine kind and mount plumbing

- Add `KindVideo`, `CapVideo`, and a shared `engineKindNeedsMount` helper in Go and JS.
- Teach `ir/lower.go` to accept `video` and auto-inject `video`, `fetch`, and `audio`.
- Update renderer / manifest code so `kind:"video"` is valid with an empty `programRef`.
- Add runtime validation for "one video engine per page".

Acceptance:

- manifest can serialize and deserialize a `kind:"video"` engine entry
- renderer emits mount markup for video engines
- bootstrap resolves a built-in video factory without `jsRef`

### Phase 2: Shared-signal bridge exposure

- Add JS-readable shared-signal snapshot access in the WASM runtime.
- Add shared-signal change notifications from bridge -> JS.
- Extend bootstrap runtime-loading rules so video pages always get the bridge.
- Change startup order so video engines mount before islands hydrate.

Acceptance:

- bootstrap-managed code can observe `$video.command` writes from an island
- bootstrap-managed code can publish `$video.position` updates and islands re-render
- engine-only video pages still load the runtime bridge correctly

### Phase 3: Local playback core

- Implement the built-in video factory in `client/js/bootstrap-src/*`.
- Create and manage the `<video>` element.
- Handle source load/swap, native HLS, HLS.js fallback, fullscreen, resize, and media event -> signal updates.
- Support standard video props plus the fixed signal inputs.

Acceptance:

- play / pause / seek / volume / mute / fullscreen work through signals
- source swaps cleanly tear down and rebuild transport
- runtime issues restore server fallback when mount fails

### Phase 4: Subtitles

- Add `TrackInfo` / `VideoCue` shared types.
- Support HLS subtitle discovery plus prop-driven subtitle tracks.
- Implement VTT fetch, warmup polling, parsing, sanitization, and active-cue evaluation.

Acceptance:

- subtitle track selection works by stable ID
- 202 warmup transitions through `"warming"` to `"ready"`
- active cue updates do not spam signals when nothing changed

### Phase 5: Sync

- Implement follow / lead socket behavior.
- Add drift correction and reconnect handling.
- Surface socket state through `$video.syncConnected`.

Acceptance:

- follower converges without visible jitter under normal latency
- snap mode seeks when drift crosses threshold
- reconnect recovers cleanly after disconnect

### Phase 6: Packaging, docs, and examples

- Stage HLS.js through build, export, and runtime-asset serving paths.
- Regenerate `bootstrap.js` / `bootstrap-lite.js` from `bootstrap-src`.
- Update docs and add at least one end-to-end example page.

Acceptance:

- source-build, hashed build, and `gosx export` all ship the HLS asset
- runtime asset serving works at both compatibility and hashed paths
- the example app runs with zero authored JS

## Likely Files Touched

| File | Change |
|------|--------|
| `engine/engine.go` | Add `KindVideo`, `CapVideo`, and mount-bearing-kind helper |
| `ir/lower.go` | Accept `//gosx:engine video`, auto-inject capabilities |
| `hydrate/manifest.go` | Ensure `kind:"video"` entries work cleanly with ordinary props and optional empty `programRef` |
| `island/island.go` | Treat `video` as mount-bearing; update page-head/runtime decisions |
| `client/bridge/bridge.go` | Expose shared-signal read / change notification hooks for JS |
| `client/wasm/main.go` | Register the new shared-signal bridge exports |
| `client/js/bootstrap-src/20-scene-mount.js` | Extend engine mount handling for `video` |
| `client/js/bootstrap-src/30-tail.js` | Add built-in video factory, runtime-loading rule, mount ordering, and teardown |
| `client/js/build-bootstrap.mjs` | Regenerate bundled bootstrap assets after source edits |
| `buildmanifest/manifest.go` | Add a hashed runtime asset entry for HLS.js |
| `cmd/gosx/build.go` | Stage the HLS asset into hashed runtime outputs |
| `cmd/gosx/export.go` | Copy the compatibility HLS asset for static export |
| `server/runtime_assets.go` | Serve `/gosx/hls.min.js` and hashed HLS runtime assets |
| `server/page.go` / `server/runtime.go` | Surface the resolved HLS asset path to bootstrap |
| `client/js/runtime.test.js` | Bootstrap coverage for video mount, teardown, signal flow, HLS loading, and fallback |
| `client/bridge/bridge_test.go` | Shared-signal notification coverage |
| `client/wasm/main_test.go` | Runtime export coverage for shared-signal read / notify hooks |
| `hydrate/manifest_test.go` | Manifest coverage for video entries |
| `island/island_test.go` | Renderer / page-head coverage for video pages |

## Usage Examples

### Minimal player

```gsx
//gosx:engine video
func Player(props struct{ Src string }) Node {
    return <video src={props.Src} playsinline />
}
```

### Controls island using the existing shared-signal API

```gsx
//gosx:island
func PlayerControls() Node {
    playing := signal.NewShared("video.playing", false)
    position := signal.NewShared("video.position", 0.0)
    duration := signal.NewShared("video.duration", 0.0)
    stalled := signal.NewShared("video.stalled", false)
    command := signal.NewShared("video.command", "")

    return <div class="player-controls">
        <If when={stalled.Get()}>
            <span class="buffering">Buffering...</span>
        </If>
        <button onClick={func() {
            if playing.Get() {
                command.Set("pause")
            } else {
                command.Set("play")
            }
        }}>
            Toggle
        </button>
        <span>{formatTime(position.Get())} / {formatTime(duration.Get())}</span>
    </div>
}
```

### Subtitle overlay island

```gsx
//gosx:island
func SubtitleOverlay() Node {
    cues := signal.NewShared("video.activeCues", []VideoCue{})

    return <div class="subtitle-overlay">
        {range cues.Get() as cue}
            <div class="subtitle-line" innerHTML={cue.Text}></div>
        {end}
    </div>
}
```

## Done Definition

This feature is done when all of the following are true:

- a `video` engine can mount without authored JS
- controls islands can drive playback entirely through shared signals
- subtitles work for both HLS-native and prop-supplied track lists
- sync mode is optional and observable through signals
- runtime/build/export pipelines all ship the framework-owned HLS asset
- the test suite covers the compiler, manifest, renderer, bridge, bootstrap, and asset-serving paths that make the feature possible
