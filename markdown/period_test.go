package markdown

import (
	"strings"
	"testing"
)

func TestParagraphWithPeriod(t *testing.T) {
	r := NewRenderer(WithHardWraps(true))
	html := r.RenderString("This is the way.")
	if !strings.Contains(html, "This is the way.") {
		t.Fatalf("period dropped text, got: %q", html)
	}
}

func TestHeadingThenParagraphWithPeriod(t *testing.T) {
	r := NewRenderer(WithHardWraps(true))
	html := r.RenderString("# Preview\n\nThis is the way.")
	if !strings.Contains(html, "This is the way.") {
		t.Fatalf("paragraph text lost, got: %q", html)
	}
	if !strings.Contains(html, "Preview") {
		t.Fatalf("heading lost, got: %q", html)
	}
}

func TestMultipleSentences(t *testing.T) {
	r := NewRenderer()
	html := r.RenderString("Hello world. This is a test. Final sentence.")
	if !strings.Contains(html, "Hello world.") {
		t.Fatalf("first sentence lost, got: %q", html)
	}
	if !strings.Contains(html, "Final sentence.") {
		t.Fatalf("last sentence lost, got: %q", html)
	}
}

func TestParagraphWithComma(t *testing.T) {
	r := NewRenderer()
	html := r.RenderString("Hello, world")
	if !strings.Contains(html, "Hello, world") {
		t.Fatalf("comma text lost, got: %q", html)
	}
}

func TestParagraphWithExclamation(t *testing.T) {
	r := NewRenderer()
	html := r.RenderString("Hello world!")
	if !strings.Contains(html, "Hello world!") {
		t.Fatalf("exclamation text lost, got: %q", html)
	}
}
