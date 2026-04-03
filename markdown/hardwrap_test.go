package markdown

import (
	"strings"
	"testing"
)

func TestHardWrapNewlines(t *testing.T) {
	r := NewRenderer(WithHardWraps(true))
	html := r.RenderString("line one\nline two\nline three")
	if !strings.Contains(html, "line one<br />\nline two<br />\nline three") {
		t.Fatalf("expected <br> between lines, got: %q", html)
	}
}

func TestHardWrapAfterHeading(t *testing.T) {
	r := NewRenderer(WithHardWraps(true))
	html := r.RenderString("# Preview\nWhat else is good.\nthis is the way.")
	if !strings.Contains(html, "good.<br") || !strings.Contains(html, "way.") {
		t.Fatalf("expected <br> between paragraph lines, got: %q", html)
	}
}
