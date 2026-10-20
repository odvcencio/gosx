package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestFileRendererUsesNamedHTMLBooleanSemantics(t *testing.T) {
	var b strings.Builder
	renderFileEvaluatedAttr(&b, "hidden", false)
	renderFileEvaluatedAttr(&b, "required", false)
	renderFileEvaluatedAttr(&b, "selected", true)
	renderFileEvaluatedAttr(&b, "checked", true)
	renderFileEvaluatedAttr(&b, "aria-pressed", false)
	renderFileEvaluatedAttr(&b, "spellcheck", false)
	renderFileAttr(&b, ir.Attr{Kind: ir.AttrStatic, Name: "hidden", Value: "until-found"}, fileRenderEnv{}, "")
	html := b.String()

	if strings.Contains(html, `hidden="false"`) || strings.Contains(html, `required="false"`) {
		t.Fatalf("false HTML boolean attrs must be omitted, got %q", html)
	}
	for _, want := range []string{" selected", " checked", ` aria-pressed="false"`, ` spellcheck="false"`, ` hidden="until-found"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered attrs %q missing %q", html, want)
		}
	}
}
