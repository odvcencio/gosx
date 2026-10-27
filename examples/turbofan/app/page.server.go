package app

import (
	"log"
	"net/url"
	"path/filepath"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/examples/turbofan/cycle"
	"m31labs.dev/gosx/route"
)

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
	options.Visual = &visual
	props := struct {
		cycle.Inputs
		Model  string            `json:"model"`
		Visual cycle.RenderState `json:"visual"`
	}{inputs, mode, visual}
	return map[string]any{
		"scene": Turbofan(options), "view": options.View, "period": period,
		"cycle": display, "inputs": inputs, "browserProps": props, "mode": mode,
		"visual":   visual,
		"mapPlots": mapped.Plots(), "mapChecks": mapped.Checks(),
	}
}
