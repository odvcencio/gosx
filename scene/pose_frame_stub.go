//go:build !js || !wasm

package scene

import "errors"

// DispatchPoseFrame is available only in a browser WASM runtime.
func DispatchPoseFrame(string, PoseFrame, []Command) error {
	return errors.New("Scene3D pose frame dispatch requires js/wasm")
}

// DispatchPoseFrameAfterCommands is available only in a browser WASM runtime.
func DispatchPoseFrameAfterCommands(string, []Command, PoseFrame, []Command) error {
	return errors.New("Scene3D pose frame dispatch requires js/wasm")
}
