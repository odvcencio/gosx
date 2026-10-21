//go:build js && wasm

package host

import (
	"sync"
	"syscall/js"
)

// FrameSourceJS wraps window.requestAnimationFrame and reports
// tab visibility from document.hidden.
//
// In a GoSX engine worker (engine.Config Thread: worker) there is no
// document, and requestAnimationFrame, where it exists, follows the page's
// display. FrameSourceJS then uses the worker's fixed-rate timer clock
// (__gosx.worker.requestFrame, 60 Hz by default) and the page visibility the
// page forwards (__gosx.worker.hidden). Callers change nothing.
type FrameSourceJS struct {
	mu      sync.Mutex
	pending map[int]js.Func
}

// NewFrameSourceJS creates a FrameSourceJS.
func NewFrameSourceJS() *FrameSourceJS {
	return &FrameSourceJS{pending: make(map[int]js.Func)}
}

// RequestFrame implements FrameSource.
func (s *FrameSourceJS) RequestFrame(cb func(timestampMS float64)) int {
	var handle int
	var fn js.Func
	fn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		timestamp := 0.0
		if len(args) > 0 {
			timestamp = args[0].Float()
		}
		s.mu.Lock()
		delete(s.pending, handle)
		s.mu.Unlock()
		fn.Release()
		cb(timestamp)
		return nil
	})
	if bridge := workerClock(); bridge.Truthy() {
		handle = bridge.Call("requestFrame", fn).Int()
	} else {
		handle = js.Global().Call("requestAnimationFrame", fn).Int()
	}
	s.mu.Lock()
	s.pending[handle] = fn
	s.mu.Unlock()
	return handle
}

// CancelFrame implements FrameSource.
func (s *FrameSourceJS) CancelFrame(handle int) {
	if bridge := workerClock(); bridge.Truthy() {
		bridge.Call("cancelFrame", handle)
	} else {
		js.Global().Call("cancelAnimationFrame", handle)
	}
	s.mu.Lock()
	fn, ok := s.pending[handle]
	delete(s.pending, handle)
	s.mu.Unlock()
	if ok {
		fn.Release()
	}
}

// Hidden implements FrameSource, reading document.hidden, or the forwarded
// page visibility inside an engine worker.
func (s *FrameSourceJS) Hidden() bool {
	if bridge := workerClock(); bridge.Truthy() {
		return bridge.Get("hidden").Truthy()
	}
	doc := js.Global().Get("document")
	if doc.IsUndefined() || doc.IsNull() {
		return false
	}
	hidden := doc.Get("hidden")
	if hidden.Type() != js.TypeBoolean {
		return false
	}
	return hidden.Bool()
}

// workerClock returns the engine worker's clock bridge, or an undefined value
// on the main thread.
func workerClock() js.Value {
	root := js.Global().Get("__gosx")
	if !root.Truthy() {
		return js.Undefined()
	}
	bridge := root.Get("worker")
	if bridge.Truthy() && bridge.Get("requestFrame").Type() == js.TypeFunction {
		return bridge
	}
	return js.Undefined()
}
