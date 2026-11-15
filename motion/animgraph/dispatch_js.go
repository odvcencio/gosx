//go:build js && wasm

package animgraph

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"syscall/js"
)

type dispatcher struct {
	encoder FrameEncoder
	defined map[uint32]bool
	buf     js.Value
	bufCap  int
	view    js.Value
	viewLen int
	fb      js.Value
	fbBytes js.Value
	fbGo    []byte
	vals    []float64
}

var (
	dispatchMu sync.Mutex
	dispatch   dispatcher
)

const feedbackFloats = 3 + 9*MaxIK + 6*MaxLookAt + 4

// Dispatch sends the instances' current poses to the mounted Scene3D engine
// named target (its engine ID or mount element ID), then reads each model's
// Feedback. Call it once per frame after Step. The first call per Program
// sends its rig description. It returns ErrNotReady while the runtime loads.
func Dispatch(target string, models ...*Instance) error {
	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	d := &dispatch
	root := js.Global().Get("__gosx")
	if root.IsUndefined() || root.IsNull() {
		return ErrNotReady
	}
	api := root.Get("scene3d")
	if api.IsUndefined() || api.IsNull() || api.Get("dispatchAnimationFrame").Type() != js.TypeFunction {
		return ErrNotReady
	}
	if d.defined == nil {
		d.defined = map[uint32]bool{}
	}
	for _, in := range models {
		if in == nil || d.defined[in.prog.rigHash] {
			continue
		}
		api.Call("defineAnimationRig", in.prog.rigHash, string(in.prog.rigJSON))
		d.defined[in.prog.rigHash] = true
	}
	data, err := d.encoder.Encode(models...)
	if err != nil {
		return err
	}
	if d.bufCap < len(data) {
		d.bufCap = 2 * len(data)
		d.buf = js.Global().Get("Uint8Array").New(d.bufCap)
		d.viewLen = -1
	}
	// Frames usually keep their size, so the exact-length view is reused.
	if d.viewLen != len(data) {
		d.view = d.buf.Call("subarray", 0, len(data))
		d.viewLen = len(data)
	}
	view := d.view
	js.CopyBytesToJS(view, data)
	result := api.Call("dispatchAnimationFrame", target, view)
	if result.Type() == js.TypeObject && !result.Get("applied").Truthy() {
		reason := result.Get("reason")
		if reason.Type() == js.TypeString && reason.String() != "not-ready" {
			return errors.New("animgraph: frame rejected: " + reason.String())
		}
		return ErrNotReady
	}
	if d.fb.IsUndefined() || d.fb.IsNull() {
		d.fb = js.Global().Get("Float64Array").New(feedbackFloats)
		d.fbBytes = js.Global().Get("Uint8Array").New(d.fb.Get("buffer"))
		d.fbGo = make([]byte, feedbackFloats*8)
		d.vals = make([]float64, feedbackFloats)
	}
	for _, in := range models {
		n := api.Call("animationFeedback", target, in.id, d.fb).Int()
		if n <= 0 || n > feedbackFloats {
			continue
		}
		js.CopyBytesToGo(d.fbGo, d.fbBytes)
		for i := 0; i < n; i++ {
			d.vals[i] = math.Float64frombits(binary.LittleEndian.Uint64(d.fbGo[i*8:]))
		}
		if in.feedback.DecodeFeedback(d.vals[:n]) && in.feedback.RigHash != in.prog.rigHash {
			d.defined[in.prog.rigHash] = false
		}
	}
	return nil
}
