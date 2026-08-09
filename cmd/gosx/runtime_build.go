package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"m31labs.dev/gosx/perf/ouroboros"
)

func RunBuildRuntime(outDir string) error {
	return RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{})
}

type buildRuntimeOptions struct {
	OuroborosOut  string
	InventoryPath string
	RepoRoot      string
}

var (
	runtimeBuildTinyGoWASM              = buildTinyGoWASM
	runtimeOptimizeWASMWithWasmOpt      = optimizeWASMWithWasmOpt
	runtimeMetricsForFile               = ouroboros.MetricsForFile
	runtimeStageTinyGoWASMExec          = stageTinyGoWASMExec
	runtimeResolveWASMCompiler          = func() (wasmCompiler, string, error) { return resolveWASMCompiler(BuildOptions{}, exec.LookPath) }
	runtimeBuildInputEvidenceForRepo    = ouroboros.BuildInputEvidenceForRepo
	runtimeBuildCanonicalSourceIdentity = ouroboros.BuildCanonicalSourceIdentity
)

func RunBuildRuntimeWithOptions(outDir string, opts buildRuntimeOptions) error {
	if outDir == "" {
		outDir = "build"
	}
	gosxRoot, err := resolveGoSXModuleRoot(".")
	if err != nil {
		return err
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve runtime build directory: %w", err)
	}
	evidence := newRuntimeBuildEvidence(outDir)
	if opts.OuroborosOut != "" {
		evidencePath := filepath.Join(opts.OuroborosOut, "wasm", "runtime-artifacts.json")
		if err := ouroboros.EnsureNewJSONFilePath(evidencePath); err != nil {
			return err
		}
		if _, err := os.Lstat(outDir); err == nil {
			return fmt.Errorf("canonical runtime output already exists: %s", outDir)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := ensureWASMRuntimeDependencies(gosxRoot); err != nil {
		return err
	}
	_, tinygoPath, err := runtimeResolveWASMCompiler()
	if err != nil {
		evidence.TinyGo = toolStatusFromLookPath("tinygo", "", err)
		return err
	}
	evidence.TinyGo = toolStatusFromCommand("tinygo", tinygoPath, "version")
	evidence.WasmOpt = toolStatusFromCommand("wasm-opt", "", "--version")
	evidence.GoVersion = toolStatusFromCommand("go", "", "version")
	if opts.OuroborosOut != "" {
		repoRoot := opts.RepoRoot
		if repoRoot == "" {
			repoRoot = "."
		}
		buildInput, err := runtimeBuildInputEvidenceForRepo(repoRoot, "", "")
		if err != nil {
			return err
		}
		source, err := runtimeBuildCanonicalSourceIdentity(context.Background(), repoRoot, opts.InventoryPath, opts.OuroborosOut)
		if err != nil {
			return err
		}
		evidence.Source = source
		evidence.BuildInput = buildInput
	}
	buildOutDir := outDir
	cleanupBuildOut := func() {}
	if opts.OuroborosOut != "" {
		parent := filepath.Dir(outDir)
		tmp, err := os.MkdirTemp(parent, "."+filepath.Base(outDir)+".ouroboros-*")
		if err != nil {
			return fmt.Errorf("create atomic runtime build directory: %w", err)
		}
		buildOutDir = tmp
		cleanupBuildOut = func() { _ = os.RemoveAll(tmp) }
		defer cleanupBuildOut()
	} else if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create runtime build directory: %w", err)
	}

	shim, err := runtimeStageTinyGoWASMExec(tinygoPath, buildOutDir)
	if err != nil {
		return err
	}
	if opts.OuroborosOut != "" && shim.Source.SHA256 == "" {
		return fmt.Errorf("canonical runtime evidence requires TinyGo wasm_exec.js shim provenance")
	}

	for _, target := range runtimeBuildTargets() {
		outputPath := filepath.Join(buildOutDir, target.file)
		publishedPath := filepath.Join(outDir, target.file)
		if err := runtimeBuildTinyGoWASM(gosxRoot, gosxRoot, outputPath, tinygoPath, target.tags...); err != nil {
			recordRuntimeBuildFailure(evidence, target, err)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("build %s runtime with TinyGo: %w", target.label, err)
		}
		if err := validateTinyGoWASMExec(shim, buildOutDir); err != nil {
			recordRuntimeBuildFailure(evidence, target, err)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("validate TinyGo wasm_exec.js after %s build: %w", target.label, err)
		}
		optimized, err := runtimeOptimizeWASMWithWasmOpt(outputPath)
		if err != nil {
			recordRuntimeBuildFailure(evidence, target, err)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("optimize %s runtime: %w", target.label, err)
		} else if optimized {
			fmt.Printf("Applied wasm-opt -Oz (%s)\n", target.label)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			recordRuntimeBuildFailure(evidence, target, err)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("read %s runtime: %w", target.label, err)
		}
		if err := writeCompressedSidecarsIfSmaller(outputPath, data); err != nil {
			recordRuntimeBuildFailure(evidence, target, err)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("write %s runtime compression sidecars: %w", target.label, err)
		}
		metrics, err := runtimeMetricsForFile(outputPath)
		if err != nil {
			recordRuntimeBuildFailure(evidence, target, err)
			if writeErr := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("measure %s runtime: %w", target.label, err)
		}
		var shimPtr *ouroboros.AssetMetrics
		if shim.Source.SHA256 != "" {
			shimCopy := shim.Source
			shimPtr = &shimCopy
		}
		sizeBytes := metrics.Bytes
		evidence.Variants = append(evidence.Variants, currentRuntimeVariant(target, metrics, sizeBytes, publishedPath, evidence, optimized, shimPtr))
		fmt.Printf("%s (%d bytes)\n", target.file, len(data))
	}
	if err := validateTinyGoWASMExec(shim, buildOutDir); err != nil {
		return fmt.Errorf("validate TinyGo wasm_exec.js before runtime output completion: %w", err)
	}
	finishRuntimeContractRows(evidence)
	if opts.OuroborosOut != "" {
		if err := publishRuntimeBuildOutput(buildOutDir, outDir); err != nil {
			return err
		}
		cleanupBuildOut = func() {}
	}
	if err := maybeWriteRuntimeBuildEvidence(opts.OuroborosOut, evidence); err != nil {
		return err
	}
	return nil
}

func cmdBuildRuntime() {
	outDir := "build"
	opts := buildRuntimeOptions{RepoRoot: "."}
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--ouroboros-out":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build-runtime error: --ouroboros-out requires a directory")
				os.Exit(1)
			}
			opts.OuroborosOut = args[i]
		case "--inventory":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build-runtime error: --inventory requires a file")
				os.Exit(1)
			}
			opts.InventoryPath = args[i]
		case "--root":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build-runtime error: --root requires a directory")
				os.Exit(1)
			}
			opts.RepoRoot = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				fmt.Fprintf(os.Stderr, "build-runtime error: unknown flag %s\n", arg)
				os.Exit(1)
			}
			outDir = arg
		}
	}
	if err := RunBuildRuntimeWithOptions(outDir, opts); err != nil {
		fmt.Fprintf(os.Stderr, "build-runtime error: %v\n", err)
		os.Exit(1)
	}
}

type runtimeBuildTarget struct {
	id             string
	label          string
	file           string
	tags           []string
	selectedRoutes []string
}

func runtimeBuildTargets() []runtimeBuildTarget {
	return []runtimeBuildTarget{
		{id: "runtime", label: "runtime", file: "gosx-runtime.wasm", selectedRoutes: []string{"R05", "R06", "R07", "R08", "R10"}},
		{id: "islands", label: "islands", file: "gosx-runtime-islands.wasm", tags: islandOnlyWASMTags(wasmCompilerTinyGo), selectedRoutes: []string{"R02", "R03", "R09A", "R09B"}},
	}
}

func newRuntimeBuildEvidence(outDir string) *ouroboros.RuntimeBuildEvidence {
	return &ouroboros.RuntimeBuildEvidence{
		SchemaVersion: ouroboros.SchemaVersion,
		Contract:      ouroboros.ContractO02,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		OutputDir:     outDir,
	}
}

func maybeWriteRuntimeBuildEvidence(root string, evidence *ouroboros.RuntimeBuildEvidence) error {
	if strings.TrimSpace(root) == "" || evidence == nil {
		return nil
	}
	return ouroboros.WriteNewJSONFile(filepath.Join(root, "wasm", "runtime-artifacts.json"), evidence)
}

func plannedRuntimeVariant(id string, selectedRoutes []string) ouroboros.RuntimeArtifactVariant {
	return ouroboros.RuntimeArtifactVariant{
		ID:                id,
		Generation:        "future",
		Status:            "planned",
		PlannedSelectedBy: selectedRoutes,
		MissingReason:     "planned O1-O6 bounded TinyGo variant; no artifact exists in O0.2",
	}
}

func skippedCurrentRuntimeVariant(target runtimeBuildTarget) ouroboros.RuntimeArtifactVariant {
	return ouroboros.RuntimeArtifactVariant{
		ID:                target.id,
		Generation:        "current",
		Status:            "skipped",
		PlannedSelectedBy: target.selectedRoutes,
		MissingReason:     "not attempted after earlier runtime build failure",
	}
}

func recordRuntimeBuildFailure(evidence *ouroboros.RuntimeBuildEvidence, target runtimeBuildTarget, cause error) {
	if evidence == nil {
		return
	}
	evidence.Variants = append(evidence.Variants, ouroboros.RuntimeArtifactVariant{
		ID:                target.id,
		Generation:        "current",
		Status:            "failed",
		PlannedSelectedBy: target.selectedRoutes,
		MissingReason:     cause.Error(),
		BuildTags:         tinyGoWASMTags(target.tags...),
	})
	finishRuntimeContractRows(evidence)
}

func finishRuntimeContractRows(evidence *ouroboros.RuntimeBuildEvidence) {
	if evidence == nil {
		return
	}
	seen := map[string]bool{}
	for _, variant := range evidence.Variants {
		seen[variant.ID] = true
	}
	for _, target := range runtimeBuildTargets() {
		if !seen[target.id] {
			evidence.Variants = append(evidence.Variants, skippedCurrentRuntimeVariant(target))
			seen[target.id] = true
		}
	}
	for _, variant := range plannedRuntimeVariants() {
		if !seen[variant.ID] {
			evidence.Variants = append(evidence.Variants, variant)
			seen[variant.ID] = true
		}
	}
}

func plannedRuntimeVariants() []ouroboros.RuntimeArtifactVariant {
	return []ouroboros.RuntimeArtifactVariant{
		plannedRuntimeVariant("core", []string{"R01", "R02", "R03", "R04", "R09A", "R09B"}),
		plannedRuntimeVariant("engine", []string{"R05", "R07", "R08", "R10"}),
		plannedRuntimeVariant("collab", []string{"R06"}),
		plannedRuntimeVariant("full", []string{}),
	}
}

func currentRuntimeVariant(target runtimeBuildTarget, metrics ouroboros.AssetMetrics, sizeBytes int64, publishedPath string, evidence *ouroboros.RuntimeBuildEvidence, optimized bool, shim *ouroboros.AssetMetrics) ouroboros.RuntimeArtifactVariant {
	return ouroboros.RuntimeArtifactVariant{
		ID:                target.id,
		Generation:        "current",
		Status:            "measured",
		SizeBytes:         &sizeBytes,
		File:              filepath.Base(publishedPath),
		SourcePath:        publishedPath,
		SHA256:            metrics.SHA256,
		Bytes:             metrics.Bytes,
		GzipBytes:         metrics.GzipBytes,
		BrotliBytes:       metrics.BrotliBytes,
		BuildArgs:         tinyGoBuildArgs(publishedPath, target.tags...),
		BuildTags:         tinyGoWASMTags(target.tags...),
		TinyGoVersion:     evidence.TinyGo.Version,
		GoVersion:         evidence.GoVersion.Version,
		WasmOptVersion:    evidence.WasmOpt.Version,
		WasmOptAvailable:  evidence.WasmOpt.Available,
		Optimized:         optimized,
		Shim:              shim,
		PlannedSelectedBy: target.selectedRoutes,
	}
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
	shimPath := filepath.Join(resolvedRoot, "targets", "wasm_exec.js")
	resolvedShim, err := filepath.EvalSymlinks(shimPath)
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
	if err := os.MkdirAll(buildOutDir, 0755); err != nil {
		return runtimeShimPublication{}, fmt.Errorf("create runtime build directory: %w", err)
	}
	outPath := filepath.Join(buildOutDir, "wasm_exec.js")
	if info, err := os.Lstat(outPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return runtimeShimPublication{}, fmt.Errorf("staged TinyGo wasm_exec.js must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return runtimeShimPublication{}, fmt.Errorf("inspect staged TinyGo wasm_exec.js: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
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
