//go:build !js || !wasm

package game_test

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// browserPackages are the packages a browser (js/wasm) game build imports: the
// game runtime and kit, the simulation, the animation graph, the markup
// package the kit screens build their HTML with, the standard-Go client
// runtime (client/wasm, default tags) with the engine-surface packages it
// links, and the Go WASM engine fixture that the size numbers in the README
// come from.
var browserPackages = []string{
	"m31labs.dev/gosx/client/bridge",
	"m31labs.dev/gosx/client/wasm",
	"m31labs.dev/gosx/engine/surface",
	"m31labs.dev/gosx/engine/surface/runtime",
	"m31labs.dev/gosx/game",
	"m31labs.dev/gosx/game/actions",
	"m31labs.dev/gosx/game/audio",
	"m31labs.dev/gosx/game/audio/host",
	"m31labs.dev/gosx/game/audio/worklet",
	"m31labs.dev/gosx/game/boot",
	"m31labs.dev/gosx/game/camera",
	"m31labs.dev/gosx/game/flow",
	"m31labs.dev/gosx/game/gamepad",
	"m31labs.dev/gosx/game/host",
	"m31labs.dev/gosx/game/kitui",
	"m31labs.dev/gosx/game/loop",
	"m31labs.dev/gosx/game/netsync",
	"m31labs.dev/gosx/game/physicsys",
	"m31labs.dev/gosx/game/replay",
	"m31labs.dev/gosx/game/replay/replayui",
	"m31labs.dev/gosx/game/settings",
	"m31labs.dev/gosx/game/storage",
	"m31labs.dev/gosx/game/teamai",
	"m31labs.dev/gosx/markup",
	"m31labs.dev/gosx/motion/animgraph",
	"m31labs.dev/gosx/sim",
	"m31labs.dev/gosx/sim/simcore",
	"m31labs.dev/gosx/engine/wasm/internal/browserworker/fixture",
}

// forbiddenInBrowser are server-only and compiler packages. Each one adds
// megabytes to a game's WASM binary: the hub links net/http and crypto/tls,
// and the root gosx package links the .gsx compiler. Server helpers belong in
// game/serve; browser code builds HTML nodes with package markup.
var forbiddenInBrowser = map[string]string{
	"m31labs.dev/gosx":                             "the root package links the .gsx compiler; build nodes with package markup, mount helpers live in game/serve",
	"m31labs.dev/gosx/internal/gsxparse":           "the .gsx parser",
	"github.com/odvcencio/gotreesitter":            "the tree-sitter runtime behind the .gsx and Selena parsers",
	"github.com/odvcencio/gotreesitter/grammargen": "the grammar generator the .gsx parser links",
	"m31labs.dev/selena/parse":                     "the Selena compiler; scene loads Selena artifacts without it",
	"m31labs.dev/selena/lower":                     "the Selena compiler; scene loads Selena artifacts without it",
	"m31labs.dev/gosx/hub":                         "the hub server; game/serve.NewRunner wires a hub",
	"m31labs.dev/gosx/game/serve":                  "server-only helpers",
	"net/http":                                     "HTTP server and client",
	"crypto/tls":                                   "TLS",
}

// pendingInBrowser are forbidden imports that another lane of the jam upgrade
// removes. The test logs them instead of failing. Delete an entry when its
// change lands; the test then enforces it. Keys must also be in
// forbiddenInBrowser.
var pendingInBrowser = map[string]string{}

// pendingInPackage are pending forbidden imports of one package only.
var pendingInPackage = map[string]map[string]string{}

// TestBrowserBuildsLinkNoServerPackages lists the js/wasm dependencies of every
// browser package and fails if a server-only or compiler package returns. The
// failure names the import chain that pulls the package in.
func TestBrowserBuildsLinkNoServerPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("lists js/wasm dependencies with the go tool")
	}
	args := append([]string{"list", "-deps", "-f", "{{.ImportPath}} {{join .Imports \" \"}}"}, browserPackages...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go list for js/wasm: %v\n%s", err, exit.Stderr)
		}
		t.Fatalf("go list for js/wasm: %v", err)
	}
	imports := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			imports[fields[0]] = fields[1:]
		}
	}
	for _, pkg := range browserPackages {
		if _, ok := imports[pkg]; !ok {
			t.Fatalf("go list did not report %s:\n%s", pkg, output)
		}
		chains := importChains(pkg, imports)
		for _, dep := range slices.Sorted(maps.Keys(chains)) {
			why, ok := forbiddenInBrowser[dep]
			if !ok {
				continue
			}
			if note, ok := pendingInPackage[pkg][dep]; ok {
				t.Logf("%s still imports %s (pending, %s)", pkg, dep, note)
				continue
			}
			if note, ok := pendingInBrowser[dep]; ok {
				t.Logf("%s still imports %s (pending, %s)", pkg, dep, note)
				continue
			}
			t.Errorf("%s imports %s on js/wasm: %s\n\tchain: %s", pkg, dep, why, strings.Join(chains[dep], " -> "))
		}
	}
}

// TestBrowserGuardTablesAgree keeps the pending tables honest: a pending entry
// is only meaningful for a package the guard forbids, and only for a package
// the guard lists.
func TestBrowserGuardTablesAgree(t *testing.T) {
	listed := map[string]bool{}
	for _, pkg := range browserPackages {
		if listed[pkg] {
			t.Errorf("browserPackages lists %s twice", pkg)
		}
		listed[pkg] = true
	}
	for dep := range pendingInBrowser {
		if _, ok := forbiddenInBrowser[dep]; !ok {
			t.Errorf("pendingInBrowser has %s, which forbiddenInBrowser does not forbid", dep)
		}
	}
	for pkg, deps := range pendingInPackage {
		if !listed[pkg] {
			t.Errorf("pendingInPackage has %s, which browserPackages does not list", pkg)
		}
		for dep := range deps {
			if _, ok := forbiddenInBrowser[dep]; !ok {
				t.Errorf("pendingInPackage[%s] has %s, which forbiddenInBrowser does not forbid", pkg, dep)
			}
		}
	}
}

// importChains walks the import graph from root and returns, for each package
// reached, the shortest import chain from root to it.
func importChains(root string, imports map[string][]string) map[string][]string {
	parent := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		for _, dep := range imports[pkg] {
			if _, seen := parent[dep]; !seen {
				parent[dep] = pkg
				queue = append(queue, dep)
			}
		}
	}
	chains := make(map[string][]string, len(parent))
	for dep := range parent {
		if dep == root {
			continue
		}
		var chain []string
		for at := dep; at != ""; at = parent[at] {
			chain = append(chain, at)
		}
		for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
			chain[i], chain[j] = chain[j], chain[i]
		}
		chains[dep] = chain
	}
	return chains
}
