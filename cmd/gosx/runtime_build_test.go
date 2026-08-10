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

func TestRunBuildRuntimePublishesWASMVariantsAndTinyGoShim(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	tinygoPath, shimPath := writeFakeTinyGoWithShim(t, dir, []byte("tinygo shim bytes"))
	outDir := filepath.Join(dir, "runtime-out")
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, tinygoPath, nil }
	runtimeStageTinyGoWASMExec = stageTinyGoWASMExec
	runtimeBuildTinyGoWASM = func(projectDir, gosxRoot, outputPath, tinygoPath string, extraTags ...string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte(filepath.Base(outputPath)), 0644)
	}

	if err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gosx-runtime.wasm", "gosx-runtime-islands.wasm", "wasm_exec.js"} {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("published %s missing: %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0644 {
			t.Fatalf("%s mode = %v, want 0644", name, got)
		}
	}
	source, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join(outDir, "wasm_exec.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(published) != string(source) {
		t.Fatalf("published shim bytes = %q, want %q", published, source)
	}
}

func TestRunBuildRuntimePublishesCanonicalOutputWithTinyGoShim(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	tinygoPath, shimPath := writeFakeTinyGoWithShim(t, dir, []byte("canonical tinygo shim"))
	outDir := filepath.Join(dir, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, tinygoPath, nil }
	runtimeStageTinyGoWASMExec = stageTinyGoWASMExec
	runtimeBuildTinyGoWASM = func(projectDir, gosxRoot, outputPath, tinygoPath string, extraTags ...string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("wasm "+filepath.Base(outputPath)), 0644)
	}

	if err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "source-inventory.json"),
		RepoRoot:      ".",
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".runtime-out.ouroboros-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("canonical temp directory remained: %#v", matches)
	}
	for _, name := range []string{"gosx-runtime.wasm", "gosx-runtime-islands.wasm", "wasm_exec.js"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("published %s missing: %v", name, err)
		}
	}
	sourceMetrics, err := ouroboros.MetricsForFile(shimPath)
	if err != nil {
		t.Fatal(err)
	}
	outputMetrics, err := ouroboros.MetricsForFile(filepath.Join(outDir, "wasm_exec.js"))
	if err != nil {
		t.Fatal(err)
	}
	if outputMetrics.SHA256 != sourceMetrics.SHA256 || outputMetrics.Bytes != sourceMetrics.Bytes {
		t.Fatalf("published shim metrics = %#v, want source %#v", outputMetrics, sourceMetrics)
	}
	var evidence ouroboros.RuntimeBuildEvidence
	body, err := os.ReadFile(filepath.Join(artifactRoot, "wasm", "runtime-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatal(err)
	}
	for _, variant := range evidence.Variants {
		if variant.ID != "runtime" && variant.ID != "islands" {
			continue
		}
		if variant.Shim == nil {
			t.Fatalf("%s variant is missing shim provenance: %#v", variant.ID, variant)
		}
		if variant.Shim.SHA256 != sourceMetrics.SHA256 || variant.Shim.SourcePath != sourceMetrics.SourcePath {
			t.Fatalf("%s shim provenance = %#v, want %#v", variant.ID, variant.Shim, sourceMetrics)
		}
	}
}

func TestRunBuildRuntimePublishesNestedCanonicalOutputWithoutPrecreatedParent(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	tinygoPath, _ := writeFakeTinyGoWithShim(t, dir, []byte("nested canonical tinygo shim"))
	outParent := filepath.Join(dir, "nested", "runtime")
	outDir := filepath.Join(outParent, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, tinygoPath, nil }
	runtimeStageTinyGoWASMExec = stageTinyGoWASMExec
	runtimeBuildTinyGoWASM = func(projectDir, gosxRoot, outputPath, tinygoPath string, extraTags ...string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("wasm "+filepath.Base(outputPath)), 0644)
	}

	if _, err := os.Stat(outParent); !os.IsNotExist(err) {
		t.Fatalf("test parent precondition failed: %v", err)
	}
	if err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "source-inventory.json"),
		RepoRoot:      ".",
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(outParent, ".runtime-out.ouroboros-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("canonical temp directory remained: %#v", matches)
	}
	for _, name := range []string{"gosx-runtime.wasm", "gosx-runtime-islands.wasm", "wasm_exec.js"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("published %s missing: %v", name, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(artifactRoot, "wasm", "runtime-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence ouroboros.RuntimeBuildEvidence
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.OutputDir != outDir {
		t.Fatalf("evidence OutputDir = %q, want %q", evidence.OutputDir, outDir)
	}
	rows := map[string]ouroboros.RuntimeArtifactVariant{}
	for _, variant := range evidence.Variants {
		rows[variant.ID] = variant
	}
	for _, id := range []string{"runtime", "islands"} {
		if rows[id].Status != "measured" {
			t.Fatalf("%s status = %q, want measured: %#v", id, rows[id].Status, rows[id])
		}
		if !strings.HasPrefix(rows[id].SourcePath, outDir+string(filepath.Separator)) {
			t.Fatalf("%s SourcePath = %q, want published nested path", id, rows[id].SourcePath)
		}
	}
}

func TestRunBuildRuntimeFailsClosedWhenTinyGoShimIsMissing(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	tinygoPath := writeFakeTinyGo(t, dir, filepath.Join(dir, "tinygo-root"))
	outDir := filepath.Join(dir, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, tinygoPath, nil }
	runtimeStageTinyGoWASMExec = stageTinyGoWASMExec

	err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "source-inventory.json"),
		RepoRoot:      ".",
	})
	if err == nil || !strings.Contains(err.Error(), "resolve TinyGo wasm_exec.js") {
		t.Fatalf("RunBuildRuntimeWithOptions error = %v, want missing shim failure", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("canonical output was published after missing shim: %v", statErr)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".runtime-out.ouroboros-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial canonical temp directory remained: %#v", matches)
	}
}

func TestRunBuildRuntimeCleansCanonicalOutputAfterShimStageFailure(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "runtime-out")
	artifactRoot := filepath.Join(dir, "evidence")
	runtimeStageTinyGoWASMExec = func(tinygoPath, buildOutDir string) (runtimeShimPublication, error) {
		if err := os.MkdirAll(buildOutDir, 0755); err != nil {
			return runtimeShimPublication{}, err
		}
		if err := os.WriteFile(filepath.Join(buildOutDir, "wasm_exec.js"), []byte("partial"), 0644); err != nil {
			return runtimeShimPublication{}, err
		}
		return runtimeShimPublication{}, errors.New("stage shim failed")
	}

	err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
		OuroborosOut:  artifactRoot,
		InventoryPath: filepath.Join(dir, "source-inventory.json"),
		RepoRoot:      ".",
	})
	if err == nil || !strings.Contains(err.Error(), "stage shim failed") {
		t.Fatalf("RunBuildRuntimeWithOptions error = %v, want shim stage failure", err)
	}
	if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
		t.Fatalf("canonical output was published after shim failure: %v", statErr)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".runtime-out.ouroboros-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial canonical temp directory remained: %#v", matches)
	}
}

func TestRunBuildRuntimeRejectsShimMutationAfterWASMBuild(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, shimPath string){
		"changed": func(t *testing.T, shimPath string) {
			t.Helper()
			if err := os.WriteFile(shimPath, []byte("changed shim"), 0644); err != nil {
				t.Fatal(err)
			}
		},
		"removed": func(t *testing.T, shimPath string) {
			t.Helper()
			if err := os.Remove(shimPath); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			restore := stubRuntimeBuildHooks(t)
			defer restore()
			dir := t.TempDir()
			tinygoPath, _ := writeFakeTinyGoWithShim(t, dir, []byte("trusted shim"))
			outDir := filepath.Join(dir, "runtime-out")
			artifactRoot := filepath.Join(dir, "evidence")
			runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, tinygoPath, nil }
			runtimeStageTinyGoWASMExec = stageTinyGoWASMExec
			builds := 0
			runtimeBuildTinyGoWASM = func(projectDir, gosxRoot, outputPath, tinygoPath string, extraTags ...string) error {
				if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(outputPath, []byte("wasm "+filepath.Base(outputPath)), 0644); err != nil {
					return err
				}
				builds++
				mutate(t, filepath.Join(filepath.Dir(outputPath), "wasm_exec.js"))
				return nil
			}

			err := RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{
				OuroborosOut:  artifactRoot,
				InventoryPath: filepath.Join(dir, "source-inventory.json"),
				RepoRoot:      ".",
			})
			if err == nil || !strings.Contains(err.Error(), "validate TinyGo wasm_exec.js after runtime build") {
				t.Fatalf("RunBuildRuntimeWithOptions error = %v, want post-build shim validation failure", err)
			}
			if builds != 1 {
				t.Fatalf("build count = %d, want early failure after first build", builds)
			}
			if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
				t.Fatalf("canonical output was published after shim mutation: %v", statErr)
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".runtime-out.ouroboros-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("partial canonical temp directory remained: %#v", matches)
			}
		})
	}
}

func TestTinyGoWASMExecSourcePathRejectsShimOutsideTinyGoRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "tinygo-root")
	targetsDir := filepath.Join(root, "targets")
	if err := os.MkdirAll(targetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside-wasm_exec.js")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(targetsDir, "wasm_exec.js")); err != nil {
		t.Fatal(err)
	}
	tinygoPath := writeFakeTinyGo(t, dir, root)

	_, err := tinyGoWASMExecSourcePath(tinygoPath)
	if err == nil || !strings.Contains(err.Error(), "outside TINYGOROOT") {
		t.Fatalf("tinyGoWASMExecSourcePath error = %v, want outside-root refusal", err)
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
		"islands": {"current", "measured", []string{"R02", "R03", "R09A", "R09B"}},
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

func TestRunBuildRuntimeWritesFailureEvidenceForNestedCanonicalOutputWithoutPrecreatedParent(t *testing.T) {
	restore := stubRuntimeBuildHooks(t)
	defer restore()
	dir := t.TempDir()
	outParent := filepath.Join(dir, "nested", "runtime")
	outDir := filepath.Join(outParent, "runtime-out")
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

	if _, err := os.Stat(outParent); !os.IsNotExist(err) {
		t.Fatalf("test parent precondition failed: %v", err)
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
	matches, err := filepath.Glob(filepath.Join(outParent, ".runtime-out.ouroboros-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial runtime temp directory was not cleaned: %#v", matches)
	}
	body, err := os.ReadFile(filepath.Join(artifactRoot, "wasm", "runtime-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence ouroboros.RuntimeBuildEvidence
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
	oldStageShim := runtimeStageTinyGoWASMExec
	oldResolve := runtimeResolveWASMCompiler
	oldInput := runtimeBuildInputEvidenceForRepo
	oldSource := runtimeBuildCanonicalSourceIdentity
	runtimeResolveWASMCompiler = func() (wasmCompiler, string, error) { return wasmCompilerTinyGo, "tinygo", nil }
	runtimeOptimizeWASMWithWasmOpt = func(string) (bool, error) { return false, nil }
	runtimeMetricsForFile = ouroboros.MetricsForFile
	runtimeStageTinyGoWASMExec = func(tinygoPath, buildOutDir string) (runtimeShimPublication, error) {
		if err := os.MkdirAll(buildOutDir, 0755); err != nil {
			return runtimeShimPublication{}, err
		}
		outPath := filepath.Join(buildOutDir, "wasm_exec.js")
		if err := os.WriteFile(outPath, []byte("shim"), 0644); err != nil {
			return runtimeShimPublication{}, err
		}
		output, err := ouroboros.MetricsForFile(outPath)
		if err != nil {
			return runtimeShimPublication{}, err
		}
		source := output
		source.SourcePath = "tinygo/targets/wasm_exec.js"
		return runtimeShimPublication{Source: source, Output: output}, nil
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
		runtimeStageTinyGoWASMExec = oldStageShim
		runtimeResolveWASMCompiler = oldResolve
		runtimeBuildInputEvidenceForRepo = oldInput
		runtimeBuildCanonicalSourceIdentity = oldSource
	}
}

func writeFakeTinyGoWithShim(t *testing.T, dir string, shim []byte) (string, string) {
	t.Helper()
	root := filepath.Join(dir, "tinygo-root")
	shimPath := filepath.Join(root, "targets", "wasm_exec.js")
	if err := os.MkdirAll(filepath.Dir(shimPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shimPath, shim, 0644); err != nil {
		t.Fatal(err)
	}
	return writeFakeTinyGo(t, dir, root), shimPath
}

func writeFakeTinyGo(t *testing.T, dir, root string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "targets"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tinygo")
	script := "#!/bin/sh\nif [ \"$1\" = \"env\" ] && [ \"$2\" = \"TINYGOROOT\" ]; then\n  printf '%s\\n' '" + root + "'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
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
