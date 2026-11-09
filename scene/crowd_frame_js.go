//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// DispatchCrowdFrame encodes frame and applies it to the crowd on a mounted
// Scene3D engine, identified by its mount or engine ID. It returns once the
// frame is queued. The browser fetches the crowd chunk on first use, and a
// frame that arrives before its crowd has drawn waits for it and applies then.
func DispatchCrowdFrame(target string, frame CrowdFrame) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("enqueue Scene3D crowd frame: %v", recovered)
		}
	}()
	if target == "" {
		return errors.New("scene crowd frame target is empty")
	}
	data, err := EncodeCrowdFrame(frame)
	if err != nil {
		return err
	}
	root := js.Global().Get("__gosx")
	if root.IsUndefined() || root.IsNull() {
		return errors.New("GoSX browser runtime is unavailable")
	}
	api := root.Get("scene3d")
	if api.IsUndefined() || api.IsNull() || api.Get("dispatchCrowdFrame").Type() != js.TypeFunction {
		return errors.New("Scene3D crowd frame dispatch is unavailable")
	}
	promise := api.Call("dispatchCrowdFrame", target, jsutil.NewUint8ArrayFromBytes(data))
	if promise.IsUndefined() || promise.IsNull() || promise.Get("catch").Type() != js.TypeFunction {
		return errors.New("Scene3D crowd frame dispatch did not return a Promise")
	}
	promise.Call("catch", crowdFrameRejectionHandler())
	return nil
}

var (
	crowdRejectionOnce sync.Once
	crowdRejectionFunc js.Func
)

func crowdFrameRejectionHandler() js.Value {
	crowdRejectionOnce.Do(func() {
		crowdRejectionFunc = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				js.Global().Get("console").Call("error", "[gosx] Scene3D crowd frame rejected:", jsutil.Describe(args[0]))
			}
			return nil
		})
	})
	return crowdRejectionFunc.Value
}
