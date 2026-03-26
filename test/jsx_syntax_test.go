package test

import (
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
)

// mustCompile is a test helper that compiles .gsx source and fails the test on error.
func mustCompile(t *testing.T, source string) *ir.Program {
	t.Helper()
	prog, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return prog
}

// === Element Tests ===

func TestJSXSimpleElement(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>hello</div>
}`)

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}
	comp := prog.Components[0]
	if comp.Name != "App" {
		t.Fatalf("expected component name 'App', got %q", comp.Name)
	}

	root := prog.NodeAt(comp.Root)
	if root.Kind != ir.NodeElement {
		t.Fatalf("expected NodeElement, got %d", root.Kind)
	}
	if root.Tag != "div" {
		t.Fatalf("expected tag 'div', got %q", root.Tag)
	}
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(root.Children))
	}

	child := prog.NodeAt(root.Children[0])
	if child.Kind != ir.NodeText {
		t.Fatalf("expected NodeText child, got %d", child.Kind)
	}
	if child.Text != "hello" {
		t.Fatalf("expected text 'hello', got %q", child.Text)
	}
}

func TestJSXNestedElements(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div><span><em>deep</em></span></div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Tag != "div" {
		t.Fatalf("expected 'div', got %q", root.Tag)
	}
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child of div, got %d", len(root.Children))
	}

	span := prog.NodeAt(root.Children[0])
	if span.Tag != "span" {
		t.Fatalf("expected 'span', got %q", span.Tag)
	}
	if len(span.Children) != 1 {
		t.Fatalf("expected 1 child of span, got %d", len(span.Children))
	}

	em := prog.NodeAt(span.Children[0])
	if em.Tag != "em" {
		t.Fatalf("expected 'em', got %q", em.Tag)
	}
	if len(em.Children) != 1 {
		t.Fatalf("expected 1 child of em, got %d", len(em.Children))
	}

	text := prog.NodeAt(em.Children[0])
	if text.Kind != ir.NodeText {
		t.Fatalf("expected NodeText, got %d", text.Kind)
	}
	if text.Text != "deep" {
		t.Fatalf("expected 'deep', got %q", text.Text)
	}
}

func TestJSXSelfClosing(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div><img /><br /><input /></div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Tag != "div" {
		t.Fatalf("expected 'div', got %q", root.Tag)
	}

	// Filter out whitespace text nodes to get the actual element children
	var elemChildren []ir.NodeID
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeElement || n.Kind == ir.NodeComponent {
			elemChildren = append(elemChildren, cid)
		}
	}

	if len(elemChildren) != 3 {
		t.Fatalf("expected 3 self-closing element children, got %d (total children: %d)", len(elemChildren), len(root.Children))
	}

	expectedTags := []string{"img", "br", "input"}
	for i, cid := range elemChildren {
		child := prog.NodeAt(cid)
		if child.Tag != expectedTags[i] {
			t.Errorf("child %d: expected tag %q, got %q", i, expectedTags[i], child.Tag)
		}
		if child.Kind != ir.NodeElement {
			t.Errorf("child %d: expected NodeElement, got %d", i, child.Kind)
		}
		if len(child.Children) != 0 {
			t.Errorf("child %d (%s): self-closing element should have 0 children, got %d", i, child.Tag, len(child.Children))
		}
	}
}

func TestJSXSiblingElements(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div><span>a</span><span>b</span><span>c</span></div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)

	// Filter to span elements only
	var spans []ir.NodeID
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeElement && n.Tag == "span" {
			spans = append(spans, cid)
		}
	}

	if len(spans) != 3 {
		t.Fatalf("expected 3 span children, got %d", len(spans))
	}

	expectedTexts := []string{"a", "b", "c"}
	for i, cid := range spans {
		span := prog.NodeAt(cid)
		if len(span.Children) < 1 {
			t.Fatalf("span %d has no children", i)
		}
		text := prog.NodeAt(span.Children[0])
		if text.Kind != ir.NodeText {
			t.Errorf("span %d child: expected NodeText, got %d", i, text.Kind)
		}
		if text.Text != expectedTexts[i] {
			t.Errorf("span %d text: expected %q, got %q", i, expectedTexts[i], text.Text)
		}
	}
}

func TestJSXVoidElements(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div><hr /><br /></div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)

	var elemChildren []ir.NodeID
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeElement {
			elemChildren = append(elemChildren, cid)
		}
	}

	if len(elemChildren) != 2 {
		t.Fatalf("expected 2 void element children, got %d", len(elemChildren))
	}

	hr := prog.NodeAt(elemChildren[0])
	br := prog.NodeAt(elemChildren[1])
	if hr.Tag != "hr" {
		t.Errorf("expected 'hr', got %q", hr.Tag)
	}
	if br.Tag != "br" {
		t.Errorf("expected 'br', got %q", br.Tag)
	}
	if len(hr.Children) != 0 {
		t.Errorf("hr should have 0 children")
	}
	if len(br.Children) != 0 {
		t.Errorf("br should have 0 children")
	}
}

// === Attribute Tests ===

func TestJSXStaticAttribute(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div class="container" id="main">x</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(root.Attrs))
	}

	if root.Attrs[0].Kind != ir.AttrStatic {
		t.Errorf("attr 0: expected AttrStatic, got %d", root.Attrs[0].Kind)
	}
	if root.Attrs[0].Name != "class" || root.Attrs[0].Value != "container" {
		t.Errorf("attr 0: expected class='container', got %s=%q", root.Attrs[0].Name, root.Attrs[0].Value)
	}

	if root.Attrs[1].Kind != ir.AttrStatic {
		t.Errorf("attr 1: expected AttrStatic, got %d", root.Attrs[1].Kind)
	}
	if root.Attrs[1].Name != "id" || root.Attrs[1].Value != "main" {
		t.Errorf("attr 1: expected id='main', got %s=%q", root.Attrs[1].Name, root.Attrs[1].Value)
	}
}

func TestJSXHyphenatedAttributes(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <a data-gosx-link aria-label="Docs" hx-get="/docs">Docs</a>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) != 3 {
		t.Fatalf("expected 3 attrs, got %d", len(root.Attrs))
	}

	if root.Attrs[0].Kind != ir.AttrBool || root.Attrs[0].Name != "data-gosx-link" {
		t.Fatalf("attr 0: expected bool data-gosx-link, got %s kind=%d", root.Attrs[0].Name, root.Attrs[0].Kind)
	}
	if root.Attrs[1].Kind != ir.AttrStatic || root.Attrs[1].Name != "aria-label" || root.Attrs[1].Value != "Docs" {
		t.Fatalf("attr 1: expected aria-label='Docs', got %s=%q kind=%d", root.Attrs[1].Name, root.Attrs[1].Value, root.Attrs[1].Kind)
	}
	if root.Attrs[2].Kind != ir.AttrStatic || root.Attrs[2].Name != "hx-get" || root.Attrs[2].Value != "/docs" {
		t.Fatalf("attr 2: expected hx-get='/docs', got %s=%q kind=%d", root.Attrs[2].Name, root.Attrs[2].Value, root.Attrs[2].Kind)
	}
}

func TestJSXBooleanAttribute(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <input disabled />
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) != 1 {
		t.Fatalf("expected 1 attr, got %d", len(root.Attrs))
	}

	attr := root.Attrs[0]
	if attr.Kind != ir.AttrBool {
		t.Fatalf("expected AttrBool, got %d", attr.Kind)
	}
	if attr.Name != "disabled" {
		t.Fatalf("expected 'disabled', got %q", attr.Name)
	}
}

func TestJSXExpressionAttribute(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div onClick={handler}>x</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) != 1 {
		t.Fatalf("expected 1 attr, got %d", len(root.Attrs))
	}

	attr := root.Attrs[0]
	if attr.Kind != ir.AttrExpr {
		t.Fatalf("expected AttrExpr, got %d", attr.Kind)
	}
	if attr.Name != "onClick" {
		t.Fatalf("expected 'onClick', got %q", attr.Name)
	}
	if !attr.IsEvent {
		t.Fatal("expected IsEvent=true for onClick")
	}
	if attr.Expr != "handler" {
		t.Fatalf("expected expr 'handler', got %q", attr.Expr)
	}
}

func TestJSXMultipleAttributes(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <input type="text" class="input" placeholder="name" disabled />
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) != 4 {
		t.Fatalf("expected 4 attrs, got %d", len(root.Attrs))
	}

	staticCount := 0
	boolCount := 0
	for _, attr := range root.Attrs {
		switch attr.Kind {
		case ir.AttrStatic:
			staticCount++
		case ir.AttrBool:
			boolCount++
		}
	}

	if staticCount != 3 {
		t.Errorf("expected 3 static attrs, got %d", staticCount)
	}
	if boolCount != 1 {
		t.Errorf("expected 1 bool attr, got %d", boolCount)
	}

	// Verify specific attrs
	if root.Attrs[0].Name != "type" || root.Attrs[0].Value != "text" {
		t.Errorf("attr 0: expected type='text', got %s=%q", root.Attrs[0].Name, root.Attrs[0].Value)
	}
	if root.Attrs[3].Name != "disabled" || root.Attrs[3].Kind != ir.AttrBool {
		t.Errorf("attr 3: expected disabled (bool), got %s kind=%d", root.Attrs[3].Name, root.Attrs[3].Kind)
	}
}

func TestJSXCustomElementTag(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <paper-card data-state="ready" />
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Kind != ir.NodeElement {
		t.Fatalf("expected NodeElement, got %d", root.Kind)
	}
	if root.Tag != "paper-card" {
		t.Fatalf("expected custom element tag 'paper-card', got %q", root.Tag)
	}
	if len(root.Attrs) != 1 || root.Attrs[0].Name != "data-state" || root.Attrs[0].Value != "ready" {
		t.Fatalf("unexpected attrs %+v", root.Attrs)
	}
}

func TestJSXSpreadAttribute(t *testing.T) {
	// Spread attributes work in non-island components
	prog := mustCompile(t, `package main

func App() Node {
	return <div {...props}>x</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) != 1 {
		t.Fatalf("expected 1 attr, got %d", len(root.Attrs))
	}

	attr := root.Attrs[0]
	if attr.Kind != ir.AttrSpread {
		t.Fatalf("expected AttrSpread, got %d", attr.Kind)
	}
	if attr.Expr != "props" {
		t.Fatalf("expected expr 'props', got %q", attr.Expr)
	}
}

// === Expression Tests ===

func TestJSXTextExpressionHole(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>{name}</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Children) < 1 {
		t.Fatal("expected at least 1 child")
	}

	// Find the expression child
	var exprChild *ir.Node
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeExpr {
			exprChild = n
			break
		}
	}
	if exprChild == nil {
		t.Fatal("expected an expression child")
	}
	if exprChild.Text != "name" {
		t.Fatalf("expected expr text 'name', got %q", exprChild.Text)
	}
}

func TestJSXMultipleExpressions(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>{a} and {b}</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)

	exprCount := 0
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeExpr {
			exprCount++
		}
	}
	if exprCount != 2 {
		t.Fatalf("expected 2 expression children, got %d", exprCount)
	}
}

func TestJSXExpressionWithMethodCall(t *testing.T) {
	// Non-island component: expression text is stored raw, no validation on method calls
	prog := mustCompile(t, `package main

func App() Node {
	return <span>{count.Get()}</span>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	var exprChild *ir.Node
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeExpr {
			exprChild = n
			break
		}
	}
	if exprChild == nil {
		t.Fatal("expected expression child")
	}
	if exprChild.Text != "count.Get()" {
		t.Fatalf("expected expr text 'count.Get()', got %q", exprChild.Text)
	}
}

// === Fragment Tests ===

func TestJSXFragment(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <><div>a</div><div>b</div></>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Kind != ir.NodeFragment {
		t.Fatalf("expected NodeFragment, got %d", root.Kind)
	}

	// Find div children (may include whitespace text nodes)
	var divChildren []ir.NodeID
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeElement && n.Tag == "div" {
			divChildren = append(divChildren, cid)
		}
	}
	if len(divChildren) != 2 {
		t.Fatalf("expected 2 div children in fragment, got %d", len(divChildren))
	}

	// Verify text content of each div
	expectedTexts := []string{"a", "b"}
	for i, cid := range divChildren {
		div := prog.NodeAt(cid)
		if len(div.Children) < 1 {
			t.Fatalf("div %d has no children", i)
		}
		text := prog.NodeAt(div.Children[0])
		if text.Text != expectedTexts[i] {
			t.Errorf("div %d text: expected %q, got %q", i, expectedTexts[i], text.Text)
		}
	}
}

func TestJSXFragmentWithText(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <>hello world</>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Kind != ir.NodeFragment {
		t.Fatalf("expected NodeFragment, got %d", root.Kind)
	}

	// Should have at least one text child
	if len(root.Children) < 1 {
		t.Fatal("expected at least 1 child in fragment")
	}

	// Find a text child containing "hello world"
	foundText := false
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeText && n.Text == "hello world" {
			foundText = true
			break
		}
	}
	if !foundText {
		// Dump what we got for debugging
		for i, cid := range root.Children {
			n := prog.NodeAt(cid)
			t.Logf("child %d: kind=%d text=%q", i, n.Kind, n.Text)
		}
		t.Fatal("expected text child 'hello world'")
	}
}

// === Component Tests ===

func TestJSXComponent(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div><Counter /><Footer /></div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)

	var compChildren []ir.NodeID
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeComponent {
			compChildren = append(compChildren, cid)
		}
	}
	if len(compChildren) != 2 {
		t.Fatalf("expected 2 component children, got %d", len(compChildren))
	}

	counter := prog.NodeAt(compChildren[0])
	footer := prog.NodeAt(compChildren[1])
	if counter.Tag != "Counter" {
		t.Errorf("expected 'Counter', got %q", counter.Tag)
	}
	if footer.Tag != "Footer" {
		t.Errorf("expected 'Footer', got %q", footer.Tag)
	}
}

func TestJSXComponentWithProps(t *testing.T) {
	// Note: in the current grammar, static attrs must precede expression attrs.
	prog := mustCompile(t, `package main

func App() Node {
	return <div label="clicks" count={val}>x</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Kind != ir.NodeElement {
		t.Fatalf("expected NodeElement, got %d", root.Kind)
	}
	if root.Tag != "div" {
		t.Fatalf("expected 'div', got %q", root.Tag)
	}
	if len(root.Attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(root.Attrs))
	}

	// Check count={val} is an expression attr
	foundExpr := false
	foundStatic := false
	for _, attr := range root.Attrs {
		if attr.Name == "count" && attr.Kind == ir.AttrExpr {
			foundExpr = true
			if attr.Expr != "val" {
				t.Errorf("expected expr 'val', got %q", attr.Expr)
			}
		}
		if attr.Name == "label" && attr.Kind == ir.AttrStatic {
			foundStatic = true
			if attr.Value != "clicks" {
				t.Errorf("expected value 'clicks', got %q", attr.Value)
			}
		}
	}
	if !foundExpr {
		t.Error("expected expression attr 'count'")
	}
	if !foundStatic {
		t.Error("expected static attr 'label'")
	}
}

// === Island Directive Tests ===

func TestJSXIslandDirective(t *testing.T) {
	prog := mustCompile(t, `package main

//gosx:island
func Counter() Node {
	return <div>0</div>
}`)

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}
	if !prog.Components[0].IsIsland {
		t.Fatal("expected IsIsland=true")
	}
}

func TestJSXNoDirective(t *testing.T) {
	prog := mustCompile(t, `package main

func Counter() Node {
	return <div>0</div>
}`)

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}
	if prog.Components[0].IsIsland {
		t.Fatal("expected IsIsland=false")
	}
}

func TestJSXEngineDirective(t *testing.T) {
	prog := mustCompile(t, `package main

//gosx:engine surface
//gosx:capabilities canvas webgl
func Renderer() Node {
	return <div>canvas</div>
}`)

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}
	comp := prog.Components[0]
	if !comp.IsEngine {
		t.Fatal("expected IsEngine=true")
	}
	if comp.EngineKind != "surface" {
		t.Fatalf("expected EngineKind 'surface', got %q", comp.EngineKind)
	}
	if len(comp.EngineCapabilities) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(comp.EngineCapabilities))
	}
	if comp.EngineCapabilities[0] != "canvas" {
		t.Errorf("capability 0: expected 'canvas', got %q", comp.EngineCapabilities[0])
	}
	if comp.EngineCapabilities[1] != "webgl" {
		t.Errorf("capability 1: expected 'webgl', got %q", comp.EngineCapabilities[1])
	}
}

// === Complex / Real-World Tests ===

func TestJSXCompleteCounter(t *testing.T) {
	prog := mustCompile(t, `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<button onClick={increment}>+</button>
		<span class="count">{count.Get()}</span>
	</div>
}`)

	comp := prog.Components[0]
	if !comp.IsIsland {
		t.Fatal("expected island")
	}
	if comp.Scope == nil {
		t.Fatal("expected scope")
	}
	if len(comp.Scope.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(comp.Scope.Signals))
	}
	if len(comp.Scope.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(comp.Scope.Handlers))
	}
	if comp.Scope.Signals[0].Name != "count" {
		t.Fatalf("expected signal named 'count', got %q", comp.Scope.Signals[0].Name)
	}
	if comp.Scope.Handlers[0].Name != "increment" {
		t.Fatalf("expected handler named 'increment', got %q", comp.Scope.Handlers[0].Name)
	}
	if comp.Scope.Signals[0].InitExpr != "0" {
		t.Fatalf("expected init expr '0', got %q", comp.Scope.Signals[0].InitExpr)
	}
	if comp.Scope.Signals[0].TypeHint != "int" {
		t.Fatalf("expected type hint 'int', got %q", comp.Scope.Signals[0].TypeHint)
	}

	// Verify node structure: root is a div
	root := prog.NodeAt(comp.Root)
	if root.Tag != "div" {
		t.Fatalf("expected root tag 'div', got %q", root.Tag)
	}

	// Verify root has class="counter"
	foundClass := false
	for _, attr := range root.Attrs {
		if attr.Name == "class" && attr.Value == "counter" {
			foundClass = true
		}
	}
	if !foundClass {
		t.Fatal("expected class='counter' on root div")
	}

	// Verify the tree has a button with onClick and a span
	var foundButton, foundSpan bool
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeElement && n.Tag == "button" {
			foundButton = true
			for _, attr := range n.Attrs {
				if attr.Name == "onClick" && attr.IsEvent {
					t.Logf("button onClick handler: %q", attr.Expr)
				}
			}
		}
		if n.Kind == ir.NodeElement && n.Tag == "span" {
			foundSpan = true
		}
	}
	if !foundButton {
		t.Error("expected button child")
	}
	if !foundSpan {
		t.Error("expected span child")
	}
}

func TestJSXCompleteForm(t *testing.T) {
	prog := mustCompile(t, `package main

//gosx:island
func LoginForm() Node {
	email := signal.New("")
	password := signal.New("")
	error := signal.New("")
	submit := func() { error.Set("") }
	return <form class="login">
		<input type="email" placeholder="Email" />
		<input type="password" placeholder="Password" />
		<button onClick={submit}>Log In</button>
		<span class="error">{error.Get()}</span>
	</form>
}`)

	comp := prog.Components[0]
	if comp.Name != "LoginForm" {
		t.Fatalf("expected 'LoginForm', got %q", comp.Name)
	}
	if !comp.IsIsland {
		t.Fatal("expected island")
	}
	if comp.Scope == nil {
		t.Fatal("expected scope")
	}
	if len(comp.Scope.Signals) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(comp.Scope.Signals))
	}
	if len(comp.Scope.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(comp.Scope.Handlers))
	}

	// Verify signal names
	sigNames := make(map[string]bool)
	for _, sig := range comp.Scope.Signals {
		sigNames[sig.Name] = true
	}
	if !sigNames["email"] {
		t.Error("expected signal 'email'")
	}
	if !sigNames["password"] {
		t.Error("expected signal 'password'")
	}
	if !sigNames["error"] {
		t.Error("expected signal 'error'")
	}

	// Verify handler
	if comp.Scope.Handlers[0].Name != "submit" {
		t.Fatalf("expected handler 'submit', got %q", comp.Scope.Handlers[0].Name)
	}

	// Verify root is a form
	root := prog.NodeAt(comp.Root)
	if root.Tag != "form" {
		t.Fatalf("expected root tag 'form', got %q", root.Tag)
	}
}

func TestJSXMultipleComponents(t *testing.T) {
	prog := mustCompile(t, `package main

func Header() Node {
	return <h1>Title</h1>
}

func Footer() Node {
	return <footer>bye</footer>
}

func App() Node {
	return <div><Header /><Footer /></div>
}`)

	if len(prog.Components) != 3 {
		t.Fatalf("expected 3 components, got %d", len(prog.Components))
	}

	names := map[string]bool{}
	for _, comp := range prog.Components {
		names[comp.Name] = true
	}
	if !names["Header"] {
		t.Error("missing Header component")
	}
	if !names["Footer"] {
		t.Error("missing Footer component")
	}
	if !names["App"] {
		t.Error("missing App component")
	}
}

// === Static Detection Tests ===

func TestJSXStaticDetection(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div class="static"><span>text</span></div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if !root.IsStatic {
		t.Fatal("root div should be static (no expressions)")
	}

	// Check that child span is also static
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if !n.IsStatic {
			t.Errorf("child node (kind=%d, tag=%q, text=%q) should be static", n.Kind, n.Tag, n.Text)
		}
	}
}

func TestJSXDynamicDetection(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>{x}</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.IsStatic {
		t.Fatal("root with expression child should not be static")
	}

	// The expression child itself should not be static
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		if n.Kind == ir.NodeExpr && n.IsStatic {
			t.Error("expression node should not be static")
		}
	}
}

func TestJSXStaticWithDynamicAttr(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div onClick={handler}>static text</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.IsStatic {
		t.Fatal("element with expression attr should not be static")
	}
}

// === Event Handler Tests ===

func TestJSXEventHandlerOnElement(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <button onClick={handler}>click</button>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if len(root.Attrs) == 0 {
		t.Fatal("expected attrs")
	}

	found := false
	for _, attr := range root.Attrs {
		if attr.Name == "onClick" && attr.IsEvent {
			found = true
			if attr.Kind != ir.AttrExpr {
				t.Errorf("expected AttrExpr kind, got %d", attr.Kind)
			}
			if attr.Expr != "handler" {
				t.Errorf("expected expr 'handler', got %q", attr.Expr)
			}
		}
	}
	if !found {
		t.Fatal("expected onClick event attr")
	}
}

func TestJSXEventHandlerDetection(t *testing.T) {
	// onSubmit should be detected as event; data= is not an event
	prog := mustCompile(t, `package main

func App() Node {
	return <form onSubmit={submit}>
		<div onChange={update}>inner</div>
		<div data="val">x</div>
	</form>
}`)

	root := prog.NodeAt(prog.Components[0].Root)

	// Check onSubmit on form
	foundSubmit := false
	for _, attr := range root.Attrs {
		if attr.Name == "onSubmit" && attr.IsEvent {
			foundSubmit = true
		}
	}
	if !foundSubmit {
		t.Error("expected onSubmit event attr on form")
	}

	// Check onChange on the div child (used instead of self-closing input)
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		for _, attr := range n.Attrs {
			if attr.Name == "onChange" {
				if !attr.IsEvent {
					t.Error("expected onChange to be detected as event")
				}
				if attr.Kind != ir.AttrExpr {
					t.Errorf("expected AttrExpr for onChange, got %d", attr.Kind)
				}
			}
			// data= is a static attr, not an event
			if attr.Name == "data" {
				if attr.IsEvent {
					t.Error("data= should not be detected as event")
				}
				if attr.Kind != ir.AttrStatic {
					t.Errorf("expected AttrStatic for data=, got %d", attr.Kind)
				}
			}
		}
	}
}

// === Package Detection ===

func TestJSXPackageName(t *testing.T) {
	prog := mustCompile(t, `package myapp

func App() Node {
	return <div>hello</div>
}`)

	if prog.Package != "myapp" {
		t.Fatalf("expected package 'myapp', got %q", prog.Package)
	}
}

// === Component Props Type ===

func TestJSXComponentPropsType(t *testing.T) {
	prog := mustCompile(t, `package main

func Greeting(props GreetingProps) Node {
	return <span>hi</span>
}`)

	comp := prog.Components[0]
	if comp.PropsType != "GreetingProps" {
		t.Fatalf("expected PropsType 'GreetingProps', got %q", comp.PropsType)
	}
}

func TestJSXComponentNoProps(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>hello</div>
}`)

	comp := prog.Components[0]
	if comp.PropsType != "" {
		t.Fatalf("expected empty PropsType, got %q", comp.PropsType)
	}
}

// === Mixed Content ===

func TestJSXMixedTextAndExpressions(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>Hello {name}!</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)

	var textCount, exprCount int
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		switch n.Kind {
		case ir.NodeText:
			textCount++
		case ir.NodeExpr:
			exprCount++
		}
	}

	if textCount < 1 {
		t.Error("expected at least 1 text child")
	}
	if exprCount != 1 {
		t.Errorf("expected 1 expression child, got %d", exprCount)
	}
}

// === Node Kind Verification ===

func TestJSXNodeKinds(t *testing.T) {
	prog := mustCompile(t, `package main

func App() Node {
	return <div>
		<Custom />
		<>inner</>
		<span>text</span>
		{expr}
	</div>
}`)

	root := prog.NodeAt(prog.Components[0].Root)
	if root.Kind != ir.NodeElement {
		t.Fatalf("root: expected NodeElement, got %d", root.Kind)
	}

	kindCounts := map[ir.NodeKind]int{}
	for _, cid := range root.Children {
		n := prog.NodeAt(cid)
		kindCounts[n.Kind]++
	}

	if kindCounts[ir.NodeComponent] < 1 {
		t.Error("expected at least 1 NodeComponent child")
	}
	if kindCounts[ir.NodeFragment] < 1 {
		t.Error("expected at least 1 NodeFragment child")
	}
	if kindCounts[ir.NodeElement] < 1 {
		t.Error("expected at least 1 NodeElement child")
	}
	if kindCounts[ir.NodeExpr] < 1 {
		t.Error("expected at least 1 NodeExpr child")
	}
}

// === Scope Locals Map ===

func TestJSXScopeLocalsMap(t *testing.T) {
	prog := mustCompile(t, `package main

//gosx:island
func App() Node {
	count := signal.New(0)
	doubled := signal.Derive(func() int { return count.Get() * 2 })
	reset := func() { count.Set(0) }
	return <div>{count.Get()}</div>
}`)

	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected scope")
	}

	if comp.Scope.Locals["count"] != "signal" {
		t.Errorf("expected count='signal', got %q", comp.Scope.Locals["count"])
	}
	if comp.Scope.Locals["doubled"] != "computed" {
		t.Errorf("expected doubled='computed', got %q", comp.Scope.Locals["doubled"])
	}
	if comp.Scope.Locals["reset"] != "handler" {
		t.Errorf("expected reset='handler', got %q", comp.Scope.Locals["reset"])
	}
}

// === No Scope for Static Components ===

func TestJSXNoScopeForStatic(t *testing.T) {
	prog := mustCompile(t, `package main

func Static() Node {
	return <div>hello</div>
}`)

	if prog.Components[0].Scope != nil {
		t.Error("expected nil scope for component with no signals/handlers")
	}
}

// === IsIslandRoot Flag ===

func TestJSXIslandNotServerOnly(t *testing.T) {
	prog := mustCompile(t, `package main

//gosx:island
func Counter() Node {
	return <div>0</div>
}`)

	comp := prog.Components[0]
	if comp.ServerOnly {
		t.Fatal("island components should not be ServerOnly")
	}
}
