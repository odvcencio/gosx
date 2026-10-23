package docs

import (
	"strconv"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

const voyagePosterSrc = "/demos/posters/voyage.webp"

func init() {
	docsapp.RegisterDocsPage("Voyage", "An expedition ship under way on a spectral ocean, with four sea states and five game speeds, all declared in Go.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			docsapp.AddScenePosterPreload(ctx, voyagePosterSrc)
			ctx.SetNavigationHead(nil)
			view := voyageViewFor(ctx.Query("view"))
			state, speed := voyageParsePeriod(ctx.Query("period"))
			period := voyagePeriodID(state, speed)
			return map[string]any{
				"scene": VoyageProgram(view.ID, period), "posterSrc": voyagePosterSrc,
				"view": view.ID, "state": state, "speed": speed, "speedID": strconv.Itoa(speed) + "x", "period": period,
				"apparent": VoyageApparentWaterSpeed(speed),
			}, nil
		},
	})
}
