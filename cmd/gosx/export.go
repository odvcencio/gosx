package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/gosx/env"
)

// RunExport prerenders static file-routed pages into dist/static.
func RunExport(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}

	isMain, err := isMainPackage(absDir)
	if err != nil {
		return fmt.Errorf("inspect package: %w", err)
	}
	if !isMain {
		return fmt.Errorf("gosx export requires a runnable app directory (package main): %s", absDir)
	}
	if err := syncModulesPackage(absDir); err != nil {
		return err
	}
	if err := ensureModuleDependencies(absDir); err != nil {
		return err
	}

	if err := env.LoadDir(absDir, ""); err != nil {
		return fmt.Errorf("load env: %w", err)
	}
	if err := prepareDevAssets(absDir); err != nil {
		return err
	}

	binDir, err := os.MkdirTemp("", "gosx-export-bin-*")
	if err != nil {
		return fmt.Errorf("create temp bin dir: %w", err)
	}
	defer os.RemoveAll(binDir)

	binaryPath := filepath.Join(binDir, "app")
	built, err := buildServerBinaryIfPresent(absDir, binaryPath)
	if err != nil {
		return fmt.Errorf("build export binary: %w", err)
	}
	if !built {
		return fmt.Errorf("gosx export requires a runnable app binary: %s", absDir)
	}

	manifest, err := prerenderStaticBundle(staticExportOptions{
		AppRoot:    absDir,
		OutputDir:  filepath.Join(absDir, "dist", "static"),
		BinaryPath: binaryPath,
		StageAssets: func(outputDir string) error {
			return copyExportRuntime(filepath.Join(absDir, "build"), outputDir)
		},
	})
	if err != nil {
		return err
	}
	if err := writeExportManifest(filepath.Join(absDir, "dist", "export.json"), manifest); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "gosx export: wrote %d pages to %s\n", len(manifest.Pages), filepath.Join(absDir, "dist", "static"))
	return nil
}

func copyExportRuntime(buildDir, outputDir string) error {
	gosxDir := filepath.Join(outputDir, "gosx")
	if err := os.MkdirAll(gosxDir, 0755); err != nil {
		return err
	}
	for _, asset := range []struct {
		src string
		dst string
	}{
		{src: filepath.Join(buildDir, "gosx-runtime.wasm"), dst: filepath.Join(gosxDir, "runtime.wasm")},
		{src: filepath.Join(buildDir, "wasm_exec.js"), dst: filepath.Join(gosxDir, "wasm_exec.js")},
		{src: filepath.Join(buildDir, "bootstrap.js"), dst: filepath.Join(gosxDir, "bootstrap.js")},
		{src: filepath.Join(buildDir, "patch.js"), dst: filepath.Join(gosxDir, "patch.js")},
	} {
		if err := copyFile(asset.dst, asset.src); err != nil {
			return err
		}
	}
	if err := copyDirIfPresent(filepath.Join(buildDir, "islands"), filepath.Join(gosxDir, "islands")); err != nil {
		return err
	}
	if err := copyDirIfPresent(filepath.Join(buildDir, "css"), filepath.Join(gosxDir, "css")); err != nil {
		return err
	}
	return nil
}
