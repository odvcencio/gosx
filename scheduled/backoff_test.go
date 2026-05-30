package scheduled

import (
	"testing"
	"time"
)

func TestExponential_Delays(t *testing.T) {
	p := Exponential(time.Second, 4, time.Minute)

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 4 * time.Second},
		{3, 16 * time.Second},
		{4, 60 * time.Second}, // 64s capped at 60s (max=1min)
	}
	for _, c := range cases {
		got := p.NextDelay(c.attempt)
		if got != c.want {
			t.Errorf("Exponential attempt %d: got %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestExponential_SpecValues(t *testing.T) {
	// Spec says Exponential(1s,4,1m) => 1s/4s/16s
	p := Exponential(time.Second, 4, time.Minute)
	if d := p.NextDelay(1); d != time.Second {
		t.Errorf("attempt 1: got %v, want 1s", d)
	}
	if d := p.NextDelay(2); d != 4*time.Second {
		t.Errorf("attempt 2: got %v, want 4s", d)
	}
	if d := p.NextDelay(3); d != 16*time.Second {
		t.Errorf("attempt 3: got %v, want 16s", d)
	}
}

func TestExponential_CapHonored(t *testing.T) {
	p := Exponential(time.Second, 4, 10*time.Second)
	// attempt 3 would be 16s, capped at 10s
	got := p.NextDelay(3)
	if got != 10*time.Second {
		t.Errorf("cap not honored: got %v, want 10s", got)
	}
}

func TestExponential_NoCapWhenZero(t *testing.T) {
	p := Exponential(time.Second, 2, 0) // no cap
	// attempt 10: 2^9 = 512s
	got := p.NextDelay(10)
	want := 512 * time.Second
	if got != want {
		t.Errorf("no-cap: got %v, want %v", got, want)
	}
}

func TestFixed(t *testing.T) {
	p := Fixed(5 * time.Second)
	for _, attempt := range []int{1, 2, 10, 100} {
		got := p.NextDelay(attempt)
		if got != 5*time.Second {
			t.Errorf("Fixed attempt %d: got %v, want 5s", attempt, got)
		}
	}
}

func TestImmediate(t *testing.T) {
	p := Immediate()
	for _, attempt := range []int{1, 2, 10} {
		got := p.NextDelay(attempt)
		if got != 0 {
			t.Errorf("Immediate attempt %d: got %v, want 0", attempt, got)
		}
	}
}

func TestDefaultBackoff(t *testing.T) {
	p := DefaultBackoff()
	// 1s, 4s, 16s, then capped at 64s
	expected := []time.Duration{time.Second, 4 * time.Second, 16 * time.Second, 64 * time.Second, 64 * time.Second}
	for i, want := range expected {
		got := p.NextDelay(i + 1)
		if got != want {
			t.Errorf("DefaultBackoff attempt %d: got %v, want %v", i+1, got, want)
		}
	}
}

func TestShouldRetry(t *testing.T) {
	// Boundary: attempt == maxAttempts -> false (no more retries)
	if ShouldRetry(3, 3) {
		t.Error("ShouldRetry(3,3) should be false")
	}
	// Below limit -> true
	if !ShouldRetry(2, 3) {
		t.Error("ShouldRetry(2,3) should be true")
	}
	// Infinite retries
	if !ShouldRetry(99, 0) {
		t.Error("ShouldRetry(99,0) should be true (infinite)")
	}
	if !ShouldRetry(99, -1) {
		t.Error("ShouldRetry(99,-1) should be true (infinite)")
	}
	// Over limit
	if ShouldRetry(10, 5) {
		t.Error("ShouldRetry(10,5) should be false")
	}
}
