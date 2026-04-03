package markdown

import (
	"strings"
	"testing"
)

func assertContains(t *testing.T, html, substr string) {
	t.Helper()
	if !strings.Contains(html, substr) {
		t.Fatalf("expected %q in:\n%s", substr, html)
	}
}

func assertNotContains(t *testing.T, html, substr string) {
	t.Helper()
	if strings.Contains(html, substr) {
		t.Fatalf("did not expect %q in:\n%s", substr, html)
	}
}

// --- Admonitions ---

func TestAdmonitionNote(t *testing.T) {
	html := NewRenderer().RenderString("> [!NOTE]\n> This is a note")
	assertContains(t, html, `class="admonition admonition-note"`)
	assertContains(t, html, `class="admonition-title"`)
	assertContains(t, html, "NOTE")
	assertContains(t, html, "This is a note")
}

func TestAdmonitionWarning(t *testing.T) {
	html := NewRenderer().RenderString("> [!WARNING]\n> Be careful")
	assertContains(t, html, `admonition-warning`)
	assertContains(t, html, "Be careful")
}

func TestAdmonitionTip(t *testing.T) {
	html := NewRenderer().RenderString("> [!TIP]\n> A helpful tip")
	assertContains(t, html, `admonition-tip`)
	assertContains(t, html, "A helpful tip")
}

func TestAdmonitionImportant(t *testing.T) {
	html := NewRenderer().RenderString("> [!IMPORTANT]\n> Very important")
	assertContains(t, html, `admonition-important`)
}

func TestAdmonitionCaution(t *testing.T) {
	html := NewRenderer().RenderString("> [!CAUTION]\n> Proceed with caution")
	assertContains(t, html, `admonition-caution`)
}

func TestBlockquoteNotAdmonition(t *testing.T) {
	html := NewRenderer().RenderString("> Just a normal quote")
	assertContains(t, html, "<blockquote>")
	assertNotContains(t, html, "admonition")
}

// --- Task Lists ---

func TestTaskListChecked(t *testing.T) {
	html := NewRenderer().RenderString("- [x] Done\n- [ ] Todo")
	assertContains(t, html, `type="checkbox"`)
	assertContains(t, html, `checked`)
	assertContains(t, html, `class="task-list-item"`)
	assertContains(t, html, "Done")
	assertContains(t, html, "Todo")
}

func TestTaskListUnchecked(t *testing.T) {
	html := NewRenderer().RenderString("- [ ] Not done")
	assertContains(t, html, `class="task-list-item"`)
	assertContains(t, html, `disabled`)
	assertContains(t, html, "Not done")
}

func TestNormalListNotTask(t *testing.T) {
	html := NewRenderer().RenderString("- Normal item")
	assertContains(t, html, "<li>")
	assertNotContains(t, html, "task-list-item")
}

// --- Footnotes ---

func TestFootnote(t *testing.T) {
	html := NewRenderer().RenderString("Text[^1]\n\n[^1]: Footnote content")
	assertContains(t, html, `class="footnote-ref"`)
	assertContains(t, html, `href="#fn-1"`)
	assertContains(t, html, `id="fnref-1"`)
	assertContains(t, html, "Footnote content")
	assertContains(t, html, `class="footnotes"`)
}

func TestFootnoteMultiple(t *testing.T) {
	html := NewRenderer().RenderString("A[^a] B[^b]\n\n[^a]: First\n\n[^b]: Second")
	assertContains(t, html, `href="#fn-a"`)
	assertContains(t, html, `href="#fn-b"`)
	assertContains(t, html, "First")
	assertContains(t, html, "Second")
}

// --- Math ---

func TestMathInline(t *testing.T) {
	html := NewRenderer().RenderString("The formula $E = mc^2$ is famous")
	assertContains(t, html, `class="math-inline"`)
	assertContains(t, html, `E = mc^2`)
}

func TestMathBlock(t *testing.T) {
	html := NewRenderer().RenderString("$$E = mc^2$$")
	assertContains(t, html, `class="math-block"`)
	assertContains(t, html, `E = mc^2`)
}

func TestMathNotTriggeredInCode(t *testing.T) {
	// Dollar signs inside code spans should not be treated as math
	html := NewRenderer().RenderString("`$not math$`")
	assertNotContains(t, html, "math-inline")
}

// --- Superscript / Subscript ---

func TestSuperscript(t *testing.T) {
	html := NewRenderer().RenderString("x^2^")
	assertContains(t, html, "<sup>2</sup>")
}

func TestSubscript(t *testing.T) {
	html := NewRenderer().RenderString("H~2~O")
	assertContains(t, html, "<sub>2</sub>")
}

func TestSuperscriptInSentence(t *testing.T) {
	html := NewRenderer().RenderString("The value is x^n^ where n is large")
	assertContains(t, html, "<sup>n</sup>")
	assertContains(t, html, "The value is x")
	assertContains(t, html, " where n is large")
}

func TestSubscriptInSentence(t *testing.T) {
	html := NewRenderer().RenderString("Water is H~2~O")
	assertContains(t, html, "<sub>2</sub>")
	assertContains(t, html, "Water is H")
}

// --- Strikethrough (GFM, should already work via tree-sitter) ---

func TestStrikethrough(t *testing.T) {
	html := NewRenderer().RenderString("~~deleted~~")
	assertContains(t, html, "<del>")
	assertContains(t, html, "deleted")
}
