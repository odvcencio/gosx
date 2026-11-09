//go:build !js || !wasm

package game_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// browserPackages are the game packages a browser (js/wasm) build imports, plus
// the Go WASM engine fixture that the size numbers in the README come from.
var browserPackages = []string{
	"m31labs.dev/gosx/game",
	"m31labs.dev/gosx/game/host",
	"m31labs.dev/gosx/game/loop",
	"m31labs.dev/gosx/game/gamepad",
	"m31labs.dev/gosx/game/storage",
	"m31labs.dev/gosx/game/audio",
	"m31labs.dev/gosx/game/audio/host",
	"m31labs.dev/gosx/game/audio/worklet",
	"m31labs.dev/gosx/sim/simcore",
	"m31labs.dev/gosx/engine/wasm/internal/browserworker/fixture",
}

// forbiddenInBrowser are server-only packages. Each one adds megabytes to a
// game's WASM binary: the hub links net/http and crypto/tls, and the root gosx
// package links the GoSX compiler. Server helpers belong in game/serve.
var forbiddenInBrowser = map[string]string{
	"m31labs.dev/gosx":            "the GoSX compiler; mount helpers live in game/serve",
	"m31labs.dev/gosx/hub":        "the hub server; game/serve.NewRunner wires a hub",
	"m31labs.dev/gosx/sim":        "sim.Runner links the hub; browser code uses sim/simcore",
	"m31labs.dev/gosx/game/serve": "server-only helpers",
	"net/http":                    "HTTP server and client",
	"crypto/tls":                  "TLS",
}

// pendingInBrowser are forbidden imports that another lane of the jam upgrade
// removes. The test logs them instead of failing. Delete an entry when its
// change lands; the test then enforces it.
var pendingInBrowser = map[string]string{
	"m31labs.dev/selena/parse":          "C5: scene loads Selena artifacts without the compiler",
	"m31labs.dev/selena/lower":          "C5: scene loads Selena artifacts without the compiler",
	"github.com/odvcencio/gotreesitter": "C5: linked through the Selena parser",
}

// pendingInPackage are pending imports of one package only.
var pendingInPackage = map[string]map[string]string{
	"m31labs.dev/gosx/game/audio": {
		"net/http":   "C4: audio.HTTPLoader fetches clips through net/http",
		"crypto/tls": "C4: linked through net/http by audio.HTTPLoader",
	},
}

// TestBrowserBuildsLinkNoServerPackages lists the js/wasm dependencies of every
// browser package and fails if a server-only package returns.
func TestBrowserBuildsLinkNoServerPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("lists js/wasm dependencies with the go tool")
	}
	args := append([]string{"list", "-f", "{{.ImportPath}} {{join .Deps \" \"}}"}, browserPackages...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list for js/wasm: %v\n%s", err, output)
	}
	seen := 0
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg, deps := fields[0], fields[1:]
		seen++
		for _, dep := range deps {
			if why, ok := forbiddenInBrowser[dep]; ok {
				if note, ok := pendingInPackage[pkg][dep]; ok {
					t.Logf("%s still imports %s (pending, %s)", pkg, dep, note)
					continue
				}
				t.Errorf("%s imports %s on js/wasm: %s", pkg, dep, why)
			}
			if note, ok := pendingInBrowser[dep]; ok {
				t.Logf("%s still imports %s (pending, %s)", pkg, dep, note)
			}
		}
	}
	if seen != len(browserPackages) {
		t.Fatalf("go list reported %d packages, want %d:\n%s", seen, len(browserPackages), output)
	}
}
