package gosx_test

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// serverPackageTree is every package under m31labs.dev/gosx/server -- the
// package every gosx app imports to run. None of them may directly import
// m31labs.dev/gosx/imagepipe, or its WebP encoder dependency
// github.com/gen2brain/webp, from a non-_test.go file (issue #200: package
// imagepipe is build-only, imported by cmd/gosx and its check stage, never
// by server -- the encoder adds roughly 5 MB to every linked application
// binary, a cost every deployed app would otherwise pay for a build-time-only
// feature).
var serverPackageTree = []string{
	"m31labs.dev/gosx/server",
	"m31labs.dev/gosx/server/redis",
}

// TestServerPackageTreeNeverImportsImagepipe is the module-graph proof
// issue #200's isolation rule requires. It runs `go list -json` -- a
// package's own direct, non-test import list -- over every package under
// server and fails if "m31labs.dev/gosx/imagepipe" or
// "github.com/gen2brain/webp" appears in it. Both may only ever be
// reachable from a package's _test.go files, which `go list -json` (field
// Imports) does not report; this mirrors gsxmail's own
// structural_isolation_test.go pattern (TestGotreesitterIsolatedFromCorePath)
// for the analogous gotreesitter/render-path boundary in that module.
//
// This is a direct-import check, not a transitive one, matching that same
// precedent: it proves gosx build's own new dependency did not migrate onto
// the server package's own doorstep, not that gen2brain/webp is absent from
// the module's build list entirely (it is present -- cmd/gosx depends on
// it, on purpose).
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
			if dep == "github.com/gen2brain/webp" {
				t.Errorf("%s directly imports github.com/gen2brain/webp from a non-_test.go file; the WebP encoder is build-only, never part of a deployed application binary", pkg)
			}
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
