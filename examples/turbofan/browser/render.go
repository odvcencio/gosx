//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"syscall/js"

	"m31labs.dev/gosx/engine/wasm"
	"m31labs.dev/gosx/examples/turbofan/cycle"
	"m31labs.dev/gosx/scene"
)

// renderLink sends the solved point through the public Scene3D command API.
// Renderer acceptance is asynchronous; a single buffered state coalesces rapid
// inputs. There is no second simulation clock or replacement of the scene.
type renderLink struct {
	viewer, controls js.Value
	updates          chan cycle.RenderState
	cancel           context.CancelFunc
}

func newRenderLink(ctx wasm.Context, initial cycle.RenderState) *renderLink {
	viewer := js.Global().Get("document").Call("querySelector", ".viewer")
	l := &renderLink{viewer: viewer, controls: viewer.Call("querySelector", "[data-render-controls]"), updates: make(chan cycle.RenderState, 1)}
	run, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	mount := viewer.Call("querySelector", "[data-gosx-scene3d]")
	target := ctx.Scene3D(mount)
	go func() {
		previous := [2]float64{initial.LowSpin, initial.HighSpin}
		last := initial
		ready := false
		var offset [2]float64
		for {
			var v cycle.RenderState
			select {
			case <-run.Done():
				return
			case v = <-l.updates:
			}
			// SSR already authored this exact operating point into the scene.
			// Reuse it instead of loading the command bridge for a no-op. The
			// bridge is needed only when a control changes the visual state.
			unchanged := v.Mode == last.Mode && v.Valid == last.Valid && v.Color == last.Color && math.Abs(v.LowSpin-last.LowSpin) < 1e-10 && math.Abs(v.HighSpin-last.HighSpin) < 1e-10 && math.Abs(v.TemperatureK-last.TemperatureK) < 1e-6
			if !unchanged && !ready {
				if err := target.Ready(run); err != nil {
					return
				}
				ready = true
			}
			clock, _ := strconv.ParseFloat(mount.Call("getAttribute", "data-gosx-scene3d-animation-clock").String(), 64)
			var commands []scene.Command
			for i, id := range []string{"low-spool", "high-spool"} {
				next := []float64{v.LowSpin, v.HighSpin}[i]
				// Spin is sampled against the resident scene clock. Offset its
				// authored rotation so a speed change preserves the current pose,
				// including while the user has rotation paused.
				offset[i] = math.Mod(offset[i]+(previous[i]-next)*clock, 2*math.Pi)
				previous[i] = next
				commands = append(commands, scene.Command{Kind: scene.CommandSetTransform, ObjectID: id, Data: map[string]any{"spinX": next, "rotationX": offset[i]}})
			}
			for _, id := range []string{"combustor-liner", "combustor-liner-cover"} {
				commands = append(commands, scene.Command{Kind: scene.CommandSetMaterial, ObjectID: id, Data: map[string]any{"color": v.Color}})
			}
			if !unchanged {
				if err := target.Dispatch(run, commands); err != nil {
					if run.Err() == nil {
						viewer.Call("setAttribute", "data-render-error", err.Error())
					}
					return
				}
			}
			last = v
			if run.Err() == nil {
				if data, err := json.Marshal(v); err == nil {
					viewer.Call("setAttribute", "data-render-applied", string(data))
				}
			}
		}
	}()
	return l
}

func (l *renderLink) update(v cycle.RenderState, fuel float64) {
	l.viewer.Call("setAttribute", "data-render-model", v.Mode)
	for key, value := range map[string]string{"n1": v.LowLabel, "n2": v.HighLabel, "temperature": v.TemperatureLabel, "caption": v.Caption} {
		l.viewer.Call("querySelector", "[data-render-output="+key+"]").Set("textContent", value)
	}
	toggle := l.controls.Call("querySelector", "[data-thermal-toggle]")
	toggle.Set("disabled", false)
	toggle.Call("setAttribute", "aria-pressed", strconv.FormatBool(v.ThermalTint))
	l.controls.Call("querySelector", "input").Set("value", strconv.FormatFloat(fuel, 'g', -1, 64))
	l.controls.Call("querySelector", "input").Set("disabled", false)
	l.controls.Call("querySelector", "[data-render-fuel-label]").Set("textContent", strconv.FormatFloat(fuel, 'f', 4, 64)+" kg/s")
	buttons := l.controls.Call("querySelectorAll", "button")
	for i := 0; i < buttons.Length(); i++ {
		buttons.Index(i).Set("disabled", false)
	}
	select {
	case <-l.updates:
	default:
	}
	l.updates <- v
}
