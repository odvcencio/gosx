//go:build js && wasm

package highlight

import (
	"html"
	"strings"
)

func renderHTML(lang, source string) string {
	switch NormalizeLanguage(lang) {
	case LangGo, LangGoSX:
		return highlightGoLike(source)
	default:
		return html.EscapeString(source)
	}
}

func highlightGoLike(source string) string {
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
		if idx := strings.Index(line, "//"); idx >= 0 {
			tokenizeGoLikeLine(&b, line[:idx])
			b.WriteString(`<span class="ts-comment">`)
			b.WriteString(html.EscapeString(line[idx:]))
			b.WriteString(`</span>`)
			continue
		}
		tokenizeGoLikeLine(&b, line)
	}
	return b.String()
}

func tokenizeGoLikeLine(b *strings.Builder, line string) {
	i := 0
	for i < len(line) {
		ch := line[i]
		if ch == '"' || ch == '\'' || ch == '`' {
			end := findStringEnd(line, i)
			b.WriteString(`<span class="ts-string">`)
			b.WriteString(html.EscapeString(line[i:end]))
			b.WriteString(`</span>`)
			i = end
			continue
		}
		if ch >= '0' && ch <= '9' {
			j := scanNumber(line, i)
			b.WriteString(`<span class="ts-number">`)
			b.WriteString(html.EscapeString(line[i:j]))
			b.WriteString(`</span>`)
			i = j
			continue
		}
		if isIdentStart(ch) {
			j := i + 1
			for j < len(line) && isIdentPart(line[j]) {
				j++
			}
			word := line[i:j]
			switch {
			case keywords[word]:
				b.WriteString(`<span class="ts-keyword">`)
				b.WriteString(word)
				b.WriteString(`</span>`)
			case word == "true" || word == "false":
				b.WriteString(`<span class="ts-bool">`)
				b.WriteString(word)
				b.WriteString(`</span>`)
			case builtins[word]:
				b.WriteString(`<span class="ts-builtin">`)
				b.WriteString(word)
				b.WriteString(`</span>`)
			case len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z':
				b.WriteString(`<span class="ts-type">`)
				b.WriteString(word)
				b.WriteString(`</span>`)
			default:
				b.WriteString(html.EscapeString(word))
			}
			i = j
			continue
		}
		if isOperator(ch) {
			b.WriteString(`<span class="ts-operator">`)
			b.WriteString(html.EscapeString(string(ch)))
			b.WriteString(`</span>`)
			i++
			continue
		}
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
	return len(line)
}

func scanNumber(line string, start int) int {
	i := start
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
	for i < len(line) && ((line[i] >= '0' && line[i] <= '9') || line[i] == '_') {
		i++
	}
	if i < len(line) && line[i] == '.' {
		i++
		for i < len(line) && ((line[i] >= '0' && line[i] <= '9') || line[i] == '_') {
			i++
		}
	}
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
