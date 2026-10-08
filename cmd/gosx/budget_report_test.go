package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

func budgetPublicCommandFixture(t *testing.T) (string, string, *budget.Report) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "perf/budget/testdata/public-report.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	record, err := budget.DecodeRecord(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return root, path, record.(*budget.Report)
}

func TestBudgetReportAndPublicCheckRoundTrip(t *testing.T) {
	root, path, report := budgetPublicCommandFixture(t)
	before, _ := os.ReadFile(path)
	out := filepath.Join(t.TempDir(), "report.md")
	var stdout, stderr bytes.Buffer
	args := []string{"report", "--root", root, "--report", path, "--out", out}
	if code := RunBudget(args, &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("report status %d: %s", code, stderr.String())
	}
	validator, err := budgetPublicValidator(root)
	if err != nil {
		t.Fatal(err)
	}
	var expected bytes.Buffer
	if err := validator.WriteMarkdown(&expected, *report); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(expected.Bytes(), actual) {
		t.Fatalf("report content differs: %v", err)
	}
	if code := RunBudget([]string{"public-check", "--root", root, path, out}, &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("public check status %d: %s", code, stderr.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("report command modified input JSON")
	}
}

func TestBudgetPublicCheckFormatsAndNativeDiagnostics(t *testing.T) {
	root, path, report := budgetPublicCommandFixture(t)
	point := budget.SeriesPoint{Schema: "gosx.perf-series/v1", Info: report.Info, Cell: budget.Cell{App: "fixture", RouteTemplate: "/counter/", PageType: "island", Scenario: "hard-cold", Backend: "none", Metric: "lcp", Unit: "ms"}, At: "2026-01-01T00:00:00Z", RunOrdinal: 1, N: 2, Samples: []float64{1, 2}, Median: 1.5, P75: 2, MAD: .5, InvalidReasons: []budget.CountReason{}}
	pointData, _ := json.Marshal(point)
	stream := filepath.Join(t.TempDir(), "records.jsonl")
	if err := os.WriteFile(stream, append(append(pointData, '\n'), append(pointData, '\n')...), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{stream}, {"--format", "jsonl", stream}, {"--format", "json", path}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"public-check", "--root", root}, flags...)
		if code := RunBudget(args, &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("valid format status %d: %s", code, stderr.String())
		}
	}
	invalid := filepath.Join(t.TempDir(), "unregistered-record.json")
	if err := os.WriteFile(invalid, []byte(`{"secret-key":"secret-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{"--format", "xml", path}, {stream, invalid}, {"--format", "json", stream}, {path + ".secret"}, {}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"public-check", "--root", root}, flags...)
		if code := RunBudget(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("invalid input status %d", code)
		}
		for _, secret := range []string{root, path, "secret-key", "secret-value", ".secret"} {
			if strings.Contains(stderr.String(), secret) {
				t.Fatalf("native value echoed: %s", stderr.String())
			}
		}
	}
}

func TestBudgetReportFailurePreservesOutput(t *testing.T) {
	root, path, report := budgetPublicCommandFixture(t)
	status := budget.RunStatus{Schema: "gosx.perf-run/v1", Info: report.Info, Status: "skipped-busy", ReasonCode: "busy", PlannedCells: 2}
	statusData, _ := json.Marshal(status)
	for _, body := range [][]byte{
		[]byte(`{"secret-key":"secret-value"}`), statusData,
		append([]byte{0xff}, statusData...),
	} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "report.md")
		if err := os.WriteFile(out, []byte("previous output"), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := RunBudget([]string{"report", "--root", root, "--report", path, "--out", out}, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("invalid report status %d", code)
		}
		got, _ := os.ReadFile(out)
		if string(got) != "previous output" || strings.Contains(stderr.String(), "secret") || strings.Contains(stderr.String(), out) {
			t.Fatal("rejected input changed output or leaked native data")
		}
	}
}

func TestBudgetPublicCommandsRejectMissingAndFutureFlags(t *testing.T) {
	for _, args := range [][]string{
		{"report"}, {"report", "--field", "unregistered-file"}, {"report", "--release"},
		{"public-check"}, {"public-check", "--unknown", "unregistered-file"},
	} {
		var stdout, stderr bytes.Buffer
		if code := RunBudget(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.String() != "invalid-input: cli#/flags\n" {
			t.Fatalf("flags status %d: %s", code, stderr.String())
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "perf/budgets/gosx.budget.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"secret-key":"secret-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := budgetPublicValidator(root); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid installed budget fell back to another catalog or leaked values")
	}
}
