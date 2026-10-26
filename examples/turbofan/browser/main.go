//go:build js && wasm

// The cycle engine owns only its Surface mount. All computation is Go; it
// shares the exact cycle package used by native tests and server rendering.
package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"syscall/js"

	"m31labs.dev/gosx/engine/wasm"
	"m31labs.dev/gosx/examples/turbofan/cycle"
)

func main() {
	if err := wasm.Register("TurbofanCycle", mount); err != nil {
		panic(err)
	}
	select {}
}

func mount(ctx wasm.Context) (wasm.Handle, error) {
	root := ctx.Mount()
	query := func(selector string) js.Value { return root.Call("querySelector", selector) }
	if !query("[name=altitudeM]").Truthy() {
		return nil, fmt.Errorf("cycle controls are missing")
	}
	m := cycle.DefaultModel()
	var result cycle.Result
	var solveErr error
	var downloadURL string
	setInputs := func(in cycle.Inputs) {
		for name, value := range map[string]float64{"altitudeM": in.AltitudeM, "mach": in.Mach, "turbineInletK": in.TurbineInletK,
			"overallPressureRatio": in.OverallPressureRatio, "fanPressureRatio": in.FanPressureRatio, "bypassRatio": in.BypassRatio} {
			query("[name="+name+"]").Set("value", strconv.FormatFloat(value, 'g', -1, 64))
		}
	}
	update := func() {
		read := func(name string) float64 { return query("[name=" + name + "]").Get("valueAsNumber").Float() }
		in := cycle.Inputs{AltitudeM: read("altitudeM"), Mach: read("mach"), TurbineInletK: read("turbineInletK"),
			OverallPressureRatio: read("overallPressureRatio"), FanPressureRatio: read("fanPressureRatio"), BypassRatio: read("bypassRatio")}
		for name, text := range map[string]string{
			"altitudeM": fmt.Sprintf("%.1f km", in.AltitudeM/1000), "mach": fmt.Sprintf("M %.2f", in.Mach),
			"turbineInletK": fmt.Sprintf("%.0f K", in.TurbineInletK), "overallPressureRatio": fmt.Sprintf("%.0f : 1", in.OverallPressureRatio),
			"fanPressureRatio": fmt.Sprintf("%.2f : 1", in.FanPressureRatio), "bypassRatio": fmt.Sprintf("%.1f : 1", in.BypassRatio),
		} {
			query("[data-cycle-input-label="+name+"]").Set("textContent", text)
		}
		result, solveErr = m.Solve(in)
		view := cycle.Format(result, solveErr)
		outputs := root.Call("querySelectorAll", "[data-cycle-output]")
		for i := 0; i < outputs.Length(); i++ {
			node := outputs.Index(i)
			node.Set("textContent", view.Values[node.Call("getAttribute", "data-cycle-output").String()])
		}
		for _, row := range view.Stations {
			for name, value := range map[string]string{"temperature": row.T, "pressure": row.P, "flow": row.Flow} {
				query("[data-cycle-station='"+row.ID+"'] [data-quantity="+name+"]").Set("textContent", value)
			}
		}
		query("[data-cycle-plot]").Call("setAttribute", "d", view.TemperaturePath)
		query("[data-cycle-export]").Set("disabled", solveErr != nil)
		root.Call("setAttribute", "data-cycle-valid", strconv.FormatBool(solveErr == nil))
		// A transparent state snapshot also lets browser checks compare the
		// running WASM calculation with the native analytical benchmark.
		state := struct {
			Valid  bool         `json:"valid"`
			Result cycle.Result `json:"result"`
			Error  string       `json:"error,omitempty"`
		}{Valid: solveErr == nil, Result: result}
		if solveErr != nil {
			state.Error = solveErr.Error()
		}
		if data, err := json.Marshal(state); err == nil {
			root.Call("setAttribute", "data-cycle-state", string(data))
		}
	}
	initial := cycle.DefaultInputs()
	if err := ctx.DecodeProps(&initial); err != nil {
		return nil, err
	}
	setInputs(initial)
	input := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("target").Call("matches", "input[type=range]").Bool() {
			update()
		}
		return nil
	})
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		button := target.Call("closest", "[data-cycle-preset]")
		if button.Truthy() {
			in := result.Inputs
			switch button.Call("getAttribute", "data-cycle-preset").String() {
			case "static":
				in.AltitudeM, in.Mach = 0, 0
			case "cruise":
				in.AltitudeM, in.Mach = 11000, .78
			case "reset":
				in = cycle.DefaultInputs()
			}
			setInputs(in)
			update()
		}
		if target.Call("closest", "[data-cycle-export]").Truthy() && solveErr == nil {
			url := js.Global().Get("URL")
			if downloadURL != "" {
				url.Call("revokeObjectURL", downloadURL)
			}
			blob := js.Global().Get("Blob").New([]any{m.CSV(result)}, map[string]any{"type": "text/csv;charset=utf-8"})
			downloadURL = url.Call("createObjectURL", blob).String()
			a := js.Global().Get("document").Call("createElement", "a")
			a.Set("href", downloadURL)
			a.Set("download", "turbofan-design-point.csv")
			root.Call("appendChild", a)
			a.Call("click")
			a.Call("remove")
		}
		return nil
	})
	root.Call("addEventListener", "input", input)
	root.Call("addEventListener", "click", click)
	update()
	query("fieldset").Set("disabled", false)
	root.Call("setAttribute", "data-cycle-ready", "true")
	return wasm.HandleFunc(func() {
		root.Call("removeEventListener", "input", input)
		input.Release()
		root.Call("removeEventListener", "click", click)
		click.Release()
		query("fieldset").Set("disabled", true)
		root.Call("removeAttribute", "data-cycle-ready")
		if downloadURL != "" {
			js.Global().Get("URL").Call("revokeObjectURL", downloadURL)
		}
	}), nil
}
