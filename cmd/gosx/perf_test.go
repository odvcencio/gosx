package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInterspersedPerfArgsMovesFlagsBeforeURLs(t *testing.T) {
	got := interspersedPerfArgs([]string{
		"http://localhost:3000/",
		"--mobile", "pixel7",
		"http://localhost:3000/scene",
		"--throttle=4",
		"--coverage",
		"--budget", "perf-budget.json",
	})
	want := []string{
		"--mobile", "pixel7",
		"--throttle=4",
		"--coverage",
		"--budget", "perf-budget.json",
		"http://localhost:3000/",
		"http://localhost:3000/scene",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interspersedPerfArgs() = %#v, want %#v", got, want)
	}
}

func TestInterspersedPerfArgsStopsAtSeparator(t *testing.T) {
	got := interspersedPerfArgs([]string{
		"--json",
		"--",
		"http://localhost:3000/",
		"--not-a-flag",
	})
	want := []string{
		"--json",
		"http://localhost:3000/",
		"--not-a-flag",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interspersedPerfArgs() = %#v, want %#v", got, want)
	}
}

func TestPerfCompareCLIExitsNonzeroForIncompleteRequiredEvidence(t *testing.T) {
	tmp := t.TempDir()
	baseline := filepath.Join(tmp, "baseline.json")
	candidate := filepath.Join(tmp, "candidate.json")
	if err := os.WriteFile(baseline, []byte(`{
		"url":"baseline",
		"timestamp":"2026-04-11T12:00:00Z",
		"pages":[{"url":"baseline","resources":[{"name":"app.js","transferSize":1024}],"totalBytesTransferred":1024,"measurements":{"version":1,"groups":{"resource-waterfall":{"state":"ok"},"long-tasks":{"state":"ok"}}}}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte(`{
		"url":"candidate",
		"timestamp":"2026-04-11T12:00:00Z",
		"pages":[{"url":"candidate","totalBytesTransferred":0,"measurements":{"version":1,"groups":{"resource-waterfall":{"state":"failed","reason":"cdp timeout"},"long-tasks":{"state":"ok"}}}}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPerfCompareCLIHelperProcess$")
	cmd.Env = append(os.Environ(),
		"GOSX_PERF_COMPARE_HELPER=1",
		"GOSX_PERF_COMPARE_BASELINE="+baseline,
		"GOSX_PERF_COMPARE_CANDIDATE="+candidate,
	)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("perf compare should fail for incomplete required evidence:\n%s", out)
	}
	if ctx.Err() != nil {
		t.Fatalf("perf compare subprocess timed out: %v\n%s", ctx.Err(), out)
	}
	var payload struct {
		Metrics []map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("perf compare JSON output invalid: %v\n%s", err, out)
	}
	var network map[string]any
	for _, row := range payload.Metrics {
		if row["name"] == "Network bytes" {
			network = row
			break
		}
	}
	if network == nil {
		t.Fatalf("missing Network bytes row: %s", out)
	}
	if _, ok := network["deltaPct"]; ok {
		t.Fatalf("incomplete JSON row must omit deltaPct: %+v", network)
	}
	if network["candidateFound"] != false || network["incomplete"] != true ||
		!strings.Contains(network["incompleteReason"].(string), "cdp timeout") {
		t.Fatalf("incomplete JSON row missing flags/reason: %+v", network)
	}
}

func TestPerfCompareCLIHelperProcess(t *testing.T) {
	if os.Getenv("GOSX_PERF_COMPARE_HELPER") != "1" {
		return
	}
	baseline := os.Getenv("GOSX_PERF_COMPARE_BASELINE")
	candidate := os.Getenv("GOSX_PERF_COMPARE_CANDIDATE")
	if baseline == "" || candidate == "" {
		t.Fatalf("missing helper report paths")
	}
	os.Args = []string{"gosx", "perf", "compare", "--json", baseline, candidate}
	cmdPerfCompare()
	t.Fatalf("cmdPerfCompare returned; want os.Exit")
}
