package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Debugging Scene3D", "Diagnose invisible geometry, untrustworthy captures, and GPU compositor bugs with the tooling GoSX already ships.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"sample001":   docsapp.DocSample("debugging-scene3d/code-001.bash.sample"),
				"sample002":   docsapp.DocSample("debugging-scene3d/code-002.go.sample"),
				"sample003":   docsapp.DocSample("debugging-scene3d/code-003.js.sample"),
				"sample004":   docsapp.DocSample("debugging-scene3d/code-004.bash.sample"),
				"sample005":   docsapp.DocSample("debugging-scene3d/code-005.js.sample"),
				"sample006":   docsapp.DocSample("debugging-scene3d/code-006.text.sample"),
				"sample007":   docsapp.DocSample("debugging-scene3d/code-007.js.sample"),
				"sample008":   docsapp.DocSample("debugging-scene3d/code-008.js.sample"),
				"sample009":   docsapp.DocSample("debugging-scene3d/code-009.js.sample"),
				"sample010":   docsapp.DocSample("debugging-scene3d/code-010.gosx.sample"),
				"sample011":   docsapp.DocSample("debugging-scene3d/code-011.bash.sample"),
				"sample012":   docsapp.DocSample("debugging-scene3d/code-012.bash.sample"),
				"sample013":   docsapp.DocSample("debugging-scene3d/code-013.bash.sample"),
				"sample014":   docsapp.DocSample("debugging-scene3d/code-014.bash.sample"),
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
