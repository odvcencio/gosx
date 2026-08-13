//go:build e2e

// Port of the retired e2e/gosx_docs_e2e.test.mjs (playwright) to chromedp.
// Covers: homepage and searchable docs render, persistent/mobile navigation
// works without a client component runtime, reference pages return 200,
// scoped and root 404s, and accessibility invariants on /docs/forms.
package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func docsBaseURL() string {
	if url := os.Getenv("GOSX_E2E_BASE_URL"); url != "" {
		return url
	}
	return "http://127.0.0.1:3070"
}

func TestDocsSiteServes(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, docsBaseURL())
	page := newBrowserPage(t, chrome, nil, 1440, 960, "", 90*time.Second)

	// Homepage renders successfully.
	if status := page.navigate(t, app.baseURL); status < 200 || status > 299 {
		t.Fatalf("homepage returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	var title string
	if err := chromedp.Run(page.ctx, chromedp.Title(&title)); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if !strings.Contains(title, "GoSX") {
		t.Fatalf("expected title containing GoSX, got %q\n\nLogs:\n%s", title, app.logs.String())
	}

	// The documentation index is a real searchable route, not a redirect.
	if status := page.navigate(t, app.baseURL+"/docs"); status < 200 || status > 299 {
		t.Fatalf("/docs returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	var location string
	if err := chromedp.Run(page.ctx, chromedp.Location(&location)); err != nil {
		t.Fatalf("read location: %v", err)
	}
	if !strings.HasSuffix(location, "/docs") {
		t.Fatalf("expected searchable /docs index, got %s\n\nLogs:\n%s", location, app.logs.String())
	}
	var docsIndexOK bool
	page.eval(t, `!!document.querySelector('form[action="/docs"] input[name="q"]') &&
      !!document.querySelector('.docs-guide-navigation') &&
      document.querySelector('h1')?.textContent.trim() === 'Documentation'`, &docsIndexOK)
	if !docsIndexOK {
		t.Fatalf("docs index is missing search or persistent navigation\n\nLogs:\n%s", app.logs.String())
	}
	if status := page.navigate(t, app.baseURL+"/docs?q=webgpu"); status < 200 || status > 299 {
		t.Fatalf("docs search returned %d", status)
	}
	var searchOK bool
	page.eval(t, `document.querySelector('.docs-search__summary')?.textContent.includes('webgpu') &&
      [...document.querySelectorAll('.docs-result')].some((el) => /WebGPU|Scene3D/i.test(el.textContent))`, &searchOK)
	if !searchOK {
		t.Fatal("server-rendered WebGPU search returned no matching guide")
	}

	// Reference pages render.
	for _, path := range []string{"/docs/typed-live", "/docs/routing", "/docs/forms", "/docs/scene3d", "/docs/site"} {
		if status := page.navigate(t, app.baseURL+path); status < 200 || status > 299 {
			t.Fatalf("%s returned %d\n\nLogs:\n%s", path, status, app.logs.String())
		}
	}

	// Scoped 404 within /docs returns a page (not a crash).
	if status := page.navigate(t, app.baseURL+"/docs/nonexistent"); status != 404 {
		t.Fatalf("expected 404 for /docs/nonexistent, got %d\n\nLogs:\n%s", status, app.logs.String())
	}

	// Root 404.
	if status := page.navigate(t, app.baseURL+"/totally-missing"); status != 404 {
		t.Fatalf("expected 404 for /totally-missing, got %d\n\nLogs:\n%s", status, app.logs.String())
	}
}

func TestDocsMobileNavigationWorksWithoutDisclosureRuntime(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, docsBaseURL())
	page := newBrowserPage(t, chrome, nil, 390, 844, "", 90*time.Second)
	if status := page.navigate(t, app.baseURL+"/docs"); status < 200 || status > 299 {
		t.Fatalf("/docs returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	if err := chromedp.Run(page.ctx, chromedp.Click(".pill-toggle", chromedp.ByQuery)); err != nil {
		t.Fatalf("open mobile navigation: %v", err)
	}
	var open bool
	page.eval(t, `location.hash === '#nav-overlay' &&
      getComputedStyle(document.querySelector('#nav-overlay')).display !== 'none' &&
      !!document.querySelector('#nav-overlay form[action="/docs"]')`, &open)
	if !open {
		t.Fatal("mobile navigation did not open through the native anchor target")
	}
	if err := chromedp.Run(page.ctx, chromedp.Click(".nav-overlay__close", chromedp.ByQuery)); err != nil {
		t.Fatalf("close mobile navigation: %v", err)
	}
	var closed bool
	page.eval(t, `location.hash !== '#nav-overlay' && getComputedStyle(document.querySelector('#nav-overlay')).display === 'none'`, &closed)
	if !closed {
		t.Fatal("mobile navigation did not close")
	}
}

type accessibilityReport struct {
	HasMain            bool     `json:"hasMain"`
	HasContentInfo     bool     `json:"hasContentInfo"`
	DuplicateIDs       []string `json:"duplicateIds"`
	UnnamedControls    []string `json:"unnamedControls"`
	BrokenDescriptions []string `json:"brokenDescriptions"`
}

func TestDocsAccessibilityInvariants(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, docsBaseURL())
	page := newBrowserPage(t, chrome, nil, 1440, 960, "", 90*time.Second)

	if status := page.navigate(t, app.baseURL+"/docs/forms"); status < 200 || status > 299 {
		t.Fatalf("/docs/forms returned %d\n\nLogs:\n%s", status, app.logs.String())
	}

	var report accessibilityReport
	page.eval(t, `(() => {
    const ids = new Map();
    for (const el of document.querySelectorAll("[id]")) {
      const id = el.getAttribute("id");
      ids.set(id, (ids.get(id) || 0) + 1);
    }
    const duplicateIds = [...ids.entries()].filter(([, count]) => count > 1).map(([id]) => id);
    const unnamedControls = [...document.querySelectorAll("button, a[href], input, select, textarea")]
      .filter((el) => {
        if (el.matches("input[type=hidden]")) return false;
        const labelledBy = el.getAttribute("aria-labelledby");
        const label = el.id ? document.querySelector('label[for="' + CSS.escape(el.id) + '"]') : null;
        const name = [
          el.getAttribute("aria-label"),
          labelledBy && labelledBy.split(/\s+/).map((id) => document.getElementById(id)?.textContent || "").join(" "),
          label?.textContent,
          el.textContent,
          el.getAttribute("title"),
          el.getAttribute("placeholder"),
        ].filter(Boolean).join(" ").trim();
        return name === "";
      })
      .map((el) => el.outerHTML.slice(0, 160));
    const brokenDescriptions = [...document.querySelectorAll("[aria-describedby]")]
      .filter((el) => el.getAttribute("aria-describedby").split(/\s+/).some((id) => id && !document.getElementById(id)))
      .map((el) => el.outerHTML.slice(0, 160));
    return {
      hasMain: !!document.querySelector("main#main-content"),
      hasContentInfo: !!document.querySelector('[role="contentinfo"]'),
      duplicateIds,
      unnamedControls,
      brokenDescriptions,
    };
  })()`, &report)

	if !report.HasMain {
		t.Error("expected main#main-content landmark")
	}
	if !report.HasContentInfo {
		t.Error("expected contentinfo landmark")
	}
	if len(report.DuplicateIDs) > 0 {
		t.Errorf("duplicate ids: %s", strings.Join(report.DuplicateIDs, ", "))
	}
	if len(report.UnnamedControls) > 0 {
		t.Errorf("unnamed controls: %s", strings.Join(report.UnnamedControls, "\n"))
	}
	if len(report.BrokenDescriptions) > 0 {
		t.Errorf("broken aria-describedby refs: %s", strings.Join(report.BrokenDescriptions, "\n"))
	}
	if t.Failed() {
		blob, _ := json.Marshal(report)
		t.Logf("report: %s\n\nLogs:\n%s", blob, app.logs.String())
	}
}
