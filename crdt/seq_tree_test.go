package crdt

import (
	"math/rand"
	"testing"
)

// walkPrefix recomputes the prefix sums by the forward walk the tree replaced.
func walkPrefix(blocks []*seqBlock) []int {
	out := make([]int, len(blocks)+1)
	for i := range blocks {
		out[i+1] = out[i] + blocks[i].visible
	}
	return out
}

// checkTree compares every tree answer against the forward walk.
func checkTree(t *testing.T, s *seq, step int) {
	t.Helper()
	s.tree.ensure(s.blocks)
	want := walkPrefix(s.blocks)
	for bi := 0; bi <= len(s.blocks); bi++ {
		if got := s.tree.prefixSum(bi); got != want[bi] {
			t.Fatalf("step %d: prefixSum(%d) = %d, want %d", step, bi, got, want[bi])
		}
	}
	for index := 0; index < s.vis; index++ {
		bi, remaining := s.tree.search(index)
		if bi >= len(s.blocks) {
			t.Fatalf("step %d: search(%d) returned block %d of %d", step, index, bi, len(s.blocks))
		}
		if want[bi] > index || want[bi+1] <= index {
			t.Fatalf("step %d: search(%d) chose block %d covering [%d,%d)",
				step, index, bi, want[bi], want[bi+1])
		}
		if remaining != index-want[bi] {
			t.Fatalf("step %d: search(%d) offset = %d, want %d", step, index, remaining, index-want[bi])
		}
	}
}

// TestSeqTreeMatchesForwardWalk drives inserts, splits, and tombstone flips, and
// checks the tree against the forward walk after every step.
func TestSeqTreeMatchesForwardWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	s := newSeq()
	counter := uint64(0)
	ids := make([]OpID, 0, 600)

	for step := 0; step < 600; step++ {
		counter++
		id := OpID{Actor: "a", Counter: counter}
		if len(ids) == 0 {
			s.insertAfter(OpID{}, false, listElem{ID: id, Value: StringValue("x")})
		} else {
			after := ids[rng.Intn(len(ids))]
			s.insertAfter(after, true, listElem{ID: id, Value: StringValue("x")})
		}
		ids = append(ids, id)

		if step%7 == 0 && len(ids) > 1 {
			counter++
			victim := ids[rng.Intn(len(ids))]
			s.setVisibility(victim, true, OpID{Actor: "z", Counter: counter})
		}
		if step%23 == 0 && len(ids) > 1 {
			counter++
			revived := ids[rng.Intn(len(ids))]
			s.setVisibility(revived, false, OpID{Actor: "z", Counter: counter})
		}
		if step%11 == 0 {
			checkTree(t, s, step)
		}
	}
	checkTree(t, s, 600)
}

// TestSeqTreeAfterReset checks the tree once a snapshot load replaces the blocks.
func TestSeqTreeAfterReset(t *testing.T) {
	elems := make([]listElem, 500)
	for i := range elems {
		elems[i] = listElem{
			ID:      OpID{Actor: "a", Counter: uint64(i + 1)},
			Value:   StringValue("x"),
			Deleted: i%5 == 0,
		}
	}
	s := newSeq()
	s.reset(elems)
	checkTree(t, s, 0)

	// An edit after the reload must keep the tree correct.
	id, ok := s.visibleIDAt(s.vis / 2)
	if !ok {
		t.Fatal("mid element missing")
	}
	s.insertAfter(id, true, listElem{
		ID:    OpID{Actor: "b", Counter: 1},
		Value: StringValue("y"),
	})
	checkTree(t, s, 1)
}

// TestSeqVisibleIndexRoundTrip checks that an index maps to an identity and back.
func TestSeqVisibleIndexRoundTrip(t *testing.T) {
	elems := make([]listElem, 1000)
	for i := range elems {
		elems[i] = listElem{
			ID:      OpID{Actor: "a", Counter: uint64(i + 1)},
			Value:   StringValue("x"),
			Deleted: i%3 == 0,
		}
	}
	s := newSeq()
	s.reset(elems)
	for index := 0; index < s.vis; index++ {
		id, ok := s.visibleIDAt(index)
		if !ok {
			t.Fatalf("visibleIDAt(%d) missing", index)
		}
		if got := s.visibleIndexOf(id); got != index {
			t.Fatalf("visibleIndexOf round trip = %d, want %d", got, index)
		}
	}
}

// TestSeqTreeEmptyBlocksAreSkipped checks the descent over blocks that hold no
// visible element.
func TestSeqTreeEmptyBlocksAreSkipped(t *testing.T) {
	elems := make([]listElem, 400)
	for i := range elems {
		// Tombstone the first two blocks completely.
		elems[i] = listElem{
			ID:      OpID{Actor: "a", Counter: uint64(i + 1)},
			Value:   StringValue("x"),
			Deleted: i < 2*seqBlockTarget,
		}
	}
	s := newSeq()
	s.reset(elems)
	checkTree(t, s, 0)

	id, ok := s.visibleIDAt(0)
	if !ok {
		t.Fatal("first visible element missing")
	}
	want := OpID{Actor: "a", Counter: uint64(2*seqBlockTarget + 1)}
	if id != want {
		t.Fatalf("first visible id = %+v, want %+v", id, want)
	}
}
