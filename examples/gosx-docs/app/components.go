package docs

import (
	"strconv"
	"strings"
	"sync/atomic"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/highlight"
)

var tooltipID atomic.Int64

func tooltipCounter() int {
	return int(tooltipID.Add(1))
}

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

// StatCard renders a proof-point stat card.
func StatCard(value, label string) gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "stat-card glass-panel")),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "stat-card__value")), gosx.Text(value)),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "stat-card__label")), gosx.Text(label)),
	)
}

// CapabilityTag renders a small tag badge.
func CapabilityTag(label string) gosx.Node {
	return gosx.El("span", gosx.Attrs(gosx.Attr("class", "cap-tag")), gosx.Text(label))
}

// Tooltip renders a trigger element with an accessible tooltip overlay.
func Tooltip(trigger gosx.Node, content string) gosx.Node {
	id := "tip-" + strconv.Itoa(tooltipCounter())
	return gosx.El("span", gosx.Attrs(gosx.Attr("class", "tooltip-trigger"), gosx.Attr("aria-describedby", id)),
		trigger,
		gosx.El("span", gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("class", "tooltip glass-panel"),
			gosx.Attr("role", "tooltip"),
		), gosx.Text(content)),
	)
}
