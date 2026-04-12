package perf

import (
	"fmt"
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
}

// Interaction is a user action to execute during profiling.
type Interaction struct {
	Kind     string // "click", "type", "scroll"
	Selector string
	Value    string // text for type, pixels for scroll
}

// RunScenario executes a full profiling session and returns a Report.
func RunScenario(s *Scenario) (*Report, error) {
	opts := []Option{WithHeadless(s.Headless)}
	if s.Timeout > 0 {
		opts = append(opts, WithTimeout(s.Timeout))
	}

	d, err := New(opts...)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	if err := InjectDriver(d); err != nil {
		return nil, fmt.Errorf("inject: %w", err)
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
	for i, url := range s.URLs {
		navigate := func() error {
			if err := d.Navigate(url); err != nil {
				return fmt.Errorf("navigate %s: %w", url, err)
			}
			if err := d.WaitReady(); err != nil {
				return fmt.Errorf("wait ready %s: %w", url, err)
			}
			// Allow instrumentation + initial renders to settle.
			time.Sleep(300 * time.Millisecond)
			return nil
		}

		if s.TracePath != "" && i == 0 {
			tb, err := CaptureTrace(d, navigate)
			if err != nil {
				return nil, fmt.Errorf("capture trace: %w", err)
			}
			traceBytes = tb
		} else if err := navigate(); err != nil {
			return nil, err
		}

		page, err := CollectPageReport(d, url)
		if err != nil {
			return nil, fmt.Errorf("collect %s: %w", url, err)
		}

		// If scene detected, wait for frames then re-collect scene metrics.
		if page.Scene != nil {
			waitForSceneFrames(d, frames)
			sceneEntries, _ := QuerySceneFrames(d)
			if len(sceneEntries) > 0 {
				durations := make([]float64, len(sceneEntries))
				for i, e := range sceneEntries {
					durations[i] = e.Duration
				}
				page.Scene.FrameStats = ComputeFrameStats(durations)
				page.Scene.FrameCount = len(sceneEntries)
			}
		}

		report.Pages = append(report.Pages, *page)
	}

	// Run interactions on the LAST navigated page.
	if len(s.Interactions) > 0 && len(s.URLs) > 0 {
		lastIdx := len(report.Pages) - 1
		for _, inter := range s.Interactions {
			metric, err := runInteraction(d, inter)
			if err != nil {
				return nil, fmt.Errorf("interaction %s %s: %w", inter.Kind, inter.Selector, err)
			}
			report.Pages[lastIdx].Interactions = append(report.Pages[lastIdx].Interactions, *metric)
		}
	}

	// Single-page mode: copy into embedded PageReport for backward compat.
	if len(report.Pages) == 1 {
		report.PageReport = report.Pages[0]
		report.URL = report.Pages[0].URL
	} else if len(report.Pages) > 0 {
		report.URL = report.Pages[0].URL
	}

	// Stop recording and write file.
	if recorder != nil {
		if err := recorder.Stop(d, s.RecordPath); err != nil {
			return nil, fmt.Errorf("stop recording: %w", err)
		}
	}

	// Write trace file if one was captured. Written at the end so a
	// mid-run error doesn't leave a partial file around.
	if len(traceBytes) > 0 && s.TracePath != "" {
		if err := os.WriteFile(s.TracePath, traceBytes, 0o644); err != nil {
			return nil, fmt.Errorf("write trace: %w", err)
		}
	}

	return report, nil
}

func waitForSceneFrames(d *Driver, target int) {
	// Poll frameCount until we have enough frames or timeout.
	js := fmt.Sprintf(`window.__gosx_perf && window.__gosx_perf.frameCount >= %d`, target)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var done bool
		if err := d.Evaluate(js, &done); err != nil || done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
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
