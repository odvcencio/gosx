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
	fullRange      byteRange
	selectionRange byteRange
}

type componentRef struct {
	tag            string
	selectionRange byteRange
}

func indexSource(path string, source []byte) (sourceIndex, []Diagnostic) {
	return indexSourceWithSchemas(path, source, nil)
}

func indexSourceWithSchemas(path string, source []byte, schemas *ir.SchemaSet) (sourceIndex, []Diagnostic) {
	idx := sourceIndex{
		source:     string(source),
		components: make(map[string]componentSymbol),
	}

	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return idx, diagnosticsForSourceError(source, err)
	}

	root := tree.RootNode()
	if root.HasError() {
		return idx, diagnosticsForSourceError(source, gosx.DescribeParseError(root, source, lang))
	}

	prog, err := ir.LowerWithOptions(root, source, lang, ir.LowerOptions{Schemas: schemas})
	if err != nil {
		return idx, diagnosticsForSourceError(source, err)
	}
	idx.program = prog
	idx.indexComponents()
	idx.indexComponentRefs()

	// ir.Validate and ir.ValidateWarnings are siblings (see the latter's doc
	// comment): the LSP is the one caller that always wants both, since an
	// author benefits from seeing a warning-severity finding (for example a
	// near-miss data-gosx-* attribute name, gosx#249) as they type just as
	// much as an error -- neither pass performs file I/O, so running both on
	// every keystroke keeps the same "no I/O in ir.Lower's synchronous path"
	// constraint the fast path already holds. A whole-project check that
	// does need I/O (the stylesheet, the action registry, the route tree)
	// is out of scope here; see AnalyzeProject for where those land instead.
	errs := ir.Validate(prog)
	warnings := ir.ValidateWarnings(prog)
	diags := make([]Diagnostic, 0, len(errs)+len(warnings))
	for _, diag := range errs {
		diags = append(diags, Diagnostic{
			Range:    rangeFromSpanInSource(source, diag.Span),
			Severity: severityFromIR(diag.Severity),
			Source:   diagnosticSource(path),
			Message:  diagnosticMessage(diag),
		})
	}
	for _, diag := range warnings {
		diags = append(diags, Diagnostic{
			Range:    rangeFromSpanInSource(source, diag.Span),
			Severity: severityFromIR(diag.Severity),
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
		fullRange := byteRangeFromSpan(idx.source, comp.Span)
		prefix := "func "
		if comp.Syntax == ir.ComponentSyntaxStrict {
			prefix = "component "
		}
		selectionRange := idx.nameRangeInSpan(comp.Span, comp.Name, prefix)
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
	return documentSymbolsFromIndex(idx)
}

func documentSymbolsFromIndex(idx sourceIndex) []DocumentSymbol {
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
			Range:          protocolRangeForByteRange(idx.source, sym.fullRange),
			SelectionRange: protocolRangeForByteRange(idx.source, sym.selectionRange),
		})
	}
	return out
}

// HoverAt returns component hover information for a source position.
func HoverAt(path string, source []byte, pos Position) *Hover {
	idx, _ := indexSource(path, source)
	return hoverFromIndex(idx, pos)
}

func hoverFromIndex(idx sourceIndex, pos Position) *Hover {
	if idx.program == nil {
		return nil
	}
	offset := byteOffsetForProtocolPosition(idx.source, pos)
	for _, sym := range idx.components {
		if !offsetInByteRange(offset, sym.selectionRange) {
			continue
		}
		hover := componentHover(sym)
		hover.Range = protocolRangeForByteRange(idx.source, sym.selectionRange)
		return hover
	}
	for _, ref := range idx.refs {
		if !offsetInByteRange(offset, ref.selectionRange) {
			continue
		}
		sym, ok := idx.components[ref.tag]
		if !ok {
			return nil
		}
		hover := componentHover(sym)
		hover.Range = protocolRangeForByteRange(idx.source, ref.selectionRange)
		return hover
	}
	return nil
}

func DefinitionAt(uri, path string, source []byte, pos Position) *Location {
	idx, _ := indexSource(path, source)
	return definitionFromIndex(uri, idx, pos)
}

func definitionFromIndex(uri string, idx sourceIndex, pos Position) *Location {
	if idx.program == nil {
		return nil
	}
	offset := byteOffsetForProtocolPosition(idx.source, pos)
	for _, ref := range idx.refs {
		if !offsetInByteRange(offset, ref.selectionRange) {
			continue
		}
		sym, ok := idx.components[ref.tag]
		if !ok {
			return nil
		}
		return &Location{
			URI:   uri,
			Range: protocolRangeForByteRange(idx.source, sym.selectionRange),
		}
	}
	return nil
}

func offsetInByteRange(offset int, r byteRange) bool {
	return offset >= r.start && offset < r.end
}

func componentHover(sym componentSymbol) *Hover {
	value := "```go\n" + sym.detail + "\n```\n\n" + sym.description
	return &Hover{
		Contents: markupContent{
			Kind:  "markdown",
			Value: value,
		},
	}
}

func componentSignature(comp ir.Component) string {
	if comp.Syntax == ir.ComponentSyntaxStrict {
		if comp.PropsType == "" {
			return "component " + comp.Name + "()"
		}
		return "component " + comp.Name + "(props: " + comp.PropsType + ")"
	}
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
	case comp.Syntax == ir.ComponentSyntaxStrict:
		return "Strict GoSX server component"
	default:
		return "GoSX component"
	}
}

func (idx sourceIndex) nameRangeInSpan(span ir.Span, name, prefix string) byteRange {
	fallback := byteRangeFromSpan(idx.source, span)
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
	return byteRange{start: absolute, end: absolute + len(name)}
}

func byteRangeFromSpan(source string, span ir.Span) byteRange {
	start := byteOffsetForSourcePoint(source, clampZeroBased(span.StartLine-1), clampZeroBased(span.StartCol-1))
	endLine := span.EndLine
	if endLine <= 0 {
		endLine = span.StartLine
	}
	endCol := span.EndCol
	if endCol <= 0 {
		endCol = span.StartCol + 1
	}
	end := byteOffsetForSourcePoint(source, clampZeroBased(endLine-1), clampZeroBased(endCol-1))
	if end <= start {
		end = nextRuneBoundary(source, start)
	}
	return byteRange{start: start, end: end}
}

func spanOffsets(source string, span ir.Span) (int, int) {
	r := byteRangeFromSpan(source, span)
	return r.start, r.end
}

func rangeForOffset(source string, offset, length int) Range {
	return protocolRangeForByteRange(source, byteRange{start: offset, end: offset + length})
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
	return byteOffsetForProtocolPosition(source, pos)
}

func positionForOffset(source string, offset int) Position {
	return protocolPositionForByteOffset(source, offset)
}
