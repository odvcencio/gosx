//go:build !js || !wasm

package host_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserHostHasNoServerDependencies(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./testdata/browser")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list WASM dependencies: %v\n%s", err, output)
	}
	for _, dep := range strings.Fields(string(output)) {
		if dep == "net/http" || dep == "crypto/tls" || dep == "m31labs.dev/gosx" || dep == "m31labs.dev/gosx/game" ||
			strings.HasPrefix(dep, "m31labs.dev/gosx/hub") || strings.HasPrefix(dep, "github.com/odvcencio/gotreesitter") {
			t.Errorf("browser host unexpectedly imports %s", dep)
		}
	}
	cmd = exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "browser.wasm"), "./testdata/browser")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build host-only WASM engine: %v\n%s", err, output)
	}
}
