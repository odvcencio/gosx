//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestProductionBuildHydratesStrictIsland is the browser half of the strict
// island proof (the server-side half lives in strict_island_render_test.go
// at the repo root). It builds e2e/testdata/strict-island with `gosx build
// --prod` — the real production pipeline, not a stub — serves the resulting
// bundle, and drives a real Chrome browser: it asserts the server-rendered
// props (Label="Draft Pick", Start=7) appear in the initial HTML, then
// clicks the island's button and asserts the DOM updates from a real
// dispatched click, not a server round trip.
func TestProductionBuildHydratesStrictIsland(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "strict-island")
	copyFixtureTree(t, filepath.Join(root, "e2e", "testdata", "strict-island"), fixture)

	module := fmt.Sprintf("module example.com/gosx-strict-island\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}

	build := exec.Command("go", "run", "./cmd/gosx", "build", "--prod", fixture)
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("gosx build --prod: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Counter") {
		t.Fatalf("gosx build --prod did not report the Counter island:\n%s", output)
	}

	dist := filepath.Join(fixture, "dist")
	port := freeE2EPort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	logs := startBuiltFixture(t, dist, port)
	if err := waitForHealthy(baseURL+"/", 45*time.Second); err != nil {
		t.Fatalf("%v\n%s", err, logs.String())
	}

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("root status = %d\n%s", resp.StatusCode, logs.String())
	}

	for _, delayed := range []bool{false, true} {
		name, initScript := "normal", ""
		if delayed {
			name, initScript = "delayed-program", strictIslandDelayedProgram
		}
		t.Run(name, func(t *testing.T) {
			page := newBrowserPage(t, chrome, nil, 1024, 768, initScript, 60*time.Second)
			if status := page.navigate(t, baseURL+"/"); status != http.StatusOK {
				t.Fatalf("fixture status %d\n%s", status, logs.String())
			}
			if err := chromedp.Run(page.ctx,
				chromedp.WaitVisible(`#strict-counter`, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("wait for strict island: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
			}

			var before string
			page.eval(t, `document.querySelector("#strict-counter").textContent`, &before)
			if !strings.Contains(before, "Draft Pick") || !strings.Contains(strings.TrimSpace(before), "7") {
				t.Fatalf("initial server-rendered island text = %q, want it to contain the proven props \"Draft Pick\" and \"7\"", before)
			}

			if delayed {
				page.waitFor(t, `window.__strictIslandProgramRequested === true`, 10*time.Second, "held strict island program fetch")
				var ready bool
				page.eval(t, strictIslandClickReady, &ready)
				if ready {
					t.Fatal("server-rendered island must not have a click listener while its program fetch is held")
				}
				page.eval(t, `window.__releaseStrictIslandProgram()`, nil)
			}

			// Visible SSR markup precedes asynchronous program loading. Hydration
			// publishes the registry entry only after attaching delegated listeners.
			page.waitFor(t, strictIslandClickReady, 10*time.Second, "strict island click listener")
			page.eval(t, `document.querySelector("#strict-counter-button").click()`, nil)
			if err := chromedp.Run(page.ctx, chromedp.Poll(
				`document.querySelector("#strict-counter-button").textContent === "8"`,
				nil,
				chromedp.WithPollingTimeout(10*time.Second),
			)); err != nil {
				t.Fatalf("wait for strict island increment: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
			}
			var after string
			page.eval(t, `document.querySelector("#strict-counter-button").textContent`, &after)
			if strings.TrimSpace(after) != "8" {
				t.Fatalf("strict island did not hydrate: button text=%q", after)
			}
		})
	}
}

const strictIslandClickReady = `(() => {
const root = document.querySelector("#strict-counter-button")?.closest("[data-gosx-island]");
const island = window.__gosx?.islands?.get(root?.id);
return !!island && island.root === root && island.listeners.some(entry => entry.type === "click" && entry.target === root);
})()`

// Hold the actual compiled island program until the test releases it. Runtime
// and markup loading proceed normally, so SSR visibility cannot stand in for
// hydration readiness. No timer or machine-dependent sleep controls this race.
const strictIslandDelayedProgram = `(() => {
const originalFetch = window.fetch;
const released = new Promise(resolve => { window.__releaseStrictIslandProgram = resolve; });
window.__strictIslandProgramRequested = false;
window.fetch = function(input, options) {
  const url = new URL(input instanceof Request ? input.url : input, document.baseURI);
  if (url.pathname.includes("/islands/")) {
    window.__strictIslandProgramRequested = true;
    return released.then(() => originalFetch.call(this, input, options));
  }
  return originalFetch.call(this, input, options);
};
})()`
