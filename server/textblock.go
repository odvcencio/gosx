package server

import (
	"strconv"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/textlayout"
)

const textBlockAttr = "data-gosx-text-layout"

// TextBlockProps configures a declarative DOM text-layout node that the shared
// bootstrap runtime can observe and keep up to date.
type TextBlockProps struct {
	Tag           string
	Font          string
	WhiteSpace    textlayout.WhiteSpace
	LineHeight    float64
	MaxWidth      float64
	HeightHint    float64
	LineCountHint int
	Static        bool
	Source        string
}

// TextBlockAttr describes one server-rendered text-layout attribute.
type TextBlockAttr struct {
	Name  string
	Value string
	Bool  bool
}

// TextBlockAttrs returns the HTML attributes required for a managed text block.
func TextBlockAttrs(props TextBlockProps) []TextBlockAttr {
	attrs := []TextBlockAttr{{Name: textBlockAttr, Bool: true}}

	if font := strings.TrimSpace(props.Font); font != "" {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-font", Value: font})
	}
	if ws := normalizeTextBlockWhiteSpace(props.WhiteSpace); ws != "" && ws != string(textlayout.WhiteSpaceNormal) {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-white-space", Value: ws})
	}
	if props.LineHeight > 0 {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-line-height", Value: formatTextBlockFloat(props.LineHeight)})
	}
	if props.MaxWidth > 0 {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-max-width", Value: formatTextBlockFloat(props.MaxWidth)})
	}
	if props.HeightHint > 0 {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-height-hint", Value: formatTextBlockFloat(props.HeightHint)})
	}
	if props.LineCountHint > 0 {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-line-count-hint", Value: strconv.Itoa(props.LineCountHint)})
	}
	if props.Static {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-observe", Value: "false"})
	}
	if source := props.Source; source != "" {
		attrs = append(attrs, TextBlockAttr{Name: "data-gosx-text-layout-source", Value: source})
	}

	return attrs
}

// TextBlock renders a DOM node opted into the shared text-layout substrate.
func TextBlock(props TextBlockProps, args ...any) gosx.Node {
	tag := strings.TrimSpace(props.Tag)
	if tag == "" {
		tag = "div"
	}

	attrArgs := make([]any, 0, len(args)+len(TextBlockAttrs(props)))
	for _, attr := range TextBlockAttrs(props) {
		if attr.Bool {
			attrArgs = append(attrArgs, gosx.BoolAttr(attr.Name))
			continue
		}
		attrArgs = append(attrArgs, gosx.Attr(attr.Name, attr.Value))
	}

	prefixed := make([]any, 0, len(args)+1)
	prefixed = append(prefixed, gosx.Attrs(attrArgs...))
	prefixed = append(prefixed, args...)
	return gosx.El(tag, prefixed...)
}

func normalizeTextBlockWhiteSpace(ws textlayout.WhiteSpace) string {
	switch ws {
	case textlayout.WhiteSpacePreWrap:
		return string(textlayout.WhiteSpacePreWrap)
	case textlayout.WhiteSpacePre:
		return string(textlayout.WhiteSpacePre)
	case textlayout.WhiteSpaceNormal, "":
		return string(textlayout.WhiteSpaceNormal)
	default:
		return strings.TrimSpace(string(ws))
	}
}

func formatTextBlockFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
