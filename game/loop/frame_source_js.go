//go:build js && wasm

package loop

import (
	"sync"
	"syscall/js"
)

// FrameSourceJS drives Driver from window.requestAnimationFrame and reports
// tab visibility from document.hidden.
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
	handle = js.Global().Call("requestAnimationFrame", fn).Int()
	s.mu.Lock()
	s.pending[handle] = fn
	s.mu.Unlock()
	return handle
}

// CancelFrame implements FrameSource.
func (s *FrameSourceJS) CancelFrame(handle int) {
	js.Global().Call("cancelAnimationFrame", handle)
	s.mu.Lock()
	fn, ok := s.pending[handle]
	delete(s.pending, handle)
	s.mu.Unlock()
	if ok {
		fn.Release()
	}
}

// Hidden implements FrameSource, reading document.hidden.
func (s *FrameSourceJS) Hidden() bool {
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
