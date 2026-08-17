package gosx_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

// serverPackageTree is every package under m31labs.dev/gosx/server -- the
// package every gosx app imports to run. None of them may directly import
// m31labs.dev/gosx/imagepipe from a non-_test.go file (issue #200: package
// imagepipe is build-only, imported by cmd/gosx and its check stage, never
// by server).
var serverPackageTree = []string{
	"m31labs.dev/gosx/server",
	"m31labs.dev/gosx/server/redis",
}

// TestServerPackageTreeNeverImportsImagepipe is the module-graph proof
// issue #200's isolation rule requires. It runs `go list -json` -- a
// package's own direct, non-test import list -- over every package under
// server and fails if "m31labs.dev/gosx/imagepipe" appears in it. That
// import may only ever be reachable from a package's _test.go files, which
// `go list -json` (field Imports) does not report; this mirrors gsxmail's
// own structural_isolation_test.go pattern
// (TestGotreesitterIsolatedFromCorePath) for the analogous
// gotreesitter/render-path boundary in that module.
//
// This is a direct-import check, not a transitive one, matching that same
// precedent: it proves the build-time image pipeline did not migrate onto
// the server package's own doorstep. TestModuleGraphExcludesForeignRuntimes
// below is this repo's separate, transitive-graph proof that gosx ships no
// WebP encoder dependency (and so no wasm runtime, no FFI shim) at all,
// in-tree or otherwise.
func TestServerPackageTreeNeverImportsImagepipe(t *testing.T) {
	for _, pkg := range serverPackageTree {
		out, err := exec.Command("go", "list", "-json", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -json %s: %v\n%s", pkg, err, out)
		}
		var info struct {
			ImportPath string
			Imports    []string
		}
		if err := json.Unmarshal(out, &info); err != nil {
			t.Fatalf("parsing go list -json %s output: %v\n%s", pkg, err, out)
		}
		for _, dep := range info.Imports {
			if dep == "m31labs.dev/gosx/imagepipe" {
				t.Errorf("%s directly imports m31labs.dev/gosx/imagepipe from a non-_test.go file; server must never import the build-time image pipeline", pkg)
			}
		}
	}
}

// forbiddenGraphModules names the two dependencies the owner's no-foreign-
// runtime rule excludes from this module's build graph entirely: a wasm
// runtime (tetratelabs/wazero) and an FFI shim (ebitengine/purego). Both
// arrived transitively through github.com/gen2brain/webp, imagepipe's own
// former WebP encoder; removing that encoder (in favor of the
// imagepipe.RegisterEncoder extension point -- see the CHANGELOG) is what
// this test proves stayed removed.
var forbiddenGraphModules = []string{
	"github.com/tetratelabs/wazero",
	"github.com/ebitengine/purego",
}

// TestModuleGraphExcludesForeignRuntimes is the whole-module-graph proof,
// as opposed to TestServerPackageTreeNeverImportsImagepipe's direct-import-
// only one above: it runs `go mod graph` -- every module reachable from
// this module's own build, at any depth, through any package -- and fails
// if either forbiddenGraphModules entry appears anywhere in it. Unlike a
// `go list -json` import check, this also catches a module reintroduced
// indirectly (through a new dependency of a dependency), not just a direct
// import gosx's own code wrote.
func TestModuleGraphExcludesForeignRuntimes(t *testing.T) {
	out, err := exec.Command("go", "mod", "graph").CombinedOutput()
	if err != nil {
		t.Fatalf("go mod graph: %v\n%s", err, out)
	}
	for _, module := range forbiddenGraphModules {
		if bytes.Contains(out, []byte(module)) {
			t.Errorf("go mod graph contains %q; gosx ships no wasm runtimes and no FFI shims (see the CHANGELOG entry for this module's WebP encoder removal)", module)
		}
	}
}

// TestCmdGosxReachesImagepipe is TestServerPackageTreeNeverImportsImagepipe's
// complementary proof: cmd/gosx does reach package imagepipe, on purpose,
// through its build stage. If this test ever fails, either the image
// variant stage moved to a different package (update the import path
// below) or was removed entirely (delete this test alongside it, not
// silently) -- either way, this is the intentional counterpart to the
// isolation rule above, not a boundary this repo is also trying to hold.
func TestCmdGosxReachesImagepipe(t *testing.T) {
	out, err := exec.Command("go", "list", "-json", "m31labs.dev/gosx/cmd/gosx").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -json m31labs.dev/gosx/cmd/gosx: %v\n%s", err, out)
	}
	var info struct {
		ImportPath string
		Imports    []string
	}
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("parsing go list -json output: %v\n%s", err, out)
	}
	found := false
	for _, dep := range info.Imports {
		if dep == "m31labs.dev/gosx/imagepipe" {
			found = true
		}
	}
	if !found {
		t.Error("cmd/gosx no longer imports m31labs.dev/gosx/imagepipe; if the image variant stage moved, update this test's own expectation instead of deleting it")
	}
}
