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

	page := newBrowserPage(t, chrome, nil, 1024, 768, "", 60*time.Second)
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
		t.Fatalf("initial hydrated island text = %q, want it to contain the proven props \"Draft Pick\" and \"7\"", before)
	}

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
}
