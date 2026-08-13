package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	docsapp.RegisterStaticDocsPage("Images", "Local PNG, JPEG, and GIF resizing with responsive image markup.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Images",
				"description": "Local PNG, JPEG, and GIF resizing with responsive image markup.",
				"tags":        []string{"images", "resize", "responsive", "cache"},
				"toc": []map[string]string{
					{"href": "#helper", "label": "Image Helper"},
					{"href": "#responsive", "label": "Responsive Images"},
					{"href": "#formats", "label": "Formats & Sizing"},
					{"href": "#serving", "label": "Serving"},
					{"href": "#caching", "label": "Caching"},
				},
				"imageSample":      "node := server.Image(server.ImageProps{\n\tSrc:      \"/photos/harbor.jpg\",\n\tAlt:      \"Harbor at dusk\",\n\tWidth:    1200,\n\tHeight:   800,\n\tQuality:  82,\n\tPriority: true,\n})",
				"responsiveSample": "node := server.Image(server.ImageProps{\n\tSrc:        \"/photos/harbor.jpg\",\n\tAlt:        \"Harbor at dusk\",\n\tResponsive: true,\n\tWidth:      1200,\n\tWidths:     []int{320, 640, 960, 1200},\n\tSizes:      \"(max-width: 720px) 100vw, 720px\",\n})",
				"urlSample":        "url := server.ImageURL(\"/photos/harbor.jpg\", server.ImageTransform{\n\tWidth:   640,\n\tQuality: 78,\n\tFormat:  \"jpeg\",\n})",
				"liveImage": server.Image(server.ImageProps{
					Src:        "/checkers-native-preview.png",
					Alt:        "Native GoSX Chinese Checkers renderer preview",
					Width:      960,
					Responsive: true,
					Widths:     []int{320, 640, 960},
					Sizes:      "(max-width: 720px) 100vw, 720px",
					Quality:    82,
				}),
			}, nil
		},
	})
}
