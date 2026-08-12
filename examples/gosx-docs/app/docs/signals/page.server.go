package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Signals", "Go reactive values and the distinct signal subset compiled into browser islands.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Signals",
				"description": "Go reactive values and the distinct signal subset compiled into browser islands.",
				"tags":        []string{"signals", "derive", "watch", "batch", "islands"},
				"toc": []map[string]string{
					{"href": "#go-signals", "label": "Go Signals"},
					{"href": "#equality", "label": "Equality"},
					{"href": "#derived", "label": "Derived Values"},
					{"href": "#effects", "label": "Effects"},
					{"href": "#batching", "label": "Batching"},
					{"href": "#island-signals", "label": "Island Signals"},
				},
				"basicSample":  "count := signal.New(0)\n\nunsubscribe := count.Subscribe(func() {\n\tlog.Printf(\"count = %d\", count.Get())\n})\ndefer unsubscribe()\n\ncount.Set(1)\ncount.Update(func(value int) int { return value + 1 })\nrevision := count.Revision()",
				"equalSample":  "items := signal.NewWithEqual(\n\t[]string{\"one\"},\n\tfunc(a, b []string) bool { return slices.Equal(a, b) },\n)\n\n// Passing nil as the equality function notifies on every Set.\nalways := signal.NewWithEqual(map[string]int{}, nil)",
				"deriveSample": "first := signal.New(\"Ada\")\nlast := signal.New(\"Lovelace\")\nfull := signal.Derive(func() string {\n\treturn first.Get() + \" \" + last.Get()\n})\ndefer full.Stop()\n\nfmt.Println(full.Get())",
				"watchSample":  "effect := signal.Watch(func() {\n\tlog.Printf(\"total changed: %d\", total.Get())\n})\ndefer effect.Dispose()",
				"batchSample":  "signal.Batch(func() {\n\tfirst.Set(\"Grace\")\n\tlast.Set(\"Hopper\")\n})",
				"islandSample": "//gosx:island\nfunc Counter() Node {\n\tcount := signal.New(0)\n\ttheme := signal.NewShared(\"theme\", \"dark\")\n\tincrement := func() { count.Set(count.Get() + 1) }\n\treturn <button class={theme.Get()} onClick={increment}>\n\t\t{count.Get()}\n\t</button>\n}",
			}, nil
		},
	})
}
