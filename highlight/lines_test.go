package highlight

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A 6-line Go snippet must yield exactly 6 line wrappers, each carrying the
// correct 1-based data-line attribute and the standard ts-line class.
func TestHTMLLinesWrapsEachLine(t *testing.T) {
	source := "package main\n" + // 1
		"\n" + //                   2
		"func main() {\n" + //      3
		"\tx := 42\n" + //          4
		"\tprintln(x)\n" + //       5
		"}" //                      6

	lines := HTMLLines("go", source)
	if len(lines) != 6 {
		t.Fatalf("expected 6 line wrappers, got %d: %#v", len(lines), lines)
	}

	for i, line := range lines {
		wantAttr := fmt.Sprintf(`data-line="%d"`, i+1)
		if !strings.Contains(line, wantAttr) {
			t.Errorf("line %d missing %s: %q", i+1, wantAttr, line)
		}
		if !strings.Contains(line, `class="ts-line"`) {
			t.Errorf("line %d missing ts-line class: %q", i+1, line)
		}
		if !strings.HasPrefix(line, "<span") || !strings.HasSuffix(line, "</span>") {
			t.Errorf("line %d is not a balanced span wrapper: %q", i+1, line)
		}
	}
}

// Per-line token markup must match the whole-block render: the highlighted
// inner content of a line should appear inside its wrapper, so coloring is
// identical to HTML().
func TestHTMLLinesKeepsTokenMarkup(t *testing.T) {
	source := "func main() {\n\tx := 42\n}"
	lines := HTMLLines("go", source)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `<span class="ts-keyword">func</span>`) {
		t.Errorf("expected ts-keyword for func on line 1, got %q", lines[0])
	}
	if !strings.Contains(lines[1], `<span class="ts-number">42</span>`) {
		t.Errorf("expected ts-number for 42 on line 2, got %q", lines[1])
	}
}

// A token span that itself contains a newline (multi-line raw string) must be
// split across lines with each line's span balanced (closed then reopened).
func TestHTMLLinesBalancesMultiLineSpan(t *testing.T) {
	source := "x := `line1\nline2`"
	lines := HTMLLines("go", source)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %#v", len(lines), lines)
	}
	for i, line := range lines {
		if strings.Count(line, "<span") != strings.Count(line, "</span>") {
			t.Errorf("line %d has unbalanced spans: %q", i+1, line)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("line %d must not contain a newline: %q", i+1, line)
		}
	}
	// The string class must survive on both halves of the split span.
	if !strings.Contains(lines[0], "ts-string") {
		t.Errorf("line 1 lost ts-string class: %q", lines[0])
	}
	if !strings.Contains(lines[1], "ts-string") {
		t.Errorf("line 2 lost ts-string class: %q", lines[1])
	}
}

// Empty source yields a single empty line wrapper (mirrors LineCount("") == 1).
func TestHTMLLinesEmpty(t *testing.T) {
	lines := HTMLLines("go", "")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for empty source, got %d: %#v", len(lines), lines)
	}
}

// HTML escaping must hold inside per-line wrappers too.
func TestHTMLLinesEscaping(t *testing.T) {
	lines := HTMLLines("text", "<script>\nok")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if strings.Contains(lines[0], "<script>") {
		t.Errorf("expected escaping on line 1, got %q", lines[0])
	}
}

// parseRangeSpec parses the click-step DSL: groups split on '|', items within a
// group split on ',', each item is N, N-M, or "all".
// "1-3|5|all" -> step1={1,2,3}, step2={5}, step3=all.
func TestParseRangeSpec(t *testing.T) {
	got := parseRangeSpec("1-3|5|all")
	want := []HighlightStep{
		{Lines: []int{1, 2, 3}},
		{Lines: []int{5}},
		{All: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRangeSpec(\"1-3|5|all\") = %#v, want %#v", got, want)
	}
}

// Comma lists combine with ranges within a single step, with whitespace
// tolerated and lines sorted/deduplicated.
func TestParseRangeSpecCommaAndRanges(t *testing.T) {
	got := parseRangeSpec("1,4 | 2-3")
	want := []HighlightStep{
		{Lines: []int{1, 4}},
		{Lines: []int{2, 3}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRangeSpec(\"1,4 | 2-3\") = %#v, want %#v", got, want)
	}
}

// Empty / whitespace-only / garbage specs produce no steps rather than
// panicking or emitting empty steps.
func TestParseRangeSpecEmpty(t *testing.T) {
	for _, spec := range []string{"", "   ", "|", " | ", "abc", "-", "3-1"} {
		got := parseRangeSpec(spec)
		if len(got) != 0 {
			t.Errorf("parseRangeSpec(%q) = %#v, want no steps", spec, got)
		}
	}
}
