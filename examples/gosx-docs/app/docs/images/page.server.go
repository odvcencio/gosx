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
				"imageSample": "node := server.Image(server.ImageProps{\n\tSrc:      \"/photos/harbor.jpg\",\n\tAlt:      \"Harbor at dusk\",\n\tWidth:    1200,\n\tHeight:   800,\n\tQuality:  82,\n\tPriority: true,\n})",
				// Width and Height are both set, at the source's own 8:5
				// aspect ratio (960x600) -- an example that omits Height
				// leaves the emitted <img> with no height attribute at
				// all, which reserves no layout box before the image
				// loads (layout-shift-prone; flagged in review).
				"responsiveSample":   "node := server.Image(server.ImageProps{\n\tSrc:        \"/photos/harbor.jpg\",\n\tAlt:        \"Harbor at dusk\",\n\tResponsive: true,\n\tWidth:      1200,\n\tHeight:     750,\n\tWidths:     []int{320, 640, 960, 1200},\n\tSizes:      \"(max-width: 720px) 100vw, 720px\",\n})",
				"artDirectionSample": "node := server.Image(server.ImageProps{\n\tSrc:    \"https://images.example.com/hero-wide.jpg\",\n\tAlt:    \"Potter shaping clay at the wheel\",\n\tWidth:  1600,\n\tHeight: 900,\n\tSources: []server.ImageSource{\n\t\t{\n\t\t\tMedia:  \"(max-width: 600px)\",\n\t\t\tSrcSet: \"https://images.example.com/hero-tight-480.jpg 480w, https://images.example.com/hero-tight-800.jpg 800w\",\n\t\t\tSizes:  \"100vw\",\n\t\t\tWidth:  800,\n\t\t\tHeight: 1000,\n\t\t},\n\t},\n\tPictureAttrs: gosx.Attrs(\n\t\tgosx.Attr(\"class\", \"responsive-picture\"),\n\t),\n})",
				"builtinArtDirectionSample": `<Image
	src={data.hero}
	alt="Potter shaping clay at the wheel"
	width={1600}
	height={900}
	sources={data.sources}
	pictureAttrs={data.pictureAttrs}
	class="hero-image"
/>`,
				"urlSample":             "url := server.ImageURL(\"/photos/harbor.jpg\", server.ImageTransform{\n\tWidth:   640,\n\tQuality: 78,\n\tFormat:  \"jpeg\",\n})",
				"builtinLocalSample":    `<Image src="/photos/harbor.jpg" alt="Harbor at dusk" />`,
				"builtinExternalSample": `<Image src="https://cdn.example.com/harbor.jpg" alt="Harbor at dusk" width={1200} height={800} />`,
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
