//go:build js && wasm

package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"
)

// DispatchVesselCutaway changes the cutaway of the vessel nodeID on the mount
// whose element ID is target. It returns an error until the vessel has mounted,
// so call it after the scene is ready. In an engine worker the page's answer
// arrives asynchronously, so the first call for a node returns the error.
func DispatchVesselCutaway(target, nodeID string, c VesselCutaway) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dispatch Scene3D vessel cutaway: %v", r)
		}
	}()
	if target == "" || nodeID == "" {
		return errors.New("scene vessel cutaway needs a target and a node ID")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	fn := js.Global().Get("__gosx_scene3d_vessel_cutaway")
	if fn.Type() != js.TypeFunction {
		return errors.New("Scene3D vessel controller is not loaded")
	}
	if !fn.Invoke(target, nodeID, string(data)).Bool() {
		return errors.New("Scene3D mount has no cutaway-capable vessel with that ID")
	}
	return nil
}
