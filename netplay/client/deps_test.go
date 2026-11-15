//go:build !js || !wasm

package client_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// browserNetplay are the packages a browser netplay client imports.
var browserNetplay = []string{
	"m31labs.dev/gosx/netplay",
	"m31labs.dev/gosx/netplay/client",
	"m31labs.dev/gosx/netplay/dgram",
	"m31labs.dev/gosx/netplay/signal",
	"m31labs.dev/gosx/hub/client",
}

// forbidden are server-only packages. net/http and crypto/tls alone added
// about 3 MB to a game's WASM binary (Primal Shift, v0.59.1).
var forbidden = map[string]string{
	"net/http":                     "HTTP server and client; use map[string][]string for headers",
	"crypto/tls":                   "TLS",
	"github.com/gorilla/websocket": "native WebSocket transport",
	"m31labs.dev/gosx/hub":         "the hub server; share constants through netplay/dgram",
}

// TestBrowserNetplayLinksNoServerPackages fails if a js/wasm build of the
// netplay client links net/http, crypto/tls or the hub server, and names the
// import chain.
func TestBrowserNetplayLinksNoServerPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("lists js/wasm dependencies with the go tool")
	}
	args := append([]string{"list", "-deps", "-f", "{{.ImportPath}} {{join .Imports \" \"}}"}, browserNetplay...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list for js/wasm: %v\n%s", err, stderr.String())
	}
	imports := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			imports[f[0]] = f[1:]
		}
	}
	for _, root := range browserNetplay {
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
		for dep, why := range forbidden {
			if _, bad := parent[dep]; !bad {
				continue
			}
			var chain []string
			for at := dep; at != ""; at = parent[at] {
				chain = append([]string{at}, chain...)
			}
			t.Errorf("%s imports %s on js/wasm: %s\n\tchain: %s", root, dep, why, strings.Join(chain, " -> "))
		}
	}
}

// wasmBudget is the largest raw size, in bytes, of the minimal netplay client
// built for js/wasm. It was 4,218,724 B when the server packages left the
// build (6,851,413 B before). Raise it only with a reason in the commit.
const wasmBudget = 4_600_000

// TestMinimalClientWasmSizeBudget builds the smallest client and fails if it
// grows past the budget.
func TestMinimalClientWasmSizeBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a js/wasm binary")
	}
	out := t.TempDir() + "/probe.wasm"
	cmd := exec.Command("go", "build", "-o", out, "m31labs.dev/gosx/netplay/client/internal/sizeprobe")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build for js/wasm: %v\n%s", err, stderr.String())
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("minimal client: %d bytes (budget %d)", fi.Size(), wasmBudget)
	if fi.Size() > wasmBudget {
		t.Fatalf("minimal netplay client is %d bytes, over the %d byte budget", fi.Size(), wasmBudget)
	}
}
