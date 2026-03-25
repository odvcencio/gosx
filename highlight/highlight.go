// Package highlight provides syntax highlighting for Go source code.
//
// Produces HTML with CSS class annotations for each token type.
// Uses fast lexer-based tokenization — no parser dependency.
package highlight

import (
	"html"
	"strings"
)

// CSS class prefix for all syntax tokens.
const classPrefix = "ts-"

// Go keywords
var keywords = map[string]bool{
	"func": true, "return": true, "if": true, "else": true, "for": true,
	"range": true, "switch": true, "case": true, "default": true,
	"var": true, "const": true, "type": true, "struct": true,
	"interface": true, "map": true, "chan": true, "go": true, "defer": true,
	"select": true, "break": true, "continue": true, "package": true,
	"import": true, "fallthrough": true, "goto": true,
}

var builtins = map[string]bool{
	"nil": true, "append": true, "cap": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"new": true, "panic": true, "print": true, "println": true, "real": true,
	"recover": true, "error": true, "string": true, "bool": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "byte": true, "rune": true, "any": true,
}

// Go produces syntax-highlighted HTML from Go source code.
func Go(source string) string {
	if source == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(source) * 2)
	lines := strings.Split(source, "\n")

	for li, line := range lines {
		if li > 0 {
			b.WriteByte('\n')
		}

		// Handle line comments
		if idx := strings.Index(line, "//"); idx >= 0 {
			tokenizeLine(&b, line[:idx])
			b.WriteString(`<span class="ts-comment">`)
			b.WriteString(html.EscapeString(line[idx:]))
			b.WriteString("</span>")
			continue
		}

		tokenizeLine(&b, line)
	}

	return b.String()
}

func tokenizeLine(b *strings.Builder, line string) {
	i := 0
	for i < len(line) {
		ch := line[i]

		// String literals
		if ch == '"' || ch == '\'' || ch == '`' {
			end := findStringEnd(line, i)
			b.WriteString(`<span class="ts-string">`)
			b.WriteString(html.EscapeString(line[i:end]))
			b.WriteString("</span>")
			i = end
			continue
		}

		// Numbers
		if ch >= '0' && ch <= '9' {
			j := scanNumber(line, i)
			b.WriteString(`<span class="ts-number">`)
			b.WriteString(html.EscapeString(line[i:j]))
			b.WriteString("</span>")
			i = j
			continue
		}

		// Identifiers and keywords
		if isIdentStart(ch) {
			j := i + 1
			for j < len(line) && isIdentPart(line[j]) {
				j++
			}
			word := line[i:j]

			if keywords[word] {
				b.WriteString(`<span class="ts-keyword">`)
				b.WriteString(word)
				b.WriteString("</span>")
			} else if word == "true" || word == "false" {
				b.WriteString(`<span class="ts-bool">`)
				b.WriteString(word)
				b.WriteString("</span>")
			} else if builtins[word] {
				b.WriteString(`<span class="ts-builtin">`)
				b.WriteString(word)
				b.WriteString("</span>")
			} else if len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z' {
				b.WriteString(`<span class="ts-type">`)
				b.WriteString(word)
				b.WriteString("</span>")
			} else {
				b.WriteString(html.EscapeString(word))
			}
			i = j
			continue
		}

		// Operators
		if isOperator(ch) {
			b.WriteString(`<span class="ts-operator">`)
			b.WriteString(html.EscapeString(string(ch)))
			b.WriteString("</span>")
			i++
			continue
		}

		// Punctuation and whitespace
		b.WriteString(html.EscapeString(string(ch)))
		i++
	}
}

func findStringEnd(line string, start int) int {
	quote := line[start]
	i := start + 1
	for i < len(line) {
		if line[i] == '\\' && quote != '`' {
			i += 2
			continue
		}
		if line[i] == quote {
			return i + 1
		}
		i++
	}
	return len(line) // unterminated
}

func scanNumber(line string, start int) int {
	i := start
	// Hex, octal, binary prefixes
	if i+1 < len(line) && line[i] == '0' {
		switch line[i+1] {
		case 'x', 'X':
			i += 2
			for i < len(line) && isHexDigit(line[i]) {
				i++
			}
			return i
		case 'o', 'O', 'b', 'B':
			i += 2
		}
	}
	for i < len(line) && (line[i] >= '0' && line[i] <= '9' || line[i] == '_') {
		i++
	}
	if i < len(line) && line[i] == '.' {
		i++
		for i < len(line) && (line[i] >= '0' && line[i] <= '9' || line[i] == '_') {
			i++
		}
	}
	// Exponent
	if i < len(line) && (line[i] == 'e' || line[i] == 'E') {
		i++
		if i < len(line) && (line[i] == '+' || line[i] == '-') {
			i++
		}
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
	}
	return i
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func isOperator(ch byte) bool {
	return ch == '+' || ch == '-' || ch == '*' || ch == '/' || ch == '%' ||
		ch == '=' || ch == '!' || ch == '<' || ch == '>' || ch == '&' ||
		ch == '|' || ch == '^' || ch == '~'
}

// LineNumbers produces HTML for a line number gutter.
func LineNumbers(lineCount int) string {
	var b strings.Builder
	for i := 1; i <= lineCount; i++ {
		if i > 1 {
			b.WriteByte('\n')
		}
		writeInt(&b, i)
	}
	return b.String()
}

// LineCount returns the number of lines in source.
func LineCount(source string) int {
	if source == "" {
		return 1
	}
	return strings.Count(source, "\n") + 1
}

func writeInt(b *strings.Builder, n int) {
	if n == 0 {
		b.WriteByte('0')
		return
	}
	digits := make([]byte, 0, 4)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	b.Write(digits)
}
