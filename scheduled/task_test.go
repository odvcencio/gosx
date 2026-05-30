package scheduled

import (
	"testing"
	"time"
)

// fixedClock returns a clock function that always returns t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// advancingClock returns a clock whose time advances by step on each call.
func advancingClock(start time.Time, step time.Duration) func() time.Time {
	current := start
	return func() time.Time {
		t := current
		current = current.Add(step)
		return t
	}
}

func TestTickHandle_Progress(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := advancingClock(base, 5*time.Second)

	handle, rec := newTickHandle(1, clock, nil)

	// Initial lastProgressAt set at construction
	initialAt := rec.lastProgressAt

	// Call Progress
	handle.Progress("step 1")

	if rec.lastProgress != "step 1" {
		t.Errorf("lastProgress: got %q, want %q", rec.lastProgress, "step 1")
	}
	if !rec.lastProgressAt.After(initialAt) {
		t.Errorf("lastProgressAt should advance after Progress call: initial=%v, after=%v", initialAt, rec.lastProgressAt)
	}
}

func TestTickHandle_ProgressUpdatesTimestamp(t *testing.T) {
	// Each Progress call should stamp the current clock time
	t1 := time.Date(2025, 6, 1, 0, 0, 10, 0, time.UTC)
	t2 := time.Date(2025, 6, 1, 0, 0, 20, 0, time.UTC)
	t3 := time.Date(2025, 6, 1, 0, 0, 30, 0, time.UTC)

	calls := []time.Time{t1, t2, t3}
	idx := 0
	clock := func() time.Time {
		ts := calls[idx]
		if idx < len(calls)-1 {
			idx++
		}
		return ts
	}

	handle, rec := newTickHandle(1, clock, nil)
	// construction consumed t1
	handle.Progress("a") // should use t2
	if !rec.lastProgressAt.Equal(t2) {
		t.Errorf("first Progress: got %v, want %v", rec.lastProgressAt, t2)
	}

	handle.Progress("b") // should use t3
	if !rec.lastProgressAt.Equal(t3) {
		t.Errorf("second Progress: got %v, want %v", rec.lastProgressAt, t3)
	}
	if rec.lastProgress != "b" {
		t.Errorf("lastProgress: got %q, want %q", rec.lastProgress, "b")
	}
}

func TestTickHandle_Attempt(t *testing.T) {
	for _, attempt := range []int{1, 2, 5} {
		h, _ := newTickHandle(attempt, fixedClock(time.Now()), nil)
		if got := h.Attempt(); got != attempt {
			t.Errorf("Attempt() = %d, want %d", got, attempt)
		}
	}
}

func TestTickHandle_Metadata(t *testing.T) {
	meta := map[string]string{"env": "prod", "region": "us-west"}
	h, _ := newTickHandle(1, fixedClock(time.Now()), meta)
	got := h.Metadata()
	if got["env"] != "prod" {
		t.Errorf("Metadata env: got %q, want %q", got["env"], "prod")
	}
	if got["region"] != "us-west" {
		t.Errorf("Metadata region: got %q, want %q", got["region"], "us-west")
	}
}

func TestTickHandle_NilMetadata(t *testing.T) {
	h, _ := newTickHandle(1, fixedClock(time.Now()), nil)
	got := h.Metadata()
	if got != nil {
		t.Errorf("Metadata with nil: got %v, want nil", got)
	}
}

func TestNewRunID_Unique(t *testing.T) {
	clock := fixedClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	seen := map[RunID]bool{}
	for i := 0; i < 100; i++ {
		id := newRunID("task1", clock)
		if seen[id] {
			t.Fatalf("duplicate RunID generated: %v", id)
		}
		seen[id] = true
	}
}

func TestNewRunID_ContainsName(t *testing.T) {
	clock := fixedClock(time.Now())
	id := newRunID("my-task", clock)
	idStr := string(id)
	if len(idStr) == 0 {
		t.Fatal("RunID is empty")
	}
	// The ID should contain the task name as a prefix
	if idStr[:len("my-task")] != "my-task" {
		t.Errorf("RunID %q does not start with task name", idStr)
	}
}

func TestTask_EffectiveBackoff_Default(t *testing.T) {
	task := &Task{Name: "t"}
	b := task.effectiveBackoff()
	if b == nil {
		t.Fatal("effectiveBackoff returned nil")
	}
	// Should behave like DefaultBackoff: 1s on attempt 1
	if d := b.NextDelay(1); d != time.Second {
		t.Errorf("default backoff attempt 1: got %v, want 1s", d)
	}
}

func TestTask_EffectiveBackoff_Custom(t *testing.T) {
	task := &Task{Name: "t", Backoff: Fixed(3 * time.Second)}
	b := task.effectiveBackoff()
	if d := b.NextDelay(1); d != 3*time.Second {
		t.Errorf("custom backoff: got %v, want 3s", d)
	}
}
