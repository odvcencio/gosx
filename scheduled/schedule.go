// Package scheduled provides background-work primitives for the GoSX framework.
package scheduled

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Schedule determines when a task should fire.
type Schedule interface {
	// NextDue returns the next time the schedule should fire after `after`.
	// ok=false means "never again" (a one-shot that already fired).
	NextDue(after time.Time) (due time.Time, ok bool)
}

// intervalSchedule fires every d after the reference time.
type intervalSchedule struct {
	d time.Duration
}

// NextDue returns (after + d, true) — an interval never stops.
func (s intervalSchedule) NextDue(after time.Time) (time.Time, bool) {
	return after.Add(s.d), true
}

// Interval returns a Schedule that fires every d after each call site's `after`.
func Interval(d time.Duration) Schedule {
	return intervalSchedule{d: d}
}

// onceSchedule is a programmatic one-shot: the first NextDue returns (after, true)
// (i.e. due immediately); subsequent calls return (zero, false).
//
// fired is an atomic so the type is safe even if a future caller shares the same
// *onceSchedule across goroutines. The scheduler keeps the schedule loop as the
// sole NextDue caller, but this guards against accidental concurrent use.
type onceSchedule struct {
	fired atomic.Bool
}

// NextDue returns (after, true) on the first call, then (zero, false) forever.
// CompareAndSwap makes the one-shot transition atomic, so exactly one caller can
// observe the (after, true) result even under concurrent invocation.
func (s *onceSchedule) NextDue(after time.Time) (time.Time, bool) {
	if !s.fired.CompareAndSwap(false, true) {
		return time.Time{}, false
	}
	return after, true
}

// Once returns a one-shot Schedule that fires exactly once (due immediately
// relative to the `after` value passed in).
func Once() Schedule {
	return &onceSchedule{}
}

// ParseEvery parses strings of the form "every <duration>" (case-sensitive,
// lowercase "every") and returns an Interval schedule.
// Examples: "every 30m", "every 5s", "every 1h30m".
// Returns an error for any other input.
func ParseEvery(s string) (Schedule, error) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "every ") {
		return nil, fmt.Errorf("scheduled: ParseEvery: expected format \"every <duration>\", got %q", s)
	}
	durStr := strings.TrimSpace(trimmed[len("every "):])
	if durStr == "" {
		return nil, fmt.Errorf("scheduled: ParseEvery: missing duration in %q", s)
	}
	d, err := time.ParseDuration(durStr)
	if err != nil {
		return nil, fmt.Errorf("scheduled: ParseEvery: invalid duration %q: %w", durStr, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("scheduled: ParseEvery: duration must be positive, got %v", d)
	}
	return Interval(d), nil
}
