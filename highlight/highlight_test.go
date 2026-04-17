package highlight

import (
	"strings"
	"testing"
)

func TestGoKeywordHighlighting(t *testing.T) {
	result := Go("func main() {}")
	if !strings.Contains(result, "ts-keyword") {
		t.Fatal("expected ts-keyword class for 'func'")
	}
}

func TestGoStringHighlighting(t *testing.T) {
	result := Go(`x := "hello"`)
	if !strings.Contains(result, "ts-string") {
		t.Fatal("expected ts-string class")
	}
}

func TestGoNumberHighlighting(t *testing.T) {
	result := Go("x := 42")
	if !strings.Contains(result, "ts-number") {
		t.Fatal("expected ts-number class")
	}
}

func TestGoCommentHighlighting(t *testing.T) {
	result := Go("// this is a comment")
	if !strings.Contains(result, "ts-comment") {
		t.Fatal("expected ts-comment class")
	}
}

func TestGoTypeHighlighting(t *testing.T) {
	result := Go("var x MyType")
	if !strings.Contains(result, "ts-type") {
		t.Fatalf("expected ts-type class for capitalized identifier, got %q", result)
	}
}

func TestGoBuiltinHighlighting(t *testing.T) {
	result := Go("x := len(s)")
	if !strings.Contains(result, "ts-builtin") {
		t.Fatal("expected ts-builtin class for 'len'")
	}
}

func TestGoSXHighlighting(t *testing.T) {
	result := GoSX(`func Page() Node {
	return <Scene3D class="scene-shell">{value}</Scene3D>
}`)
	if !strings.Contains(result, `ts-tag">Scene3D</span>`) {
		t.Fatal("expected ts-tag class for GoSX tag")
	}
	if !strings.Contains(result, `ts-attr">class</span>`) {
		t.Fatal("expected ts-attr class for GoSX attribute")
	}
}

func TestJavaScriptHighlighting(t *testing.T) {
	result := JavaScript(`await page.goto("/docs/runtime")`)
	if !strings.Contains(result, "ts-keyword") {
		t.Fatal("expected ts-keyword class for JavaScript")
	}
}

func TestJSONHighlighting(t *testing.T) {
	result := JSON(`{"cache": true}`)
	if !strings.Contains(result, "ts-bool") {
		t.Fatal("expected ts-bool class for JSON")
	}
}

func TestNormalizeLanguage(t *testing.T) {
	if got := NormalizeLanguage("gsx"); got != LangGoSX {
		t.Fatalf("expected gsx to normalize to %q, got %q", LangGoSX, got)
	}
	if got := NormalizeLanguage("sh"); got != LangBash {
		t.Fatalf("expected sh to normalize to %q, got %q", LangBash, got)
	}
	if got := NormalizeLanguage("unknown"); got != LangText {
		t.Fatalf("expected unknown to normalize to %q, got %q", LangText, got)
	}
}

func TestLabel(t *testing.T) {
	if got := Label("gosx"); got != "GoSX" {
		t.Fatalf("expected GoSX label, got %q", got)
	}
	if got := Label("json"); got != "JSON" {
		t.Fatalf("expected JSON label, got %q", got)
	}
}

func TestGoEmpty(t *testing.T) {
	result := Go("")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestGoEscaping(t *testing.T) {
	result := Go("x := \"<script>\"")
	if strings.Contains(result, "<script>") {
		t.Fatal("expected HTML escaping")
	}
}

func TestPlainTextEscaping(t *testing.T) {
	result := HTML("text", `<div>unsafe</div>`)
	if strings.Contains(result, "<div>unsafe</div>") {
		t.Fatal("expected plain-text escaping")
	}
}

func TestLineNumbers(t *testing.T) {
	result := LineNumbers(5)
	if !strings.Contains(result, "1") || !strings.Contains(result, "5") {
		t.Fatalf("expected 1-5, got %q", result)
	}
}

func TestLineCount(t *testing.T) {
	if LineCount("a\nb\nc") != 3 {
		t.Fatal("expected 3 lines")
	}
	if LineCount("") != 1 {
		t.Fatal("expected 1 line for empty")
	}
}
