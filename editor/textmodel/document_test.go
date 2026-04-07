package textmodel

import "testing"

func TestNewDocument_Empty(t *testing.T) {
	doc := NewDocument("")
	if doc.LineCount() != 1 {
		t.Fatalf("empty doc should have 1 line, got %d", doc.LineCount())
	}
	if doc.Content() != "" {
		t.Fatalf("empty doc content should be empty, got %q", doc.Content())
	}
}

func TestNewDocument_WithContent(t *testing.T) {
	doc := NewDocument("hello\nworld")
	if doc.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", doc.LineCount())
	}
	if doc.Line(0) != "hello" {
		t.Fatalf("line 0 = %q, want %q", doc.Line(0), "hello")
	}
	if doc.Line(1) != "world" {
		t.Fatalf("line 1 = %q, want %q", doc.Line(1), "world")
	}
}

func TestDocument_Insert(t *testing.T) {
	doc := NewDocument("hello\nworld")
	doc.Insert(Position{0, 5}, " there")
	if doc.Line(0) != "hello there" {
		t.Fatalf("after insert line 0 = %q, want %q", doc.Line(0), "hello there")
	}
	if doc.LineCount() != 2 {
		t.Fatalf("line count changed unexpectedly: %d", doc.LineCount())
	}
}

func TestDocument_InsertNewline(t *testing.T) {
	doc := NewDocument("helloworld")
	doc.Insert(Position{0, 5}, "\n")
	if doc.LineCount() != 2 {
		t.Fatalf("expected 2 lines after newline insert, got %d", doc.LineCount())
	}
	if doc.Line(0) != "hello" || doc.Line(1) != "world" {
		t.Fatalf("lines = %q, %q", doc.Line(0), doc.Line(1))
	}
}

func TestDocument_Delete(t *testing.T) {
	doc := NewDocument("hello\nworld")
	doc.Delete(Range{Position{0, 3}, Position{0, 5}})
	if doc.Line(0) != "hel" {
		t.Fatalf("after delete line 0 = %q, want %q", doc.Line(0), "hel")
	}
}

func TestDocument_DeleteAcrossLines(t *testing.T) {
	doc := NewDocument("hello\nworld\nfoo")
	doc.Delete(Range{Position{0, 3}, Position{1, 3}})
	if doc.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", doc.LineCount())
	}
	if doc.Line(0) != "helld" {
		t.Fatalf("merged line = %q, want %q", doc.Line(0), "helld")
	}
}

func TestDocument_Replace(t *testing.T) {
	doc := NewDocument("hello world")
	doc.Replace(Range{Position{0, 6}, Position{0, 11}}, "Go")
	if doc.Content() != "hello Go" {
		t.Fatalf("after replace = %q, want %q", doc.Content(), "hello Go")
	}
}

func TestDocument_Version(t *testing.T) {
	doc := NewDocument("hi")
	v0 := doc.Version()
	doc.Insert(Position{0, 2}, "!")
	if doc.Version() <= v0 {
		t.Fatal("version should increment after edit")
	}
}
