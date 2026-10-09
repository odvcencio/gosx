// Package audio is a small WebAudio bus graph and one-shot player for a
// browser game client.
//
// Package game already describes the data a game wants to play — see
// game.AudioManifest, game.AudioBus, game.AudioClip, game.AudioPlayback —
// but nothing in gosx consumed that manifest and actually drove WebAudio.
// This package is that consumer.
//
// Host abstracts the WebAudio graph nodes this package needs: gain buses, a
// compressor, and buffer sources. HostJS, built only under
// GOOS=js/GOARCH=wasm, implements Host over a real browser AudioContext. A
// fake Host in this package's tests implements the same interface, so the
// bus graph wiring and playback bookkeeping in BusGraph and Player are
// unit-tested natively.
//
// Player.Unlock must be called from inside a user gesture handler (a click
// or key handler) — every modern browser starts an AudioContext suspended
// until one fires, and Unlock is this package's counterpart to that
// requirement.
//
// HostJS also provides AudioWorklet lifecycle and structured-clone messaging,
// transferable buffers, sample-rate and latency queries, and context shutdown.
// Processors are loaded from external asset URLs; their protocol and any DSP
// kernel remain application-owned. The package does not generate JavaScript.
// Promise-waiting operations must run in a goroutine, never block a syscall/js
// event callback. Closing a host cancels pending callers and detaches callbacks.
//
// StartRegion and SetLoopRegion add audio sprites and bounded loops without
// extending the existing Host/SourceNode interfaces. SetGainTarget provides
// click-free gain automation. A host lacking those optional capabilities
// returns ErrUnsupported instead of silently playing a different sound.
//
// The manifest player covers one master bus with a compressor, named child
// buses, and scheduled buffer playback with volume and rate. Positional/3D audio
// (game.AudioPlayback's Position, Velocity, and rolloff fields) is not
// implemented here; a game that needs it can read those fields itself from
// whatever consumes game.Runtime.Events.
package audio
