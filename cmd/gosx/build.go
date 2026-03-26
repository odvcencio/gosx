package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// BuildManifest describes all build outputs for deployment.
// The server reads this at startup to generate page manifests with
// correct content-hashed asset URLs.
type BuildManifest = buildmanifest.Manifest
type RuntimeAssets = buildmanifest.RuntimeAssets
type IslandAsset = buildmanifest.IslandAsset
type CSSAsset = buildmanifest.CSSAsset
type HashedAsset = buildmanifest.HashedAsset

// contentHash returns the first 8 hex chars of sha256.
func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

// writeHashed writes data to dir/name.hash.ext and returns the asset info.
func writeHashed(dir, name, ext string, data []byte) (HashedAsset, error) {
	hash := contentHash(data)
	filename := fmt.Sprintf("%s.%s%s", name, hash, ext)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return HashedAsset{}, err
	}
	return HashedAsset{
		File: filename,
		Hash: hash,
		Size: int64(len(data)),
	}, nil
}

// RunBuild orchestrates the full GoSX build pipeline.
//
// Output structure (prod):
//
//	dist/
//	  build.json                              ← build manifest
//	  server/app                              ← Go binary (Tier 1: mutable)
//	  assets/
//	    runtime/
//	      gosx-runtime.<hash>.wasm            ← Tier 2: immutable
//	      wasm_exec.<hash>.js
//	      bootstrap.<hash>.js
//	      patch.<hash>.js
//	    islands/
//	      Counter.<hash>.gxi                  ← Tier 3: immutable
//	      Tabs.<hash>.gxi
//	    css/
//	      counter.<hash>.css
//
// Deploy: server binary rolls often. Runtime/island/CSS assets are
// content-hashed and CDN-cached with Cache-Control: immutable.
func RunBuild(dir string, dev bool) error {
	distDir := filepath.Join(dir, "dist")
	runtimeDir := filepath.Join(distDir, "assets", "runtime")
	islandDir := filepath.Join(distDir, "assets", "islands")
	cssDir := filepath.Join(distDir, "assets", "css")

	for _, d := range []string{runtimeDir, islandDir, cssDir, filepath.Join(distDir, "server")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create output directory %s: %w", d, err)
		}
	}

	manifest := BuildManifest{}
	mode := "prod"
	if dev {
		mode = "dev"
	}

	fmt.Printf("GoSX build (%s)\n", mode)
	fmt.Println("─────────────────────────────────")

	// ── Tier 1: Compile .gsx files ──────────────────────────────────────

	var gsxFiles []string
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip dist/ and build/ directories
		if info.IsDir() && (info.Name() == "dist" || info.Name() == "build") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".gsx") {
			gsxFiles = append(gsxFiles, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk source tree: %w", err)
	}

	fmt.Printf("  Sources: %d .gsx files\n", len(gsxFiles))

	var islandProgs []*program.Program

	for _, file := range gsxFiles {
		source, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		irProg, err := gosx.Compile(source)
		if err != nil {
			return fmt.Errorf("compile %s: %w", file, err)
		}

		for i, comp := range irProg.Components {
			if comp.IsIsland {
				island, err := ir.LowerIsland(irProg, i)
				if err != nil {
					return fmt.Errorf("lower island %s in %s: %w", comp.Name, file, err)
				}
				islandProgs = append(islandProgs, island)
			}
		}
	}

	// ── Tier 3: Island programs (content-hashed) ────────────────────────

	fmt.Println("\n  Islands:")
	islandFormat := "json"
	islandExt := ".gxi" // GoSX Island — binary prod format
	if dev {
		islandExt = ".json"
	}

	for _, prog := range islandProgs {
		var data []byte
		var err error
		if dev {
			data, err = program.EncodeJSON(prog)
			islandFormat = "json"
		} else {
			data, err = program.EncodeBinary(prog)
			islandFormat = "bin"
		}
		if err != nil {
			return fmt.Errorf("serialize %s: %w", prog.Name, err)
		}

		asset, err := writeHashed(islandDir, prog.Name, islandExt, data)
		if err != nil {
			return fmt.Errorf("write island %s: %w", prog.Name, err)
		}

		manifest.Islands = append(manifest.Islands, IslandAsset{
			Name:        prog.Name,
			Format:      islandFormat,
			HashedAsset: asset,
		})

		fmt.Printf("    %s → %s (%d bytes)\n", prog.Name, asset.File, asset.Size)
	}

	// ── Tier 3: Sidecar CSS (content-hashed) ────────────────────────────

	for _, gsxFile := range gsxFiles {
		cssFile := strings.TrimSuffix(gsxFile, ".gsx") + ".css"
		data, err := os.ReadFile(cssFile)
		if err != nil {
			continue
		}

		component := strings.TrimSuffix(filepath.Base(gsxFile), ".gsx")
		asset, err := writeHashed(cssDir, component, ".css", data)
		if err != nil {
			return fmt.Errorf("write CSS %s: %w", component, err)
		}

		manifest.CSS = append(manifest.CSS, CSSAsset{
			Component:   component,
			HashedAsset: asset,
		})

		fmt.Printf("    CSS: %s → %s (%d bytes)\n", component, asset.File, asset.Size)
	}

	// ── Tier 2: Shared runtime (content-hashed) ─────────────────────────

	fmt.Println("\n  Runtime:")

	// Build WASM — try TinyGo first (smaller binary), fall back to standard Go
	wasmTmp := filepath.Join(distDir, "gosx-runtime.wasm.tmp")
	moduleRoot := findModuleRoot(dir)
	usedTinyGo := false

	tinygoPath, tinygoErr := exec.LookPath("tinygo")
	if tinygoErr == nil && !dev {
		// TinyGo available and prod mode — use it for smaller WASM
		fmt.Println("    Using TinyGo for smaller WASM binary...")
		cmd := exec.Command(tinygoPath, "build", "-target", "wasm", "-o", wasmTmp, "./client/wasm/")
		cmd.Dir = moduleRoot
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			usedTinyGo = true
			// Try wasm-opt if available
			woptPath, woptErr := exec.LookPath("wasm-opt")
			if woptErr == nil {
				optTmp := wasmTmp + ".opt"
				optCmd := exec.Command(woptPath, "-Oz", wasmTmp, "-o", optTmp)
				if optCmd.Run() == nil {
					if err := os.Rename(optTmp, wasmTmp); err != nil {
						return fmt.Errorf("rename optimized wasm: %w", err)
					}
					fmt.Println("    Applied wasm-opt -Oz")
				}
			}
		} else {
			fmt.Printf("    TinyGo build failed, falling back to standard Go: %v\n", err)
		}
	}

	if !usedTinyGo {
		cmd := exec.Command("go", "build", "-o", wasmTmp, "./client/wasm/")
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		cmd.Dir = moduleRoot
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go wasm build failed: %w", err)
		}
	}

	wasmData, err := os.ReadFile(wasmTmp)
	if err != nil {
		return fmt.Errorf("read compiled WASM: %w", err)
	}
	asset, err := writeHashed(runtimeDir, "gosx-runtime", ".wasm", wasmData)
	if err != nil {
		return fmt.Errorf("write wasm asset: %w", err)
	}
	manifest.Runtime.WASM = asset
	compiler := "Go"
	if usedTinyGo {
		compiler = "TinyGo"
	}
	gzEst := gzip_c_len(wasmData)
	fmt.Printf("    %s (%d bytes, %dKB gz, built with %s)\n", asset.File, asset.Size, gzEst/1024, compiler)
	if err := os.Remove(wasmTmp); err != nil {
		return fmt.Errorf("remove temporary WASM artifact: %w", err)
	}

	// wasm_exec.js — use TinyGo's version if we built with TinyGo
	wasmExecFound := false
	if usedTinyGo {
		// TinyGo has its own wasm_exec.js
		out, err := exec.Command(tinygoPath, "env", "TINYGOROOT").Output()
		if err == nil {
			tinygoRoot := strings.TrimSpace(string(out))
			tryPath := filepath.Join(tinygoRoot, "targets", "wasm_exec.js")
			if data, err := os.ReadFile(tryPath); err == nil {
				asset, err := writeHashed(runtimeDir, "wasm_exec", ".js", data)
				if err != nil {
					return fmt.Errorf("write wasm_exec.js: %w", err)
				}
				manifest.Runtime.WASMExec = asset
				fmt.Printf("    %s (%d bytes, TinyGo)\n", asset.File, asset.Size)
				wasmExecFound = true
			}
		}
	}
	if !wasmExecFound {
		goroot := getGOROOT()
		for _, tryPath := range []string{
			filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
			filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
		} {
			if data, err := os.ReadFile(tryPath); err == nil {
				asset, err := writeHashed(runtimeDir, "wasm_exec", ".js", data)
				if err != nil {
					return fmt.Errorf("write wasm_exec.js: %w", err)
				}
				manifest.Runtime.WASMExec = asset
				fmt.Printf("    %s (%d bytes)\n", asset.File, asset.Size)
				wasmExecFound = true
				break
			}
		}
	}
	if !wasmExecFound {
		return fmt.Errorf("unable to locate wasm_exec.js")
	}

	// bootstrap.js and patch.js
	for _, js := range []struct {
		name string
		path string
		dest *HashedAsset
	}{
		{"bootstrap", filepath.Join(moduleRoot, "client", "js", "bootstrap.js"), &manifest.Runtime.Bootstrap},
		{"patch", filepath.Join(moduleRoot, "client", "js", "patch.js"), &manifest.Runtime.Patch},
	} {
		data, err := os.ReadFile(js.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", js.path, err)
		}
		asset, err := writeHashed(runtimeDir, js.name, ".js", data)
		if err != nil {
			return fmt.Errorf("write %s: %w", js.name, err)
		}
		*js.dest = asset
		fmt.Printf("    %s (%d bytes)\n", asset.File, asset.Size)
	}

	// ── Build manifest ──────────────────────────────────────────────────

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(distDir, "build.json")
	if err := os.WriteFile(manifestPath, manifestJSON, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Build the application binary when the target directory is a runnable app.
	serverBinaryPath := filepath.Join(distDir, "server", "app")
	builtServer, err := buildServerBinaryIfPresent(dir, serverBinaryPath)
	if err != nil {
		return fmt.Errorf("build server binary: %w", err)
	}
	if err := stageDeploymentBundle(dir, distDir, builtServer, serverBinaryPath); err != nil {
		return fmt.Errorf("stage deployment bundle: %w", err)
	}

	// ── Summary ─────────────────────────────────────────────────────────

	fmt.Println("\n─────────────────────────────────")
	fmt.Printf("Build complete: %s\n\n", distDir)
	fmt.Printf("  Tier 1 (server):  deploy Go binary, mutable\n")
	if builtServer {
		fmt.Printf("  Server binary: %s\n", serverBinaryPath)
		fmt.Printf("  Launch script: %s\n", filepath.Join(distDir, "run.sh"))
	} else {
		fmt.Printf("  Server binary: skipped (target is not a main package)\n")
	}
	fmt.Printf("  Tier 2 (runtime): %d assets, immutable CDN\n", countNonEmpty(
		manifest.Runtime.WASM.File,
		manifest.Runtime.WASMExec.File,
		manifest.Runtime.Bootstrap.File,
		manifest.Runtime.Patch.File,
	))
	fmt.Printf("  Tier 3 (islands): %d programs + %d CSS, immutable CDN\n",
		len(manifest.Islands), len(manifest.CSS))
	fmt.Printf("  Manifest: %s\n", manifestPath)
	fmt.Println("\nDeploy strategy:")
	fmt.Println("  • Server binary rolls often (Tier 1)")
	fmt.Println("  • Runtime assets cached forever with content hashes (Tier 2)")
	fmt.Println("  • Island programs cached forever, invalidated by hash (Tier 3)")
	fmt.Println("  • Manifest tells the server which hashed URLs to reference")
	fmt.Println("  • dist/ includes app/ and public/ for file-routed runtime deployment")

	return nil
}

// gzip_c_len estimates gzipped size.
func gzip_c_len(data []byte) int {
	return int(float64(len(data)) * 0.35)
}

func countNonEmpty(strs ...string) int {
	n := 0
	for _, s := range strs {
		if s != "" {
			n++
		}
	}
	return n
}

func findModuleRoot(dir string) string {
	d, _ := filepath.Abs(dir)
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func getGOROOT() string {
	if gr := os.Getenv("GOROOT"); gr != "" {
		return gr
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return "/usr/local/go"
	}
	return strings.TrimSpace(string(out))
}

func buildServerBinaryIfPresent(dir, outputPath string) (bool, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Name}}", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(string(out)) != "main" {
		return false, nil
	}

	buildCmd := exec.Command("go", "build", "-o", outputPath, ".")
	buildCmd.Dir = dir
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return false, err
	}
	return true, nil
}

func stageDeploymentBundle(projectDir, distDir string, builtServer bool, serverBinaryPath string) error {
	if err := copyDirIfPresent(filepath.Join(projectDir, "app"), filepath.Join(distDir, "app")); err != nil {
		return err
	}
	if err := copyDirIfPresent(filepath.Join(projectDir, "public"), filepath.Join(distDir, "public")); err != nil {
		return err
	}
	if err := copyFileIfPresent(filepath.Join(projectDir, ".env.example"), filepath.Join(distDir, ".env.example")); err != nil {
		return err
	}
	if err := writeBuildReadme(filepath.Join(distDir, "README.md"), builtServer); err != nil {
		return err
	}
	if builtServer {
		if err := writeRunScript(filepath.Join(distDir, "run.sh"), serverBinaryPath); err != nil {
			return err
		}
	}
	return nil
}

func copyDirIfPresent(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(target, path)
	})
}

func copyFileIfPresent(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	return copyFile(dst, src)
}

func writeRunScript(path string, serverBinaryPath string) error {
	relBinary, err := filepath.Rel(filepath.Dir(path), serverBinaryPath)
	if err != nil {
		return err
	}
	script := `#!/usr/bin/env sh
set -eu

DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
export GOSX_APP_ROOT="${GOSX_APP_ROOT:-$DIR}"
exec "$DIR/` + filepath.ToSlash(relBinary) + `" "$@"
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		return err
	}
	return nil
}

func writeBuildReadme(path string, builtServer bool) error {
	lines := []string{
		"# GoSX Build Output",
		"",
		"This directory is a deployable GoSX bundle.",
		"",
		"Contents:",
		"- `assets/` contains immutable hashed runtime, island, and CSS assets.",
		"- `app/` contains runtime file-routed page sources used by `route.AddDir(...)`.",
		"- `public/` contains root-served static assets when present.",
		"- `build.json` maps hashed asset names for runtime/island loading.",
	}
	if builtServer {
		lines = append(lines,
			"- `server/app` is the compiled Go server binary.",
			"- `run.sh` launches the bundle with `GOSX_APP_ROOT` pointing at this directory.",
			"",
			"Run locally:",
			"```sh",
			"./run.sh",
			"```",
			"",
			"Direct binary run:",
			"```sh",
			"GOSX_APP_ROOT=$(pwd) ./server/app",
			"```",
		)
	}
	lines = append(lines,
		"",
		"Deployment notes:",
		"- Keep `assets/` on immutable caching because filenames are content-hashed.",
		"- Roll the server binary independently from hashed assets.",
		"- Provide runtime env vars externally; `.env.example` is copied only when present.",
	)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
