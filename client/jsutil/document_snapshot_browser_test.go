//go:build !js

package jsutil_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Run with GOSX_BROWSER_EXECUTABLE set to a Chromium executable. This exercises
// the shipped runtime in a real DOM, including synchronous mutation delivery.
func TestDocumentEnhancementSnapshot(t *testing.T) {
	browser := os.Getenv("GOSX_BROWSER_EXECUTABLE")
	if browser == "" {
		t.Skip("set GOSX_BROWSER_EXECUTABLE to verify document snapshots")
	}
	bundle, err := filepath.Abs("../js/bootstrap-runtime.js")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootstrap.js" {
			http.ServeFile(w, r, bundle)
			return
		}
		disableObserver := ""
		if r.URL.Query().Get("observer") == "off" {
			disableObserver = "window.MutationObserver = undefined;"
		}
		fmt.Fprintf(w, `<!doctype html><html><head><title>Document snapshot fixture</title><script>
			window.scanCount = 0;
			const select = Element.prototype.querySelectorAll;
			Element.prototype.querySelectorAll = function(selector) {
				if (selector === "[data-gosx-enhance]") window.scanCount++;
				return select.call(this, selector);
			};
			%s
		</script><script defer src="/bootstrap.js"></script></head>
		<body data-gosx-enhance="html" id="root">
			<section id="entry" data-gosx-enhance="engine" data-gosx-enhance-layer="runtime"
				data-gosx-engine="Workspace" data-gosx-fallback="server"></section>
			%s
		</body></html>`, disableObserver, strings.Repeat("<article><span>Shot fixture</span></article>", 240))
	}))
	defer server.Close()
	for _, observer := range []string{"on", "off"} {
		t.Run(observer, func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(browser), chromedp.Flag("headless", "new"), chromedp.NoSandbox)
			allocator, stop := chromedp.NewExecAllocator(parent, options...)
			defer stop()
			ctx, closeBrowser := chromedp.NewContext(allocator)
			defer closeBrowser()
			var got struct {
				Initial, Unchanged int
				Correct            bool
				Failure            string
			}
			const verify = `(() => {
				const snapshots = window.__gosx.document;
				const check = (ok, message) => { if (!ok) throw new Error(message); };
				try {
					const state = snapshots.refresh("initial");
					const initial = window.scanCount;
					check(state.enhancements.count === 2, "missing root or descendant enhancement");
					const returned = snapshots.get();
					returned.enhancements.entries[0].kind = "caller mutation";
					check(snapshots.get().enhancements.entries[0].kind === "html", "snapshot aliases public state");
					snapshots.refresh("unchanged");
					snapshots.refresh("unchanged again");
					const unchanged = window.scanCount;
					const added = document.createElement("div");
					added.id = "added";
					added.dataset.gosxEnhance = "island";
					document.body.append(added);
					check(snapshots.refresh("append").enhancements.count === 3, "synchronous append is stale");
					const entry = document.getElementById("entry");
					entry.id = "renamed";
					entry.dataset.gosxEnhance = "video";
					entry.dataset.gosxEnhanceLayer = "bootstrap";
					entry.dataset.gosxFallback = "poster";
					entry.dataset.gosxEngine = "Review";
					let enhancement = snapshots.refresh("attributes").enhancements.entries[1];
					check(enhancement.id === "renamed" && enhancement.kind === "video"
						&& enhancement.layer === "bootstrap" && enhancement.fallback === "poster"
						&& enhancement.engine === "Review", "synchronous attributes are stale");
					document.body.prepend(added);
					check(snapshots.refresh("reorder").enhancements.entries[1].id === "added", "document order is stale");
					added.remove();
					document.body.removeAttribute("data-gosx-enhance");
					check(snapshots.refresh("remove").enhancements.count === 1, "removed enhancement is stale");
					const body = document.createElement("body");
					body.dataset.gosxEnhance = "replacement";
					document.body.replaceWith(body);
					check(snapshots.refresh("replace body").enhancements.entries[0].kind === "replacement", "replaced root is stale");
					return { initial, unchanged, correct: true };
				} catch (error) {
					return { correct: false, failure: error.message };
				}
			})()`
			if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"?observer="+observer),
				chromedp.Poll("window.__gosx && window.__gosx.ready", nil), chromedp.Evaluate(verify, &got)); err != nil {
				t.Fatal(err)
			}
			if !got.Correct {
				t.Fatal(got.Failure)
			}
			if observer == "on" && (got.Initial != 1 || got.Unchanged != got.Initial) {
				t.Fatalf("unchanged document was rescanned: %+v", got)
			}
			if observer == "off" && got.Unchanged <= got.Initial {
				t.Fatalf("missing-observer fallback reused a stale cache: %+v", got)
			}
			t.Logf("observer=%s, initial scans=%d, scans after repeated refresh=%d; synchronous append, attributes, order, remove, root replacement and clone isolation passed", observer, got.Initial, got.Unchanged)
		})
	}
}
