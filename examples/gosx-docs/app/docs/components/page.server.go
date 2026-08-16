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
	return <main><Badge label="Inbox" count={0} /></main>
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

const strictConcatSample = `package profile

type BadgeProps struct {
	Tone string
}

component Badge(props: BadgeProps) {
	return <span class={"badge tone-" + props.Tone}>
		{props.Tone}
	</span>
}

component Page() {
	return <main><Badge tone="alert" /></main>
}`

const strictConditionalSample = `package profile

type BadgeProps struct {
	Ready bool
}

component Badge(props: BadgeProps) {
	return <span>
		<If cond={props.Ready}>Ready</If>
		<If cond={props.Ready == false}>Pending</If>
	</span>
}

component Page() {
	return <main><Badge ready={true} /></main>
}`

const strictEachSample = `package profile

type Stat struct {
	Label string
	Value string
}

type CardProps struct {
	Stats []Stat
}

component Card(props: CardProps) {
	return <ul>
		<Each of={props.Stats} as="stat" index="i">
			<li>{i}: {stat.Label} = {stat.Value}</li>
		</Each>
	</ul>
}

func Page() Node {
	return <main><Card {...loaderCard} /></main>
}`

const strictSpreadSample = `package profile

type BadgeProps struct {
	Label string
	Count int
}

component Badge(props: BadgeProps) {
	return <span>{props.Label}: {props.Count}</span>
}

component Panel(props: BadgeProps) {
	return <div class="panel"><Badge {...props} /></div>
}

func Page() Node {
	return <main><Badge {...data.loaderRow} /></main>
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
					{"href": "#strict-expressions", "label": "Strict Expressions"},
					{"href": "#strict-loops-and-spread", "label": "Loops & Spread Props"},
					{"href": "#legacy-components", "label": "Legacy Components"},
					{"href": "#attributes", "label": "Elements & Attributes"},
					{"href": "#tooling", "label": "Tooling"},
					{"href": "#choosing", "label": "Choosing a Style"},
				},
				"strictSample":      strictComponentSample,
				"legacySample":      legacyComponentSample,
				"concatSample":      strictConcatSample,
				"conditionalSample": strictConditionalSample,
				"eachSample":        strictEachSample,
				"spreadSample":      strictSpreadSample,
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
gosx render app/page.gsx Page
gosx dev
gosx build .
gosx export .`,
			}, nil
		},
	})
}
