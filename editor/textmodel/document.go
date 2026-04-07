package textmodel

import "strings"

// Document is the interface for a mutable text document.
// Two implementations exist: LocalDocument (direct line array)
// and CRDTDocument (wraps gosx/crdt, built in a later phase).
type Document interface {
	Insert(pos Position, text string)
	Delete(rng Range)
	Replace(rng Range, text string)
	Content() string
	Line(n int) string
	LineCount() int
	Version() int
}

// LocalDocument is a mutable text document stored as a line array.
type LocalDocument struct {
	lines   [][]byte
	version int
}

// NewDocument creates a local document from initial content.
func NewDocument(content string) *LocalDocument {
	var lines [][]byte
	if content == "" {
		lines = [][]byte{{}}
	} else {
		for _, l := range strings.Split(content, "\n") {
			lines = append(lines, []byte(l))
		}
	}
	return &LocalDocument{lines: lines}
}

func (d *LocalDocument) LineCount() int { return len(d.lines) }
func (d *LocalDocument) Version() int   { return d.version }

func (d *LocalDocument) Line(n int) string {
	if n < 0 || n >= len(d.lines) {
		return ""
	}
	return string(d.lines[n])
}

func (d *LocalDocument) Content() string {
	parts := make([]string, len(d.lines))
	for i, l := range d.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (d *LocalDocument) Insert(pos Position, text string) {
	if pos.Line < 0 || pos.Line >= len(d.lines) {
		return
	}
	line := d.lines[pos.Line]
	col := pos.Col
	if col > len(line) {
		col = len(line)
	}

	inserted := []byte(text)
	newLines := strings.Split(string(inserted), "\n")

	if len(newLines) == 1 {
		d.lines[pos.Line] = append(line[:col:col], append(inserted, line[col:]...)...)
	} else {
		before := make([]byte, col)
		copy(before, line[:col])
		after := make([]byte, len(line)-col)
		copy(after, line[col:])

		firstLine := append(before, []byte(newLines[0])...)
		lastLine := append([]byte(newLines[len(newLines)-1]), after...)

		replacement := make([][]byte, 0, len(newLines))
		replacement = append(replacement, firstLine)
		for i := 1; i < len(newLines)-1; i++ {
			replacement = append(replacement, []byte(newLines[i]))
		}
		replacement = append(replacement, lastLine)

		newDoc := make([][]byte, 0, len(d.lines)+len(newLines)-1)
		newDoc = append(newDoc, d.lines[:pos.Line]...)
		newDoc = append(newDoc, replacement...)
		newDoc = append(newDoc, d.lines[pos.Line+1:]...)
		d.lines = newDoc
	}
	d.version++
}

func (d *LocalDocument) Delete(rng Range) {
	if rng.Empty() {
		return
	}
	startLine := d.lines[rng.Start.Line]
	endLine := d.lines[rng.End.Line]

	startCol := rng.Start.Col
	if startCol > len(startLine) {
		startCol = len(startLine)
	}
	endCol := rng.End.Col
	if endCol > len(endLine) {
		endCol = len(endLine)
	}

	merged := make([]byte, 0, startCol+len(endLine)-endCol)
	merged = append(merged, startLine[:startCol]...)
	merged = append(merged, endLine[endCol:]...)

	newDoc := make([][]byte, 0, len(d.lines)-(rng.End.Line-rng.Start.Line))
	newDoc = append(newDoc, d.lines[:rng.Start.Line]...)
	newDoc = append(newDoc, merged)
	newDoc = append(newDoc, d.lines[rng.End.Line+1:]...)
	d.lines = newDoc
	d.version++
}

func (d *LocalDocument) Replace(rng Range, text string) {
	d.Delete(rng)
	d.Insert(rng.Start, text)
}
