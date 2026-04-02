package markdown

import (
	"strings"
	"testing"
)

func TestHighlightGoCode(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```go\nfunc main() {}\n```")
	// Should contain highlighted spans
	if !strings.Contains(html, "<span") {
		t.Fatalf("expected highlighted spans, got: %s", html)
	}
	if !strings.Contains(html, "func") {
		t.Fatalf("expected 'func' in output")
	}
}

func TestHighlightUnknownLanguage(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```unknownlang\nhello\n```")
	if !strings.Contains(html, "hello") {
		t.Fatal("expected unhighlighted content")
	}
}

func TestHighlightDisabled(t *testing.T) {
	r := NewRenderer(WithHighlightCode(false))
	html := r.RenderString("```go\nfunc main() {}\n```")
	if strings.Contains(html, `class="hl-`) {
		t.Fatal("expected no highlighting when disabled")
	}
}

func TestHighlightPython(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```python\ndef hello():\n    pass\n```")
	if !strings.Contains(html, "<span") {
		t.Fatalf("expected highlighted Python, got: %s", html)
	}
}
