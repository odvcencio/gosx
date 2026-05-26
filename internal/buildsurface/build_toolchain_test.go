package buildsurface

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCompileTinyGoSetsGotoolchainAuto verifies that the TinyGo env construction
// helper sets GOTOOLCHAIN=auto (not "local") so the inner `go build` shelled
// out by TinyGo can satisfy a higher go-directive declared by gosx's module
// when the selected SDK is older. Closes spec §C / defect 3.
func TestCompileTinyGoSetsGotoolchainAuto(t *testing.T) {
	root := "/home/test/sdk/go1.25.9"
	env := tinygoEnvFor(root)

	if got := envValueOf(env, "GOTOOLCHAIN"); got != "auto" {
		t.Errorf("GOTOOLCHAIN = %q, want \"auto\"", got)
	}
	if got := envValueOf(env, "GOROOT"); got != root {
		t.Errorf("GOROOT = %q, want pinned SDK %q", got, root)
	}
	pathPrefix := filepath.Join(root, "bin")
	if got := envValueOf(env, "PATH"); !strings.HasPrefix(got, pathPrefix) {
		t.Errorf("PATH = %q, want prefix %q", got, pathPrefix)
	}
}

// TestCompileTinyGoEnvWithoutRoot leaves GOTOOLCHAIN/GOROOT alone when no
// compatible SDK was found. The default behavior must not regress for hosts
// that don't need a pinned SDK.
func TestCompileTinyGoEnvWithoutRoot(t *testing.T) {
	env := tinygoEnvFor("")
	if got := envValueOf(env, "GOWORK"); got != "off" {
		t.Errorf("GOWORK = %q, want \"off\"", got)
	}
	if got := envValueOf(env, "GOFLAGS"); !strings.Contains(got, "-mod=mod") {
		t.Errorf("GOFLAGS = %q, want to contain -mod=mod", got)
	}
}

// TestParseToolchainMismatchError converts TinyGo's raw stderr into a
// clarifying error that names the target Go version, the running SDK, and
// the override env var. Closes spec §C action item 2.
func TestParseToolchainMismatchError(t *testing.T) {
	stderr := `tinygo build: exit status 1
go: module /home/draco/work/gosx requires go >= 1.26 (running go 1.25.9; GOTOOLCHAIN=local)`
	got := parseTinyGoToolchainError(stderr)
	if got == "" {
		t.Fatalf("expected non-empty rewritten error for matching stderr; got empty")
	}
	if !strings.Contains(got, "Go >= 1.26") && !strings.Contains(got, "go >= 1.26") {
		t.Errorf("rewritten error %q missing target version 1.26", got)
	}
	if !strings.Contains(got, "1.25.9") {
		t.Errorf("rewritten error %q missing observed SDK version 1.25.9", got)
	}
	if !strings.Contains(got, "GOSX_TINYGO_GOROOT") {
		t.Errorf("rewritten error %q missing override env var hint", got)
	}
	if !strings.Contains(got, "GOSX_FORCE_GO_COMPILER") {
		t.Errorf("rewritten error %q missing escape hatch hint", got)
	}
}

// TestParseToolchainMismatchError_NoMatchReturnsEmpty verifies the parser
// returns "" when the stderr does not contain a recognizable mismatch
// pattern — letting the caller surface the raw error unchanged.
func TestParseToolchainMismatchError_NoMatchReturnsEmpty(t *testing.T) {
	got := parseTinyGoToolchainError("some other tinygo error")
	if got != "" {
		t.Errorf("non-mismatch input should return \"\", got %q", got)
	}
}
