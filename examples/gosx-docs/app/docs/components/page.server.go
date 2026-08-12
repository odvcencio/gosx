package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

const strictComponentSample = `package profile

type BadgeProps struct {
	Label string
	Count int
}

component Badge(props: BadgeProps) {
	return <span className="badge">
		{props.Label}: {props.Count}
	</span>
}

component Page() {
	return <main><Badge Label="Inbox" Count={3} /></main>
}`

const legacyComponentSample = `package profile

func Page() Node {
	return <main>
		<h1>{data.title}</h1>
		<If cond={data.showInbox}>
			<a href="/inbox">Open inbox</a>
		</If>
	</main>
}`

func init() {
	docs.RegisterStaticDocsPage("Components", "Choose between strict typed and legacy Go-function GSX components.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Components",
				"description": "Choose between strict typed and legacy Go-function GSX components.",
				"tags":        []string{"components", "props", "strict", "tsx", "legacy"},
				"toc": []map[string]string{
					{"href": "#two-styles", "label": "Two Styles"},
					{"href": "#strict-components", "label": "Strict Components"},
					{"href": "#legacy-components", "label": "Legacy Components"},
					{"href": "#attributes", "label": "Elements & Attributes"},
					{"href": "#tooling", "label": "Tooling"},
					{"href": "#choosing", "label": "Choosing a Style"},
				},
				"strictSample": strictComponentSample,
				"legacySample": legacyComponentSample,
				"attributesSample": `type FieldProps struct {
	ID    string
	Class string
	Label string
}

component Field(props: FieldProps) {
	return <label className={props.Class} htmlFor={props.ID}>
		{props.Label}
		<input id={props.ID} required />
	</label>
}`,
				"commandsSample": `gosx check app/page.gsx
gosx render app/page.gsx
gosx dev
gosx build .
gosx export .`,
			}, nil
		},
	})
}
