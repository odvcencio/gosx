package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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
		StageAssets: func(outputDir string, manifest exportManifest) error {
			return copyExportRuntime(filepath.Join(absDir, "build"), outputDir, manifest)
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

func copyExportRuntime(buildDir, outputDir string, manifest exportManifest) error {
	for _, ref := range manifest.AssetRefs {
		src, ok := exportRuntimeBuildPath(buildDir, ref)
		if !ok {
			continue
		}
		dst, ok := exportRuntimeOutputPath(outputDir, ref)
		if !ok {
			continue
		}
		if err := copyFile(dst, src); err != nil {
			return err
		}
	}
	return nil
}

func exportRuntimeBuildPath(buildDir, ref string) (string, bool) {
	ref = path.Clean("/" + strings.TrimLeft(strings.TrimSpace(ref), "/"))
	switch ref {
	case "/gosx/runtime.wasm":
		return filepath.Join(buildDir, "gosx-runtime.wasm"), true
	case "/gosx/runtime-islands.wasm":
		return filepath.Join(buildDir, "gosx-runtime-islands.wasm"), true
	case "/gosx/wasm_exec.js":
		return filepath.Join(buildDir, "wasm_exec.js"), true
	case "/gosx/bootstrap.js":
		return filepath.Join(buildDir, "bootstrap.js"), true
	case "/gosx/bootstrap-lite.js":
		return filepath.Join(buildDir, "bootstrap-lite.js"), true
	case "/gosx/bootstrap-runtime.js":
		return filepath.Join(buildDir, "bootstrap-runtime.js"), true
	case "/gosx/bootstrap-feature-islands.js":
		return filepath.Join(buildDir, "bootstrap-feature-islands.js"), true
	case "/gosx/bootstrap-feature-engines.js":
		return filepath.Join(buildDir, "bootstrap-feature-engines.js"), true
	case "/gosx/bootstrap-feature-hubs.js":
		return filepath.Join(buildDir, "bootstrap-feature-hubs.js"), true
	case "/gosx/bootstrap-feature-scene3d.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d.js"), true
	case "/gosx/bootstrap-feature-scene3d-webgpu.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-webgpu.js"), true
	case "/gosx/bootstrap-feature-scene3d-gltf.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-gltf.js"), true
	case "/gosx/bootstrap-feature-scene3d-animation.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-animation.js"), true
	case "/gosx/patch.js":
		return filepath.Join(buildDir, "patch.js"), true
	case "/gosx/hls.min.js":
		return filepath.Join(buildDir, "hls.min.js"), true
	}
	if rel, ok := strings.CutPrefix(ref, "/gosx/islands/"); ok && rel != "" {
		return filepath.Join(buildDir, "islands", filepath.FromSlash(rel)), true
	}
	if rel, ok := strings.CutPrefix(ref, "/gosx/css/"); ok && rel != "" {
		return filepath.Join(buildDir, "css", filepath.FromSlash(rel)), true
	}
	return "", false
}

func exportRuntimeOutputPath(outputDir, ref string) (string, bool) {
	ref = path.Clean("/" + strings.TrimLeft(strings.TrimSpace(ref), "/"))
	if !strings.HasPrefix(ref, "/gosx/") {
		return "", false
	}
	return filepath.Join(outputDir, filepath.FromSlash(strings.TrimPrefix(ref, "/"))), true
}
