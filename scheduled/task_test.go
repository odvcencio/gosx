package scheduled

import (
	"testing"
	"time"
)

// fixedClock returns a clock function that always returns t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
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
