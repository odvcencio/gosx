package ir_test

import (
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
)

func parse(t *testing.T, source []byte) (*ir.Program, error) {
	t.Helper()
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	return ir.Lower(root, source, lang)
}

func TestLowerSimpleElement(t *testing.T) {
	source := []byte(`package main

func Hello() Node {
	return <div class="hello">Hello</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}

	comp := prog.Components[0]
	if comp.Name != "Hello" {
		t.Errorf("expected component 'Hello', got %q", comp.Name)
	}

	root := prog.NodeAt(comp.Root)
	if root.Kind != ir.NodeElement {
		t.Errorf("expected NodeElement, got %d", root.Kind)
	}
	if root.Tag != "div" {
		t.Errorf("expected tag 'div', got %q", root.Tag)
	}
	if len(root.Attrs) == 0 {
		t.Fatal("expected attributes")
	}
	if root.Attrs[0].Name != "class" || root.Attrs[0].Value != "hello" {
		t.Errorf("expected class='hello', got %q=%q", root.Attrs[0].Name, root.Attrs[0].Value)
	}
}

func TestLowerSelfClosing(t *testing.T) {
	source := []byte(`package main

func Image() Node {
	return <img src="photo.jpg" />
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	comp := prog.Components[0]
	root := prog.NodeAt(comp.Root)
	if root.Tag != "img" {
		t.Errorf("expected 'img', got %q", root.Tag)
	}
	if len(root.Children) != 0 {
		t.Errorf("expected no children for self-closing, got %d", len(root.Children))
	}
}

func TestLowerFragment(t *testing.T) {
	source := []byte(`package main

func List() Node {
	return <>
		<li>A</li>
		<li>B</li>
	</>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	comp := prog.Components[0]
	root := prog.NodeAt(comp.Root)
	if root.Kind != ir.NodeFragment {
		t.Errorf("expected NodeFragment, got %d", root.Kind)
	}
}

func TestLowerExpressionHole(t *testing.T) {
	source := []byte(`package main

func Greeting(props Props) Node {
	return <span>{props.Name}</span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	comp := prog.Components[0]
	if comp.PropsType != "Props" {
		t.Errorf("expected props type 'Props', got %q", comp.PropsType)
	}

	root := prog.NodeAt(comp.Root)
	if root.Tag != "span" {
		t.Errorf("expected 'span', got %q", root.Tag)
	}

	// Should have expression child
	found := false
	for _, childID := range root.Children {
		child := prog.NodeAt(childID)
		if child.Kind == ir.NodeExpr {
			found = true
			if child.Text != "props.Name" {
				t.Errorf("expected expr 'props.Name', got %q", child.Text)
			}
		}
	}
	if !found {
		t.Error("expected expression hole child not found")
	}
}

func TestLowerNestedElements(t *testing.T) {
	source := []byte(`package main

func Counter(props Props) Node {
	return <div class="counter">
		<button onClick={props.Dec}>-</button>
		<span>{props.Count}</span>
		<button onClick={props.Inc}>+</button>
	</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	comp := prog.Components[0]
	root := prog.NodeAt(comp.Root)
	if root.Tag != "div" {
		t.Errorf("expected 'div', got %q", root.Tag)
	}

	// Count element children (excluding whitespace text nodes)
	elementCount := 0
	for _, childID := range root.Children {
		child := prog.NodeAt(childID)
		if child.Kind == ir.NodeElement {
			elementCount++
		}
	}
	if elementCount < 3 {
		t.Errorf("expected at least 3 element children (2 buttons + span), got %d", elementCount)
	}
}

func TestLowerEventAttributes(t *testing.T) {
	source := []byte(`package main

func Button() Node {
	return <button onClick={handleClick}>Click</button>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	comp := prog.Components[0]
	root := prog.NodeAt(comp.Root)
	if len(root.Attrs) == 0 {
		t.Fatal("expected attributes")
	}

	found := false
	for _, attr := range root.Attrs {
		if attr.Name == "onClick" {
			found = true
			if !attr.IsEvent {
				t.Error("onClick should be marked as event")
			}
			if attr.Expr != "handleClick" {
				t.Errorf("expected expr 'handleClick', got %q", attr.Expr)
			}
		}
	}
	if !found {
		t.Error("onClick attribute not found")
	}
}

func TestLowerStaticDetection(t *testing.T) {
	source := []byte(`package main

func Static() Node {
	return <div class="static">Hello</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	comp := prog.Components[0]
	root := prog.NodeAt(comp.Root)
	if !root.IsStatic {
		t.Error("expected static subtree for purely static element")
	}
}
