//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestBrowserTypedMapIndexAfterHydration(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := t.TempDir()
	copyFixtureTree(t, filepath.Join(root, "e2e", "testdata", "map-index"), fixture)
	module := fmt.Sprintf("module example.invalid/map-index\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	app := startDevApp(t, fixture, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t)))
	page := newBrowserPage(t, chrome, nil, 1024, 768, "", 120*time.Second)
	page.navigate(t, app.baseURL)
	page.waitFor(t, `(()=>{const button=document.querySelector('#show'),root=button?.closest('[data-gosx-island]'),island=window.__gosx?.islands?.get(root?.id);return !!island&&island.listeners.some(entry=>entry.type==='click'&&entry.target===root)})()`, 30*time.Second, "island click listener")
	if err := chromedp.Run(page.ctx, chromedp.Click("#show", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	page.waitFor(t, `document.querySelector('#bool')?.textContent==='' && document.querySelector('#string')?.textContent===''`, 10*time.Second, "sibling-resolved map zero values after browser interaction")
	if errors := page.PageErrors(); len(errors) != 0 {
		t.Fatalf("browser errors: %v", errors)
	}
}
