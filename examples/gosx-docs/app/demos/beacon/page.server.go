package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	docsapp.RegisterDocsPage("Blackglass Beach", "A black sand beach at golden hour, generated in Go and rendered by GoSX Scene3D on WebGPU or WebGL2.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			docsapp.AddScenePosterPreload(ctx, "/demos/posters/beacon.webp")
			// This full-window scene uses ordinary links. Skip the 109 KB inline
			// docs navigation runtime; the scene bootstrap still owns its controls.
			ctx.SetNavigationHead(nil)
			ctx.AddHead(server.LifecycleScript("/blackglass-content.js"))
			view := blackglassBeachViewFor(ctx.Query("view"))
			period := blackglassBeachPeriodFor(ctx.Query("period"))
			return map[string]any{
				"scene": BlackglassBeachProgram(view.ID, period.ID),
				"view":  view.ID, "period": period.ID,
				"shoreHref":  "?view=shore&period=" + period.ID,
				"glassHref":  "?view=glass&period=" + period.ID,
				"cliffHref":  "?view=cliff&period=" + period.ID,
				"shipHref":   "?view=ship&period=" + period.ID,
				"goldenHref": "?view=" + view.ID + "&period=golden-hour",
				"blueHref":   "?view=" + view.ID + "&period=blue-hour",
				"noonHref":   "?view=" + view.ID + "&period=noon",
			}, nil
		},
	})
}
