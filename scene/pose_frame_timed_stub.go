//go:build !js || !wasm

package scene

import (
	"errors"
	"time"
)

// DispatchTimedPoseFrame is available only in a browser WASM runtime.
func DispatchTimedPoseFrame(string, PoseFrame, time.Duration, time.Duration) error {
	return errors.New("Scene3D timed pose frame dispatch requires js/wasm")
}

// DispatchTimedPoseFrameWith is available only in a browser WASM runtime.
func DispatchTimedPoseFrameWith(string, PoseFrame, time.Duration, time.Duration, TimedPoseOptions) error {
	return errors.New("Scene3D timed pose frame dispatch requires js/wasm")
}
