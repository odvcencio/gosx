package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/ouroboros"
)

func TestRuntimeBuildTargetsRecordCurrentVariants(t *testing.T) {
	targets := runtimeBuildTargets()
	if len(targets) != 2 {
		t.Fatalf("target count = %d", len(targets))
	}
	if targets[0].id != "runtime" || targets[0].file != "gosx-runtime.wasm" {
		t.Fatalf("unexpected full target: %#v", targets[0])
	}
	if targets[1].id != "islands" || targets[1].file != "gosx-runtime-islands.wasm" {
		t.Fatalf("unexpected islands target: %#v", targets[1])
	}
	if !stringSliceContains(targets[1].tags, "gosx_tiny_islands_only") {
		t.Fatalf("islands target tags = %#v", targets[1].tags)
	}
}

func TestPlannedRuntimeVariantIsExplicitlyMissing(t *testing.T) {
	variant := plannedRuntimeVariant("core", []string{"R01"})
	if variant.Status != "planned" || variant.MissingReason == "" {
		t.Fatalf("unexpected planned variant: %#v", variant)
	}
	if variant.Generation != "future" {
		t.Fatalf("planned generation = %q", variant.Generation)
	}
	if variant.SizeBytes != nil || variant.BudgetBytes != nil {
		t.Fatalf("planned size/budget must stay null: %#v", variant)
	}
	if len(variant.PlannedSelectedBy) != 1 || variant.PlannedSelectedBy[0] != "R01" {
		t.Fatalf("unexpected planned routes: %#v", variant.PlannedSelectedBy)
	}
}

func TestRunBuildRuntimePreflightsExistingEvidenceBeforeOutputMutation(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	evidencePath := filepath.Join(artifactRoot, "wasm", "runtime-artifacts.json")
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, []byte(`{"stale":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "missing-inventory.json"),
		RepoRoot:      ".",
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite existing evidence") {
		t.Fatalf("RunBuildRuntimeWithOptions error = %v, want stale evidence refusal", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("runtime output was mutated before preflight failure: %v", statErr)
	}
}

func TestRunBuildRuntimePreflightsExistingOutputBeforeArtifactMutation(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "missing-inventory.json"),
		RepoRoot:      ".",
	})
	if err == nil || !strings.Contains(err.Error(), "canonical runtime output already exists") {
		t.Fatalf("RunBuildRuntimeWithOptions error = %v, want output preflight refusal", err)
	}
	if _, statErr := os.Stat(artifactRoot); !os.IsNotExist(statErr) {
		t.Fatalf("artifact root was mutated before output preflight failure: %v", statErr)
	}
}

func TestCurrentRuntimeVariantBindsPublishedPath(t *testing.T) {
	target := runtimeBuildTarget{
		id:             "runtime",
		file:           "gosx-runtime.wasm",
		selectedRoutes: []string{"R05"},
	}
	tmpPath := filepath.Join(t.TempDir(), ".runtime.ouroboros-123", target.file)
	publishedPath := filepath.Join(t.TempDir(), "runtime-out", target.file)
	metrics := ouroboros.AssetMetrics{
		File:        filepath.Base(tmpPath),
		SourcePath:  tmpPath,
		SHA256:      strings.Repeat("a", 64),
		Bytes:       10,
		GzipBytes:   11,
		BrotliBytes: 12,
	}
	evidence := &ouroboros.RuntimeBuildEvidence{
		TinyGo:    ouroboros.ToolStatus{Version: "tinygo version"},
		GoVersion: ouroboros.ToolStatus{Version: "go version"},
		WasmOpt:   ouroboros.ToolStatus{Version: "wasm-opt version", Available: true},
	}
	variant := currentRuntimeVariant(target, metrics, metrics.Bytes, publishedPath, evidence, true, nil)
	if variant.Status != "measured" {
		t.Fatalf("Status = %q, want measured", variant.Status)
	}
	if variant.SourcePath != publishedPath {
		t.Fatalf("SourcePath = %q, want %q", variant.SourcePath, publishedPath)
	}
	if strings.Join(variant.BuildArgs, " ") == "" || strings.Contains(strings.Join(variant.BuildArgs, " "), tmpPath) {
		t.Fatalf("BuildArgs still reference temp path: %#v", variant.BuildArgs)
	}
	if !strings.Contains(strings.Join(variant.BuildArgs, " "), publishedPath) {
		t.Fatalf("BuildArgs do not reference published path: %#v", variant.BuildArgs)
	}
}

func TestPublishRuntimeBuildOutputRenamesWholeDirectoryAtomically(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tmp-runtime")
	dst := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "gosx-runtime.wasm"), []byte("wasm"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishRuntimeBuildOutput(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "gosx-runtime.wasm")); err != nil {
		t.Fatalf("published runtime missing: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source directory still exists after publish: %v", err)
	}
}

func TestPublishRuntimeBuildOutputRefusesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tmp-runtime")
	dst := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "old.wasm"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	err := publishRuntimeBuildOutput(src, dst)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("publishRuntimeBuildOutput error = %v, want existing destination refusal", err)
	}
	if body, readErr := os.ReadFile(filepath.Join(dst, "old.wasm")); readErr != nil || string(body) != "old" {
		t.Fatalf("existing destination was mutated: body=%q err=%v", body, readErr)
	}
}

func TestReservedRuntimeVariantIDs(t *testing.T) {
	got := []string{
		plannedRuntimeVariant("core", nil).ID,
		plannedRuntimeVariant("engine", nil).ID,
		plannedRuntimeVariant("collab", nil).ID,
		plannedRuntimeVariant("full", nil).ID,
	}
	want := []string{"core", "engine", "collab", "full"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reserved ids = %#v, want %#v", got, want)
		}
	}
}

func TestRuntimeBuildEvidenceUsesExactSixRows(t *testing.T) {
	evidence := &ouroboros.RuntimeBuildEvidence{
		TinyGo:    ouroboros.ToolStatus{Version: "tinygo version"},
		GoVersion: ouroboros.ToolStatus{Version: "go version"},
		WasmOpt:   ouroboros.ToolStatus{Version: "wasm-opt version", Available: true},
	}
	for _, target := range runtimeBuildTargets() {
		sizeBytes := int64(10)
		evidence.Variants = append(evidence.Variants, currentRuntimeVariant(target, ouroboros.AssetMetrics{
			File:        target.file,
			SourcePath:  filepath.Join("runtime", target.file),
			SHA256:      strings.Repeat("a", 64),
			Bytes:       sizeBytes,
			GzipBytes:   11,
			BrotliBytes: 12,
		}, sizeBytes, filepath.Join("published", target.file), evidence, false, nil))
	}
	finishRuntimeContractRows(evidence)
	want := map[string]struct {
		generation string
		status     string
		routes     []string
	}{
		"runtime": {"current", "measured", []string{"R05", "R06", "R07", "R08", "R10"}},
		"islands": {"current", "measured", []string{"R02", "R03"}},
		"core":    {"future", "planned", []string{"R01", "R02", "R03", "R04", "R09A", "R09B"}},
		"engine":  {"future", "planned", []string{"R05", "R07", "R08", "R10"}},
		"collab":  {"future", "planned", []string{"R06"}},
		"full":    {"future", "planned", []string{}},
	}
	if len(evidence.Variants) != len(want) {
		t.Fatalf("variant rows = %d, want %d: %#v", len(evidence.Variants), len(want), evidence.Variants)
	}
	for _, variant := range evidence.Variants {
		expected, ok := want[variant.ID]
		if !ok {
			t.Fatalf("unexpected variant ID %q", variant.ID)
		}
		if variant.Generation != expected.generation || variant.Status != expected.status {
			t.Fatalf("%s generation/status = %s/%s, want %s/%s", variant.ID, variant.Generation, variant.Status, expected.generation, expected.status)
		}
		if strings.Join(variant.PlannedSelectedBy, ",") != strings.Join(expected.routes, ",") {
			t.Fatalf("%s routes = %#v, want %#v", variant.ID, variant.PlannedSelectedBy, expected.routes)
		}
		delete(want, variant.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing rows: %#v", want)
	}
}

func TestRunBuildRuntimeWritesFailureEvidenceWithoutPublishingPartialOutput(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	runtimeBuildTinyGoWASM = func(projectDir, gosxRoot, outputPath, tinygoPath string, extraTags ...string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, []byte("partial"), 0644); err != nil {
			return err
		}
		return errors.New("tinygo failed after start")
	}
	err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "source-inventory.json"),
		RepoRoot:      ".",
	})
	if err == nil || !strings.Contains(err.Error(), "tinygo failed after start") {
		t.Fatalf("RunBuildRuntimeWithOptions error = %v, want TinyGo failure", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("runtime output was published after failure: %v", statErr)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".runtime-out.ouroboros-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial runtime temp directory was not cleaned: %#v", matches)
	}
	var evidence ouroboros.RuntimeBuildEvidence
	body, err := os.ReadFile(filepath.Join(artifactRoot, "wasm", "runtime-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatal(err)
	}
	rows := map[string]ouroboros.RuntimeArtifactVariant{}
	for _, variant := range evidence.Variants {
		rows[variant.ID] = variant
	}
	if len(rows) != 6 {
		t.Fatalf("failure evidence rows = %d, want 6: %#v", len(rows), evidence.Variants)
	}
	if rows["runtime"].Generation != "current" || rows["runtime"].Status != "failed" || !strings.Contains(rows["runtime"].MissingReason, "tinygo failed after start") {
		t.Fatalf("bad runtime failure row: %#v", rows["runtime"])
	}
	if rows["islands"].Generation != "current" || rows["islands"].Status != "skipped" {
		t.Fatalf("bad islands skipped row: %#v", rows["islands"])
	}
	if rows["core"].Generation != "future" || rows["core"].Status != "planned" {
		t.Fatalf("bad future row: %#v", rows["core"])
	}
}

func stubRuntimeBuildHooks(t *testing.T) func() {
	t.Helper()
	oldBuild := runtimeBuildTinyGoWASM
	oldOptimize := runtimeOptimizeWASMWithWasmOpt
	oldMetrics := runtimeMetricsForFile
	oldShim := runtimeTinyGoShimMetrics
	oldResolve := runtimeResolveWASMCompiler
	oldInput := runtimeBuildInputEvidenceForRepo
	oldSource := runtimeBuildCanonicalSourceIdentity
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, "tinygo", nil }
	runtimeOptimizeWASMWithWasmOpt = func(string) (bool, error) { return false, nil }
	runtimeMetricsForFile = ouroboros.MetricsForFile
	runtimeTinyGoShimMetrics = func(string) ouroboros.AssetMetrics {
		return ouroboros.AssetMetrics{File: "wasm_exec.js", SourcePath: "tinygo/targets/wasm_exec.js", SHA256: strings.Repeat("b", 64), Bytes: 1}
	}
	runtimeBuildInputEvidenceForRepo = func(repoRoot, manifestPath, exportPath string) (ouroboros.BuildInputEvidence, error) {
		return ouroboros.BuildInputEvidence{GoSXModuleDir: repoRoot, RejectsModuleCacheMismatch: true}, nil
	}
	runtimeBuildCanonicalSourceIdentity = func(ctx context.Context, repoRoot, inventoryPath, artifactRoot string) (ouroboros.SourceIdentity, error) {
		return strictTestSourceIdentity(), nil
	}
	return func() {
		runtimeBuildTinyGoWASM = oldBuild
		runtimeOptimizeWASMWithWasmOpt = oldOptimize
		runtimeMetricsForFile = oldMetrics
		runtimeTinyGoShimMetrics = oldShim
		runtimeResolveWASMCompiler = oldResolve
		runtimeBuildInputEvidenceForRepo = oldInput
		runtimeBuildCanonicalSourceIdentity = oldSource
	}
}

func strictTestSourceIdentity() ouroboros.SourceIdentity {
	return ouroboros.SourceIdentity{
		BaseRevision:               "test",
		OverlayHash:                "sha256:test",
		InventoryRef:               "source-inventory.json",
		InventorySHA256:            "sha256:inventory",
		RejectsModuleCacheMismatch: true,
		CurrentOverlayVerified:     true,
		StrictInventory:            true,
		ReconstructionProof:        true,
		RuntimeJSONStatic: &ouroboros.RuntimeJSONStaticIdentity{
			SchemaVersion:      "gosx.ouroboros.runtime-json-probe.v1",
			ScannerVersion:     "test",
			QueryID:            "test",
			PhaseClassifier:    "test",
			SourceIdentityHash: "sha256:source",
			SemanticHash:       "sha256:semantic",
			CountsHash:         "sha256:counts",
			GlobalNameHash:     "sha256:globals",
			Validated:          true,
		},
		CompatibilityAudit: &ouroboros.CompatibilityAuditIdentity{
			SchemaVersion:                 "gosx.ouroboros.compatibility-audit.v1",
			Status:                        "pass",
			CanonicalAvailable:            true,
			RuntimeJSONSourceIdentityHash: "sha256:source",
			RuntimeJSONSemanticHash:       "sha256:semantic",
			RuntimeJSONCountsHash:         "sha256:counts",
			RuntimeJSONGlobalNameHash:     "sha256:globals",
		},
	}
}
