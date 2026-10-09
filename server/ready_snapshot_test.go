package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLastReadinessUnknownWithoutProbe(t *testing.T) {
	var nilApp *App
	if got := nilApp.LastReadiness(); got.Known || !got.EvaluatedAt.IsZero() || len(got.Checks) != 0 {
		t.Fatalf("nil snapshot = %+v", got)
	}
	app := New()
	var calls atomic.Int32
	app.UseReadyCheck("store", ReadyCheckFunc(func(context.Context) error { calls.Add(1); return nil }))
	handler := app.Build()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	for range 10 {
		got := app.LastReadiness()
		if got.Known || got.OK || got.Complete || !got.EvaluatedAt.IsZero() || len(got.Checks) != 0 {
			t.Fatalf("unprobed snapshot = %+v", got)
		}
	}
	if calls.Load() != 0 || app.scheduler != nil {
		t.Fatalf("snapshot created work: calls=%d scheduler=%v", calls.Load(), app.scheduler)
	}
}

func TestLastReadinessCopiesCompletedProbe(t *testing.T) {
	app := New()
	var fail atomic.Bool
	app.UseReadyCheck("store", ReadyCheckFunc(func(context.Context) error {
		if fail.Load() {
			return errors.New("private-error-canary")
		}
		return nil
	}))
	app.UseReadyCheck("worker", ReadyCheckFunc(func(context.Context) error { return nil }))
	handler := app.Build()
	before := time.Now().UTC()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	first := app.LastReadiness()
	if w.Code != 200 || !first.Known || !first.OK || !first.Complete || len(first.Checks) != 2 {
		t.Fatalf("success: status=%d snapshot=%+v", w.Code, first)
	}
	if first.EvaluatedAt.Before(before) || first.EvaluatedAt.After(time.Now()) || first.EvaluatedAt.Location() != time.UTC {
		t.Fatalf("completion timestamp = %v", first.EvaluatedAt)
	}
	if first.Checks[0] != (ReadySnapshotCheck{"store", true}) || first.Checks[1] != (ReadySnapshotCheck{"worker", true}) {
		t.Fatalf("checks = %+v", first.Checks)
	}
	first.Checks[0] = ReadySnapshotCheck{"caller-mutation", false}
	if got := app.LastReadiness(); got.Checks[0].Name != "store" || !got.Checks[0].OK {
		t.Fatalf("caller changed cached snapshot: %+v", got)
	}
	fail.Store(true)
	r := httptest.NewRequest("GET", "/readyz", nil)
	r.Header.Set("X-Request-ID", "request-id-canary")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	second := app.LastReadiness()
	if w.Code != 503 || !second.Known || second.OK || !second.Complete || second.Checks[0].OK || !second.Checks[1].OK {
		t.Fatalf("failure: status=%d snapshot=%+v", w.Code, second)
	}
	if second.EvaluatedAt.Before(first.EvaluatedAt) {
		t.Fatal("completion timestamp went backwards")
	}
	encoded, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-error-canary", "request-id-canary", "RequestID", "Error", "progress"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("snapshot retained %q: %s", forbidden, encoded)
		}
	}
}

func TestLastReadinessIncludesFailureBeyondSnapshotCap(t *testing.T) {
	app := New()
	var calls atomic.Int32
	for i := range 65 {
		app.UseReadyCheck(fmt.Sprintf("check-%02d", i), ReadyCheckFunc(func(context.Context) error {
			calls.Add(1)
			if i == 64 {
				return errors.New("unavailable")
			}
			return nil
		}))
	}
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	got := app.LastReadiness()
	if w.Code != 503 || !got.Known || got.OK || got.Complete || len(got.Checks) != 64 || calls.Load() != 65 {
		t.Fatalf("status=%d calls=%d snapshot=%+v", w.Code, calls.Load(), got)
	}
	for i, check := range got.Checks {
		if !check.OK || check.Name != fmt.Sprintf("check-%02d", i) {
			t.Fatalf("included check %d = %+v", i, check)
		}
	}
	var report ReadinessReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) != 65 || report.Checks[64].OK {
		t.Fatalf("public probe lost evaluated checks: %+v", report)
	}
}

func TestLastReadinessNameBounds(t *testing.T) {
	app := New()
	names := []struct{ input, want string }{
		{"  store  ", "store"},
		{"", "unnamed"},
		{strings.Repeat("x", 128), strings.Repeat("x", 128)},
		{strings.Repeat("x", 129), "other"},
		{strings.Repeat("界", 42), strings.Repeat("界", 42)},
		{strings.Repeat("界", 43), "other"},
		{"bad\x00name", "other"},
		{"bad\nname", "other"},
		{"bad\x7fname", "other"},
		{"bad\xffname", "other"},
	}
	for _, name := range names {
		app.UseReadyCheck(name.input, ReadyCheckFunc(func(context.Context) error { return nil }))
	}
	app.UseReadyCheck("ignored", nil)
	app.Build().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/readyz", nil))
	got := app.LastReadiness()
	if len(got.Checks) != len(names) || !got.Complete {
		t.Fatalf("checks = %+v", got)
	}
	for i, name := range names {
		if got.Checks[i].Name != name.want {
			t.Fatalf("name %d = %q, want %q", i, got.Checks[i].Name, name.want)
		}
	}
}

func TestLastReadinessPublishesOnlyAfterCallbacksReturn(t *testing.T) {
	app := New()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	app.UseReadyCheck("blocked", ReadyCheckFunc(func(context.Context) error {
		// A callback may inspect the previous result: no cache lock is held.
		if app.LastReadiness().Known {
			return errors.New("unexpected prior result")
		}
		calls.Add(1)
		close(entered)
		<-release
		return nil
	}))
	handler := app.Build()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/readyz", nil))
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not enter callback")
	}
	for range 100 {
		if got := app.LastReadiness(); got.Known {
			t.Fatalf("published an unfinished probe: %+v", got)
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not finish")
	}
	if got := app.LastReadiness(); !got.Known || !got.OK || calls.Load() != 1 {
		t.Fatalf("completed snapshot=%+v calls=%d", got, calls.Load())
	}
}

func TestLastReadinessConcurrentProbesAndCopies(t *testing.T) {
	app := New()
	var calls atomic.Int32
	for _, name := range []string{"store", "worker"} {
		app.UseReadyCheck(name, ReadyCheckFunc(func(context.Context) error { calls.Add(1); return nil }))
	}
	handler := app.Build()
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 10 {
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/readyz", nil))
			}
		}()
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				got := app.LastReadiness()
				if !got.Known {
					continue
				}
				if !got.OK || !got.Complete || len(got.Checks) != 2 || got.Checks[0].Name != "store" || got.Checks[1].Name != "worker" {
					t.Errorf("inconsistent snapshot: %+v", got)
					return
				}
				got.Checks[0].Name = "caller-mutation"
			}
		}()
	}
	group.Wait()
	if got := app.LastReadiness(); !got.Known || calls.Load() != 320 || got.Checks[0].Name != "store" {
		t.Fatalf("final snapshot=%+v calls=%d", got, calls.Load())
	}
}

func TestLastReadinessDrainingClearsChecks(t *testing.T) {
	app := New()
	var calls atomic.Int32
	app.UseReadyCheck("store", ReadyCheckFunc(func(context.Context) error { calls.Add(1); return nil }))
	handler := app.Build()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/readyz", nil))
	app.draining.Store(true)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	got := app.LastReadiness()
	if w.Code != 503 || !got.Known || got.OK || !got.Complete || len(got.Checks) != 0 || calls.Load() != 1 {
		t.Fatalf("draining: status=%d calls=%d snapshot=%+v", w.Code, calls.Load(), got)
	}
	if app.readiness.checks[0] != (ReadySnapshotCheck{}) {
		t.Fatal("draining snapshot retained an old check")
	}
}

func TestReadinessPanicDisablesOnlyFailedCallback(t *testing.T) {
	app := New()
	var panics, healthy atomic.Int32
	app.UseReadyCheck("panicking", ReadyCheckFunc(func(context.Context) error {
		panics.Add(1)
		panic("private-panic-canary")
	}))
	app.UseReadyCheck("healthy", ReadyCheckFunc(func(context.Context) error { healthy.Add(1); return nil }))
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	handler := app.Build()
	for range 2 {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
		got := app.LastReadiness()
		if w.Code != 503 || !got.Known || got.OK || !got.Complete || len(got.Checks) != 2 || got.Checks[0].OK || !got.Checks[1].OK {
			t.Fatalf("panicking probe: status=%d snapshot=%+v", w.Code, got)
		}
		if strings.Contains(w.Body.String(), "private-panic-canary") {
			t.Fatal("public response exposed panic text")
		}
	}
	if panics.Load() != 1 || healthy.Load() != 2 || strings.Contains(logs.String(), "private-panic-canary") || strings.Count(logs.String(), "readiness_callback_panic") != 1 {
		t.Fatalf("callbacks=%d,%d logs=%q", panics.Load(), healthy.Load(), logs.String())
	}
}

func TestLastReadinessUsesProbeContext(t *testing.T) {
	app := New()
	app.UseReadyCheck("canceled", ReadyCheckFunc(func(ctx context.Context) error { return ctx.Err() }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, r)
	if got := app.LastReadiness(); w.Code != 503 || got.OK || !got.Known || got.Checks[0].OK {
		t.Fatalf("canceled probe: status=%d snapshot=%+v", w.Code, got)
	}
}
