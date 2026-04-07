package textmodel

import "testing"

func TestPosition_Before(t *testing.T) {
	a := Position{Line: 1, Col: 5}
	b := Position{Line: 2, Col: 0}
	if !a.Before(b) {
		t.Fatal("a should be before b")
	}
	if b.Before(a) {
		t.Fatal("b should not be before a")
	}
}

func TestRange_Empty(t *testing.T) {
	r := Range{Start: Position{1, 5}, End: Position{1, 5}}
	if !r.Empty() {
		t.Fatal("same start and end should be empty")
	}
	r2 := Range{Start: Position{1, 5}, End: Position{1, 8}}
	if r2.Empty() {
		t.Fatal("different start and end should not be empty")
	}
}

func TestRange_Contains(t *testing.T) {
	r := Range{Start: Position{1, 0}, End: Position{3, 0}}
	if !r.Contains(Position{2, 5}) {
		t.Fatal("should contain position inside range")
	}
	if r.Contains(Position{4, 0}) {
		t.Fatal("should not contain position outside range")
	}
}
