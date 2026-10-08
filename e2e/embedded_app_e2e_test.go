//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/internal/chrometest"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// TestEmbeddedAppUnderPrefix uses distinct HTTPS sites for the parent and app.
// It proves native links/assets, lazy controller and hub bundles, WebSocket
// upgrades, partitioned session persistence and CSRF-protected form redirects.
func TestEmbeddedAppUnderPrefix(t *testing.T) {
	const prefix = "/.proxy/game"
	var appURL string
	parent := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><title>Embedding test</title><iframe src="%s%s/" title="GoSX app"></iframe>`, appURL, prefix)
	}))
	defer parent.Close()
	app := server.New()
	if err := app.SetBasePath(prefix); err != nil {
		t.Fatal(err)
	}
	if err := app.EnableSecurityPolicy(server.SecurityPolicy{FrameAncestors: []string{parent.URL}}); err != nil {
		t.Fatal(err)
	}
	app.SetRuntimeRoot(e2eRepoRoot(t))
	public := t.TempDir()
	if err := os.WriteFile(filepath.Join(public, "style.css"), []byte("body { color: rgb(1, 2, 3); }"), 0600); err != nil {
		t.Fatal(err)
	}
	app.SetPublicDir(public)
	sessions := session.MustNew("embedded-browser-test-secret", session.Options{Path: prefix, SameSite: http.SameSiteNoneMode, Partitioned: true})
	app.Use(sessions.Middleware)
	room := hub.New("embedded-room")
	room.RequireOrigin = true
	app.Mount("/ws", room)
	app.Page("/", func(ctx *server.Context) gosx.Node {
		store := session.Current(ctx.Request)
		var visits int
		store.Decode("visits", &visits)
		store.Set("visits", visits+1)
		ctx.AddHead(gosx.El("link", gosx.Attrs(gosx.Attr("rel", "stylesheet"), gosx.Attr("href", "/style.css"))))
		ctx.Runtime().Controller(controller.Config{Resources: []controller.FetchResource{{Name: "state", URL: "/api/state", Output: "$state"}}})
		ctx.Runtime().BindHub("room", "/ws", nil)
		return gosx.Fragment(
			gosx.El("a", gosx.Attrs(gosx.Attr("id", "next"), gosx.Attr("href", "/done")), gosx.Text("Next")),
			gosx.El("form", gosx.Attrs(gosx.Attr("method", "POST"), gosx.Attr("action", "/save")),
				gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", session.Token(ctx.Request)))),
				gosx.El("button", gosx.Text("Save"))),
		)
	})
	app.API("GET /api/state", func(ctx *server.Context) (any, error) {
		var visits int
		session.Current(ctx.Request).Decode("visits", &visits)
		return map[string]any{"visits": visits}, nil
	})
	app.Page("/done", func(ctx *server.Context) gosx.Node {
		return gosx.El("div", gosx.Attrs(gosx.Attr("id", "saved")), gosx.Text(session.Current(ctx.Request).String("saved")))
	})
	actions := action.NewRegistry()
	actions.Register("save", func(ctx *action.Context) error {
		session.Current(ctx.Request).Set("saved", "yes")
		ctx.Redirect("/done")
		return nil
	})
	app.Mount("/save", sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("name", "save"); actions.ServeHTTP(w, r) })))
	var unprefixed atomic.Int32
	built := app.Build()
	web := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			unprefixed.Add(1)
		}
		built.ServeHTTP(w, r)
	}))
	defer web.Close()
	appURL = strings.Replace(web.URL, "127.0.0.1", "localhost", 1)
	chrome := os.Getenv("GOSX_CHROME_BIN")
	if chrome == "" {
		chrome = findChrome(t)
	}
	browser, err := chrometest.Start(t.Context(), chrome, "--no-sandbox", "--ignore-certificate-errors", "--disable-site-isolation-trials")
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	ctx, cancel := context.WithTimeout(browser.Context, 40*time.Second)
	defer cancel()
	var child atomic.Int64
	chromedp.ListenTarget(ctx, func(event any) {
		if e, ok := event.(*runtime.EventExecutionContextCreated); ok && e.Context.Origin == appURL && e.Context.Name == "" {
			child.Store(int64(e.Context.ID))
		}
	})
	if err := chromedp.Run(ctx, runtime.Enable(), chromedp.Navigate(parent.URL)); err != nil {
		t.Fatal(err)
	}
	evaluate := func(expression string, out any) error {
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			result, exception, err := runtime.Evaluate(expression).WithContextID(runtime.ExecutionContextID(child.Load())).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
			if err != nil {
				return err
			}
			if exception != nil {
				return fmt.Errorf("iframe evaluation: %s", exception.Text)
			}
			if out != nil {
				return json.Unmarshal(result.Value, out)
			}
			return nil
		}))
	}
	wait := func(expression string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			var ok bool
			if child.Load() != 0 && evaluate(expression, &ok) == nil && ok {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		var diagnostic string
		diagnosticErr := evaluate(`JSON.stringify({href:location.href, keys:Object.keys(window.__gosx || {}), hubs:Array.from(window.__gosx?.hubs?.keys() || []), manifest:document.getElementById("gosx-manifest")?.textContent})`, &diagnostic)
		t.Fatalf("iframe did not reach %s: context=%d diagnostic=%s error=%v", expression, child.Load(), diagnostic, diagnosticErr)
	}
	wait(`!!window.__gosx && !!window.__gosx.hubs && !!window.__gosx.hubs.get("gosx-hub-0") && window.__gosx.hubs.get("gosx-hub-0").socket.readyState === 1`)
	var result struct {
		Visits      int             `json:"visits"`
		Denied      int             `json:"denied"`
		Redirect    string          `json:"redirect"`
		Link        string          `json:"link"`
		Color       string          `json:"color"`
		Controllers bool            `json:"controllers"`
		Status      int             `json:"status"`
		Saved       json.RawMessage `json:"saved"`
	}
	err = evaluate(`(async () => {
  const visits = await (await fetch("/.proxy/game/api/state")).json();
  const token = document.querySelector('[name="csrf_token"]').value;
  const denied = await fetch("/.proxy/game/save", {method:"POST",headers:{Accept:"application/json"}});
  const savedResponse = await fetch(document.querySelector("form").getAttribute("action"), {method:"POST",headers:{Accept:"application/json","Content-Type":"application/x-www-form-urlencoded","X-CSRF-Token":token},body:new URLSearchParams({csrf_token:token})});
  const saved = await savedResponse.json();
  return {status:savedResponse.status,saved:saved,visits:visits.visits, denied:denied.status, redirect:saved.redirect, link:document.querySelector("#next").getAttribute("href"), color:getComputedStyle(document.body).color, controllers:!!window.__gosx_bootstrap_features.controllers};
 })()`, &result)
	if err != nil || result.Visits != 1 || result.Denied != 403 || result.Redirect != prefix+"/done" || result.Link != prefix+"/done" || result.Color != "rgb(1, 2, 3)" || !result.Controllers {
		t.Fatalf("iframe cookies, CSRF, links, assets or lazy controller: %v result=%+v", err, result)
	}
	if err := evaluate(`document.querySelector("form").requestSubmit(); true`, nil); err != nil {
		t.Fatal(err)
	}
	wait(`location.pathname === "/.proxy/game/done" && document.getElementById("saved")?.textContent === "yes"`)
	if unprefixed.Load() != 0 {
		t.Fatalf("%d requests escaped the base path", unprefixed.Load())
	}
}
