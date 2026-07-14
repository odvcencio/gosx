package textmodel

import (
	"strings"
	"sync"
)

// ExternalOpSource bridges the editor to an external sequence CRDT. Remote
// operations must already be resolved from stable element anchors to document
// positions before delivery.
type ExternalOpSource interface {
	ApplyLocal(Operation)
	SubscribeRemote(func(Operation)) func()
}

// DocumentChange describes one applied local or remote operation. MapPosition
// keeps cursors, selections, and decorations attached across that operation.
type DocumentChange struct {
	Operation Operation
	Version   int
	Remote    bool
	before    string
	after     string
}

// MapPosition maps a position from the document state before the change to the
// state after it. Positions inside replaced text attach to the end of the new
// text, while positions after it retain their relative byte distance.
func (c DocumentChange) MapPosition(position Position) Position {
	oldOffset := positionOffset(c.before, position)
	start := positionOffset(c.before, c.Operation.Range.Start)
	end := positionOffset(c.before, c.Operation.Range.End)
	if end < start {
		end = start
	}
	inserted := len(c.Operation.Content)
	newOffset := oldOffset
	switch {
	case oldOffset < start:
	case oldOffset <= end:
		newOffset = start + inserted
	default:
		newOffset = oldOffset - (end - start) + inserted
	}
	return offsetPosition(c.after, newOffset)
}

// CRDTDocument is a Document driven by an external sequence-CRDT operation
// source. Local edits are forwarded once; remote edits bypass the local-input
// path and therefore cannot echo back into the source.
type CRDTDocument struct {
	mu          sync.RWMutex
	content     string
	version     int
	source      ExternalOpSource
	unsubscribe func()
	listeners   map[uint64]func(DocumentChange)
	nextID      uint64
}

func NewCRDTDocument(content string, source ExternalOpSource) *CRDTDocument {
	document := &CRDTDocument{content: content, source: source, listeners: make(map[uint64]func(DocumentChange))}
	if source != nil {
		document.unsubscribe = source.SubscribeRemote(document.ApplyRemote)
	}
	return document
}

func (d *CRDTDocument) Close() {
	d.mu.Lock()
	unsubscribe := d.unsubscribe
	d.unsubscribe = nil
	d.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
}

func (d *CRDTDocument) Subscribe(listener func(DocumentChange)) func() {
	if listener == nil {
		return func() {}
	}
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	d.listeners[id] = listener
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		delete(d.listeners, id)
		d.mu.Unlock()
	}
}

func (d *CRDTDocument) Insert(position Position, text string) {
	d.apply(Operation{Kind: OpInsert, Range: Range{Start: position, End: position}, Content: []byte(text), Origin: "user"}, false)
}

func (d *CRDTDocument) Delete(rng Range) {
	d.apply(Operation{Kind: OpDelete, Range: rng, Origin: "user"}, false)
}

func (d *CRDTDocument) Replace(rng Range, text string) {
	d.apply(Operation{Kind: OpReplace, Range: rng, Content: []byte(text), Origin: "user"}, false)
}

func (d *CRDTDocument) ApplyRemote(operation Operation) {
	operation.Origin = "crdt"
	d.apply(operation, true)
}

func (d *CRDTDocument) apply(operation Operation, remote bool) {
	d.mu.Lock()
	before := d.content
	start := positionOffset(before, operation.Range.Start)
	end := positionOffset(before, operation.Range.End)
	if operation.Kind == OpInsert {
		end = start
	}
	if end < start {
		end = start
	}
	d.content = before[:start] + string(operation.Content) + before[end:]
	d.version++
	change := DocumentChange{Operation: operation, Version: d.version, Remote: remote, before: before, after: d.content}
	listeners := make([]func(DocumentChange), 0, len(d.listeners))
	for _, listener := range d.listeners {
		listeners = append(listeners, listener)
	}
	source := d.source
	d.mu.Unlock()
	if !remote && source != nil {
		source.ApplyLocal(operation)
	}
	for _, listener := range listeners {
		listener(change)
	}
}

func (d *CRDTDocument) Content() string { d.mu.RLock(); defer d.mu.RUnlock(); return d.content }
func (d *CRDTDocument) Version() int    { d.mu.RLock(); defer d.mu.RUnlock(); return d.version }

func (d *CRDTDocument) Line(n int) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	lines := strings.Split(d.content, "\n")
	if n < 0 || n >= len(lines) {
		return ""
	}
	return lines[n]
}

func (d *CRDTDocument) LineCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return strings.Count(d.content, "\n") + 1
}

func positionOffset(content string, position Position) int {
	if position.Line < 0 {
		return 0
	}
	offset, line := 0, 0
	for line < position.Line {
		index := strings.IndexByte(content[offset:], '\n')
		if index < 0 {
			return len(content)
		}
		offset += index + 1
		line++
	}
	end := strings.IndexByte(content[offset:], '\n')
	if end < 0 {
		end = len(content) - offset
	}
	column := position.Col
	if column < 0 {
		column = 0
	}
	if column > end {
		column = end
	}
	return offset + column
}

func offsetPosition(content string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	prefix := content[:offset]
	line := strings.Count(prefix, "\n")
	column := offset
	if index := strings.LastIndexByte(prefix, '\n'); index >= 0 {
		column = offset - index - 1
	}
	return Position{Line: line, Col: column}
}
