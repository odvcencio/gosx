package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/gosx/perf"
)

func cmdPerf() {
	// Subcommand dispatch: `gosx perf compare a.json b.json`. Detected
	// before the flag parser so the compare subcommand doesn't conflict
	// with perf's main flag set.
	if len(os.Args) > 2 && os.Args[2] == "compare" {
		cmdPerfCompare()
		return
	}
	if len(os.Args) > 2 && os.Args[2] == "budget" {
		cmdPerfBudget()
		return
	}

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
	heapSnap := fs.String("heap-snapshot", "", "write a .heapsnapshot file (load in DevTools Memory panel)")
	var asserts stringSlice
	fs.Var(&asserts, "assert", "assertion expression (repeatable)")
	budgetPath := fs.String("budget", "", "perf budget JSON to evaluate after profiling")
	budgetProfile := fs.String("budget-profile", "", "budget profile to apply to every page")
	fs.Parse(interspersedPerfArgs(os.Args[2:]))

	urls := fs.Args()
	if len(urls) == 0 {
		fatal("usage: gosx perf <url> [url...] [flags]")
	}

	scenario := &perf.Scenario{
		URLs:             urls,
		Frames:           *frames,
		Timeout:          *timeout,
		Headless:         *headless,
		RecordPath:       *record,
		TracePath:        *trace,
		CPUThrottle:      *throttle,
		MobileName:       *mobile,
		Coverage:         *coverage,
		HeapSnapshotPath: *heapSnap,
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

	if strings.TrimSpace(*budgetPath) != "" {
		budget, err := perf.LoadBudgetFile(*budgetPath)
		if err != nil {
			fatal("gosx perf: load budget: %v", err)
		}
		result, err := perf.EvaluateBudget(report, budget, *budgetProfile)
		if err != nil {
			fatal("gosx perf: budget: %v", err)
		}
		fmt.Fprint(os.Stderr, perf.FormatBudgetResult(result))
		if !result.Passed {
			os.Exit(1)
		}
	}
}

// cmdPerfCompare implements `gosx perf compare baseline.json candidate.json`.
// Reads two gosx perf --json reports, diffs the key metrics, and prints a
// side-by-side table. Exits nonzero when any metric regresses beyond the
// --threshold (default 5%).
func cmdPerfCompare() {
	fs := flag.NewFlagSet("perf compare", flag.ExitOnError)
	threshold := fs.Float64("threshold", 5, "regression threshold percent (exit 1 if any metric moves worse by more than this)")
	jsonOut := fs.Bool("json", false, "output as JSON")
	// os.Args[2] is "compare"; os.Args[3..] are baseline, candidate, and flags.
	fs.Parse(os.Args[3:])

	if fs.NArg() != 2 {
		fatal("usage: gosx perf compare <baseline.json> <candidate.json> [--threshold N] [--json]")
	}

	baseline, err := perf.LoadReport(fs.Arg(0))
	if err != nil {
		fatal("gosx perf compare: load baseline: %v", err)
	}
	candidate, err := perf.LoadReport(fs.Arg(1))
	if err != nil {
		fatal("gosx perf compare: load candidate: %v", err)
	}

	cmp := perf.CompareReports(baseline, candidate)

	if *jsonOut {
		enc, err := json.MarshalIndent(cmp, "", "  ")
		if err != nil {
			fatal("gosx perf compare: json: %v", err)
		}
		fmt.Println(string(enc))
	} else {
		fmt.Print(perf.FormatComparison(cmp, *threshold))
	}

	if perf.AnyRegression(cmp, *threshold) {
		os.Exit(1)
	}
}

// cmdPerfBudget implements `gosx perf budget report.json budget.json`.
// It evaluates a saved perf report against named budget profiles and exits
// nonzero when any assertion fails.
func cmdPerfBudget() {
	fs := flag.NewFlagSet("perf budget", flag.ExitOnError)
	profile := fs.String("profile", "", "budget profile to apply to every page, ignoring route mappings")
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Parse(os.Args[3:])

	if fs.NArg() != 2 {
		fatal("usage: gosx perf budget <report.json> <budget.json> [--profile name] [--json]")
	}

	report, err := perf.LoadReport(fs.Arg(0))
	if err != nil {
		fatal("gosx perf budget: load report: %v", err)
	}
	budget, err := perf.LoadBudgetFile(fs.Arg(1))
	if err != nil {
		fatal("gosx perf budget: load budget: %v", err)
	}

	result, err := perf.EvaluateBudget(report, budget, *profile)
	if err != nil {
		fatal("gosx perf budget: %v", err)
	}

	if *jsonOut {
		enc, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fatal("gosx perf budget: json: %v", err)
		}
		fmt.Println(string(enc))
	} else {
		fmt.Print(perf.FormatBudgetResult(result))
	}

	if !result.Passed {
		os.Exit(1)
	}
}

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

func interspersedPerfArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	valueFlags := map[string]bool{
		"assert":         true,
		"budget":         true,
		"budget-profile": true,
		"click":          true,
		"frames":         true,
		"heap-snapshot":  true,
		"mobile":         true,
		"record":         true,
		"scroll":         true,
		"throttle":       true,
		"timeout":        true,
		"trace":          true,
		"type":           true,
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	out := make([]string, 0, len(args))
	out = append(out, flags...)
	out = append(out, positionals...)
	return out
}
