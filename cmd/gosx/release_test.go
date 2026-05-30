package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/version"
)

func TestRunReleaseCheckCommandPassesCurrentRepo(t *testing.T) {
	var out bytes.Buffer
	if err := runReleaseCommand([]string{"check", "--json", "--root", "../.."}, &out); err != nil {
		t.Fatal(err)
	}
	var report releaseCheckReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if !report.OK || report.Version != version.Current {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestCheckCITagIgnoresBranchPushes(t *testing.T) {
	// Branch/PR push: GITHUB_REF_NAME is the branch ("main"), not a release
	// tag. The check must skip, not fail.
	t.Setenv("GITHUB_REF_TYPE", "branch")
	t.Setenv("GITHUB_REF_NAME", "main")
	t.Setenv("GITHUB_REF", "refs/heads/main")
	t.Setenv("CI_COMMIT_TAG", "")
	if got := checkCITag(version.Current); got.Status != "skip" {
		t.Fatalf("branch push must skip ci.tag, got %+v", got)
	}
}

func TestCheckCITagValidatesTagPushes(t *testing.T) {
	t.Setenv("GITHUB_REF_TYPE", "tag")
	t.Setenv("GITHUB_REF_NAME", version.Current)
	t.Setenv("GITHUB_REF", "refs/tags/"+version.Current)
	t.Setenv("CI_COMMIT_TAG", "")
	if got := checkCITag(version.Current); got.Status != "pass" {
		t.Fatalf("matching tag push must pass ci.tag, got %+v", got)
	}

	t.Setenv("GITHUB_REF_NAME", "v0.0.1")
	t.Setenv("GITHUB_REF", "refs/tags/v0.0.1")
	if got := checkCITag(version.Current); got.Status != "fail" {
		t.Fatalf("mismatched tag push must fail ci.tag, got %+v", got)
	}
}

func TestReleaseCheckDetectsStaleReadme(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "README.md"), "Current release: **v0.18.28**.\n")
	mustWriteFile(t, filepath.Join(dir, "CHANGELOG.md"), "## "+version.Current+"\n")

	report := buildReleaseCheckReport(dir, "")
	if report.OK {
		t.Fatalf("expected stale README to fail: %+v", report)
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "readme.current_release" && check.Status == "fail" && check.Actual == "v0.18.28" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stale README failure: %+v", report.Checks)
	}
}

func TestReleaseCheckNextRequiresChangelogSection(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "README.md"), "Current release: **"+version.Current+"**.\n")
	mustWriteFile(t, filepath.Join(dir, "CHANGELOG.md"), "## "+version.Current+"\n")

	report := buildReleaseCheckReport(dir, "v0.18.31")
	if report.OK {
		t.Fatalf("expected missing next changelog section to fail: %+v", report)
	}
	text := releaseCheckText(report)
	if !strings.Contains(text, "changelog.next: fail") {
		t.Fatalf("expected changelog.next failure in output:\n%s", text)
	}
}

func releaseCheckText(report releaseCheckReport) string {
	var out bytes.Buffer
	printReleaseCheck(&out, report)
	return out.String()
}
