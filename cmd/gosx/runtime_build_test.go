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
	if len(targets) != 5 {
		t.Fatalf("target count = %d", len(targets))
	}
	want := []struct {
		id   string
		file string
		tag  string
	}{
		{id: "core", file: "gosx-runtime-core.wasm", tag: "gosx_runtime_core"},
		{id: "engine", file: "gosx-runtime-engine.wasm", tag: "gosx_runtime_engine"},
		{id: "collab", file: "gosx-runtime-collab.wasm", tag: "gosx_runtime_collab"},
		{id: "full", file: "gosx-runtime.wasm", tag: "gosx_runtime_full"},
		{id: "islands", file: "gosx-runtime-islands.wasm", tag: "gosx_tiny_islands_only"},
	}
	for i, want := range want {
		if targets[i].id != want.id || targets[i].file != want.file {
			t.Fatalf("target %d = %#v, want id=%q file=%q", i, targets[i], want.id, want.file)
		}
		if !stringSliceContains(targets[i].tags, want.tag) {
			t.Fatalf("target %q tags = %#v", want.id, targets[i].tags)
		}
	}
}

func TestRuntimeTargetsCarryCapabilityRoutes(t *testing.T) {
	targets := runtimeBuildTargets()
	for _, target := range targets[:4] {
		if len(target.selectedRoutes) == 0 && target.id != "full" {
			t.Fatalf("variant %q has no selected routes", target.id)
		}
	}
	if len(targets[0].selectedRoutes) != 6 || targets[0].selectedRoutes[0] != "R01" {
		t.Fatalf("core selected routes = %#v", targets[0].selectedRoutes)
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
		id:             "full",
		file:           "gosx-runtime.wasm",
		tags:           []string{"gosx_runtime_full"},
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
	if variant.Generation != "current" || variant.Variant != "full" || variant.FeatureMask == 0 {
		t.Fatalf("capability metadata = %#v", variant)
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
	targets := runtimeBuildTargets()
	got := make([]string, 0, 4)
	for _, target := range targets[:4] {
		got = append(got, target.id)
	}
	want := []string{"core", "engine", "collab", "full"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reserved ids = %#v, want %#v", got, want)
		}
	}
}
