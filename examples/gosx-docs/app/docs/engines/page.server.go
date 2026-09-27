package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Engines", "Managed worker, surface, and video mounts with explicit browser capabilities.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			config := engine.Config{
				Name:         "SearchWorker",
				Kind:         engine.KindWorker,
				Runtime:      engine.RuntimeNone,
				Capabilities: []engine.Capability{engine.CapWorker, engine.CapFetch},
			}
			validation := "Valid"
			if err := config.Validate(); err != nil {
				validation = "Invalid: " + err.Error()
			}
			return map[string]any{
				"engineExample": map[string]any{
					"name":         config.Name,
					"kind":         string(config.Kind),
					"runtime":      string(config.Runtime),
					"capabilities": "worker, fetch",
					"status":       validation,
				},
				"mode":        "",
				"title":       "Engines",
				"description": "Managed worker, surface, and video mounts with explicit browser capabilities.",
				"tags":        []string{"engines", "surface", "webgpu", "wasm", "capabilities"},
				"toc": []map[string]string{
					{"href": "#engine-model", "label": "Engine Model"},
					{"href": "#mounting", "label": "Mounting"},
					{"href": "#capabilities", "label": "Capabilities"},
					{"href": "#runtimes", "label": "Runtimes"},
					{"href": "#go-wasm", "label": "Go WASM"},
					{"href": "#lifecycle", "label": "Lifecycle"},
				},
				// mountSample is teaching text, rendered through CodeBlock on the
				// page. It shows a ctx.Engine call site, real Go code that would
				// live in a Load function. Its fallback argument uses gosx.El on
				// purpose: a one-node inline fallback is the idiom this same
				// package already uses in Go glue code, for example in
				// server.ScenePosterPreload.
				"engineSample":     docsapp.DocSample("engines/liveConfig.go.sample"),
				"mountSample":      docsapp.DocSample("engines/mountSample.go.sample"),
				"webgpuSample":     docsapp.DocSample("engines/webgpuSample.go.sample"),
				"wasmConfigSample": docsapp.DocSample("engines/wasmConfigSample.go.sample"),
				"wasmModuleSample": docsapp.DocSample("engines/wasmModuleSample.go.sample"),
			}, nil
		},
	})
}
