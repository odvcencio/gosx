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

func RunBuildRuntime(outDir string) error {
	return RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{})
}

type buildRuntimeOptions struct {
	OuroborosOut  string
	InventoryPath string
	RepoRoot      string
}

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
	_, tinygoPath, err := resolveWASMCompiler(BuildOptions{}, exec.LookPath)
	if err != nil {
		evidence.TinyGo = toolStatusFromLookPath("tinygo", "", err)
		return err
	}
	evidence.TinyGo = toolStatusFromCommand("tinygo", tinygoPath, "version")
	evidence.WasmOpt = toolStatusFromCommand("wasm-opt", "", "--version")
	evidence.GoVersion = toolStatusFromCommand("go", "", "version")
	shim := tinyGoShimMetrics(tinygoPath)
	if opts.OuroborosOut != "" && shim.SHA256 == "" {
		return fmt.Errorf("canonical runtime evidence requires TinyGo wasm_exec.js shim provenance")
	}
	if opts.OuroborosOut != "" {
		repoRoot := opts.RepoRoot
		if repoRoot == "" {
			repoRoot = "."
		}
		buildInput, err := ouroboros.BuildInputEvidenceForRepo(repoRoot, "", "")
		if err != nil {
			return err
		}
		source, err := ouroboros.BuildCanonicalSourceIdentity(context.Background(), repoRoot, opts.InventoryPath, opts.OuroborosOut)
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

	for _, target := range runtimeBuildTargets() {
		outputPath := filepath.Join(buildOutDir, target.file)
		publishedPath := filepath.Join(outDir, target.file)
		if err := buildTinyGoWASM(gosxRoot, gosxRoot, outputPath, tinygoPath, target.tags...); err != nil {
			targetEvidence := failedRuntimeVariant(target)
			targetEvidence.Status = "failed"
			targetEvidence.MissingReason = err.Error()
			evidence.Variants = append(evidence.Variants, targetEvidence)
			return fmt.Errorf("build %s runtime with TinyGo: %w", target.label, err)
		}
		optimized, err := optimizeWASMWithWasmOpt(outputPath)
		if err != nil {
			return fmt.Errorf("optimize %s runtime: %w", target.label, err)
		} else if optimized {
			fmt.Printf("Applied wasm-opt -Oz (%s)\n", target.label)
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			return fmt.Errorf("read %s runtime: %w", target.label, err)
		}
		if err := writeCompressedSidecarsIfSmaller(outputPath, data); err != nil {
			return fmt.Errorf("write %s runtime compression sidecars: %w", target.label, err)
		}
		metrics, err := ouroboros.MetricsForFile(outputPath)
		if err != nil {
			return fmt.Errorf("measure %s runtime: %w", target.label, err)
		}
		var shimPtr *ouroboros.AssetMetrics
		if shim.SHA256 != "" {
			shimCopy := shim
			shimPtr = &shimCopy
		}
		sizeBytes := metrics.Bytes
		evidence.Variants = append(evidence.Variants, currentRuntimeVariant(target, metrics, sizeBytes, publishedPath, evidence, optimized, shimPtr))
		fmt.Printf("%s (%d bytes)\n", target.file, len(data))
	}
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

func failedRuntimeVariant(target runtimeBuildTarget) ouroboros.RuntimeArtifactVariant {
	return ouroboros.RuntimeArtifactVariant{
		ID:                target.id,
		Variant:           target.id,
		FeatureMask:       uint32(runtimewasm.RequiredFeaturesForVariant(runtimewasm.Variant(target.id))),
		Generation:        "current",
		Status:            "failed",
		PlannedSelectedBy: target.selectedRoutes,
	}
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
		{id: "core", label: "core", file: "gosx-runtime-core.wasm", tags: []string{"gosx_runtime_core"}, selectedRoutes: []string{"R01", "R02", "R03", "R04", "R09A", "R09B"}},
		{id: "engine", label: "engine", file: "gosx-runtime-engine.wasm", tags: []string{"gosx_runtime_engine"}, selectedRoutes: []string{"R05", "R07", "R08", "R10"}},
		{id: "collab", label: "collab", file: "gosx-runtime-collab.wasm", tags: []string{"gosx_runtime_collab"}, selectedRoutes: []string{"R06"}},
		{id: "full", label: "full", file: "gosx-runtime.wasm", tags: []string{"gosx_runtime_full"}, selectedRoutes: []string{}},
		{id: "islands", label: "islands", file: "gosx-runtime-islands.wasm", tags: islandOnlyWASMTags(wasmCompilerTinyGo), selectedRoutes: []string{}},
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
		Variant:           string(runtimewasm.Variant(target.id)),
		FeatureMask:       uint32(runtimewasm.RequiredFeaturesForVariant(runtimewasm.Variant(target.id))),
	}
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

func tinyGoShimMetrics(tinygoPath string) ouroboros.AssetMetrics {
	out, err := exec.Command(tinygoPath, "env", "TINYGOROOT").Output()
	if err != nil {
		return ouroboros.AssetMetrics{}
	}
	path := filepath.Join(strings.TrimSpace(string(out)), "targets", "wasm_exec.js")
	metrics, err := ouroboros.MetricsForFile(path)
	if err != nil {
		return ouroboros.AssetMetrics{}
	}
	return metrics
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
