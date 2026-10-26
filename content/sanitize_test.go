package content

import (
	"strings"
	"testing"
)

func TestSanitizedMarkdownRejectsActiveURLsAndMarkup(t *testing.T) {
	for _, source := range []string{
		`<a href="javascript:alert(1)">link</a>`, `<a href="java&#x09;script:alert(1)">link</a>`,
		`<a href="&#x6a;avascript:alert(1)" onclick="bad()">link</a>`, `<img src="data:image/svg+xml,bad" onerror="bad()">`,
		`<svg><a href="javascript:bad()">bad</a></svg>`, `<math><mtext><img src=x onerror=bad()></mtext></math>`,
		`<script>bad()</script><style>bad{}</style><iframe srcdoc="bad()"></iframe>`,
		`<form action="/mutate"><input type=submit></form><div data-gosx-action="/mutate">text</div>`,
	} {
		got, err := sanitizeMarkdownHTML([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"javascript:", "data:image", "onclick", "onerror", "<svg", "<math", "<script", "<style", "<iframe", "<form", "data-gosx-action", "bad()"} {
			if strings.Contains(string(got), forbidden) {
				t.Errorf("%q survived in %q from %q", forbidden, got, source)
			}
		}
	}
}

func TestSanitizedMarkdownRetainsFormattingAndSourcePositions(t *testing.T) {
	doc := ParseDocument("notes", "note.md", "# Heading\n\n**Bold** [link](https://example.test/a?q=x%20y) ![alt](/image.png)\n")
	got, err := doc.HTML(MDPPOptions{Sanitize: true, SourcePositions: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<h1", "<strong>Bold</strong>", `href="https://example.test/a?q=x%20y"`, `src="/image.png"`, "data-mdpp-source-line", `rel="noopener noreferrer"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	doc = ParseDocument("notes", "unsafe.md", "[link](javascript:alert%281%29)\n")
	got, err = doc.HTML(MDPPOptions{Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "javascript:") {
		t.Fatalf("Sanitize option did not reach the content renderer: %s", got)
	}
}
