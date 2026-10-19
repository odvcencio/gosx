//go:build !js || !wasm

package worklet

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func browserGoCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") &&
			!strings.HasPrefix(entry, "CGO_ENABLED=") && !strings.HasPrefix(entry, "GOWORK=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOWORK=off")
	return cmd
}

func TestBrowserWorkletExcludesServerDependencies(t *testing.T) {
	cmd := browserGoCommand("list", "-deps", "-json", "m31labs.dev/gosx/game/audio/host", "m31labs.dev/gosx/game/audio/worklet")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser dependency graph: %v\n%s", err, output)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	foundWorklet := false
	for {
		var pkg struct {
			ImportPath string
			GoFiles    []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if pkg.ImportPath == "net/http" || pkg.ImportPath == "crypto/tls" || pkg.ImportPath == "crypto/x509" {
			t.Fatalf("browser audio host imports server dependency %s", pkg.ImportPath)
		}
		if pkg.ImportPath == "m31labs.dev/gosx/game/audio/worklet" {
			foundWorklet = true
			for _, file := range pkg.GoFiles {
				if file == "asset_server.go" {
					t.Fatal("HTTP asset delivery participates in browser worklet package")
				}
			}
		}
	}
	if !foundWorklet {
		t.Fatal("browser host did not import the worklet ABI")
	}
}

func TestUnusedBrowserAudioHostDoesNotRetainProcessor(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	if err := os.WriteFile(main, []byte("package main\nimport _ \"m31labs.dev/gosx/game/audio/host\"\nfunc main(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "probe.wasm")
	cmd := browserGoCommand("build", "-trimpath", "-ldflags=-s -w", "-o", outputPath, main)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser audio probe: %v\n%s", err, output)
	}
	module, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(module, Source()) {
		t.Fatal("unused browser audio host retained the embedded worklet processor")
	}
}
