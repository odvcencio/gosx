//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"math"
	"syscall/js"
)

// Surface is a typed client for an existing Scene3D mount. It does not own the
// renderer. Dispose releases its references without destroying that renderer.
type Surface struct {
	target   string
	mount    js.Value
	disposed bool
}

func NewSurface(target string) *Surface { return &Surface{target: target} }

func (s *Surface) element() (js.Value, error) {
	if s == nil || s.disposed || s.target == "" {
		return js.Undefined(), errors.New("scene: surface is unavailable")
	}
	connected := false
	if s.mount.Truthy() {
		state := s.mount.Get("isConnected")
		connected = state.Type() != js.TypeBoolean || state.Bool()
	}
	if !connected {
		s.mount = js.Global().Get("document").Call("getElementById", s.target)
	}
	if !s.mount.Truthy() {
		return js.Undefined(), errors.New("scene: surface mount not found")
	}
	return s.mount, nil
}

func (s *Surface) renderer() (js.Value, error) {
	mount, err := s.element()
	if err != nil {
		return js.Undefined(), err
	}
	handle := mount.Get("__gosxScene3DHandle")
	if !handle.Truthy() {
		return js.Undefined(), errors.New("scene: renderer is not ready")
	}
	return handle, nil
}

// DispatchCommands transfers an owned command snapshot to the renderer. Later
// mutation of the Go command slices cannot alter an in-flight transaction.
// Use pose/instance streams for per-frame transforms; this is the graph path.
func (s *Surface) DispatchCommands(batch MountCommandBatch) (err error) {
	defer surfaceRecover(&err)
	mount, err := s.element()
	if err != nil {
		return err
	}
	data, err := batch.Marshal()
	if err != nil {
		return err
	}
	detail := js.Global().Get("JSON").Call("parse", string(data))
	event := js.Global().Get("CustomEvent").New(MountCommandsEvent, map[string]any{"detail": detail})
	mount.Call("dispatchEvent", event)
	return nil
}

func (s *Surface) Update(options SurfaceOptions) (err error) {
	defer surfaceRecover(&err)
	if options.MaxFrameRate == nil && options.PostFXMaxPixels == nil {
		return nil
	}
	handle, err := s.renderer()
	if err != nil {
		return err
	}
	partial := js.Global().Get("Object").New()
	if options.MaxFrameRate != nil {
		fps := *options.MaxFrameRate
		if math.IsNaN(fps) || math.IsInf(fps, 0) || fps < 0 {
			return errors.New("scene: invalid frame rate")
		}
		partial.Set("maxFrameRate", fps)
	}
	if options.PostFXMaxPixels != nil {
		if *options.PostFXMaxPixels < 0 {
			return errors.New("scene: invalid post-FX pixel budget")
		}
		partial.Set("postFXMaxPixels", *options.PostFXMaxPixels)
	}
	handle.Call("updateSceneProps", partial)
	return nil
}

// Resize changes backing pixels only when needed. CSS layout remains owned by
// the mount. Call Update after a change to schedule a redraw of a static scene.
func (s *Surface) Resize(width, height int) (changed bool, err error) {
	defer surfaceRecover(&err)
	if width < 1 || height < 1 || width > 65536 || height > 65536 {
		return false, errors.New("scene: invalid backing dimensions")
	}
	mount, err := s.element()
	if err != nil {
		return false, err
	}
	canvas := mount.Call("querySelector", "canvas")
	if !canvas.Truthy() {
		return false, errors.New("scene: surface canvas is unavailable")
	}
	if canvas.Get("width").Int() != width {
		canvas.Set("width", width)
		changed = true
	}
	if canvas.Get("height").Int() != height {
		canvas.Set("height", height)
		changed = true
	}
	return changed, nil
}

func (s *Surface) Dispose() {
	if s != nil {
		s.disposed = true
		s.mount = js.Undefined()
	}
}

func surfaceRecover(err *error) {
	if value := recover(); value != nil {
		*err = fmt.Errorf("scene: browser surface: %v", value)
	}
}

// ContextLoss owns a diagnostic WebGL context-loss extension, when available.
type ContextLoss struct{ extension js.Value }

// LoseContext exercises the browser's actual recovery path. It returns an
// error on WebGPU or when WEBGL_lose_context is unavailable; no fake reset is
// reported as a successful device recovery.
func (s *Surface) LoseContext() (loss *ContextLoss, err error) {
	defer surfaceRecover(&err)
	mount, err := s.element()
	if err != nil {
		return nil, err
	}
	canvases := mount.Call("querySelectorAll", "canvas")
	for i := 0; i < canvases.Length(); i++ {
		canvas := canvases.Index(i)
		for _, name := range []string{"webgl2", "webgl"} {
			context := canvas.Call("getContext", name)
			if !context.Truthy() {
				continue
			}
			extension := context.Call("getExtension", "WEBGL_lose_context")
			if !extension.Truthy() {
				continue
			}
			extension.Call("loseContext")
			return &ContextLoss{extension: extension}, nil
		}
	}
	return nil, errors.New("scene: WEBGL_lose_context is unavailable")
}
func (c *ContextLoss) Restore() (err error) {
	defer surfaceRecover(&err)
	if c != nil && c.extension.Truthy() {
		c.extension.Call("restoreContext")
	}
	return nil
}
