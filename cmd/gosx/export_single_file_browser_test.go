package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/perf"
)

// openPacked loads url in headless Chrome and returns the document title and
// the JSON that the fixture's app.js wrote into #out.
func openPacked(t *testing.T, chrome, url string, blockNetwork bool) (title string, results map[string]any) {
	t.Helper()
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()
	ctx, cancelTimeout := context.WithTimeout(ctx, 90*time.Second)
	defer cancelTimeout()

	var consoleMu sync.Mutex
	var console []string
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*runtime.EventConsoleAPICalled); ok {
			var parts []string
			for _, a := range e.Args {
				parts = append(parts, strings.Trim(string(a.Value), `"`)+a.Description)
			}
			consoleMu.Lock()
			console = append(console, e.Type.String()+": "+strings.Join(parts, " "))
			consoleMu.Unlock()
		}
	})

	var text string
	actions := chromedp.Tasks{network.Enable()}
	if blockNetwork {
		actions = append(actions, network.SetBlockedURLs().WithURLPatterns([]*network.BlockPattern{
			{URLPattern: "http://*", Block: true},
			{URLPattern: "https://*", Block: true},
		}))
	}
	actions = append(actions,
		chromedp.Navigate(url),
		chromedp.Poll(`document.getElementById('out') !== null && document.getElementById('out').dataset.done === '1'`, nil,
			chromedp.WithPollingTimeout(60*time.Second)),
		chromedp.Title(&title),
		chromedp.Text("#out", &text, chromedp.ByQuery),
	)
	if err := chromedp.Run(ctx, actions); err != nil {
		consoleMu.Lock()
		defer consoleMu.Unlock()
		t.Fatalf("browser run failed: %v\nconsole: %v", err, console)
	}
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("results are not JSON: %v\n%s", err, text)
	}

	// The image and the CSS background must have decoded. The stylesheet
	// import must have applied (border-top-width 7px on #out).
	var dom struct {
		PicWidth   int    `json:"picWidth"`
		BgWidth    int    `json:"bgWidth"`
		BgURL      string `json:"bgUrl"`
		BorderTop  string `json:"borderTop"`
		StatusLeft bool   `json:"statusLeft"`
	}
	const probe = `(async () => {
	  const bg = getComputedStyle(document.getElementById('bg')).backgroundImage;
	  const url = (bg.match(/url\("?([^")]+)"?\)/) || [])[1] || '';
	  const img = new Image();
	  const loaded = url ? await new Promise(r => { img.onload = () => r(img.naturalWidth); img.onerror = () => r(0); img.src = url; }) : 0;
	  return {picWidth: document.getElementById('pic').naturalWidth, bgWidth: loaded, bgUrl: url,
	    borderTop: getComputedStyle(document.getElementById('out')).borderTopWidth,
	    statusLeft: document.getElementById('status') !== null};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &dom, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if dom.PicWidth != 3 {
		t.Errorf("img naturalWidth = %d, want 3", dom.PicWidth)
	}
	if dom.BgWidth != 3 || !strings.HasPrefix(dom.BgURL, "blob:") {
		t.Errorf("background image: loaded width %d from %q, want 3 from a blob: URL", dom.BgWidth, dom.BgURL)
	}
	if dom.BorderTop != "7px" {
		t.Errorf("@import of extra.css did not apply: border-top-width = %q", dom.BorderTop)
	}
	if dom.StatusLeft {
		t.Error("the loading status line is still in the document")
	}
	return title, results
}

func chromeForBrowserTest(t *testing.T) string {
	t.Helper()
	chrome, err := perf.FindChrome()
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so Chrome is required: %v", err)
		}
		t.Skipf("skipping the packed-file browser test: %v (set CHROME_PATH to a Chrome or Chromium binary)", err)
	}
	return chrome
}

// wantFixtureResults pins what the packed fixture does in Chrome. Entries
// marked "limit" are the documented limits in docs/static-game.md.
func wantFixtureResults(t *testing.T, results map[string]any, passthrough func(string) bool) {
	t.Helper()
	want := map[string]any{
		"manifestRewritten": true,
		"modelKept":         "models/box.gltf", // models are never rewritten in the markup
		"info":              "from a bundled file",
		"sum":               float64(42),
		"sumFromManifest":   float64(42),
		"model":             "box.bin",
		"modelBuffer":       float64(4),
		"worker":            "worker-ok",
		"worklet":           "ok",
	}
	for k, v := range want {
		if results[k] != v {
			t.Errorf("results[%q] = %#v, want %#v", k, results[k], v)
		}
	}
	// Limits: import() in a bundled script, a module that imports another
	// module by relative path, and a worker that calls importScripts by
	// relative path, all fail.
	if s, _ := results["dynamicImport"].(string); !strings.HasPrefix(s, "error") {
		t.Errorf("results[dynamicImport] = %#v, documented limit is an error", results["dynamicImport"])
	}
	if s, _ := results["nestedImport"].(string); !strings.HasPrefix(s, "error") {
		t.Errorf("results[nestedImport] = %#v, documented limit is an error", results["nestedImport"])
	}
	if results["workerImportScripts"] != "importScripts failed" {
		t.Errorf("results[workerImportScripts] = %#v, documented limit is a failure", results["workerImportScripts"])
	}
	if s, _ := results["passthrough"].(string); !passthrough(s) {
		t.Errorf("results[passthrough] = %#v", results["passthrough"])
	}
}

func TestSingleFileRunsFromDiskAndOverHTTPInChrome(t *testing.T) {
	chrome := chromeForBrowserTest(t)
	static := filepath.Join(t.TempDir(), "static")
	singleFileFixture(t, static)
	packed := filepath.Join(t.TempDir(), "game dir", "game.html") // a space: the path is percent-encoded in the URL
	if _, err := writeSingleFile(static, packed); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(packed); err == nil {
		t.Logf("packed fixture: %d bytes", info.Size())
	}

	t.Run("file with all http and https requests blocked", func(t *testing.T) {
		title, results := openPacked(t, chrome, "file://"+filepath.ToSlash(packed), true)
		if title != "Fixture & Page" {
			t.Errorf("title = %q", title)
		}
		// Not bundled, no network: the real fetch runs and fails.
		wantFixtureResults(t, results, func(s string) bool { return strings.HasPrefix(s, "error") })
	})

	t.Run("http from a subpath", func(t *testing.T) {
		data, err := os.ReadFile(packed)
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var seen []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, r.URL.Path)
			mu.Unlock()
			switch r.URL.Path {
			case "/deep/sub/game.html":
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write(data)
			case "/deep/sub/not-bundled.txt":
				fmt.Fprint(w, "live")
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		title, results := openPacked(t, chrome, srv.URL+"/deep/sub/game.html", false)
		if title != "Fixture & Page" {
			t.Errorf("title = %q", title)
		}
		wantFixtureResults(t, results, func(s string) bool { return s == "ok:live" })

		mu.Lock()
		defer mu.Unlock()
		for _, p := range seen {
			if p != "/deep/sub/game.html" && p != "/deep/sub/not-bundled.txt" && p != "/favicon.ico" {
				t.Errorf("the server saw a request for %s; bundled files must come from memory", p)
			}
		}
	})

	t.Run("a damaged payload shows a plain error", func(t *testing.T) {
		data, err := os.ReadFile(packed)
		if err != nil {
			t.Fatal(err)
		}
		damaged := packagePattern.ReplaceAll(data, []byte(`<script id="package" type="application/octet-stream">QUJDRA==</script>`))
		p := filepath.Join(t.TempDir(), "damaged.html")
		if err := os.WriteFile(p, damaged, 0o644); err != nil {
			t.Fatal(err)
		}
		allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(chrome), chromedp.Flag("headless", true), chromedp.Flag("no-sandbox", true))...)
		defer cancel()
		ctx, cancelCtx := chromedp.NewContext(allocCtx)
		defer cancelCtx()
		ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
		defer cancelTimeout()
		var text, role string
		var ok bool
		if err := chromedp.Run(ctx,
			chromedp.Navigate("file://"+filepath.ToSlash(p)),
			chromedp.Poll(`document.getElementById('status').getAttribute('role') === 'alert'`, nil, chromedp.WithPollingTimeout(30*time.Second)),
			chromedp.Text("#status", &text, chromedp.ByQuery),
			chromedp.AttributeValue("#status", "role", &role, &ok, chromedp.ByQuery),
		); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(text, "Could not unpack this file") || role != "alert" {
			t.Errorf("status = %q (role %q)", text, role)
		}
	})
}
