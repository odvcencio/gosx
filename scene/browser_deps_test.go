//go:build !(js && wasm)

package scene

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// selenaCompilerPackages are the Selena packages that compile .sel source, and
// the tree-sitter runtime its parser uses. The js/wasm build of this package
// must link none of them: a browser module that shows precompiled materials
// needs only the result types in m31labs.dev/selena/result. A path that ends
// in "/" bans every package under it.
var selenaCompilerPackages = []string{
	"m31labs.dev/selena",
	"m31labs.dev/selena/parse",
	"m31labs.dev/selena/lower",
	"m31labs.dev/selena/emit/",
	"github.com/odvcencio/gotreesitter",
	"github.com/odvcencio/gotreesitter/",
}

// TestBrowserBuildOmitsSelenaCompiler lists the js/wasm import closure of this
// package and fails if it holds a Selena compiler package. The failure names
// the import chain, so the file that added the import is the first thing it
// shows.
func TestBrowserBuildOmitsSelenaCompiler(t *testing.T) {
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
	// Guard against a check that can never fail: the closure must still hold
	// the result types it is meant to use instead of the compiler.
	if _, ok := graph["m31labs.dev/selena/result"]; !ok {
		t.Fatalf("closure of %s lacks m31labs.dev/selena/result", root)
	}
	for pkg := range graph {
		for _, banned := range selenaCompilerPackages {
			if pkg == banned || strings.HasSuffix(banned, "/") && strings.HasPrefix(pkg, banned) {
				t.Errorf("js/wasm build of %s links %s: %s", root, pkg, strings.Join(selenaImportChain(graph, root, pkg), " -> "))
			}
		}
	}
}

// selenaImportChain returns the shortest chain of imports from root to target,
// or just target when the graph has no such chain.
func selenaImportChain(graph map[string][]string, root, target string) []string {
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
