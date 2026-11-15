package app

import (
	"log"
	"time"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// PageData is the loader's result. A strict page reads it as props: the field
// names match PageProps in page.gsx.
type PageData struct {
	ServerTime string
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return PageData{ServerTime: time.Now().Format(time.RFC3339)}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Default: "GoSX Example - Home"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
