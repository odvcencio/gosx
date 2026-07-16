// Package prose contains reusable presentation primitives for Markdown++
// content surfaces.
package prose

import (
	"fmt"
	"regexp"
	"strings"
)

// Style controls the three dimensions of prose rhythm shared by GoSX content
// surfaces: base size, line leading, and flow between top-level blocks.
//
// Values are CSS tokens rather than fixed numbers so a consumer can use
// rem, clamp(), or a project-owned custom property. The values are validated
// before being emitted into a style attribute.
type Style struct {
	Size    string
	Leading string
	Flow    string
}

// DefaultStyle is the baseline rhythm for rendered GoSX prose.
var DefaultStyle = Style{
	Size:    "1rem",
	Leading: "1.65",
	Flow:    "1rem",
}

var cssTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9.%()+*/,_ -]+$`)

// Normalize applies defaults and rejects CSS punctuation that could escape a
// style declaration. Consumers can still override the resulting variables
// from their own stylesheet.
func (s Style) Normalize() Style {
	if strings.TrimSpace(s.Size) == "" || !cssTokenPattern.MatchString(strings.TrimSpace(s.Size)) {
		s.Size = DefaultStyle.Size
	}
	if strings.TrimSpace(s.Leading) == "" || !cssTokenPattern.MatchString(strings.TrimSpace(s.Leading)) {
		s.Leading = DefaultStyle.Leading
	}
	if strings.TrimSpace(s.Flow) == "" || !cssTokenPattern.MatchString(strings.TrimSpace(s.Flow)) {
		s.Flow = DefaultStyle.Flow
	}
	return Style{
		Size:    strings.TrimSpace(s.Size),
		Leading: strings.TrimSpace(s.Leading),
		Flow:    strings.TrimSpace(s.Flow),
	}
}

// CSSVariables returns the inline custom-property declaration for a prose
// container. It deliberately emits only GoSX-owned variables; typography,
// colors, and component details remain stylesheet-owned.
func (s Style) CSSVariables() string {
	normalized := s.Normalize()
	return fmt.Sprintf(
		"--gosx-prose-size:%s;--gosx-prose-leading:%s;--gosx-prose-flow:%s;",
		normalized.Size,
		normalized.Leading,
		normalized.Flow,
	)
}
