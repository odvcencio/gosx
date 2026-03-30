# GoSX Video Engine

**Date:** 2026-03-29
**Status:** Draft

## Problem

Every GoSX application that needs video playback rewrites the same glue: HLS.js setup, `<video>` element management, MediaSource wiring, event listeners for position/duration/buffering, subtitle fetching and cue evaluation, and WebSocket sync with drift correction. This is hundreds of lines of browser interop that belongs in the framework, not in application code.

## Goals

1. Add `video` as a third engine kind alongside `worker` and `surface`.
2. Provide a fixed `$video.*` signal contract that any island can consume.
3. Handle HLS.js transport, subtitle management, and optional WebSocket sync internally.
4. Ship HLS.js as a framework module asset — lazy-loaded, zero cost for non-video pages.
5. A GoSX video player requires only a `.gsx` template and zero authored JavaScript.

## Engine Kind

The `//gosx:engine video` directive declares a video engine. The compiler sets `EngineKind = "video"` and auto-injects capabilities `[CapVideo, CapFetch, CapAudio]`.

```gsx
//gosx:engine video
func Player(props PlayerProps) Node {
    return <video src={props.src} />
}
```

The framework handles everything else.

**Relationship to `surface`:** A video engine uses the same mount-resolution path as a surface engine — the bootstrap finds or creates a mount `<div>` by ID. The difference is the factory: surface engines invoke a user-provided or program-driven factory, while video engines invoke the framework's built-in video factory. Every bootstrap check for `entry.kind === "surface"` must also match `"video"` for mount resolution purposes.

**One video engine per page.** The `$video.*` signal namespace is global. Multiple video engines on a single page would collide. This constraint matches how most applications use video. If a future use case requires multiple players, instance-scoped signals (e.g., `$video.{engineID}.position`) can be added as an extension without breaking the single-engine contract.

## Signal Contract

Every video engine exposes these signals automatically. The author does not declare them.

### Outputs (engine writes, islands read)

| Signal | Type | Description |
|--------|------|-------------|
| `$video.position` | `float64` | Current playback time in seconds |
| `$video.duration` | `float64` | Total duration in seconds |
| `$video.playing` | `bool` | Whether video is actively playing |
| `$video.buffered` | `float64` | Seconds of buffer ahead of position |
| `$video.stalled` | `bool` | True when playback stopped due to buffering (not user pause) |
| `$video.fullscreen` | `bool` | Fullscreen state |
| `$video.viewport` | `[2]int` | Player container dimensions `[width, height]` |
| `$video.ready` | `bool` | Enough data buffered to begin playback |
| `$video.muted` | `bool` | Whether video is muted (distinct from volume=0 for autoplay policy) |
| `$video.actualRate` | `float64` | Actual playback rate (may differ from requested rate during drift correction) |
| `$video.error` | `string` | Error message, empty when healthy |
| `$video.subtitleTracks` | `[]TrackInfo` | Available subtitle tracks with language, default, forced flags |
| `$video.subtitleStatus` | `string` | `"idle"`, `"loading"`, `"warming"`, `"ready"`, `"error"` |
| `$video.activeCues` | `[]VideoCue` | Currently visible subtitle cues |

### Inputs (islands write, engine reads)

| Signal | Type | Description |
|--------|------|-------------|
| `$video.src` | `string` | HLS manifest URL — changing this loads a new source |
| `$video.seek` | `float64` | Write a value to seek to that position |
| `$video.command` | `string` | `"play"`, `"pause"`, `"toggle"` |
| `$video.volume` | `float64` | 0.0 to 1.0 |
| `$video.mute` | `bool` | Write to toggle mute state |
| `$video.rate` | `float64` | Requested playback rate, 1.0 = normal |
| `$video.subtitleTrack` | `int` | Selected subtitle track index, -1 = off |

The engine subscribes to input signal changes and reacts immediately. Writing `$video.src` triggers an HLS source swap. Writing `$video.seek` seeks the video. Writing `$video.command` controls play/pause.

### Command and Seek Idempotency

`$video.command` and `$video.seek` are imperative in nature but delivered via reactive signals. Writing the same value twice produces no signal change and no reaction. To handle this, the engine treats these signals as edge-triggered: it reads the value on change, executes the action, then resets the signal to a neutral value (`""` for command, `-1` for seek). This ensures the next write of the same value triggers a change.

### Error Clearing

`$video.error` clears (resets to `""`) when:
- A new source is loaded (`$video.src` changes)
- Playback resumes successfully after an error
- The engine is disposed

Transient network errors that resolve on retry do not set `$video.error` — only persistent failures that stop playback.

## Props

```go
type VideoEngineProps struct {
    Src          string `json:"src"`                     // HLS manifest URL (also settable via $video.src signal)
    Sync         string `json:"sync,omitempty"`          // WebSocket URL for synchronized playback
    SyncMode     string `json:"sync_mode,omitempty"`     // "follow" or "lead"
    SyncStrategy string `json:"sync_strategy,omitempty"` // "nudge" (default) or "snap"
    SubtitleBase string `json:"subtitle_base,omitempty"` // Base URL for subtitle tracks
}
```

If `Sync` is empty, the engine is a standalone player with no sync behavior.

## HLS.js Module

HLS.js ships as a framework-owned asset at `gosx/engine/video/hls.min.js`. The build system copies it to the application's static output directory.

The bootstrap lazy-loads HLS.js only when a video engine appears in the page manifest:

1. Bootstrap reads manifest, finds engine with `kind: "video"`.
2. Injects `<script>` for the HLS.js module asset.
3. Waits for load.
4. Mounts the video engine.

Non-video pages pay zero cost — no script tag, no download, no parse.

For browsers with native HLS support (Safari, iOS), the engine skips HLS.js and sets the `<video>` element's `src` directly.

## Sync Adapter

Optional. Activated when the `Sync` prop contains a WebSocket URL.

### Sync Modes

**`"follow"` (default):** Position derived from server clock. The engine receives sync messages and adjusts local playback to match. `$video.command` inputs are ignored — the server controls play/pause state.

**`"lead"`:** The engine is the playback authority. `$video.command` inputs work normally. The engine broadcasts position updates to other viewers via WebSocket.

### Drift Correction

**`"nudge"` strategy (default):**

- Measure drift every 500ms: `localPosition - serverPosition`
- Maintain a sliding window of 10 samples
- Require 5 consecutive samples in the same direction before adjusting (hysteresis prevents jitter from noisy measurements)
- Drift > 0.5s ahead: set playback rate to 0.92x (slow down gradually)
- Drift > 0.5s behind: set playback rate to 1.08x (speed up gradually)
- Drift within 0.5s: restore rate to 1.0x
- Emergency seek if drift exceeds 5s
- Never rewind for drift — only seek forward or to server position

During drift correction, `$video.actualRate` reflects the adjusted rate (0.92 or 1.08) while `$video.rate` remains at the user's requested value.

**`"snap"` strategy:**

- Hard seek to server position whenever drift exceeds 1s
- Simpler but produces visible jumps

### WebSocket Protocol

The engine expects JSON messages:

```json
{"type": "sync", "position": 42.5, "playing": true}
```

In `"lead"` mode, the engine sends the same format. The protocol is intentionally minimal — the application's WebSocket handler maps its internal sync protocol to this shape.

### Connection Loss Behavior

**In `"follow"` mode:** When the WebSocket disconnects, the engine continues playback at the last known position and rate. Drift correction pauses (no server reference). `$video.command` inputs remain ignored. On reconnect, the engine receives the current server position and resumes drift correction, seeking if the gap exceeds the strategy's threshold.

**In `"lead"` mode:** The engine continues playback normally. Position updates are buffered and sent on reconnect.

WebSocket reconnection uses exponential backoff (initial 1s, max 30s).

## Subtitle Integration

The video engine handles the full subtitle pipeline internally.

### Track Discovery

When HLS.js loads a manifest containing `#EXT-X-MEDIA:TYPE=SUBTITLES` entries, the engine extracts track metadata and writes `$video.subtitleTracks`:

```go
type TrackInfo struct {
    Index    int    `json:"index"`
    Language string `json:"language"`
    Title    string `json:"title"`
    Default  bool   `json:"default"`
    Forced   bool   `json:"forced"`
}
```

For non-HLS subtitle sources (e.g., goetrope's `/stream/{id}/subtitles/{track}.vtt` pattern), the application sets the `SubtitleBase` prop and the track list comes from the page data, written to `$video.subtitleTracks` by an island.

### Track Selection

An island writes `$video.subtitleTrack` with a track index. The engine constructs the VTT URL (`SubtitleBase` + track index + `.vtt`) and fetches it.

Track indices depend on the source's track ordering. For persistence across sessions, applications should store the language code alongside the index and re-resolve on load. The engine does not handle this — track persistence is the application's concern.

### Warmup Handling

If the server returns HTTP 202 with a `Retry-After` header, the engine polls automatically:

- 1.5-second interval between retries
- Maximum 60 retries (90 seconds)
- `$video.subtitleStatus` set to `"warming"` during polling
- `$video.error` not set for 202 — this is expected warmup behavior, not an error
- On success: `$video.subtitleStatus` transitions to `"ready"`

### Cue Evaluation

Event-driven, not polling. On each `timeupdate` event from the `<video>` element:

1. Convert `currentTime` to milliseconds.
2. Binary-search the sorted cue list for active cues (`startMS <= position < endMS`).
3. Compute a signature hash of the active cue set.
4. Write `$video.activeCues` only if the signature changed.

This produces minimal signal updates — islands re-render only when visible cues actually change.

### Cue Object Shape

```go
type VideoCue struct {
    Text    string `json:"text"`              // May contain HTML: <b>, <i>, <c.color-RRGGBB>, <c.fs-N>
    Align   string `json:"align,omitempty"`   // "start", "center", "end"
    Line    int    `json:"line,omitempty"`     // Vertical position percentage
    FadeIn  int    `json:"fade_in,omitempty"`  // Fade-in duration in ms
    FadeOut int    `json:"fade_out,omitempty"` // Fade-out duration in ms
}
```

The engine handles timing and data. The island handles presentation. The engine never touches the subtitle overlay DOM.

### VTT Parsing

The engine's built-in parser handles:

- Standard WebVTT timing and cue settings (`align`, `line`, `position`, `size`)
- `NOTE data-fade-in="N" data-fade-out="N"` lines (custom extension from ASS enrichment)
- HTML markup preservation (`<b>`, `<i>`, `<u>`, `<s>`, `<c.color-*>`, `<c.fs-*>`)
- Malformed cue graceful skipping

## Bootstrap Integration

The video engine factory is built into `bootstrap.js` (not an external script). It activates only when the manifest contains a video engine.

### Mount Sequence

1. Bootstrap reads manifest, finds `kind: "video"` engine entry.
2. Uses the same mount-resolution path as `"surface"` — finds the mount div by `mountId`.
3. Lazy-loads `hls.min.js` from the framework's static asset path.
4. Creates a `<video>` element inside the engine's mount div.
5. Initializes HLS.js (or native HLS for Safari).
6. Registers `$video.*` signals in the shared signal store.
7. Subscribes to input signals (`$video.src`, `$video.seek`, `$video.command`, etc.).
8. Attaches event listeners (`timeupdate`, `durationchange`, `play`, `pause`, `waiting`, `stalled`, `error`, `fullscreenchange`, `volumechange`).
9. If sync props present, opens WebSocket connection.
10. Loads initial source from `Src` prop or `$video.src` signal.

### Teardown

On engine dispose:

1. Destroy HLS.js instance.
2. Close WebSocket connection.
3. Remove event listeners.
4. Remove `<video>` element from mount.
5. Unsubscribe from all signals.

## Compiler Changes

The IR lowerer (`ir/lower.go`) recognizes `//gosx:engine video`:

```go
case "video":
    comp.EngineKind = "video"
    comp.EngineCapabilities = append(comp.EngineCapabilities, "video", "fetch", "audio")
```

No `//gosx:capabilities` directive needed for video engines — the capabilities are implied.

## Files Changed

| File | Change |
|------|--------|
| `engine/engine.go` | Add `KindVideo = "video"`, `CapVideo = "video"` |
| `engine/video/` | New package: video engine types (`VideoEngineProps`, `TrackInfo`, `VideoCue`) |
| `engine/video/hls.min.js` | Vendored HLS.js asset (framework-owned, version-pinned) |
| `engine/video/signals.go` | Standard signal definitions for `$video.*` namespace |
| `engine/render_bundle.go` | Handle `"video"` kind in engine entry rendering (same mount path as `"surface"`) |
| `ir/lower.go` | Handle `"video"` in `parseEngineDirective`, auto-inject capabilities |
| `client/js/bootstrap.js` | Add video engine factory, lazy HLS.js loader, mount-resolution to treat `"video"` like `"surface"` |
| `client/wasm/main.go` | Register video signal definitions in WASM runtime |
| `client/bridge/bridge.go` | Handle `$video.*` signal namespace for video engines |
| `hydrate/manifest.go` | Add nested `*VideoConfig` struct to `EngineEntry` (sync URL, mode, strategy, subtitle base) |
| `build/` | Asset pipeline: copy `engine/video/hls.min.js` to output static directory |
| `engine/engine_test.go` | Test `KindVideo` config, capability auto-injection, mount resolution |
| `engine/video/signals_test.go` | Test signal definitions and namespacing |

## Usage Examples

### Minimal player (no sync, no subtitles)

```gsx
//gosx:engine video
func Player(props struct{ Src string }) Node {
    return <video src={props.src} />
}
```

### Synchronized player with subtitles

```gsx
//gosx:engine video
func WatchPlayer(props WatchPlayerProps) Node {
    return <video src={props.src} sync={props.sync} sync_mode={props.sync_mode} subtitle_base={props.subtitle_base} />
}
```

### Subtitle overlay island

The `$video.activeCues` signal is provided by the video engine. The display option signals (`$subtitle.size`, etc.) are application-level shared signals declared by the island — not part of the video engine contract.

```gsx
//gosx:island
func SubtitleOverlay() Node {
    cues := $video.activeCues
    size := signal.New("m")   // Application-level, persisted to localStorage
    color := signal.New("white")
    bg := signal.New("solid")

    return <div class={"subtitle-overlay subtitle--size-" + size + " subtitle--color-" + color + " subtitle--bg-" + bg}>
        <div class="subtitle-overlay-text">
            {range cues as cue}
                <div class="subtitle-overlay-line" innerHTML={cue.text}></div>
            {end}
        </div>
    </div>
}
```

### Player controls island

```gsx
//gosx:island
func PlayerControls() Node {
    playing := $video.playing
    position := $video.position
    duration := $video.duration
    stalled := $video.stalled

    return <div class="player-controls">
        <If when={stalled}>
            <span class="buffering">Buffering...</span>
        </If>
        <button onClick={() => { $video.command = playing ? "pause" : "play" }}>
            {playing ? "Pause" : "Play"}
        </button>
        <span>{formatTime(position)} / {formatTime(duration)}</span>
    </div>
}
```

## Not In Scope

- DASH/MPEG-DASH support — HLS only for now
- DRM (Widevine, FairPlay) — can be added later via engine props
- Picture-in-picture API — can be added as a signal later
- Chromecast/AirPlay — external concern, not engine responsibility
- Video recording/capture — different engine kind
- Audio track selection (`$video.audioTracks` / `$video.audioTrack`) — can follow the subtitle pattern later
- Multi-engine per page — requires instance-scoped signal namespacing
