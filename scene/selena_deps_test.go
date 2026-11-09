//go:build !js || !wasm

package scene

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Browser builds load Selena materials from artifact files, so the js/wasm
// import graphs of scene and engine/wasm must not reach the Selena compiler or
// the tree-sitter runtime it parses with. Measured with go1.26 on a program
// that imports scene: calling CompileSelenaMaterial grows it from 3.9 MB to
// 20.5 MB raw (0.81 MB to 3.62 MB brotli), and linking the compiler without
// calling it still costs 0.9 MB raw. The gosx_selena_compiler build tag opts a
// browser build back into run-time compilation.
func TestBrowserSceneDoesNotLinkSelenaCompiler(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool unavailable")
	}
	for _, pkg := range []string{".", "../engine/wasm"} {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", pkg)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s: %v\n%s", pkg, err, out)
		}
		for _, dep := range strings.Fields(string(out)) {
			if reason := selenaCompilerDependency(dep); reason != "" {
				t.Errorf("js/wasm %s links %s (%s)", pkg, dep, reason)
			}
		}
	}
}

// selenaCompilerDependency explains why dep must stay out of a browser build,
// or returns "" when it may stay.
func selenaCompilerDependency(dep string) string {
	switch {
	case dep == "m31labs.dev/selena":
		return "the root package holds Compile"
	case strings.HasPrefix(dep, "m31labs.dev/selena/parse"),
		strings.HasPrefix(dep, "m31labs.dev/selena/grammar"):
		return "Selena parser"
	case strings.HasPrefix(dep, "m31labs.dev/selena/lower"):
		return "Selena lowerer"
	case strings.HasPrefix(dep, "m31labs.dev/selena/emit/"):
		return "Selena emitter"
	case strings.HasPrefix(dep, "m31labs.dev/selena/materiallib"),
		strings.HasPrefix(dep, "m31labs.dev/selena/artifact/builder"):
		return "links the compiler"
	case strings.HasPrefix(dep, "github.com/odvcencio/gotreesitter"):
		return "tree-sitter runtime"
	}
	return ""
}
