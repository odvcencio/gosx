//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// DispatchClipFrame encodes a ClipFrame and applies it to the mounted Scene3D
// engine identified by target. It returns after enqueueing, without blocking
// the browser event loop. Async failures appear in clipFrames telemetry and in
// the gosx:scene3d:clip-frame-error event.
func DispatchClipFrame(target string, frame ClipFrame) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("enqueue Scene3D clip frame: %v", recovered)
		}
	}()
	if target == "" {
		return errors.New("scene clip frame target is empty")
	}
	encoder := clipEncoderPool.Get().(*ClipFrameEncoder)
	defer clipEncoderPool.Put(encoder)
	data, err := encoder.Encode(frame)
	if err != nil {
		return err
	}
	root := js.Global().Get("__gosx")
	if root.IsUndefined() || root.IsNull() {
		return errors.New("GoSX browser runtime is unavailable")
	}
	api := root.Get("scene3d")
	if api.IsUndefined() || api.IsNull() || api.Get("dispatchClipFrame").Type() != js.TypeFunction {
		return errors.New("Scene3D clip frame dispatch is unavailable")
	}
	promise := api.Call("dispatchClipFrame", target, jsutil.NewUint8ArrayFromBytes(data))
	if promise.IsUndefined() || promise.IsNull() || promise.Get("catch").Type() != js.TypeFunction {
		return errors.New("Scene3D clip frame dispatch did not return a Promise")
	}
	promise.Call("catch", clipFrameRejectionHandler())
	return nil
}

var (
	clipEncoderPool   = sync.Pool{New: func() any { return new(ClipFrameEncoder) }}
	clipRejectionOnce sync.Once
	clipRejectionFunc js.Func
)

func clipFrameRejectionHandler() js.Value {
	clipRejectionOnce.Do(func() {
		clipRejectionFunc = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				js.Global().Get("console").Call("error", "[gosx] Scene3D clip frame rejected:", jsutil.Describe(args[0]))
			}
			return nil
		})
	})
	return clipRejectionFunc.Value
}
