package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
)

func validateGoWASMEntries(entries map[string]string) error {
	for name, pkg := range entries {
		if !buildmanifest.ValidGoWASMName(name) {
			return fmt.Errorf("build.goWASM: invalid name %q (use a-z followed by a-z, 0-9, _ or -)", name)
		}
		rel := strings.TrimPrefix(pkg, "./")
		if pkg != "." && !strings.HasPrefix(pkg, "./") ||
			!filepath.IsLocal(rel) || path.Clean(rel) != rel ||
			strings.ContainsAny(pkg, "\\ \t\r\n\x00") || strings.Contains(pkg, "...") {
			return fmt.Errorf("build.goWASM %q: package must be one local directory, such as ./cmd/browser", name)
		}
	}
	return nil
}

// buildGoWASMAssets compiles explicit entry packages without running them.
// The current go executable/toolchain supplies both the module and the standard
// wasm_exec loader emitted by the surrounding build; TinyGo is not used here.
func buildGoWASMAssets(projectDir, distDir string, entries map[string]string) (map[string]HashedAsset, error) {
	if err := validateGoWASMEntries(entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	distDir, err := filepath.Abs(distDir)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return nil, fmt.Errorf("Go WASM project directory: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	env := goWASMBuildEnv()
	// Validate every package before emitting any module. In particular, go build
	// succeeds for a library package but emits an archive rather than WASM.
	for _, name := range names {
		pkg := entries[name]
		dir, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(pkg)))
		if err != nil {
			return nil, fmt.Errorf("build.goWASM %q package %q: %w", name, pkg, err)
		}
		if !isPathWithin(dir, root) {
			return nil, fmt.Errorf("build.goWASM %q: package resolves outside the project", name)
		}
		cmd := exec.Command("go", "list", "-json", pkg)
		cmd.Dir, cmd.Env = root, env
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("build.goWASM %q package %q: %w: %s", name, pkg, err, strings.TrimSpace(stderr.String()))
		}
		var info struct{ Name, Dir string }
		if err := json.Unmarshal(output, &info); err != nil {
			return nil, fmt.Errorf("build.goWASM %q package metadata: %w", name, err)
		}
		infoDir, err := filepath.EvalSymlinks(info.Dir)
		if err != nil || info.Name != "main" || filepath.Clean(infoDir) != filepath.Clean(dir) {
			return nil, fmt.Errorf("build.goWASM %q: %q must be a main package directory", name, pkg)
		}
	}
	assetDir := filepath.Join(distDir, "assets", "go-wasm")
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp(distDir, ".go-wasm-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	assets := make(map[string]HashedAsset, len(entries))
	for _, name := range names {
		target := filepath.Join(tempDir, name+".wasm")
		cmd := exec.Command("go", "build", "-trimpath", "-o", target, entries[name])
		cmd.Dir, cmd.Env = root, env
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build.goWASM %q: %w: %s", name, err, strings.TrimSpace(string(output)))
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(data, []byte("\x00asm\x01\x00\x00\x00")) {
			return nil, fmt.Errorf("build.goWASM %q: compiler did not emit a WASM module", name)
		}
		asset, err := writeHashed(assetDir, name, ".wasm", data)
		if err != nil {
			return nil, fmt.Errorf("build.goWASM %q assets: %w", name, err)
		}
		assets[name] = asset
		fmt.Printf("    Go WASM: %s → %s (%d bytes)\n", name, asset.File, asset.Size)
	}
	return assets, nil
}

func goWASMBuildEnv() []string {
	env := execEnvWithoutGoFlags()
	for key, value := range map[string]string{"GOOS": "js", "GOARCH": "wasm", "CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": goModuleCommandFlags} {
		env = setEnv(env, key, value)
	}
	return env
}

func readProjectStandardGoWASMExec(projectDir string) ([]byte, error) {
	cmd := exec.Command("go", "env", "GOROOT")
	cmd.Dir, cmd.Env = projectDir, goWASMBuildEnv()
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("resolve application Go WASM toolchain: %w", err)
	}
	return readGoWASMExec(strings.TrimSpace(string(output)))
}
