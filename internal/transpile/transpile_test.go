package transpile

import (
	"errors"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestTranspileParseErrorIncludesLocationAndSnippet(t *testing.T) {
	source := []byte(`package main

func Broken() Node {
	return <div>{</div>
}
`)

	_, err := Transpile(source, Options{SourceFile: "broken.gsx"})
	if err == nil {
		t.Fatal("expected parse error")
	}

	var parseErr *gosx.ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ParseError, got %T: %v", err, err)
	}
	if parseErr.Line == 0 || parseErr.Column == 0 {
		t.Fatalf("expected line/column, got %d:%d", parseErr.Line, parseErr.Column)
	}
	if !strings.Contains(parseErr.Snippet, `return <div>{</div>`) {
		t.Fatalf("expected source snippet, got %q", parseErr.Snippet)
	}
}

func TestTranspileLocalTypedComponentProps(t *testing.T) {
	source := []byte(`package main

type Node = gosx.Node

type ChildProps struct {
	Label string
	Disabled bool
}

func Parent(active bool) Node {
	return <Child Label="Run" Disabled={active} />
}

func Child(props ChildProps) Node {
	return <button disabled={props.Disabled}>{props.Label}</button>
}
`)

	out, err := Transpile(source, Options{SourceFile: "typed_props.gsx"})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	if !strings.Contains(out, `Child(ChildProps{Label: "Run", Disabled: active})`) {
		t.Fatalf("expected typed component props literal, got:\n%s", out)
	}
	if strings.Contains(out, `Child(gosx.Props`) {
		t.Fatalf("typed local component should not receive gosx.Props attr list:\n%s", out)
	}
}

func TestTranspileAttrListComponentKeepsPropsList(t *testing.T) {
	source := []byte(`package main

type Node = gosx.Node

func Parent() Node {
	return <Box class="panel" />
}

func Box(attrs gosx.AttrList) Node {
	return <div />
}
`)

	out, err := Transpile(source, Options{SourceFile: "attr_list.gsx"})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	if !strings.Contains(out, `Box(gosx.Props(gosx.Attr("class", "panel")))`) {
		t.Fatalf("expected attr-list component to keep gosx.Props, got:\n%s", out)
	}
}

func TestTranspileTypedComponentRejectsSpreadAttrs(t *testing.T) {
	source := []byte(`package main

type Node = gosx.Node

type ChildProps struct {
	Label string
}

func Parent(attrs map[string]any) Node {
	return <Child {...attrs} />
}

func Child(props ChildProps) Node {
	return <div />
}
`)

	_, err := Transpile(source, Options{SourceFile: "typed_spread.gsx"})
	if err == nil {
		t.Fatal("expected typed component spread attrs to fail")
	}
	if !strings.Contains(err.Error(), "spread attributes are not supported for typed component props") {
		t.Fatalf("expected typed spread attr error, got: %v", err)
	}
}

func TestTranspileMemberComponentCall(t *testing.T) {
	source := []byte(`package main

type Node = gosx.Node

func Parent() Node {
	return <Demo.ThemeSwitcher />
}
`)

	out, err := Transpile(source, Options{SourceFile: "member_component.gsx"})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	if !strings.Contains(out, `Demo.ThemeSwitcher()`) {
		t.Fatalf("expected dotted component call, got:\n%s", out)
	}
	if strings.Contains(out, `gosx.El("Demo.ThemeSwitcher"`) {
		t.Fatalf("dotted component should not become a literal element:\n%s", out)
	}
}

func TestTranspileGoSXUIImportedComponentProps(t *testing.T) {
	source := []byte(`package main

import (
	"github.com/odvcencio/gosx"
	ui "github.com/odvcencio/gosx/ui"
)

func View(disabled bool) gosx.Node {
	return <ui.Button type="submit" variant="secondary" size="sm" class="cta" disabled={disabled} data-action="run">Run</ui.Button>
}
`)

	out, err := Transpile(source, Options{SourceFile: "gosx_ui.gsx"})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	expected := `ui.Button(ui.ButtonProps{BaseProps: ui.BaseProps{Class: "cta", Attrs: gosx.Attrs(gosx.Attr("data-action", "run"))}, Type: "submit", Variant: "secondary", Size: "sm", Disabled: disabled}, gosx.Text("Run"))`
	if !strings.Contains(out, expected) {
		t.Fatalf("expected typed GoSX UI component props, got:\n%s", out)
	}
}
