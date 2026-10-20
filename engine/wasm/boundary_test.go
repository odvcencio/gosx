//go:build !js || !wasm

package wasm

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Check the complete browser import graph: an otherwise unused dependency can
// still retain server initialization and increase every authored engine module.
func TestBrowserEngineHasNoGameDriverOrServerDependencies(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./testdata/fixture")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list browser engine dependencies: %v\n%s", err, output)
	}
	for _, dep := range strings.Fields(string(output)) {
		if dep == "net/http" || dep == "crypto/tls" || dep == "m31labs.dev/gosx" ||
			dep == "m31labs.dev/gosx/game" || dep == "m31labs.dev/gosx/game/loop" ||
			strings.HasPrefix(dep, "m31labs.dev/gosx/hub") {
			t.Errorf("browser engine unexpectedly imports %s", dep)
		}
	}
}
