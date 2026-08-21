package syntax

import (
	"html"
	"strings"
)

// RenderText is the single pre-v1 GSX text policy used by strict IR lowering
// and legacy Go emission. It follows JSX-like source-layout normalization:
// line breaks are layout, indentation on lines after a break is discarded,
// non-empty lines are separated by one space, and a same-line leading/trailing
// space remains available as an intentional separator next to an expression or
// element. Authors can make a separator unambiguous with `{" "}`. Entities
// decode once here so strict and legacy rendering receive the same text.
func RenderText(source string) string {
	if source == "" {
		return ""
	}
	if !strings.ContainsAny(source, "\r\n") {
		return html.UnescapeString(source)
	}

	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	parts := make([]string, 0, len(lines))
	for i, line := range lines {
		// Only an ASCII space/tab-only line is source layout. Do not use
		// strings.TrimSpace here: GSX text is rendered content, and Unicode
		// whitespace such as NBSP, narrow NBSP, em space, U+2028/U+2029,
		// U+0085, and form feed can be intentional literal content. Newline
		// normalization above deliberately happens before this classification
		// so CRLF/bare-CR layout lines retain the same policy as LF lines.
		if isASCIILayoutLine(line) {
			continue
		}
		// A line that follows a source newline is layout-indented. The first
		// line may begin on the same line as its opening tag or expression;
		// preserve that leading space because it can be intentional content.
		if i > 0 {
			line = strings.TrimLeft(line, " \t")
		}
		// A line followed by a source newline is layout-terminated. The final
		// line may end immediately before an expression/element on the same
		// line, so preserve its trailing separator.
		if i < len(lines)-1 {
			line = strings.TrimRight(line, " \t")
		}
		if line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return html.UnescapeString(strings.Join(parts, " "))
}

// isASCIILayoutLine reports whether line contains only the horizontal ASCII
// characters that the GSX source-layout policy treats as indentation/padding.
// It is intentionally byte-based: every non-ASCII byte is semantic content,
// including UTF-8 encodings of Unicode whitespace, and form feed is preserved
// as authored content rather than silently discarded as layout.
func isASCIILayoutLine(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return false
		}
	}
	return true
}
