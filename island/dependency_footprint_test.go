package island

import (
	"os/exec"
	"strings"
	"testing"
)

// TestIslandPackageAddsNoModuleRequirementsToApps protects every app's
// dependency footprint. Package island is imported by every GoSX app with
// islands, and `gosx init` tidies only the starter's imports. A new module
// that island or its tests reach makes `gosx build` fail on an existing app
// with "updates to go.mod needed". Two ways this happened:
// The failure that prompted this test: an island test imported game, which
// imports hub, which imports gorilla/websocket. `go mod tidy` also loads the
// tests of imported packages, so the starter's go.sum gained websocket and
// `go list` then demanded a go.mod update.
//
// golang.org/x/net/html is not checked here: package ir already reaches it
// through island/aot on main, and starters already require x/net.
func TestIslandPackageAddsNoModuleRequirementsToApps(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, module := range strings.Fields(string(out)) {
		if module == "github.com/gorilla/websocket" {
			t.Errorf("package island (including its tests) reaches %s; apps without hubs would then need it in go.mod", module)
		}
	}
	prod, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, pkg := range strings.Fields(string(prod)) {
		if pkg == "m31labs.dev/gosx/island/perfscan" {
			t.Errorf("package island imports %s; the HTML scanner must be supplied by tooling", pkg)
		}
	}
}
