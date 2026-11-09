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
//
// A FrameSourceJS creates one js.Func for its whole life and keeps at most one
// browser frame request outstanding: every RequestFrame made before a frame
// fires runs in that frame, in request order. Steady-state frames therefore
// allocate no callbacks. Call Release when the source is no longer used.
type FrameSourceJS struct {
	mu       sync.Mutex
	queue    frameQueue
	fn       js.Func
	hasFn    bool
	resolved bool
	worker   js.Value // __gosx.worker clock bridge, or undefined
	document js.Value // document, or undefined
	browser  int      // outstanding browser request id, 0 if none
}

// NewFrameSourceJS creates a FrameSourceJS.
func NewFrameSourceJS() *FrameSourceJS { return &FrameSourceJS{} }

// resolve looks up the worker bridge and document once. The bridge is
// installed before the engine's Go code runs, so it cannot appear later.
func (s *FrameSourceJS) resolve() {
	if s.resolved {
		return
	}
	s.resolved = true
	s.worker = workerClock()
	s.document = js.Global().Get("document")
}

// RequestFrame implements FrameSource.
func (s *FrameSourceJS) RequestFrame(cb func(timestampMS float64)) int {
	s.mu.Lock()
	s.resolve()
	handle := s.queue.add(cb)
	schedule := s.browser == 0
	if schedule && !s.hasFn {
		s.fn = js.FuncOf(s.onFrame)
		s.hasFn = true
	}
	fn, worker := s.fn, s.worker
	s.mu.Unlock()
	if !schedule {
		return handle
	}
	var id int
	if worker.Truthy() {
		id = worker.Call("requestFrame", fn).Int()
	} else {
		id = js.Global().Call("requestAnimationFrame", fn).Int()
	}
	s.mu.Lock()
	if s.queue.empty() {
		// Cancelled while the request was being made.
		s.mu.Unlock()
		s.cancelBrowser(id)
		return handle
	}
	s.browser = id
	s.mu.Unlock()
	return handle
}

// CancelFrame implements FrameSource.
func (s *FrameSourceJS) CancelFrame(handle int) {
	s.mu.Lock()
	s.queue.cancel(handle)
	id := 0
	if s.queue.empty() && s.browser != 0 {
		id, s.browser = s.browser, 0
	}
	s.mu.Unlock()
	if id != 0 {
		s.cancelBrowser(id)
	}
}

func (s *FrameSourceJS) cancelBrowser(id int) {
	if s.worker.Truthy() {
		s.worker.Call("cancelFrame", id)
	} else {
		js.Global().Call("cancelAnimationFrame", id)
	}
}

func (s *FrameSourceJS) onFrame(_ js.Value, args []js.Value) any {
	timestamp := 0.0
	if len(args) > 0 {
		timestamp = args[0].Float()
	}
	s.mu.Lock()
	s.browser = 0
	s.queue.begin()
	s.mu.Unlock()
	for {
		s.mu.Lock()
		cb, ok := s.queue.next()
		s.mu.Unlock()
		if !ok {
			return nil
		}
		cb(timestamp)
	}
}

// Hidden implements FrameSource, reading document.hidden, or the forwarded
// page visibility inside an engine worker.
func (s *FrameSourceJS) Hidden() bool {
	s.mu.Lock()
	s.resolve()
	worker, doc := s.worker, s.document
	s.mu.Unlock()
	if worker.Truthy() {
		return worker.Get("hidden").Truthy()
	}
	if doc.IsUndefined() || doc.IsNull() {
		return false
	}
	hidden := doc.Get("hidden")
	if hidden.Type() != js.TypeBoolean {
		return false
	}
	return hidden.Bool()
}

// Release cancels any pending frames and releases the js.Func. The source can
// be used again afterwards; it then creates a new js.Func.
func (s *FrameSourceJS) Release() {
	s.mu.Lock()
	s.queue.clear()
	id := s.browser
	s.browser = 0
	fn, hasFn := s.fn, s.hasFn
	s.fn, s.hasFn = js.Func{}, false
	s.mu.Unlock()
	if id != 0 {
		s.cancelBrowser(id)
	}
	if hasFn {
		fn.Release()
	}
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
