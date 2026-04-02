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

	if !strings.Contains(scoped, `:where([data-gosx-s="c8a3"]) .button`) {
		t.Fatalf("expected scoped .button selector, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `.button:where([data-gosx-s="c8a3"])`) {
		t.Fatalf("expected root-safe .button selector, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `:where([data-gosx-s="c8a3"]) .label`) {
		t.Fatalf("expected scoped .label selector, got:\n%s", scoped)
	}
}

func TestScopeCSSMultipleSelectors(t *testing.T) {
	css := `.a, .b, .c { display: flex; }`
	scoped := ScopeCSS(css, "x1y2")

	if !strings.Contains(scoped, `:where([data-gosx-s="x1y2"]) .a`) {
		t.Fatal("expected scoped .a")
	}
	if !strings.Contains(scoped, `:where([data-gosx-s="x1y2"]) .b`) {
		t.Fatal("expected scoped .b")
	}
	if !strings.Contains(scoped, `:where([data-gosx-s="x1y2"]) .c`) {
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

func TestScopeCSSSupportsRootSelectors(t *testing.T) {
	css := `:root { --card-gap: 1rem; }
.shell > .card { gap: var(--card-gap); }`

	scoped := ScopeCSS(css, "root1")

	if !strings.Contains(scoped, `:root { --card-gap: 1rem; }`) {
		t.Fatalf("expected :root to pass through unscoped, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `:where([data-gosx-s="root1"]) .shell > .card, .shell:where([data-gosx-s="root1"]) > .card`) {
		t.Fatalf("expected direct-child selector to preserve root matching, got:\n%s", scoped)
	}
}

func TestScopeCSSScopesNestedAtRules(t *testing.T) {
	css := `@media (min-width: 768px) {
  .shell main { max-width: 60rem; }
}
@supports (display: grid) {
  .grid { display: grid; }
}`

	scoped := ScopeCSS(css, "nest1")

	if !strings.Contains(scoped, `@media (min-width: 768px) {
  :where([data-gosx-s="nest1"]) .shell main, .shell:where([data-gosx-s="nest1"]) main { max-width: 60rem; }
}`) {
		t.Fatalf("expected nested @media selectors to be scoped, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `@supports (display: grid) {
  :where([data-gosx-s="nest1"]) .grid, .grid:where([data-gosx-s="nest1"]) { display: grid; }
}`) {
		t.Fatalf("expected nested @supports selectors to be scoped, got:\n%s", scoped)
	}
}

func TestScopeCSSPreservesKeyframesAndFontFace(t *testing.T) {
	css := `@keyframes float {
  from { opacity: 0; }
  to { opacity: 1; }
}
@font-face {
  font-family: Demo;
  src: url("/demo.woff2");
}`

	scoped := ScopeCSS(css, "anim1")

	if strings.Contains(scoped, `:where([data-gosx-s="anim1"]) from`) {
		t.Fatalf("expected keyframe steps to stay unscoped, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `@font-face {
  font-family: Demo;`) {
		t.Fatalf("expected @font-face to be preserved, got:\n%s", scoped)
	}
}

func TestScopeCSSSupportsGlobalSelectors(t *testing.T) {
	css := `:global(body[data-theme="docs"]) { background: #08151f; }
.copy :global(a) { color: inherit; }`

	scoped := ScopeCSS(css, "glob1")

	if !strings.Contains(scoped, `body[data-theme="docs"] { background: #08151f; }`) {
		t.Fatalf("expected standalone :global selector to remain global, got:\n%s", scoped)
	}
	if !strings.Contains(scoped, `:where([data-gosx-s="glob1"]) .copy a, .copy:where([data-gosx-s="glob1"]) a`) {
		t.Fatalf("expected inline :global(...) wrapper to unwrap inside scoped selector, got:\n%s", scoped)
	}
}

func TestScopeCSSInherentlyGlobalSelectors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "body passes through unscoped",
			input:    "body { margin: 0; }",
			expected: "body { margin: 0; }",
		},
		{
			name:     "html passes through unscoped",
			input:    "html { scroll-behavior: smooth; }",
			expected: "html { scroll-behavior: smooth; }",
		},
		{
			name:     "universal selectors pass through unscoped",
			input:    "*, *::before, *::after { box-sizing: border-box; }",
			expected: "*,  *::before,  *::after { box-sizing: border-box; }",
		},
		{
			name:     "::selection passes through unscoped",
			input:    "::selection { background: gold; }",
			expected: "::selection { background: gold; }",
		},
		{
			name:     ":root passes through unscoped",
			input:    ":root { --color: red; }",
			expected: ":root { --color: red; }",
		},
		{
			name:     "class selector is still scoped",
			input:    ".my-class { color: red; }",
			expected: `:where([data-gosx-s="g1"]) .my-class`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := ScopeCSS(tt.input, "g1")
			if !strings.Contains(scoped, tt.expected) {
				t.Fatalf("expected output to contain %q, got:\n%s", tt.expected, scoped)
			}
		})
	}
}
