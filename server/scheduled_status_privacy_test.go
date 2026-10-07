package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/scheduled"
)

func TestScheduledStatusPublicDefault(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(fmt.Sprint(registered), func(t *testing.T) {
			app := New()
			if registered {
				registerStatusTask(t, app.Scheduler(), "refresh")
			}
			h := app.Build()
			for _, path := range []string{"/_gosx/scheduled", "/telemetry/v1/scheduled"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusNotFound {
					t.Fatalf("%s: status %d", path, w.Code)
				}
			}
			if !registered && app.scheduler != nil {
				t.Fatal("Build created a scheduler")
			}
		})
	}
}

func TestScheduledStatusEmptyDoesNotCreateScheduler(t *testing.T) {
	app := New()
	if err := app.EnablePublicScheduledStatus(); err != nil {
		t.Fatal(err)
	}
	for _, h := range []http.Handler{app.Build(), app.ScheduledStatusHandler(), ScheduledStatusHandler(nil)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/_gosx/scheduled", nil))
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
			t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-GoSX-Snapshot-Complete") != "true" {
			t.Fatalf("headers: %v", w.Header())
		}
	}
	if app.scheduler != nil {
		t.Fatal("status handler created a scheduler")
	}
}

func TestScheduledStatusRedactedAndBounded(t *testing.T) {
	store := &scheduledStatusStore{items: make(map[string]scheduled.TaskStatus)}
	s := scheduled.New(scheduled.Options{Store: store})
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("task-%03d", i)
		registerStatusTask(t, s, name)
		if err := store.Save(scheduled.TaskStatus{Name: name, CurrentProgress: "progress-secret-canary", RecentError: "error-secret-canary"}); err != nil {
			t.Fatal(err)
		}
	}
	app := New()
	app.schedulerOnce.Do(func() { app.scheduler = s })
	for _, h := range []http.Handler{ScheduledStatusHandler(s), app.ScheduledStatusHandler()} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/_gosx/scheduled", nil))
		body := w.Body.String()
		for _, forbidden := range []string{"secret-canary", "current_progress\"", "recent_error", "task-064"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("response contains %q", forbidden)
			}
		}
		if strings.Count(body, `"name":`) != 64 || !strings.Contains(body, `"error_class":"task_failed"`) || w.Header().Get("X-GoSX-Snapshot-Complete") != "false" {
			t.Fatalf("bounded response: %s", body)
		}
	}
	statuses, complete := app.ScheduledStatus(1)
	if complete || len(statuses) != 1 || statuses[0].CurrentProgress != "" || statuses[0].RecentError != "task_failed" {
		t.Fatalf("snapshot=%+v complete=%v", statuses, complete)
	}
	original, _ := store.Load("task-000")
	if original.CurrentProgress != "progress-secret-canary" || original.RecentError != "error-secret-canary" {
		t.Fatal("sanitization mutated stored status")
	}
}

func registerStatusTask(t *testing.T, s *scheduled.Scheduler, name string) {
	t.Helper()
	if err := s.Register(scheduled.Task{Name: name, Schedule: scheduled.Interval(time.Minute), Fn: func(context.Context, scheduled.TickHandle) error { return nil }}); err != nil {
		t.Fatal(err)
	}
}

// This store never runs tasks; it seeds status canaries for handler tests.
type scheduledStatusStore struct {
	items map[string]scheduled.TaskStatus
}

func (s *scheduledStatusStore) Save(st scheduled.TaskStatus) error { s.items[st.Name] = st; return nil }
func (s *scheduledStatusStore) Load(name string) (scheduled.TaskStatus, bool) {
	st, ok := s.items[name]
	return st, ok
}
func (s *scheduledStatusStore) List() []scheduled.TaskStatus { return nil }

func TestEnablePublicScheduledStatusBeforeBuild(t *testing.T) {
	app := New()
	app.Build()
	if err := app.EnablePublicScheduledStatus(); !errors.Is(err, telemetryerr.ErrAfterBuild) {
		t.Fatalf("late opt-in: %v", err)
	}
	var nilApp *App
	if err := nilApp.EnablePublicScheduledStatus(); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatalf("nil app: %v", err)
	}
}
