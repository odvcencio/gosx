package app

import (
	"log"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// dashboardStats is a real Go type, buildable and importable the normal
// way — unlike app/page.gsx's PageProps, which is only ever compiled by the
// gosx toolchain, never by `go build`. It mirrors PageProps field for
// field, which is what the strict boundary actually proves at render time
// (structural field coverage, not this struct's own type identity — see
// ProgramRenderEnv.Props and TestStrictSpreadProps), so returning it here
// satisfies app/page.gsx's `component Page(props: PageProps)` (gosx#248).
type dashboardStats struct {
	Users   string
	Active  string
	Revenue string
	Growth  string
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return dashboardStats{
				Users:   "1,247",
				Active:  "892",
				Revenue: "$48,290",
				Growth:  "+12.5%",
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "Dashboard"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
