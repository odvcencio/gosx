//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

var vesselPoseBuffers = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

// DispatchVesselPose sends a pose frame to the Scene3D mount whose element ID
// is target. It is synchronous and cheap: one encode into a pooled buffer and
// one call into the lazy vessel controller. It returns an error while the
// controller has not mounted yet or the mount is not showing a Vessel with
// ExternalPose; a game that sends a pose every tick can ignore those errors
// during start-up. In an engine worker the page's answer arrives asynchronously:
// the first call returns the not-ready error, and later calls return what the
// page last answered.
func DispatchVesselPose(target string, frame VesselPoseFrame) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dispatch Scene3D vessel pose: %v", r)
		}
	}()
	if target == "" {
		return errors.New("scene vessel pose target is empty")
	}
	buf := vesselPoseBuffers.Get().(*[]byte)
	defer vesselPoseBuffers.Put(buf)
	data, err := EncodeVesselPoseFrame((*buf)[:0], frame)
	if err != nil {
		return err
	}
	*buf = data
	fn := js.Global().Get("__gosx_scene3d_vessel_pose")
	if fn.Type() != js.TypeFunction {
		return errors.New("Scene3D vessel controller is not loaded")
	}
	if !fn.Invoke(target, jsutil.NewUint8ArrayFromBytes(data)).Bool() {
		return errors.New("Scene3D mount has no external-pose vessel yet")
	}
	return nil
}
