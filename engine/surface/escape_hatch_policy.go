// Escape-hatch policy enforces ADR 0006: //gosx:engine surface=wasm
// annotations require a documented capability gap (criterion 1), trigger
// a build warning (criterion 3) that promotes to error past 90 days
// (criterion 4), and are forbidden in Studio paths (criterion 5).
//
// The policy reads the gap ledger spec from the hyphae knowledge tree
// (default $HOME/.hyphae/spaces/m31labs-gosx/specs/gosx-vm-capability-gaps.md;
// override with GOSX_VM_CAPABILITY_GAPS_FILE).

package surface

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// studioPathDenies declares the path prefixes that cannot use the
// surface=wasm escape hatch per ADR 0006 §5. A surface whose
// repo-relative path begins with one of these (or matches the glob
// suffix `*/admin/editor/`) fails the build with an explicit error.
//
// The values are normalized to forward slashes during matching so they
// work regardless of host OS.
var studioPathDenies = []string{
	"gosx-studio/",
	"gosx-cms/",
	"gosx-admin/",
	"*/admin/editor/",
}

// escapeHatchReviewWindow is the periodic-review window from ADR 0006 §4.
// An entry whose last_reviewed is older than this promotes the build
// warning to an error.
const escapeHatchReviewWindow = 90 * 24 * time.Hour

// capabilityGapEntry is one parsed entry from the gosx-vm-capability-gaps
// spec. Fields mirror the schema documented in that file.
type capabilityGapEntry struct {
	ID                          string
	SurfacePath                 string
	SurfaceLine                 int
	FailingHandlerOrExpression  string
	MissingOpcodeOrCapability   string
	EstimatedWork               string
	WorkaroundAttempted         string
	LastReviewed                time.Time
	Status                      string
	ClosedBy                    string
}

// capabilityGapLedger is the parsed gap-file contents, indexed by
// (surfacePath, surfaceLine). Lookup is O(1) per escape-hatch annotation.
type capabilityGapLedger struct {
	entries     map[gapKey]capabilityGapEntry
	sourcePath  string
	parseErrors []error
}

type gapKey struct {
	surfacePath string
	surfaceLine int
}

// errEscapeHatchUndocumented is returned by checkEscapeHatch when a
// surface=wasm annotation has no matching gap-file entry. Per ADR 0006
// §1 this is a hard build failure.
var errEscapeHatchUndocumented = errors.New("gosx/surface: surface=wasm annotation has no matching entry in gosx-vm-capability-gaps.md (ADR 0006 §1)")

// errEscapeHatchStudioPath is returned when a surface=wasm annotation
// is used inside a Studio path per the deny list. Per ADR 0006 §5 this
// is a hard build failure.
var errEscapeHatchStudioPath = errors.New("gosx/surface: surface=wasm forbidden in Studio paths (gosx-studio/, gosx-cms/, gosx-admin/, */admin/editor/) per ADR 0006 §5")

// errEscapeHatchGapStale is returned when the matching gap entry's
// last_reviewed field is more than 90 days old. Per ADR 0006 §4 this
// promotes the warning to an error.
var errEscapeHatchGapStale = errors.New("gosx/surface: gap entry last_reviewed > 90 days; re-review and update before continuing to use surface=wasm (ADR 0006 §4)")

// gapFilePath returns the resolved path to the capability-gap spec.
// The env var GOSX_VM_CAPABILITY_GAPS_FILE overrides; otherwise the
// default location is $HOME/.hyphae/spaces/m31labs-gosx/specs/gosx-vm-capability-gaps.md.
func gapFilePath() string {
	if override := strings.TrimSpace(os.Getenv("GOSX_VM_CAPABILITY_GAPS_FILE")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".hyphae", "spaces", "m31labs-gosx", "specs", "gosx-vm-capability-gaps.md")
}

// loadCapabilityGapLedger parses the gap spec at path. Missing files
// produce an empty ledger (no entries) rather than an error — the
// effect is that any surface=wasm annotation fails the "must have an
// entry" check. Parse errors are collected but do not cause a hard
// failure; instead they surface alongside lookup misses.
func loadCapabilityGapLedger(path string) *capabilityGapLedger {
	ledger := &capabilityGapLedger{
		entries:    map[gapKey]capabilityGapEntry{},
		sourcePath: path,
	}
	if path == "" {
		return ledger
	}
	f, err := os.Open(path)
	if err != nil {
		// Missing file -> empty ledger. Lookup misses will produce
		// the "undocumented" error which is the right surface.
		return ledger
	}
	defer f.Close()
	parseGapLedger(f, ledger)
	return ledger
}

// parseGapLedger reads markdown from r and populates ledger.entries.
// Entries are level-3 headings (`### gap-...`) followed by a fenced
// code block containing `key: value` lines.
func parseGapLedger(r io.Reader, ledger *capabilityGapLedger) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var current *capabilityGapEntry
	inFence := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "### ") {
			// Close any in-progress entry.
			finalizeGapEntry(ledger, current)
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			// Strip markdown strikethrough syntax (~~heading~~) for closed entries.
			heading = strings.TrimPrefix(strings.TrimSuffix(heading, "~~"), "~~")
			current = &capabilityGapEntry{ID: heading}
			inFence = false
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if current == nil || !inFence {
			continue
		}
		applyGapField(current, trimmed, lineNumber, ledger)
	}
	finalizeGapEntry(ledger, current)
}

// applyGapField parses one `key: value` line from inside a gap entry's
// fenced block.
func applyGapField(entry *capabilityGapEntry, line string, lineNumber int, ledger *capabilityGapLedger) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return
	}
	key := strings.TrimSpace(line[:colon])
	value := strings.TrimSpace(line[colon+1:])
	switch key {
	case "id":
		entry.ID = value
	case "surface_path":
		entry.SurfacePath = normalizePath(value)
	case "surface_line":
		if n, err := strconv.Atoi(value); err == nil {
			entry.SurfaceLine = n
		}
	case "failing_handler_or_expression":
		entry.FailingHandlerOrExpression = value
	case "missing_opcode_or_capability":
		entry.MissingOpcodeOrCapability = value
	case "estimated_work":
		entry.EstimatedWork = value
	case "workaround_attempted":
		entry.WorkaroundAttempted = value
	case "last_reviewed":
		if t, err := time.Parse("2006-01-02", value); err == nil {
			entry.LastReviewed = t
		} else {
			ledger.parseErrors = append(ledger.parseErrors, fmt.Errorf("gap-file line %d: invalid last_reviewed %q (want YYYY-MM-DD)", lineNumber, value))
		}
	case "status":
		entry.Status = value
	case "closed_by":
		entry.ClosedBy = value
	}
}

// finalizeGapEntry indexes a fully-parsed entry into the ledger. Entries
// without a surface_path are skipped (placeholder/template entries).
func finalizeGapEntry(ledger *capabilityGapLedger, entry *capabilityGapEntry) {
	if entry == nil {
		return
	}
	if entry.SurfacePath == "" {
		return
	}
	// Skip example/template entries by path convention.
	if strings.HasPrefix(entry.SurfacePath, "example/") {
		return
	}
	key := gapKey{surfacePath: entry.SurfacePath, surfaceLine: entry.SurfaceLine}
	ledger.entries[key] = *entry
}

// checkEscapeHatch enforces ADR 0006 for a single surface=wasm
// annotation. It returns:
//   - nil and a non-empty warning when the annotation is allowed (criterion 3 warning).
//   - nil and "" when the annotation is allowed and silent (not currently used; reserved).
//   - an error when the annotation must fail the build per criteria 1, 4, or 5.
//
// surfacePath is the repo-relative path of the .gsx file (or the
// project-rooted path the build sees). surfaceLine is the source line
// of the annotation. ledger is the parsed gap spec.
func checkEscapeHatch(surfacePath string, surfaceLine int, ledger *capabilityGapLedger, now time.Time) (warning string, err error) {
	normalized := normalizePath(surfacePath)
	if pathInStudioDeny(normalized) {
		return "", fmt.Errorf("%w: surface=%s line=%d", errEscapeHatchStudioPath, normalized, surfaceLine)
	}
	if ledger == nil || len(ledger.entries) == 0 {
		return "", fmt.Errorf("%w: surface=%s line=%d (gap-file %s missing or empty)", errEscapeHatchUndocumented, normalized, surfaceLine, ledger.sourcePathOrUnknown())
	}
	entry, ok := ledger.entries[gapKey{surfacePath: normalized, surfaceLine: surfaceLine}]
	if !ok {
		return "", fmt.Errorf("%w: surface=%s line=%d (add an entry to %s)", errEscapeHatchUndocumented, normalized, surfaceLine, ledger.sourcePath)
	}
	if entry.Status == "closed" {
		return "", fmt.Errorf("%w: gap entry %q is marked closed (closed_by: %s); remove the surface=wasm annotation", errEscapeHatchUndocumented, entry.ID, entry.ClosedBy)
	}
	if !entry.LastReviewed.IsZero() && now.Sub(entry.LastReviewed) > escapeHatchReviewWindow {
		days := int(now.Sub(entry.LastReviewed) / (24 * time.Hour))
		return "", fmt.Errorf("%w: gap %q last_reviewed=%s (%d days ago)", errEscapeHatchGapStale, entry.ID, entry.LastReviewed.Format("2006-01-02"), days)
	}
	// Criterion 3: every valid escape-hatch surface emits a build warning.
	reviewBy := entry.LastReviewed.Add(escapeHatchReviewWindow)
	return fmt.Sprintf(
		"gosx/surface: %q uses the WASM backend escape hatch (ADR 0006)\n  gap: %s\n  status: review by %s",
		surfacePath,
		entry.ID,
		reviewBy.Format("2006-01-02"),
	), nil
}

// pathInStudioDeny returns true if path matches any entry in
// studioPathDenies. Path is expected to be normalized (forward slashes).
func pathInStudioDeny(path string) bool {
	for _, deny := range studioPathDenies {
		if matchDenyEntry(deny, path) {
			return true
		}
	}
	return false
}

// matchDenyEntry implements the deny-list matching rules:
//   - exact prefix match for entries without a leading `*/`
//   - "*/admin/editor/" matches any path containing the substring
//     "/admin/editor/" or starting with "admin/editor/"
//
// The matcher is intentionally simple — full glob is overkill for the
// small known deny set.
func matchDenyEntry(deny, path string) bool {
	if strings.HasPrefix(deny, "*/") {
		needle := strings.TrimPrefix(deny, "*/")
		return strings.Contains(path, "/"+needle) || strings.HasPrefix(path, needle)
	}
	return strings.HasPrefix(path, deny)
}

// normalizePath converts the path to forward slashes and trims any
// leading "./".
func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "./")
	return path
}

// sourcePathOrUnknown returns a non-empty descriptor for the ledger's
// source for use in error messages.
func (l *capabilityGapLedger) sourcePathOrUnknown() string {
	if l == nil || l.sourcePath == "" {
		return "(no GOSX_VM_CAPABILITY_GAPS_FILE configured and no $HOME/.hyphae spec)"
	}
	return l.sourcePath
}

// emitEscapeHatchWarning writes warning to stderr unsuppressibly per
// ADR 0006 §3.
func emitEscapeHatchWarning(warning string) {
	if warning == "" {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, warning)
}
