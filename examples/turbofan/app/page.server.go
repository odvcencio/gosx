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
	options := Options{Exterior: query.Get("shell") == "full", Paused: query.Get("motion") == "paused", View: query.Get("view"), Backend: query.Get("backend")}
	if options.View != "side" && options.View != "intake" {
		options.View = "perspective"
	}
	// Only retain recognized presentation parameters in navigation links.
	q := url.Values{"view": {options.View}}
	if options.Exterior {
		q.Set("shell", "full")
	}
	if options.Paused {
		q.Set("motion", "paused")
	}
	if options.Backend == "webgl" || options.Backend == "webgpu" {
		q.Set("backend", options.Backend)
	}
	link := func(key, value string) string {
		copy := url.Values{}
		for k, v := range q {
			copy[k] = append([]string(nil), v...)
		}
		copy.Set(key, value)
		return "/?" + copy.Encode()
	}
	motionLabel, motionTarget, state := "Pause rotation", "paused", "SLOW MOTION"
	if options.Paused {
		motionLabel, motionTarget, state = "Resume rotation", "running", "ROTATION PAUSED"
	}
	return map[string]any{
		"scene": Turbofan(options), "state": state,
		"motionLabel": motionLabel, "motionURL": link("motion", motionTarget),
		"cutawayURL": link("shell", "cutaway"), "exteriorURL": link("shell", "full"),
		"cutawayActive": !options.Exterior, "exteriorActive": options.Exterior,
		"perspectiveURL": link("view", "perspective"), "sideURL": link("view", "side"), "intakeURL": link("view", "intake"),
		"perspectiveActive": options.View == "perspective", "sideActive": options.View == "side", "intakeActive": options.View == "intake",
	}
}
