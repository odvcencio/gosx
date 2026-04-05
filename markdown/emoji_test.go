package markdown

import (
	"strings"
	"testing"
)

func TestEmojiShortcode(t *testing.T) {
	html := NewRenderer().RenderString("Hello :fire: world")
	if !strings.Contains(html, "\U0001f525") {
		t.Errorf("expected fire emoji, got: %s", html)
	}
}

func TestEmojiMultiple(t *testing.T) {
	html := NewRenderer().RenderString(":rocket: Launch :tada:")
	if !strings.Contains(html, "\U0001f680") || !strings.Contains(html, "\U0001f389") {
		t.Errorf("expected rocket and tada, got: %s", html)
	}
}

func TestEmojiUnknownLeftAsText(t *testing.T) {
	html := NewRenderer().RenderString("Hello :notanemoji: world")
	if !strings.Contains(html, ":notanemoji:") {
		t.Errorf("expected unknown shortcode preserved, got: %s", html)
	}
}

func TestEmojiInline(t *testing.T) {
	html := NewRenderer().RenderString("**bold :heart: text**")
	if !strings.Contains(html, "\u2764") {
		t.Errorf("expected heart in bold, got: %s", html)
	}
}

func TestEmojiWrapped(t *testing.T) {
	r := NewRenderer(WithWrapEmoji(true))
	html := r.RenderString("Hello :fire:")
	if !strings.Contains(html, `role="img"`) {
		t.Errorf("expected wrapped emoji with role=img, got: %s", html)
	}
	if !strings.Contains(html, `aria-label="fire"`) {
		t.Errorf("expected aria-label, got: %s", html)
	}
}

func TestEmojiPlusOne(t *testing.T) {
	html := NewRenderer().RenderString(":+1: :thumbsup:")
	// Both should resolve to 👍
	if !strings.Contains(html, "\U0001f44d") {
		t.Errorf("expected thumbsup emoji, got: %s", html)
	}
}

func TestEmojiInCodeSpanNotProcessed(t *testing.T) {
	html := NewRenderer().RenderString("Use `:fire:` for flames")
	// Inside code span, should remain as text
	if strings.Contains(html, "\U0001f525") {
		t.Errorf("emoji should not be processed inside code spans, got: %s", html)
	}
}
