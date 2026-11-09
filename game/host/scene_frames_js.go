//go:build js && wasm

package host

import (
	"sync"
	"syscall/js"
)

// SceneFrameSource is a FrameSource whose frames come from a Scene3D
// renderer: the renderer's own requestAnimationFrame runs the pending
// callbacks before it builds and draws the frame, so poses a callback writes
// through scene.StreamWriter appear in that same frame, and the page has one
// animation-frame loop instead of two. While a callback is pending the renderer
// keeps drawing every frame, at the pacing it applies to its own animation.
//
// Until the renderer at target is mounted, after it is disposed, and in an
// engine worker (where the renderer is on the page, out of reach of a
// synchronous call), SceneFrameSource falls back to a FrameSourceJS, so a game
// steps from the first frame either way.
//
// SceneFrameSource implements FrameAlphaSink: game/loop.Driver hands it each
// frame's interpolation alpha, and the renderer receives it as the frame
// callback's result (bundle.simAlpha, and simAlpha in the mount's telemetry).
type SceneFrameSource struct {
	mu       sync.Mutex
	target   string
	queue    frameQueue
	hook     js.Func
	hasHook  bool
	handle   js.Value // the mount handle the hook is registered on
	attached bool
	unhook   js.Value // the function setFrameHook returned
	alpha    float64
	firing   bool

	fallback        *FrameSourceJS
	fallbackPending int // handle of the fallback request, 0 if none
	fallbackFire    func(float64)
}

// NewSceneFrameSource creates a FrameSource driven by the Scene3D mounted on
// the element whose id is target.
func NewSceneFrameSource(target string) *SceneFrameSource {
	s := &SceneFrameSource{target: target, fallback: NewFrameSourceJS()}
	s.fallbackFire = s.onFallback
	return s
}

// RequestFrame implements FrameSource.
func (s *SceneFrameSource) RequestFrame(cb func(timestampMS float64)) int {
	s.mu.Lock()
	handle := s.queue.add(cb)
	if s.attached || s.firing {
		s.mu.Unlock()
		return handle
	}
	if s.tryAttachLocked() {
		s.mu.Unlock()
		return handle
	}
	needFallback := s.fallbackPending == 0
	s.mu.Unlock()
	if needFallback {
		pending := s.fallback.RequestFrame(s.fallbackFire)
		s.mu.Lock()
		s.fallbackPending = pending
		s.mu.Unlock()
	}
	return handle
}

// CancelFrame implements FrameSource. When nothing waits any more, the source
// unregisters from the renderer, so the renderer can stop drawing.
func (s *SceneFrameSource) CancelFrame(handle int) {
	s.mu.Lock()
	s.queue.cancel(handle)
	idle := s.queue.empty() && !s.firing
	fallback := 0
	if idle {
		fallback, s.fallbackPending = s.fallbackPending, 0
		s.detachLocked()
	}
	s.mu.Unlock()
	if fallback != 0 {
		s.fallback.CancelFrame(fallback)
	}
}

// Hidden implements FrameSource with document.hidden, or the forwarded page
// visibility inside an engine worker.
func (s *SceneFrameSource) Hidden() bool { return s.fallback.Hidden() }

// SetFrameAlpha implements FrameAlphaSink.
func (s *SceneFrameSource) SetFrameAlpha(alpha float64) {
	s.mu.Lock()
	s.alpha = alpha
	s.mu.Unlock()
}

// Attached reports whether the renderer's frame loop drives the source; false
// means it is on the fallback clock.
func (s *SceneFrameSource) Attached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached
}

// Release unregisters from the renderer, drops pending callbacks and releases
// the js.Func values.
func (s *SceneFrameSource) Release() {
	s.mu.Lock()
	s.queue.clear()
	s.detachLocked()
	hook, hasHook := s.hook, s.hasHook
	s.hook, s.hasHook = js.Func{}, false
	s.fallbackPending = 0
	s.mu.Unlock()
	s.fallback.Release()
	if hasHook {
		hook.Release()
	}
}

// tryAttachLocked registers the frame hook on the renderer's mount handle.
func (s *SceneFrameSource) tryAttachLocked() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if workerClock().Truthy() {
		return false
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return false
	}
	mount := doc.Call("getElementById", s.target)
	if !mount.Truthy() {
		return false
	}
	handle := mount.Get("__gosxScene3DHandle")
	if !handle.Truthy() || handle.Get("setFrameHook").Type() != js.TypeFunction {
		return false
	}
	if !s.hasHook {
		s.hook = js.FuncOf(s.onHook)
		s.hasHook = true
	}
	s.unhook = handle.Call("setFrameHook", s.hook)
	s.handle = handle
	s.attached = true
	return true
}

func (s *SceneFrameSource) detachLocked() {
	if !s.attached {
		return
	}
	s.attached = false
	unhook := s.unhook
	s.unhook, s.handle = js.Undefined(), js.Undefined()
	if unhook.Type() == js.TypeFunction {
		func() {
			defer func() { _ = recover() }()
			unhook.Invoke()
		}()
	}
}

// onHook is the renderer's frame callback: hook(now, deltaMS, detached). It
// runs the pending callbacks and returns the alpha the last of them reported.
func (s *SceneFrameSource) onHook(_ js.Value, args []js.Value) any {
	if len(args) > 2 && args[2].Truthy() {
		// The mount was disposed: move the pending callbacks to the fallback.
		s.mu.Lock()
		s.attached = false
		s.unhook, s.handle = js.Undefined(), js.Undefined()
		waiting := !s.queue.empty()
		need := waiting && s.fallbackPending == 0
		s.mu.Unlock()
		if need {
			pending := s.fallback.RequestFrame(s.fallbackFire)
			s.mu.Lock()
			s.fallbackPending = pending
			s.mu.Unlock()
		}
		return nil
	}
	now := 0.0
	if len(args) > 0 {
		now = args[0].Float()
	}
	s.run(now)
	s.mu.Lock()
	alpha := s.alpha
	if s.queue.empty() {
		s.detachLocked()
	}
	s.mu.Unlock()
	return alpha
}

// onFallback runs the pending callbacks on the fallback clock, then attaches
// to the renderer if it has appeared meanwhile.
func (s *SceneFrameSource) onFallback(now float64) {
	s.mu.Lock()
	s.fallbackPending = 0
	s.mu.Unlock()
	s.run(now)
	s.mu.Lock()
	if s.queue.empty() || s.attached || s.tryAttachLocked() {
		s.mu.Unlock()
		return
	}
	need := s.fallbackPending == 0
	s.mu.Unlock()
	if need {
		pending := s.fallback.RequestFrame(s.fallbackFire)
		s.mu.Lock()
		s.fallbackPending = pending
		s.mu.Unlock()
	}
}

func (s *SceneFrameSource) run(now float64) {
	s.mu.Lock()
	s.queue.begin()
	s.firing = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.firing = false
		s.mu.Unlock()
	}()
	for {
		s.mu.Lock()
		cb, ok := s.queue.next()
		s.mu.Unlock()
		if !ok {
			return
		}
		cb(now)
	}
}
