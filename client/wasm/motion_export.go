//go:build js && wasm

package main

import (
	"encoding/binary"
	"math"
	"syscall/js"

	"m31labs.dev/gosx/motion"
)

// motionRT is the single host-side motion runtime wrapped by the WASM exports.
// All loaded programs share this runtime, keyed by integer handles (>=1).
var motionRT = motion.NewRuntime()

// Reusable per-frame scratches. Grown on demand and reused across ticks to keep
// the hot path allocation-free on the Go side (the only unavoidable cost is the
// js.CopyBytesToJS boundary copy). motionFloatScratch receives the packed writes
// from Runtime.Tick; motionByteScratch holds their little-endian float64 encoding.
var (
	motionFloatScratch []float64
	motionByteScratch  []byte
)

// registerMotionExports installs the motion load/tick/refs/unload functions on
// the JS global. Called from the same registration path as the engine exports
// (registerRuntime).
func registerMotionExports() {
	setRuntimeFunc("__gosx_motion_load", motionLoadFunc())
	setRuntimeFunc("__gosx_motion_tick", motionTickFunc())
	setRuntimeFunc("__gosx_motion_refs", motionRefsFunc())
	setRuntimeFunc("__gosx_motion_unload", motionUnloadFunc())
	registerMotionMixerExports()
}

// motionLoadFunc returns the __gosx_motion_load(uint8Array) export.
//
// args[0] is a JS Uint8Array of wire-format program bytes. Returns the loaded
// program handle (>=1) on success, or -1 on any error (missing arg, decode
// failure).
func motionLoadFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		return motionLoad(args)
	})
}

// motionLoad is the load handler, split out so tests can invoke it directly with
// a faked []js.Value.
func motionLoad(args []js.Value) int {
	if len(args) < 1 {
		return -1
	}
	src := args[0]
	if src.Type() != js.TypeObject {
		return -1
	}
	n := src.Get("length").Int()
	b := make([]byte, n)
	js.CopyBytesToGo(b, src)
	h, err := motionRT.Load(b)
	if err != nil {
		return -1
	}
	return h
}

// motionTickFunc returns the __gosx_motion_tick(handle, t, reduced, out) export.
//
// out is a JS Uint8Array viewing a Float64Array's buffer; its byte length is
// floatCapacity*8. The handler evaluates program `handle` at time `t` (with the
// `reduced` motion policy), encodes the produced writes as little-endian float64
// into `out`, and returns the FULL float count produced. If that count exceeds
// out's capacity, only the values that fit are written and the caller must grow
// `out` and re-tick. Returns 0 on error (unknown handle, missing args).
func motionTickFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		return motionTick(args)
	})
}

// motionTick is the tick handler, split out so tests can invoke it directly.
func motionTick(args []js.Value) int {
	if len(args) < 4 {
		return 0
	}
	handle := args[0].Int()
	t := args[1].Float()
	reduced := args[2].Bool()
	out := args[3]
	if out.Type() != js.TypeObject {
		return 0
	}

	// Capacity in float64s the JS buffer can hold.
	capFloats := out.Get("length").Int() / 8

	// Grow the float scratch so Runtime.Tick can report the full count even when
	// the JS buffer is too small (it fills only what fits, but n is the true count).
	if cap(motionFloatScratch) < capFloats {
		motionFloatScratch = make([]float64, capFloats)
	} else {
		motionFloatScratch = motionFloatScratch[:capFloats]
	}

	n, err := motionRT.Tick(handle, t, reduced, motionFloatScratch)
	if err != nil {
		return 0
	}

	// How many floats we can actually deliver (bounded by both the scratch we
	// filled and the JS buffer capacity).
	copied := n
	if copied > len(motionFloatScratch) {
		copied = len(motionFloatScratch)
	}
	if copied > capFloats {
		copied = capFloats
	}

	// Encode copied floats as little-endian float64 into the reused byte scratch.
	need := copied * 8
	if cap(motionByteScratch) < need {
		motionByteScratch = make([]byte, need)
	} else {
		motionByteScratch = motionByteScratch[:need]
	}
	for i := 0; i < copied; i++ {
		binary.LittleEndian.PutUint64(motionByteScratch[i*8:], math.Float64bits(motionFloatScratch[i]))
	}

	if need > 0 {
		js.CopyBytesToJS(out, motionByteScratch[:need])
	}

	// Return the FULL count so JS knows whether to grow and re-tick.
	return n
}

// motionRefsFunc returns the __gosx_motion_refs(handle) export.
//
// Returns a JS object {target: [...strings], prop: [...strings]} where each
// array index maps a numeric id to its string ref (target object-id or property
// name). Returns js.Null() when the handle is unknown.
func motionRefsFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		return motionRefs(args)
	})
}

// motionRefs is the refs handler, split out so tests can invoke it directly
// with a faked []js.Value.
func motionRefs(args []js.Value) js.Value {
	if len(args) < 1 {
		return js.Null()
	}
	handle := args[0].Int()

	targets, ok := motionRT.TargetRefs(handle)
	if !ok {
		return js.Null()
	}
	props, ok := motionRT.PropRefs(handle)
	if !ok {
		return js.Null()
	}

	// Build JS arrays from the Go slices.
	targetArr := js.Global().Get("Array").New(len(targets))
	for i, s := range targets {
		targetArr.SetIndex(i, js.ValueOf(s))
	}
	propArr := js.Global().Get("Array").New(len(props))
	for i, s := range props {
		propArr.SetIndex(i, js.ValueOf(s))
	}

	obj := js.Global().Get("Object").New()
	obj.Set("target", targetArr)
	obj.Set("prop", propArr)
	return obj
}

// motionUnloadFunc returns the __gosx_motion_unload(handle) export.
//
// Frees the handle from the runtime. Safe on unknown handle (no panic).
// Returns js.Undefined().
func motionUnloadFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		motionUnload(args)
		return js.Undefined()
	})
}

// motionUnload is the unload handler, split out so tests can invoke it directly
// with a faked []js.Value.
func motionUnload(args []js.Value) {
	if len(args) < 1 {
		return
	}
	handle := args[0].Int()
	motionRT.Unload(handle)
}
