package lsp

import (
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

const (
	symbolKindFunction = 12
)

// DocumentSymbol is an LSP documentSymbol result.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// Hover is an LSP hover result.
type Hover struct {
	Contents markupContent `json:"contents"`
	Range    Range         `json:"range,omitempty"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Location is an LSP source location.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type sourceIndex struct {
	source     string
	program    *ir.Program
	components map[string]componentSymbol
	refs       []componentRef
}

type componentSymbol struct {
	name           string
	detail         string
	description    string
	fullRange      Range
	selectionRange Range
}

type componentRef struct {
	tag            string
	selectionRange Range
}

func indexSource(path string, source []byte) (sourceIndex, []Diagnostic) {
	idx := sourceIndex{
		source:     string(source),
		components: make(map[string]componentSymbol),
	}

	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return idx, diagnosticsForError(err)
	}

	root := tree.RootNode()
	if root.HasError() {
		return idx, diagnosticsForError(gosx.DescribeParseError(root, source, lang))
	}

	prog, err := ir.Lower(root, source, lang)
	if err != nil {
		return idx, diagnosticsForError(err)
	}
	idx.program = prog
	idx.indexComponents()
	idx.indexComponentRefs()

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

	return idx, diags
}

func (idx *sourceIndex) indexComponents() {
	if idx.program == nil {
		return
	}
	for _, comp := range idx.program.Components {
		fullRange := rangeFromSpan(comp.Span)
		selectionRange := idx.nameRangeInSpan(comp.Span, comp.Name, "func ")
		sym := componentSymbol{
			name:           comp.Name,
			detail:         componentSignature(comp),
			description:    componentDescription(comp),
			fullRange:      fullRange,
			selectionRange: selectionRange,
		}
		idx.components[comp.Name] = sym
	}
}

func (idx *sourceIndex) indexComponentRefs() {
	if idx.program == nil {
		return
	}
	for _, node := range idx.program.Nodes {
		if node.Kind != ir.NodeComponent || node.Tag == "" {
			continue
		}
		idx.refs = append(idx.refs, componentRef{
			tag:            node.Tag,
			selectionRange: idx.nameRangeInSpan(node.Span, node.Tag, "<"),
		})
	}
}

// DocumentSymbols returns top-level GoSX component symbols for an editor.
func DocumentSymbols(path string, source []byte) []DocumentSymbol {
	idx, _ := indexSource(path, source)
	if idx.program == nil {
		return nil
	}
	out := make([]DocumentSymbol, 0, len(idx.program.Components))
	for _, comp := range idx.program.Components {
		sym, ok := idx.components[comp.Name]
		if !ok {
			continue
		}
		out = append(out, DocumentSymbol{
			Name:           sym.name,
			Detail:         sym.detail,
			Kind:           symbolKindFunction,
			Range:          sym.fullRange,
			SelectionRange: sym.selectionRange,
		})
	}
	return out
}

// HoverAt returns component hover information for a source position.
func HoverAt(path string, source []byte, pos Position) *Hover {
	idx, _ := indexSource(path, source)
	if idx.program == nil {
		return nil
	}
	for _, sym := range idx.components {
		if positionInRange(pos, sym.selectionRange) {
			return componentHover(sym)
		}
	}
	for _, ref := range idx.refs {
		if !positionInRange(pos, ref.selectionRange) {
			continue
		}
		sym, ok := idx.components[ref.tag]
		if !ok {
			return nil
		}
		hover := componentHover(sym)
		hover.Range = ref.selectionRange
		return hover
	}
	return nil
}

// DefinitionAt resolves a local component reference to its component definition.
func DefinitionAt(uri, path string, source []byte, pos Position) *Location {
	idx, _ := indexSource(path, source)
	if idx.program == nil {
		return nil
	}
	for _, ref := range idx.refs {
		if !positionInRange(pos, ref.selectionRange) {
			continue
		}
		sym, ok := idx.components[ref.tag]
		if !ok {
			return nil
		}
		return &Location{
			URI:   uri,
			Range: sym.selectionRange,
		}
	}
	return nil
}

func componentHover(sym componentSymbol) *Hover {
	value := "```go\n" + sym.detail + "\n```\n\n" + sym.description
	return &Hover{
		Contents: markupContent{
			Kind:  "markdown",
			Value: value,
		},
		Range: sym.selectionRange,
	}
}

func componentSignature(comp ir.Component) string {
	if comp.PropsType == "" {
		return "func " + comp.Name + "() Node"
	}
	return "func " + comp.Name + "(props " + comp.PropsType + ") Node"
}

func componentDescription(comp ir.Component) string {
	switch {
	case comp.IsEngine && comp.EngineKind != "":
		return "GoSX engine component (" + comp.EngineKind + ")"
	case comp.IsEngine:
		return "GoSX engine component"
	case comp.IsIsland:
		return "GoSX island component"
	default:
		return "GoSX component"
	}
}

func (idx sourceIndex) nameRangeInSpan(span ir.Span, name, prefix string) Range {
	fallback := rangeFromSpan(span)
	if name == "" {
		return fallback
	}
	start, end := spanOffsets(idx.source, span)
	if start < 0 || end <= start || start >= len(idx.source) {
		return fallback
	}
	if end > len(idx.source) {
		end = len(idx.source)
	}
	window := idx.source[start:end]
	offset := -1
	if prefix != "" {
		offset = strings.Index(window, prefix+name)
		if offset >= 0 {
			offset += len(prefix)
		}
	}
	if offset < 0 {
		offset = strings.Index(window, name)
	}
	if offset < 0 {
		return fallback
	}
	absolute := start + offset
	return rangeForOffset(idx.source, absolute, len(name))
}

func spanOffsets(source string, span ir.Span) (int, int) {
	start := positionOffset(source, Position{
		Line:      clampZeroBased(span.StartLine - 1),
		Character: clampZeroBased(span.StartCol - 1),
	})
	end := positionOffset(source, Position{
		Line:      clampZeroBased(span.EndLine - 1),
		Character: clampZeroBased(span.EndCol - 1),
	})
	return start, end
}

func rangeForOffset(source string, offset, length int) Range {
	if length < 0 {
		length = 0
	}
	return Range{
		Start: positionForOffset(source, offset),
		End:   positionForOffset(source, offset+length),
	}
}

func positionInRange(pos Position, r Range) bool {
	if comparePosition(pos, r.Start) < 0 {
		return false
	}
	return comparePosition(pos, r.End) < 0
}

func comparePosition(a, b Position) int {
	if a.Line < b.Line {
		return -1
	}
	if a.Line > b.Line {
		return 1
	}
	if a.Character < b.Character {
		return -1
	}
	if a.Character > b.Character {
		return 1
	}
	return 0
}

func positionOffset(source string, pos Position) int {
	if pos.Line <= 0 && pos.Character <= 0 {
		return 0
	}
	line := 0
	lineStart := 0
	for i := 0; i < len(source); i++ {
		if line == pos.Line {
			offset := lineStart + pos.Character
			if offset > len(source) {
				return len(source)
			}
			lineEnd := strings.IndexByte(source[lineStart:], '\n')
			if lineEnd < 0 {
				lineEnd = len(source)
			} else {
				lineEnd += lineStart
			}
			if offset > lineEnd {
				return lineEnd
			}
			return offset
		}
		if source[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	if line == pos.Line {
		offset := lineStart + pos.Character
		if offset > len(source) {
			return len(source)
		}
		return offset
	}
	return len(source)
}

func positionForOffset(source string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return Position{
		Line:      line,
		Character: offset - lineStart,
	}
}
