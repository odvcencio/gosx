package prose

import "testing"

func TestStyleCSSVariablesUsesDefaults(t *testing.T) {
	got := (Style{}).CSSVariables()
	want := "--gosx-prose-size:1rem;--gosx-prose-leading:1.65;--gosx-prose-flow:1rem;"
	if got != want {
		t.Fatalf("CSSVariables() = %q, want %q", got, want)
	}
}

func TestStyleCSSVariablesAllowsResponsiveSize(t *testing.T) {
	got := (Style{Size: "clamp(1rem, 2vw, 1.25rem)", Leading: "1.7", Flow: "1.1rem"}).CSSVariables()
	if got != "--gosx-prose-size:clamp(1rem, 2vw, 1.25rem);--gosx-prose-leading:1.7;--gosx-prose-flow:1.1rem;" {
		t.Fatalf("CSSVariables() = %q", got)
	}
}

func TestStyleCSSVariablesRejectsDeclarationEscape(t *testing.T) {
	got := (Style{Size: "1rem; color:red"}).CSSVariables()
	if got != "--gosx-prose-size:1rem;--gosx-prose-leading:1.65;--gosx-prose-flow:1rem;" {
		t.Fatalf("CSSVariables() = %q", got)
	}
}
