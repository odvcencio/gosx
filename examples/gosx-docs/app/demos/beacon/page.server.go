package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Blackglass Coast", "A Studio-authored volcanic cove bound to GoSX Scene3D water, gameplay anchors, and performance telemetry.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			view := blackglassViewID(ctx.Query("view"))
			period := blackglassPeriodFor(ctx.Query("period"))
			data := map[string]any{
				"scene": BlackglassCoastProgram(view, period.ID),
				"view":  view, "viewName": blackglassViewName(view),
				"period": period.ID, "periodName": period.Name,
			}
			for key, value := range blackglassPageLinks(view, period.ID) {
				data[key] = value
			}
			return data, nil
		},
	})
}
