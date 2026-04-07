package highlight

import "testing"

func TestHighlight_Markdown(t *testing.T) {
	h := New(LangMarkdown)
	decorations := h.Highlight([]byte("# Hello\n\nSome **bold** text"))

	if len(decorations) == 0 {
		t.Fatal("expected decorations for markdown content")
	}

	// Line 0 should have heading decorations
	line0 := decorations[0]
	found := false
	for _, d := range line0 {
		if d.Class == "hl-heading" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected heading decoration on line 0")
	}
}

func TestHighlight_Incremental(t *testing.T) {
	h := New(LangMarkdown)
	h.Highlight([]byte("# Hello\n\nworld"))

	decorations := h.HighlightIncremental([]byte("# Hello\n\nnew world"), EditRange{
		StartLine: 2, StartCol: 0,
		OldEndLine: 2, OldEndCol: 0,
		NewEndLine: 2, NewEndCol: 4,
	})

	if len(decorations) == 0 {
		t.Fatal("incremental highlight should return decorations")
	}
}
