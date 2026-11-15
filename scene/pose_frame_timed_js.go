//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/client/jsutil"
)

// DispatchTimedPoseFrame sends frame, which holds the poses at simulation
// time at, to the Scene3D mount target for page-side interpolation (see the
// doc comment at the top of pose_frame_timed.go). step is the simulation's
// tick length. It works the same from a Web Worker engine and from the main
// thread, and it returns without waiting for the page.
func DispatchTimedPoseFrame(target string, frame PoseFrame, at, step time.Duration) error {
	return DispatchTimedPoseFrameWith(target, frame, at, step, TimedPoseOptions{})
}

// DispatchTimedPoseFrameWith is DispatchTimedPoseFrame with options.
func DispatchTimedPoseFrameWith(target string, frame PoseFrame, at, step time.Duration, opts TimedPoseOptions) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("enqueue Scene3D timed pose frame: %v", recovered)
		}
	}()
	if target == "" {
		return errors.New("scene pose frame target is empty")
	}
	atMS, stepMS, delayMS, err := timedPoseMillis(at, step, opts.Delay)
	if err != nil {
		return err
	}
	encoder := poseEncoderPool.Get().(*PoseFrameEncoder)
	defer poseEncoderPool.Put(encoder)
	data, err := encoder.Encode(frame)
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
	options := js.Global().Get("Object").New()
	options.Set("at", atMS)
	options.Set("step", stepMS)
	if delayMS > 0 {
		options.Set("delay", delayMS)
	}
	promise := api.Call("dispatchPoseFrame", target, jsutil.NewUint8ArrayFromBytes(data), options)
	if promise.IsUndefined() || promise.IsNull() || promise.Get("catch").Type() != js.TypeFunction {
		return errors.New("Scene3D pose frame dispatch did not return a Promise")
	}
	promise.Call("catch", poseFrameRejectionHandler())
	return nil
}
