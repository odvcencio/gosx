package gosx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoModHasNoLocalReplace fails when go.mod carries a replace directive that
// points at a filesystem path.
//
// A local replace is normal while debugging a dependency — point the module at a
// checkout, iterate, then drop it. The failure mode is forgetting the last step.
// The directive resolves fine on the machine that added it and nowhere else, so
// the module is broken for every consumer while looking healthy locally: builds
// pass, tests pass, and the break only appears in CI or after a release.
//
// v0.40.0 shipped exactly that. A replace pointing at a scratch directory was
// committed and tagged; the release job failed on a missing go.mod and published
// nothing, but the tag reached the module proxy and is immutable, so the version
// is permanently unusable. v0.40.1 is the same work with the directive removed.
//
// Version replacements (`=> module v1.2.3`) are untouched by this test. Only
// filesystem paths are rejected.
func TestGoModHasNoLocalReplace(t *testing.T) {
	for _, name := range []string{
		"go.mod",
		filepath.Join("cmd", "buildbootstrap", "go.mod"),
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", name, err)
		}

		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			// Both the single-line form and the block form land here; a block
			// body line has no "replace" keyword, so match on the arrow.
			arrow := strings.Index(trimmed, "=>")
			if arrow < 0 {
				continue
			}
			target := strings.TrimSpace(trimmed[arrow+2:])
			if target == "" {
				continue
			}
			if isAbsolutePath(target) {
				t.Errorf("%s:%d has an absolute replace target %q\n"+
					"  An absolute path resolves only on the machine that added it.\n"+
					"  Drop it before committing: go mod edit -dropreplace <module>",
					name, i+1, target)
			}
		}
	}
}

// isAbsolutePath reports whether a replace target is an absolute filesystem
// path.
//
// Repo-relative targets are deliberately allowed. cmd/buildbootstrap is its own
// module and carries `replace m31labs.dev/gosx => ../..` on purpose, so it
// builds against this source tree rather than a published version. That
// resolves wherever the repository is cloned. An absolute path does not.
func isAbsolutePath(target string) bool {
	switch {
	case strings.HasPrefix(target, "/"):
		return true
	case len(target) > 2 && target[1] == ':': // C:\... on Windows
		return true
	default:
		return false
	}
}
