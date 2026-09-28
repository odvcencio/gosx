package docs

import (
	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
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
					{"href": "#dom-motion", "label": "DOM Motion"},
					{"href": "#presets", "label": "Presets"},
					{"href": "#triggers", "label": "Triggers"},
					{"href": "#reduced-motion", "label": "Reduced Motion"},
					{"href": "#timing", "label": "Timing"},
					{"href": "#bootstrap", "label": "Bootstrap"},
				},
				"motionSample":  docsapp.DocSample("motion/motionSample.go.sample"),
				"reducedSample": docsapp.DocSample("motion/reducedSample.go.sample"),
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
