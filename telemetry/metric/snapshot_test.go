package metric

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshotCopiesAndReentry(t *testing.T) {
	r := &Registry{}
	v, _ := r.NewHistogram(HistogramOptions{Name: "hist", Labels: []Label{{Name: "kind", Values: []string{"ok"}}}, Bounds: []float64{1, 2}})
	h, _ := v.Bind("ok")
	h.Observe(1)
	h.Observe(3)
	var retained Snapshot
	if err := r.WithSnapshot(context.Background(), func(s Snapshot) error {
		if r.Usage().Samples != 5 {
			t.Fatal("callback under registry lock or bad accounting")
		}
		retained = s.Clone()
		f := &s.Families[0]
		series := &f.Series[0]
		if series.Histogram.Counts[0] != 1 || series.Histogram.Counts[1] != 0 || series.Histogram.Counts[2] != 1 {
			t.Fatal("counts are cumulative")
		}
		f.Name = "changed"
		series.Labels[0].Value = "changed"
		series.Histogram.Counts[0] = 100
		series.Histogram.Bounds[0] = 100
		series.Histogram.Counts = nil
		series.Histogram.Bounds = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.Observe(2)
	if err := r.WithSnapshot(context.Background(), func(s Snapshot) error {
		f := s.Families[0]
		h := f.Series[0].Histogram
		if f.Name != "hist" || f.Series[0].Labels[0].Value != "ok" || h.Bounds[0] != 1 || h.Count != 3 || h.Sum != 6 || h.Counts[0] != 1 {
			t.Fatal("snapshot aliases or stale scratch")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if retained.Families[0].Series[0].Histogram.Count != 2 {
		t.Fatal("retained clone changed")
	}
}

func TestSnapshotLeaseFailureAndBounds(t *testing.T) {
	r := &Registry{}
	err := r.WithSnapshot(context.Background(), func(Snapshot) error { panic("PRIVATE_PANIC") })
	if !errors.Is(err, errSnapshotPanic) {
		t.Fatal(err)
	}
	cause := errors.New("PRIVATE_CALLBACK_ERROR")
	err = r.WithSnapshot(context.Background(), func(Snapshot) error { return cause })
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal(err)
	}
	if err := r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }); err != nil {
		t.Fatal("panic retained lease", err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	owner := make(chan error, 1)
	go func() {
		owner <- r.WithSnapshot(context.Background(), func(Snapshot) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.WithSnapshot(ctx, func(Snapshot) error { t.Error("cancelled waiter visited"); return nil })
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		}()
	}
	deadline := time.Now().Add(time.Second)
	for {
		s := r.get()
		s.mu.Lock()
		n := s.snapshotWaiters
		s.mu.Unlock()
		if n == 8 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiters did not arrive")
		}
		time.Sleep(time.Millisecond)
	}
	if err := r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	cancel()
	wg.Wait()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer waitCancel()
	if err := r.WithSnapshot(waitCtx, func(Snapshot) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// Even without a caller deadline, the shared waiter deadline is 2 seconds.
	start := time.Now()
	if err := r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("waiter was unbounded")
	}
	close(release)
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
	if err := r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotConcurrentHistogramAndRegistration(t *testing.T) {
	r := &Registry{}
	v, _ := r.NewHistogram(HistogramOptions{Name: "hist", Bounds: []float64{0, 1, 2}})
	h, _ := v.Bind()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				h.Observe(1)
			}
		}()
	}
	for i := 0; i < 100; i++ {
		if err := r.WithSnapshot(context.Background(), func(s Snapshot) error {
			h := s.Families[0].Series[0].Histogram
			var n uint64
			for _, count := range h.Counts {
				n += count
			}
			if n != h.Count || h.Sum != float64(n) {
				t.Error("torn histogram")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	// Registration during a callback cannot mutate its existing snapshot.
	if err := r.WithSnapshot(context.Background(), func(s Snapshot) error {
		_, err := r.NewCounter(CounterOptions{Name: "earlier"})
		if len(s.Families) != 1 || s.Families[0].Name != "hist" {
			t.Fatal("registration mutated snapshot")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
