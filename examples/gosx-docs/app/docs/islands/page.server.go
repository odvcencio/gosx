package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Islands", "Explicit interactive regions compiled for the shared GoSX browser VM.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Islands",
				"description": "Explicit interactive regions compiled for the shared GoSX browser VM.",
				"tags":        []string{"islands", "signals", "handlers", "hydration", "vm"},
				"toc": []map[string]string{
					{"href": "#island-model", "label": "Island Model"},
					{"href": "#authoring", "label": "Authoring"},
					{"href": "#vm-subset", "label": "VM Subset"},
					{"href": "#shared-signals", "label": "Shared Signals"},
					{"href": "#program-assets", "label": "Program Assets"},
					{"href": "#choosing", "label": "Choosing Islands"},
				},
				"counterSample": "package counter\n\n//gosx:island\ncomponent Counter() {\n\tcount := signal.New(0)\n\tincrement := func() { count.Set(count.Get() + 1) }\n\tdecrement := func() { count.Set(count.Get() - 1) }\n\treturn <div class=\"counter\">\n\t\t<button onClick={decrement}>-</button>\n\t\t<span>{count.Get()}</span>\n\t\t<button onClick={increment}>+</button>\n\t</div>\n}",
				"sharedSample":  "//gosx:island\ncomponent ThemeSwitch() {\n\ttheme := signal.NewShared(\"theme\", \"dark\")\n\ttoggle := func() {\n\t\ttheme.Set(theme.Get() == \"dark\" ? \"light\" : \"dark\")\n\t}\n\treturn <button onClick={toggle}>{theme.Get()}</button>\n}",
				"assetSample":   "node := ctx.Runtime().IslandWithProgramAsset(\n\tcounterProgram,\n\tmap[string]int{\"initial\": 2},\n\t\"/islands/Counter.bin\",\n\t\"bin\",\n\tcounterHash,\n)",
			}, nil
		},
	})
}
