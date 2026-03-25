package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// RunBuild orchestrates the full GoSX build pipeline.
func RunBuild(dir string, dev bool) error {
	buildDir := filepath.Join(dir, "build")

	// Create output directories
	for _, d := range []string{
		buildDir,
		filepath.Join(buildDir, "islands"),
		filepath.Join(buildDir, "css"),
	} {
		os.MkdirAll(d, 0755)
	}

	// 1. Scan for .gsx files
	var gsxFiles []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasSuffix(path, ".gsx") {
			gsxFiles = append(gsxFiles, path)
		}
		return nil
	})

	fmt.Printf("Found %d .gsx files\n", len(gsxFiles))

	// 2-4. Parse, lower, validate each file
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

		// Check for island components
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

	// Sidecar CSS: for each .gsx file, check for matching .css
	var cssFiles []struct {
		source    string
		component string
		hash      string
	}

	for _, gsxFile := range gsxFiles {
		cssFile := strings.TrimSuffix(gsxFile, ".gsx") + ".css"
		if data, err := os.ReadFile(cssFile); err == nil {
			h := sha256.Sum256(data)
			hash := hex.EncodeToString(h[:4])
			component := strings.TrimSuffix(filepath.Base(gsxFile), ".gsx")

			// Copy to build/css/ with hashed name
			outName := fmt.Sprintf("%s.%s.css", component, hash)
			outPath := filepath.Join(buildDir, "css", outName)
			os.WriteFile(outPath, data, 0644)

			cssFiles = append(cssFiles, struct {
				source    string
				component string
				hash      string
			}{cssFile, component, hash})

			fmt.Printf("  CSS: %s → %s (%d bytes)\n", filepath.Base(cssFile), outName, len(data))
		}
	}

	// 5. Serialize island programs
	format := "json"
	ext := ".json"
	if !dev {
		format = "bin"
		ext = ".bin"
	}

	for _, prog := range islandProgs {
		var data []byte
		var err error
		if dev {
			data, err = program.EncodeJSON(prog)
		} else {
			data, err = program.EncodeBinary(prog)
		}
		if err != nil {
			return fmt.Errorf("serialize %s: %w", prog.Name, err)
		}

		outPath := filepath.Join(buildDir, "islands", prog.Name+ext)
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}

		hash := sha256.Sum256(data)
		fmt.Printf("  Island: %s (%d bytes, hash=%s, format=%s)\n",
			prog.Name, len(data), hex.EncodeToString(hash[:4]), format)
	}

	// 6. Copy bootstrap.js and patch.js
	jsDir := filepath.Join(buildDir)
	copyEmbeddedJS(jsDir)

	// 7. Build shared WASM runtime
	wasmPath := filepath.Join(buildDir, "gosx-runtime.wasm")
	fmt.Println("Building shared WASM runtime...")

	cmd := exec.Command("go", "build", "-o", wasmPath, "./client/wasm/")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Dir = findModuleRoot(dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("  WASM build failed: %v (this is expected if not in the gosx module)\n", err)
		fmt.Println("  To build the WASM runtime manually:")
		fmt.Println("    GOOS=js GOARCH=wasm go build -o build/gosx-runtime.wasm ./client/wasm/")
	} else {
		info, _ := os.Stat(wasmPath)
		if info != nil {
			fmt.Printf("  Runtime: gosx-runtime.wasm (%d bytes)\n", info.Size())
		}
	}

	// 8. Copy wasm_exec.js from GOROOT
	wasmExecSrc := filepath.Join(os.Getenv("GOROOT"), "misc", "wasm", "wasm_exec.js")
	wasmExecDst := filepath.Join(buildDir, "wasm_exec.js")
	if data, err := os.ReadFile(wasmExecSrc); err == nil {
		os.WriteFile(wasmExecDst, data, 0644)
		fmt.Printf("  Copied wasm_exec.js (%d bytes)\n", len(data))
	} else {
		// Try alternate location
		goroot := getGOROOT()
		wasmExecSrc = filepath.Join(goroot, "misc", "wasm", "wasm_exec.js")
		if data, err := os.ReadFile(wasmExecSrc); err == nil {
			os.WriteFile(wasmExecDst, data, 0644)
			fmt.Printf("  Copied wasm_exec.js (%d bytes)\n", len(data))
		} else {
			fmt.Printf("  Warning: could not find wasm_exec.js\n")
		}
	}

	// 9. Summary
	fmt.Printf("\nBuild complete: %s\n", buildDir)
	fmt.Printf("  Islands: %d\n", len(islandProgs))
	fmt.Printf("  Format: %s\n", format)
	fmt.Printf("  CSS files: %d\n", len(cssFiles))

	return nil
}

func copyEmbeddedJS(dir string) {
	// The JS files are in client/js/ relative to the module root
	// For now, just note their locations
	fmt.Println("  JS assets: client/js/bootstrap.js, client/js/patch.js")
	fmt.Println("  Serve these from /gosx/ in your app")
}

func findModuleRoot(dir string) string {
	d := dir
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
