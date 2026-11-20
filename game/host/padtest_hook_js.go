//go:build js && wasm && gosx_padtest

package host

import "sync/atomic"

// padHooks is what the pad test adapter installs: the source GamepadReader
// reads instead of navigator.getGamepads(), and the functions that bracket the
// designated engine dispatch. Only this tagged build has any of it: the
// ordinary build compiles gamepad_js.go, frames_js.go and scene_frames_js.go,
// which hold none of this.
type padHooks struct {
	source GamepadSource
	begin  func()
	end    func()
}

var padHook atomic.Pointer[padHooks]

// padOverride returns the installed alternate pad source, or nil.
func padOverride() GamepadSource {
	if h := padHook.Load(); h != nil {
		return h.source
	}
	return nil
}

// The browser runs these on one thread.
var (
	// padDesignated is the one dispatcher whose batches are frames: the first
	// that finds a callback to run after the adapter is installed, as an
	// owner pointer (*FrameSourceJS or *SceneFrameSource). A SceneFrameSource's
	// own fallback clock is not a separate owner. A game must run one frame
	// loop; a second source's batches are not frames and are not counted.
	padDesignated any
	// padInFrame is true between the designated dispatcher's begin and end.
	padInFrame bool
)

// padRelease forgets owner when a released source was the designated one.
func padRelease(owner any) {
	if padDesignated == owner {
		padDesignated = nil
	}
}

// frameBracket marks one dispatcher's batch. enter is called with the source,
// just before a callback runs (so a batch with no callback makes no frame):
// for the designated source it begins the pad frame, once per batch. leave is
// deferred and ends the frame after the last callback returns, or panics, so
// the frame is acknowledged and the source is not left open.
type frameBracket struct {
	open  bool
	hooks *padHooks
}

func (b *frameBracket) enter(owner any) {
	if b.open || padInFrame {
		return
	}
	h := padHook.Load()
	if h == nil {
		return
	}
	if padDesignated == nil {
		padDesignated = owner
	} else if padDesignated != owner {
		return
	}
	padInFrame, b.open, b.hooks = true, true, h
	if h.begin != nil {
		h.begin()
	}
}

func (b *frameBracket) leave() {
	if !b.open {
		return
	}
	b.open, padInFrame = false, false
	if b.hooks.end != nil {
		b.hooks.end()
	}
	b.hooks = nil
}
