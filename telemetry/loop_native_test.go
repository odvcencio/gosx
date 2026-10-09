//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
)

// Native attachment uses the same startup and sealing fixture as hub metrics.
func TestLoopKindNativeAdmissionCapsAndReuse(t *testing.T) {
	tel, app := hubTelemetry(t)
	if _, err := tel.NewLoopKind("match", LoopOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	for _, opts := range []LoopOptions{{TickRate: -1}, {TickRate: 1001}, {Budget: -1}} {
		if _, err := tel.NewLoopKind("match", opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	k, err := tel.NewLoopKind("match", LoopOptions{TickRate: 30})
	if err != nil || k.opts.Budget != time.Second/30 {
		t.Fatal(k, err)
	}
	if _, err := tel.NewLoopKind("match", LoopOptions{TickRate: 30}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	other, err := tel.NewLoopKind("round", LoopOptions{Budget: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	app.Build()
	if _, err := tel.NewLoopKind("late", LoopOptions{TickRate: 60}); !errors.Is(err, ErrAfterBuild) {
		t.Fatal(err)
	}
	loops := make([]*Loop, 256)
	for i := range loops {
		kind := k
		if i%2 != 0 {
			kind = other
		}
		loops[i], err = kind.Instance()
		if err != nil {
			t.Fatal(err)
		}
	}
	capped, err := k.Instance()
	if !errors.Is(err, ErrCapacity) || capped.Health().Available {
		t.Fatal(err, capped.Health())
	}
	if err := capped.Observe(2*time.Millisecond, TickInfo{}); err != nil {
		t.Fatal(err)
	}
	if hubSample(t, tel, "gosx_telemetry_dropped_total", "reason", "loop_instances").Counter != 1 {
		t.Fatal("missing cap loss")
	}
	old := loops[0]
	tok := old.Begin()
	old.Close()
	old.Close()
	reused, err := k.Instance()
	if err != nil {
		t.Fatal(err)
	}
	if old.Health().Available {
		t.Fatal(old.Health())
	}
	if err := old.Observe(1, TickInfo{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := reused.End(tok, TickInfo{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	old.Close()
	if err := reused.Observe(1, TickInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := tel.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reused.Health().Available {
		t.Fatal(reused.Health())
	}
	if err := capped.Observe(1, TickInfo{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestLoopKindsAtomicCapacityAndAccountedHeap(t *testing.T) {
	tel, _ := hubTelemetry(t)
	kinds := make([]*LoopKind, 32)
	for i := range kinds {
		var err error
		kinds[i], err = tel.NewLoopKind(fmt.Sprintf("kind_%d", i), LoopOptions{TickRate: 60})
		if err != nil {
			t.Fatal(err)
		}
	}
	beforeUsage := tel.registry.Usage()
	if _, err := tel.NewLoopKind("overflow", LoopOptions{TickRate: 60}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if tel.registry.Usage() != beforeUsage {
		t.Fatal("failed kind mutated reservation")
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	loops := make([]*Loop, 256)
	for i := range loops {
		var err error
		loops[i], err = kinds[i%32].Instance()
		if err != nil {
			t.Fatal(err)
		}
		loops[i].Observe(time.Millisecond, TickInfo{})
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if retained > loopArenaBytes || tel.loops.bytes.Load() != loopArenaBytes {
		t.Fatal(retained, tel.loops.bytes.Load())
	}
	t.Logf("loop retained=%d charged=%d", retained, loopArenaBytes)
	runtime.KeepAlive(loops)
}

func TestLoopKindRegistryCapacityCountsRejection(t *testing.T) {
	baseline, _ := hubTelemetry(t)
	opts := hubCoreOptions()
	opts.Metrics.MaxSeries = baseline.registry.Usage().Samples
	tel, err := Enable(server.New(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	before := tel.registry.Usage()
	if before.Samples != opts.Metrics.MaxSeries || len(tel.loops.kinds) != 0 {
		t.Fatal("fixture must exhaust the registry before the kind limit", before)
	}
	beforeBytes := tel.loops.bytes.Load()
	beforeLoss := hubSample(t, tel, "gosx_telemetry_dropped_total", "reason", "series").Counter
	kind, err := tel.NewLoopKind("match", LoopOptions{TickRate: 60})
	if kind != nil || !errors.Is(err, ErrCapacity) {
		t.Fatal("expected registry-capacity rejection", kind, err)
	}
	if after := tel.registry.Usage(); after != before {
		t.Error("failed kind changed registry reservations", before, after)
	}
	if len(tel.loops.kinds) != 0 || tel.loops.bytes.Load() != beforeBytes {
		t.Error("failed kind changed loop reservations")
	}
	if after := hubSample(t, tel, "gosx_telemetry_dropped_total", "reason", "series").Counter; after != beforeLoss+1 {
		t.Errorf("series rejection counter=%d want=%d", after, beforeLoss+1)
	}
}

func TestLoopConcurrentCloseAndSlotReuse(t *testing.T) {
	tel, _ := hubTelemetry(t)
	k, err := tel.NewLoopKind("match", LoopOptions{TickRate: 60})
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		old, err := k.Instance()
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			for range 1000 {
				if err := old.Observe(time.Millisecond, TickInfo{}); err != nil && !errors.Is(err, ErrClosed) {
					t.Error(err)
					return
				}
				old.Health()
			}
		})
		old.Close()
		current, err := k.Instance()
		if err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if current.Health().Samples != 0 || old.Health().Available {
			t.Fatal("old writer reached reused slot")
		}
		current.Close()
	}
}
