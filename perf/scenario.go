package perf

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// Scenario describes a profiling session.
type Scenario struct {
	URLs         []string
	Frames       int // scene3D frames to sample (default 120)
	Interactions []Interaction
	Timeout      time.Duration
	Headless     bool
	RecordPath   string // video output path (empty = no recording)
	TracePath    string // Chrome DevTools trace output path (empty = no trace)

	// Device emulation — match Chrome DevTools' device toolbar.
	CPUThrottle float64 // 1 = realtime, 4 = mid-range phone, 6 = low-end
	MobileName  string  // "" | "pixel7" | "iphone14"

	// Capture JS coverage (Profiler.startPreciseCoverage + blocks) for
	// the first URL. Expensive enough that it's opt-in.
	Coverage bool

	// Write a .heapsnapshot file for the final page state after all
	// interactions have run. GC runs first so the snapshot reflects
	// live retention, not ephemeral churn.
	HeapSnapshotPath string

	routeRunner scenarioRouteRunner
	diagnostics io.Writer
}

type scenarioRouteRunner func(ctx context.Context, url string, index int, total int) (*PageReport, error)

// Interaction is a user action to execute during profiling.
type Interaction struct {
	Kind     string // "click", "type", "scroll"
	Selector string
	Value    string // text for type, pixels for scroll
}

// RunScenario executes a full profiling session and returns a Report.
func RunScenario(s *Scenario) (*Report, error) {
	opts := []Option{WithHeadless(s.Headless), WithTimeout(0)}

	d, err := New(opts...)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	if err := InjectDriver(d); err != nil {
		return nil, fmt.Errorf("inject: %w", err)
	}

	// Install console + exception capture BEFORE navigation so we see
	// early-page errors. Warnings/errors/asserts/exceptions only — we
	// don't want info/log/debug noise in the perf report.
	console, err := StartConsoleCapture(d)
	if err != nil {
		return nil, fmt.Errorf("console capture: %w", err)
	}

	// Apply device emulation BEFORE navigation so the initial load sees
	// the simulated viewport, user agent, and CPU speed. Chrome DevTools'
	// "Performance panel with Slow 4x CPU" is the direct analogue.
	if s.MobileName != "" {
		profile := ResolveMobileProfile(s.MobileName)
		if profile.Width == 0 {
			return nil, fmt.Errorf("unknown mobile profile %q (want: pixel7, iphone14)", s.MobileName)
		}
		if err := ApplyMobileEmulation(d, profile); err != nil {
			return nil, fmt.Errorf("mobile emulation: %w", err)
		}
	}
	if s.CPUThrottle > 1 {
		if err := ApplyCPUThrottle(d, s.CPUThrottle); err != nil {
			return nil, fmt.Errorf("cpu throttle: %w", err)
		}
	}

	frames := s.Frames
	if frames <= 0 {
		frames = 120
	}

	// Start recording if requested.
	var recorder *Recorder
	if s.RecordPath != "" {
		rec, err := StartRecording(d)
		if err != nil {
			return nil, fmt.Errorf("start recording: %w", err)
		}
		recorder = rec
	}

	report := &Report{
		Timestamp: time.Now(),
	}

	// Navigate to each URL, collect page metrics. A trace capture — if
	// requested — wraps the navigation + ready wait of the FIRST URL only,
	// since the common use case is "what's happening during page load" and
	// capturing across a full multi-page scenario would produce an
	// oversized file. The trace is written after the run completes.
	var traceBytes []byte
	diagnostics := s.diagnostics
	if diagnostics == nil {
		diagnostics = os.Stderr
	}
	routeRunner := s.routeRunner
	if routeRunner == nil {
		routeRunner = func(routeCtx context.Context, url string, i int, total int) (*PageReport, error) {
			routeD, routeCancel := d.WithOperationContext(routeCtx, 0)
			defer routeCancel()

			var routePhase string
			runPhase := func(name string, fn func() error) error {
				routePhase = name
				start := time.Now()
				fmt.Fprintf(diagnostics, "gosx perf: route %d/%d %s phase=%s start timeout=%s\n", i+1, total, url, name, formatPerfTimeout(s.Timeout))
				err := fn()
				elapsed := time.Since(start)
				if err != nil {
					fmt.Fprintf(diagnostics, "gosx perf: route %d/%d %s phase=%s failed elapsed=%s: %v\n", i+1, total, url, name, elapsed.Round(time.Millisecond), err)
					return fmt.Errorf("%s %s: %w", name, url, err)
				}
				fmt.Fprintf(diagnostics, "gosx perf: route %d/%d %s phase=%s done elapsed=%s\n", i+1, total, url, name, elapsed.Round(time.Millisecond))
				return nil
			}

			navigate := func() error {
				if err := runPhase("navigate", func() error { return routeD.Navigate(url) }); err != nil {
					return err
				}
				if err := runPhase("wait-ready", routeD.WaitReady); err != nil {
					return err
				}
				if err := runPhase("settle", func() error {
					select {
					case <-time.After(300 * time.Millisecond):
						return nil
					case <-routeCtx.Done():
						return routeCtx.Err()
					case <-routeD.Context().Done():
						return routeD.Context().Err()
					}
				}); err != nil {
					return err
				}
				return nil
			}

			// If coverage and trace are both requested, coverage wraps the
			// trace capture so a single navigate yields both signals.
			var coverageEntries []CoverageEntry
			runNav := navigate
			if s.TracePath != "" && i == 0 {
				runNav = func() error {
					return runPhase("trace", func() error {
						tb, err := CaptureTrace(routeD, navigate)
						if err != nil {
							return err
						}
						traceBytes = tb
						return nil
					})
				}
			}
			if captureCoverageForRoute(s.Coverage, i) {
				err := runPhase("coverage", func() error {
					entries, err := CaptureCoverage(routeD, runNav)
					if err != nil {
						return err
					}
					coverageEntries = entries
					return nil
				})
				if err != nil {
					return nil, fmt.Errorf("phase %s: %w", routePhase, err)
				}
			} else if err := runNav(); err != nil {
				return nil, fmt.Errorf("phase %s: %w", routePhase, err)
			}

			// Scene3D engines can initialize after the generic GoSX ready mark,
			// especially when large model/runtime chunks are involved. If the
			// route declared a Scene3D surface, wait for the requested render sample
			// before the first collection so missing scene metrics mean "no frames",
			// not "sampled too early".
			scenePresent := false
			if err := runPhase("detect-scene3d", func() error {
				scenePresent = scene3DSurfacePresent(routeD)
				return nil
			}); err != nil {
				return nil, fmt.Errorf("phase %s: %w", routePhase, err)
			}
			if scenePresent {
				if err := runPhase("scene3d-wait", func() error {
					return waitForSceneFrames(routeD, frames)
				}); err != nil {
					return nil, fmt.Errorf("phase %s: %w", routePhase, err)
				}
			}

			var page *PageReport
			if err := runPhase("collect", func() error {
				var err error
				page, err = CollectPageReport(routeD, url)
				return err
			}); err != nil {
				return nil, fmt.Errorf("phase %s: %w", routePhase, err)
			}

			// If scene detected, wait for frames then re-collect scene metrics.
			if page.Scene != nil {
				if err := runPhase("scene3d-rewait", func() error {
					return waitForSceneFrames(routeD, frames)
				}); err != nil {
					return nil, fmt.Errorf("phase %s: %w", routePhase, err)
				}
				var sceneEntries []PerfEntry
				if err := runPhase("scene3d-recollect", func() error {
					var err error
					sceneEntries, err = QuerySceneFrames(routeD)
					return err
				}); err != nil {
					return nil, fmt.Errorf("phase %s: %w", routePhase, err)
				}
				if len(sceneEntries) > 0 {
					durations := make([]float64, len(sceneEntries))
					for i, e := range sceneEntries {
						durations[i] = e.Duration
					}
					page.Scene.FrameStats = ComputeFrameStats(durations)
					page.Scene.FrameCount = len(sceneEntries)
				}
			}

			// Attach console entries captured so far to this page. For
			// multi-page scenarios we clear between pages so each page
			// reports only its own errors.
			page.ConsoleEntries = console.Entries()
			console.Clear()

			// Attach coverage if captured for this (first) page.
			if captureCoverageForRoute(s.Coverage, i) {
				page.CoverageCaptured = true
				page.Coverage = coverageEntries
			}
			return page, nil
		}
	}

	if err := runScenarioRoutes(s, report, routeRunner, diagnostics); err != nil {
		finalizeScenarioReport(report)
		return report, err
	}

	// Run interactions on the LAST navigated page.
	if len(s.Interactions) > 0 && len(s.URLs) > 0 {
		lastIdx := len(report.Pages) - 1
		for _, inter := range s.Interactions {
			metric, err := runInteraction(d, inter)
			if err != nil {
				finalizeScenarioReport(report)
				return report, fmt.Errorf("interaction %s %s: %w", inter.Kind, inter.Selector, err)
			}
			report.Pages[lastIdx].Interactions = append(report.Pages[lastIdx].Interactions, *metric)
		}
	}

	finalizeScenarioReport(report)

	// Stop recording and write file.
	if recorder != nil {
		if err := recorder.Stop(d, s.RecordPath); err != nil {
			return report, fmt.Errorf("stop recording: %w", err)
		}
	}

	// Write trace file if one was captured. Written at the end so a
	// mid-run error doesn't leave a partial file around.
	if len(traceBytes) > 0 && s.TracePath != "" {
		if err := os.WriteFile(s.TracePath, traceBytes, 0o644); err != nil {
			return report, fmt.Errorf("write trace: %w", err)
		}
	}

	// Heap snapshot of the final page state, after interactions. GC
	// first so we measure live retention. Written last so its disk
	// write doesn't block the rest of the run.
	if s.HeapSnapshotPath != "" {
		snap, err := TakeHeapSnapshotAfterGC(d)
		if err != nil {
			return report, fmt.Errorf("heap snapshot: %w", err)
		}
		if err := os.WriteFile(s.HeapSnapshotPath, snap, 0o644); err != nil {
			return report, fmt.Errorf("write heap snapshot: %w", err)
		}
	}

	return report, nil
}

func runScenarioRoutes(s *Scenario, report *Report, runRoute scenarioRouteRunner, diagnostics io.Writer) error {
	total := len(s.URLs)
	for i, url := range s.URLs {
		routeStart := time.Now()
		fmt.Fprintf(diagnostics, "gosx perf: route %d/%d %s start timeout=%s\n", i+1, total, url, formatPerfTimeout(s.Timeout))
		ctx := context.Background()
		var cancel context.CancelFunc
		if s.Timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		} else {
			ctx, cancel = context.WithCancel(ctx)
		}
		page, err := runRoute(ctx, url, i, total)
		cancel()
		elapsed := time.Since(routeStart).Round(time.Millisecond)
		if err != nil {
			fmt.Fprintf(diagnostics, "gosx perf: route %d/%d %s failed elapsed=%s: %v\n", i+1, total, url, elapsed, err)
			return fmt.Errorf("route %d/%d %s: %w", i+1, total, url, err)
		}
		report.Pages = append(report.Pages, *page)
		fmt.Fprintf(diagnostics, "gosx perf: route %d/%d %s done elapsed=%s\n", i+1, total, url, elapsed)
	}
	return nil
}

func finalizeScenarioReport(report *Report) {
	// Single-page mode: copy into embedded PageReport for backward compat.
	if len(report.Pages) == 1 {
		report.PageReport = report.Pages[0]
		report.URL = report.Pages[0].URL
	} else if len(report.Pages) > 0 {
		report.URL = report.Pages[0].URL
	}
}

func formatPerfTimeout(timeout time.Duration) string {
	if timeout <= 0 {
		return "none"
	}
	return timeout.String()
}

func captureCoverageForRoute(enabled bool, index int) bool {
	return enabled && index == 0
}

func waitForSceneFrames(d *Driver, target int) error {
	// Poll frameCount until we have enough frames or timeout.
	if target <= 0 {
		return nil
	}
	js := fmt.Sprintf(`window.__gosx_perf && window.__gosx_perf.frameCount >= %d`, target)
	waitD, cancel := d.WithOperationContext(d.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var done bool
		if err := waitD.Evaluate(js, &done); err != nil {
			if waitD.Context().Err() != nil {
				return waitD.Context().Err()
			}
			return err
		}
		if done {
			return nil
		}
		select {
		case <-waitD.Context().Done():
			return waitD.Context().Err()
		case <-ticker.C:
		}
	}
}

func scene3DSurfacePresent(d *Driver) bool {
	var present bool
	err := d.Evaluate(`(function(){
		if (document.querySelector('[data-gosx-scene3d], [data-gosx-scene3d-mounted="true"], [data-gosx-scene3d-canvas="true"]')) {
			return true;
		}
		var engines = window.__gosx && window.__gosx.engines;
		if (!engines || typeof engines.forEach !== "function") {
			return false;
		}
		var found = false;
		engines.forEach(function(engine) {
			if (engine && (engine.component === "GoSXScene3D" || engine.name === "GoSXScene3D")) {
				found = true;
			}
		});
		return found;
	})()`, &present)
	return err == nil && present
}

func runInteraction(d *Driver, inter Interaction) (*InteractionMetric, error) {
	// Clear dispatch log before interaction so we measure only this one.
	_ = d.Evaluate(`window.__gosx_perf && (window.__gosx_perf.dispatchLog = [])`, nil)

	action := inter.Kind + " " + inter.Selector
	switch inter.Kind {
	case "click":
		if err := Click(d, inter.Selector); err != nil {
			return nil, err
		}
	case "type":
		if err := Type(d, inter.Selector, inter.Value); err != nil {
			return nil, err
		}
	case "scroll":
		pixels := 0
		fmt.Sscanf(inter.Value, "%d", &pixels)
		if err := Scroll(d, pixels); err != nil {
			return nil, err
		}
		action = fmt.Sprintf("scroll %dpx", pixels)
	default:
		return nil, fmt.Errorf("unknown interaction kind %q", inter.Kind)
	}

	// Wait for effects to settle.
	time.Sleep(200 * time.Millisecond)

	// Collect dispatch log for this interaction.
	dispatches, _ := QueryDispatchLog(d)
	metric := &InteractionMetric{Action: action}
	if len(dispatches) > 0 {
		for _, dl := range dispatches {
			metric.DispatchMs += dl.Ms
			metric.PatchCount += dl.Patches
		}
	}
	return metric, nil
}
