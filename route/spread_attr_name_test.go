package route

import (
	"strings"
	"testing"

	"golang.org/x/net/html"

	"m31labs.dev/gosx"
)

// TestSpreadAttrNameSmugglingDropped is gosx#189's exact P7c probe shape
// (found on the gosx#185 review, scratchpad/probes185/p7_htmlparse_test.go):
// a {...spread} whose map key is `x onmouseover=alert(1) y`.
// html.EscapeString does not escape a space or an "=", so before this fix
// normalizeFileAttrName passed the key through unchanged and the renderer
// wrote three attributes — x, onmouseover=alert(1), and y — from one map
// entry. The fix drops an invalid spread key inertly: the render still
// succeeds, but none of the smuggled attributes appear.
func TestSpreadAttrNameSmugglingDropped(t *testing.T) {
	prog, err := gosx.Compile([]byte("package main\n\nfunc Page() Node {\n\treturn <div {...extra}>x</div>\n}\n"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"extra": map[string]any{`x onmouseover=alert(1) y`: "v"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, "onmouseover") {
		t.Fatalf("smuggled onmouseover attribute reached output: %s", out)
	}

	doc, err := html.Parse(strings.NewReader("<!doctype html><html><body>" + out + "</body></html>"))
	if err != nil {
		t.Fatalf("parse rendered HTML: %v", err)
	}
	div := findHTMLNode(doc, "div")
	if div == nil {
		t.Fatalf("no <div> in rendered output: %s", out)
	}
	for _, a := range div.Attr {
		if a.Key == "x" || a.Key == "onmouseover" || a.Key == "y" {
			t.Fatalf("smuggled attribute %q survived as its own name: %s", a.Key, out)
		}
	}
}

// TestSpreadAttrNameValidNamesStillPass proves the drop in
// TestSpreadAttrNameSmugglingDropped is name-specific, not a blanket spread
// failure: a spread map with only valid attribute names renders every one
// of them.
func TestSpreadAttrNameValidNamesStillPass(t *testing.T) {
	prog, err := gosx.Compile([]byte("package main\n\nfunc Page() Node {\n\treturn <div {...extra}>x</div>\n}\n"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"extra": map[string]any{
			"data-testid": "widget",
			"aria-label":  "Widget",
			"title":       "A widget",
		}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{`data-testid="widget"`, `aria-label="Widget"`, `title="A widget"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("valid spread attribute %q missing from output: %s", want, out)
		}
	}
}

// TestSpreadAttrNameEmptyAndWhitespaceDropped proves an empty or
// whitespace-only spread key is dropped rather than emitted as a bare or
// malformed attribute (gosx#185 n4's rule, shared here by validRenderAttrName).
func TestSpreadAttrNameEmptyAndWhitespaceDropped(t *testing.T) {
	prog, err := gosx.Compile([]byte("package main\n\nfunc Page() Node {\n\treturn <div {...extra}>x</div>\n}\n"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"extra": map[string]any{
			"":      "v1",
			"   ":   "v2",
			"valid": "v3",
		}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, `valid="v3"`) {
		t.Fatalf("valid spread attribute missing from output: %s", out)
	}
	if strings.Contains(out, "v1") || strings.Contains(out, "v2") {
		t.Fatalf("empty or whitespace-only spread key was not dropped: %s", out)
	}
}

func findHTMLNode(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if got := findHTMLNode(c, tag); got != nil {
			return got
		}
	}
	return nil
}
