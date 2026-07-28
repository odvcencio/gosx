package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Debugging Scene3D", "Diagnose invisible geometry, untrustworthy captures, and GPU compositor bugs with the tooling GoSX already ships.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Debugging Scene3D",
				"description": "Diagnose invisible geometry, untrustworthy captures, and GPU compositor bugs with the tooling GoSX already ships.",
				"tags":        []string{"scene3d", "debug", "webgpu", "webgl", "telemetry", "visual-regression"},
				"toc": []map[string]string{
					{"href": "#quick-start", "label": "My Geometry Is Invisible"},
					{"href": "#cpu-reference-renderer", "label": "CPU Reference Renderer"},
					{"href": "#state-and-draw-recorder", "label": "State & Draw Recorder"},
					{"href": "#model-hydration-diagnostics", "label": "Model Hydration"},
					{"href": "#live-inspector", "label": "Live Inspector"},
					{"href": "#compositor-diagnostics", "label": "Compositor Diagnostics"},
					{"href": "#enforce-backend-in-captures", "label": "Enforce a Backend in Captures"},
					{"href": "#common-traps", "label": "Common Traps"},
				},
			}, nil
		},
	})
}
