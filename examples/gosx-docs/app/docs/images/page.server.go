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
					{"href": "#builtin", "label": "<Image> Builtin"},
					{"href": "#responsive", "label": "Responsive Images"},
					{"href": "#art-direction", "label": "Art Direction"},
					{"href": "#formats", "label": "Formats & Sizing"},
					{"href": "#serving", "label": "Serving"},
					{"href": "#caching", "label": "Caching"},
				},
				"imageSample": docsapp.DocSample("images/imageSample.go.sample"),
				// Width and Height are both set, at the source's own 8:5
				// aspect ratio (960x600) -- an example that omits Height
				// leaves the emitted <img> with no height attribute at
				// all, which reserves no layout box before the image
				// loads (layout-shift-prone; flagged in review).
				"responsiveSample":          docsapp.DocSample("images/responsiveSample.go.sample"),
				"artDirectionSample":        docsapp.DocSample("images/artDirectionSample.go.sample"),
				"builtinArtDirectionSample": docsapp.DocSample("images/builtinArtDirectionSample.gsx.sample"),
				"urlSample":                 docsapp.DocSample("images/urlSample.go.sample"),
				"builtinLocalSample":        docsapp.DocSample("images/builtinLocalSample.gsx.sample"),
				"builtinExternalSample":     docsapp.DocSample("images/builtinExternalSample.gsx.sample"),
				"liveImage": server.Image(server.ImageProps{
					Src:        "/checkers-native-preview.png",
					Alt:        "Native GoSX Chinese Checkers renderer preview",
					Width:      960,
					Height:     600,
					Responsive: true,
					Widths:     []int{320, 640, 960},
					Sizes:      "(max-width: 720px) 100vw, 720px",
					Quality:    82,
				}),
			}, nil
		},
	})
}
