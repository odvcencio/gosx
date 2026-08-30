package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Compiler", "How GoSX parses, validates, lowers, checks, and renders GSX source.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Compiler",
				"description": "How GoSX parses, validates, lowers, checks, and renders GSX source.",
				"tags":        []string{"compiler", "gsx", "strict", "ir", "tree-sitter"},
				"toc": []map[string]string{
					{"href": "#source-model", "label": "Source Model"},
					{"href": "#strict-validation", "label": "Strict Validation"},
					{"href": "#pipeline", "label": "Pipeline"},
					{"href": "#expressions", "label": "Expressions"},
					{"href": "#islands", "label": "Island Lowering"},
					{"href": "#commands", "label": "Commands"},
					{"href": "#lsp", "label": "LSP Boundary"},
				},
				"strictSample": `package cards

type CardProps struct {
	Title string
	Count int
}

component Card(props: CardProps) {
	return <article className="card">
		<h2>{props.Title}</h2>
		<span>{props.Count}</span>
	</article>
}

component Page() {
	return <main><Card Title="Queue" Count={2} /></main>
}`,
				"legacySample": `package cards

func Page() Node {
	return <main>
		<h1>{data.title}</h1>
		<Each of={data.cards} as="card">
			<article>{card.title}</article>
		</Each>
	</main>
}`,
				"programSample": `type Program struct {
	Package    string
	Imports    []Import
	Components []Component
	Nodes      []Node
}

type Component struct {
	Name      string
	PropsType string
	Root      NodeID
	Syntax    ComponentSyntax
}`,
				"islandSample": `package counter

//gosx:island
component Counter() {
	count := signal.New(0)
	return <button data-on-click="count.Set(count.Get() + 1)">
		{count.Get()}
	</button>
}`,
				"commandsSample": `gosx check app/page.gsx
gosx render app/page.gsx Page
gosx compile app/page.gsx
gosx fmt --check app/page.gsx
gosx build .`,
			}, nil
		},
	})
}
