//go:build js && wasm

package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// DispatchPoseFrame encodes and applies a pose frame to a mounted Scene3D
// engine identified by its stable mount/engine ID. It returns after enqueueing
// without blocking the browser event loop. When retained application is
// unavailable, optional fallbackCommands use the JSON command path. Async
// failures appear in poseFrames telemetry and gosx:scene3d:pose-frame-error.
func DispatchPoseFrame(target string, frame PoseFrame, fallbackCommands []Command) error {
	return DispatchPoseFrameAfterCommands(target, nil, frame, fallbackCommands)
}

// DispatchPoseFrameAfterCommands enqueues one ordered transaction:
// prerequisite membership/appearance commands, then the pose, with optional
// JSON fallback. The browser keeps at most one active and one latest pending
// transaction per target; stale pending frames are superseded.
func DispatchPoseFrameAfterCommands(target string, beforeCommands []Command, frame PoseFrame, fallbackCommands []Command) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("enqueue Scene3D pose frame: %v", recovered)
		}
	}()
	if target == "" {
		return errors.New("scene pose frame target is empty")
	}
	data, err := EncodePoseFrame(frame)
	if err != nil {
		return err
	}
	root := js.Global().Get("__gosx")
	if root.IsUndefined() || root.IsNull() {
		return errors.New("GoSX browser runtime is unavailable")
	}
	api := root.Get("scene3d")
	if api.IsUndefined() || api.IsNull() || api.Get("dispatchPoseFrame").Type() != js.TypeFunction {
		return errors.New("Scene3D pose frame dispatch is unavailable")
	}
	options := js.Undefined()
	if beforeCommands != nil || fallbackCommands != nil {
		options = js.Global().Get("Object").New()
	}
	if beforeCommands != nil {
		commands, encodeErr := poseCommandsJS(beforeCommands)
		if encodeErr != nil {
			return fmt.Errorf("encode Scene3D prerequisite commands: %w", encodeErr)
		}
		options.Set("beforeCommands", commands)
	}
	if fallbackCommands != nil {
		commands, encodeErr := poseCommandsJS(fallbackCommands)
		if encodeErr != nil {
			return fmt.Errorf("encode Scene3D fallback commands: %w", encodeErr)
		}
		options.Set("fallbackCommands", commands)
	}
	promise := api.Call("dispatchPoseFrame", target, jsutil.NewUint8ArrayFromBytes(data), options)
	if promise.IsUndefined() || promise.IsNull() || promise.Get("catch").Type() != js.TypeFunction {
		return errors.New("Scene3D pose frame dispatch did not return a Promise")
	}
	promise.Call("catch", poseFrameRejectionHandler())
	return nil
}

var (
	poseRejectionOnce sync.Once
	poseRejectionFunc js.Func
)

func poseFrameRejectionHandler() js.Value {
	poseRejectionOnce.Do(func() {
		poseRejectionFunc = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				js.Global().Get("console").Call("error", "[gosx] Scene3D pose frame rejected:", jsutil.Describe(args[0]))
			}
			return nil
		})
	})
	return poseRejectionFunc.Value
}

func poseCommandsJS(commands []Command) (js.Value, error) {
	payload, err := json.Marshal(commands)
	if err != nil {
		return js.Undefined(), err
	}
	return js.Global().Get("JSON").Call("parse", string(payload)), nil
}
