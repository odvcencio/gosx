package docs

import (
	"strconv"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/highlight"
)

func DocsCodeBlock(lang, source string) gosx.Node {
	normalized := highlight.NormalizeLanguage(lang)
	lineCount := highlight.LineCount(source)
	return gosx.El("figure", gosx.Attrs(
		gosx.Attr("class", "code-sample"),
		gosx.Attr("data-lang", normalized),
	),
		gosx.El("figcaption", gosx.Attrs(gosx.Attr("class", "code-sample__head")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "code-sample__label")), gosx.Text(highlight.Label(normalized))),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "code-sample__meta")), gosx.Text(codeLineMeta(lineCount))),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "code-sample__body")),
			gosx.El("pre", gosx.Attrs(
				gosx.Attr("class", "code-sample__gutter"),
				gosx.Attr("aria-hidden", "true"),
			), gosx.Text(highlight.LineNumbers(lineCount))),
			gosx.El("pre", gosx.Attrs(gosx.Attr("class", "code-block")),
				gosx.El("code", gosx.RawHTML(highlight.HTML(normalized, source))),
			),
		),
	)
}

func codeLineMeta(count int) string {
	if count == 1 {
		return "1 line"
	}
	return strconv.Itoa(count) + " lines"
}
