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
		t.Fatal("expected ts-type class for capitalized identifier")
	}
}

func TestGoBuiltinHighlighting(t *testing.T) {
	result := Go("x := len(s)")
	if !strings.Contains(result, "ts-builtin") {
		t.Fatal("expected ts-builtin class for 'len'")
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
