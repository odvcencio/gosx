//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestCMSPublishKeepsNewerEditsUnsaved(t *testing.T) {
	chrome := e2eChromePath(t)
	source, err := os.ReadFile(filepath.Join(e2eRepoRoot(t), "examples/gosx-docs/public/cms-client.js"))
	if err != nil {
		t.Fatal(err)
	}
	submitted, release := make(chan string, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			submitted <- r.FormValue("block_0_title")
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true,"data":{"count":1}}`)
			return
		}
		if r.URL.Path == "/cms-client.js" {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(source)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><form id="cms-content-form" method="post">
<input name="csrf_token" type="hidden" value="fixture">
<input id="cms-block-count" name="block_count" type="hidden" value="1">
<div id="cms-block-list"><article data-block-kind="hero" data-block-index="0">
<input id="title" name="block_0_title" data-preview-field="title" value="Submitted draft"></article></div>
<div id="cms-preview-canvas"></div>
<span id="cms-unsaved-badge" hidden>Unsaved changes</span>
<span id="cms-status-text"></span><span id="cms-publish-feedback"></span>
<button id="cms-publish-btn" type="submit"><span id="cms-publish-label">Publish changes</span></button>
</form><script src="/cms-client.js"></script>`)
	}))
	defer server.Close()
	defer unblock()
	page := newBrowserPage(t, chrome, nil, 800, 640, "", 45*time.Second)
	page.navigate(t, server.URL)
	if err := chromedp.Run(page.ctx, chromedp.Click("#cms-publish-btn")); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-submitted:
		if value != "Submitted draft" {
			t.Fatalf("published %q", value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publish did not reach server")
	}
	if err := chromedp.Run(page.ctx, chromedp.SendKeys("#title", " plus newer edits")); err != nil {
		t.Fatal(err)
	}
	unblock()
	page.waitFor(t, `!document.querySelector('#cms-publish-btn').disabled`, 5*time.Second, "publish response")
	var dirty bool
	page.eval(t, `!document.querySelector('#cms-unsaved-badge').hidden`, &dirty)
	if !dirty {
		t.Fatal("publish response marked edits made after submission as saved")
	}
}

func TestDocsRendererBenchControlsRemainVisible(t *testing.T) {
	chrome := e2eChromePath(t)
	baseURL := os.Getenv("GOSX_E2E_BASE_URL")
	if baseURL == "" {
		baseURL = startDocsApp(t, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t))).baseURL
	}
	page := newBrowserPage(t, chrome, nil, 1280, 900, "", 90*time.Second)
	for _, width := range []int64{1280, 390} {
		if err := chromedp.Run(page.ctx, chromedp.EmulateViewport(width, 900)); err != nil {
			t.Fatal(err)
		}
		page.navigate(t, baseURL+"/demos/scene3d-bench?workload=static")
		var visible bool
		page.eval(t, `(() => { const panel = document.querySelector('#bench3d-overlay'); const r = panel.getBoundingClientRect(); return r.width > 0 && r.height > 0 && getComputedStyle(panel).visibility !== 'hidden' && document.documentElement.scrollWidth <= innerWidth; })()`, &visible)
		if !visible {
			t.Fatalf("bench controls hidden or overflowing at width %d", width)
		}
		if err := chromedp.Run(page.ctx, chromedp.Click(`.scene3d-bench__workloads a[href="?workload=thick-lines"]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		page.waitFor(t, `document.querySelector('#bench3d-workload').textContent === 'thick-lines'`, 10*time.Second, "workload switch")
		var location string
		if err := chromedp.Run(page.ctx, chromedp.Location(&location)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(location, "workload=thick-lines") {
			t.Fatalf("workload URL = %s", location)
		}
	}
}

// Page-owned demo scripts do not participate in the managed runtime lifecycle.
// Verify real gallery clicks initialize them, including a second visit, rather
// than proving only that a direct URL load works.
func TestDocsGalleryInitializesPageOwnedDemoScripts(t *testing.T) {
	chrome := e2eChromePath(t)
	baseURL := os.Getenv("GOSX_E2E_BASE_URL")
	if baseURL == "" {
		// Hubs need the verified production WASM manifest. The dev server's
		// compatibility URL does not establish that immutable asset contract.
		baseURL = startProductionDocsApp(t).baseURL
	}
	page := newBrowserPage(t, chrome, nil, 1280, 900, "", 5*time.Minute)
	cases := []struct{ slug, ready string }{
		{"collab", `document.querySelector('#collab-preview')?.textContent.length > 0 && ['connected','synced'].includes(document.querySelector('#collab-status')?.textContent)`},
		{"cms", `!!document.querySelector('#cms-content-form')`},
		{"fluid", `Number(document.querySelector('#fluid-particles')?.textContent) > 0`},
		{"livesim", `document.querySelector('#livesim-state')?.textContent === 'live'`},
		{"checkers", `document.querySelector('#checkers-status') && !document.querySelector('#checkers-status').textContent.includes('Connecting')`},
		{"playground", `document.querySelector('[data-editor-stat="chars"]')?.textContent.includes('char')`},
		{"scene3d-bench", `!!document.querySelector('#bench3d-fps') && document.querySelector('#bench3d-fps').textContent !== '—'`},
		{"collab", `document.querySelector('#collab-preview')?.textContent.length > 0 && ['connected','synced'].includes(document.querySelector('#collab-status')?.textContent)`},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			page.navigate(t, baseURL+"/demos/")
			page.eval(t, `window.__demoGalleryDocument = true`, nil)
			selector := fmt.Sprintf(`.demos-gallery a.demo-card__open[href="%s"],.demos-gallery a.demo-card__open[href$="/%s"]`, tc.slug, tc.slug)
			if err := chromedp.Run(page.ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			page.waitFor(t, `typeof window.__demoGalleryDocument === 'undefined'`, 10*time.Second, "fresh demo document")
			// Click does not await a native navigation's load event. The browser
			// can still be loading the deferred runtime while the new document
			// already exists, so give loading its own bounded deadline before
			// checking whether the page-owned script initialized.
			page.waitFor(t, `document.readyState === 'complete'`, 60*time.Second, tc.slug+" document load")
			page.waitFor(t, tc.ready, 20*time.Second, tc.slug+" script initialization")
			if tc.slug == "cms" {
				if err := chromedp.Run(page.ctx, chromedp.SendKeys(`[data-preview-field="title"]`, " gallery edit", chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				page.waitFor(t, `!document.querySelector('#cms-unsaved-badge').hidden`, 5*time.Second, "CMS input listener")
			}
		})
	}
}
