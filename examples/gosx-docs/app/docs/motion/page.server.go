package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
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
				"motionSample":  "node := ctx.Motion(server.MotionProps{\n\tTag:      \"article\",\n\tPreset:   server.MotionPresetSlideUp,\n\tTrigger:  server.MotionTriggerView,\n\tDuration: 260,\n\tDelay:    40,\n},\n\tgosx.Attrs(gosx.Attr(\"class\", \"result-card\")),\n\tgosx.Text(\"Ready\"),\n)",
				"reducedSample": "respect := false\nnode := ctx.Motion(server.MotionProps{\n\tPreset:               server.MotionPresetFade,\n\tRespectReducedMotion: &respect,\n}, content)",
			}, nil
		},
	})
}
