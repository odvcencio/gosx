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
	Text          string
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
	props = normalizeTextBlockProps(props, "")
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
	props = normalizeTextBlockProps(props, textBlockPlainTextArgs(args))
	tag := strings.TrimSpace(props.Tag)
	if tag == "" {
		tag = "div"
	}
	if !textBlockHasNodeChild(args) && props.Text != "" {
		args = append(args, gosx.Text(props.Text))
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

// EstimateTextBlockMetrics returns an approximate server-side layout plan that
// can be used as a stable first-pass hint before browser refinement.
func EstimateTextBlockMetrics(props TextBlockProps) (textlayout.Metrics, bool) {
	source := effectiveTextBlockSource(props, "")
	if source == "" || props.MaxWidth <= 0 {
		return textlayout.Metrics{}, false
	}
	lineHeight := props.LineHeight
	if lineHeight <= 0 {
		lineHeight = textlayoutApproximateLineHeight(props.Font)
	}
	metrics, err := textlayout.LayoutTextMetrics(
		source,
		textlayout.ApproximateMeasurer{},
		props.Font,
		textlayout.PrepareOptions{WhiteSpace: props.WhiteSpace},
		textlayout.LayoutOptions{
			MaxWidth:   props.MaxWidth,
			LineHeight: lineHeight,
		},
	)
	if err != nil || metrics.LineCount <= 0 {
		return textlayout.Metrics{}, false
	}
	return metrics, true
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

func normalizeTextBlockProps(props TextBlockProps, fallbackSource string) TextBlockProps {
	props.Source = effectiveTextBlockSource(props, fallbackSource)
	if (props.HeightHint <= 0 || props.LineCountHint <= 0) && props.Source != "" && props.MaxWidth > 0 {
		if metrics, ok := EstimateTextBlockMetrics(props); ok {
			if props.LineCountHint <= 0 {
				props.LineCountHint = metrics.LineCount
			}
			if props.HeightHint <= 0 {
				props.HeightHint = metrics.Height
			}
		}
	}
	return props
}

func effectiveTextBlockSource(props TextBlockProps, fallback string) string {
	switch {
	case props.Source != "":
		return props.Source
	case props.Text != "":
		return props.Text
	default:
		return fallback
	}
}

func textBlockHasNodeChild(args []any) bool {
	for _, arg := range args {
		if _, ok := arg.(gosx.Node); ok {
			return true
		}
	}
	return false
}

func textBlockPlainTextArgs(args []any) string {
	var b strings.Builder
	for _, arg := range args {
		node, ok := arg.(gosx.Node)
		if !ok {
			continue
		}
		b.WriteString(gosx.PlainText(node))
	}
	return b.String()
}

func textlayoutApproximateLineHeight(font string) float64 {
	return textlayoutApproximateFontSize(font)*1.35 + 1
}

func textlayoutApproximateFontSize(font string) float64 {
	value := 0.0
	haveDigits := false
	decimal := false
	scale := 1.0
	current := 0.0
	font = strings.TrimSpace(font)
	for i := 0; i < len(font); i++ {
		ch := font[i]
		switch {
		case ch >= '0' && ch <= '9':
			digit := float64(ch - '0')
			if decimal {
				scale *= 0.1
				current += digit * scale
			} else {
				current = current*10 + digit
			}
			haveDigits = true
		case ch == '.' && !decimal:
			decimal = true
		default:
			if haveDigits && i+1 < len(font) && (font[i] == 'p' || font[i] == 'P') && (font[i+1] == 'x' || font[i+1] == 'X') {
				value = current
				i = len(font)
				break
			}
			haveDigits = false
			decimal = false
			scale = 1.0
			current = 0.0
		}
	}
	if value <= 0 {
		return 16
	}
	return value
}
