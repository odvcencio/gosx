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

// TextureWriter creates and updates host-written data textures in an existing
// Scene3D mount. A Selena material reads one with load() after its texture
// param is set to DataTextureRef(key) in the material's uniforms.
//
// Frames travel as binary GSXT bytes, never JSON (see DataTextureFrame). Like
// StreamWriter, the writer copies each frame into a reused JS buffer and the
// renderer copies the changed rectangle into its own storage before the call
// returns, so the Go slice may be reused at once. Call Prepare until it
// returns true, then Create, WriteTexture and Delete. In an engine worker the
// page applies the frame; a page-side rejection arrives as a
// gosx:engine:worker-frame-error event.
//
// A writer belongs to one browser event loop and rejects reentrant calls.
type TextureWriter struct {
	surface                        *Surface
	handle, bytes, view            js.Value
	encoder                        *DataTextureEncoder
	capacity, viewLength           int
	loading, ready, disposed, busy bool
	retryAt                        float64
	cancel                         context.CancelFunc
	loadErr                        error
}

// NewTextureWriter returns a writer for the Scene3D mount with the given element id.
func NewTextureWriter(target string) *TextureWriter {
	return &TextureWriter{surface: NewSurface(target), encoder: NewDataTextureEncoder()}
}

// Prepare reports whether the renderer can take texture writes, and loads the
// data-texture chunk when it cannot yet. Call it each frame until it returns
// true; it retries a failed load after one second.
func (w *TextureWriter) Prepare() (ready bool) {
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
	if scene := workerDataTextureScene3D(); scene.Truthy() {
		w.ready = true
		return true
	}
	handle, err := w.surface.renderer()
	if err != nil || handle.Get("applyDataTexture").Type() != js.TypeFunction {
		return false
	}
	w.handle = handle
	if js.Global().Get("__gosx_scene3d_data_texture_api").Truthy() {
		w.ready = true
		return true
	}
	if w.loading || browser.Now() < w.retryAt {
		return false
	}
	api := js.Global().Get("__gosx_scene3d_api")
	if !api.Truthy() || api.Get("loadPresentation").Type() != js.TypeFunction {
		return false
	}
	promise := api.Call("loadPresentation", "data-texture", "gosxScene3dDataTextureUrl", "")
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.loading = true
	go func() {
		_, err := jsutil.AwaitPromiseContext(ctx, promise)
		cancel()
		w.loading = false
		w.loadErr = err
		if err != nil && !w.disposed {
			w.retryAt = browser.Now() + 1000
		}
	}()
	return false
}

// Err returns why the data-texture chunk failed to load on the last attempt,
// for example because the page did not advertise it (no Selena material on the
// page reads a data texture). It is nil while loading or after a success.
func (w *TextureWriter) Err() error {
	if w == nil {
		return nil
	}
	return w.loadErr
}

// Create declares a width x height texture in format under key, zero-filled.
// Creating a key again clears it, and replaces it when the size or format
// differs. It returns false, nil while the renderer is not ready.
func (w *TextureWriter) Create(key string, width, height int, format TextureFormat) (bool, error) {
	return w.send(key, func() ([]byte, error) { return w.encoder.Create(key, width, height, format) })
}

// WriteTexture copies texels into the width x height rectangle whose top-left
// texel is (originX, originY) of the texture created under key. format must
// match the texture. texels holds width*height texels, top row first. It
// returns false, nil while the renderer is not ready, and an error when the
// arguments are invalid or the renderer rejects the frame.
func (w *TextureWriter) WriteTexture(key string, width, height int, format TextureFormat, texels []byte, originX, originY int) (bool, error) {
	return w.send(key, func() ([]byte, error) {
		return w.encoder.WriteTexture(key, width, height, format, texels, originX, originY)
	})
}

// Delete releases the texture under key.
func (w *TextureWriter) Delete(key string) (bool, error) {
	return w.send(key, func() ([]byte, error) { return w.encoder.Delete(key) })
}

func (w *TextureWriter) send(key string, encode func() ([]byte, error)) (applied bool, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("scene: data texture %q: %v", key, value)
		}
	}()
	if w == nil || w.disposed || !w.ready {
		return false, nil
	}
	if w.busy {
		return false, errors.New("scene: reentrant data texture write")
	}
	w.busy = true
	defer func() { w.busy = false }()
	data, err := encode()
	if err != nil {
		return false, err
	}
	if scene := workerDataTextureScene3D(); scene.Truthy() {
		// The worker transfers a fresh copy; the page reports rejections.
		result := scene.Call("applyDataTexture", w.surface.target, jsutil.NewUint8ArrayFromBytes(data))
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
	result := w.handle.Call("applyDataTexture", w.view)
	if result.Truthy() && result.Get("applied").Truthy() {
		return true, nil
	}
	if reason := result.Get("reason"); reason.Type() == js.TypeString {
		return false, fmt.Errorf("scene: data texture %q: %s", key, reason.String())
	}
	return false, nil
}

// workerDataTextureScene3D returns the worker's __gosx.scene3d stand-in, or
// undefined on the main thread.
func workerDataTextureScene3D() js.Value {
	if _, ok := workerBridge(); !ok {
		return js.Undefined()
	}
	scene := js.Global().Get("__gosx").Get("scene3d")
	if scene.Truthy() && scene.Get("applyDataTexture").Type() == js.TypeFunction {
		return scene
	}
	return js.Undefined()
}

// Dispose releases the writer's buffers. The textures stay in the renderer
// until Delete removes them.
func (w *TextureWriter) Dispose() {
	if w == nil || w.disposed {
		return
	}
	w.disposed = true
	w.ready = false
	if w.cancel != nil {
		w.cancel()
	}
	w.surface.Dispose()
	w.bytes, w.view, w.handle = js.Undefined(), js.Undefined(), js.Undefined()
}
