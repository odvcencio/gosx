package crdt

import (
	"fmt"
	"testing"
)

// buildSeq returns a sequence that holds n visible elements.
func buildSeq(n int) *seq {
	elems := make([]listElem, n)
	for i := 0; i < n; i++ {
		elems[i] = listElem{
			ID:    OpID{Actor: "actor", Counter: uint64(i + 1)},
			Value: StringValue("a"),
		}
	}
	s := newSeq()
	s.reset(elems)
	return s
}

// BenchmarkSeqMidInsert measures one insert at the middle of the sequence. The
// prefix sums are stale from the middle block onward after every such insert, so
// this benchmark isolates the cost of the prefix rebuild.
func BenchmarkSeqMidInsert(b *testing.B) {
	for _, n := range []int{10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			s := buildSeq(n)
			counter := uint64(n + 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mid := s.vis / 2
				id, ok := s.visibleIDAt(mid)
				if !ok {
					b.Fatal("mid element missing")
				}
				counter++
				s.insertAfter(id, true, listElem{
					ID:    OpID{Actor: "typer", Counter: counter},
					Value: StringValue("x"),
				})
			}
		})
	}
}

// BenchmarkSeqTailInsert is the same measurement at the tail. The prefix sums
// stay valid up to the last block, so this case was already cheap.
func BenchmarkSeqTailInsert(b *testing.B) {
	for _, n := range []int{10_000, 1_000_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			s := buildSeq(n)
			counter := uint64(n + 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id, ok := s.visibleIDAt(s.vis - 1)
				if !ok {
					b.Fatal("tail element missing")
				}
				counter++
				s.insertAfter(id, true, listElem{
					ID:    OpID{Actor: "typer", Counter: counter},
					Value: StringValue("x"),
				})
			}
		})
	}
}

// BenchmarkSeqRandomVisibleAt measures index lookups spread across the document
// with no edit between them. The prefix sums stay valid, so this measures the
// query path alone.
func BenchmarkSeqRandomVisibleAt(b *testing.B) {
	for _, n := range []int{10_000, 1_000_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			s := buildSeq(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, ok := s.visibleIDAt((i * 7919) % n); !ok {
					b.Fatal("element missing")
				}
			}
		})
	}
}

// BenchmarkSeqSetVisibilityMid measures a tombstone flip at the middle, which
// also invalidates the prefix sums from the middle block onward.
func BenchmarkSeqSetVisibilityMid(b *testing.B) {
	for _, n := range []int{10_000, 1_000_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			s := buildSeq(n)
			ids := make([]OpID, 0, 64)
			for i := 0; i < 64; i++ {
				id, ok := s.visibleIDAt(n/2 + i)
				if !ok {
					b.Fatal("element missing")
				}
				ids = append(ids, id)
			}
			visCounter := uint64(n + 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Flip the same element back and forth so every call
				// changes a block count and stales the index.
				id := ids[i%len(ids)]
				visCounter++
				deleted := (i/len(ids))%2 == 0
				s.setVisibility(id, deleted, OpID{Actor: "zzz", Counter: visCounter})
				// Force a query so the prefix sums must be current again.
				if _, ok := s.visibleIDAt(s.vis / 2); !ok {
					b.Fatal("mid element missing")
				}
			}
		})
	}
}
