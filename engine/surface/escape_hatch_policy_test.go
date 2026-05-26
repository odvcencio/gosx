package surface

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// validGapMD is a minimal gap-file fixture used by most tests below.
// It contains one active entry whose key matches the test's surface
// path/line.
const validGapMD = "" +
	"# Gap ledger\n\n" +
	"### gap-test-active\n\n" +
	"```\n" +
	"id: gap-test-active\n" +
	"surface_path: cmd/hypha-viz/graphsurface/graph.gsx\n" +
	"surface_line: 1\n" +
	"failing_handler_or_expression: OnFrame\n" +
	"missing_opcode_or_capability: no goroutine spawn opcode\n" +
	"estimated_work: 2 weeks\n" +
	"workaround_attempted: tried iterative bytecode, ran out of time budget\n" +
	"last_reviewed: 2026-05-01\n" +
	"status: active\n" +
	"```\n\n" +
	"### gap-test-stale\n\n" +
	"```\n" +
	"id: gap-test-stale\n" +
	"surface_path: third_party/legacy/old.gsx\n" +
	"surface_line: 2\n" +
	"missing_opcode_or_capability: legacy capability\n" +
	"last_reviewed: 2025-01-01\n" +
	"status: active\n" +
	"```\n\n" +
	"### gap-test-closed\n\n" +
	"```\n" +
	"id: gap-test-closed\n" +
	"surface_path: closed/example.gsx\n" +
	"surface_line: 3\n" +
	"missing_opcode_or_capability: closed gap\n" +
	"last_reviewed: 2026-05-01\n" +
	"status: closed\n" +
	"closed_by: ADR 0009\n" +
	"```\n"

// writeGapFile writes content to a temp file and returns its path.
func writeGapFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gaps.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write gap file: %v", err)
	}
	return path
}

func TestLoadCapabilityGapLedgerParsesEntries(t *testing.T) {
	path := writeGapFile(t, validGapMD)
	ledger := loadCapabilityGapLedger(path)
	if got := len(ledger.entries); got != 3 {
		t.Fatalf("expected 3 entries, got %d", got)
	}
	entry, ok := ledger.entries[gapKey{surfacePath: "cmd/hypha-viz/graphsurface/graph.gsx", surfaceLine: 1}]
	if !ok {
		t.Fatal("active entry not indexed")
	}
	if entry.ID != "gap-test-active" {
		t.Errorf("entry.ID = %q, want gap-test-active", entry.ID)
	}
	if entry.Status != "active" {
		t.Errorf("entry.Status = %q, want active", entry.Status)
	}
	expectedDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !entry.LastReviewed.Equal(expectedDate) {
		t.Errorf("entry.LastReviewed = %v, want %v", entry.LastReviewed, expectedDate)
	}
}

func TestLoadCapabilityGapLedgerMissingFileIsEmptyLedger(t *testing.T) {
	ledger := loadCapabilityGapLedger(filepath.Join(t.TempDir(), "does-not-exist.md"))
	if got := len(ledger.entries); got != 0 {
		t.Errorf("missing gap file should produce empty ledger, got %d entries", got)
	}
}

func TestLoadCapabilityGapLedgerSkipsTemplate(t *testing.T) {
	const md = "" +
		"### gap-template\n\n" +
		"```\n" +
		"id: gap-template\n" +
		"surface_path: example/path/to/surface.gsx\n" +
		"surface_line: 1\n" +
		"status: deferred\n" +
		"```\n"
	path := writeGapFile(t, md)
	ledger := loadCapabilityGapLedger(path)
	if got := len(ledger.entries); got != 0 {
		t.Errorf("template entry should be skipped, got %d entries", got)
	}
}

func TestCheckEscapeHatchAllowsDocumentedEntry(t *testing.T) {
	path := writeGapFile(t, validGapMD)
	ledger := loadCapabilityGapLedger(path)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	warning, err := checkEscapeHatch("cmd/hypha-viz/graphsurface/graph.gsx", 1, ledger, now)
	if err != nil {
		t.Fatalf("expected nil error for documented gap, got %v", err)
	}
	if !strings.Contains(warning, "gosx/surface:") {
		t.Errorf("warning missing prefix: %q", warning)
	}
	if !strings.Contains(warning, "gap-test-active") {
		t.Errorf("warning missing gap id: %q", warning)
	}
	if !strings.Contains(warning, "review by 2026-07-30") {
		t.Errorf("warning missing review-by date: %q", warning)
	}
}

func TestCheckEscapeHatchRejectsUndocumented(t *testing.T) {
	path := writeGapFile(t, validGapMD)
	ledger := loadCapabilityGapLedger(path)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	_, err := checkEscapeHatch("plugins/whatever/never-documented.gsx", 12, ledger, now)
	if err == nil {
		t.Fatal("expected error for undocumented surface=wasm, got nil")
	}
	if !errors.Is(err, errEscapeHatchUndocumented) {
		t.Errorf("expected errEscapeHatchUndocumented, got %v", err)
	}
}

func TestCheckEscapeHatchRejectsEmptyLedger(t *testing.T) {
	emptyLedger := loadCapabilityGapLedger(filepath.Join(t.TempDir(), "missing.md"))
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	_, err := checkEscapeHatch("anywhere/legit.gsx", 1, emptyLedger, now)
	if err == nil {
		t.Fatal("expected error when ledger empty, got nil")
	}
	if !errors.Is(err, errEscapeHatchUndocumented) {
		t.Errorf("expected errEscapeHatchUndocumented, got %v", err)
	}
}

func TestCheckEscapeHatchRejectsStaleEntry(t *testing.T) {
	path := writeGapFile(t, validGapMD)
	ledger := loadCapabilityGapLedger(path)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	_, err := checkEscapeHatch("third_party/legacy/old.gsx", 2, ledger, now)
	if err == nil {
		t.Fatal("expected error for stale (>90d) gap entry, got nil")
	}
	if !errors.Is(err, errEscapeHatchGapStale) {
		t.Errorf("expected errEscapeHatchGapStale, got %v", err)
	}
}

func TestCheckEscapeHatchRejectsClosedEntry(t *testing.T) {
	path := writeGapFile(t, validGapMD)
	ledger := loadCapabilityGapLedger(path)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	_, err := checkEscapeHatch("closed/example.gsx", 3, ledger, now)
	if err == nil {
		t.Fatal("expected error for closed gap entry, got nil")
	}
	if !errors.Is(err, errEscapeHatchUndocumented) {
		t.Errorf("expected errEscapeHatchUndocumented for closed entry, got %v", err)
	}
}

func TestCheckEscapeHatchRejectsStudioPath(t *testing.T) {
	path := writeGapFile(t, validGapMD)
	ledger := loadCapabilityGapLedger(path)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	deniedPaths := []string{
		"gosx-studio/some/surface.gsx",
		"gosx-cms/foo/bar.gsx",
		"gosx-admin/x/y.gsx",
		"muddy-noni-commerce/app/admin/editor/widget.gsx",
	}
	for _, p := range deniedPaths {
		_, err := checkEscapeHatch(p, 1, ledger, now)
		if err == nil {
			t.Errorf("path %q should be denied, got nil error", p)
			continue
		}
		if !errors.Is(err, errEscapeHatchStudioPath) {
			t.Errorf("path %q expected errEscapeHatchStudioPath, got %v", p, err)
		}
	}
}

func TestStudioPathDenyMatcherNonDeniedPathsPass(t *testing.T) {
	allowed := []string{
		"cmd/hypha-viz/graphsurface/graph.gsx",
		"examples/spinning-cube.gsx",
		"third_party/marketplace/widget.gsx",
		"docs/tutorial.gsx",
	}
	for _, p := range allowed {
		if pathInStudioDeny(p) {
			t.Errorf("path %q should not be denied", p)
		}
	}
}

func TestNormalizePathFoldsBackslashesAndDotSlash(t *testing.T) {
	tests := map[string]string{
		`cmd\hypha-viz\foo.gsx`:    "cmd/hypha-viz/foo.gsx",
		"./relative/path.gsx":      "relative/path.gsx",
		"  /trim/space.gsx  ":      "/trim/space.gsx",
		"already/forward/slash":    "already/forward/slash",
	}
	for in, want := range tests {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGapFilePathHonorsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.md")
	t.Setenv("GOSX_VM_CAPABILITY_GAPS_FILE", override)
	if got := gapFilePath(); got != override {
		t.Errorf("gapFilePath() = %q, want %q", got, override)
	}
}
