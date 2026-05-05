package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func RunBuildRuntime(outDir string) error {
	if outDir == "" {
		outDir = "build"
	}
	gosxRoot, err := resolveGoSXModuleRoot(".")
	if err != nil {
		return err
	}
	if err := ensureWASMRuntimeDependencies(gosxRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create runtime build directory: %w", err)
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve runtime build directory: %w", err)
	}

	_, tinygoPath, err := resolveWASMCompiler(BuildOptions{}, exec.LookPath)
	if err != nil {
		return err
	}

	for _, target := range []struct {
		label string
		file  string
		tags  []string
	}{
		{label: "runtime", file: "gosx-runtime.wasm"},
		{label: "islands", file: "gosx-runtime-islands.wasm", tags: islandOnlyWASMTags(wasmCompilerTinyGo)},
	} {
		outputPath := filepath.Join(outDir, target.file)
		if err := buildTinyGoWASM(gosxRoot, gosxRoot, outputPath, tinygoPath, target.tags...); err != nil {
			return fmt.Errorf("build %s runtime with TinyGo: %w", target.label, err)
		}
		if optimized, err := optimizeWASMWithWasmOpt(outputPath); err != nil {
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
		fmt.Printf("%s (%d bytes)\n", target.file, len(data))
	}
	return nil
}

func cmdBuildRuntime() {
	outDir := "build"
	if len(os.Args) > 2 {
		outDir = os.Args[2]
	}
	if err := RunBuildRuntime(outDir); err != nil {
		fmt.Fprintf(os.Stderr, "build-runtime error: %v\n", err)
		os.Exit(1)
	}
}
