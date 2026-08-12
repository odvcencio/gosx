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

func TestCanonicalRuntimeBuildRejectsEvidenceSourceBuildRootMismatch(t *testing.T) {
	restore := saveRuntimeBuildHooks()
	defer restore()

	repoRoot := t.TempDir()
	otherRoot := t.TempDir()
	runtimeResolveGoSXModuleRoot = func(projectDir string) (string, error) {
		if projectDir != repoRoot {
			t.Fatalf("module resolution projectDir = %q, want canonical root %q", projectDir, repoRoot)
		}
		return otherRoot, nil
	}
	runtimeEnsureWASMRuntimeDeps = func(string) error {
		t.Fatal("dependency resolution ran after root mismatch")
		return nil
	}

	err := RunBuildRuntimeWithOptions(filepath.Join(t.TempDir(), "runtime"), buildRuntimeOptions{
		OuroborosOut:  filepath.Join(t.TempDir(), "evidence"),
		InventoryPath: filepath.Join(repoRoot, "inventory.json"),
		RepoRoot:      repoRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "canonical runtime root mismatch") {
		t.Fatalf("root mismatch error = %v", err)
	}
}

func TestRuntimeBuildTargetsAreFourProfilesPlusIslandsCompatibility(t *testing.T) {
	targets := runtimeBuildTargets()
	if len(targets) != 5 {
		t.Fatalf("runtime target count = %d, want 5", len(targets))
	}
	want := []struct {
		id            string
		file          string
		compatibility bool
	}{
		{"core", "gosx-runtime-core.wasm", false},
		{"engine", "gosx-runtime-engine.wasm", false},
		{"collab", "gosx-runtime-collab.wasm", false},
		{"full", "gosx-runtime.wasm", false},
		{"islands", "gosx-runtime-islands.wasm", true},
	}
	for i, expected := range want {
		got := targets[i]
		if got.id != expected.id || got.file != expected.file || got.compatibility != expected.compatibility {
			t.Fatalf("target[%d] = %+v, want %+v", i, got, expected)
		}
		if len(got.tags) == 0 {
			t.Fatalf("target %s has no explicit build profile", got.id)
		}
	}
}

func TestRuntimeBuildFailureRowsAllocateEmptyRouteReceipts(t *testing.T) {
	evidence := newRuntimeBuildEvidence("/host/output", true)
	targets := runtimeBuildTargets()
	recordRuntimeBuildFailure(evidence, targets, 0, targets[0], errors.New("boom"), true)
	if len(evidence.Variants) != len(targets) {
		t.Fatalf("failure rows = %d, want %d", len(evidence.Variants), len(targets))
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"selectedByRoutes":null`) {
		t.Fatalf("failure evidence contains null route receipt: %s", data)
	}
	for _, variant := range evidence.Variants {
		if variant.PlannedSelectedBy == nil || len(variant.PlannedSelectedBy) != 0 {
			t.Fatalf("variant %s route receipt = %#v", variant.ID, variant.PlannedSelectedBy)
		}
	}
}

func TestCanonicalRuntimeBuildRollsBackOutputWhenReceiptWriteFails(t *testing.T) {
	restore := saveRuntimeBuildHooks()
	defer restore()

	repoRoot := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "runtime")
	evidenceRoot := filepath.Join(t.TempDir(), "evidence")
	stubSuccessfulRuntimeBuild(t, repoRoot)
	runtimeWriteBuildEvidence = func(string, *ouroboros.RuntimeBuildEvidence) error {
		return errors.New("receipt disk full")
	}

	err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  evidenceRoot,
		InventoryPath: filepath.Join(repoRoot, "inventory.json"),
		RepoRoot:      repoRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "published output rolled back") {
		t.Fatalf("receipt failure error = %v", err)
	}
	if _, statErr := os.Lstat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("runtime output survived failed receipt publication: %v", statErr)
	}
}

func TestCanonicalRuntimeBuildWritesPortableMeasuredEvidence(t *testing.T) {
	restore := saveRuntimeBuildHooks()
	defer restore()

	repoRoot := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "runtime")
	evidenceRoot := filepath.Join(t.TempDir(), "evidence")
	stubSuccessfulRuntimeBuild(t, repoRoot)

	var written *ouroboros.RuntimeBuildEvidence
	runtimeWriteBuildEvidence = func(_ string, evidence *ouroboros.RuntimeBuildEvidence) error {
		data, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		written = new(ouroboros.RuntimeBuildEvidence)
		return json.Unmarshal(data, written)
	}

	if err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  evidenceRoot,
		InventoryPath: filepath.Join(repoRoot, "inventory.json"),
		RepoRoot:      repoRoot,
	}); err != nil {
		t.Fatal(err)
	}
	if written == nil || written.OutputDir != "." || written.BuildInput.GoSXModuleDir != "." {
		t.Fatalf("portable evidence identity = %+v", written)
	}
	if len(written.Variants) != 5 {
		t.Fatalf("measured variants = %d, want 5", len(written.Variants))
	}
	data, err := json.Marshal(written)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), repoRoot) || strings.Contains(string(data), filepath.Dir(outDir)) {
		t.Fatalf("canonical evidence leaked a host path: %s", data)
	}
	for _, variant := range written.Variants {
		if variant.Status != "measured" || variant.SizeBytes == nil || variant.Variant == "" || variant.FeatureMask == 0 {
			t.Fatalf("variant is not measured and contract-bound: %+v", variant)
		}
		if variant.PlannedSelectedBy == nil || len(variant.PlannedSelectedBy) != 0 {
			t.Fatalf("variant %s route receipt = %#v", variant.ID, variant.PlannedSelectedBy)
		}
	}
}

type runtimeBuildHookSnapshot struct {
	build        func(string, string, string, string, ...string) error
	optimize     func(string) (bool, error)
	metrics      func(string) (ouroboros.AssetMetrics, error)
	stage        func(string, string) (runtimeShimPublication, error)
	resolveRoot  func(string) (string, error)
	deps         func(string) error
	compiler     func() (wasmCompiler, string, error)
	buildInput   func(string, string, string) (ouroboros.BuildInputEvidence, error)
	source       func(context.Context, string, string, string) (ouroboros.SourceIdentity, error)
	writeReceipt func(string, *ouroboros.RuntimeBuildEvidence) error
}

func saveRuntimeBuildHooks() func() {
	snapshot := runtimeBuildHookSnapshot{
		build:        runtimeBuildTinyGoWASM,
		optimize:     runtimeOptimizeWASMWithWasmOpt,
		metrics:      runtimeMetricsForFile,
		stage:        runtimeStageTinyGoWASMExec,
		resolveRoot:  runtimeResolveGoSXModuleRoot,
		deps:         runtimeEnsureWASMRuntimeDeps,
		compiler:     runtimeResolveWASMCompiler,
		buildInput:   runtimeBuildInputEvidenceForRepo,
		source:       runtimeBuildCanonicalSourceIdentity,
		writeReceipt: runtimeWriteBuildEvidence,
	}
	return func() {
		runtimeBuildTinyGoWASM = snapshot.build
		runtimeOptimizeWASMWithWasmOpt = snapshot.optimize
		runtimeMetricsForFile = snapshot.metrics
		runtimeStageTinyGoWASMExec = snapshot.stage
		runtimeResolveGoSXModuleRoot = snapshot.resolveRoot
		runtimeEnsureWASMRuntimeDeps = snapshot.deps
		runtimeResolveWASMCompiler = snapshot.compiler
		runtimeBuildInputEvidenceForRepo = snapshot.buildInput
		runtimeBuildCanonicalSourceIdentity = snapshot.source
		runtimeWriteBuildEvidence = snapshot.writeReceipt
	}
}

func stubSuccessfulRuntimeBuild(t *testing.T, repoRoot string) {
	t.Helper()
	runtimeResolveGoSXModuleRoot = func(projectDir string) (string, error) { return projectDir, nil }
	runtimeEnsureWASMRuntimeDeps = func(string) error { return nil }
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, "tinygo-fixture", nil }
	runtimeBuildInputEvidenceForRepo = func(root, _, _ string) (ouroboros.BuildInputEvidence, error) {
		if root != repoRoot {
			t.Fatalf("build input root = %q, want %q", root, repoRoot)
		}
		return ouroboros.BuildInputEvidence{GoSXModuleDir: root, GoWork: filepath.Join(root, "go.work"), GoModSHA256: strings.Repeat("b", 64)}, nil
	}
	runtimeBuildCanonicalSourceIdentity = func(_ context.Context, root, _, _ string) (ouroboros.SourceIdentity, error) {
		if root != repoRoot {
			t.Fatalf("source root = %q, want %q", root, repoRoot)
		}
		return ouroboros.SourceIdentity{BaseRevision: strings.Repeat("c", 40), OverlayHash: strings.Repeat("d", 64)}, nil
	}
	runtimeStageTinyGoWASMExec = func(_ string, buildOutDir string) (runtimeShimPublication, error) {
		path := filepath.Join(buildOutDir, "wasm_exec.js")
		if err := os.WriteFile(path, []byte("shim"), 0o644); err != nil {
			return runtimeShimPublication{}, err
		}
		metrics, err := ouroboros.MetricsForFile(path)
		return runtimeShimPublication{Source: metrics, Output: metrics}, err
	}
	runtimeBuildTinyGoWASM = func(_, _, outputPath, _ string, _ ...string) error {
		return os.WriteFile(outputPath, []byte("wasm:"+filepath.Base(outputPath)), 0o644)
	}
	runtimeOptimizeWASMWithWasmOpt = func(string) (bool, error) { return false, nil }
	runtimeMetricsForFile = ouroboros.MetricsForFile
}
