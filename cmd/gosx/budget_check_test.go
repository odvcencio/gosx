package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

func budgetCheckCommandFixture(t *testing.T) ([]string, *budget.Report) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GOSX_BUDGET_PRODUCER_SHA256_FIXTURE", strings.Repeat("7", 64))
	root, path, base := budgetPublicCommandFixture(t)
	config := filepath.Join(root, "perf/budget/testdata/budget.v2.json")
	inputs, err := budget.LoadInputs(config, budget.LoadOptions{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	base.Info.SHA = strings.TrimSpace(string(head))
	base.Info.ProfileSHA256, base.Info.CoefficientSHA256 = inputs.File.Profile.SHA256, inputs.File.Coefficients.SHA256
	base.Info.ToolchainSHA256, base.Info.FixtureSHA256 = inputs.File.Toolchain.SHA256, inputs.File.Fixtures.SHA256
	digest := strings.Repeat("7", 64)
	base.Info.ArtifactSHA256 = &digest
	data, _ := json.Marshal(base)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"--budget", config, "--root", root, "--app", "fixture=sensitive-native-endpoint", "--dist", "fixture=" + t.TempDir(), "--base-report", path}, base
}
func budgetCheckFakeCollector(base *budget.Report) func(context.Context, budget.CollectOptions) (*budget.Report, error) {
	return func(_ context.Context, opts budget.CollectOptions) (*budget.Report, error) {
		data, _ := json.Marshal(base)
		var report budget.Report
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, err
		}
		report.Info.SHA, report.Info.BaseSHA, report.Info.BaseArtifactSHA256 = opts.SHA, base.Info.SHA, *base.Info.ArtifactSHA256
		return &report, nil
	}
}

func TestBudgetCheckExitMatrixAndValidatedOutput(t *testing.T) {
	args, base := budgetCheckCommandFixture(t)
	for _, reportOnly := range []bool{false, true} {
		flags := append([]string{}, args...)
		if reportOnly {
			flags = append(flags, "--report-only")
		}
		var stdout, stderr bytes.Buffer
		code := runBudgetCheckWith(flags, &stdout, &stderr, budgetCheckFakeCollector(base))
		want := 1
		if reportOnly {
			want = 0
		}
		if code != want || stderr.Len() != 0 {
			t.Fatalf("status %d, want %d: %s", code, want, stderr.String())
		}
		record, err := budget.DecodeRecord(bytes.NewReader(stdout.Bytes()))
		if err != nil || record.(*budget.Report).Passed || len(record.(*budget.Report).Violations) == 0 {
			t.Fatalf("violations hidden: %v", err)
		}
		if strings.Contains(stdout.String(), "sensitive-native-endpoint") {
			t.Fatal("native endpoint entered output")
		}
	}
	var stdout, stderr bytes.Buffer
	code := runBudgetCheckWith(append(args, "--report-only"), &stdout, &stderr, func(context.Context, budget.CollectOptions) (*budget.Report, error) {
		return nil, errors.New("sensitive-native-endpoint")
	})
	if code != 2 || stdout.Len() != 0 || stderr.String() != "environment: collection#\n" {
		t.Fatalf("tool error suppressed or echoed: %d %s", code, stderr.String())
	}
}

func TestBudgetCheckAtomicArtifactsAndProvenanceMismatch(t *testing.T) {
	args, base := budgetCheckCommandFixture(t)
	report, markdown := filepath.Join(t.TempDir(), "head.json"), filepath.Join(t.TempDir(), "head.md")
	flags := append(args, "--report-only", "--report", report, "--markdown", markdown)
	var stdout, stderr bytes.Buffer
	if code := runBudgetCheckWith(flags, &stdout, &stderr, budgetCheckFakeCollector(base)); code != 0 || stdout.Len() != 0 {
		t.Fatalf("artifact status %d: %s", code, stderr.String())
	}
	jsonBefore, _ := os.ReadFile(report)
	mdBefore, _ := os.ReadFile(markdown)
	if code := RunBudget([]string{"public-check", "--root", args[3], report, markdown}, &stdout, &stderr); code != 0 {
		t.Fatalf("published artifact invalid: %s", stderr.String())
	}
	bad := func(ctx context.Context, o budget.CollectOptions) (*budget.Report, error) {
		r, e := budgetCheckFakeCollector(base)(ctx, o)
		r.Info.BaseSHA = strings.Repeat("f", 40)
		return r, e
	}
	if code := runBudgetCheckWith(flags, &stdout, &stderr, bad); code != 2 {
		t.Fatal("wrong base SHA accepted", code)
	}
	jsonAfter, _ := os.ReadFile(report)
	mdAfter, _ := os.ReadFile(markdown)
	if !bytes.Equal(jsonBefore, jsonAfter) || !bytes.Equal(mdBefore, mdAfter) {
		t.Fatal("rejected provenance replaced artifacts")
	}
}

func TestBudgetCheckFlagsCIClockAndTrustedEvent(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "--budget", "sensitive-native-input"}, {"check", "--app", "fixture=one", "--app", "fixture=two"}, {"check", "--pair-report", "sensitive-native-input"}} {
		code, out, err := runBudgetTest(args...)
		if code != 2 || out != "" || strings.Contains(err, "sensitive-native-input") {
			t.Fatal("invalid arguments accepted or echoed", code, err)
		}
	}
	args, base := budgetCheckCommandFixture(t)
	var stdout, stderr bytes.Buffer
	t.Setenv("GITHUB_ACTIONS", "true")
	if code := runBudgetCheckWith(append(args, "--now", "2026-01-01"), &stdout, &stderr, budgetCheckFakeCollector(base)); code != 2 || stdout.Len() != 0 {
		t.Fatal("CI clock override accepted")
	}
	root := args[3]
	path := filepath.Join(t.TempDir(), "event.json")
	t.Setenv("GITHUB_EVENT_PATH", path)
	data, _ := json.Marshal(map[string]any{"pull_request": map[string]any{"head": map[string]string{"sha": base.Info.SHA}, "base": map[string]string{"sha": strings.Repeat("b", 40)}}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	head, previous, err := budgetGitIdentity(root)
	if err != nil || head != base.Info.SHA || previous != strings.Repeat("b", 40) {
		t.Fatalf("trusted event binding failed: %v", err)
	}
	data = bytes.Replace(data, []byte(base.Info.SHA), []byte(strings.Repeat("f", 40)), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budgetGitIdentity(root); err == nil {
		t.Fatal("event head differs from checkout but accepted")
	}
}

func TestBudgetCheckChunksRemainAdvisoryAndBindingsComplete(t *testing.T) {
	args, base := budgetCheckCommandFixture(t)
	var stdout, stderr bytes.Buffer
	chunks := func(ctx context.Context, o budget.CollectOptions) (*budget.Report, error) {
		r, e := budgetCheckFakeCollector(base)(ctx, o)
		r.Mode = "chunks-only"
		r.Rows = []budget.Row{}
		r.Coverage.RoutesMeasured = 0
		r.Coverage.Reachability = "unknown"
		return r, e
	}
	if code := runBudgetCheckWith(append(args, "--chunks-only"), &stdout, &stderr, chunks); code != 0 {
		t.Fatal("advisory inventory failed", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	for _, extra := range [][]string{{"--app", "unregistered=sensitive-native-input"}, {"--dist", "unregistered=sensitive-native-input"}} {
		if code := runBudgetCheckWith(append(args, extra...), &stdout, &stderr, budgetCheckFakeCollector(base)); code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "sensitive-native-input") {
			t.Fatal("extra app binding accepted or echoed")
		}
	}
}
