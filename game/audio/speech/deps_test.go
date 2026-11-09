//go:build !js || !wasm

package speech

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBrowserBuildLinksNoTooling checks that the runtime packages a game's
// js/wasm client imports (speech, speech/voice) do not pull in the render
// tooling: no HTTP stack, no os/exec, no voicerender.
func TestBrowserBuildLinksNoTooling(t *testing.T) {
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "list", "-deps",
		"m31labs.dev/gosx/game/audio/speech", "m31labs.dev/gosx/game/audio/speech/voice")
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
	deps := map[string]bool{}
	for _, p := range strings.Fields(string(out)) {
		deps[p] = true
	}
	for _, bad := range []string{"os/exec", "m31labs.dev/gosx/game/audio/speech/voicerender", "m31labs.dev/gosx/game/audio/speech/loudness"} {
		if deps[bad] {
			t.Errorf("the js/wasm speech runtime links %s", bad)
		}
	}
	// net/http is allowed only if package game already brings it (C1 import diet pending).
	if deps["net/http"] {
		game := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "list", "-deps", "m31labs.dev/gosx/game")
		game.Env = cmd.Env
		g, _ := game.CombinedOutput()
		if !strings.Contains(string(g), "\nnet/http\n") {
			t.Error("the js/wasm speech runtime links net/http through its own imports")
		}
	}
}
