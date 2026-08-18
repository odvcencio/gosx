// Package counter renders /counter, a server-side counter demonstration.
// Each button click does a full page navigation (server-first, no client
// JS): the count lives in the "count" query parameter, and Load reads it
// fresh on every request.
package counter

import (
	"fmt"
	"log"
	"strconv"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			count, _ := strconv.Atoi(ctx.Query("count"))
			return map[string]any{
				"count":    count,
				"prevHref": fmt.Sprintf("/counter?count=%d", count-1),
				"nextHref": fmt.Sprintf("/counter?count=%d", count+1),
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Default: "GoSX Example - Counter"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
