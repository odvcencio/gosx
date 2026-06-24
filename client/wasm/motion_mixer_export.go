//go:build js && wasm

package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"syscall/js"

	"m31labs.dev/gosx/motion"
)

// mixerSlot bundles a live *motion.Mixer with its reusable per-mixer byte
// scratch. The Mixer pools its blend accumulators and float writes internally;
// here we keep only the byte scratch holding the LE float64 encoding for the
// single js.CopyBytesToJS boundary copy, so the update hot path stays
// allocation-free after warmup.
type mixerSlot struct {
	mixer       *motion.Mixer
	byteScratch []byte
}

// mixerTable is the handle table mapping integer handles (>=1) to live mixers.
// Handles are never reused within a session (nextMixerHandle only grows), so a
// stale handle from a destroyed mixer reliably misses.
var (
	mixerTable      = make(map[int]*mixerSlot)
	nextMixerHandle = 1
)

// mixerWriteBuf is a shared WriteBuf reused across every mixer Update; the slot
// holds the float/byte scratch but the Mixer needs a *WriteBuf target. Mixer
// owns no external state on the WriteBuf between calls (it Resets at the top of
// Update), so one shared buffer is safe for the single-threaded WASM runtime.
var mixerWriteBuf = motion.NewWriteBuf(64)

// clipJSONChannel mirrors one glTF animation channel as delivered from JS. Field
// names match the JSON keys the model loader emits.
type clipJSONChannel struct {
	Node          int       `json:"node"`
	Property      string    `json:"property"`
	Interpolation string    `json:"interpolation"`
	Times         []float64 `json:"times"`
	Values        []float64 `json:"values"`
}

// clipJSONDoc is the {duration, channels} object passed to add_clip.
type clipJSONDoc struct {
	Duration float64           `json:"duration"`
	Channels []clipJSONChannel `json:"channels"`
}

// registerMotionMixerExports installs the mixer lifecycle functions on the JS
// global. Called from registerMotionExports alongside the load/tick exports.
func registerMotionMixerExports() {
	setRuntimeFunc("__gosx_motion_mixer_create", js.FuncOf(func(this js.Value, args []js.Value) any {
		return mixerCreate(args)
	}))
	setRuntimeFunc("__gosx_motion_mixer_add_clip", js.FuncOf(func(this js.Value, args []js.Value) any {
		return mixerAddClip(args)
	}))
	setRuntimeFunc("__gosx_motion_mixer_play", js.FuncOf(func(this js.Value, args []js.Value) any {
		mixerPlay(args)
		return js.Undefined()
	}))
	setRuntimeFunc("__gosx_motion_mixer_stop", js.FuncOf(func(this js.Value, args []js.Value) any {
		mixerStop(args)
		return js.Undefined()
	}))
	setRuntimeFunc("__gosx_motion_mixer_update", js.FuncOf(func(this js.Value, args []js.Value) any {
		return mixerUpdate(args)
	}))
	setRuntimeFunc("__gosx_motion_mixer_is_playing", js.FuncOf(func(this js.Value, args []js.Value) any {
		return mixerIsPlaying(args)
	}))
	setRuntimeFunc("__gosx_motion_mixer_destroy", js.FuncOf(func(this js.Value, args []js.Value) any {
		mixerDestroy(args)
		return js.Undefined()
	}))
}

// mixerCreate(): allocates a new Mixer and returns its handle (>=1). args is
// unused (accepted for the js.FuncOf signature; tests pass nil).
func mixerCreate(args []js.Value) int {
	h := nextMixerHandle
	nextMixerHandle++
	mixerTable[h] = &mixerSlot{mixer: motion.NewMixer()}
	return h
}

// mixerAddClip(mixerHandle, name string, clipJSON string) → ok bool.
//
// Parses clipJSON ({duration, channels:[…]}) into motion.ClipChannel records,
// builds an evaluable Timeline via motion.BuildClipTimeline, and registers it on
// the mixer under name. The authored duration is honored when positive; the
// builder's keyframe-derived duration is the fallback. Returns false on a
// missing/unknown handle, malformed JSON, or a clip that yields no channels —
// never a panic.
func mixerAddClip(args []js.Value) bool {
	if len(args) < 3 {
		return false
	}
	slot, ok := mixerTable[args[0].Int()]
	if !ok {
		return false
	}
	name := args[1].String()
	raw := args[2].String()

	var doc clipJSONDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return false
	}

	channels := make([]motion.ClipChannel, 0, len(doc.Channels))
	for _, c := range doc.Channels {
		channels = append(channels, motion.ClipChannel{
			Node:     c.Node,
			Property: c.Property,
			Interp:   c.Interpolation,
			Times:    c.Times,
			Values:   c.Values,
		})
	}

	tl, keyDur := motion.BuildClipTimeline(channels)

	// A timeline with zero surviving tracks is a no-op clip — reject so callers
	// can surface a load error rather than silently registering an empty clip.
	if len(tl.Children) == 0 {
		return false
	}

	duration := doc.Duration
	if duration <= 0 {
		duration = keyDur
	}

	slot.mixer.AddClip(name, tl, duration)
	return true
}

// mixerPlay(mixerHandle, name, fadeIn, loop bool, speed, weight). Unknown
// handle is a no-op.
func mixerPlay(args []js.Value) {
	if len(args) < 6 {
		return
	}
	slot, ok := mixerTable[args[0].Int()]
	if !ok {
		return
	}
	slot.mixer.Play(args[1].String(), motion.PlayOptions{
		FadeIn: args[2].Float(),
		Loop:   args[3].Bool(),
		Speed:  args[4].Float(),
		Weight: args[5].Float(),
	})
}

// mixerStop(mixerHandle, name, fadeOut). Unknown handle is a no-op.
func mixerStop(args []js.Value) {
	if len(args) < 3 {
		return
	}
	slot, ok := mixerTable[args[0].Int()]
	if !ok {
		return
	}
	slot.mixer.Stop(args[1].String(), motion.StopOptions{FadeOut: args[2].Float()})
}

// mixerUpdate(mixerHandle, dt, reduced bool, out) → full float count.
//
// Advances and blends the mixer by dt, encoding the blended packed writes as
// little-endian float64 into the JS Uint8Array out. Returns the FULL float
// count produced (grow-and-retick contract): when the count exceeds out's
// capacity only the floats that fit are written and the caller must grow out and
// re-update. Returns 0 on an unknown handle or missing args (no panic).
func mixerUpdate(args []js.Value) int {
	if len(args) < 4 {
		return 0
	}
	slot, ok := mixerTable[args[0].Int()]
	if !ok {
		return 0
	}
	dt := args[1].Float()
	reduced := args[2].Bool()
	out := args[3]
	if out.Type() != js.TypeObject {
		return 0
	}

	mixerWriteBuf.Reset()
	slot.mixer.Update(dt, motion.Policy{ReducedMotion: reduced}, mixerWriteBuf)
	writes := mixerWriteBuf.Writes()
	n := len(writes)

	// Capacity in float64s the JS buffer can hold.
	capFloats := out.Get("length").Int() / 8
	copied := n
	if copied > capFloats {
		copied = capFloats
	}

	// Encode the floats that fit as little-endian float64 into the reused byte
	// scratch, then copy across the JS boundary once.
	need := copied * 8
	if cap(slot.byteScratch) < need {
		slot.byteScratch = make([]byte, need)
	} else {
		slot.byteScratch = slot.byteScratch[:need]
	}
	for i := 0; i < copied; i++ {
		binary.LittleEndian.PutUint64(slot.byteScratch[i*8:], math.Float64bits(writes[i]))
	}
	if need > 0 {
		js.CopyBytesToJS(out, slot.byteScratch[:need])
	}

	// Return the FULL count so JS knows whether to grow and re-update.
	return n
}

// mixerIsPlaying(mixerHandle, name) → bool. Unknown handle → false.
func mixerIsPlaying(args []js.Value) bool {
	if len(args) < 2 {
		return false
	}
	slot, ok := mixerTable[args[0].Int()]
	if !ok {
		return false
	}
	return slot.mixer.IsPlaying(args[1].String())
}

// mixerDestroy(mixerHandle). Frees the handle (safe on unknown handle, no panic).
func mixerDestroy(args []js.Value) {
	if len(args) < 1 {
		return
	}
	delete(mixerTable, args[0].Int())
}
