package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

var strictComponentSample = docsapp.DocSample("components/strictSample.gosx.sample")

var legacyComponentSample = docsapp.DocSample("components/legacySample.gosx.sample")

var strictConcatSample = docsapp.DocSample("components/concatSample.gosx.sample")

var strictConditionalSample = docsapp.DocSample("components/conditionalSample.gosx.sample")

var strictEachSample = docsapp.DocSample("components/eachSample.gosx.sample")

var strictSpreadSample = docsapp.DocSample("components/spreadSample.gosx.sample")

func init() {
	docsapp.RegisterStaticDocsPage("Components", "Choose between strict typed and legacy Go-function GSX components.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Components",
				"description": "Choose between strict typed and legacy Go-function GSX components.",
				"tags":        []string{"components", "props", "strict", "tsx", "legacy"},
				"toc": []map[string]string{
					{"href": "#two-styles", "label": "Two Styles"},
					{"href": "#strict-components", "label": "Strict Components"},
					{"href": "#strict-expressions", "label": "Strict Expressions"},
					{"href": "#strict-loops-and-spread", "label": "Loops & Spread Props"},
					{"href": "#legacy-components", "label": "Legacy Components"},
					{"href": "#attributes", "label": "Elements & Attributes"},
					{"href": "#children", "label": "Children"},
					{"href": "#island-composition", "label": "Island Composition"},
					{"href": "#tooling", "label": "Tooling"},
					{"href": "#choosing", "label": "Choosing a Style"},
				},
				"strictSample":      strictComponentSample,
				"legacySample":      legacyComponentSample,
				"concatSample":      strictConcatSample,
				"conditionalSample": strictConditionalSample,
				"eachSample":        strictEachSample,
				"spreadSample":      strictSpreadSample,
				"attributesSample":  docsapp.DocSample("components/attributesSample.gosx.sample"),
				"commandsSample":    docsapp.DocSample("components/commandsSample.bash.sample"),
			}, nil
		},
	})
}
