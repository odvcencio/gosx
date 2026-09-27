package docs

import (
	"fmt"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

func init() {
	docsapp.RegisterDocsPage("Debugging Scene3D", "Diagnose invisible geometry, untrustworthy captures, and GPU compositor bugs with the tooling GoSX already ships.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"debugReport": debuggingSceneReport(),
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

func debuggingSceneReport() map[string]any {
	props := scene.Props{
		Background: "#111318",
		Camera:     scene.PerspectiveCamera{Position: scene.Vec3(0, 0.2, 4), FOV: 45},
		Graph: scene.NewGraph(
			scene.AmbientLight{Color: "#d8e2f0", Intensity: 0.3},
			scene.DirectionalLight{ID: "debug-key", Color: "#fff2d5", Intensity: 1.8, Direction: scene.Vec3(-0.4, -0.8, -0.6)},
			scene.Mesh{
				ID:       "debug-sphere",
				Geometry: scene.SphereGeometry{Radius: 0.9, Segments: 32},
				Material: scene.StandardMaterial{Color: "#d4af37", Roughness: 0.35},
			},
		),
	}
	session := harness.New(props, preview.Options{Width: 240, Height: 150, DisableShadows: true, DisablePostFX: true})
	if _, err := session.Render(0); err != nil {
		return map[string]any{"status": "Render failed: " + err.Error()}
	}
	if err := session.Validate(); err != nil {
		return map[string]any{"status": "Check failed: " + err.Error()}
	}
	report := session.Report()
	frame := report.Events[0].Frame
	if frame == nil || frame.VisibleBounds == nil {
		return map[string]any{"status": "No visible pixels were recorded"}
	}
	bounds := frame.VisibleBounds
	return map[string]any{
		"status":        "Passed",
		"backend":       report.Backend,
		"objects":       fmt.Sprint(report.Scene.Objects),
		"lights":        fmt.Sprint(report.Scene.Lights),
		"coverage":      fmt.Sprintf("%.1f%%", frame.Coverage*100),
		"uniqueColors":  fmt.Sprint(frame.UniqueColors),
		"visibleBounds": fmt.Sprintf("x %d–%d, y %d–%d", bounds.MinX, bounds.MaxX, bounds.MinY, bounds.MaxY),
	}
}
