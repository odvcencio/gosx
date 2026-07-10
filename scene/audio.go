// Package scene is GoSX's typed authoring surface for Scene3D content:
// meshes, lights, cameras, particles, physics, motion, and (via this file)
// audio. Types here describe scene state declaratively in Go; each has a
// lowering method that produces the exact map/JSON shape the client runtime
// (client/js/bootstrap-src/*.js) already consumes, so the server and the
// generated client bundle share one source of truth for the wire schema.
//
// # Audio: gosxAudio vs arcadeAudio
//
// GoSX ships two independent WebAudio engines in the client runtime. This
// file is the typed front door to both — study the two engines below before
// picking one.
//
//   - gosxAudio (client/js/bootstrap-src/05-document-env.js, exposed as
//     window.__gosx.audio) is a manifest-driven sample player: named buses
//     with volume/mute, preloaded or streamed clips addressed by ID, and
//     optional 3D PannerNode spatialization per playback. Use Audio,
//     AudioBus, and AudioClip when you have real audio assets (music,
//     voice-over, recorded SFX) to register once at mount time and then
//     play/stop by ID, optionally positioned in world space.
//
//   - arcadeAudio (client/js/bootstrap-src/30-tail.js) is a procedural
//     synth: oscillator tones, frequency sweeps, and filtered noise bursts
//     shaped by short attack/decay/sustain/release gain envelopes, mixed
//     through a shared compressor bus with a 28-voice limiter. It needs no
//     asset loading. Use SynthPatch (built from ToneLayer, SweepLayer, and
//     NoiseLayer) when you want short, code-generated stingers — arcade-
//     style hit/guard/meter/UI blips driven straight from gameplay state.
//     arcadeAudio also ships a fixed vocabulary of ~15 named cues (see the
//     Cue field on AudioCue) that a fighting-game demo already drives this
//     way; SynthPatch lets any scene define new cues without touching the
//     client bundle.
//
// # Declaring a manifest vs. firing a cue
//
// Audio is a one-time declaration: buses and clips a scene registers when
// its engine mounts. Lower it with Props and assign the result under the
// "audio" key of the engine's props — e.g. props["audio"] = manifest.Props().
// The client's mountEngine already does the rest: whenever entry.props.audio
// is present it calls window.__gosx.audio.registerManifest(entry.props.audio)
// automatically (client/js/bootstrap-src/30-tail.js, ~line 3712). No client
// changes were needed for this path.
//
// AudioCue is the live-fire counterpart: a single "play this now" event a
// server-driven scene sends per tick or on demand. See AudioCue's doc for
// its two delivery paths (an existing embedded-tick-field path with zero
// client changes, and a new dedicated hub-event path this change adds).
package scene

import "strings"

// AudioBus is a named volume/mute group in the gosxAudio manifest.
//
// Volume is a linear gain in [0,1]; nil means "unspecified" — the client
// leaves an already-registered bus's volume untouched, or defaults a new
// bus to 1. Muted is a hard mute independent of Volume.
//
// Parent names a containing bus for organizational purposes, but the
// current client only ever composites a bus's own Volume/Muted against the
// single hard-coded "master" bus — it does not walk a Parent chain. Buses
// are otherwise flat.
type AudioBus struct {
	// ID is the bus name. Required; buses without an ID are dropped by the
	// client. The bus named "master" always exists and is the root of the
	// volume chain gosxAudioBusVolume multiplies through.
	ID string
	// Parent optionally names a containing bus (accepted on the wire;
	// not yet composited beyond one level — see type doc).
	Parent string
	// Volume is linear gain in [0,1]. Values outside the range are
	// clamped client-side. nil omits the field so an existing bus's
	// current volume is preserved.
	Volume *float64
	// Muted hard-mutes this bus (and, if this is "master", everything).
	Muted bool
}

// AudioClip is a sample-based clip registered with gosxAudio: a decodable
// audio asset (fetched once, cached, and decoded via
// AudioContext.decodeAudioData) playable by ID through AudioCue.Clip or the
// client's window.__gosx.audio.play(id, options).
type AudioClip struct {
	// ID addresses this clip in later play/stop calls. Required.
	ID string
	// Src is the clip's URL. Required — clips without a Src are dropped
	// by the client.
	Src string
	// ContentType is an optional MIME type hint (e.g. "audio/mpeg").
	ContentType string
	// Bus names the AudioBus this clip's volume is composited against.
	// Defaults to "master" when empty.
	Bus string
	// Preload hints the client to fetch/decode this clip immediately
	// rather than lazily on first play.
	Preload bool
	// Loop makes playback repeat until explicitly stopped, unless
	// overridden per-play by an AudioCue's Loop field.
	Loop bool
	// Volume is this clip's own linear gain in [0,1], multiplied with the
	// bus volume and any per-play volume at playback time. nil defaults
	// to 1 client-side.
	Volume *float64
	// Rate is the clip's default playback rate (1 = normal speed). nil
	// defaults to 1 client-side; the client floors any resolved rate at
	// 0.05.
	Rate *float64
	// Metadata is passed through verbatim for application-specific use
	// (e.g. captions, loudness tags); the client stores it but does not
	// interpret it.
	Metadata map[string]any
}

// Audio is the gosxAudio manifest: buses and clips a scene registers once,
// typically when its engine mounts.
//
// Lower it with Props and assign the result under an engine's "audio" prop
// key. The client already wires this up with no changes required by this
// package: mountEngine calls window.__gosx.audio.registerManifest(entry.
// props.audio) whenever that key is present (client/js/bootstrap-src/
// 30-tail.js, ~line 3712).
type Audio struct {
	// Buses declares named volume/mute groups. The client always has an
	// implicit "master" bus; declaring "master" here overrides its
	// volume/mute instead of creating a duplicate.
	Buses []AudioBus
	// Clips declares sample-based clips to register (see AudioClip).
	Clips []AudioClip
	// MasterVolume, if non-nil, sets the master bus's volume as part of
	// this manifest. It composes with Muted (see Muted's doc for a
	// client-side quirk when only one of the two is set).
	MasterVolume *float64
	// Muted hard-mutes the master bus.
	//
	// Quirk inherited from the current client (gosxAudioRegisterManifest
	// in 05-document-env.js): Muted is only applied together with
	// MasterVolume — if MasterVolume is nil, Muted is ignored even when
	// true, because the client only re-registers the master bus when the
	// manifest carries a "masterVolume" key at all. Set MasterVolume
	// (e.g. to its previous/current value) whenever you need Muted to
	// take effect.
	Muted bool
}

// Props lowers b to the map shape gosxAudioNormalizeBus expects. Returns
// nil if ID is empty (mirrors the client, which drops busless entries).
func (b AudioBus) Props() map[string]any {
	id := strings.TrimSpace(b.ID)
	if id == "" {
		return nil
	}
	out := map[string]any{"id": id}
	setString(out, "parent", b.Parent)
	setNumericPtr(out, "volume", b.Volume)
	if b.Muted {
		out["muted"] = true
	}
	return out
}

// Props lowers c to the map shape gosxAudioNormalizeClip expects. Returns
// nil if ID or Src is empty (mirrors the client, which drops such clips).
func (c AudioClip) Props() map[string]any {
	id := strings.TrimSpace(c.ID)
	src := strings.TrimSpace(c.Src)
	if id == "" || src == "" {
		return nil
	}
	out := map[string]any{
		"id":  id,
		"src": src,
	}
	setString(out, "contentType", c.ContentType)
	setString(out, "bus", c.Bus)
	if c.Preload {
		out["preload"] = true
	}
	if c.Loop {
		out["loop"] = true
	}
	setNumericPtr(out, "volume", c.Volume)
	setNumericPtr(out, "rate", c.Rate)
	if len(c.Metadata) > 0 {
		out["metadata"] = cloneSceneAnyMap(c.Metadata)
	}
	return out
}

// Props lowers a to the exact map shape
// window.__gosx.audio.registerManifest expects (gosxAudioRegisterManifest
// in 05-document-env.js). Assign the result to an engine's "audio" prop.
func (a Audio) Props() map[string]any {
	out := map[string]any{}
	if len(a.Buses) > 0 {
		buses := make([]map[string]any, 0, len(a.Buses))
		for _, bus := range a.Buses {
			if p := bus.Props(); p != nil {
				buses = append(buses, p)
			}
		}
		if len(buses) > 0 {
			out["buses"] = buses
		}
	}
	if len(a.Clips) > 0 {
		clips := make([]map[string]any, 0, len(a.Clips))
		for _, clip := range a.Clips {
			if p := clip.Props(); p != nil {
				clips = append(clips, p)
			}
		}
		if len(clips) > 0 {
			out["clips"] = clips
		}
	}
	if a.MasterVolume != nil {
		out["masterVolume"] = *a.MasterVolume
		if a.Muted {
			out["muted"] = true
		}
	}
	return out
}

// SynthWaveform selects an OscillatorNode waveform for a ToneLayer or
// SweepLayer.
type SynthWaveform string

const (
	WaveSine     SynthWaveform = "sine"
	WaveSquare   SynthWaveform = "square"
	WaveSawtooth SynthWaveform = "sawtooth"
	WaveTriangle SynthWaveform = "triangle"
)

// NoiseFilter selects the BiquadFilterNode type a NoiseLayer's generated
// white noise is passed through.
type NoiseFilter string

const (
	FilterLowpass  NoiseFilter = "lowpass"
	FilterHighpass NoiseFilter = "highpass"
	FilterBandpass NoiseFilter = "bandpass"
)

// ADSR shapes a synth layer's gain envelope: linear ramp up over Attack,
// down to a Sustain level over Decay, held, then down to silence over
// Release. Attack/Decay/Release are seconds; Sustain is a level in [0,1]
// relative to the layer's peak gain.
//
// A zero field falls back to a musically-sensible client-side default
// (see arcadeEnvelopeADSR in 30-tail.js) rather than a literal zero — a
// WebAudio gain ramp can't target exactly 0 (it floors at 0.0001), so
// there is no meaningful difference between "unset" and "as fast/low as
// the engine allows" here, unlike Volume/Gain elsewhere in this file.
type ADSR struct {
	Attack  float64
	Decay   float64
	Sustain float64
	Release float64
}

// Props lowers e to the map arcadeEnvelopeADSR expects, or nil for the
// zero value (equivalent to "no envelope" — callers should leave the
// containing layer's Envelope field nil instead of using a zero ADSR).
func (e ADSR) Props() map[string]any {
	if e == (ADSR{}) {
		return nil
	}
	out := map[string]any{}
	setNumeric(out, "attack", e.Attack)
	setNumeric(out, "decay", e.Decay)
	setNumeric(out, "sustain", e.Sustain)
	setNumeric(out, "release", e.Release)
	return out
}

// ToneLayer is one oscillator voice in a SynthPatch: a single sustained
// frequency (see arcadeTone in 30-tail.js).
type ToneLayer struct {
	// Frequency in Hz and Duration in seconds define the tone; set both
	// explicitly.
	Frequency float64
	Duration  float64
	// Waveform selects the oscillator type. Empty defaults to "square"
	// client-side.
	Waveform SynthWaveform
	// Envelope shapes the gain ramp. nil uses the engine's simple
	// built-in attack/release envelope (~6ms attack, ~40ms release tail).
	Envelope *ADSR
	// Gain is this layer's own peak linear gain, independent of the fire-
	// time AudioCue.Intensity it's multiplied against. nil defaults to 1
	// client-side.
	Gain *float64
	// Pan is a per-layer stereo pan in [-1,1] overriding the cue's pan
	// for this layer only. nil inherits the cue's pan.
	Pan *float64
	// DelayMS delays this layer's start relative to the patch's fire
	// time, additive with the firing AudioCue's own delay.
	DelayMS float64
	// Rate multiplies Frequency (0 or unset leaves Frequency unscaled;
	// this mirrors arcadeSoundOptions.rate, which independently scales
	// playback speed for rhythm/pitch effects).
	Rate float64
}

// Props lowers t to the map arcadePlayPatch expects for one tones[] entry.
func (t ToneLayer) Props() map[string]any {
	out := map[string]any{
		"frequency": t.Frequency,
		"duration":  t.Duration,
	}
	setString(out, "waveform", string(t.Waveform))
	if t.Envelope != nil {
		if env := t.Envelope.Props(); env != nil {
			out["envelope"] = env
		}
	}
	setNumericPtr(out, "gain", t.Gain)
	setNumericPtr(out, "pan", t.Pan)
	setNumeric(out, "delayMS", t.DelayMS)
	setNumeric(out, "rate", t.Rate)
	return out
}

// SweepLayer is one oscillator voice in a SynthPatch whose frequency
// glides linearly from StartFrequency to EndFrequency over Duration (see
// arcadeSweep in 30-tail.js).
type SweepLayer struct {
	StartFrequency float64
	EndFrequency   float64
	Duration       float64
	// Waveform selects the oscillator type. Empty defaults to
	// "sawtooth" client-side.
	Waveform SynthWaveform
	Envelope *ADSR
	Gain     *float64
	Pan      *float64
	DelayMS  float64
	Rate     float64
}

// Props lowers s to the map arcadePlayPatch expects for one sweeps[] entry.
func (s SweepLayer) Props() map[string]any {
	out := map[string]any{
		"startFrequency": s.StartFrequency,
		"endFrequency":   s.EndFrequency,
		"duration":       s.Duration,
	}
	setString(out, "waveform", string(s.Waveform))
	if s.Envelope != nil {
		if env := s.Envelope.Props(); env != nil {
			out["envelope"] = env
		}
	}
	setNumericPtr(out, "gain", s.Gain)
	setNumericPtr(out, "pan", s.Pan)
	setNumeric(out, "delayMS", s.DelayMS)
	setNumeric(out, "rate", s.Rate)
	return out
}

// NoiseLayer is one filtered-noise burst in a SynthPatch (see arcadeNoise
// in 30-tail.js): a short buffer of white noise with linear amplitude
// falloff, passed through a BiquadFilterNode.
type NoiseLayer struct {
	Duration float64
	// FilterType selects the filter. Empty defaults to "bandpass"
	// client-side.
	FilterType NoiseFilter
	// FilterFrequency is the filter's center/cutoff frequency in Hz.
	FilterFrequency float64
	Envelope        *ADSR
	Gain            *float64
	Pan             *float64
	DelayMS         float64
}

// Props lowers n to the map arcadePlayPatch expects for one noises[] entry.
func (n NoiseLayer) Props() map[string]any {
	out := map[string]any{
		"duration": n.Duration,
	}
	setString(out, "filterType", string(n.FilterType))
	setNumeric(out, "filterFrequency", n.FilterFrequency)
	if n.Envelope != nil {
		if env := n.Envelope.Props(); env != nil {
			out["envelope"] = env
		}
	}
	setNumericPtr(out, "gain", n.Gain)
	setNumericPtr(out, "pan", n.Pan)
	setNumeric(out, "delayMS", n.DelayMS)
	return out
}

// SynthPatch is a named, code-generated arcadeAudio sound: a combination
// of ToneLayer, SweepLayer, and NoiseLayer voices fired together. It is
// the typed equivalent of the ~15 hard-coded cues playArcadeSFX already
// implements (see AudioCue.Cue) — use SynthPatch to define new cues from
// Go without editing the client bundle, via AudioCue.Patch.
type SynthPatch struct {
	// Name is an optional label for debugging/logging; it has no effect
	// on playback.
	Name   string
	Tones  []ToneLayer
	Sweeps []SweepLayer
	Noises []NoiseLayer
}

// Props lowers p to the map arcadeAudio.playPatch expects.
func (p SynthPatch) Props() map[string]any {
	out := map[string]any{}
	setString(out, "name", p.Name)
	if len(p.Tones) > 0 {
		tones := make([]map[string]any, 0, len(p.Tones))
		for _, tone := range p.Tones {
			tones = append(tones, tone.Props())
		}
		out["tones"] = tones
	}
	if len(p.Sweeps) > 0 {
		sweeps := make([]map[string]any, 0, len(p.Sweeps))
		for _, sweep := range p.Sweeps {
			sweeps = append(sweeps, sweep.Props())
		}
		out["sweeps"] = sweeps
	}
	if len(p.Noises) > 0 {
		noises := make([]map[string]any, 0, len(p.Noises))
		for _, noise := range p.Noises {
			noises = append(noises, noise.Props())
		}
		out["noises"] = noises
	}
	return out
}

// AudioCue is the live-fire audio message: a single "play this now" event
// a server-driven scene sends per tick or on demand, as distinct from
// Audio's one-time manifest declaration.
//
// Two delivery paths consume this same lowered shape:
//
//  1. Embedded tick field (pre-existing, zero client changes): assign
//     AudioCue.Props() under the "audio" key of whatever payload a hub
//     broadcasts as e.g. its "tick" event. The engine's built-in hub
//     input controller (createHubInputController's onHubMessage, in
//     client/js/bootstrap-src/30-tail.js) already reads message.data.audio
//     and fires playArcadeSFX(cue.cue || <inferred from event.kind>,
//     {intensity, pan, depth}) plus phase/state cues (round/fight/ko/
//     surge/...) inferred from other tick fields. Seq/Cue/PhaseCue/
//     Intensity/Pan/Depth/Bus match that path's existing hand-rolled
//     payload field-for-field (see e.g. a fighting-game demo's
//     AudioPayload type) — this is the "zero JS changes" path.
//
//  2. Dedicated hub event "audio" (new, generic — added by this change):
//     hub.Broadcast("audio", cue.Props()) delivers the cue to any mounted
//     Scene3D engine directly, independent of the fight-shaped hub input
//     controller above. client/js/bootstrap-src/20-scene-mount.js's
//     sceneHubListener special-cases the "audio" event name (mirroring
//     its pre-existing "motion" special-case) and routes the payload:
//     Clip set -> gosxAudio sample playback (window.__gosx.audio.play),
//     honoring Position for 3D spatialization; otherwise Patch or Cue ->
//     arcadeAudio synth playback (window.__gosx.arcadeAudio.playPatch /
//     .play) — a trigger seam this change adds, since arcadeAudio
//     previously had no path to fire from outside the hub input
//     controller's own hard-coded tick parsing.
type AudioCue struct {
	// Seq is a monotonically increasing per-source counter. Both delivery
	// paths use it to de-duplicate a cue that arrives more than once
	// (e.g. redelivered on the same tick); 0 disables de-duplication.
	Seq uint32
	// Cue names an arcadeAudio synth cue: either one of the built-in
	// names playArcadeSFX already implements (move, confirm, round,
	// fight, ko, match, hit_light, hit_heavy, counter, punish, launcher,
	// block, guard, just_guard, guard_cancel, armor, throw, throw_tech,
	// surge, surge_ready) or, for the dedicated hub-event path, any name
	// meaningful to the receiving scene once Patch is also handled.
	Cue string
	// PhaseCue names a coarser phase-level cue (round/fight/ko/match/...)
	// applied at most once per distinct value by the embedded-tick-field
	// path's de-duplication. Unused by the dedicated hub-event path.
	PhaseCue string
	// Intensity scales synth volume, roughly in [0.05, 1.35] after
	// client-side clamping (arcadeSoundOptions). There is no "unset"
	// sentinel here — 0 clamps to the engine's floor rather than a
	// default — matching the pre-existing embedded-tick-field schema
	// this field mirrors.
	Intensity float64
	// Pan is stereo pan in [-1, 1] (negative = left), clamped client-
	// side.
	Pan float64
	// Depth is a synthetic front/back cue in [-0.75, 0.75] arcadeAudio
	// uses to bias playback (no direct WebAudio parameter; purely a
	// gameplay-computed hint), clamped client-side.
	Depth float64
	// Bus optionally names an AudioBus for Clip playback (gosxAudio).
	// Ignored by arcadeAudio, which has no bus routing.
	Bus string

	// Clip optionally names an AudioClip ID (or a bare URL, resolved the
	// same way window.__gosx.audio.play does) to play via gosxAudio
	// instead of firing an arcadeAudio synth cue. When set, Clip takes
	// priority over Cue/Patch.
	Clip string
	// Position optionally places Clip at a 3D point for gosxAudio's
	// PannerNode spatialization. Ignored when Clip is empty, or by
	// arcadeAudio synth playback.
	Position *Vector3
	// Patch optionally supplies an inline SynthPatch to play via
	// arcadeAudio instead of a built-in named Cue. Only meaningful on the
	// dedicated hub-event delivery path (see type doc); ignored by the
	// embedded-tick-field path's existing client parsing. Ignored when
	// Clip is set.
	Patch *SynthPatch
	// Volume is Clip's per-play linear gain in [0,1]. nil defaults to 1
	// client-side. Ignored by arcadeAudio synth playback (use Intensity).
	Volume *float64
	// Rate overrides playback rate (1 = normal). For Clip playback, 0
	// leaves the clip's own default rate; for arcadeAudio playback, 0
	// leaves oscillator frequencies unscaled.
	Rate float64
	// Loop repeats Clip playback until stopped. Ignored by arcadeAudio.
	Loop bool
	// Handle optionally names this playback instance so a later call can
	// stop it by ID via window.__gosx.audio.stop(handle). Ignored by
	// arcadeAudio, which has no per-instance stop.
	Handle string
}

// Props lowers c to the map both delivery paths described in AudioCue's
// doc consume. Seq/Cue/PhaseCue/Intensity/Pan/Depth are always present
// (even when zero) to match the pre-existing embedded-tick-field schema
// byte-for-byte; the remaining fields are included only when set.
func (c AudioCue) Props() map[string]any {
	out := map[string]any{
		"seq":       c.Seq,
		"cue":       c.Cue,
		"phaseCue":  c.PhaseCue,
		"intensity": c.Intensity,
		"pan":       c.Pan,
		"depth":     c.Depth,
	}
	setString(out, "bus", c.Bus)
	setString(out, "clip", c.Clip)
	if c.Position != nil {
		out["position"] = map[string]any{
			"x": c.Position.X,
			"y": c.Position.Y,
			"z": c.Position.Z,
		}
	}
	if c.Patch != nil {
		out["patch"] = c.Patch.Props()
	}
	setNumericPtr(out, "volume", c.Volume)
	setNumeric(out, "rate", c.Rate)
	if c.Loop {
		out["loop"] = true
	}
	setString(out, "handle", c.Handle)
	return out
}
