package main

import (
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
