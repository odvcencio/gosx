package docs

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestCMSPreviewAnchorTargetsTheOffsetPreviewPanel(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gosx.Compile(source); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`href="#cms-preview"`, `id="cms-preview" aria-label="Live preview"`} {
		if !strings.Contains(string(source), marker) {
			t.Errorf("preview anchor contract missing %q", marker)
		}
	}
	css, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), "scroll-margin-block-start: calc(var(--site-nav-offset, 4rem)") {
		t.Fatal("direct preview-canvas hashes must also account for the fixed header")
	}
}
