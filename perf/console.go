package perf

import (
	"context"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// ConsoleEntry is a single captured console API call or uncaught exception.
// Level is the API type ("log", "warn", "error", "exception") and Text is
// the concatenated message text, joined from the console call arguments.
type ConsoleEntry struct {
	Level string  `json:"level"`
	Text  string  `json:"text"`
	URL   string  `json:"url,omitempty"`
	Line  int64   `json:"line,omitempty"`
	Col   int64   `json:"col,omitempty"`
	TimeS float64 `json:"timeSeconds,omitempty"` // seconds since capture start
}

// ConsoleCapture collects Runtime.consoleAPICalled and Runtime.exceptionThrown
// events for the life of its context. It's installed once per Driver and
// read via Entries().
//
// Only Warning, Error, Assert, and exceptions are kept — info/log/debug are
// noisy and rarely actionable in a perf report. Callers who want the full
// log can use CaptureAllLevels.
type ConsoleCapture struct {
	mu      sync.Mutex
	entries []ConsoleEntry
	all     bool
}

// StartConsoleCapture installs event listeners on the driver's context and
// enables the Runtime domain so events start flowing. The returned capture
// is safe to call Entries() on from another goroutine.
//
// Call this BEFORE Navigate() if you want to capture early-page errors.
func StartConsoleCapture(d *Driver) (*ConsoleCapture, error) {
	return startConsoleCapture(d, false)
}

// StartConsoleCaptureAll is StartConsoleCapture with info/log/debug included.
func StartConsoleCaptureAll(d *Driver) (*ConsoleCapture, error) {
	return startConsoleCapture(d, true)
}

func startConsoleCapture(d *Driver, all bool) (*ConsoleCapture, error) {
	if d == nil {
		return nil, nil
	}
	c := &ConsoleCapture{all: all}

	// Listen on the driver's parent context so the subscription lives as
	// long as the driver does.
	chromedp.ListenTarget(d.ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			level := string(e.Type)
			if !c.all && !isNotableLevel(level) {
				return
			}
			c.mu.Lock()
			c.entries = append(c.entries, ConsoleEntry{
				Level: level,
				Text:  joinConsoleArgs(e.Args),
			})
			c.mu.Unlock()
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails == nil {
				return
			}
			d := e.ExceptionDetails
			text := d.Text
			if d.Exception != nil && d.Exception.Description != "" {
				// Description is the full "Error: message\n  at ..." stack,
				// which is usually more informative than the Text preamble.
				text = d.Exception.Description
			}
			c.mu.Lock()
			c.entries = append(c.entries, ConsoleEntry{
				Level: "exception",
				Text:  text,
				URL:   d.URL,
				Line:  int64(d.LineNumber),
				Col:   int64(d.ColumnNumber),
			})
			c.mu.Unlock()
		}
	})

	// Enable the Runtime domain so the events actually fire.
	if err := chromedp.Run(d.ctx, runtime.Enable()); err != nil {
		return nil, err
	}
	return c, nil
}

// Entries returns a snapshot copy of the captured entries.
func (c *ConsoleCapture) Entries() []ConsoleEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ConsoleEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// Clear drops all captured entries. Useful between interactions.
func (c *ConsoleCapture) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = nil
	c.mu.Unlock()
}

// Disable turns off Runtime event delivery. The capture keeps whatever it
// already collected.
func (c *ConsoleCapture) Disable(ctx context.Context) error {
	return chromedp.Run(ctx, runtime.Disable())
}

func isNotableLevel(level string) bool {
	switch level {
	case "warning", "error", "assert":
		return true
	}
	return false
}

func joinConsoleArgs(args []*runtime.RemoteObject) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for _, a := range args {
		if a == nil {
			continue
		}
		if a.Description != "" {
			parts = append(parts, a.Description)
			continue
		}
		if len(a.Value) > 0 {
			// Value is a raw JSON fragment; strip wrapping quotes for
			// string primitives so messages read naturally.
			v := string(a.Value)
			v = strings.TrimPrefix(v, `"`)
			v = strings.TrimSuffix(v, `"`)
			parts = append(parts, v)
			continue
		}
		if a.UnserializableValue != "" {
			parts = append(parts, string(a.UnserializableValue))
		}
	}
	return strings.Join(parts, " ")
}
