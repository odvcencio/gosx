package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/env"
)

// ExportOptions configures gosx export.
type ExportOptions struct {
	// Zip writes the bundle as one archive. "-" or "auto" means dist/static.zip.
	Zip string
}

// RunExport builds the app's static bundle into dist/static.
func RunExport(dir string) error {
	return RunExportWithOptions(dir, ExportOptions{})
}

// RunExportWithOptions runs the production build pipeline (TinyGo runtime,
// content-hashed assets, wasm-opt when installed) and keeps its static output,
// so `gosx export` and `gosx build --prod` write the same dist/static. The
// export differs in two ways: it needs no Go server on the host, so a page that
// cannot be prerendered fails the command, and it can write a zip archive.
func RunExportWithOptions(dir string, opts ExportOptions) error {
	absDir, err := canonicalExistingDir(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	if err := env.LoadDir(absDir, ""); err != nil {
		return fmt.Errorf("load env: %w", err)
	}
	buildOpts := BuildOptions{StaticOnly: true}
	if z := strings.TrimSpace(opts.Zip); z != "" {
		if z == "-" || z == "auto" {
			z = filepath.Join(absDir, "dist", "static.zip")
		} else if !filepath.IsAbs(z) {
			if z, err = filepath.Abs(z); err != nil {
				return err
			}
		}
		buildOpts.ZipPath = z
	}
	if err := RunBuildWithOptions(absDir, buildOpts); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "gosx export: static bundle in %s\n", filepath.Join(absDir, "dist", "static"))
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
		if err := copyCompressedSidecars(dst, src); err != nil {
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
	case "/gosx/runtime-core.wasm":
		return filepath.Join(buildDir, "gosx-runtime-core.wasm"), true
	case "/gosx/runtime-engine.wasm":
		return filepath.Join(buildDir, "gosx-runtime-engine.wasm"), true
	case "/gosx/runtime-collab.wasm":
		return filepath.Join(buildDir, "gosx-runtime-collab.wasm"), true
	case "/gosx/wasm_exec.js":
		return filepath.Join(buildDir, "wasm_exec.js"), true
	case "/gosx/standard-go-wasm_exec.js":
		return filepath.Join(buildDir, "standard-go-wasm_exec.js"), true
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
	case "/gosx/bootstrap-feature-controllers.js":
		return filepath.Join(buildDir, "bootstrap-feature-controllers.js"), true
	case "/gosx/bootstrap-controller-input.js":
		return filepath.Join(buildDir, "bootstrap-controller-input.js"), true
	case "/gosx/bootstrap-feature-textlayout.js":
		return filepath.Join(buildDir, "bootstrap-feature-textlayout.js"), true
	case "/gosx/bootstrap-feature-scene3d.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d.js"), true
	case "/gosx/bootstrap-feature-scene3d-command.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-command.js"), true
	case "/gosx/bootstrap-feature-scene3d-hydrate.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-hydrate.js"), true
	case "/gosx/bootstrap-feature-scene3d-webgpu.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-webgpu.js"), true
	case "/gosx/bootstrap-feature-scene3d-webgl.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-webgl.js"), true
	case "/gosx/bootstrap-feature-scene3d-gltf.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-gltf.js"), true
	case "/gosx/bootstrap-feature-scene3d-animation.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-animation.js"), true
	case "/gosx/bootstrap-feature-scene3d-compute.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-compute.js"), true
	case "/gosx/bootstrap-feature-scene3d-decompress.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-decompress.js"), true
	case "/gosx/bootstrap-feature-scene3d-instance-stream.js":
		return filepath.Join(buildDir, "bootstrap-feature-scene3d-instance-stream.js"), true
	case "/gosx/patch.js":
		return filepath.Join(buildDir, "patch.js"), true
	case "/gosx/hls.min.js":
		return filepath.Join(buildDir, "hls.min.js"), true
	case "/gosx/stripe-bridge.js":
		return filepath.Join(buildDir, "stripe-bridge.js"), true
	case "/gosx/relay.js":
		return filepath.Join(buildDir, "relay.js"), true
	}
	// Any other opt-in feature chunk stages under its own file name.
	if file, ok := strings.CutPrefix(ref, "/gosx/"); ok {
		if _, valid := buildmanifest.FeatureChunkName(file); valid {
			return filepath.Join(buildDir, file), true
		}
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
