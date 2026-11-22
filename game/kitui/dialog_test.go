package kitui

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/markup"
)

func TestDialogHasItsPartsByID(t *testing.T) {
	html := string(markup.RenderHTML(Dialog(DialogProps{ID: "dialog", Title: "Football", CloseID: "close",
		BodyID: "content", FootID: "foot", HeadClass: "dhead", FootClass: "dfoot"})))
	for _, want := range []string{
		`<dialog id="dialog" class="kit-dialog">`,
		`<div class="kit-dialog-head dhead"><span>Football</span>`,
		`<button type="button" id="close" class="kit-dialog-close" aria-label="Close">×</button>`,
		`<div id="content" class="kit-dialog-body"></div>`,
		`<div id="foot" class="kit-dialog-foot dfoot" hidden></div>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in\n%s", want, html)
		}
	}
	// The footer comes after the body, outside the part that scrolls.
	if strings.Index(html, `id="foot"`) < strings.Index(html, `id="content"`) {
		t.Errorf("the footer must follow the body:\n%s", html)
	}
}

func TestDialogLeavesOutTheCloseButtonAndFooterWhenNotNamed(t *testing.T) {
	html := string(markup.RenderHTML(Dialog(DialogProps{ID: "d", Title: "T", BodyID: "b"})))
	if strings.Contains(html, "<button") || strings.Contains(html, "kit-dialog-foot") {
		t.Fatalf("a dialog with no CloseID or FootID has no close button or footer:\n%s", html)
	}
}
