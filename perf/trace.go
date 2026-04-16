package perf

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/io"
	"github.com/chromedp/cdproto/tracing"
	"github.com/chromedp/chromedp"
)

// CaptureTrace records a Chrome DevTools trace for the duration of the
// callback. The returned bytes are a Chrome-format JSON trace (same format
// as chrome://tracing / Perfetto / Chrome DevTools' Performance panel import).
//
// The default category set matches what Chrome DevTools' Performance panel
// records: JavaScript execution, layout/paint/raster, V8 compile, and
// user_timing marks (which is where gosx:ready / scene3d-render / island
// hydrate measures from instrument.js show up).
//
// Typical use:
//
//	trace, err := perf.CaptureTrace(d, func() error {
//	    return d.Navigate(url)
//	})
//	os.WriteFile("trace.json", trace, 0o644)
//
// Users then drop trace.json into chrome://tracing, https://ui.perfetto.dev,
// or Chrome DevTools' Performance panel (Load profile…).
func CaptureTrace(d *Driver, during func() error) ([]byte, error) {
	return CaptureTraceWithCategories(d, defaultTraceCategories(), during)
}

// CaptureTraceWithCategories is CaptureTrace with a custom category set.
func CaptureTraceWithCategories(d *Driver, categories []string, during func() error) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("perf: nil driver")
	}

	// Buffer collected events and the stream handle emitted when tracing
	// completes. Listener callbacks fire on a background goroutine inside
	// chromedp, so synchronize with a mutex and signal completion on a
	// channel that CaptureTrace can select on.
	var (
		mu       sync.Mutex
		events   []string
		streamH  io.StreamHandle
		doneCh   = make(chan struct{})
		doneOnce sync.Once
	)

	listenCtx, cancelListen := context.WithCancel(d.ctx)
	defer cancelListen()

	chromedp.ListenTarget(listenCtx, func(ev interface{}) {
		switch e := ev.(type) {
		case *tracing.EventDataCollected:
			// Each Value is a single raw trace event (as a JSON object).
			// Some Chrome builds deliver events this way; others deliver
			// everything via a stream handle. Handle both — stream wins
			// when present.
			mu.Lock()
			for _, v := range e.Value {
				events = append(events, string(v))
			}
			mu.Unlock()
		case *tracing.EventTracingComplete:
			mu.Lock()
			streamH = e.Stream
			mu.Unlock()
			doneOnce.Do(func() { close(doneCh) })
		}
	})

	// Start tracing before the callback runs.
	startParams := tracing.Start().
		WithTransferMode(tracing.TransferModeReturnAsStream).
		WithStreamFormat(tracing.StreamFormatJSON).
		WithTraceConfig(&tracing.TraceConfig{
			RecordMode:         tracing.RecordModeRecordUntilFull,
			IncludedCategories: categories,
		})
	if err := chromedp.Run(d.ctx, startParams); err != nil {
		return nil, fmt.Errorf("tracing.Start: %w", err)
	}

	// Run the user callback while tracing is active.
	cbErr := during()

	// End tracing — Chrome flushes the buffer and fires TracingComplete.
	if err := chromedp.Run(d.ctx, tracing.End()); err != nil {
		return nil, fmt.Errorf("tracing.End: %w", err)
	}

	// Wait for TracingComplete — Chrome sometimes takes a moment to fire
	// it after End() returns. Bail out if the underlying context dies.
	select {
	case <-doneCh:
	case <-d.ctx.Done():
		return nil, fmt.Errorf("tracing: context ended before TracingComplete")
	}

	// Prefer the stream handle path — TransferModeReturnAsStream is the
	// modern default and guarantees a well-formed JSON document.
	mu.Lock()
	sh := streamH
	inlineEvents := events
	mu.Unlock()

	var data []byte
	if sh != "" {
		var err error
		data, err = readTraceStream(d.ctx, sh)
		if err != nil {
			return nil, err
		}
	} else {
		// Fallback: assemble inline event fragments into a single array.
		// Each fragment is already a JSON array — concatenate with commas
		// and wrap. This branch is rarely hit with modern Chrome.
		if len(inlineEvents) == 0 {
			return nil, fmt.Errorf("tracing: no trace data received")
		}
		var b strings.Builder
		b.WriteString(`{"traceEvents":[`)
		first := true
		for _, frag := range inlineEvents {
			frag = strings.TrimSpace(frag)
			if frag == "" {
				continue
			}
			if !first {
				b.WriteString(",")
			}
			b.WriteString(frag)
			first = false
		}
		b.WriteString("]}")
		data = []byte(b.String())
	}

	return data, cbErr
}

// readTraceStream pulls all chunks from a CDP stream handle and returns
// the assembled bytes. Chrome delivers trace streams in base64-encoded
// chunks of a few KB each; we loop until EOF.
func readTraceStream(ctx context.Context, handle io.StreamHandle) ([]byte, error) {
	var out []byte
	for {
		data, eof, err := io.Read(handle).WithSize(1 << 20).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target))
		if err != nil {
			return nil, fmt.Errorf("io.Read: %w", err)
		}
		// Chrome may return base64 even when not requested if the chunk
		// contains non-UTF8 bytes. Detect via the ReadReturns.Base64encoded
		// flag — but chromedp's helper discards that, so we heuristically
		// try to decode as base64 only when the raw text isn't valid JSON.
		out = append(out, []byte(data)...)
		if eof {
			break
		}
	}
	// Close the stream to release browser-side resources.
	_ = io.Close(handle).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target))

	// If the assembled bytes look base64-encoded (no leading {), try to
	// decode. Chrome with JSON stream format usually returns plain text,
	// but base64 is possible with binary transfer modes.
	if len(out) > 0 && out[0] != '{' && out[0] != '[' {
		if dec, err := base64.StdEncoding.DecodeString(string(out)); err == nil {
			return dec, nil
		}
	}
	return out, nil
}

// TraceHotEvent is a single notable event extracted from a captured trace.
// The text form is what we print in the FormatTable summary.
type TraceHotEvent struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Duration float64 `json:"durationMs"`
	URL      string  `json:"url,omitempty"`
	Function string  `json:"function,omitempty"`
}

// SummarizeTrace parses a captured Chrome trace and returns the top-N longest
// events matching an interesting-subset filter (JS compile/parse, script eval,
// event dispatch, microtasks, animation frame, layout/paint). Anything under
// minMs is skipped — we care about things that show up on the main thread,
// not noise.
func SummarizeTrace(trace []byte, topN int, minMs float64) ([]TraceHotEvent, error) {
	var doc struct {
		TraceEvents []struct {
			Name     string          `json:"name"`
			Category string          `json:"cat"`
			Dur      float64         `json:"dur"`
			Args     json.RawMessage `json:"args"`
		} `json:"traceEvents"`
	}
	if err := json.Unmarshal(trace, &doc); err != nil {
		return nil, fmt.Errorf("parse trace: %w", err)
	}

	// Only keep events whose name signals main-thread JS work or layout
	// cost — anything users can act on. The toplevel/RunTask shells are
	// intentionally excluded because they just wrap the real work and
	// double-count it in the summary.
	interesting := map[string]bool{
		"EvaluateScript":          true,
		"v8.compile":              true,
		"v8.parseOnBackground":    true,
		"CompileScript":           true,
		"FunctionCall":            true,
		"EventDispatch":           true,
		"RunMicrotasks":           true,
		"v8.runMicrotasks":        true,
		"FireAnimationFrame":      true,
		"Layout":                  true,
		"UpdateLayoutTree":        true,
		"Paint":                   true,
		"ParseHTML":               true,
		"WebAssembly.Compile":     true,
		"WebAssembly.Instantiate": true,
	}

	minUs := minMs * 1000
	type hot struct {
		name     string
		cat      string
		durUs    float64
		url      string
		function string
	}
	var hits []hot
	for _, e := range doc.TraceEvents {
		if !interesting[e.Name] || e.Dur < minUs {
			continue
		}
		h := hot{name: e.Name, cat: e.Category, durUs: e.Dur}
		if len(e.Args) > 0 {
			var args struct {
				Data struct {
					URL          string `json:"url"`
					FunctionName string `json:"functionName"`
				} `json:"data"`
			}
			if err := json.Unmarshal(e.Args, &args); err == nil {
				h.url = args.Data.URL
				h.function = args.Data.FunctionName
			}
		}
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].durUs > hits[j].durUs })

	if topN > 0 && len(hits) > topN {
		hits = hits[:topN]
	}
	out := make([]TraceHotEvent, len(hits))
	for i, h := range hits {
		out[i] = TraceHotEvent{
			Name:     h.name,
			Category: h.cat,
			Duration: h.durUs / 1000,
			URL:      h.url,
			Function: h.function,
		}
	}
	return out, nil
}

// FormatTraceSummary renders a SummarizeTrace result as a table fragment
// suitable for appending to FormatTable output.
func FormatTraceSummary(events []TraceHotEvent, tracePath string) string {
	var b strings.Builder
	b.WriteString("\n  Trace Summary\n")
	if tracePath != "" {
		b.WriteString(fmt.Sprintf("    %-24s%s\n", "Saved to", tracePath))
	}
	if len(events) == 0 {
		b.WriteString("    (no notable main-thread events)\n")
		return b.String()
	}
	b.WriteString("    Top main-thread events\n")
	for _, e := range events {
		extra := e.URL
		if extra == "" {
			extra = e.Function
		}
		if len(extra) > 48 {
			extra = "…" + extra[len(extra)-47:]
		}
		b.WriteString(fmt.Sprintf("      %-24s%7.1fms  %s\n", e.Name, e.Duration, extra))
	}
	return b.String()
}

// defaultTraceCategories returns the category set that matches what Chrome
// DevTools' Performance panel records by default. This is enough to get a
// useful flame chart showing JS call stacks, V8 compile, layout/paint, and
// user_timing marks.
func defaultTraceCategories() []string {
	return []string{
		"devtools.timeline",
		"v8.execute",
		"disabled-by-default-devtools.timeline",
		"disabled-by-default-devtools.timeline.frame",
		"disabled-by-default-devtools.timeline.stack",
		"disabled-by-default-v8.cpu_profiler",
		"disabled-by-default-v8.cpu_profiler.hires",
		"blink.user_timing",
		"latencyInfo",
		"loading",
		"toplevel",
	}
}
