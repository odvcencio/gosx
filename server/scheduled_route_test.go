package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"m31labs.dev/gosx/scheduled"
)

// TestScheduledStatusEndpoint verifies that GET /_gosx/scheduled returns
// a 2-element JSON array with expected fields for two registered tasks.
func TestScheduledStatusEndpoint(t *testing.T) {
	app := New()

	// Register two tasks before building: one interval, one with ProgressTimeout.
	if err := app.Scheduler().Register(scheduled.Task{
		Name:     "sync-data",
		Schedule: scheduled.Interval(5 * time.Minute),
		Fn: func(ctx context.Context, tick scheduled.TickHandle) error {
			return nil
		},
	}); err != nil {
		t.Fatalf("register sync-data: %v", err)
	}
	if err := app.Scheduler().Register(scheduled.Task{
		Name:            "health-check",
		Schedule:        scheduled.Interval(30 * time.Second),
		ProgressTimeout: 10 * time.Second,
		Fn: func(ctx context.Context, tick scheduled.TickHandle) error {
			return nil
		},
	}); err != nil {
		t.Fatalf("register health-check: %v", err)
	}

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/_gosx/scheduled", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct == "" || len(ct) < len("application/json") || ct[:len("application/json")] != "application/json" {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, w.Body.String())
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %s", len(items), w.Body.String())
	}

	// Build a map by name for easy lookup.
	byName := make(map[string]map[string]interface{}, 2)
	for _, item := range items {
		name, _ := item["name"].(string)
		byName[name] = item
	}

	for _, name := range []string{"sync-data", "health-check"} {
		item, ok := byName[name]
		if !ok {
			t.Fatalf("task %q not found in response; items: %v", name, items)
		}
		if item["name"] != name {
			t.Errorf("task %q: unexpected name %v", name, item["name"])
		}
		sched, _ := item["schedule"].(string)
		if sched == "" {
			t.Errorf("task %q: schedule should be non-empty", name)
		}
	}

	// health-check has a ProgressTimeout so progress_timeout_ms must be present.
	hc := byName["health-check"]
	if _, ok := hc["progress_timeout_ms"]; !ok {
		t.Errorf("health-check: expected progress_timeout_ms field")
	}
}

// TestSchedulerSameInstance verifies that Scheduler() returns the same instance.
func TestSchedulerSameInstance(t *testing.T) {
	app := New()
	s1 := app.Scheduler()
	s2 := app.Scheduler()
	if s1 != s2 {
		t.Fatalf("Scheduler() returned different instances on repeated calls")
	}
}

// TestShutdownNoPanic verifies that Shutdown does not panic with or without
// a started scheduler.
func TestShutdownNoPanic(t *testing.T) {
	t.Run("without scheduler started", func(t *testing.T) {
		app := New()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// srv is nil (ListenAndServe not called), Shutdown should not panic.
		if err := app.Shutdown(ctx); err != nil {
			// nil srv means no error expected
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("with scheduler accessed but not started", func(t *testing.T) {
		app := New()
		_ = app.Scheduler() // create scheduler via accessor
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
