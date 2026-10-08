//go:build !js || !wasm

package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"m31labs.dev/gosx/server"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/metric"
)

type controlledClock struct{ ticker *controlledTicker }

func (c controlledClock) Now() Instant                   { return Instant{Wall: time.Unix(1234, 0)} }
func (c controlledClock) NewTicker(time.Duration) Ticker { return c.ticker }

type controlledTicker struct {
	ch                    chan time.Time
	entered, release      chan struct{}
	stops                 atomic.Int32
	panicStamp, panicStop bool
}

func (t *controlledTicker) C() <-chan time.Time { return t.ch }
func (t *controlledTicker) Stamp(time.Time) Instant {
	if t.panicStamp {
		panic("private-canary")
	}
	return Instant{Wall: time.Unix(1235, 0), Monotonic: time.Second}
}
func (t *controlledTicker) Stop() {
	t.stops.Add(1)
	if t.panicStop {
		panic("private-canary")
	}
	if t.entered != nil {
		close(t.entered)
		<-t.release
	}
}

func TestCloseDeadlineRetainsOneOwner(t *testing.T) {
	o := aggregateCoreOptions(t)
	tick := &controlledTicker{ch: make(chan time.Time), entered: make(chan struct{}), release: make(chan struct{})}
	o.Clock = controlledClock{tick}
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err = tel.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-tick.entered
	select {
	case <-tel.done:
		t.Fatal("released unresolved owner")
	default:
	}
	if tel.Enabled() {
		t.Fatal("producer admission remained open")
	}
	before := runtime.NumGoroutine()
	expired, stop := context.WithCancel(context.Background())
	stop()
	for range 100 {
		if err := tel.Close(expired); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if runtime.NumGoroutine() > before+2 || tick.stops.Load() != 1 {
		t.Fatal("close callers created extra owners or waiters")
	}
	close(tick.release)
	if err = tel.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("late completion lost shared deadline", err)
	}
	if tick.stops.Load() != 1 {
		t.Fatal("ticker stopped twice")
	}
}

func TestConcurrentCloseAndCancelledFlush(t *testing.T) {
	o := aggregateCoreOptions(t)
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = tel.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := tel.Close(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := tel.Close(nil); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
}

func TestCloseBackgroundCallerKeepsSharedDeadline(t *testing.T) {
	o := aggregateCoreOptions(t)
	tick := &controlledTicker{ch: make(chan time.Time), entered: make(chan struct{}), release: make(chan struct{})}
	o.Clock = controlledClock{tick}
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer close(tick.release)
	tel.signal(context.Background())
	deadline, ok := tel.closeContext.Deadline()
	if !ok || time.Until(deadline) > 20*time.Second {
		t.Fatal("close did not cap its shared deadline")
	}
	<-tick.entered
	tel.closeCancel()
	if err := tel.Close(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("background caller ignored the shared context", err)
	}
	select {
	case <-tel.done:
		t.Fatal("close released a blocked owner")
	default:
	}
}

// A context may publish its own cancellation before propagating it to children.
// Hold its AfterFunc callback to make that interval deterministic.
type delayedClosePropagation struct {
	context.Context
	release <-chan struct{}
}

func (c delayedClosePropagation) Value(any) any { return nil }
func (c delayedClosePropagation) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.Context, func() { <-c.release; f() })
}

func TestCloseRecordsElapsedDeadlineBeforeCancellationPropagates(t *testing.T) {
	o := aggregateCoreOptions(t)
	tick := &controlledTicker{ch: make(chan time.Time), entered: make(chan struct{}), release: make(chan struct{})}
	o.Clock = controlledClock{tick}
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	propagate := make(chan struct{})
	defer close(propagate)
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ctx := delayedClosePropagation{parent, propagate}
	if err := tel.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-tick.entered
	sharedDeadline, _ := tel.closeContext.Deadline()
	callerDeadline, _ := parent.Deadline()
	if !sharedDeadline.Equal(callerDeadline) {
		t.Fatal("shared work lost the caller's deadline")
	}
	close(tick.release)
	if err := tel.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("elapsed owner deadline was lost", err)
	}
}

func TestCloseCallerCancellationDoesNotCancelSharedWork(t *testing.T) {
	o := aggregateCoreOptions(t)
	tick := &controlledTicker{ch: make(chan time.Time), entered: make(chan struct{}), release: make(chan struct{})}
	o.Clock = controlledClock{tick}
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	propagate := make(chan struct{})
	defer close(propagate)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := delayedClosePropagation{parent, propagate}
	result := make(chan error, 1)
	go func() { result <- tel.Close(ctx) }()
	<-tick.entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := tel.closeContext.Err(); err != nil {
		t.Fatal("caller cancellation reached shared work", err)
	}
	other := make(chan error, 1)
	go func() { other <- tel.Close(context.Background()) }()
	select {
	case err := <-other:
		t.Fatal("another caller stopped waiting on the first caller's cancellation", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(tick.release)
	if err := <-other; err != nil {
		t.Fatal("caller cancellation poisoned shared completion", err)
	}
}

func TestAlreadyCancelledCloseCallerStillStartsIndependentWork(t *testing.T) {
	o := aggregateCoreOptions(t)
	tick := &controlledTicker{ch: make(chan time.Time), entered: make(chan struct{}), release: make(chan struct{})}
	o.Clock = controlledClock{tick}
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tel.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-tick.entered
	if err := tel.closeContext.Err(); err != nil {
		t.Fatal("cancelled caller cancelled the owner", err)
	}
	close(tick.release)
	if err := tel.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type panickingLogHandler struct{}

func (panickingLogHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (panickingLogHandler) Handle(context.Context, slog.Record) error { panic("private-canary") }
func (h panickingLogHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h panickingLogHandler) WithGroup(string) slog.Handler           { return h }

func TestClockFailureContainsApplicationLoggerPanic(t *testing.T) {
	o := aggregateCoreOptions(t)
	tick := &controlledTicker{ch: make(chan time.Time, 1), panicStamp: true}
	o.Clock, o.Logger = controlledClock{tick}, slog.New(panickingLogHandler{})
	tel, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	tick.ch <- time.Time{}
	select {
	case <-tel.done:
	case <-time.After(time.Second):
		t.Fatal("logger panic prevented worker completion")
	}
	if err := tel.Close(context.Background()); !errors.Is(err, ErrInvalidOptions) || strings.Contains(err.Error(), "private-canary") {
		t.Fatal(err)
	}
	if tel.closeContext != nil || tick.stops.Load() != 1 {
		t.Fatal("completed owner acquired another close timer")
	}
}

func TestClockCallbackPanicIsFixedAndIsolated(t *testing.T) {
	for _, phase := range []string{"stamp", "stop"} {
		t.Run(phase, func(t *testing.T) {
			o := aggregateCoreOptions(t)
			tick := &controlledTicker{ch: make(chan time.Time, 1), panicStamp: phase == "stamp", panicStop: phase == "stop"}
			o.Clock = controlledClock{tick}
			var logs bytes.Buffer
			o.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			tel, err := Enable(server.New(), o)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "stamp" {
				tick.ch <- time.Unix(1, 0)
			} else {
				tel.signal(context.Background())
			}
			select {
			case <-tel.done:
			case <-time.After(time.Second):
				t.Fatal("failed clock worker did not stop")
			}
			err = tel.Close(context.Background())
			if !errors.Is(err, ErrInvalidOptions) || strings.Contains(err.Error(), "private-canary") || strings.Contains(logs.String(), "private-canary") || strings.Count(logs.String(), "telemetry clock failed") != 1 {
				t.Fatalf("panic boundary: %v %s", err, logs.String())
			}
			if err := tel.registry.WithSnapshot(context.Background(), func(snapshot metric.Snapshot) error {
				for _, family := range snapshot.Families {
					if family.Name != "gosx_telemetry_dropped_total" {
						continue
					}
					for _, series := range family.Series {
						reason := series.Labels[0].Value
						if reason == "clock" && series.Counter != 1 || reason == "observer_panic" && series.Counter != 0 {
							t.Fatal("clock failure misclassified", reason, series.Counter)
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
