//go:build js && wasm

// The cycle engine owns the workbench and its viewer controls. All computation
// is Go; typed Scene3D commands connect the shared cycle solver to the render.
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
	mappedModel, err := cycle.JT9D()
	if err != nil {
		return nil, err
	}
	initial := struct {
		cycle.Inputs
		Model  string             `json:"model"`
		Visual cycle.RenderState  `json:"visual"`
		Sweep  []cycle.SweepPoint `json:"sweep"`
	}{Inputs: cycle.DefaultInputs(), Model: "generic"}
	if err := ctx.DecodeProps(&initial); err != nil {
		return nil, err
	}
	renderer := newRenderLink(ctx, initial.Visual)
	thermalTint := initial.Visual.ThermalTint
	mode := "generic"
	var matched cycle.MappedResult
	var result cycle.Result
	var solveErr error
	var downloadURL string
	var engineering cycle.EngineeringRecord
	engineeringUI := bindEngineering(root)
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
		root.Call("setAttribute", "data-cycle-mode", mode)
		query("[name=altitudeM]").Set("disabled", mode == "mapped")
		query("[name=mach]").Set("disabled", mode == "mapped")
		query("[data-cycle-model=generic]").Call("setAttribute", "aria-pressed", strconv.FormatBool(mode == "generic"))
		query("[data-cycle-model=mapped]").Call("setAttribute", "aria-pressed", strconv.FormatBool(mode == "mapped"))
		fuel := read("fuelFlowKGS")
		query("[data-cycle-input-label=fuelFlowKGS]").Set("textContent", fmt.Sprintf("%.4f kg/s", fuel))
		for name, text := range map[string]string{
			"altitudeM": fmt.Sprintf("%.1f km", in.AltitudeM/1000), "mach": fmt.Sprintf("M %.2f", in.Mach),
			"turbineInletK": fmt.Sprintf("%.0f K", in.TurbineInletK), "overallPressureRatio": fmt.Sprintf("%.0f : 1", in.OverallPressureRatio),
			"fanPressureRatio": fmt.Sprintf("%.2f : 1", in.FanPressureRatio), "bypassRatio": fmt.Sprintf("%.1f : 1", in.BypassRatio),
		} {
			query("[data-cycle-input-label="+name+"]").Set("textContent", text)
		}
		result, solveErr = m.Solve(in)
		view := cycle.Format(result, solveErr)
		query("[data-lp-label]").Set("textContent", "Fan / LP shaft")
		if mode == "mapped" {
			matched, solveErr = mappedModel.Solve(cycle.MappedInputs{FuelFlowKGS: fuel})
			result = matched.Cycle
			view = cycle.FormatMapped(matched, solveErr)
			query("[data-lp-label]").Set("textContent", "Fan + booster / LP shaft")
		}
		var point *cycle.MappedResult
		if mode == "mapped" {
			point = &matched
		}
		engineering = cycle.Analyze(result, point, mappedModel, solveErr)
		if mode == "mapped" && solveErr == nil {
			engineering.SampleSensitivity(initial.Sweep, fuel)
		}
		engineeringUI.update(engineering, point, mappedModel, solveErr == nil)
		query("[data-engineering-export]").Set("disabled", solveErr != nil)
		query("[data-engineering-sweep-export]").Set("disabled", len(initial.Sweep) == 0)
		dots := root.Call("querySelectorAll", "[data-map-dot]")
		for i := 0; i < dots.Length(); i++ {
			dots.Index(i).Call("setAttribute", "visibility", "hidden")
		}
		if mode == "mapped" && solveErr == nil {
			for _, p := range matched.MapPoints {
				dot := query("[data-map-dot=" + p.Name + "]")
				if dot.Truthy() {
					x, y := mappedModel.PlotPoint(p)
					dot.Call("setAttribute", "cx", x)
					dot.Call("setAttribute", "cy", y)
					dot.Call("setAttribute", "visibility", "visible")
					dot.Call("setAttribute", "data-map-extended", strconv.FormatBool(p.Extended))
				}
			}
		}
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
			Mode    string              `json:"mode"`
			Matched *cycle.MappedResult `json:"matched,omitempty"`
			Valid   bool                `json:"valid"`
			Result  cycle.Result        `json:"result"`
			Error   string              `json:"error,omitempty"`
		}{Mode: mode, Valid: solveErr == nil, Result: result}
		if mode == "mapped" && solveErr == nil {
			state.Matched = &matched
		}
		if solveErr != nil {
			state.Error = solveErr.Error()
		}
		if data, err := json.Marshal(state); err == nil {
			root.Call("setAttribute", "data-cycle-state", string(data))
		}
		renderer.update(cycle.Visualize(mode, result, matched, solveErr).WithThermalTint(thermalTint), fuel)
	}
	if initial.Model == "mapped" {
		mode = "mapped"
	}
	setInputs(initial.Inputs)
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
		if target.Call("closest", "[data-engineering-expand],[data-engineering-collapse]").Truthy() {
			open := target.Call("closest", "[data-engineering-expand]").Truthy()
			details := root.Call("querySelectorAll", ".engineering-tables details")
			for i := 0; i < details.Length(); i++ {
				details.Index(i).Set("open", open)
			}
		}
		if target.Call("closest", "[data-thermal-toggle]").Truthy() {
			thermalTint = !thermalTint
			update()
			return nil
		}
		model := target.Call("closest", "[data-cycle-model]")
		if model.Truthy() {
			mode = model.Call("getAttribute", "data-cycle-model").String()
			setInputs(cycle.DefaultInputs())
			query("[name=fuelFlowKGS]").Set("value", "2.055")
			update()
		}
		lto := target.Call("closest", "[data-cycle-lto],[data-render-lto]")
		if lto.Truthy() {
			mode = "mapped"
			name := lto.Call("getAttribute", "data-cycle-lto")
			if name.IsNull() {
				name = lto.Call("getAttribute", "data-render-lto")
			}
			for _, p := range cycle.Reference().Points {
				if p.Mode == name.String() {
					query("[name=fuelFlowKGS]").Set("value", strconv.FormatFloat(p.FuelFlowKGS, 'g', -1, 64))
				}
			}
			update()
		}
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
		pointExport := target.Call("closest", "[data-cycle-export],[data-engineering-export]").Truthy()
		sweepExport := target.Call("closest", "[data-engineering-sweep-export]").Truthy()
		if pointExport && solveErr == nil || sweepExport && len(initial.Sweep) > 0 {
			url := js.Global().Get("URL")
			if downloadURL != "" {
				url.Call("revokeObjectURL", downloadURL)
			}
			csv := m.CSV(result)
			if mode == "mapped" {
				csv = mappedModel.CSV(matched)
			}
			filename := "turbofan-design-point.csv"
			if target.Call("closest", "[data-engineering-export]").Truthy() {
				csv += engineering.CSV()
				filename = "turbofan-engineering-point.csv"
			}
			if sweepExport {
				csv = cycle.SweepCSV(initial.Sweep)
				filename = "jt9d-model-operating-line.csv"
			}
			blob := js.Global().Get("Blob").New([]any{csv}, map[string]any{"type": "text/csv;charset=utf-8"})
			downloadURL = url.Call("createObjectURL", blob).String()
			a := js.Global().Get("document").Call("createElement", "a")
			a.Set("href", downloadURL)
			a.Set("download", filename)
			root.Call("appendChild", a)
			a.Call("click")
			a.Call("remove")
		}
		return nil
	})
	root.Call("addEventListener", "input", input)
	root.Call("addEventListener", "click", click)
	renderInput := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("target").Call("matches", "[data-render-fuel]").Bool() {
			query("[name=fuelFlowKGS]").Set("value", args[0].Get("target").Get("value"))
			update()
		}
		return nil
	})
	renderer.controls.Call("addEventListener", "input", renderInput)
	renderer.controls.Call("addEventListener", "click", click)
	update()
	query("fieldset").Set("disabled", false)
	for _, selector := range []string{"[data-engineering-expand]", "[data-engineering-collapse]"} {
		query(selector).Set("disabled", false)
	}
	root.Call("setAttribute", "data-cycle-ready", "true")
	return wasm.HandleFunc(func() {
		renderer.cancel()
		renderer.controls.Call("removeEventListener", "input", renderInput)
		renderer.controls.Call("removeEventListener", "click", click)
		renderInput.Release()
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
