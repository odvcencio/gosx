//go:build browser

package perf

import (
	"os"
	"testing"
	"time"
)

// requireChromeEnv names the environment variable that turns a missing browser
// from a skip into a failure.
const requireChromeEnv = "GOSX_REQUIRE_CHROME"

// requireDriver launches a browser for a test in this package.
//
// A missing Chrome is a legitimate reason to skip on a developer machine, and
// every test here used to skip on it directly. It is NOT a legitimate reason in
// continuous integration: the browser-tests job installs Chrome and sets
// CHROME_PATH, so an absent browser there means the install step broke — and then
// every test in this package would report "ok" with nothing run.
//
// That invisible pass is the exact defect this suite was wired up to end. Before
// `make test-perf-browser` existed, nothing in the repository passed -tags
// browser, so 11 tests never compiled. A step that runs but skips everything is
// the same hiding place with a green tick over it.
//
// So GOSX_REQUIRE_CHROME turns the skip into a failure. The Makefile target sets
// it, which is what makes the gate mean something. A plain
// `go test -tags browser ./perf/...` still skips, which is right for a laptop
// without Chrome.
func requireDriver(t *testing.T, timeout time.Duration) *Driver {
	t.Helper()
	d, err := New(WithHeadless(true), WithTimeout(timeout))
	if err == nil {
		t.Cleanup(func() { d.Close() })
		return d
	}
	if os.Getenv(requireChromeEnv) != "" {
		t.Fatalf("%s is set, so a browser is required here and one could not start: %v\n"+
			"Every test in this package needs Chrome. Skipping them all would report a green run "+
			"over zero executed tests.", requireChromeEnv, err)
	}
	t.Skipf("skipping: %v (set %s to make this a failure)", err, requireChromeEnv)
	return nil
}

// TestBrowserSuiteHasABrowserWhenRequired is the gate itself. It fails fast, with
// one clear message, when the browser the whole package depends on is absent in an
// environment that promised one.
//
// FindChrome has its own unit tests in chrome_test.go, which run without the
// browser tag. This test is about the LAUNCH, because a found binary that cannot
// start produces the same all-skipped run.
func TestBrowserSuiteHasABrowserWhenRequired(t *testing.T) {
	d := requireDriver(t, 20*time.Second)

	// A driver that launches but cannot evaluate is no better than none, so prove
	// the session works before the rest of the package trusts it.
	var answer int
	if err := d.Evaluate(`6 * 7`, &answer); err != nil {
		t.Fatalf("the browser launched but Evaluate failed: %v", err)
	}
	if answer != 42 {
		t.Fatalf("Evaluate returned %d, want 42; the CDP session is not usable", answer)
	}
}
