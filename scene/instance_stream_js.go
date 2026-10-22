//go:build js && wasm

package scene

import (
	"context"
	"errors"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/client/jsutil"
)

// StreamWriter keeps reusable Go and browser buffers for synchronous retained
// instance updates. Prepare loads the codec without creating dummy geometry.
// Call Prepare once before a batch of Apply calls. It refreshes the renderer
// handle after context recovery; Apply itself performs no DOM lookup or JSON.
// A writer belongs to one browser event loop and rejects reentrant application.
type StreamWriter struct {
	surface                        *Surface
	handle, bytes, view            js.Value
	buffer                         []byte
	capacity, viewLength           int
	loading, ready, disposed, busy bool
	retryAt                        float64
	cancel                         context.CancelFunc
}

func NewStreamWriter(target string) *StreamWriter { return &StreamWriter{surface: NewSurface(target)} }

func (w *StreamWriter) Prepare() (ready bool) {
	defer func() {
		if recover() != nil {
			if w != nil && !w.disposed {
				w.ready = false
				w.retryAt = browser.Now() + 1000
			}
			ready = false
		}
	}()
	if w == nil || w.disposed {
		return false
	}
	w.ready = false
	if scene := workerScene3D(); scene.Truthy() {
		// In an engine worker the page applies the stream; there is no
		// renderer handle or codec to load here.
		w.ready = true
		return true
	}
	handle, err := w.surface.renderer()
	if err != nil || handle.Get("applyInstanceStream").Type() != js.TypeFunction {
		return false
	}
	w.handle = handle
	if js.Global().Get("__gosx_scene3d_instance_stream_apply").Type() == js.TypeFunction {
		w.ready = true
		return true
	}
	if w.loading || browser.Now() < w.retryAt {
		return false
	}
	namespace := js.Global().Get("__gosx")
	if !namespace.Truthy() || !namespace.Get("host").Truthy() {
		return false
	}
	sceneHost := namespace.Get("host").Get("scene3d")
	if !sceneHost.Truthy() {
		return false
	}
	preload := sceneHost.Get("preloadInstanceStream")
	if preload.Type() != js.TypeFunction {
		return false
	}
	promise := preload.Invoke()
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.loading = true
	go func() {
		_, err := jsutil.AwaitPromiseContext(ctx, promise)
		cancel()
		w.loading = false
		if err != nil && !w.disposed {
			w.retryAt = browser.Now() + 1000
		}
	}()
	return false
}

// Apply copies the encoded frame into reusable JS storage and returns only
// after the renderer has copied its view into retained buffers. The next call
// can safely overwrite both buffers. False lets a caller keep its declaration
// fallback while resources are loading or the renderer rejects the frame.
func (w *StreamWriter) Apply(frame InstanceStreamFrame) (applied bool, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("scene: instance stream: %v", value)
		}
	}()
	if w == nil || w.disposed || !w.ready {
		return false, nil
	}
	if w.busy {
		return false, errors.New("scene: reentrant instance stream write")
	}
	w.busy = true
	defer func() { w.busy = false }()
	data, err := frame.EncodeInto(w.buffer)
	if err != nil {
		return false, err
	}
	w.buffer = data
	if scene := workerScene3D(); scene.Truthy() {
		// The worker transfers a fresh copy; the reusable buffers stay here.
		// "Applied" means queued for the page; a page-side rejection arrives
		// as a gosx:engine:worker-frame-error event.
		result := scene.Call("applyInstanceStream", w.surface.target, jsutil.NewUint8ArrayFromBytes(data))
		return result.Truthy() && result.Get("applied").Truthy(), nil
	}
	if len(data) > w.capacity {
		w.capacity = max(len(data), 2*w.capacity)
		w.bytes = js.Global().Get("Uint8Array").New(w.capacity)
		w.viewLength = -1
	}
	if w.viewLength != len(data) {
		w.view = w.bytes.Call("subarray", 0, len(data))
		w.viewLength = len(data)
	}
	js.CopyBytesToJS(w.view, data)
	result := w.handle.Call("applyInstanceStream", w.view)
	return result.Truthy() && result.Get("applied").Truthy(), nil
}

// workerScene3D returns the worker's __gosx.scene3d stand-in, or undefined on
// the main thread.
func workerScene3D() js.Value {
	if _, ok := workerBridge(); !ok {
		return js.Undefined()
	}
	scene := js.Global().Get("__gosx").Get("scene3d")
	if scene.Truthy() && scene.Get("applyInstanceStream").Type() == js.TypeFunction {
		return scene
	}
	return js.Undefined()
}

func (w *StreamWriter) Dispose() {
	if w == nil || w.disposed {
		return
	}
	w.disposed = true
	w.ready = false
	if w.cancel != nil {
		w.cancel()
	}
	w.surface.Dispose()
	w.buffer = nil
	w.bytes = js.Undefined()
	w.view = js.Undefined()
	w.handle = js.Undefined()
}
