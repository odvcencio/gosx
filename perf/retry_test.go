package perf

import (
	"bytes"
	"strings"
	"testing"
)

func recoveredWaterPage(fallback string) PageReport {
	return PageReport{
		URL:                      "http://127.0.0.1/demos/water",
		LargestContentfulPaintMs: 9648,
		TotalBytesTransferred:    1,
		WebGL:                    &WebGLInfo{Renderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero)), SwiftShader driver)"},
		Scene: &SceneMetric{Mounts: []SceneMountMetric{{
			Index: 0, Backend: "webgl", Renderer: "webgl", Fallback: fallback,
		}}},
	}
}

func waterBudget() *BudgetFile {
	return &BudgetFile{
		DefaultProfile: "water-ci",
		Profiles:       map[string]BudgetProfile{"water-ci": {Assertions: []string{"lcp <= 2000"}}},
	}
}

func TestRendererRecoveryIgnoresInitialFallback(t *testing.T) {
	for fallback, want := range map[string]string{
		"webgpu-device-lost":                     "webgpu-device-lost",
		"webgpu-render-stall":                    "webgpu-render-stall",
		"webgpu-persistent-frame-error-fallback": "webgpu-persistent-frame-error-fallback",
		"webgpu-unavailable":                     "",
		"webgpu-feature-gap":                     "",
		"":                                       "",
	} {
		if got := RendererRecovery(recoveredWaterPage(fallback)); got != want {
			t.Errorf("fallback %q: recovery %q, want %q", fallback, got, want)
		}
	}
	page := recoveredWaterPage("")
	page.Scene.Counters = map[string]float64{"render-watchdog-fallbacks": 1}
	if got := RendererRecovery(page); got != "render-watchdog-fallback" {
		t.Errorf("watchdog counter: recovery %q", got)
	}
	if got := RendererRecovery(PageReport{URL: "http://127.0.0.1/docs"}); got != "" {
		t.Errorf("page without a scene: recovery %q", got)
	}
}

func TestBudgetFailureDuringRecoveryIsInconclusive(t *testing.T) {
	report := &Report{Pages: []PageReport{recoveredWaterPage("webgpu-device-lost")}}
	result, err := EvaluateBudget(report, waterBudget(), "")
	if err != nil {
		t.Fatal(err)
	}
	page := result.Pages[0]
	if !result.Passed || !page.Inconclusive || page.Recovery != "webgpu-device-lost" || page.Assertions[0].Passed {
		t.Fatalf("recovered page: %+v", result)
	}
	out := FormatBudgetResult(result)
	for _, want := range []string{"inconclusive", "SwiftShader", "backend=webgl", "recovery=webgpu-device-lost", "inc  lcp <= 2000"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted result lacks %q:\n%s", want, out)
		}
	}
}

func TestBudgetFailureWithoutRecoveryStillFails(t *testing.T) {
	for _, fallback := range []string{"", "webgpu-unavailable"} {
		report := &Report{Pages: []PageReport{recoveredWaterPage(fallback)}}
		result, err := EvaluateBudget(report, waterBudget(), "")
		if err != nil {
			t.Fatal(err)
		}
		if result.Passed || result.Pages[0].Inconclusive {
			t.Fatalf("fallback %q: a real LCP regression must fail: %+v", fallback, result)
		}
	}
}

func TestPassingRecoveredPageIsNotInconclusive(t *testing.T) {
	page := recoveredWaterPage("webgpu-device-lost")
	page.LargestContentfulPaintMs = 1200
	result, err := EvaluateBudget(&Report{Pages: []PageReport{page}}, waterBudget(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Pages[0].Inconclusive {
		t.Fatalf("passing page: %+v", result)
	}
}

func TestRetryRecoveredPagesRemeasuresOnlyRecoveredPages(t *testing.T) {
	clean := PageReport{URL: "http://127.0.0.1/docs/getting-started", LargestContentfulPaintMs: 400}
	report := &Report{Pages: []PageReport{clean, recoveredWaterPage("webgpu-device-lost")}}
	var log bytes.Buffer
	s := &Scenario{
		URLs:         []string{clean.URL, "http://127.0.0.1/demos/water"},
		Coverage:     true,
		Interactions: []Interaction{{Kind: "click", Selector: "#x"}},
		diagnostics:  &log,
	}
	var runs []Scenario
	retryRecoveredPages(s, report, func(r *Scenario) (*Report, error) {
		runs = append(runs, *r)
		page := recoveredWaterPage("")
		page.LargestContentfulPaintMs = 1100
		return &Report{Pages: []PageReport{page}}, nil
	})
	if len(runs) != 1 || len(runs[0].URLs) != 1 || runs[0].URLs[0] != "http://127.0.0.1/demos/water" {
		t.Fatalf("retries: %+v", runs)
	}
	if runs[0].Coverage {
		t.Fatal("retry of the second route captured coverage; the first attempt did not")
	}
	if len(runs[0].Interactions) != 1 {
		t.Fatal("retry of the last route dropped its interactions")
	}
	if report.Pages[0].LargestContentfulPaintMs != 400 || report.Pages[1].LargestContentfulPaintMs != 1100 {
		t.Fatalf("pages after retry: %+v", report.Pages)
	}
	result, err := EvaluateBudget(report, waterBudget(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Pages[1].Inconclusive {
		t.Fatalf("clean retry should be judged normally: %+v", result)
	}
	if !strings.Contains(log.String(), "renderer-recovery=webgpu-device-lost") || !strings.Contains(log.String(), "retry measured without a renderer recovery") {
		t.Fatalf("diagnostics:\n%s", log.String())
	}
}

func TestRetryThatRecoversAgainStaysInconclusive(t *testing.T) {
	report := &Report{Pages: []PageReport{recoveredWaterPage("webgpu-device-lost")}}
	var log bytes.Buffer
	s := &Scenario{URLs: []string{"http://127.0.0.1/demos/water"}, diagnostics: &log}
	retryRecoveredPages(s, report, func(*Scenario) (*Report, error) {
		return &Report{Pages: []PageReport{recoveredWaterPage("webgpu-device-lost")}}, nil
	})
	result, err := EvaluateBudget(report, waterBudget(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || !result.Pages[0].Inconclusive {
		t.Fatalf("second recovery: %+v", result)
	}
	if !strings.Contains(log.String(), "retry recovered again") {
		t.Fatalf("diagnostics:\n%s", log.String())
	}
}
