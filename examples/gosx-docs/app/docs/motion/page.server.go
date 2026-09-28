package docs

import (
	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	docsapp.RegisterDocsPage("Motion", "Server-authored motion presets with reduced-motion awareness.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			// Demo diagnostics: shows the demo scene's renderer and GPU adapter.
			// It is page-scoped, so it ships only on this route.
			ctx.AddHead(server.LifecycleScript("/motion-adapter-report.js"))
			program, err := motionDemoProgram()
			if err != nil {
				return nil, err
			}
			encoded, err := program.Marshal()
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"mode":        "light",
				"title":       "Motion",
				"description": "Server-authored motion presets with reduced-motion awareness.",
				"tags":        []string{"animation", "motion", "transitions", "reduced-motion"},
				"toc": []map[string]string{
					{"href": "#dom-motion", "label": "DOM motion"},
					{"href": "#presets", "label": "Presets"},
					{"href": "#triggers", "label": "Triggers"},
					{"href": "#reduced-motion", "label": "Reduced motion"},
					{"href": "#timing", "label": "Timing"},
					{"href": "#one-program", "label": "Shared motion"},
					{"href": "#bootstrap", "label": "Bootstrap"},
				},
				"motionSample":  docsapp.DocSample("motion/motionSample.go.sample"),
				"programSample": docsapp.DocSample("motion/programSample.go.sample"),
				"reducedSample": docsapp.DocSample("motion/reducedSample.go.sample"),
				"motionProgram": string(encoded),
			}, nil
		},
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			var motionExample gosx.Node = gosx.Text("")
			if ctx != nil {
				motionExample = ctx.Runtime().Motion(server.MotionProps{
					Tag: "div", Preset: server.MotionPresetSlideUp, Trigger: server.MotionTriggerLoad,
					Duration: 260,
				}, gosx.Attrs(gosx.Attr("class", "motion-demo-card")), gosx.Text("This card uses a server-authored slide-up preset."))
			}
			return route.FileTemplateBindings{Values: map[string]any{"motionExample": motionExample}}
		},
	})
}

func motionDemoProgram() (*motion.Program, error) {
	program := motion.NewProgram("docs-motion")
	scroll := program.ScrollProgress("page-scroll", "", motion.AxisY)
	cardY := program.Map("card-y", scroll, 0, 1, 0, -26)
	cameraZ := program.Map("camera-z", scroll, 0, 1, 8.4, 6.4)
	program.BindCSSVariable(cardY, "#motion-card", "--motion-card-y", "px")
	program.BindCSSVariable(scroll, "#doc-motion-surface", "--motion-progress", "")
	program.BindCamera(cameraZ, "#motion-scene", "position.z")

	hover := program.Hover("button-hover", "#motion-hover")
	lift := program.Spring("hover-spring", 0, motion.SpringOptions{
		Input:   hover,
		Physics: motion.Spring{Stiffness: 280, Damping: 23},
	})
	scale := program.Map("hover-scale", lift, 0, 1, 1, 1.38)
	for _, axis := range []string{"x", "y", "z"} {
		program.BindSceneNode(scale, "#motion-scene", "doc-motion-node-hover-node", "scale."+axis)
	}
	program.PinTo("#motion-scene", "doc-motion-node-pinned-node", "#motion-pin-anchor")
	return program, nil
}
