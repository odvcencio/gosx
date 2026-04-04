package docs

import (
	"encoding/json"
	"fmt"

	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("CMS Editor", "Block editor with drag reorder, inline editing, and unified publish.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"blocks": []map[string]string{
					{"type": "hero", "title": "Welcome to GoSX", "subtitle": "The Go-native web platform"},
					{"type": "feature", "title": "Server Rendering", "body": "Every page renders on the server first."},
					{"type": "quote", "text": "One language, full stack.", "author": "GoSX"},
				},
			}, nil
		},
		Actions: route.FileActions{
			"publish": func(ctx *action.Context) error {
				var blocks []map[string]string
				if err := json.Unmarshal(ctx.Payload, &blocks); err != nil {
					return action.Error(400, "Invalid block data")
				}
				return ctx.Success("Published "+fmt.Sprintf("%d", len(blocks))+" blocks", nil)
			},
		},
	})
}
