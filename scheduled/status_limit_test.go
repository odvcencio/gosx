package scheduled

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestStatusLimit(t *testing.T) {
	store := newMemStore()
	s := New(Options{Store: store})
	for i := 99; i >= 0; i-- {
		name := fmt.Sprintf("task-%03d", i)
		if err := s.Register(Task{Name: name, Schedule: Interval(time.Minute), Fn: func(context.Context, TickHandle) error { return nil }}); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 0, 1, 64, 100, 101} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			statuses, complete := s.StatusLimit(limit)
			want := max(0, min(limit, 100))
			if len(statuses) != want || complete != (limit >= 100) {
				t.Fatalf("len=%d complete=%v", len(statuses), complete)
			}
			for i, st := range statuses {
				if st.Name != fmt.Sprintf("task-%03d", i) {
					t.Fatalf("unstable selection at %d: %q", i, st.Name)
				}
			}
		})
	}
	if got := len(s.Status()); got != 100 {
		t.Fatalf("unbounded compatibility Status returned %d", got)
	}
}

func TestStatusLimitCopiesPointers(t *testing.T) {
	store := newMemStore()
	s := New(Options{Store: store})
	if err := s.Register(Task{Name: "refresh", Schedule: Interval(time.Minute), ProgressTimeout: time.Second, Fn: func(context.Context, TickHandle) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	age := int64(42)
	if err := store.Save(TaskStatus{Name: "refresh", CurrentProgressAgeMs: &age}); err != nil {
		t.Fatal(err)
	}
	statuses, complete := s.StatusLimit(1)
	if !complete || len(statuses) != 1 {
		t.Fatalf("snapshot=%v complete=%v", statuses, complete)
	}
	*statuses[0].CurrentProgressAgeMs = 99
	*statuses[0].ProgressTimeoutMs = 99
	original, _ := store.Load("refresh")
	if *original.CurrentProgressAgeMs != 42 || s.Status()[0].ProgressTimeoutMs == statuses[0].ProgressTimeoutMs {
		t.Fatal("snapshot aliases stored pointers")
	}
}

func BenchmarkStatusLimit(b *testing.B) {
	for _, count := range []int{64, 4096} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s := New(Options{})
			for i := 0; i < count; i++ {
				if err := s.Register(Task{Name: fmt.Sprintf("task-%05d", i), Schedule: Interval(time.Minute), Fn: func(context.Context, TickHandle) error { return nil }}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				statuses, _ := s.StatusLimit(64)
				if len(statuses) != 64 {
					b.Fatal("invalid snapshot size")
				}
			}
		})
	}
}
