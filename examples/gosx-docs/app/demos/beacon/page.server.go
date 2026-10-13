package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

const (
	blackglassDiscoveriesScriptSrc = "/blackglass-discoveries.js?v=3b15c7d5725e"
	blackglassChartScriptSrc       = "/blackglass-chart.js?v=d34e26bb4429"
	blackglassDiscoveriesStylesSrc = "/blackglass-discoveries.css?v=4f268dde7de5"
)

func init() {
	docsapp.RegisterDocsPage("Blackglass Beach", "A black sand beach at golden hour, generated in Go and rendered by GoSX Scene3D on WebGPU or WebGL2.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			docsapp.AddScenePosterPreload(ctx, blackglassBeachPosterSrc)
			// This full-window scene uses ordinary links. Skip the 109 KB inline
			// docs navigation runtime; the scene bootstrap still owns its controls.
			ctx.SetNavigationHead(nil)
			ctx.AddHead(server.LifecycleScript("/blackglass-content.js"))
			ctx.AddHead(server.LifecycleScript(blackglassDiscoveriesScriptSrc))
			ctx.AddHead(server.LifecycleScript(blackglassChartScriptSrc))
			ctx.AddHead(server.Stylesheet(blackglassDiscoveriesStylesSrc))
			view := blackglassBeachViewFor(ctx.Query("view"))
			period := blackglassBeachPeriodFor(ctx.Query("period"))
			instructions := "Click or drag to look · WASD or arrows to walk · Shift to run · Esc to release"
			touchInstructions := "Left thumb to move · drag the right side to look"
			if view.ID == "glass" || view.ID == "cliff" || view.ID == "pools" {
				instructions = "Drag or use arrows to orbit · scroll or +/− to zoom · Home to reset"
				touchInstructions = "Drag to orbit · pinch to zoom"
			}
			return map[string]any{
				"instructions": instructions, "touchInstructions": touchInstructions,
				"scene": BlackglassBeachProgram(view.ID, period.ID), "posterSrc": blackglassBeachPosterSrc,
				"view": view.ID, "period": period.ID,
				"pickupCommands": blackglassHandheldPickupCommands(),
				"chartShore":     blackglassBeachChart(), "chartPlaces": blackglassBeachChartPlaces(), "poolRippleSites": blackglassBeachPoolRippleSites(),
				"shoreHref":  "?view=shore&period=" + period.ID,
				"glassHref":  "?view=glass&period=" + period.ID,
				"cliffHref":  "?view=cliff&period=" + period.ID,
				"poolsHref":  "?view=pools&period=" + period.ID,
				"shipHref":   "?view=ship&period=" + period.ID,
				"goldenHref": "?view=" + view.ID + "&period=golden-hour",
				"blueHref":   "?view=" + view.ID + "&period=blue-hour",
				"noonHref":   "?view=" + view.ID + "&period=noon",
			}, nil
		},
	})
}
