//go:build js && wasm

package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// StartParticleBurst starts a burst on the mounted Scene3D named target (its
// mount element ID or engine ID) and returns at once. The page loads the
// burst chunk on first use, plays the burst and removes it at its deadline.
// The scene must opt in with Props.ParticleBursts.
//
// It returns an error when the burst is invalid or the page has no Scene3D
// runtime. A burst the page later refuses (an unmounted target, a backend
// with no particles) is reported on the console, because the refusal arrives
// after this call has returned. Reduced motion suppresses the burst in the
// page; that is not an error.
//
// A burst is an event, not a per-frame write: the call encodes JSON. Use it
// from a game's render callback or a fixed step; it does not wait.
func StartParticleBurst(target string, burst ParticleBurst) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("start Scene3D particle burst: %v", recovered)
		}
	}()
	if target == "" {
		return errors.New("scene particle burst target is empty")
	}
	data, err := json.Marshal(burst)
	if err != nil {
		return err
	}
	root := js.Global().Get("__gosx")
	if root.IsUndefined() || root.IsNull() {
		return errors.New("GoSX browser runtime is unavailable")
	}
	api := root.Get("scene3d")
	if api.IsUndefined() || api.IsNull() || api.Get("burstParticles").Type() != js.TypeFunction {
		return errors.New("Scene3D particle bursts are unavailable")
	}
	plan := js.Global().Get("JSON").Call("parse", string(data))
	promise := api.Call("burstParticles", target, plan)
	if promise.Truthy() && promise.Get("catch").Type() == js.TypeFunction {
		promise.Call("catch", burstRejectionHandler())
	}
	return nil
}

var (
	burstRejectionOnce sync.Once
	burstRejectionFunc js.Func
)

func burstRejectionHandler() js.Value {
	burstRejectionOnce.Do(func() {
		burstRejectionFunc = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				js.Global().Get("console").Call("error", "[gosx] Scene3D particle burst rejected:", jsutil.Describe(args[0]))
			}
			return nil
		})
	})
	return burstRejectionFunc.Value
}
