package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
)

func TestGoWASMConfigValidation(t *testing.T) {
	for _, tc := range []struct{ name, pkg string }{
		{"", "./cmd/browser"}, {"../escape", "."}, {"Upper", "."}, {"bad.name", "."},
		{"ok", ""}, {"ok", "/tmp/main"}, {"ok", "../main"}, {"ok", "./../main"},
		{"ok", "./cmd/../../main"}, {"ok", "./cmd/../main"}, {"ok", "./cmd/..."},
		{"ok", "-o"}, {"ok", "./main -o /tmp/escape"}, {"ok", `./cmd\browser`},
		{"ok", "example.com/remote"}, {"ok", "./cmd/"},
	} {
		t.Run(tc.name+":"+tc.pkg, func(t *testing.T) {
			dir := t.TempDir()
			config, _ := json.Marshal(map[string]any{"build": map[string]any{"goWASM": map[string]string{tc.name: tc.pkg}}})
			mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), string(config))
			if _, err := loadProjectConfig(dir); err == nil {
				t.Fatal("invalid Go WASM configuration accepted")
			}
		})
	}
	for _, config := range []string{`{}`, `{"build":{"goWASM":{"controls":"./app/cmd/controls","root_entry":"."}}}`} {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), config)
		if _, err := loadProjectConfig(dir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoWASMServerStripConfigAndArgs(t *testing.T) {
	for _, strip := range []bool{false, true} {
		dir := t.TempDir()
		data, _ := json.Marshal(map[string]any{"build": map[string]any{"server": map[string]bool{"strip": strip}}})
		mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), string(data))
		cfg, err := loadProjectConfig(dir)
		if err != nil || cfg.Build.Server.Strip != strip {
			t.Fatalf("strip configuration: %+v, %v", cfg.Build.Server, err)
		}
		args := goServerBuildArgsWithOptions("dist/server/app", cfg.Build.Server)
		if slices.Contains(args, "-ldflags=-s -w") != strip || args[len(args)-1] != "." {
			t.Fatalf("server arguments: %v", args)
		}
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{"build":{"server":{"strip":"true"}}}`)
	if _, err := loadProjectConfig(dir); err == nil {
		t.Fatal("non-boolean server strip accepted")
	}
}

func TestGoWASMBuildRejectsMissingNonMainAndEscapingPackages(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/browser\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "lib", "lib.go"), "package lib\n")
	mustWriteFile(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n")
	for _, pkg := range []string{"./missing", "./lib", "./main.go"} {
		if _, err := buildGoWASMAssets(dir, filepath.Join(dir, "dist"), map[string]string{"test": pkg}); err == nil {
			t.Fatalf("accepted package %q", pkg)
		}
	}
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "main.go"), "package main\nfunc main() {}\n")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := buildGoWASMAssets(dir, filepath.Join(dir, "dist"), map[string]string{"test": "./escape"}); err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("outside symlink result: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist")); !os.IsNotExist(err) {
		t.Fatal("invalid package created output artifacts")
	}
}

func TestGoWASMBuildCompilesHashesCompressesAndReports(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir()) // Project and relative output have different parents.
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/browser\n\ngo 1.24\n")
	// Only js/wasm source exists, so a host build or wrong compiler target fails.
	source := filepath.Join(dir, "cmd", "browser", "main_js.go")
	dist := "dist"
	var previous HashedAsset
	for _, message := range []string{"first build", "changed source"} {
		mustWriteFile(t, source, "//go:build js && wasm\n\npackage main\nfunc main() { println("+`"`+message+`"`+") }\n")
		assets, err := buildGoWASMAssets(dir, dist, map[string]string{"controls": "./cmd/browser"})
		if err != nil {
			t.Fatal(err)
		}
		asset := assets["controls"]
		data, err := os.ReadFile(filepath.Join(dist, "assets", "go-wasm", asset.File))
		if err != nil || !bytes.HasPrefix(data, []byte("\x00asm\x01\x00\x00\x00")) {
			t.Fatalf("not a standard Go WASM module: %v", err)
		}
		if asset.Hash != buildmanifest.ContentHash(data) || asset.Size != int64(len(data)) || asset.File == previous.File {
			t.Fatalf("hash/size/source change not reflected: %+v", asset)
		}
		previous = asset
		for _, ext := range []string{".br", ".gz"} {
			compressed, err := os.ReadFile(filepath.Join(dist, "assets", "go-wasm", asset.File) + ext)
			if err != nil || len(compressed) >= len(data) {
				t.Fatalf("missing or ineffective compressed sidecar %s: %v", ext, err)
			}
			var reader io.Reader = brotli.NewReader(bytes.NewReader(compressed))
			if ext == ".gz" {
				gz, err := gzip.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				reader = gz
			}
			decoded, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(decoded, data) {
				t.Fatalf("sidecar %s does not match WASM: %v", ext, err)
			}
		}
		if _, err := writeBuildManifest(dist, &BuildManifest{GoWASM: assets}); err != nil {
			t.Fatal(err)
		}
	}
	// The manifest contains no source-machine paths for module resolution.
	relocated := filepath.Join(t.TempDir(), "bundle")
	if err := os.Rename(dist, relocated); err != nil {
		t.Fatal(err)
	}
	manifest, err := buildmanifest.Load(filepath.Join(relocated, "build.json"))
	if err != nil || manifest.GoWASMURL("/gosx/assets", "controls") != "/gosx/assets/go-wasm/"+previous.File {
		t.Fatalf("relocated module resolution: %v", err)
	}
	report, err := buildSizeReport(relocated)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Assets) != 1 || report.Assets[0].Name != "go-wasm:controls" || report.TotalBytes != previous.Size || report.TotalBrotli == 0 || report.ColdStartBytes != 0 {
		t.Fatalf("application module missing or counted as unconditional runtime: %+v", report)
	}
	if info, err := os.Stat(filepath.Join(report.RuntimeDir, report.Assets[0].File)); err != nil || info.Size() != previous.Size {
		t.Fatalf("reported asset path is not usable: %v", err)
	}
	entries, err := os.ReadDir(relocated)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".go-wasm-") {
			t.Fatal("temporary compiler output survived")
		}
	}
}

func TestGoWASMBuildRetainsCompilerDiagnostic(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/broken\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "main_js.go"), "package main\nfunc main() { missingFunction() }\n")
	_, err := buildGoWASMAssets(dir, filepath.Join(dir, "dist"), map[string]string{"broken": "."})
	if err == nil || !strings.Contains(err.Error(), "undefined: missingFunction") || !strings.Contains(err.Error(), `build.goWASM "broken"`) {
		t.Fatalf("compiler diagnostic lost: %v", err)
	}
}

func TestGoWASMShimUsesTargetProjectToolchain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture go command uses a POSIX shell")
	}
	project, toolchain, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Chdir(t.TempDir())
	const shim = "target project toolchain wasm_exec"
	mustWriteFile(t, filepath.Join(toolchain, "lib", "wasm", "wasm_exec.js"), shim)
	goCommand := filepath.Join(bin, "go")
	mustWriteFile(t, goCommand, `#!/bin/sh
test "$PWD" = "$TEST_PROJECT" || exit 21
test "$1 $2" = "env GOROOT" || exit 22
test "$GOOS $GOARCH $GOWORK $CGO_ENABLED" = "js wasm off 0" || exit 23
printf '%s\n' "$TEST_TOOLCHAIN"
`)
	if err := os.Chmod(goCommand, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEST_PROJECT", project)
	t.Setenv("TEST_TOOLCHAIN", toolchain)
	data, err := readProjectStandardGoWASMExec(project)
	if err != nil || string(data) != shim {
		t.Fatalf("wrong project/toolchain shim: %q, %v", data, err)
	}
}

func TestGoWASMAbsentConfigKeepsOutputUnchanged(t *testing.T) {
	dist := filepath.Join(t.TempDir(), "uncreated")
	assets, err := buildGoWASMAssets("missing-project", dist, nil)
	if err != nil || assets != nil {
		t.Fatalf("absent config: %v, %v", assets, err)
	}
	if _, err := os.Stat(dist); !os.IsNotExist(err) {
		t.Fatal("unconfigured build created module output")
	}
}
