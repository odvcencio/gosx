package lsp

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/format"
	"github.com/odvcencio/gosx/ir"
)

const (
	SeverityError = 1
)

// Position is an LSP line/character coordinate.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is an LSP range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Diagnostic is an LSP-compatible diagnostic payload.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Analyze parses and validates GoSX source, returning editor diagnostics.
func Analyze(path string, source []byte) []Diagnostic {
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return []Diagnostic{diagnosticForError(err)}
	}

	root := tree.RootNode()
	if root.HasError() {
		return []Diagnostic{diagnosticForError(gosx.DescribeParseError(root, source, lang))}
	}

	prog, err := ir.Lower(root, source, lang)
	if err != nil {
		return []Diagnostic{diagnosticForError(err)}
	}

	raw := ir.Validate(prog)
	diags := make([]Diagnostic, 0, len(raw))
	for _, diag := range raw {
		diags = append(diags, Diagnostic{
			Range:    rangeFromSpan(diag.Span),
			Severity: SeverityError,
			Source:   diagnosticSource(path),
			Message:  diagnosticMessage(diag),
		})
	}

	return diags
}

// FormatSource runs the canonical GoSX formatter.
func FormatSource(source []byte) ([]byte, error) {
	return format.Source(source)
}

func diagnosticForError(err error) Diagnostic {
	if err == nil {
		return Diagnostic{
			Range:    zeroRange(),
			Severity: SeverityError,
			Source:   "gosx",
			Message:  "unknown error",
		}
	}

	var parseErr *gosx.ParseError
	if errors.As(err, &parseErr) {
		line := clampZeroBased(parseErr.Line - 1)
		col := clampZeroBased(parseErr.Column - 1)
		return Diagnostic{
			Range: Range{
				Start: Position{Line: line, Character: col},
				End:   Position{Line: line, Character: col + 1},
			},
			Severity: SeverityError,
			Source:   "gosx",
			Message:  parseErr.Message,
		}
	}

	return Diagnostic{
		Range:    zeroRange(),
		Severity: SeverityError,
		Source:   "gosx",
		Message:  err.Error(),
	}
}

func diagnosticMessage(diag ir.Diagnostic) string {
	if diag.Hint == "" {
		return diag.Message
	}
	return fmt.Sprintf("%s (%s)", diag.Message, diag.Hint)
}

func rangeFromSpan(span ir.Span) Range {
	startLine := clampZeroBased(span.StartLine - 1)
	startCol := clampZeroBased(span.StartCol - 1)
	endLine := startLine
	if span.EndLine > 0 {
		endLine = clampZeroBased(span.EndLine - 1)
	}
	endCol := startCol + 1
	if span.EndCol > 0 {
		endCol = clampZeroBased(span.EndCol - 1)
		if endLine == startLine && endCol <= startCol {
			endCol = startCol + 1
		}
	}
	return Range{
		Start: Position{Line: startLine, Character: startCol},
		End:   Position{Line: endLine, Character: endCol},
	}
}

func zeroRange() Range {
	return Range{
		Start: Position{},
		End:   Position{Character: 1},
	}
}

func clampZeroBased(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func diagnosticSource(path string) string {
	if path == "" {
		return "gosx"
	}
	return "gosx"
}

// URIToPath converts a file URI into a local filesystem path.
func URIToPath(uri string) string {
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "file://") {
		parsed, err := url.Parse(uri)
		if err == nil {
			return parsed.Path
		}
	}
	return uri
}
