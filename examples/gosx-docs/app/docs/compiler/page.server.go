package docs

import (
	docs "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Compiler", "How GSX source compiles through tree-sitter parsing, IR lowering, and validation.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Compiler",
				"description": "How GSX source compiles through tree-sitter parsing, IR lowering, and validation.",
				"tags":        []string{"compiler", "gsx", "ir", "tree-sitter", "gotreesitter"},
				"toc": []map[string]string{
					{"href": "#gsx-syntax", "label": "GSX Syntax"},
					{"href": "#parsing", "label": "Parsing"},
					{"href": "#ir-lowering", "label": "IR Lowering"},
					{"href": "#validation", "label": "Validation"},
					{"href": "#expression-evaluation", "label": "Expression Evaluation"},
					{"href": "#island-compilation", "label": "Island Compilation"},
				},
				"sampleSyntax": `package docs

func Page() Node {
	return <div class="prose">
		<h1>{data.title}</h1>
		<Each of={data.items} as="item">
			<p>{item.label}</p>
		</Each>
		<If cond={data.showMore}>
			<a href="/more">Read more</a>
		</If>
	</div>
}`,
				"sampleCompile": `// gosx.Compile is the public entrypoint.
// It calls Parse then lower to produce an IR program.
func Compile(source []byte) (*ir.Program, error) {
	tree, lang, err := Parse(source)
	if err != nil {
		return nil, err
	}
	return lower(tree.RootNode(), lang, source)
}`,
				"sampleIR": `// Simplified IR instruction set.
type Op uint8

const (
	OpPushElement  Op = iota // push element onto stack
	OpPopElement             // pop element from stack
	OpSetAttr                // set attribute on TOS element
	OpPushText               // push literal text child
	OpPushExpr               // push expression child (evaluated at render)
	OpCallComponent          // call named component, result is child
	OpEach                   // begin each loop
	OpIf                     // conditional branch
	OpSlot                   // insert slot content
)

type Instruction struct {
	Op      Op
	Operand string // element tag, attr name, text value, expr source, …
}`,
				"sampleEval": `// Expression evaluation uses the data map from the Load function.
// The map key "data" always refers to the route loader return value.
//
// Expression: {data.title}
// Resolves as: data["title"].(string)
//
// Expression: {data.items[0].label}
// Resolves as: data["items"].([]any)[0].(map[string]any)["label"].(string)
//
// Expression: {if data.mode == "dark" { "dark-class" } else { "" }}
// Resolves as a conditional string at render time.`,
				"sampleIslandGSX": `// Island expressions are compiled to VM opcodes, not shipped as source.
// The compiler enforces that only the island expression subset is used.
func IslandWidget() Node {
	return <Island>
		<span class={if data.active { "badge active" } else { "badge" }}>
			{data.count}
		</span>
	</Island>
}`,
				"sampleIslandOps": `// Island VM opcodes (subset shown).
const (
	IslandOpPushSignal   = 0x01 // push named signal value
	IslandOpPushLiteral  = 0x02 // push string literal
	IslandOpCondStr      = 0x10 // conditional branch → string result
	IslandOpAdd          = 0x20 // string or numeric add
	IslandOpEq           = 0x30 // equality test
)`,
			}, nil
		},
	})
}
