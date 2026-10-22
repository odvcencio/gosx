//go:build !js || !wasm

package scene

import "errors"

// DispatchVesselPose is available only in a browser WASM runtime.
func DispatchVesselPose(string, VesselPoseFrame) error {
	return errors.New("Scene3D vessel pose dispatch requires js/wasm")
}
