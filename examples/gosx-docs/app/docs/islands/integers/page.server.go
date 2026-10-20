package integers

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

type IntegerPropsData struct{ Initial int64 }

func init() {
	docsapp.RegisterDocsPage("Exact Island Integers", "Signed 64-bit state that survives server rendering and browser hydration.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode": "", "title": "Exact Island Integers",
				"description":   "Keep large integer IDs and counters exact in the shared GoSX island VM.",
				"tags":          []string{"islands", "signals", "integers"},
				"integerProps":  IntegerPropsData{Initial: 9007199254740993},
				"integerSample": docsapp.DocSample("islands/integerSample.gosx.sample"),
				"toc":           []map[string]string{{"href": "#integer-example", "label": "Exact Counter"}, {"href": "#integer-source", "label": "Source"}},
			}, nil
		},
	})
}
