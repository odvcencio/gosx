package textmodel

// Position is a line+column location in a document.
type Position struct {
	Line int
	Col  int // byte offset within line
}

// Before reports whether p comes before other in document order.
func (p Position) Before(other Position) bool {
	return p.Line < other.Line || (p.Line == other.Line && p.Col < other.Col)
}

// Range is a span between two positions.
type Range struct {
	Start Position
	End   Position
}

// Empty reports whether the range has zero length.
func (r Range) Empty() bool {
	return r.Start == r.End
}

// Contains reports whether pos falls within the range.
func (r Range) Contains(pos Position) bool {
	if pos.Before(r.Start) {
		return false
	}
	if r.End.Before(pos) || r.End == pos {
		return false
	}
	return true
}

// OpKind classifies a document edit.
type OpKind int

const (
	OpInsert OpKind = iota
	OpDelete
	OpReplace
)

// Operation represents a single edit to the document.
type Operation struct {
	Kind    OpKind
	Range   Range
	Content []byte
	Origin  string // "user", "undo", "crdt", "toolbar"
}
