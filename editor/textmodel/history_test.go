package textmodel

import "testing"

func TestHistory_UndoRedo(t *testing.T) {
	h := NewHistory()
	h.Push(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 0}}, Content: []byte("hello"), Origin: "user"})

	op, ok := h.Undo()
	if !ok {
		t.Fatal("should have undo")
	}
	if op.Kind != OpInsert {
		t.Fatal("undo should return the original op")
	}

	op2, ok := h.Redo()
	if !ok {
		t.Fatal("should have redo")
	}
	if string(op2.Content) != "hello" {
		t.Fatal("redo should return the operation")
	}
}

func TestHistory_CoalesceTyping(t *testing.T) {
	h := NewHistory()
	h.Push(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 0}}, Content: []byte("a"), Origin: "user"})
	h.Push(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 1}}, Content: []byte("b"), Origin: "user"})
	h.Push(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 2}}, Content: []byte("c"), Origin: "user"})

	_, ok := h.Undo()
	if !ok {
		t.Fatal("should have undo")
	}
	_, ok = h.Undo()
	if ok {
		t.Fatal("should only have one undo group for consecutive typing")
	}
}

func TestHistory_ToolbarBreaksCoalesce(t *testing.T) {
	h := NewHistory()
	h.Push(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 0}}, Content: []byte("a"), Origin: "user"})
	h.Push(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 0}}, Content: []byte("**"), Origin: "toolbar"})

	h.Undo()
	op, ok := h.Undo()
	if !ok {
		t.Fatal("should still have the typing undo")
	}
	if string(op.Content) != "a" {
		t.Fatalf("expected 'a', got %q", string(op.Content))
	}
}

func TestHistory_RedoClearedOnNewEdit(t *testing.T) {
	h := NewHistory()
	h.Push(Operation{Kind: OpInsert, Content: []byte("a"), Origin: "user"})
	h.Push(Operation{Kind: OpInsert, Content: []byte("b"), Origin: "user"})
	h.Undo()
	h.Push(Operation{Kind: OpInsert, Content: []byte("c"), Origin: "toolbar"})
	_, ok := h.Redo()
	if ok {
		t.Fatal("redo should be cleared after new edit")
	}
}

func TestHistory_UndoActorDoesNotSelectRemoteEdit(t *testing.T) {
	h := NewHistory()
	h.Push(Operation{Kind: OpInsert, Content: []byte("operator"), Origin: "user", Actor: "operator"})
	h.Push(Operation{Kind: OpInsert, Content: []byte("agent"), Origin: "crdt", Actor: "agent"})

	op, ok := h.UndoActor("operator")
	if !ok || string(op.Content) != "operator" {
		t.Fatalf("operator undo = %#v, ok=%v", op, ok)
	}
	if _, ok := h.UndoActor("operator"); ok {
		t.Fatal("operator undo selected another actor's edit")
	}
	op, ok = h.UndoActor("agent")
	if !ok || string(op.Content) != "agent" {
		t.Fatalf("agent undo = %#v, ok=%v", op, ok)
	}
}

func TestHistory_NewEditClearsOnlySameActorRedo(t *testing.T) {
	h := NewHistory()
	h.Push(Operation{Kind: OpInsert, Content: []byte("a"), Origin: "user", Actor: "operator"})
	h.Push(Operation{Kind: OpInsert, Content: []byte("b"), Origin: "user", Actor: "agent"})
	h.UndoActor("operator")
	h.UndoActor("agent")
	h.Push(Operation{Kind: OpInsert, Content: []byte("new"), Origin: "toolbar", Actor: "operator"})
	if _, ok := h.RedoActor("operator"); ok {
		t.Fatal("same-actor redo survived a new edit")
	}
	if op, ok := h.RedoActor("agent"); !ok || string(op.Content) != "b" {
		t.Fatalf("other actor redo was cleared: %#v, ok=%v", op, ok)
	}
}
