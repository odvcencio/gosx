//go:build !js || !wasm

package audio

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// jsDeps returns the GOOS=js GOARCH=wasm dependency set of pkgs.
func jsDeps(t *testing.T, pkgs ...string) map[string]bool {
	t.Helper()
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append([]string{"list", "-deps"}, pkgs...)...)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOOS=") && !strings.HasPrefix(e, "GOARCH=") && !strings.HasPrefix(e, "GOWORK=") && !strings.HasPrefix(e, "CGO_ENABLED=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	set := map[string]bool{}
	for _, pkg := range strings.Fields(string(out)) {
		set[pkg] = true
	}
	return set
}

// TestBrowserBuildLinksNoServerDependencies checks the audio packages for
// GOOS=js GOARCH=wasm. A game's wasm client imports them, so none may pull in
// the HTTP stack, TLS or the hub through audio's own imports (the clip loader
// uses fetch, not net/http). The root game package, which audio imports for
// AudioManifest, is checked by its own import diet (C1); until that lands the
// test excludes what game itself brings, and it becomes a strict check on its
// own once game is clean.
func TestBrowserBuildLinksNoServerDependencies(t *testing.T) {
	forbidden := []string{"net/http", "crypto/tls", "crypto/x509", "m31labs.dev/gosx/hub"}
	all := jsDeps(t, "m31labs.dev/gosx/game/audio", "m31labs.dev/gosx/game/audio/host", "m31labs.dev/gosx/game/audio/kernel")
	game := jsDeps(t, "m31labs.dev/gosx/game")
	for _, pkg := range forbidden {
		if all[pkg] && !game[pkg] {
			t.Errorf("the js/wasm audio build depends on %s through its own imports", pkg)
		}
		if all[pkg] && game[pkg] {
			t.Logf("note: %s is still reached through package game (C1 import diet)", pkg)
		}
	}
}
