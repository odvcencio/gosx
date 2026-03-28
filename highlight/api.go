// Package highlight provides syntax-highlighting helpers for GoSX, Go, and
// common docs-site snippet languages.
package highlight

import "strings"

const (
	LangText       = "text"
	LangGo         = "go"
	LangGoSX       = "gosx"
	LangJavaScript = "javascript"
	LangJSON       = "json"
	LangBash       = "bash"
)

var keywords = map[string]bool{
	"async": true, "await": true, "break": true, "case": true, "chan": true,
	"class": true, "const": true, "continue": true, "default": true, "defer": true,
	"else": true, "export": true, "fallthrough": true, "for": true, "func": true,
	"function": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "let": true, "map": true, "new": true, "package": true,
	"range": true, "return": true, "select": true, "struct": true, "switch": true,
	"type": true, "var": true,
}

var builtins = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true,
	"close": true, "complex": true, "copy": true, "delete": true, "error": true,
	"float32": true, "float64": true, "imag": true, "int": true, "int16": true,
	"int32": true, "int64": true, "int8": true, "len": true, "make": true,
	"new": true, "panic": true, "print": true, "println": true, "real": true,
	"recover": true, "rune": true, "string": true, "uint": true, "uint16": true,
	"uint32": true, "uint64": true, "uint8": true,
}

// HTML returns syntax-highlighted HTML for the given source.
//
// Supported language values are:
// - "go"
// - "gosx" / "gsx"
// - "javascript" / "js"
// - "json"
// - "bash" / "sh" / "shell"
//
// Unknown languages fall back to escaped plain text.
func HTML(lang, source string) string {
	return renderHTML(NormalizeLanguage(lang), source)
}

// Go returns syntax-highlighted HTML for Go source.
func Go(source string) string {
	return HTML(LangGo, source)
}

// GoSX returns syntax-highlighted HTML for GoSX source.
func GoSX(source string) string {
	return HTML(LangGoSX, source)
}

// JavaScript returns syntax-highlighted HTML for JavaScript source.
func JavaScript(source string) string {
	return HTML(LangJavaScript, source)
}

// JSON returns syntax-highlighted HTML for JSON source.
func JSON(source string) string {
	return HTML(LangJSON, source)
}

// Bash returns syntax-highlighted HTML for shell source.
func Bash(source string) string {
	return HTML(LangBash, source)
}

// NormalizeLanguage canonicalizes GoSX highlight language names.
func NormalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case LangGo:
		return LangGo
	case LangGoSX, "gsx":
		return LangGoSX
	case LangJavaScript, "js":
		return LangJavaScript
	case LangJSON:
		return LangJSON
	case LangBash, "sh", "shell":
		return LangBash
	default:
		return LangText
	}
}

// Label returns a display label for a normalized or aliased language name.
func Label(lang string) string {
	switch NormalizeLanguage(lang) {
	case LangGo:
		return "Go"
	case LangGoSX:
		return "GoSX"
	case LangJavaScript:
		return "JavaScript"
	case LangJSON:
		return "JSON"
	case LangBash:
		return "Bash"
	default:
		return "Text"
	}
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
