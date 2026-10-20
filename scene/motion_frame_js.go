//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// DispatchMotionFrame encodes and applies a motion frame to a mounted
// Scene3D engine identified by its stable mount/engine ID. It returns after
// enqueueing without blocking the browser event loop. When GPU-motion
// application is unavailable, optional fallbackCommands use the JSON command
// path, and optional fallbackPoseFrame falls back to the existing per-frame
// PoseFrame (GSP2) channel instead -- useful while migrating one crowd at a
// time, or on a backend that has not implemented the GPU-motion path yet.
// Async failures appear in motionFrames telemetry and
// gosx:scene3d:motion-frame-error.
func DispatchMotionFrame(target string, frame MotionFrame, fallbackCommands []Command) error {
	return DispatchMotionFrameAfterCommands(target, nil, frame, fallbackCommands, nil)
}

// DispatchMotionFrameAfterCommands enqueues one ordered transaction:
// prerequisite membership/appearance commands, then the motion frame, with
// optional JSON and PoseFrame fallbacks. The browser keeps at most one
// active and one latest pending transaction per target; stale pending frames
// are superseded, exactly like DispatchPoseFrameAfterCommands.
func DispatchMotionFrameAfterCommands(target string, beforeCommands []Command, frame MotionFrame, fallbackCommands []Command, fallbackPoseFrame *PoseFrame) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("enqueue Scene3D motion frame: %v", recovered)
		}
	}()
	if target == "" {
		return errors.New("scene motion frame target is empty")
	}
	encoder := motionEncoderPool.Get().(*MotionFrameEncoder)
	defer motionEncoderPool.Put(encoder)
	data, err := encoder.Encode(frame)
	if err != nil {
		return err
	}
	root := js.Global().Get("__gosx")
	if root.IsUndefined() || root.IsNull() {
		return errors.New("GoSX browser runtime is unavailable")
	}
	api := root.Get("scene3d")
	if api.IsUndefined() || api.IsNull() || api.Get("dispatchMotionFrame").Type() != js.TypeFunction {
		return errors.New("Scene3D motion frame dispatch is unavailable")
	}
	options := js.Undefined()
	if beforeCommands != nil || fallbackCommands != nil || fallbackPoseFrame != nil {
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
	if fallbackPoseFrame != nil {
		poseEncoder := poseEncoderPool.Get().(*PoseFrameEncoder)
		poseData, encodeErr := poseEncoder.Encode(*fallbackPoseFrame)
		if encodeErr == nil {
			// Copy out before the pooled encoder's buffer is reused.
			copied := jsutil.NewUint8ArrayFromBytes(append([]byte(nil), poseData...))
			options.Set("fallbackPoseFrame", copied)
		}
		poseEncoderPool.Put(poseEncoder)
		if encodeErr != nil {
			return fmt.Errorf("encode Scene3D fallback pose frame: %w", encodeErr)
		}
	}
	promise := api.Call("dispatchMotionFrame", target, jsutil.NewUint8ArrayFromBytes(data), options)
	if promise.IsUndefined() || promise.IsNull() || promise.Get("catch").Type() != js.TypeFunction {
		return errors.New("Scene3D motion frame dispatch did not return a Promise")
	}
	promise.Call("catch", motionFrameRejectionHandler())
	return nil
}

var (
	motionEncoderPool   = sync.Pool{New: func() any { return new(MotionFrameEncoder) }}
	motionRejectionOnce sync.Once
	motionRejectionFunc js.Func
)

func motionFrameRejectionHandler() js.Value {
	motionRejectionOnce.Do(func() {
		motionRejectionFunc = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				js.Global().Get("console").Call("error", "[gosx] Scene3D motion frame rejected:", jsutil.Describe(args[0]))
			}
			return nil
		})
	})
	return motionRejectionFunc.Value
}

// poseCommandsJS and poseFrameRejectionHandler's pattern are defined in
// pose_frame_js.go; poseCommandsJS is reused above unchanged.
