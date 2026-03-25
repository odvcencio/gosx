package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// BuildManifest describes all build outputs for deployment.
// The server reads this at startup to generate page manifests with
// correct content-hashed asset URLs.
type BuildManifest struct {
	// Runtime assets (Tier 2: immutable, CDN-cached)
	Runtime RuntimeAssets `json:"runtime"`

	// Island programs (Tier 3: immutable, CDN-cached, per-island)
	Islands []IslandAsset `json:"islands"`

	// CSS assets (Tier 3: immutable, CDN-cached)
	CSS []CSSAsset `json:"css"`
}

type RuntimeAssets struct {
	WASM      HashedAsset `json:"wasm"`
	WASMExec  HashedAsset `json:"wasmExec"`
	Bootstrap HashedAsset `json:"bootstrap"`
	Patch     HashedAsset `json:"patch"`
}

type IslandAsset struct {
	Name   string `json:"name"`
	Format string `json:"format"` // "json" or "bin"
	HashedAsset
}

type CSSAsset struct {
	Component string `json:"component"`
	HashedAsset
}

type HashedAsset struct {
	File string `json:"file"` // content-hashed filename
	Hash string `json:"hash"` // sha256 short hash
	Size int64  `json:"size"` // bytes
}

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
		os.MkdirAll(d, 0755)
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
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip dist/ and build/ directories
		if info.IsDir() && (info.Name() == "dist" || info.Name() == "build") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".gsx") {
			gsxFiles = append(gsxFiles, path)
		}
		return nil
	})

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

	// Build WASM
	wasmTmp := filepath.Join(distDir, "gosx-runtime.wasm.tmp")
	cmd := exec.Command("go", "build", "-o", wasmTmp, "./client/wasm/")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Dir = findModuleRoot(dir)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("    WASM build failed: %v\n", err)
		fmt.Println("    Run manually: GOOS=js GOARCH=wasm go build -o dist/assets/runtime/gosx-runtime.wasm ./client/wasm/")
	} else {
		wasmData, _ := os.ReadFile(wasmTmp)
		asset, _ := writeHashed(runtimeDir, "gosx-runtime", ".wasm", wasmData)
		manifest.Runtime.WASM = asset
		os.Remove(wasmTmp)
		fmt.Printf("    %s (%d bytes, %.0fKB gzipped est.)\n", asset.File, asset.Size, float64(asset.Size)*0.29/1024)
	}

	// wasm_exec.js
	goroot := getGOROOT()
	for _, tryPath := range []string{
		filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
		filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
	} {
		if data, err := os.ReadFile(tryPath); err == nil {
			asset, _ := writeHashed(runtimeDir, "wasm_exec", ".js", data)
			manifest.Runtime.WASMExec = asset
			fmt.Printf("    %s (%d bytes)\n", asset.File, asset.Size)
			break
		}
	}

	// bootstrap.js and patch.js
	moduleRoot := findModuleRoot(dir)
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
			fmt.Printf("    Warning: %s not found at %s\n", js.name, js.path)
			continue
		}
		asset, _ := writeHashed(runtimeDir, js.name, ".js", data)
		*js.dest = asset
		fmt.Printf("    %s (%d bytes)\n", asset.File, asset.Size)
	}

	// ── Build manifest ──────────────────────────────────────────────────

	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(distDir, "build.json")
	os.WriteFile(manifestPath, manifestJSON, 0644)

	// ── Summary ─────────────────────────────────────────────────────────

	fmt.Println("\n─────────────────────────────────")
	fmt.Printf("Build complete: %s\n\n", distDir)
	fmt.Printf("  Tier 1 (server):  deploy Go binary, mutable\n")
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

	return nil
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
