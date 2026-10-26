//go:build !js || !wasm

package scene

import "errors"

// DispatchMotionFrame is available only in a browser WASM runtime.
func DispatchMotionFrame(string, MotionFrame, []Command) error {
	return errors.New("Scene3D motion frame dispatch requires js/wasm")
}

// DispatchMotionFrameAfterCommands is available only in a browser WASM runtime.
func DispatchMotionFrameAfterCommands(string, []Command, MotionFrame, []Command, *PoseFrame) error {
	return errors.New("Scene3D motion frame dispatch requires js/wasm")
}

// DispatchPartialMotionFrame is available only in a browser WASM runtime.
func DispatchPartialMotionFrame(string, PartialMotionFrame, []Command) error {
	return errors.New("Scene3D motion frame dispatch requires js/wasm")
}

// DispatchPartialMotionFrameAfterCommands is available only in a browser WASM runtime.
func DispatchPartialMotionFrameAfterCommands(string, []Command, PartialMotionFrame, []Command, *PoseFrame) error {
	return errors.New("Scene3D motion frame dispatch requires js/wasm")
}
