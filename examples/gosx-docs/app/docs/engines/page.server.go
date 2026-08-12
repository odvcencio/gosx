package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Engines", "Managed worker, surface, and video mounts with explicit browser capabilities.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
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
				"mountSample":      "raw, err := json.Marshal(ChartProps{Series: values})\nif err != nil {\n\treturn nil\n}\n\nreturn ctx.Engine(engine.Config{\n\tName:         \"Chart\",\n\tKind:         engine.KindSurface,\n\tMountID:      \"sales-chart\",\n\tProps:        raw,\n\tCapabilities: []engine.Capability{engine.CapCanvas, engine.CapPointer},\n\tMountAttrs: map[string]any{\n\t\t\"class\":      \"chart-surface\",\n\t\t\"aria-label\": \"Sales chart\",\n\t},\n}, gosx.El(\"p\", gosx.Text(\"Chart unavailable\")))",
				"webgpuSample":     "required := engine.RequireWebGPU(\n\tengine.CapWebGPUTimestampQuery,\n\tengine.WebGPULimit(\"maxComputeWorkgroupsPerDimension\", 256),\n)\n\ncfg := engine.Config{\n\tName:                 \"ParticleLab\",\n\tKind:                 engine.KindSurface,\n\tMountID:              \"particles\",\n\tCapabilities:         []engine.Capability{engine.CapCanvas, engine.CapAnimation},\n\tRequiredCapabilities: required,\n}",
				"wasmConfigSample": "cfg := engine.Config{\n\tName:     \"Chart\",\n\tKind:     engine.KindSurface,\n\tMountID:  \"chart\",\n\tRuntime:  engine.RuntimeGoWASM,\n\tWASMPath: \"/engines/chart.wasm\",\n}",
				"wasmModuleSample": "//go:build js && wasm\n\npackage main\n\nimport (\n\t\"m31labs.dev/gosx/engine/wasm\"\n)\n\nfunc main() {\n\terr := wasm.Register(\"Chart\", func(ctx wasm.Context) (wasm.Handle, error) {\n\t\tmount := ctx.Mount()\n\t\t_ = mount\n\t\treturn wasm.HandleFunc(func() {\n\t\t\t// Release listeners and browser resources here.\n\t\t}), nil\n\t})\n\tif err != nil {\n\t\tpanic(err)\n\t}\n\tselect {}\n}",
			}, nil
		},
	})
}
