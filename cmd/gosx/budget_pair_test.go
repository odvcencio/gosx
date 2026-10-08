package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

func budgetPairCommandFixture(t *testing.T, base *budget.Report) string {
	t.Helper()
	info := base.Info
	info.BaseSHA = base.Info.SHA
	info.BaseArtifactSHA256 = *base.Info.ArtifactSHA256
	hl, lower, upper := 1.1, 1.05, 1.2
	p, denominator := "1", "1024"
	cell := budget.PairCell{Cell: budget.Cell{App: "fixture", RouteTemplate: "/counter/", PageType: "island", Scenario: "hard-cold", Backend: "none", Metric: "lcp", Unit: "ms"}, Pairs: 10, HL: &hl, MedianDifference: &hl, Threshold: 1, Decision: "regression", StoppingLook: 10,
		AdjustedInterval: budget.Interval{Lower: &lower, Upper: &upper, ConfidencePPM: 996875, Bounded: true}, DescriptiveInterval: budget.Interval{Lower: &lower, Upper: &upper, ConfidencePPM: 950000, Bounded: true}, PNumerator: &p, PDenominator: &denominator, InvalidReasons: []budget.CountReason{}}
	for range 10 {
		cell.BaseSamples = append(cell.BaseSamples, 1000)
		cell.HeadSamples = append(cell.HeadSamples, 1001.1)
	}
	pair := budget.PairReport{Schema: "gosx.perf-pair/v1", Info: info, FamilySize: 1, Looks: []int64{10, 20, 30, 40}, Seed: 1, Test: "signed-rank", PlanSHA256: strings.Repeat("1", 64), Status: "complete", Cells: []budget.PairCell{cell}}
	data, err := json.Marshal(pair)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pair.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBudgetCheckConsumesPairReportAndTimingFooter(t *testing.T) {
	args, base := budgetCheckCommandFixture(t)
	pairPath := budgetPairCommandFixture(t, base)
	args = append(args, "--report-only", "--pair-report", pairPath)
	var stdout, stderr bytes.Buffer
	if code := runBudgetCheckWith(args, &stdout, &stderr, budgetCheckFakeCollector(base)); code != 0 {
		t.Fatalf("pair status %d: %s", code, stderr.String())
	}
	record, err := budget.DecodeRecord(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	report := record.(*budget.Report)
	missing := false
	for _, v := range report.Violations {
		missing = missing || v.ReasonCode == "growth-ack"
	}
	if !missing {
		t.Fatal("timing footer requirement lost")
	}
	footer := filepath.Join(t.TempDir(), "change.txt")
	if err := os.WriteFile(footer, []byte("Change\n\nPerf-Timing: cell=fixture|/counter/|island|hard-cold|none|lcp delta=+2ms issue=#7; because=Added interaction behavior\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runBudgetCheckWith(append(args, "--trailers", footer), &stdout, &stderr, budgetCheckFakeCollector(base)); code != 0 {
		t.Fatalf("ack status %d: %s", code, stderr.String())
	}
	record, err = budget.DecodeRecord(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	report = record.(*budget.Report)
	if len(report.Acknowledgments) != 1 || report.Acknowledgments[0].Kind != "timing" || report.Acknowledgments[0].Delta != 2 {
		t.Fatal("exact timing acknowledgment omitted")
	}
	for _, v := range report.Violations {
		if v.ReasonCode == "growth-ack" {
			t.Fatal("matched timing acknowledgment still failed")
		}
	}
}

func TestBudgetCheckPairRejectsPrivateDataWrongRootAndBinding(t *testing.T) {
	args, base := budgetCheckCommandFixture(t)
	pairPath := budgetPairCommandFixture(t, base)
	valid, _ := os.ReadFile(pairPath)
	wrongSHA := bytes.Replace(valid, []byte(base.Info.SHA), []byte(strings.Repeat("f", 40)), 1)
	wrongRoot, _ := json.Marshal(base)
	for _, data := range [][]byte{[]byte(`{"private-field":"private-value"}`), wrongRoot, wrongSHA} {
		if err := os.WriteFile(pairPath, data, 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := runBudgetCheckWith(append(args, "--report-only", "--pair-report", pairPath), &stdout, &stderr, budgetCheckFakeCollector(base)); code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "private-value") {
			t.Fatal("invalid timing evidence became report-only success", code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	called := false
	collect := func(context.Context, budget.CollectOptions) (*budget.Report, error) { called = true; return nil, nil }
	if code := runBudgetCheckWith(append(args, "--chunks-only", "--pair-report", pairPath), &stdout, &stderr, collect); code != 2 || called {
		t.Fatal("offline inventory consumed timing evidence")
	}
}
