package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Motion", "Server-authored motion presets with reduced-motion awareness.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Motion",
				"description": "Server-authored motion presets with reduced-motion awareness.",
				"tags":        []string{"animation", "motion", "transitions", "reduced-motion"},
				"toc": []map[string]string{
					{"href": "#motion-presets", "label": "Motion Presets"},
					{"href": "#viewport-triggers", "label": "Viewport Triggers"},
					{"href": "#reduced-motion", "label": "Reduced Motion"},
					{"href": "#custom-timing", "label": "Custom Timing"},
				},
			}, nil
		},
	})
}
