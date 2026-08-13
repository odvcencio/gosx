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

func TestPlaygroundCounterHydratesAndUpdates(t *testing.T) {
	chrome := e2eChromePath(t)
	var app *docsApp
	if existing := strings.TrimRight(os.Getenv("GOSX_E2E_EXISTING_BASE_URL"), "/"); existing != "" {
		app = &docsApp{baseURL: existing, logs: &logBuffer{}}
	} else {
		app = startProductionDocsApp(t)
	}
	page := newBrowserPage(t, chrome, nil, 1440, 960, "", 90*time.Second)

	if status := page.navigate(t, app.baseURL+"/demos/playground"); status < 200 || status > 299 {
		t.Fatalf("/demos/playground returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	page.waitFor(t,
		`document.querySelector(".play")?.dataset.playgroundState === "hydrated"`,
		20*time.Second,
		"playground hydration",
	)

	if err := chromedp.Run(page.ctx,
		chromedp.Click(`.play__preview-frame [data-gosx-island] button:last-child`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click playground +1: %v", err)
	}
	page.waitFor(t,
		`document.querySelector(".play__preview-frame [data-gosx-island] span")?.textContent.trim() === "1"`,
		10*time.Second,
		"counter increment",
	)

	// Recompile the same preset and exercise it again. This proves the editor
	// disposed the old VM/listeners, restored the compiled initial DOM, and
	// registered the replacement under the wrapper id.
	page.eval(t, `document.querySelector(".play").dataset.playgroundState = "reset-requested"`, nil)
	if err := chromedp.Run(page.ctx, chromedp.Click(`.play__reset-btn`, chromedp.ByQuery)); err != nil {
		t.Fatalf("reset playground counter: %v", err)
	}
	page.waitFor(t,
		`document.querySelector(".play")?.dataset.playgroundState === "hydrated" &&
document.querySelector(".play__preview-frame [data-gosx-island] span")?.textContent.trim() === "0"`,
		10*time.Second,
		"counter recompilation",
	)
	if err := chromedp.Run(page.ctx,
		chromedp.Click(`.play__preview-frame [data-gosx-island] button:last-child`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click recompiled playground +1: %v", err)
	}
	page.waitFor(t,
		`document.querySelector(".play__preview-frame [data-gosx-island] span")?.textContent.trim() === "1"`,
		10*time.Second,
		"recompiled counter increment",
	)
	var registeredByDOMID bool
	page.eval(t, `(() => {
const root = document.querySelector(".play__preview-frame [data-gosx-island]");
return !!root?.id && window.__gosx?.islands?.has(root.id) && !window.__gosx.islands.has(root.dataset.gosxIsland);
})()`, &registeredByDOMID)
	if !registeredByDOMID {
		t.Fatal("playground replacement island was not registered by its DOM id")
	}
	var counterListenersSelective bool
	page.eval(t, `(() => {
const root = document.querySelector(".play__preview-frame [data-gosx-island]");
const record = window.__gosx.islands.get(root.id);
if (!record || record.listeners.length !== 1 || record.listeners[0].type !== "click") return false;
const oldListeners = record.listeners.slice();
const originalRemove = root.removeEventListener.bind(root);
window.__playgroundListenerAudit = { oldListeners, removed: 0, originalRemove };
root.removeEventListener = function(type, listener, capture) {
  if (oldListeners.some((entry) => entry.type === type && entry.listener === listener && entry.capture === capture)) {
    window.__playgroundListenerAudit.removed++;
  }
  return originalRemove(type, listener, capture);
};
return true;
})()`, &counterListenersSelective)
	if !counterListenersSelective {
		t.Fatal("compiled Counter did not register only its declared click listener")
	}

	// Change component shape and event family. Counter only needs click;
	// Greeter needs input. A real browser input after the switch proves the
	// replacement listener set is attached to the stable wrapper rather than
	// inherited accidentally from the original manifest entry.
	page.eval(t, `(() => {
const picker = document.querySelector(".play__preset-select");
picker.value = "greeter";
picker.dispatchEvent(new Event("change", { bubbles: true }));
})()`, nil)
	page.waitFor(t,
		`document.querySelector(".play")?.dataset.playgroundState === "hydrated" &&
document.querySelector('.play__preview-frame [data-gosx-island="Greeter"]')?.dataset.component === "Greeter" &&
document.querySelector(".play__preview-frame h1")?.textContent.trim() === "Hello, world" &&
(() => {
  const root = document.querySelector('.play__preview-frame [data-gosx-island="Greeter"]');
  const record = window.__gosx.islands.get(root.id);
  const audit = window.__playgroundListenerAudit;
  return audit?.removed === audit?.oldListeners?.length &&
    record?.listeners?.length === 1 && record.listeners[0].type === "input" &&
    !audit.oldListeners.some((old) => old.listener === record.listeners[0].listener);
})()`,
		10*time.Second,
		"greeter preset hydration",
	)
	page.eval(t, `(() => {
const root = document.querySelector('.play__preview-frame [data-gosx-island="Greeter"]');
if (root && window.__playgroundListenerAudit?.originalRemove) {
  root.removeEventListener = window.__playgroundListenerAudit.originalRemove;
}
})()`, nil)
	if err := chromedp.Run(page.ctx,
		chromedp.Click(`.play__preview-frame input`, chromedp.ByQuery),
		chromedp.SendKeys(`.play__preview-frame input`, "Ada", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("type in playground greeter: %v", err)
	}
	page.waitFor(t,
		`document.querySelector(".play__preview-frame h1")?.textContent.trim() === "Hello, Ada"`,
		10*time.Second,
		"greeter input update",
	)

	// Reset recompiles the selected Greeter preset. Exercise input once more so
	// the regression also covers disposal and rebinding within the new family.
	if err := chromedp.Run(page.ctx, chromedp.Click(`.play__reset-btn`, chromedp.ByQuery)); err != nil {
		t.Fatalf("reset playground greeter: %v", err)
	}
	page.waitFor(t,
		`document.querySelector(".play")?.dataset.playgroundState === "hydrated" &&
document.querySelector('.play__preview-frame [data-gosx-island="Greeter"]')?.dataset.component === "Greeter" &&
document.querySelector(".play__preview-frame h1")?.textContent.trim() === "Hello, world"`,
		10*time.Second,
		"greeter recompilation",
	)
	if err := chromedp.Run(page.ctx,
		chromedp.Click(`.play__preview-frame input`, chromedp.ByQuery),
		chromedp.SendKeys(`.play__preview-frame input`, "Grace", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("type in recompiled playground greeter: %v", err)
	}
	page.waitFor(t,
		`document.querySelector(".play__preview-frame h1")?.textContent.trim() === "Hello, Grace"`,
		10*time.Second,
		"recompiled greeter input update",
	)

	// Active markup must be rejected before program encoding. Keep the live
	// Greeter DOM and global scope as sentinels: each probe must produce a
	// diagnostic without replacing the last good preview or executing content.
	page.eval(t, `window.__gosxPlaygroundPwned = 0`, nil)
	attackBodies := []string{
		`<img src="/definitely-missing.png" onerror="window.__gosxPlaygroundPwned++" />`,
		`<script>window.__gosxPlaygroundPwned++</script>`,
		`<svg onload="window.__gosxPlaygroundPwned++"></svg>`,
	}
	for index, body := range attackBodies {
		source := `package playground

//gosx:island
func Attack() Node {
	return ` + body + `
}
`
		sourceJSON, err := json.Marshal(source)
		if err != nil {
			t.Fatalf("encode unsafe playground source: %v", err)
		}
		page.eval(t, `(() => {
const editor = document.querySelector(".play__source");
editor.value = `+string(sourceJSON)+`;
editor.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true }));
})()`, nil)
		page.waitFor(t,
			`document.querySelector(".play")?.dataset.playgroundState === "diagnostic" &&
document.querySelector(".play__errors")?.textContent.includes("not allowed") &&
document.querySelector('.play__preview-frame [data-gosx-island="Greeter"]')?.dataset.component === "Greeter" &&
document.querySelector(".play__preview-frame h1")?.textContent.trim() === "Hello, Grace" &&
window.__gosxPlaygroundPwned === 0`,
			10*time.Second,
			"unsafe playground source rejection "+string(rune('1'+index)),
		)
	}

	if len(page.PageErrors()) > 0 {
		t.Fatalf("playground raised page errors: %v\nconsole:\n%s", page.PageErrors(), page.Console())
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
