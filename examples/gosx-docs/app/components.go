package docs

import (
	"strconv"
	"sync/atomic"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/highlight"
	"github.com/odvcencio/gosx/server"
)

var tooltipID atomic.Int64

func tooltipCounter() int {
	return int(tooltipID.Add(1))
}

// CodeBlock renders a syntax-highlighted code sample in a dark glass panel.
func CodeBlock(lang, source string) gosx.Node {
	normalized := highlight.NormalizeLanguage(lang)
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

// StatCard renders a proof-point stat card with server-measured text.
func StatCard(value, label string) gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "stat-card glass-panel")),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "stat-card__value")),
			server.TextBlock(server.TextBlockProps{
				Font: "700 32px Space Grotesk",
			}, value),
		),
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
