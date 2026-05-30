package videosync

// parity_test.go — Go↔JS divergence guard.
//
// Loads testdata/parity_*.json fixtures, replays their event streams through a
// fresh New(DefaultConfig()) Engine, and asserts the collected Decisions match
// the stored "expected" array.
//
// Run with -update (via -args) to regenerate the expected arrays in place:
//
//	go test ./client/videosync/ -run Parity -args -update
//
// Numeric tolerance for float fields: 1e-3 (rate/seekTo/actualRate).
// Exact comparison: kind, phase, ready, stalled, resetRate.
//
// This test file MAY import encoding/json and os — it is a _test.go file and is
// NOT compiled into the TinyGo runtime.

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateParity = flag.Bool("update", false, "regenerate parity fixture expected arrays")

// parityFixture is the JSON schema for testdata/parity_*.json.
type parityFixture struct {
	Events   []parityEvent    `json:"events"`
	Expected []parityDecision `json:"expected"`
}

// parityEvent is one entry in the "events" array.
// Only the fields relevant to the event "t" (type) are populated.
type parityEvent struct {
	T string `json:"t"` // "ingest" | "rtt" | "tick"

	// ingest fields
	ServerTimeMs uint64  `json:"serverTimeMs,omitempty"`
	Position     float32 `json:"position,omitempty"`
	Playing      bool    `json:"playing,omitempty"`
	RecvPerfMs   float64 `json:"recvPerfMs,omitempty"`

	// rtt fields
	RttMs float64 `json:"rttMs,omitempty"`

	// tick fields
	CurrentTime   float64 `json:"currentTime,omitempty"`
	PerfNowMs     float64 `json:"perfNowMs,omitempty"`
	BufferedAhead float64 `json:"bufferedAhead,omitempty"`
	Paused        bool    `json:"paused,omitempty"`
}

// parityDecision is the serialisable form of a Decision (one per tick event).
type parityDecision struct {
	Kind         uint8   `json:"kind"`
	Rate         float64 `json:"rate"`
	SeekTo       float64 `json:"seekTo"`
	ResetRate    bool    `json:"resetRate"`
	Ready        bool    `json:"ready"`
	Stalled      bool    `json:"stalled"`
	ActualRate   float64 `json:"actualRate"`
	PreloadPhase uint8   `json:"preloadPhase"`
	Reason       string  `json:"reason"`
}

func decisionToParityDecision(d Decision) parityDecision {
	return parityDecision{
		Kind:         uint8(d.Kind),
		Rate:         d.Rate,
		SeekTo:       d.SeekTo,
		ResetRate:    d.ResetRate,
		Ready:        d.Ready,
		Stalled:      d.Stalled,
		ActualRate:   d.ActualRate,
		PreloadPhase: uint8(d.PreloadPhase),
		Reason:       d.Reason,
	}
}

// replayFixture replays all events through a fresh engine and returns the
// collected Decisions for each "tick" event.
func replayFixture(f *parityFixture) []parityDecision {
	e := New(DefaultConfig())
	var results []parityDecision
	for _, ev := range f.Events {
		switch ev.T {
		case "ingest":
			e.Ingest(ev.ServerTimeMs, ev.Position, ev.Playing, ev.RecvPerfMs)
		case "rtt":
			e.RTT(ev.RttMs)
		case "tick":
			d := e.Tick(ev.CurrentTime, ev.PerfNowMs, ev.BufferedAhead, ev.Paused)
			results = append(results, decisionToParityDecision(d))
		case "start":
			e.OnPlaybackStart(ev.PerfNowMs)
		}
	}
	return results
}

const parityEps = 1e-3

func approxEqual(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Abs(a-b) <= parityEps
}

func assertDecisionsEqual(t *testing.T, fixture, tick int, got, want parityDecision) {
	t.Helper()
	if got.Kind != want.Kind {
		t.Errorf("[fixture %d tick %d] Kind: got %d, want %d", fixture, tick, got.Kind, want.Kind)
	}
	if !approxEqual(got.Rate, want.Rate) {
		t.Errorf("[fixture %d tick %d] Rate: got %v, want %v", fixture, tick, got.Rate, want.Rate)
	}
	if !approxEqual(got.SeekTo, want.SeekTo) {
		t.Errorf("[fixture %d tick %d] SeekTo: got %v, want %v", fixture, tick, got.SeekTo, want.SeekTo)
	}
	if got.ResetRate != want.ResetRate {
		t.Errorf("[fixture %d tick %d] ResetRate: got %v, want %v", fixture, tick, got.ResetRate, want.ResetRate)
	}
	if got.Ready != want.Ready {
		t.Errorf("[fixture %d tick %d] Ready: got %v, want %v", fixture, tick, got.Ready, want.Ready)
	}
	if got.Stalled != want.Stalled {
		t.Errorf("[fixture %d tick %d] Stalled: got %v, want %v", fixture, tick, got.Stalled, want.Stalled)
	}
	if !approxEqual(got.ActualRate, want.ActualRate) {
		t.Errorf("[fixture %d tick %d] ActualRate: got %v, want %v", fixture, tick, got.ActualRate, want.ActualRate)
	}
	if got.PreloadPhase != want.PreloadPhase {
		t.Errorf("[fixture %d tick %d] PreloadPhase: got %d, want %d", fixture, tick, got.PreloadPhase, want.PreloadPhase)
	}
	if got.Reason != want.Reason {
		t.Errorf("[fixture %d tick %d] Reason: got %q, want %q", fixture, tick, got.Reason, want.Reason)
	}
}

// TestParityFixtures loads all testdata/parity_*.json files, replays their
// event streams, and asserts the output matches the stored expected array.
// With -update it regenerates the expected arrays in place.
func TestParityFixtures(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("testdata", "parity_*.json"))
	if err != nil {
		t.Fatalf("glob testdata/parity_*.json: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no parity_*.json fixtures found in testdata/")
	}

	for fi, path := range matches {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var fix parityFixture
			if err := json.Unmarshal(raw, &fix); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}

			got := replayFixture(&fix)

			if *updateParity {
				fix.Expected = got
				out, err := json.MarshalIndent(fix, "", "  ")
				if err != nil {
					t.Fatalf("marshal %s: %v", path, err)
				}
				if err := os.WriteFile(path, out, 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				t.Logf("updated %s (%d ticks)", path, len(got))
				return
			}

			if len(got) != len(fix.Expected) {
				t.Fatalf("tick count mismatch: got %d decisions, want %d", len(got), len(fix.Expected))
			}
			for i, want := range fix.Expected {
				assertDecisionsEqual(t, fi, i, got[i], want)
			}
		})
	}
}
