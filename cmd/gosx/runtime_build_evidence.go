package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/perf/ouroboros"
)

type buildRuntimeOptions struct {
	OuroborosOut  string
	InventoryPath string
	RepoRoot      string
}

type runtimeBuildTarget struct {
	id            string
	label         string
	file          string
	variant       runtimewasm.Variant
	tags          []string
	compatibility bool
}

var (
	runtimeBuildTinyGoWASM              = buildTinyGoWASM
	runtimeOptimizeWASMWithWasmOpt      = optimizeWASMWithWasmOpt
	runtimeMetricsForFile               = ouroboros.MetricsForFile
	runtimeStageTinyGoWASMExec          = stageTinyGoWASMExec
	runtimeResolveGoSXModuleRoot        = resolveGoSXModuleRoot
	runtimeEnsureWASMRuntimeDeps        = ensureWASMRuntimeDependencies
	runtimeResolveWASMCompiler          = func() (wasmCompiler, string, error) { return resolveWASMCompiler(BuildOptions{}, exec.LookPath) }
	runtimeBuildInputEvidenceForRepo    = ouroboros.BuildInputEvidenceForRepo
	runtimeBuildCanonicalSourceIdentity = ouroboros.BuildCanonicalSourceIdentity
	runtimeWriteBuildEvidence           = func(root string, evidence *ouroboros.RuntimeBuildEvidence) error {
		return ouroboros.WriteNewJSONFile(filepath.Join(root, "wasm", "runtime-artifacts.json"), evidence)
	}
)

func runtimeBuildTargets() []runtimeBuildTarget {
	return []runtimeBuildTarget{
		{id: "core", label: "core", file: "gosx-runtime-core.wasm", variant: runtimewasm.VariantCore, tags: runtimeVariantBuildTags(runtimewasm.VariantCore)},
		{id: "engine", label: "engine", file: "gosx-runtime-engine.wasm", variant: runtimewasm.VariantEngine, tags: runtimeVariantBuildTags(runtimewasm.VariantEngine)},
		{id: "collab", label: "collab", file: "gosx-runtime-collab.wasm", variant: runtimewasm.VariantCollab, tags: runtimeVariantBuildTags(runtimewasm.VariantCollab)},
		{id: "full", label: "full", file: "gosx-runtime.wasm", variant: runtimewasm.VariantFull, tags: runtimeVariantBuildTags(runtimewasm.VariantFull)},
		{id: "islands", label: "islands compatibility", file: "gosx-runtime-islands.wasm", variant: runtimewasm.VariantIslands, tags: islandOnlyWASMTags(wasmCompilerTinyGo), compatibility: true},
	}
}

func RunBuildRuntimeWithOptions(outDir string, opts buildRuntimeOptions) error {
	if strings.TrimSpace(outDir) == "" {
		outDir = "build"
	}
	canonical := strings.TrimSpace(opts.OuroborosOut) != ""
	outDir, err := filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve runtime build directory: %w", err)
	}
	evidencePath := ""
	if canonical {
		evidencePath = filepath.Join(opts.OuroborosOut, "wasm", "runtime-artifacts.json")
		if err := ouroboros.EnsureNewJSONFilePath(evidencePath); err != nil {
			return err
		}
		if _, err := os.Lstat(outDir); err == nil {
			return fmt.Errorf("canonical runtime output already exists: %s", outDir)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect canonical runtime output: %w", err)
		}
	}

	projectRoot := "."
	if canonical {
		projectRoot = strings.TrimSpace(opts.RepoRoot)
		if projectRoot == "" {
			projectRoot = "."
		}
		projectRoot, err = resolvedRuntimeDirectory(projectRoot)
		if err != nil {
			return fmt.Errorf("resolve canonical runtime repository root: %w", err)
		}
	}
	gosxRoot, err := runtimeResolveGoSXModuleRoot(projectRoot)
	if err != nil {
		return err
	}
	gosxRoot, err = resolvedRuntimeDirectory(gosxRoot)
	if err != nil {
		return fmt.Errorf("resolve GoSX module root: %w", err)
	}
	if canonical && gosxRoot != projectRoot {
		return fmt.Errorf("canonical runtime root mismatch: --root resolves to %s but GoSX compilation resolves to %s", projectRoot, gosxRoot)
	}
	if err := runtimeEnsureWASMRuntimeDeps(gosxRoot); err != nil {
		return err
	}
	_, tinygoPath, err := runtimeResolveWASMCompiler()
	if err != nil {
		return err
	}

	evidence := newRuntimeBuildEvidence(outDir, canonical)
	evidence.TinyGo = toolStatusFromCommand("tinygo", tinygoPath, "version")
	evidence.WasmOpt = toolStatusFromCommand("wasm-opt", "", "--version")
	evidence.GoVersion = toolStatusFromCommand("go", "", "version")
	if canonical {
		buildInput, err := runtimeBuildInputEvidenceForRepo(projectRoot, "", "")
		if err != nil {
			return err
		}
		source, err := runtimeBuildCanonicalSourceIdentity(context.Background(), projectRoot, opts.InventoryPath, opts.OuroborosOut)
		if err != nil {
			return err
		}
		evidence.Source = source
		evidence.BuildInput = portableRuntimeBuildInput(buildInput)
		evidence.GoVersion = portableRuntimeToolStatus(evidence.GoVersion)
		evidence.TinyGo = portableRuntimeToolStatus(evidence.TinyGo)
		evidence.WasmOpt = portableRuntimeToolStatus(evidence.WasmOpt)
	}

	buildOutDir := outDir
	tempBuildDir := ""
	if canonical {
		if err := os.MkdirAll(filepath.Dir(outDir), 0o755); err != nil {
			return fmt.Errorf("create canonical runtime output parent: %w", err)
		}
		tempBuildDir, err = os.MkdirTemp(filepath.Dir(outDir), "."+filepath.Base(outDir)+".ouroboros-*")
		if err != nil {
			return fmt.Errorf("create atomic runtime build directory: %w", err)
		}
		buildOutDir = tempBuildDir
		defer func() { _ = os.RemoveAll(tempBuildDir) }()
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create runtime build directory: %w", err)
	}

	shim, err := runtimeStageTinyGoWASMExec(tinygoPath, buildOutDir)
	if err != nil {
		return err
	}
	if canonical && (shim.Source.SHA256 == "" || shim.Source.Bytes <= 0) {
		return fmt.Errorf("canonical runtime evidence requires TinyGo wasm_exec.js shim provenance")
	}

	targets := runtimeBuildTargets()
	for targetIndex, target := range targets {
		outputPath := filepath.Join(buildOutDir, target.file)
		if err := runtimeBuildTinyGoWASM(gosxRoot, gosxRoot, outputPath, tinygoPath, target.tags...); err != nil {
			recordRuntimeBuildFailure(evidence, targets, targetIndex, target, err, canonical, buildOutDir, outDir, gosxRoot)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return fmt.Errorf("build %s runtime with TinyGo: %v; write failure evidence: %w", target.label, err, writeErr)
			}
			return fmt.Errorf("build %s runtime with TinyGo: %w", target.label, err)
		}
		if err := validateTinyGoWASMExec(shim, buildOutDir); err != nil {
			recordRuntimeBuildFailure(evidence, targets, targetIndex, target, err, canonical, buildOutDir, outDir, gosxRoot)
			_ = maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence)
			return fmt.Errorf("validate TinyGo wasm_exec.js after %s build: %w", target.label, err)
		}
		optimized, err := runtimeOptimizeWASMWithWasmOpt(outputPath)
		if err != nil {
			recordRuntimeBuildFailure(evidence, targets, targetIndex, target, err, canonical, buildOutDir, outDir, gosxRoot)
			_ = maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence)
			return fmt.Errorf("optimize %s runtime: %w", target.label, err)
		}
		if optimized {
			fmt.Printf("Applied wasm-opt -Oz (%s)\n", target.label)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			recordRuntimeBuildFailure(evidence, targets, targetIndex, target, err, canonical, buildOutDir, outDir, gosxRoot)
			_ = maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence)
			return fmt.Errorf("read %s runtime: %w", target.label, err)
		}
		if err := writeCompressedSidecarsIfSmaller(outputPath, data); err != nil {
			recordRuntimeBuildFailure(evidence, targets, targetIndex, target, err, canonical, buildOutDir, outDir, gosxRoot)
			_ = maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence)
			return fmt.Errorf("write %s runtime compression sidecars: %w", target.label, err)
		}
		metrics, err := runtimeMetricsForFile(outputPath)
		if err != nil {
			recordRuntimeBuildFailure(evidence, targets, targetIndex, target, err, canonical, buildOutDir, outDir, gosxRoot)
			_ = maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence)
			return fmt.Errorf("measure %s runtime: %w", target.label, err)
		}
		evidence.Variants = append(evidence.Variants, measuredRuntimeVariant(target, metrics, evidence, optimized, shim, canonical))
		fmt.Printf("%s (%d bytes)\n", target.file, len(data))
	}
	if err := validateTinyGoWASMExec(shim, buildOutDir); err != nil {
		return fmt.Errorf("validate TinyGo wasm_exec.js before runtime output completion: %w", err)
	}

	if canonical {
		if err := publishRuntimeBuildOutput(buildOutDir, outDir); err != nil {
			return err
		}
		if err := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); err != nil {
			rollbackErr := os.Rename(outDir, tempBuildDir)
			if rollbackErr != nil {
				return fmt.Errorf("write runtime build evidence: %v; rollback published runtime output: %w", err, rollbackErr)
			}
			return fmt.Errorf("write runtime build evidence: %w (published output rolled back)", err)
		}
		// The temporary path was renamed to outDir and the receipt is durable.
		tempBuildDir = ""
	}
	return nil
}

func newRuntimeBuildEvidence(outDir string, canonical bool) *ouroboros.RuntimeBuildEvidence {
	if canonical {
		outDir = "."
	}
	return &ouroboros.RuntimeBuildEvidence{
		SchemaVersion: ouroboros.SchemaVersion,
		Contract:      ouroboros.ContractO02,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		OutputDir:     outDir,
		Variants:      []ouroboros.RuntimeArtifactVariant{},
		Notes: []string{
			"Runtime artifact evidence measures build outputs only; route selection is recorded as an allocated empty list.",
			"The islands artifact is retained for manifest compatibility and is not an independently advertised profile.",
		},
	}
}

func maybeWriteRuntimeBuildEvidence(root string, evidence *ouroboros.RuntimeBuildEvidence) error {
	if strings.TrimSpace(root) == "" || evidence == nil {
		return nil
	}
	return runtimeWriteBuildEvidence(root, evidence)
}

func measuredRuntimeVariant(target runtimeBuildTarget, metrics ouroboros.AssetMetrics, evidence *ouroboros.RuntimeBuildEvidence, optimized bool, shim runtimeShimPublication, canonical bool) ouroboros.RuntimeArtifactVariant {
	sizeBytes := metrics.Bytes
	shimMetrics := shim.Source
	if canonical {
		shimMetrics.File = "wasm_exec.js"
		shimMetrics.SourcePath = "wasm_exec.js"
	}
	sourcePath := target.file
	if !canonical {
		sourcePath = metrics.SourcePath
	}
	return ouroboros.RuntimeArtifactVariant{
		ID:                target.id,
		Variant:           string(target.variant),
		FeatureMask:       uint32(runtimewasm.FeatureMaskForVariant(target.variant)),
		Generation:        "current",
		Status:            "measured",
		SizeBytes:         &sizeBytes,
		File:              target.file,
		SourcePath:        sourcePath,
		SHA256:            metrics.SHA256,
		Bytes:             metrics.Bytes,
		GzipBytes:         metrics.GzipBytes,
		BrotliBytes:       metrics.BrotliBytes,
		BuildArgs:         tinyGoBuildArgs(target.file, target.tags...),
		BuildTags:         tinyGoWASMTags(target.tags...),
		TinyGoVersion:     evidence.TinyGo.Version,
		GoVersion:         evidence.GoVersion.Version,
		WasmOptVersion:    evidence.WasmOpt.Version,
		WasmOptAvailable:  evidence.WasmOpt.Available,
		Optimized:         optimized,
		Shim:              &shimMetrics,
		PlannedSelectedBy: []string{},
	}
}

func recordRuntimeBuildFailure(evidence *ouroboros.RuntimeBuildEvidence, targets []runtimeBuildTarget, failedIndex int, failed runtimeBuildTarget, cause error, canonical bool, portableRoots ...string) {
	if evidence == nil {
		return
	}
	reason := cause.Error()
	if canonical {
		reason = portableRuntimeDiagnostic(reason, portableRoots...)
	}
	evidence.Variants = append(evidence.Variants, ouroboros.RuntimeArtifactVariant{
		ID:                failed.id,
		Variant:           string(failed.variant),
		FeatureMask:       uint32(runtimewasm.FeatureMaskForVariant(failed.variant)),
		Generation:        "current",
		Status:            "failed",
		BuildTags:         tinyGoWASMTags(failed.tags...),
		PlannedSelectedBy: []string{},
		MissingReason:     reason,
	})
	for _, target := range targets[failedIndex+1:] {
		evidence.Variants = append(evidence.Variants, ouroboros.RuntimeArtifactVariant{
			ID:                target.id,
			Variant:           string(target.variant),
			FeatureMask:       uint32(runtimewasm.FeatureMaskForVariant(target.variant)),
			Generation:        "current",
			Status:            "skipped",
			BuildTags:         tinyGoWASMTags(target.tags...),
			PlannedSelectedBy: []string{},
			MissingReason:     "not attempted after an earlier runtime build failure",
		})
	}
}

func portableRuntimeBuildInput(input ouroboros.BuildInputEvidence) ouroboros.BuildInputEvidence {
	input.GoSXModuleDir = "."
	input.GoWork = ""
	return input
}

func portableRuntimeToolStatus(status ouroboros.ToolStatus) ouroboros.ToolStatus {
	status.Path = ""
	status.GOROOT = ""
	status.TinyGoRoot = ""
	status.Environment = nil
	if !status.Available && status.Error != "" {
		status.Error = status.Name + " is unavailable"
	}
	return status
}

func portableRuntimeDiagnostic(message string, roots ...string) string {
	replacements := []string{"$BUILD_DIR", "$OUTPUT_DIR", "$GOSX_ROOT", "$REPO_ROOT"}
	for i, root := range roots {
		if i >= len(replacements) {
			break
		}
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		message = strings.ReplaceAll(message, root, replacements[i])
		message = strings.ReplaceAll(message, filepath.ToSlash(root), replacements[i])
	}
	return message
}

func resolvedRuntimeDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

type runtimeShimPublication struct {
	Source ouroboros.AssetMetrics
	Output ouroboros.AssetMetrics
}

func toolStatusFromLookPath(name, path string, err error) ouroboros.ToolStatus {
	status := ouroboros.ToolStatus{Name: name, Path: path}
	if err == nil {
		status.Available = true
		return status
	}
	status.Error = err.Error()
	return status
}

func toolStatusFromCommand(name, explicitPath string, args ...string) ouroboros.ToolStatus {
	path := explicitPath
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = exec.LookPath(name)
		if err != nil {
			return toolStatusFromLookPath(name, "", err)
		}
	}
	status := toolStatusFromLookPath(name, path, nil)
	cmd := exec.Command(path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		status.Error = strings.TrimSpace(string(out))
		if status.Error == "" {
			status.Error = err.Error()
		}
		return status
	}
	status.Version = strings.TrimSpace(string(out))
	status.Environment = toolEnvironment(path, name)
	if name == "go" {
		status.GOROOT = localCommandOutput(filepath.Dir(path), path, "env", "GOROOT")
	}
	if name == "tinygo" {
		status.TinyGoRoot = localCommandOutput(filepath.Dir(path), path, "env", "TINYGOROOT")
		status.GOROOT = localCommandOutput(filepath.Dir(path), path, "env", "GOROOT")
	}
	return status
}

func tinyGoWASMExecSourcePath(tinygoPath string) (string, error) {
	if strings.TrimSpace(tinygoPath) == "" {
		return "", fmt.Errorf("tinygo path required for TinyGo wasm_exec.js")
	}
	out, err := exec.Command(tinygoPath, "env", "TINYGOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("locate TinyGo wasm_exec.js: tinygo env TINYGOROOT: %w", err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("locate TinyGo wasm_exec.js: TINYGOROOT is empty")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve TinyGo root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve TinyGo root: %w", err)
	}
	resolvedShim, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, "targets", "wasm_exec.js"))
	if err != nil {
		return "", fmt.Errorf("resolve TinyGo wasm_exec.js: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedShim)
	if err != nil {
		return "", fmt.Errorf("verify TinyGo wasm_exec.js path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("TinyGo wasm_exec.js resolved outside TINYGOROOT: %s", resolvedShim)
	}
	return resolvedShim, nil
}

func stageTinyGoWASMExec(tinygoPath, buildOutDir string) (runtimeShimPublication, error) {
	shimPath, err := tinyGoWASMExecSourcePath(tinygoPath)
	if err != nil {
		return runtimeShimPublication{}, err
	}
	source, err := ouroboros.MetricsForFile(shimPath)
	if err != nil {
		return runtimeShimPublication{}, fmt.Errorf("measure TinyGo wasm_exec.js: %w", err)
	}
	data, err := os.ReadFile(shimPath)
	if err != nil {
		return runtimeShimPublication{}, fmt.Errorf("read TinyGo wasm_exec.js: %w", err)
	}
	if err := os.MkdirAll(buildOutDir, 0o755); err != nil {
		return runtimeShimPublication{}, fmt.Errorf("create runtime build directory: %w", err)
	}
	outPath := filepath.Join(buildOutDir, "wasm_exec.js")
	if info, err := os.Lstat(outPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return runtimeShimPublication{}, fmt.Errorf("staged TinyGo wasm_exec.js must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return runtimeShimPublication{}, fmt.Errorf("inspect staged TinyGo wasm_exec.js: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return runtimeShimPublication{}, fmt.Errorf("stage TinyGo wasm_exec.js: %w", err)
	}
	output, err := ouroboros.MetricsForFile(outPath)
	if err != nil {
		return runtimeShimPublication{}, fmt.Errorf("measure staged TinyGo wasm_exec.js: %w", err)
	}
	if output.SHA256 != source.SHA256 || output.Bytes != source.Bytes {
		return runtimeShimPublication{}, fmt.Errorf("staged TinyGo wasm_exec.js does not match source")
	}
	return runtimeShimPublication{Source: source, Output: output}, nil
}

func validateTinyGoWASMExec(shim runtimeShimPublication, buildOutDir string) error {
	if shim.Source.SHA256 == "" || shim.Source.Bytes <= 0 {
		return fmt.Errorf("TinyGo wasm_exec.js source provenance is missing")
	}
	outPath := filepath.Join(buildOutDir, "wasm_exec.js")
	info, err := os.Lstat(outPath)
	if err != nil {
		return fmt.Errorf("inspect staged TinyGo wasm_exec.js: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("staged TinyGo wasm_exec.js must not be a symlink")
	}
	output, err := ouroboros.MetricsForFile(outPath)
	if err != nil {
		return fmt.Errorf("measure staged TinyGo wasm_exec.js: %w", err)
	}
	if output.SHA256 != shim.Source.SHA256 || output.Bytes != shim.Source.Bytes {
		return fmt.Errorf("staged TinyGo wasm_exec.js does not match source")
	}
	return nil
}

func publishRuntimeBuildOutput(srcDir, dstDir string) error {
	if _, err := os.Lstat(dstDir); err == nil {
		return fmt.Errorf("runtime output already exists: %s", dstDir)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(srcDir, dstDir); err != nil {
		return fmt.Errorf("publish runtime artifact directory: %w", err)
	}
	return nil
}

func toolEnvironment(path, name string) map[string]string {
	env := map[string]string{}
	if name == "go" || name == "tinygo" {
		if goroot := localCommandOutput(filepath.Dir(path), path, "env", "GOROOT"); goroot != "" {
			env["GOROOT"] = goroot
		}
		if gowork := localCommandOutput(filepath.Dir(path), "go", "env", "GOWORK"); gowork != "" {
			env["GOWORK"] = gowork
		}
	}
	if name == "tinygo" {
		if tinyRoot := localCommandOutput(filepath.Dir(path), path, "env", "TINYGOROOT"); tinyRoot != "" {
			env["TINYGOROOT"] = tinyRoot
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func localCommandOutput(dir, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
