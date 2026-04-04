package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Engines", "Dedicated compute surfaces for canvas, WebGL, WebGPU, and web workers.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Engines",
				"description": "Dedicated compute surfaces for canvas, WebGL, WebGPU, and web workers.",
				"tags":        []string{"engines", "canvas", "webgl", "webgpu", "workers", "compute"},
				"toc": []map[string]string{
					{"href": "#engine-model", "label": "Engine Model"},
					{"href": "#capability-tiers", "label": "Capability Tiers"},
					{"href": "#canvas-surface", "label": "Canvas Surface"},
					{"href": "#webgl-webgpu", "label": "WebGL / WebGPU"},
					{"href": "#workers", "label": "Workers"},
					{"href": "#engine-programs", "label": "Engine Programs"},
				},
			}, nil
		},
	})
}
