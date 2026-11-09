# Audio

GoSX plays all of a page's sound through one mixer: one `AudioContext` and five
buses. Clips, the built-in synth (`scene.AudioCue`), streamed `<audio>`
elements, countdown cues, Go game audio and a WASM music kernel all feed it, so
one volume slider reaches every sound.

```text
clips ─┐
synth ─┼─► sfx ───┐
Go SFX ┘          │
music kernel ─► music ──┤
voice lines ──► voice ──┼─► master ─► compressor ─► speakers
buttons, cues ► ui ─────┘
```

The mixer lives in the browser bootstrap as `window.__gosx.audioMixer`. The Go
package `m31labs.dev/gosx/game/audio` wraps it for `js/wasm` games and gives a
native fake-friendly API for tests.

## Buses

Every game mixer has `master`, `music`, `sfx`, `voice` and `ui`. A bus has a
volume from 0 to 1, a mute flag, and any number of named ducks. Its gain is
`volume × lowest duck factor`, or 0 while muted. Changes ramp over about 15 ms, so
a slider never clicks.

```go
host, _ := audio.NewHostJS()
mixer := audio.NewMixer(host)

mixer.SetVolume(audio.MusicBus, 0.6)
mixer.SetMuted(audio.SFXBus, false)
```

### Save and restore volumes

`Settings` returns every bus's volume and mute flag as a value that marshals to
JSON; `ApplySettings` restores it and returns the bus IDs it applied.

```go
saved, _ := json.Marshal(mixer.Settings())    // write to localStorage, a cookie, or the server
...
var s map[string]audio.BusSettings
_ = json.Unmarshal(saved, &s)
mixer.ApplySettings(s)
```

Volumes set before the first user gesture are kept and applied when the browser
creates the context.

### Duck one bus for another

Ducking lowers a bus while something else plays. The key names the caller, so two
callers do not undo each other; the lowest factor wins.

```go
mixer.Duck(audio.MusicBus, "dialogue", 0.35, 0.15)   // to 35% over 150 ms
// ... when the line ends:
mixer.Unduck(audio.MusicBus, "dialogue", 0.6)        // recover over 600 ms
```

### From JavaScript

```js
const mixer = window.__gosx.audioMixer;
mixer.setVolume("music", 0.5);
mixer.setMuted("sfx", true);
mixer.duck("music", 0.4, 0.1, "dialogue");
mixer.unduck("music", "dialogue", 0.5);
mixer.onChange((e) => console.log(e.bus, e.volume, e.muted, e.effective));
```

`window.__gosx.audio.setBusVolume(bus, v)` sets the same state. Any other bus ID
you name (a manifest bus, say `ambience`) becomes a bus under `master`.

## Clips

`game.AudioManifest` declares clips and extra buses. `Player` decodes and plays
them through the mixer.

```go
manifest := game.AudioManifest{
	Clips: []game.AudioClip{{ID: "door", URI: "/audio/door.ogg", Bus: audio.SFXBus, Preload: true}},
}
mixer := audio.NewMixer(host, manifest)         // adds the manifest's buses to the standard five
player := audio.NewPlayer(host, mixer.Graph(), audio.HTTPLoader{})
_ = player.LoadManifest(manifest)
player.Play(game.AudioPlayback{Clip: "door"})
```

Call `player.Unlock()` from a click or key handler. Browsers keep a new context
suspended until then.

In server-driven scenes, `scene.AudioCue{Clip: "door", Bus: "sfx"}` plays a
clip through `window.__gosx.audio`; `Bus` names the mixer bus. Without `Bus`,
clips play on `master`.

## Sound effects

`audio.SFX` is a pooled one-shot player. It reuses its per-voice gain and panner
nodes, so a burst of hits creates no new graph nodes after the first few.

```go
sfx := audio.NewSFX(mixer, player, audio.SFXConfig{MaxVoices: 32, MaxPerClip: 6})

voice, err := sfx.Play(audio.SFXRequest{
	Playback: game.AudioPlayback{Clip: "hit", Volume: 0.8},
	Priority: 2,
})
```

Limits:

- `MaxVoices` (default 32) is the most voices at once.
- `MaxPerClip` (default 8) is the most voices of one clip, so one sound cannot
  fill the pool.
- At a limit, a new voice replaces the lowest-priority voice (the oldest among
  equals), but only if its own priority is at least as high. Otherwise `Play`
  returns `audio.ErrVoiceDropped`.

`sfx.Stats()` reports active voices, pooled nodes, how many nodes were ever
created, and the drop and steal counts. `voice.Stop()`, `voice.SetVolume(v)` and
`sfx.StopAll()` end or adjust voices.

### Positional sound

Give the playback a position and the voice plays through a 3D panner (HRTF) with
distance rolloff:

```go
pos := game.Vec3{X: 4, Y: 0, Z: -10}
voice, _ := sfx.Play(audio.SFXRequest{
	Playback: game.AudioAt(pos, game.AudioPlayback{Clip: "engine", RefDistance: 2, MaxDistance: 60, RolloffFactor: 1.2}),
})

// Follow a moving scene object. Update runs once per frame, after the simulation step.
voice.Follow(func() (game.Vec3, bool) { return enemy.Position, enemy.Alive })
sfx.Update()
```

When the callback reports `false` (the object is gone) the voice stops following.
`Play` returns `audio.ErrUnsupported` for a positioned request on a host that
cannot position sound.

**The listener follows the camera.** Scene3D calls
`window.__gosx.audioMixer.updateCamera` each frame, which sets the listener's
position and orientation from the render camera. This needs no code in the game.
It does nothing until audio is in use, and
`window.__gosx.audioMixer.followCamera(false)` turns it off. A game that wants
another listener (a first-person body, say) calls
`sfx.SetListener(position, forward, up)` or
`sfx.SetListenerLookAt(eye, target, up)`.

## The built-in synth and streamed audio

- `scene.AudioCue` synth cues and `SynthPatch` voices (the arcade synth) now play
  on the shared context through the `sfx` bus, or the bus the cue names with
  `Bus`. The synth keeps its 28-voice limit.
- A clip that cannot be decoded plays through an `<audio>` element. The mixer
  captures that element into its bus, so a streamed track obeys the same volume,
  mute and duck state. The clip's own volume still applies to the element.
- Countdown cues (`data-gosx-countdown-cue`) play on the `ui` bus.

If a page loads the arcade synth without the main bootstrap, it keeps a private
context and chain, as before.

## Host a WASM audio kernel

A game can run a synthesis engine written in WebAssembly, such as
[Cicada](https://github.com/odvcencio/cicada), inside an AudioWorklet. GoSX owns
the browser side and knows nothing about the engine.

The kernel is a WASM reactor with these exports (default prefix `gosx_audio_`):
`_initialize`, `init(rate, maxFrames, channels)`, `render(frames)`, `out_ptr`,
`cmd_ptr`, `cmd_cap`, `cmd_commit(n)`, `msg_ptr` and `msg_drain`, plus an
exported `memory`. It renders planar float32 audio, takes fixed-size command
records, and returns fixed-size message records. A `worklet.Backend` describes the
record sizes, opcodes and capabilities; the full contract is in
[`game/audio/worklet/README.md`](../game/audio/worklet/README.md).

```go
node, err := kernel.Start(kernel.Config{
	Mixer:           mixer,
	Bus:             audio.MusicBus,        // the kernel's output feeds this bus
	Backend:         backend,               // the kernel's record layout
	WASM:            kernelBytes,           // the reactor module
	Image:           scoreBytes,            // optional data blob, such as a compiled score
	Info:            worklet.ImageInfo{TempoMilli: 120000},
	ProcessorPrefix: "/assets/",            // where you mount worklet.Handler()
})
```

Serve `worklet.Handler()` from your server under that prefix. `Start` loads the
processor, compiles the module once, waits until the kernel reports ready, and
connects it to the bus. Call it from a goroutine after a user gesture.

| Method | Use |
| --- | --- |
| `node.Send(records)` | Post a batch of command records. The processor rejects the whole batch if any record is invalid. |
| `node.OnMessages(fn)` | Receive the kernel's message records. The host returns the transfer buffer for you. |
| `node.OnFault(fn)` | Receive a fault code; playback has stopped. |
| `node.Replace(image, info)` | Swap the data blob. With a `Transport` in the backend, the swap lands on the next bar. |
| `node.State()` | `Ready`, `Playing`, `Fault`, `Error`, `Frames` (position), `Underruns`. |
| `node.RequestTiming()`, `node.OnTiming(fn)` | CPU-time histogram and underrun count. |
| `node.Close()` | Stop the kernel and disconnect the node. |

`Frames` is the engine position in sample frames since the last play. The
processor drains messages every eighth render quantum, so it lags the audio clock
by up to 1,024 frames.

The message protocol is plain Go in package `worklet` (`worklet.State`,
`worklet.Event`, `SplitRecords`), so it is unit-tested without a browser. The
processor itself has a Node test suite (`client/js/audio-wasm-worklet.test.mjs`)
and a real-browser `OfflineAudioContext` test (`e2e/audio_wasm_worklet_offline_e2e_test.go`).

For Cicada, see Cicada's `docs/manual/gosx-game.md`: it supplies the `Backend`,
the kernel and the score file.

## Migrating an existing game

GoSX adds the mixer without removing an exported symbol, but two behaviours change
when the page has the GoSX bootstrap:

- `NewHostJS` and `NewHostJSWithOptions` adopt the page's shared context.
  `HostOptions.SampleRate` applies if the call creates the context; if the shared
  context already runs at another rate, the host builds a private one and
  `host.Shared()` reports false. `HostOptions{Private: true}` always builds a
  private context.
- `Close` on a shared host releases the host's nodes but leaves the page's
  context open.
- `host.Destination()` is the master bus input, so a game's own graph now passes
  through the master compressor.

A game that builds its own `BusGraph` keeps working: on a shared host the graph is
the page mixer's buses. `Bus("master")` now returns the master bus, and
`Graph.Master()` returns nil there.
