//go:build js && wasm

package host

// FallBackAfter makes the source run its waiting callbacks on its own
// requestAnimationFrame while the attached renderer draws no frame for
// frames animation frames in a row: the renderer stops drawing when its
// canvas is hidden (a menu over the scene, say), and a game's loop must keep
// stepping. The renderer takes over again on the first frame it draws. Zero,
// the default, turns the watch off.
//
// While the watch is on and callbacks wait, the source keeps one
// requestAnimationFrame request outstanding beside the renderer's.
func (s *SceneFrameSource) FallBackAfter(frames int) {
	s.mu.Lock()
	s.watch = max(0, frames)
	arm := s.armWatchLocked()
	s.mu.Unlock()
	s.armWatch(arm)
}

// armWatchLocked reports whether the watch needs a frame request: it is on,
// the renderer drives the source, callbacks wait and none is outstanding.
func (s *SceneFrameSource) armWatchLocked() bool {
	if s.watch == 0 || !s.attached || s.watchPending != 0 || s.queue.empty() {
		return false
	}
	s.watchPending = -1 // reserved until armWatch stores the handle
	return true
}

func (s *SceneFrameSource) armWatch(arm bool) {
	if !arm {
		return
	}
	pending := s.fallback.RequestFrame(s.watchFire)
	s.mu.Lock()
	if s.watchPending == -1 {
		s.watchPending = pending
		pending = 0
	}
	s.mu.Unlock()
	if pending != 0 {
		// Cancelled while the request was being made.
		s.fallback.CancelFrame(pending)
	}
}

func (s *SceneFrameSource) cancelWatch(handle int) {
	if handle > 0 {
		s.fallback.CancelFrame(handle)
	}
}

// onWatch runs on the fallback clock while the renderer drives the source.
// It counts the animation frames in which the renderer drew nothing, and once
// they reach the watch it runs the waiting callbacks itself.
func (s *SceneFrameSource) onWatch(now float64) {
	s.mu.Lock()
	s.watchPending = 0
	if !s.attached || s.watch == 0 {
		s.mu.Unlock()
		return
	}
	if s.drawn != s.seen {
		s.seen, s.stale = s.drawn, 0
	} else {
		s.stale++
	}
	stalled := s.stale >= s.watch && !s.queue.empty()
	s.mu.Unlock()
	if stalled {
		s.run(now)
	}
	s.mu.Lock()
	arm := s.armWatchLocked()
	s.mu.Unlock()
	s.armWatch(arm)
}
