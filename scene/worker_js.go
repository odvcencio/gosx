//go:build js && wasm

package scene

import (
	"encoding/json"
	"errors"
	"syscall/js"
)

// errWorkerDOM reports a Surface operation that needs the page's DOM.
var errWorkerDOM = errors.New("scene: this operation needs the page DOM and is unavailable in an engine worker")

// workerBridge returns the engine worker's page bridge (__gosx.worker). It is
// present only inside a GoSX engine worker (engine.Config Thread: worker),
// where there is no window or document. The page owns the Scene3D mount, so a
// worker Surface sends events and calls to the page, and receives renderer
// events back, over the engine port. Binary frames (DispatchPoseFrame,
// DispatchMotionFrame, DispatchClipFrame, StreamWriter) use the same
// __gosx.scene3d entry points as on the main thread; the worker provides them
// and transfers each frame's buffer to the page.
func workerBridge() (js.Value, bool) {
	root := js.Global().Get("__gosx")
	if !root.Truthy() {
		return js.Undefined(), false
	}
	bridge := root.Get("worker")
	if bridge.Truthy() && bridge.Get("mountOp").Type() == js.TypeFunction {
		return bridge, true
	}
	return js.Undefined(), false
}

func (s *Surface) workerDispatchCommands(bridge js.Value, batch MountCommandBatch) error {
	if s == nil || s.disposed || s.target == "" {
		return errors.New("scene: surface is unavailable")
	}
	data, err := batch.Marshal()
	if err != nil {
		return err
	}
	bridge.Call("mountOp", s.target, "dispatchEvent", MountCommandsEvent, string(data))
	if batch.FrameRequest != nil {
		s.frameRequestID = batch.FrameRequest.ID
	}
	return nil
}

func (s *Surface) workerOnFrameSubmitted(bridge js.Value, handler func(MountFrameSubmission)) (func(), error) {
	if s == nil || s.disposed || s.target == "" {
		return nil, errors.New("scene: surface is unavailable")
	}
	active := true
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !active || s.disposed || len(args) == 0 || args[0].Type() != js.TypeString {
			return nil
		}
		var receipt MountFrameSubmission
		if json.Unmarshal([]byte(args[0].String()), &receipt) == nil && receipt.RequestID != "" && receipt.Revision > 0 &&
			receipt.CommandRevision >= receipt.Revision && receipt.HydrationGeneration > 0 && receipt.FrameSerial > 0 {
			if s.frameRequestID == receipt.RequestID {
				s.frameRequestID = ""
			}
			handler(receipt)
		}
		return nil
	})
	unlisten := bridge.Call("mountListen", s.target, MountFrameSubmittedEvent, callback)
	cancel := func() {
		if active {
			active = false
			if unlisten.Type() == js.TypeFunction {
				unlisten.Invoke()
			}
			callback.Release()
		}
	}
	s.frameSubscriptions = append(s.frameSubscriptions, cancel)
	return cancel, nil
}

func (s *Surface) workerCancelFrameRequest(bridge js.Value, id string) error {
	if s == nil || s.disposed || s.target == "" {
		return errors.New("scene: surface is unavailable")
	}
	payload, err := json.Marshal(map[string]any{"id": id})
	if err != nil {
		return err
	}
	bridge.Call("mountOp", s.target, "dispatchEvent", MountFrameCancelEvent, string(payload))
	if s.frameRequestID == id {
		s.frameRequestID = ""
	}
	return nil
}

func (s *Surface) workerUpdate(bridge js.Value, partial map[string]any) error {
	if s == nil || s.disposed || s.target == "" {
		return errors.New("scene: surface is unavailable")
	}
	payload, err := json.Marshal(partial)
	if err != nil {
		return err
	}
	bridge.Call("mountOp", s.target, "call", "updateSceneProps", string(payload))
	return nil
}
