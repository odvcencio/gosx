package markdown

import (
	"strings"
	"testing"
)

func TestRenderHeadings(t *testing.T) {
	tests := []struct {
		src      string
		contains string
	}{
		{"# H1", "<h1>H1</h1>"},
		{"## H2", "<h2>H2</h2>"},
		{"### H3", "<h3>H3</h3>"},
		{"#### H4", "<h4>H4</h4>"},
		{"##### H5", "<h5>H5</h5>"},
		{"###### H6", "<h6>H6</h6>"},
	}
	r := NewRenderer()
	for _, tt := range tests {
		out := r.RenderString(tt.src)
		if !strings.Contains(out, tt.contains) {
			t.Errorf("RenderString(%q) = %q, want to contain %q", tt.src, out, tt.contains)
		}
	}
}

func TestRenderParagraph(t *testing.T) {
	out := NewRenderer().RenderString("Hello world")
	if !strings.Contains(out, "<p>Hello world</p>") {
		t.Errorf("expected <p>Hello world</p>, got %q", out)
	}
}

func TestRenderBoldItalic(t *testing.T) {
	out := NewRenderer().RenderString("**bold** and *italic*")
	if !strings.Contains(out, "<strong>bold</strong>") {
		t.Errorf("expected <strong>bold</strong>, got %q", out)
	}
	if !strings.Contains(out, "<em>italic</em>") {
		t.Errorf("expected <em>italic</em>, got %q", out)
	}
}

func TestRenderCodeBlock(t *testing.T) {
	src := "```go\nfmt.Println(\"hello\")\n```"
	out := NewRenderer().RenderString(src)
	if !strings.Contains(out, "<pre><code class=\"language-go\">") {
		t.Errorf("expected <pre><code class=\"language-go\">, got %q", out)
	}
	if !strings.Contains(out, "</code></pre>") {
		t.Errorf("expected </code></pre>, got %q", out)
	}
}

func TestRenderCodeBlockNoLang(t *testing.T) {
	src := "```\nsome code\n```"
	out := NewRenderer().RenderString(src)
	if !strings.Contains(out, "<pre><code>") {
		t.Errorf("expected <pre><code>, got %q", out)
	}
}

func TestRenderBlockquote(t *testing.T) {
	out := NewRenderer().RenderString("> quoted text")
	if !strings.Contains(out, "<blockquote>") {
		t.Errorf("expected <blockquote>, got %q", out)
	}
	if !strings.Contains(out, "</blockquote>") {
		t.Errorf("expected </blockquote>, got %q", out)
	}
}

func TestRenderList(t *testing.T) {
	out := NewRenderer().RenderString("- one\n- two")
	if !strings.Contains(out, "<ul>") {
		t.Errorf("expected <ul>, got %q", out)
	}
	if !strings.Contains(out, "<li>") {
		t.Errorf("expected <li>, got %q", out)
	}
	if !strings.Contains(out, "</li>") {
		t.Errorf("expected </li>, got %q", out)
	}
	if !strings.Contains(out, "</ul>") {
		t.Errorf("expected </ul>, got %q", out)
	}
}

func TestRenderLink(t *testing.T) {
	out := NewRenderer().RenderString("[click me](https://example.com)")
	if !strings.Contains(out, `<a href="https://example.com">click me</a>`) {
		t.Errorf("expected link, got %q", out)
	}
}

func TestRenderImageWithTitle(t *testing.T) {
	out := NewRenderer().RenderString(`![alt text](image.png "My Title")`)
	if !strings.Contains(out, "<figure>") {
		t.Errorf("expected <figure>, got %q", out)
	}
	if !strings.Contains(out, `<img src="image.png" alt="alt text" />`) {
		t.Errorf("expected img tag, got %q", out)
	}
	if !strings.Contains(out, "<figcaption>My Title</figcaption>") {
		t.Errorf("expected figcaption, got %q", out)
	}
}

func TestRenderImageWithoutTitle(t *testing.T) {
	out := NewRenderer().RenderString("![alt text](image.png)")
	if !strings.Contains(out, `<img src="image.png" alt="alt text" />`) {
		t.Errorf("expected img tag without figure, got %q", out)
	}
	if strings.Contains(out, "<figure>") {
		t.Errorf("did not expect <figure> without title, got %q", out)
	}
}

func TestRenderTable(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |"
	out := NewRenderer().RenderString(src)
	if !strings.Contains(out, "<table>") {
		t.Errorf("expected <table>, got %q", out)
	}
	if !strings.Contains(out, "<thead>") {
		t.Errorf("expected <thead>, got %q", out)
	}
	if !strings.Contains(out, "<th>A</th>") {
		t.Errorf("expected <th>A</th>, got %q", out)
	}
	if !strings.Contains(out, "<tbody>") {
		t.Errorf("expected <tbody>, got %q", out)
	}
	if !strings.Contains(out, "<td>1</td>") {
		t.Errorf("expected <td>1</td>, got %q", out)
	}
}

func TestRenderHeadingIDs(t *testing.T) {
	r := NewRenderer(WithHeadingIDs(true))
	out := r.RenderString("# Hello World")
	if !strings.Contains(out, `<h1 id="hello-world">Hello World</h1>`) {
		t.Errorf("expected heading with id, got %q", out)
	}
}

func TestRenderUnsafeHTMLDefault(t *testing.T) {
	// By default, raw HTML should be escaped.
	// HTML blocks require preceding content for tree-sitter to recognise them.
	src := "text\n\n<div>raw html</div>\n"
	out := NewRenderer().RenderString(src)
	if strings.Contains(out, "<div>raw html</div>") {
		t.Errorf("expected HTML to be escaped by default, got %q", out)
	}
	if !strings.Contains(out, "&lt;div&gt;") {
		t.Errorf("expected escaped HTML, got %q", out)
	}
}

func TestRenderUnsafeHTMLEnabled(t *testing.T) {
	src := "text\n\n<div>raw html</div>\n"
	r := NewRenderer(WithUnsafeHTML(true))
	out := r.RenderString(src)
	if !strings.Contains(out, "<div>raw html</div>") {
		t.Errorf("expected raw HTML passthrough, got %q", out)
	}
}

func TestRenderHTMLEscaping(t *testing.T) {
	out := NewRenderer().RenderString("Use <script> & \"quotes\"")
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped <script>, got %q", out)
	}
	if !strings.Contains(out, "&amp;") {
		t.Errorf("expected escaped &, got %q", out)
	}
	if !strings.Contains(out, "&#34;quotes&#34;") && !strings.Contains(out, "&quot;quotes&quot;") {
		t.Errorf("expected escaped quotes, got %q", out)
	}
}

func TestRenderThematicBreak(t *testing.T) {
	// Thematic break requires preceding content for tree-sitter to recognise it.
	out := NewRenderer().RenderString("text\n\n---\n")
	if !strings.Contains(out, "<hr />") {
		t.Errorf("expected <hr />, got %q", out)
	}
}

func TestRenderCodeSpan(t *testing.T) {
	out := NewRenderer().RenderString("Use `fmt.Println` here")
	if !strings.Contains(out, "<code>fmt.Println</code>") {
		t.Errorf("expected <code>fmt.Println</code>, got %q", out)
	}
}

func TestRenderStrikethrough(t *testing.T) {
	out := NewRenderer().RenderString("~~deleted~~")
	if !strings.Contains(out, "<del>deleted</del>") {
		t.Errorf("expected <del>deleted</del>, got %q", out)
	}
}

func TestRenderStringPackageLevel(t *testing.T) {
	out := RenderString("# Test")
	if !strings.Contains(out, "<h1>Test</h1>") {
		t.Errorf("expected <h1>Test</h1>, got %q", out)
	}
}

func TestRenderImageResolver(t *testing.T) {
	resolver := func(src string) string {
		return "/cdn/" + src
	}
	r := NewRenderer(WithImageResolver(resolver))
	out := r.RenderString("![alt](photo.jpg)")
	if !strings.Contains(out, `src="/cdn/photo.jpg"`) {
		t.Errorf("expected resolved src, got %q", out)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello-world"},
		{"API Reference", "api-reference"},
		{"  spaces  ", "spaces"},
		{"special!@#chars", "specialchars"},
		{"already-slugified", "already-slugified"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.expected {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
