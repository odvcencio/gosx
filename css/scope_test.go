package css

import (
	"strings"
	"testing"
)

func TestScopeID(t *testing.T) {
	id := ScopeID("Counter")
	if len(id) != 4 {
		t.Fatalf("expected 4-char scope ID, got %q", id)
	}
	// Deterministic
	if ScopeID("Counter") != id {
		t.Fatal("scope ID should be deterministic")
	}
	// Different for different components
	if ScopeID("Counter") == ScopeID("Tabs") {
		t.Fatal("different components should have different scope IDs")
	}
}

func TestScopeCSS(t *testing.T) {
	css := `.button { color: red; }
.label { font-weight: bold; }`

	scoped := ScopeCSS(css, "c8a3")

	if !strings.Contains(scoped, `[data-gosx-s="c8a3"] .button`) {
		t.Fatalf("expected scoped .button selector, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `[data-gosx-s="c8a3"] .label`) {
		t.Fatalf("expected scoped .label selector, got:\n%s", scoped)
	}
}

func TestScopeCSSMultipleSelectors(t *testing.T) {
	css := `.a, .b, .c { display: flex; }`
	scoped := ScopeCSS(css, "x1y2")

	if !strings.Contains(scoped, `[data-gosx-s="x1y2"] .a`) {
		t.Fatal("expected scoped .a")
	}
	if !strings.Contains(scoped, `[data-gosx-s="x1y2"] .b`) {
		t.Fatal("expected scoped .b")
	}
	if !strings.Contains(scoped, `[data-gosx-s="x1y2"] .c`) {
		t.Fatal("expected scoped .c")
	}
}

func TestScopeCSSPreservesProperties(t *testing.T) {
	css := `.card {
  background: white;
  padding: 1rem;
}`
	scoped := ScopeCSS(css, "abcd")

	if !strings.Contains(scoped, "background: white") {
		t.Fatal("expected properties preserved")
	}
	if !strings.Contains(scoped, "padding: 1rem") {
		t.Fatal("expected padding preserved")
	}
}

func TestScopeCSSPreservesComments(t *testing.T) {
	css := `/* Component styles */
.container { margin: 0; }`
	scoped := ScopeCSS(css, "1234")

	if !strings.Contains(scoped, "/* Component styles */") {
		t.Fatal("expected comment preserved")
	}
}

func TestScopeAttr(t *testing.T) {
	attr := ScopeAttr("c8a3")
	if attr != `data-gosx-s="c8a3"` {
		t.Fatalf("expected data-gosx-s attr, got %q", attr)
	}
}

func TestScopeCSSNoCollision(t *testing.T) {
	// Two components with same class name get different scopes
	css1 := ScopeCSS(".button { color: red; }", ScopeID("Counter"))
	css2 := ScopeCSS(".button { color: blue; }", ScopeID("Form"))

	// They should have different scope attributes
	scope1 := ScopeID("Counter")
	scope2 := ScopeID("Form")

	if scope1 == scope2 {
		t.Fatal("different components should have different scope IDs")
	}

	if !strings.Contains(css1, scope1) {
		t.Fatal("css1 should contain Counter scope")
	}
	if !strings.Contains(css2, scope2) {
		t.Fatal("css2 should contain Form scope")
	}
}
