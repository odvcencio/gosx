package transpile

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
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

// TestSharedCallWithChildrenMatchesTheVariadicSignature proves the
// cross-package contract WP4 (#245) depends on: a cross-directory shared call
// that carries children projects to a call the always-variadic signature
// accepts, with the children appended after the qualified props literal.
//
// WP4's own suite shipped no children case, and the two halves land in the
// same release, so the assertion belongs here — this is the side that chose
// the signature.
func TestSharedCallWithChildrenMatchesTheVariadicSignature(t *testing.T) {
	sharedSource := []byte(`package ui

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2><div>{children}</div></section>
}
`)
	prog, err := gosx.Compile(sharedSource)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{{Path: "ui/panel.gsx", Source: sharedSource, Program: prog}})

	out, err := Transpile([]byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page(title string) gosx.Node {
	return <div><ui.Panel Title={title}><b>CALLER</b></ui.Panel></div>
}
`), Options{
		SourceFile:    "page.gsx",
		SharedImports: map[string]SharedImport{"./ui": {GoImportPath: "example.test/app/ui", Components: shared}},
	})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	want := `ui.Panel(ui.PanelProps{Title: title}, gosx.El("b", gosx.Text("CALLER")))`
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}
