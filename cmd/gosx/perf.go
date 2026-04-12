package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/gosx/perf"
)

func cmdPerf() {
	fs := flag.NewFlagSet("perf", flag.ExitOnError)
	frames := fs.Int("frames", 120, "scene3D frames to sample")
	clickSel := fs.String("click", "", "CSS selector to click")
	scrollPx := fs.String("scroll", "", "pixels to scroll")
	typeSel := fs.String("type", "", "selector:text to type")
	jsonOut := fs.Bool("json", false, "output as JSON")
	timeout := fs.Duration("timeout", 30*time.Second, "max wait duration")
	headless := fs.Bool("headless", true, "run Chrome in headless mode")
	record := fs.String("record", "", "record video to file path")
	trace := fs.String("trace", "", "capture Chrome DevTools trace to file (load in chrome://tracing or DevTools Performance panel)")
	waterfall := fs.Bool("waterfall", false, "show network resource waterfall")
	throttle := fs.Float64("throttle", 1, "CPU throttle rate (1=realtime, 4=mid-range phone, 6=low-end)")
	mobile := fs.String("mobile", "", "mobile device emulation: pixel7 | iphone14")
	coverage := fs.Bool("coverage", false, "capture JS block-level coverage per script")
	var asserts stringSlice
	fs.Var(&asserts, "assert", "assertion expression (repeatable)")
	fs.Parse(os.Args[2:])

	urls := fs.Args()
	if len(urls) == 0 {
		fatal("usage: gosx perf <url> [url...] [flags]")
	}

	scenario := &perf.Scenario{
		URLs:        urls,
		Frames:      *frames,
		Timeout:     *timeout,
		Headless:    *headless,
		RecordPath:  *record,
		TracePath:   *trace,
		CPUThrottle: *throttle,
		MobileName:  *mobile,
		Coverage:    *coverage,
	}

	if *clickSel != "" {
		scenario.Interactions = append(scenario.Interactions, perf.Interaction{
			Kind: "click", Selector: *clickSel,
		})
	}
	if *scrollPx != "" {
		scenario.Interactions = append(scenario.Interactions, perf.Interaction{
			Kind: "scroll", Value: *scrollPx,
		})
	}
	if *typeSel != "" {
		parts := strings.SplitN(*typeSel, ":", 2)
		if len(parts) == 2 {
			scenario.Interactions = append(scenario.Interactions, perf.Interaction{
				Kind: "type", Selector: parts[0], Value: parts[1],
			})
		}
	}

	report, err := perf.RunScenario(scenario)
	if err != nil {
		fatal("gosx perf: %v", err)
	}

	if *jsonOut {
		data, err := perf.FormatJSON(report)
		if err != nil {
			fatal("gosx perf: json: %v", err)
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(perf.FormatTable(report))
		if *waterfall {
			fmt.Print(perf.FormatWaterfallTable(report))
		}
		if *trace != "" {
			if data, err := os.ReadFile(*trace); err == nil {
				if hot, err := perf.SummarizeTrace(data, 10, 5); err == nil {
					fmt.Print(perf.FormatTraceSummary(hot, *trace))
				}
			}
		}
	}

	// Evaluate assertions
	if len(asserts) > 0 {
		var parsed []perf.Assertion
		for _, expr := range asserts {
			a, err := perf.ParseAssertion(expr)
			if err != nil {
				fatal("gosx perf: bad assertion %q: %v", expr, err)
			}
			parsed = append(parsed, a)
		}
		results := perf.EvalAssertions(parsed, report)
		allPassed := true
		for _, r := range results {
			if !r.Passed {
				fmt.Fprintf(os.Stderr, "  \u2717 %s %s %.2f (actual: %.2f)\n", r.Metric, r.Op, r.Value, r.Actual)
				allPassed = false
			} else {
				fmt.Fprintf(os.Stderr, "  \u2713 %s %s %.2f\n", r.Metric, r.Op, r.Value)
			}
		}
		if !allPassed {
			os.Exit(1)
		}
	}
}

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string    { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }
