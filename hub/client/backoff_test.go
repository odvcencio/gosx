package hubclient

import (
	"testing"
	"time"
)

func TestBackoffDefaultSchedule(t *testing.T) {
	b := DefaultBackoff()
	want := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		5 * time.Second,
		5 * time.Second,
	}
	for i, expect := range want {
		attempt := i + 1
		if got := b.Delay(attempt); got != expect {
			t.Fatalf("Delay(%d) = %s, want %s", attempt, got, expect)
		}
	}
}

func TestBackoffZeroAttemptIsZero(t *testing.T) {
	b := DefaultBackoff()
	if got := b.Delay(0); got != 0 {
		t.Fatalf("Delay(0) = %s, want 0", got)
	}
	if got := b.Delay(-3); got != 0 {
		t.Fatalf("Delay(-3) = %s, want 0", got)
	}
}

func TestBackoffZeroValueUsesDefaults(t *testing.T) {
	var b Backoff
	if got, want := b.Delay(1), 250*time.Millisecond; got != want {
		t.Fatalf("zero-value Delay(1) = %s, want %s", got, want)
	}
	if got, want := b.Delay(10), 5*time.Second; got != want {
		t.Fatalf("zero-value Delay(10) = %s, want %s", got, want)
	}
}

func TestBackoffCustomSchedule(t *testing.T) {
	b := Backoff{Base: 10 * time.Millisecond, Max: 40 * time.Millisecond, Factor: 3}
	cases := map[int]time.Duration{
		1: 10 * time.Millisecond,
		2: 30 * time.Millisecond,
		3: 40 * time.Millisecond, // 90ms would exceed Max
		4: 40 * time.Millisecond,
	}
	for attempt, want := range cases {
		if got := b.Delay(attempt); got != want {
			t.Fatalf("Delay(%d) = %s, want %s", attempt, got, want)
		}
	}
}

func TestBackoffMaxBelowBaseClampsToBase(t *testing.T) {
	b := Backoff{Base: 500 * time.Millisecond, Max: 100 * time.Millisecond, Factor: 2}
	if got, want := b.Delay(1), 500*time.Millisecond; got != want {
		t.Fatalf("Delay(1) = %s, want %s", got, want)
	}
}
