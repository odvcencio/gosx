package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Images",
		"Server-side image optimization with responsive resizing and format conversion.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "light",
					"title":       "Images",
					"description": "Server-side image optimization with responsive resizing and format conversion.",
					"tags":        []string{"images", "optimization", "responsive"},
					"toc": []map[string]string{
						{"href": "#image-optimization", "label": "Image Optimization"},
						{"href": "#responsive-images", "label": "Responsive Images"},
						{"href": "#format-conversion", "label": "Format Conversion"},
						{"href": "#caching", "label": "Caching"},
					},
				}, nil
			},
		},
	)
}
