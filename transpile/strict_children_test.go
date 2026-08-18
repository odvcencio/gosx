package transpile

import (
	"strings"
	"testing"
)

// TestStrictComponentSignatureIsAlwaysVariadic pins B4's decision. transpile
// walks the CST with no ir.Program in hand, so emitting the parameter only
// for a body that renders children would need a second implementation of
// ir.Component.AcceptsChildren's predicate, and two implementations of one
// predicate drift. A variadic parameter accepts zero arguments, so every
// call site that passes none keeps compiling.
func TestStrictComponentSignatureIsAlwaysVariadic(t *testing.T) {
	tests := map[string]struct {
		source string
		want   string
	}{
		"with props and children": {
			source: `package app
type PanelProps struct { Title string }
component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}
`,
			want: `func Panel(props PanelProps, children ...gosx.Node) gosx.Node`,
		},
		"with props and no children": {
			source: `package app
type LeafProps struct { Title string }
component Leaf(props: LeafProps) {
	return <span>{props.Title}</span>
}
`,
			want: `func Leaf(props LeafProps, children ...gosx.Node) gosx.Node`,
		},
		"without props": {
			source: `package app
component Bare() {
	return <span>bare</span>
}
`,
			want: `func Bare(children ...gosx.Node) gosx.Node`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := Transpile([]byte(test.source), Options{SourceFile: "page.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			if !strings.Contains(out, test.want) {
				t.Fatalf("missing %q in:\n%s", test.want, out)
			}
		})
	}
}

// TestStrictChildrenHoleProjectsToGosxExpr proves W4 and W5 stay unchanged:
// the {children} hole emits gosx.Expr(children), and gosx.Expr fragments a
// []Node, so the variadic parameter feeds it directly.
func TestStrictChildrenHoleProjectsToGosxExpr(t *testing.T) {
	out, err := Transpile([]byte(`package app
type PanelProps struct { Title string }
component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2><div class="body">{children}</div></section>
}
`), Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if !strings.Contains(out, `gosx.Expr(children)`) {
		t.Fatalf("missing gosx.Expr(children) in:\n%s", out)
	}
}

// TestStrictCallWithChildrenProjectsChildArguments proves W6: the typed call
// emitter already appends child arguments after the props literal, so a
// children-bearing call needs no emitter change at all.
func TestStrictCallWithChildrenProjectsChildArguments(t *testing.T) {
	out, err := Transpile([]byte(`package app
type PanelProps struct { Title string }
component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}
component Page(props: PanelProps) {
	return <Panel Title={props.Title}><b>content</b></Panel>
}
`), Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	want := `Panel(PanelProps{Title: props.Title}, gosx.El("b", gosx.Text("content")))`
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}
