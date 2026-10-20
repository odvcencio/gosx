package collab

import (
	"strings"
	"testing"
)

func TestDocumentPreviewAndCursorUnitsFollowTheCommittedSnapshot(t *testing.T) {
	doc := NewDoc("# Hello 🌊\n")
	before := doc.State()
	if !strings.Contains(before.HTML, "<h1") || !strings.Contains(before.HTML, "data-mdpp-source-line") {
		t.Fatalf("preview missing Markdown or source positions: %s", before.HTML)
	}
	if doc.UTF16Len() != len("# Hello ")+2+1 {
		t.Fatalf("UTF-16 length = %d", doc.UTF16Len())
	}
	after, ok := doc.Apply("**New** 🚀", before.Version)
	if !ok || !strings.Contains(after.HTML, "<strong>New</strong>") || doc.UTF16Len() != len("**New** ")+2 {
		t.Fatalf("committed snapshot: %+v, length %d", after, doc.UTF16Len())
	}
	stale, ok := doc.Apply("stale", 0)
	if ok || stale.HTML != after.HTML || stale.Text != after.Text || doc.UTF16Len() != len("**New** ")+2 {
		t.Fatal("stale edit changed the preview or cursor bounds")
	}
}

func TestDocumentPreviewDoesNotExecuteAuthoredHTMLOrScriptLinks(t *testing.T) {
	state := NewDoc("text\n\n<script>alert(1)</script>\n\n[x](javascript:alert%281%29)\n").State()
	if strings.Contains(state.HTML, "<script") || strings.Contains(state.HTML, `href="javascript:`) {
		t.Fatalf("unsafe Markdown preview: %s", state.HTML)
	}
}
