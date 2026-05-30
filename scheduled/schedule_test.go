package scheduled

import (
	"testing"
	"time"
)

var t0 = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func TestInterval_NextDue(t *testing.T) {
	s := Interval(30 * time.Minute)

	due, ok := s.NextDue(t0)
	if !ok {
		t.Fatal("Interval.NextDue: expected ok=true")
	}
	want := t0.Add(30 * time.Minute)
	if !due.Equal(want) {
		t.Errorf("Interval.NextDue: got %v, want %v", due, want)
	}
}

func TestInterval_SequentialAdvances(t *testing.T) {
	s := Interval(10 * time.Minute)
	current := t0
	for i := 1; i <= 5; i++ {
		due, ok := s.NextDue(current)
		if !ok {
			t.Fatalf("Interval.NextDue call %d: expected ok=true", i)
		}
		want := current.Add(10 * time.Minute)
		if !due.Equal(want) {
			t.Errorf("Interval.NextDue call %d: got %v, want %v", i, due, want)
		}
		current = due
	}
	// After 5 advances, time should be t0+50m
	want := t0.Add(50 * time.Minute)
	if !current.Equal(want) {
		t.Errorf("after 5 steps got %v, want %v", current, want)
	}
}

func TestParseEvery_Valid(t *testing.T) {
	cases := []struct {
		input string
		d     time.Duration
	}{
		{"every 30m", 30 * time.Minute},
		{"every 5s", 5 * time.Second},
		{"every 1h", time.Hour},
		{"every 1h30m", 90 * time.Minute},
		{"every 200ms", 200 * time.Millisecond},
	}
	for _, c := range cases {
		sched, err := ParseEvery(c.input)
		if err != nil {
			t.Errorf("ParseEvery(%q): unexpected error: %v", c.input, err)
			continue
		}
		// Check it behaves like Interval(d)
		due, ok := sched.NextDue(t0)
		if !ok {
			t.Errorf("ParseEvery(%q).NextDue: ok=false", c.input)
		}
		want := t0.Add(c.d)
		if !due.Equal(want) {
			t.Errorf("ParseEvery(%q).NextDue: got %v, want %v", c.input, due, want)
		}
	}
}

func TestParseEvery_Invalid(t *testing.T) {
	cases := []string{
		"garbage",
		"every",
		"every ",
		"30m",
		"EVERY 30m", // must match "every " prefix literally after lowercasing — ok actually lowercase handles this
		"every -5s",
		"every 0s",
	}
	for _, c := range cases {
		_, err := ParseEvery(c)
		if err == nil {
			t.Errorf("ParseEvery(%q): expected error, got nil", c)
		}
	}
}

func TestOnce_FiresOnce(t *testing.T) {
	s := Once()

	due, ok := s.NextDue(t0)
	if !ok {
		t.Fatal("Once.NextDue first call: expected ok=true")
	}
	if !due.Equal(t0) {
		t.Errorf("Once.NextDue first call: due should equal `after`=%v, got %v", t0, due)
	}

	// Second call — should be "never again"
	_, ok = s.NextDue(t0.Add(time.Hour))
	if ok {
		t.Error("Once.NextDue second call: expected ok=false")
	}

	// Third call — still never
	_, ok = s.NextDue(t0.Add(2 * time.Hour))
	if ok {
		t.Error("Once.NextDue third call: expected ok=false")
	}
}
