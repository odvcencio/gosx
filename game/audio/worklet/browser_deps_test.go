//go:build !js || !wasm

package worklet

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// browserBannedImports are the standard-library packages the js/wasm build of
// this package must not link. net/http brings in crypto/tls and crypto/x509.
// A module that needs only Path, Source or the Backend types uses none of
// them, but would still ship them. Handler, which needs net/http, lives in
// handler.go behind a !js || !wasm constraint for this reason.
var browserBannedImports = []string{"net/http", "crypto/tls", "crypto/x509"}

// TestBrowserBuildOmitsNetHTTP lists the js/wasm import closure of this
// package and fails if it holds a banned package. The failure names the import
// chain, so the file that added the import is the first thing it shows.
func TestBrowserBuildOmitsNetHTTP(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f", `{{.ImportPath}} {{.DepOnly}} {{join .Imports " "}}`, ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("list js/wasm dependencies: %v\n%s", err, exit.Stderr)
		}
		t.Fatalf("list js/wasm dependencies: %v", err)
	}
	// One line per package in the closure: import path, whether it is only a
	// dependency (false for the package itself), then its direct imports.
	graph := make(map[string][]string)
	root := ""
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		graph[fields[0]] = fields[2:]
		if fields[1] == "false" {
			root = fields[0]
		}
	}
	if root == "" {
		t.Fatalf("go list did not report the package itself:\n%s", output)
	}
	// Guard against checking the wrong build: only a js/wasm closure holds this.
	if _, ok := graph["syscall/js"]; !ok {
		t.Fatalf("closure of %s lacks syscall/js, so it is not the js/wasm build", root)
	}
	for _, banned := range browserBannedImports {
		if _, ok := graph[banned]; ok {
			t.Errorf("js/wasm build of %s links %s: %s", root, banned, strings.Join(importChain(graph, root, banned), " -> "))
		}
	}
}

// importChain returns the shortest chain of imports from root to target, or
// just target when the graph has no such chain.
func importChain(graph map[string][]string, root, target string) []string {
	parent := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		for _, imported := range graph[pkg] {
			if _, seen := parent[imported]; seen {
				continue
			}
			parent[imported] = pkg
			if imported == target {
				chain := []string{target}
				for p := pkg; p != ""; p = parent[p] {
					chain = append(chain, p)
				}
				slices.Reverse(chain)
				return chain
			}
			queue = append(queue, imported)
		}
	}
	return []string{target}
}
