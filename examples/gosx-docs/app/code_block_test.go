package docs

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestDocsCodeBlockHighlightsGo(t *testing.T) {
	html := gosx.RenderHTML(DocsCodeBlock("go", `func Page() Node {
	return nil
}`))

	if !strings.Contains(html, `data-lang="go"`) {
		t.Fatalf("expected go data-lang marker, got %q", html)
	}
	if !strings.Contains(html, `class="ts-keyword">func</span>`) {
		t.Fatalf("expected Go keyword highlighting, got %q", html)
	}
	if !strings.Contains(html, `class="ts-type">Node</span>`) {
		t.Fatalf("expected Go type highlighting, got %q", html)
	}
}

func TestDocsCodeBlockHighlightsGoSX(t *testing.T) {
	html := gosx.RenderHTML(DocsCodeBlock("gosx", `func Page() Node {
	return <Scene3D class="scene-shell">{value}</Scene3D>
}`))

	if !strings.Contains(html, `data-lang="gosx"`) {
		t.Fatalf("expected gosx data-lang marker, got %q", html)
	}
	if !strings.Contains(html, `class="ts-tag">Scene3D</span>`) {
		t.Fatalf("expected GoSX tag highlighting, got %q", html)
	}
	if !strings.Contains(html, `class="ts-attr">class</span>`) {
		t.Fatalf("expected GoSX attribute highlighting, got %q", html)
	}
}
