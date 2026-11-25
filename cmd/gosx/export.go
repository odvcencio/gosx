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
	// Out also writes the static bundle to this folder. The folder must be
	// missing, empty, or a previous export (it holds index.html); its old
	// contents are replaced. Relative paths are relative to the current
	// directory.
	Out string
	// Precompressed keeps the .gz and .br copies in the exported folder (the
	// Out folder when set, else dist/static). Nil means true.
	Precompressed *bool
	// SingleFile also writes the bundle as one HTML file that runs from disk.
	// "auto" means dist/static.html.
	SingleFile string
	// GoTags are extra Go build tags for the project's build.goWASM entries only.
	GoTags []string
}

func (o ExportOptions) keepPrecompressed() bool {
	return o.Precompressed == nil || *o.Precompressed
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
	buildOpts := BuildOptions{StaticOnly: true, GoTags: opts.GoTags}
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
	staticDir := filepath.Join(absDir, "dist", "static")
	outDir := ""
	if o := strings.TrimSpace(opts.Out); o != "" {
		if outDir, err = filepath.Abs(o); err != nil {
			return err
		}
		// Fail before the build, which takes minutes, not after it.
		if err := checkExportOutDir(outDir, absDir); err != nil {
			return err
		}
	}
	singlePath := ""
	if s := strings.TrimSpace(opts.SingleFile); s != "" {
		singlePath = filepath.Join(absDir, "dist", "static.html")
		if s != "auto" {
			if singlePath, err = filepath.Abs(s); err != nil {
				return err
			}
		}
	}
	if err := RunBuildWithOptions(absDir, buildOpts); err != nil {
		return err
	}
	if singlePath != "" {
		res, err := writeSingleFile(staticDir, singlePath)
		if err != nil {
			return err
		}
		if len(res.OtherPages) > 0 {
			fmt.Fprintf(os.Stderr, "gosx export: warning: a single file holds one page; left out of %s: %s\n",
				singlePath, strings.Join(res.OtherPages, ", "))
		}
		fmt.Fprintf(os.Stderr, "gosx export: single file %s (%d files, %d bytes)\n", singlePath, res.Files, res.Bytes)
	}
	shown := staticDir
	switch {
	case outDir != "":
		if err := checkExportOutDir(outDir, absDir); err != nil {
			return err
		}
		if _, err := copyStaticBundle(staticDir, outDir, opts.keepPrecompressed()); err != nil {
			return err
		}
		shown = outDir
	case !opts.keepPrecompressed():
		if err := removePrecompressed(staticDir); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "gosx export: static bundle in %s\n", shown)
	return nil
}

// checkExportOutDir decides whether --out may write into out. It accepts a
// missing folder, an empty folder, and a folder that already holds an export
// (it has index.html). It refuses anything else, so a mistyped path cannot
// wipe a source folder, and it refuses the project folder, its parents, and
// the build's own dist/static.
func checkExportOutDir(out, projectDir string) error {
	staticDir := filepath.Join(projectDir, "dist", "static")
	if out == projectDir || strings.HasPrefix(projectDir, out+string(filepath.Separator)) {
		return fmt.Errorf("--out %s is the project folder or one of its parents", out)
	}
	if out == staticDir || strings.HasPrefix(out, staticDir+string(filepath.Separator)) {
		return fmt.Errorf("--out %s is inside the build's own dist/static; choose another folder", out)
	}
	info, err := os.Stat(out)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("--out %s is a file, not a folder", out)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		return fmt.Errorf("--out %s is not empty and has no index.html, so it is not a previous export; use an empty folder or a previous export", out)
	}
	if _, err := os.Stat(filepath.Join(out, "go.mod")); err == nil {
		return fmt.Errorf("--out %s holds a go.mod, so it looks like a source folder, not an export", out)
	}
	return nil
}

// copyStaticBundle replaces the contents of dst with the files of src and
// returns the number of files written. With precompressed false it leaves out
// each .gz and .br copy whose original sits beside it.
func copyStaticBundle(src, dst string, precompressed bool) (int, error) {
	if entries, err := os.ReadDir(dst); err == nil {
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil {
				return 0, err
			}
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	n := 0
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !precompressed && isPrecompressedCopy(p) {
			return nil
		}
		n++
		return copyFile(filepath.Join(dst, rel), p)
	})
	return n, err
}

// removePrecompressed deletes the .gz and .br copies in dir, keeping a file
// whose original is missing (a real .gz asset of the app).
func removePrecompressed(dir string) error {
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isPrecompressedCopy(p) {
			return err
		}
		return os.Remove(p)
	})
}

// isPrecompressedCopy reports whether p is the .gz or .br sidecar of a file
// that exists next to it.
func isPrecompressedCopy(p string) bool {
	ext := filepath.Ext(p)
	if ext != ".gz" && ext != ".br" {
		return false
	}
	info, err := os.Stat(strings.TrimSuffix(p, ext))
	return err == nil && info.Mode().IsRegular()
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
