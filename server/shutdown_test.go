package server

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/scheduled"
)

func TestShutdownHookConfiguration(t *testing.T) {
	a := New()
	if !a.ConfigurationOpen() {
		t.Fatal("new App configuration is closed")
	}
	if _, err := a.UseShutdownHook(" ", ShutdownHooks{}); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatalf("empty name: %v", err)
	}
	remove, err := a.UseShutdownHook("reserved", ShutdownHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UseShutdownHook("reserved", ShutdownHooks{}); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatalf("duplicate: %v", err)
	}
	remove()
	remove()
	var calls atomic.Int64
	remove, err = a.UseShutdownHook("reserved", ShutdownHooks{Flush: func(context.Context) error { calls.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	a.Build()
	remove() // removal after Build must not detach an active owner
	if a.ConfigurationOpen() {
		t.Fatal("Build did not close configuration")
	}
	if _, err := a.UseShutdownHook("late", ShutdownHooks{}); !errors.Is(err, telemetryerr.ErrAfterBuild) {
		t.Fatalf("late hook: %v", err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("flush calls: %d", calls.Load())
	}
	var nilApp *App
	if nilApp.ConfigurationOpen() {
		t.Fatal("nil App configuration is open")
	}
	if _, err := nilApp.UseShutdownHook("owner", ShutdownHooks{}); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatalf("nil App: %v", err)
	}
	if err := nilApp.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type testShutdownSource struct{ calls []string }

func (s *testShutdownSource) SignalShutdown(context.Context) { s.calls = append(s.calls, "signal") }
func (s *testShutdownSource) Drain(context.Context) error {
	s.calls = append(s.calls, "drain")
	return nil
}

func TestShutdownSourceUsesExistingPipeline(t *testing.T) {
	a := New()
	if _, err := a.UseShutdownSource("missing", nil); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal(err)
	}
	source := &testShutdownSource{}
	if _, err := a.UseShutdownSource("resource", source); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UseShutdownSource("resource", source); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal(err)
	}
	a.Build()
	if _, err := a.UseShutdownSource("late", source); !errors.Is(err, telemetryerr.ErrAfterBuild) {
		t.Fatal(err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.calls, []string{"signal", "drain"}) {
		t.Fatal(source.calls)
	}
}

func TestShutdownHookRollbackPreservesRegistrationOrder(t *testing.T) {
	a := New()
	var calls []string
	add := func(name string) func() {
		remove, err := a.UseShutdownHook(name, ShutdownHooks{Drain: func(context.Context) error { calls = append(calls, name); return nil }})
		if err != nil {
			t.Fatal(err)
		}
		return remove
	}
	stale := add("removed")
	add("first")
	stale()
	for range 1000 {
		remove := add("retry")
		remove()
		remove()
	}
	add("last")
	stale()
	if len(a.shutdown.hooks) != 2 {
		t.Fatal("rollback retained hook slots")
	}
	a.Build()
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"first", "last"}) {
		t.Fatal(calls)
	}
}

func TestShutdownHookOrderAndClassifiedErrors(t *testing.T) {
	a := New()
	var order []string
	appendStep := func(step string) { order = append(order, step) }
	first, second := errors.New("PRIVATE_FIRST_ERROR"), errors.New("PRIVATE_SECOND_ERROR")
	for _, name := range []string{"telemetry", "room", "runner"} {
		name := name
		hooks := ShutdownHooks{
			Signal: func(context.Context) { appendStep("signal_" + name) },
			Flush: func(context.Context) error {
				appendStep("flush_" + name)
				if name == "telemetry" {
					return second
				}
				return nil
			},
		}
		if name != "telemetry" {
			hooks.Drain = func(context.Context) error {
				appendStep("drain_" + name)
				if name == "room" {
					return first
				}
				return nil
			}
		}
		if _, err := a.UseShutdownHook(name, hooks); err != nil {
			t.Fatal(err)
		}
	}
	err := a.Shutdown(context.Background())
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("original causes are unavailable: %v", err)
	}
	if strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("error exports arbitrary text: %v", err)
	}
	want := []string{"signal_room", "signal_runner", "drain_room", "drain_runner", "flush_runner", "flush_room", "flush_telemetry"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
	if again := a.Shutdown(context.Background()); again != err {
		t.Fatalf("shutdown did not share its result: %v", again)
	}
}

func TestShutdownDeadlineSignalsRemainingOwners(t *testing.T) {
	a := New()
	entered, release, telemetrySignal := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var drains, signals atomic.Int64
	if _, err := a.UseShutdownHook("telemetry", ShutdownHooks{Signal: func(context.Context) { signals.Add(1); close(telemetrySignal) }}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UseShutdownHook("blocked_owner", ShutdownHooks{
		Drain: func(context.Context) error { drains.Add(1); close(entered); <-release; return nil },
	}); err != nil {
		t.Fatal(err)
	}
	handler := a.Build()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- a.Shutdown(ctx) }()
	<-entered
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("draining readiness: %d %s", w.Code, w.Body.String())
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller waited for ignoring Drain")
	}
	select {
	case <-telemetrySignal:
	case <-time.After(time.Second):
		t.Fatal("deadline did not signal flush-only owner")
	}
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if runtime.NumGoroutine() > before+2 {
		t.Fatal("repeated callers created waiters")
	}
	a.shutdown.mu.Lock()
	owners, done := len(a.shutdown.hooks), a.shutdown.done
	a.shutdown.mu.Unlock()
	if owners != 2 || drains.Load() != 1 || signals.Load() != 1 {
		t.Fatalf("unresolved ownership: owners=%d drains=%d signals=%d", owners, drains.Load(), signals.Load())
	}
	select {
	case <-done:
		t.Fatal("ignoring Drain was reported complete")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	finish, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	if err := a.Shutdown(finish); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shared deadline lost: %v", err)
	}
}

func TestShutdownConcurrentCallersUseFirstContext(t *testing.T) {
	a := New()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var drains atomic.Int64
	if _, err := a.UseShutdownHook("owner", ShutdownHooks{Drain: func(context.Context) error {
		drains.Add(1)
		close(entered)
		<-release
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- a.Shutdown(context.Background()) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second caller: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Shutdown(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	releaseOnce.Do(func() { close(release) })
	wg.Wait()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if drains.Load() != 1 {
		t.Fatalf("drains=%d", drains.Load())
	}
}

func TestShutdownPanicIsFixedAndIsolated(t *testing.T) {
	a := New()
	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldWriter)
	var flushed atomic.Bool
	if _, err := a.UseShutdownHook("panicking", ShutdownHooks{
		Signal: func(context.Context) { panic("PRIVATE_SIGNAL") },
		Drain:  func(context.Context) error { panic("PRIVATE_DRAIN") },
		Flush:  func(context.Context) error { panic("PRIVATE_FLUSH") },
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UseShutdownHook("remaining", ShutdownHooks{Flush: func(context.Context) error { flushed.Store(true); return nil }}); err != nil {
		t.Fatal(err)
	}
	err := a.Shutdown(context.Background())
	if err == nil || !flushed.Load() {
		t.Fatalf("panic isolation: err=%v flushed=%v", err, flushed.Load())
	}
	if strings.Contains(err.Error(), "PRIVATE") || strings.Contains(logs.String(), "PRIVATE") || strings.Count(logs.String(), "shutdown callback panic") != 1 {
		t.Fatalf("panic log/error leaked or repeated: %s / %v", logs.String(), err)
	}
}

func TestShutdownDrainsOwnedHTTPThenScheduler(t *testing.T) {
	a := New()
	requestEntered, requestRelease := make(chan struct{}), make(chan struct{})
	schedulerEntered, schedulerRelease, schedulerFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var requestOnce, schedulerOnce sync.Once
	defer requestOnce.Do(func() { close(requestRelease) })
	defer schedulerOnce.Do(func() { close(schedulerRelease) })
	a.Mount("/blocked", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestEntered)
		<-requestRelease
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := a.Scheduler().Register(scheduled.Task{Name: "work", Fn: func(context.Context, scheduled.TickHandle) error {
		close(schedulerEntered)
		<-schedulerRelease
		close(schedulerFinished)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Scheduler().Enqueue("work", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Scheduler().Register(scheduled.Task{Name: "probe", Fn: func(context.Context, scheduled.TickHandle) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	<-schedulerEntered
	drained := make(chan struct{})
	if _, err := a.UseShutdownHook("source", ShutdownHooks{Drain: func(context.Context) error {
		select {
		case <-schedulerFinished:
		default:
			t.Error("source drained before scheduler")
		}
		close(drained)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a.srv = &http.Server{Handler: a.Build()}
	defer a.srv.Close()
	serveDone := make(chan error, 1)
	go func() { serveDone <- a.srv.Serve(listener) }()
	requestDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String() + "/blocked")
		if err == nil {
			resp.Body.Close()
		}
		requestDone <- err
	}()
	<-requestEntered
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- a.Shutdown(ctx) }()
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
	if _, err := a.Scheduler().Enqueue("probe", nil); err != nil {
		t.Fatalf("scheduler stopped before HTTP drained: %v", err)
	}
	requestOnce.Do(func() { close(requestRelease) })
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
		t.Fatal("source drained before scheduler finished")
	default:
	}
	schedulerOnce.Do(func() { close(schedulerRelease) })
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
}
