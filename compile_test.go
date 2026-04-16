package gosx

import (
	"errors"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/ir"
)

func TestGrammarGeneration(t *testing.T) {
	lang, err := Language()
	if err != nil {
		t.Fatalf("Language() failed: %v", err)
	}
	if lang == nil {
		t.Fatal("Language() returned nil")
	}
}

func TestGrammarBlobExposed(t *testing.T) {
	if got := len(GrammarBlob()); got == 0 {
		t.Fatal("GrammarBlob() returned empty data")
	}
}

func TestParseSimpleComponent(t *testing.T) {
	source := []byte(`package main

func Hello() Node {
	return <div class="hello">Hello, World!</div>
}
`)
	tree, lang, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}

	root := tree.RootNode()
	_ = lang
	if root.HasError() {
		t.Errorf("Parse tree has errors")
	}
}

func TestParseSelfClosing(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <img src="photo.jpg" alt="A photo" />
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for self-closing element")
	}
}

func TestParseFragment(t *testing.T) {
	source := []byte(`package main

func List() Node {
	return <>
		<li>One</li>
		<li>Two</li>
	</>
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for fragment")
	}
}

func TestParseExpressionHole(t *testing.T) {
	source := []byte(`package main

func Greeting(props Props) Node {
	return <span>{props.Name}</span>
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for expression holes")
	}
}

func TestParseNestedElements(t *testing.T) {
	source := []byte(`package main

func Counter(props Props) Node {
	return <div class="counter">
		<button onClick={props.Dec}>-</button>
		<span>{props.Count}</span>
		<button onClick={props.Inc}>+</button>
	</div>
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for nested elements")
	}
}

func TestParseComponentSiblingsAfterSelfClosing(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <article>
		<div class="editor-layout">
			<div class="editor-canvas">
				<EditorBlocks />
				<EditorEmptyState />
				<EditorPalette />
			</div>
			<div class="editor-sidebar">
				<EditorPreview />
			</div>
		</div>
	</article>
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for sibling self-closing components")
	}
}

func TestParseAttributesAfterExpressionAttribute(t *testing.T) {
	source := []byte(`package main

func Page(item Item) Node {
	return <div>
		<Link href={item.EditHref} class="btn btn-sm">Edit</Link>
		<Link href={item.ViewHref} class="btn btn-sm">View</Link>
	</div>
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for attributes after expression attributes")
	}
}

func TestCompileAttributesAfterExpressionAttribute(t *testing.T) {
	source := []byte(`package main

func Page(item Item) Node {
	return <div>
		<Link href={item.EditHref} class="btn btn-sm">Edit</Link>
		<Link href={item.ViewHref} class="btn btn-sm">View</Link>
	</div>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}

func TestCompileSelfClosingComponentWithExpressionAttribute(t *testing.T) {
	source := []byte(`package main

func Page(foo Foo) Node {
	return <Avatar userId={foo.CreatorID} />
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}

func TestCompileMultipleAttributesAfterExpressionAttribute(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <Foo bar="string" baz={2} data-i8n="dialogs.welcome.heading" bam />
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}

func TestCompileTextWithGoishEqualsContent(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <pre>foo=bar</pre>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}

func TestCompileTextWithAmpersandContent(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <p>alpha & beta</p>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}

func TestCompileTextImmediatelyAfterChildElement(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "trailing punctuation after inline element",
			source: `package main

func Page() Node {
	return <p>start <a href="#">link</a>.</p>
}
`,
		},
		{
			name: "tight text-element-text on one line",
			source: `package main

func Page() Node {
	return <p>a<span>b</span>c</p>
}
`,
		},
		{
			name: "anchor with attributes followed by punctuation",
			source: `package main

func Page() Node {
	return <p>visit <a href="https://example.com" target="_blank" rel="noopener">example</a>!</p>
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compile([]byte(tc.source)); err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
		})
	}
}

func TestCompileTextWithLiteralGSXPunctuation(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "greater than",
			source: `package main

func Page() Node {
	return <p>a > b</p>
}
`,
			want: "a > b",
		},
		{
			name: "closing brace",
			source: `package main

func Page() Node {
	return <p>Result: }</p>
}
`,
			want: "Result: }",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := Compile([]byte(tc.source))
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			for _, node := range prog.Nodes {
				if node.Kind == ir.NodeText && node.Text == tc.want {
					return
				}
			}
			t.Fatalf("expected text node %q, got %#v", tc.want, prog.Nodes)
		})
	}
}

func TestCompileReturnsAllValidationDiagnostics(t *testing.T) {
	source := []byte(`package main

func broken() Node {
	return <div>first</div>
}

func alsoBroken() Node {
	return <span>second</span>
}
`)
	_, err := Compile(source)
	if err == nil {
		t.Fatal("expected validation diagnostics")
	}
	var diagErr *ir.DiagnosticsError
	if !errors.As(err, &diagErr) {
		t.Fatalf("expected diagnostics error, got %T: %v", err, err)
	}
	if len(diagErr.Diagnostics) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diagErr.Diagnostics))
	}
	if !strings.Contains(diagErr.Diagnostics[0].Message, "uppercase") {
		t.Fatalf("unexpected first diagnostic %#v", diagErr.Diagnostics[0])
	}
	if !strings.Contains(diagErr.Diagnostics[1].Message, "uppercase") {
		t.Fatalf("unexpected second diagnostic %#v", diagErr.Diagnostics[1])
	}
}

func TestParseIfSiblingBoundary(t *testing.T) {
	source := []byte(`package main

func Page(ok bool) Node {
	return <div>
		<If when={ok}>
			<div class="empty">Ready</div>
		</If>
		<div class="next">Next</div>
	</div>
}
`)
	tree, _, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	root := tree.RootNode()
	if root.HasError() {
		t.Error("Parse tree has errors for siblings after If blocks")
	}
}

func TestCompileComponent(t *testing.T) {
	source := []byte(`package main

func Hello() Node {
	return <div class="hello">Hello, World!</div>
}
`)
	prog, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}
	if prog.Components[0].Name != "Hello" {
		t.Errorf("expected component name 'Hello', got %q", prog.Components[0].Name)
	}
	if prog.Package != "main" {
		t.Errorf("expected package 'main', got %q", prog.Package)
	}
}

func TestCompileMultipleComponents(t *testing.T) {
	source := []byte(`package main

func Header() Node {
	return <header><h1>Title</h1></header>
}

func Footer() Node {
	return <footer>Copyright 2026</footer>
}
`)
	prog, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(prog.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(prog.Components))
	}
}

func TestCompileParseErrorIncludesLocationAndSnippet(t *testing.T) {
	source := []byte(`package main

func Broken() Node {
	return <div>{</div>
}
`)

	_, err := Compile(source)
	if err == nil {
		t.Fatal("expected parse error")
	}

	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ParseError, got %T: %v", err, err)
	}
	if parseErr.Line == 0 || parseErr.Column == 0 {
		t.Fatalf("expected line/column, got %d:%d", parseErr.Line, parseErr.Column)
	}
	if !strings.Contains(parseErr.Snippet, "return <div>{</div>") {
		t.Fatalf("expected source snippet, got %q", parseErr.Snippet)
	}

	msg := err.Error()
	if !strings.Contains(msg, "^") {
		t.Fatalf("expected caret marker in error, got %q", msg)
	}
}

func TestCompileAttributes(t *testing.T) {
	source := []byte(`package main

func Image() Node {
	return <img src="photo.jpg" alt="A photo" />
}
`)
	prog, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Tag != "img" {
		t.Errorf("expected tag 'img', got %q", root.Tag)
	}
	if len(root.Attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(root.Attrs))
	}
	if root.Attrs[0].Name != "src" || root.Attrs[0].Value != "photo.jpg" {
		t.Errorf("expected src='photo.jpg', got %q=%q", root.Attrs[0].Name, root.Attrs[0].Value)
	}
}

func TestNodeRenderHTML(t *testing.T) {
	node := El("div", Attrs(Attr("class", "hello")), Text("Hello, World!"))
	html := RenderHTML(node)

	if !strings.Contains(html, `<div class="hello">`) {
		t.Errorf("expected div with class, got %q", html)
	}
	if !strings.Contains(html, "Hello, World!") {
		t.Errorf("expected text content, got %q", html)
	}
	if !strings.Contains(html, "</div>") {
		t.Errorf("expected closing tag, got %q", html)
	}
}

func TestNodeRenderNested(t *testing.T) {
	node := El("div", Attrs(Attr("class", "counter")),
		El("button", Text("-")),
		El("span", Text("0")),
		El("button", Text("+")),
	)
	html := RenderHTML(node)

	expected := `<div class="counter"><button>-</button><span>0</span><button>+</button></div>`
	if html != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, html)
	}
}

func TestNodeRenderVoidElement(t *testing.T) {
	node := El("img", Attrs(Attr("src", "photo.jpg")))
	html := RenderHTML(node)

	if html != `<img src="photo.jpg" />` {
		t.Errorf("expected self-closing img, got %q", html)
	}
}

func TestNodeRenderFragment(t *testing.T) {
	node := Fragment(
		El("li", Text("One")),
		El("li", Text("Two")),
	)
	html := RenderHTML(node)

	expected := `<li>One</li><li>Two</li>`
	if html != expected {
		t.Errorf("expected %q, got %q", expected, html)
	}
}

func TestNodeRenderEscaping(t *testing.T) {
	node := El("div", Text("<script>alert('xss')</script>"))
	html := RenderHTML(node)

	if strings.Contains(html, "<script>") {
		t.Errorf("HTML should be escaped, got %q", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected escaped HTML, got %q", html)
	}
}

func TestPlainTextWalksNodeContent(t *testing.T) {
	node := El("div",
		Text("Hello "),
		El("strong", Text("world")),
		Expr("!"),
		RawHTML("<span>ignored</span>"),
	)

	if got := PlainText(node); got != "Hello world!" {
		t.Fatalf("expected plain text content, got %q", got)
	}
}

func TestNodeRenderBoolAttr(t *testing.T) {
	node := El("input", Attrs(BoolAttr("disabled"), Attr("type", "text")))
	html := RenderHTML(node)

	if !strings.Contains(html, " disabled") {
		t.Errorf("expected boolean attr, got %q", html)
	}
	if !strings.Contains(html, `type="text"`) {
		t.Errorf("expected type attr, got %q", html)
	}
}
