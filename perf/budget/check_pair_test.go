package budget

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func gatePair(t *testing.T) CheckOptions {
	t.Helper()
	opts := gateOptions(t)
	hl, lower, upper := 1.1, 1.05, 1.2
	p, denominator := "1", "1024"
	cell := Cell{App: "fixture", RouteTemplate: "/counter/", PageType: "island", Scenario: "hard-cold", Backend: "none", Metric: "lcp", Unit: "ms"}
	row := PairCell{Cell: cell, Pairs: 10, HL: &hl, MedianDifference: &hl, Threshold: 1, Decision: "regression", StoppingLook: 10,
		AdjustedInterval: Interval{Lower: &lower, Upper: &upper, ConfidencePPM: 996875, Bounded: true}, DescriptiveInterval: Interval{Lower: &lower, Upper: &upper, ConfidencePPM: 950000, Bounded: true},
		PNumerator: &p, PDenominator: &denominator, InvalidReasons: []CountReason{}}
	for range 10 {
		row.BaseSamples = append(row.BaseSamples, 1000)
		row.HeadSamples = append(row.HeadSamples, 1001.1)
	}
	opts.Pair = &PairReport{Schema: "gosx.perf-pair/v1", Info: opts.Head.Info, FamilySize: 1, Looks: []int64{10, 20, 30, 40}, Seed: 1, Test: "signed-rank", PlanSHA256: strings.Repeat("1", 64), Status: "complete", Cells: []PairCell{row}}
	return opts
}

func TestCheckPairTimingRequiresCeilingAcknowledgment(t *testing.T) {
	opts := gatePair(t)
	for _, reportOnly := range []bool{false, true} {
		opts.ReportOnly = reportOnly
		out, err := Check(opts)
		if err != nil || out.Passed || !hasGateViolation(out, "growth-ack") {
			t.Fatalf("timing regression not reported: %v", err)
		}
		want := 1
		if reportOnly {
			want = 0
		}
		if CheckExitCode(out, err) != want {
			t.Fatal("timing violation exit mode changed")
		}
	}
	opts.Trailers, _ = ParseTrailers("Change\n\nPerf-Timing: cell=fixture|/counter/|island|hard-cold|none|lcp delta=+1ms issue=#7; because=Added interaction behavior\n")
	if out, err := Check(opts); err != nil || out.Passed || !hasGateViolation(out, "growth-ack") {
		t.Fatal("undersized timing acknowledgment accepted")
	}
	// Section 17.4 allows an amount at least ceil(HL), including a larger one.
	opts.Trailers, _ = ParseTrailers("Change\n\nPerf-Timing: cell=fixture|/counter/|island|hard-cold|none|lcp delta=+3ms issue=#7; because=Added interaction behavior\n")
	if out, err := Check(opts); err != nil || !out.Passed || out.Acknowledgments[0].Delta != 3 {
		t.Fatal("larger timing acknowledgment rejected")
	}
	opts.Trailers, _ = ParseTrailers("Change\n\nPerf-Timing: cell=fixture|/counter/|island|hard-cold|none|lcp delta=+2ms issue=#7; because=Added interaction behavior\n")
	before, _ := json.Marshal(opts)
	out, err := Check(opts)
	after, _ := json.Marshal(opts)
	if err != nil || !out.Passed || len(out.Acknowledgments) != 1 || out.Acknowledgments[0].Delta != 2 || !reflect.DeepEqual(before, after) {
		t.Fatalf("exact timing acknowledgment or immutability failed: %v", err)
	}
	page := opts.File.PageTypes["island"]
	if out.Rows[0].AllocationBytes != page.Allocation.TotalBytes || out.Rows[0].FrameworkCeilingBytes != page.Allocation.FrameworkBytes {
		t.Fatal("timing acknowledgment changed allocation")
	}
	setGateBytes(opts.Head, page.Allocation.FrameworkBytes+1, page.Allocation.FrameworkBytes+1)
	out, err = Check(opts)
	if err != nil || out.Passed || !hasGateViolation(out, "framework-share") {
		t.Fatal("timing acknowledgment waived framework cap")
	}
}

func TestCheckPairProvenanceDomainsAndScope(t *testing.T) {
	changes := map[string]func(*CheckOptions){
		"head":               func(o *CheckOptions) { o.Pair.Info.SHA = strings.Repeat("f", 40) },
		"base":               func(o *CheckOptions) { o.Pair.Info.BaseSHA = strings.Repeat("f", 40) },
		"profile":            func(o *CheckOptions) { o.Pair.Info.ProfileSHA256 = strings.Repeat("f", 64) },
		"coefficients":       func(o *CheckOptions) { o.Pair.Info.CoefficientSHA256 = strings.Repeat("f", 64) },
		"toolchain":          func(o *CheckOptions) { o.Pair.Info.ToolchainSHA256 = strings.Repeat("f", 64) },
		"catalog":            func(o *CheckOptions) { o.Pair.Info.FixtureSHA256 = strings.Repeat("f", 64) },
		"epoch":              func(o *CheckOptions) { v := strings.Repeat("f", 64); o.Pair.Info.EpochSHA256 = &v },
		"head-artifact":      func(o *CheckOptions) { v := strings.Repeat("f", 64); o.Pair.Info.ArtifactSHA256 = &v },
		"base-artifact":      func(o *CheckOptions) { o.Pair.Info.BaseArtifactSHA256 = strings.Repeat("f", 64) },
		"canonical":          func(o *CheckOptions) { o.Pair.Info.Canonical = false },
		"headless":           func(o *CheckOptions) { o.Pair.Info.Headless = false },
		"muted":              func(o *CheckOptions) { o.Pair.Info.Muted = false },
		"scenario":           func(o *CheckOptions) { o.Pair.Cells[0].Cell.Scenario = "hard-warm" },
		"backend":            func(o *CheckOptions) { o.Pair.Cells[0].Cell.Backend = "webgpu" },
		"route":              func(o *CheckOptions) { o.Pair.Cells[0].Cell.RouteTemplate = "/unregistered/" },
		"duplicates":         func(o *CheckOptions) { o.Pair.FamilySize = 2; o.Pair.Cells = append(o.Pair.Cells, o.Pair.Cells[0]) },
		"partial-regression": func(o *CheckOptions) { o.Pair.Status = "partial" },
		"no-hl":              func(o *CheckOptions) { o.Pair.Cells[0].HL = nil },
		"no-lower":           func(o *CheckOptions) { o.Pair.Cells[0].AdjustedInterval.Lower = nil },
		"no-upper":           func(o *CheckOptions) { o.Pair.Cells[0].AdjustedInterval.Upper = nil },
		"unbounded":          func(o *CheckOptions) { o.Pair.Cells[0].AdjustedInterval.Bounded = false },
		"below-threshold":    func(o *CheckOptions) { o.Pair.Cells[0].Threshold = 2 },
		"no-p":               func(o *CheckOptions) { o.Pair.Cells[0].PNumerator = nil },
		"no-denominator":     func(o *CheckOptions) { o.Pair.Cells[0].PDenominator = nil },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			o := gatePair(t)
			o.ReportOnly = true
			change(&o)
			out, err := Check(o)
			var input *InputError
			if out != nil || !errors.As(err, &input) || CheckExitCode(out, err) != 2 {
				t.Fatalf("invalid pair became report-only success: %v", err)
			}
		})
	}
}

func TestCheckPairInconclusiveEvidenceDoesNotRequireTrailer(t *testing.T) {
	for _, decision := range []string{"inconclusive", "no-material-regression", "inconclusive-environment"} {
		opts := gatePair(t)
		opts.Pair.Cells[0].Decision = decision
		out, err := Check(opts)
		if err != nil || !out.Passed || len(out.Acknowledgments) != 0 {
			t.Fatalf("nonregression cell required timing footer: %v", err)
		}
	}
	opts := gatePair(t)
	opts.Pair = nil
	if out, err := Check(opts); err != nil || !out.Passed {
		t.Fatal("optional timing evidence required", err)
	}
}

func TestCheckPairRequirementsSortCompleteCellKeysAndPreserveInputs(t *testing.T) {
	opts := gatePair(t)
	second := opts.Pair.Cells[0]
	second.Cell.Metric = "inp"
	opts.Pair.FamilySize = 2
	opts.Pair.Cells = append(opts.Pair.Cells, second)
	before, _ := json.Marshal(opts.Pair)
	rows, err := requiredPairTiming(opts.File, opts.Head, opts.Base, opts.Pair)
	if err != nil || len(rows) != 2 || !strings.HasSuffix(rows[0].Scope, "|inp") || !strings.HasSuffix(rows[1].Scope, "|lcp") || rows[0].Delta != 2 || rows[1].Delta != 2 {
		t.Fatalf("sorted requirements did not retain complete timing cell: %v", err)
	}
	after, _ := json.Marshal(opts.Pair)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("timing requirement derivation mutated evidence")
	}
	rows[0].Scope = "modified"
	if opts.Pair.Cells[1].Cell.Metric != "inp" {
		t.Fatal("requirements alias input")
	}
}

func TestCheckPairMemoryEvidenceDoesNotUseMillisecondAcknowledgment(t *testing.T) {
	opts := gatePair(t)
	row := &opts.Pair.Cells[0]
	row.Cell.Metric, row.Cell.Unit = "js_heap_peak", "B"
	for i := range row.HeadSamples {
		row.HeadSamples[i] = 1002
	}
	hl, lower, upper := 2.0, 1.5, 2.5
	row.HL, row.MedianDifference = &hl, &hl
	row.AdjustedInterval.Lower, row.AdjustedInterval.Upper = &lower, &upper
	row.DescriptiveInterval = row.AdjustedInterval
	rows, err := requiredPairTiming(opts.File, opts.Head, opts.Base, opts.Pair)
	if err != nil || len(rows) != 0 {
		t.Fatalf("memory bytes were relabeled as milliseconds: %v", err)
	}
}

func TestCheckPairCompleteFamilyAndDeclaredStoppingLook(t *testing.T) {
	for _, mutate := range []func(*PairReport){
		func(p *PairReport) { p.FamilySize = 2 },
		func(p *PairReport) { p.Cells[0].StoppingLook = 11 },
	} {
		opts := gatePair(t)
		mutate(opts.Pair)
		if out, err := Check(opts); out != nil || err == nil {
			t.Fatal("incomplete family or undeclared stopping look accepted")
		}
	}
}

func TestCheckPairByteAndTimingGrowthAreIndependent(t *testing.T) {
	opts := gatePair(t)
	// This increase exceeds both the fixed floor and the allocation's 1% tripwire.
	opts.Head.Assets[0].Brotli += 10_000
	timing := "Perf-Timing: cell=fixture|/counter/|island|hard-cold|none|lcp delta=+2ms issue=#7; because=Added interaction behavior"
	bytes := "Perf-Budget: scope=asset:app/fixture/counter metric=brotli delta=+10000B disposition=permanent issue=#8; because=Added interaction behavior"
	for _, footer := range []string{timing, bytes} {
		opts.Trailers, _ = ParseTrailers("Change\n\n" + footer + "\n")
		out, err := Check(opts)
		if err != nil || out.Passed || !hasGateViolation(out, "growth-ack") {
			t.Fatal("one acknowledgment covered unrelated regression", err)
		}
	}
	opts.Trailers, _ = ParseTrailers("Change\n\n" + timing + "\n" + bytes + "\n")
	out, err := Check(opts)
	if err != nil || !out.Passed || len(out.Acknowledgments) != 2 {
		t.Fatal("combined exact requirements failed", err)
	}
}
