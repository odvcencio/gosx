package app

import (
	"log"
	"net/url"

	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, _ route.FilePage) (any, error) {
			return pageData(ctx.Request.URL.Query()), nil
		},
	}); err != nil {
		log.Fatal(err)
	}
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
	return map[string]any{
		"scene": Turbofan(options), "view": options.View, "period": period,
	}
}
