//go:build !js || !wasm

package scene

import "errors"

// DispatchVesselCutaway is available only in a browser WASM runtime.
func DispatchVesselCutaway(string, string, VesselCutaway) error {
	return errors.New("Scene3D vessel cutaway dispatch requires js/wasm")
}
