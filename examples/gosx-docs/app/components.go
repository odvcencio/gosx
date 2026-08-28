// Package docs implements the GoSX documentation site.
//
// This file used to hold the site's whole shared component library, hand
// built on the low-level gosx.El/Attrs/Text API because a .gsx component
// could not be shared across pages. Three of its four components — every
// one with a typed-props shape a strict component can actually declare —
// now live in app/ui/*.gsx, a shared component directory reached through
// the shared-components design (import ui "./ui"; <ui.StatCard .../>):
// StatCard, CapabilityTag, and Tooltip. Their 7 call sites (StatCard was
// the only one of the three anything actually called) moved with them,
// from the old {StatCard(value, label)} expression-call form to
// <ui.StatCard Value={value} Label={label}/>.
//
// A Go-callable adapter that forwarded to the .gsx source was considered
// and rejected: a props struct declared inside a .gsx file is not a real
// Go type until gosx build transpiles it, so no plain .go file — this one
// included — can name it, and moving the struct to a companion .go file
// instead would leave ir.Lower (which never reads a sibling file) unable
// to see it, degrading the strict component back to untyped attribute
// handling. Real call-site migration was the only option that keeps both
// halves genuinely typed.
//
// CodeBlock stays hand-built. Its core job is embedding ALREADY-RENDERED,
// syntax-highlighted HTML (the highlight package's own output) as raw
// markup, and a strict component has no channel for that: a typed props
// field may only be a scalar or a same-file struct, never a gosx.Node (see
// ir.strictRendererScalarType), and {children} — the one place a gosx.Node
// DOES pass through unescaped — binds only at a nested call site inside
// another component's body. CodeBlock's 103 call sites all supply a plain
// (lang, source string) pair, not pre-rendered markup, so in principle a
// typed Lang/Source props call could still work; the blocker is scale, not
// architecture — rewriting 103 call sites, many holding multi-line/
// multi-language literal source samples as page content, was judged too
// large a mechanical change for this conversion to also carry safely.
package docs

import (
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/highlight"
)

// CodeBlock renders a syntax-highlighted code sample in a dark glass panel.
func CodeBlock(lang, source string) gosx.Node {
	normalized := highlight.NormalizeLanguage(lang)
	source = normalizeCodeBlockSource(lang, source)
	lineCount := highlight.LineCount(source)
	return gosx.El("figure", gosx.Attrs(
		gosx.Attr("class", "code-sample"),
		gosx.Attr("role", "region"),
		gosx.Attr("aria-label", highlight.Label(normalized)+" code sample"),
	),
		gosx.El("figcaption", gosx.Attrs(gosx.Attr("class", "code-sample__head")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "code-sample__label")), gosx.Text(highlight.Label(normalized))),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "code-sample__body")),
			gosx.El("pre", gosx.Attrs(
				gosx.Attr("class", "code-sample__gutter"),
				gosx.Attr("aria-hidden", "true"),
			), gosx.Text(highlight.LineNumbers(lineCount))),
			gosx.El("pre", gosx.Attrs(gosx.Attr("class", "code-block")),
				gosx.El("code", gosx.Attrs(gosx.Attr("data-lang", normalized)), gosx.RawHTML(highlight.HTML(normalized, source))),
			),
		),
	)
}

const codeBlockTabWidth = 4

// normalizeCodeBlockSource removes the incidental margin introduced when a
// multiline snippet is nested inside a formatted GoSX expression. Plain text
// and unknown languages remain byte-exact because their leading whitespace may
// be the content (for example, directory trees or whitespace-sensitive DSLs).
func normalizeCodeBlockSource(lang, source string) string {
	if !codeBlockLanguageSupportsDedent(lang) {
		return source
	}

	lines := strings.Split(source, "\n")
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	last := len(lines)
	for last > first && strings.TrimSpace(lines[last-1]) == "" {
		last--
	}
	if first == last {
		return ""
	}
	lines = lines[first:last]

	// A raw/interpreted literal commonly starts immediately after its opening
	// delimiter. Its first line is already at display column zero, while every
	// continuation line still carries the enclosing GoSX expression's margin.
	indentStart := 0
	if first == 0 && leadingCodeIndentColumns(lines[0]) == 0 && len(lines) > 1 {
		indentStart = 1
	}

	commonIndent := -1
	for _, line := range lines[indentStart:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingCodeIndentColumns(line)
		if commonIndent < 0 || indent < commonIndent {
			commonIndent = indent
		}
	}
	if commonIndent <= 0 {
		return strings.Join(lines, "\n")
	}

	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[index] = ""
			continue
		}
		if index < indentStart {
			continue
		}
		lines[index] = removeCodeIndentColumns(line, commonIndent)
	}
	return strings.Join(lines, "\n")
}

func codeBlockLanguageSupportsDedent(lang string) bool {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "go", "gosx", "gsx",
		"javascript", "js",
		"json",
		"bash", "sh", "shell",
		"html", "http":
		return true
	default:
		return false
	}
}

func leadingCodeIndentColumns(line string) int {
	columns := 0
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case ' ':
			columns++
		case '\t':
			columns += codeBlockTabWidth - columns%codeBlockTabWidth
		default:
			return columns
		}
	}
	return columns
}

func removeCodeIndentColumns(line string, columns int) string {
	current := 0
	for index := 0; index < len(line); index++ {
		var next int
		switch line[index] {
		case ' ':
			next = current + 1
		case '\t':
			next = current + codeBlockTabWidth - current%codeBlockTabWidth
		default:
			return line[index:]
		}
		if next > columns {
			return strings.Repeat(" ", next-columns) + line[index+1:]
		}
		current = next
		if current == columns {
			return line[index+1:]
		}
	}
	return ""
}
