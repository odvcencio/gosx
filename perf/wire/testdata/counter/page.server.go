package counter

import (
	"log"

	"m31labs.dev/gosx/route"
)

type counterPropsData struct {
	Initial int
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{"counterProps": counterPropsData{Initial: 0}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
