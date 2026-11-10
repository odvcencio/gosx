package perf

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func recoveredBytesPage(fallback string, withChunk bool, totalKB float64) PageReport {
	page := recoveredWaterPage(fallback)
	page.MetricAvailability = &MetricAvailability{Network: true, LCP: true}
	page.LargestContentfulPaintMs = 900
	total := int64(totalKB * 1024)
	page.TotalBytesTransferred = total
	page.Resources = []ResourceEntry{{Name: "http://127.0.0.1:3071/gosx/bootstrap-feature-scene3d.js?v=abc", TransferSize: float64(total)}}
	if withChunk {
		page.Resources = append(page.Resources, ResourceEntry{Name: "http://127.0.0.1:3071/gosx/bootstrap-feature-scene3d-webgl.js?v=def", TransferSize: 329346})
		page.TotalBytesTransferred += 329346
	}
	return page
}

func bytesBudget(limit int) *BudgetFile {
	return &BudgetFile{DefaultProfile: "p", Profiles: map[string]BudgetProfile{"p": {Assertions: []string{fmt.Sprintf("network_kb <= %d", limit), "lcp <= 2000"}}}}
}

func TestRecoveryComparesBytesAfterSubtractingFallbackChunk(t *testing.T) {
	// 1919.5 KiB of page plus the 321.6 KiB WebGL chunk is 2241 KiB raw: over
	// the limit, but the page itself is under it.
	report := &Report{Pages: []PageReport{recoveredBytesPage("webgpu-device-lost", true, 1919.5)}}
	res, err := EvaluateBudget(report, bytesBudget(1968), "")
	if err != nil || !res.Passed {
		t.Fatalf("page under the limit once the chunk is subtracted must pass: %v %+v", err, res)
	}
	a := res.Pages[0].Assertions[0]
	if !a.Adjusted || a.FallbackBytes != 329346 || a.Actual < 1919 || a.Actual > 1920 {
		t.Fatalf("assertion = %+v", a)
	}
	out := FormatBudgetResult(res)
	if !strings.Contains(out, "after subtracting 329346 fallback chunk bytes") {
		t.Fatalf("report does not explain the subtraction:\n%s", out)
	}
}

func TestRecoveryWithRealByteGrowthFails(t *testing.T) {
	for _, withChunk := range []bool{true, false} {
		report := &Report{Pages: []PageReport{recoveredBytesPage("webgpu-device-lost", withChunk, 2100)}}
		res, _ := EvaluateBudget(report, bytesBudget(1968), "")
		if res.Passed || res.Pages[0].Inconclusive && res.Pages[0].Assertions[0].Inconclusive {
			t.Fatalf("chunk=%v: growth over the limit must fail, not be inconclusive: %+v", withChunk, res)
		}
	}
}

func TestRecoveryWithoutFallbackChunkStillWorks(t *testing.T) {
	report := &Report{Pages: []PageReport{recoveredBytesPage("webgpu-device-lost", false, 1900)}}
	res, _ := EvaluateBudget(report, bytesBudget(1968), "")
	if !res.Passed || res.Pages[0].Assertions[0].FallbackBytes != 0 {
		t.Fatalf("no chunk fetched, nothing to subtract: %+v", res)
	}
}

func TestRecoveryStillExemptsTimingButNotBytes(t *testing.T) {
	page := recoveredBytesPage("webgpu-device-lost", true, 1900)
	page.LargestContentfulPaintMs = 9000
	res, _ := EvaluateBudget(&Report{Pages: []PageReport{page}}, bytesBudget(1968), "")
	if !res.Passed || !res.Pages[0].Inconclusive || !res.Pages[0].Assertions[1].Inconclusive {
		t.Fatalf("slow LCP during recovery stays inconclusive: %+v", res)
	}
}

func TestNoRecoveryLeavesBytesUnchanged(t *testing.T) {
	page := recoveredBytesPage("", true, 1900) // chunk present but no recovery: it counts
	res, _ := EvaluateBudget(&Report{Pages: []PageReport{page}}, bytesBudget(1968), "")
	a := res.Pages[0].Assertions[0]
	if res.Passed || a.Adjusted || a.Actual < 2200 {
		t.Fatalf("without a recovery the full byte count is judged: %+v", res)
	}
}

func TestCollectionTimeoutWithStalledRendererIsInconclusive(t *testing.T) {
	stall := frameStallReason(sceneFrameSampleResult{Target: 120, Observed: 2, Exhausted: true})
	if stall == "" {
		t.Fatal("2 of 120 frames is a stall")
	}
	stub := collectionStallPage("http://127.0.0.1:3071/demos/water", stall, fmt.Errorf("collect/navigation-timing: %w", context.DeadlineExceeded))
	if stub == nil {
		t.Fatal("stalled timeout must produce a stub")
	}
	res, err := EvaluateBudget(&Report{Pages: []PageReport{*stub}}, bytesBudget(1968), "")
	if err != nil || !res.Passed || !res.Pages[0].Inconclusive {
		t.Fatalf("stalled collection must be inconclusive: %v %+v", err, res)
	}
	out := FormatBudgetResult(res)
	if !strings.Contains(out, "collection timed out while the renderer was stalled") || !strings.Contains(out, "2 of 120") {
		t.Fatalf("reason missing:\n%s", out)
	}
}

func TestCollectionTimeoutWithHealthyRendererStillFails(t *testing.T) {
	timeout := fmt.Errorf("collect: %w", context.DeadlineExceeded)
	if collectionStallPage("u", "", timeout) != nil {
		t.Fatal("no stall evidence: must stay a hard failure")
	}
	if frameStallReason(sceneFrameSampleResult{Target: 120, Observed: 100, Exhausted: true}) != "" {
		t.Fatal("a healthy frame rate is not a stall")
	}
	if collectionStallPage("u", "scene3d-frames-stalled", errors.New("boom")) != nil {
		t.Fatal("a non-timeout error is never turned into a stub")
	}
}
