package app

import (
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"sync"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/examples/turbofan/cycle"
	"m31labs.dev/gosx/route"
)

var sweepOnce sync.Once
var sweepPoints []cycle.SweepPoint
var sweepCharts []cycle.SweepChart
var sweepError string
var publicValidationOnce sync.Once
var publicValidation cycle.PublicValidationReport

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			data := pageData(ctx.Request.URL.Query())
			data["cycleWASMPath"] = cycleProgramURL(filepath.Dir(filepath.Dir(page.FilePath)))
			return data, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}

// Resolve the manifest for both a source app and a relocated dist bundle.
// An unbuilt app can still render its calculated HTML fallback.
func cycleProgramURL(root string) string {
	for _, path := range []string{filepath.Join(root, "build.json"), filepath.Join(root, "dist", "build.json")} {
		if manifest, err := buildmanifest.Load(path); err == nil {
			if url := manifest.GoWASMURL("/gosx/assets", "cycle"); url != "" {
				return url
			}
		}
	}
	return "/gosx/assets/go-wasm/cycle.0000000000000000.wasm"
}

func pageData(query url.Values) map[string]any {
	period := query.Get("period")
	if period != "exterior" && period != "cutaway" {
		period = "cutaway"
		if query.Get("shell") == "full" {
			period = "exterior"
		}
	}
	options := Options{Exterior: period == "exterior", View: query.Get("view"), Backend: query.Get("backend")}
	if options.View != "side" && options.View != "intake" {
		options.View = "perspective"
	}
	inputs := cycle.DefaultInputs()
	result, err := cycle.DefaultModel().Solve(inputs)
	mapped, mapErr := cycle.JT9D()
	if mapErr != nil {
		panic(mapErr)
	}
	mode := "generic"
	display := cycle.Format(result, err)
	var matched cycle.MappedResult
	if query.Get("model") == "jt9d" {
		mode = "mapped"
		r, e := mapped.Solve(cycle.MappedInputs{FuelFlowKGS: 2.055})
		display = cycle.FormatMapped(r, e)
		matched, result, err = r, r.Cycle, e
	}
	visual := cycle.Visualize(mode, result, matched, err)
	var mappedPoint *cycle.MappedResult
	if mode == "mapped" {
		mappedPoint = &matched
	}
	diagnostics := cycle.Analyze(result, mappedPoint, mapped, err)
	sweepOnce.Do(func() {
		var e error
		sweepPoints, e = mapped.Sweep(81)
		if e != nil {
			sweepError = e.Error()
			return
		}
		sweepCharts = mapped.SweepCharts(sweepPoints)
	})
	if mode == "mapped" {
		diagnostics.SampleSensitivity(sweepPoints, result.FuelFlowKGS)
	}
	options.Visual = &visual
	publicValidationOnce.Do(func() { publicValidation = mapped.ValidatePublicData() })
	charts := append([]cycle.SweepChart(nil), sweepCharts...)
	current := cycle.SweepPoint{Fuel: 2.055, Thrust: mapped.Rated.NetThrustN, N1: mapped.ReferenceN1RPM, N2: mapped.ReferenceN2RPM, OPR: 23.4, TSFC: mapped.Rated.TSFC}
	if mode == "mapped" {
		current = cycle.SweepPoint{Fuel: matched.Command.FuelFlowKGS, Thrust: result.NetThrustN, N1: matched.N1RPM, N2: matched.N2RPM, OPR: result.Inputs.OverallPressureRatio, TSFC: result.TSFC}
	}
	for i := range charts {
		v, other := cycle.SweepValues(current, charts[i].Key, mapped)
		x, y := cycle.SweepPosition(current.Fuel, v, charts[i].MaxY)
		_, yy := cycle.SweepPosition(current.Fuel, other, charts[i].MaxY)
		charts[i].X, charts[i].Y, charts[i].SecondY = fmt.Sprintf("%.2f", x), fmt.Sprintf("%.2f", y), fmt.Sprintf("%.2f", yy)
	}
	props := struct {
		cycle.Inputs
		Model  string             `json:"model"`
		Visual cycle.RenderState  `json:"visual"`
		Sweep  []cycle.SweepPoint `json:"sweep"`
	}{inputs, mode, visual, sweepPoints}
	return map[string]any{
		"scene": Turbofan(options), "view": options.View, "period": period,
		"cycle": display, "inputs": inputs, "browserProps": props, "mode": mode,
		"visual":   visual,
		"mapPlots": mapped.Plots(), "mapChecks": mapped.Checks(),
		"diagnosticTables": cycle.DiagnosticTables(diagnostics),
		"sweepCharts":      charts, "sweepError": sweepError, "engineering": diagnostics,
		"publicLimits": cycle.PublicLimitViews(diagnostics), "publicGroups": cycle.PublicComparisonGroups(publicValidation), "publicSources": publicValidation.Sources,
		"engineeringAxes": map[string]string{"smin": fmt.Sprintf("%.2f", diagnostics.Numbers["plot/s-min.J_kgK"]/1000), "smax": fmt.Sprintf("%.2f", diagnostics.Numbers["plot/s-max.J_kgK"]/1000), "pmax": fmt.Sprintf("%.0f", diagnostics.Numbers["plot/p-max.1"])},
	}
}
