//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRuntimeSurfaceLifecycleButtonDrivenReplacement(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client", "js", "bootstrap-lite.js"))
	if err != nil {
		t.Fatalf("read generated bootstrap-lite.js: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gosx/bootstrap-lite.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(bootstrap)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>runtime surface lifecycle e2e</title></head>
<body>
  <button id="replace" type="button">replace</button>
  <main id="host">
    <section id="surface" data-gosx-runtime-surface="test-surface" data-gosx-runtime-surface-version="1" data-mode="sync">fallback</section>
    <stateful-surface id="custom" data-gosx-runtime-surface="custom-surface" data-gosx-runtime-surface-version="1" data-label="custom" data-gosx-runtime-issue="old">custom fallback</stateful-surface>
  </main>
  <script src="/gosx/bootstrap-lite.js"></script>
  <script>
    window.__surfaceEvidence = { factories: [], ready: [], unmount: [], disposed: [], aborted: [], staleDisposed: 0, customFactories: [], customDisposed: [], customReadyCallbacks: 0 };
    document.addEventListener("gosx:runtime-surface:mount", function(event) {
      window.__surfaceEvidence.ready.push(event.detail.root.getAttribute("data-label") || event.detail.root.id);
    });
    document.addEventListener("gosx:runtime-surface:unmount", function(event) {
      window.__surfaceEvidence.unmount.push(event.detail.root.getAttribute("data-label") || event.detail.root.id);
    });
    window.__gosx_register_runtime_surface("test-surface", function(ctx) {
      var label = ctx.root.getAttribute("data-label") || ctx.root.id;
      window.__surfaceEvidence.factories.push(label);
      ctx.signal.addEventListener("abort", function() {
        window.__surfaceEvidence.aborted.push(label);
      }, { once: true });
      if (ctx.root.getAttribute("data-mode") === "async") {
        return new Promise(function(resolve) {
          window.__resolveAsyncSurface = function() {
            resolve({ dispose: function() { window.__surfaceEvidence.staleDisposed += 1; } });
          };
        });
      }
      ctx.root.textContent = "mounted " + label;
      return { dispose: function() { window.__surfaceEvidence.disposed.push(label); } };
    });
    customElements.define("stateful-surface", class extends HTMLElement {
      static get observedAttributes() { return ["data-gosx-runtime-state"]; }
      attributeChangedCallback(name, oldValue, newValue) {
        if (newValue !== "ready" || this.getAttribute("data-gosx-runtime-surface") !== "custom-surface") return;
        window.__surfaceEvidence.customReadyCallbacks += 1;
        window.__gosx_dispose_runtime_surfaces(this);
        this.setAttribute("data-gosx-runtime-surface", "custom-final-surface");
        this.setAttribute("data-label", "custom-final");
        window.__gosx_register_runtime_surface("custom-final-surface", function(ctx) {
          window.__surfaceEvidence.customFactories.push("custom-final");
          ctx.root.textContent = "mounted custom final";
          return { dispose: function() { window.__surfaceEvidence.customDisposed.push("custom-final"); } };
        });
        window.__gosx_mount_runtime_surfaces(this);
      }
    });
    window.__gosx_register_runtime_surface("custom-surface", function(ctx) {
      window.__surfaceEvidence.customFactories.push("custom");
      return { dispose: function() { window.__surfaceEvidence.customDisposed.push("custom"); } };
    });
    document.getElementById("replace").addEventListener("click", function() {
      var host = document.getElementById("host");
      var first = document.getElementById("surface");
      window.__gosx_dispose_runtime_surfaces(first);
      var pending = document.createElement("section");
      pending.id = "surface";
      pending.setAttribute("data-gosx-runtime-surface", "test-surface");
      pending.setAttribute("data-gosx-runtime-surface-version", "2");
      pending.setAttribute("data-mode", "async");
      pending.setAttribute("data-label", "pending");
      first.replaceWith(pending);
      window.__gosx_mount_runtime_surfaces(pending);
      setTimeout(function() {
        window.__gosx_dispose_runtime_surfaces(pending);
        var finalRoot = document.createElement("section");
        finalRoot.id = "surface";
        finalRoot.setAttribute("data-gosx-runtime-surface", "test-surface");
        finalRoot.setAttribute("data-gosx-runtime-surface-version", "3");
        finalRoot.setAttribute("data-mode", "sync");
        finalRoot.setAttribute("data-label", "final");
        pending.replaceWith(finalRoot);
        window.__resolveAsyncSurface();
        window.__gosx_mount_runtime_surfaces(finalRoot);
        host.setAttribute("data-complete", "true");
      }, 0);
    });
  </script>
</body>
</html>`))
		}
	}))
	t.Cleanup(server.Close)

	page := newBrowserPage(t, chrome, nil, 1024, 768, "", 60*time.Second)
	if status := page.navigate(t, server.URL+"/"); status < 200 || status > 299 {
		t.Fatalf("fixture returned %d", status)
	}
	page.waitFor(t, `document.querySelector("#surface")?.getAttribute("data-gosx-runtime-state") === "ready"`, 10*time.Second, "initial surface ready")
	page.waitFor(t, `document.querySelector("#custom")?.textContent === "mounted custom final" && document.querySelector("#custom")?.getAttribute("data-gosx-runtime-state") === "ready" && !document.querySelector("#custom")?.hasAttribute("data-gosx-runtime-issue")`, 10*time.Second, "custom-element reentry resolved")
	page.eval(t, `document.querySelector("#replace").click()`, nil)
	page.waitFor(t, `document.querySelector("#host")?.getAttribute("data-complete") === "true" && document.querySelector("#surface")?.textContent === "mounted final"`, 10*time.Second, "button-driven surface replacement")

	var raw string
	page.eval(t, `JSON.stringify(window.__surfaceEvidence)`, &raw)
	var got struct {
		Factories            []string `json:"factories"`
		Ready                []string `json:"ready"`
		Unmount              []string `json:"unmount"`
		Disposed             []string `json:"disposed"`
		Aborted              []string `json:"aborted"`
		StaleDisposed        int      `json:"staleDisposed"`
		CustomFactories      []string `json:"customFactories"`
		CustomDisposed       []string `json:"customDisposed"`
		CustomReadyCallbacks int      `json:"customReadyCallbacks"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode evidence: %v: %s", err, raw)
	}
	if !reflect.DeepEqual(got.Factories, []string{"surface", "pending", "final"}) {
		t.Fatalf("factory order = %#v", got.Factories)
	}
	if !reflect.DeepEqual(got.Ready, []string{"surface", "custom-final", "final"}) {
		t.Fatalf("ready events = %#v", got.Ready)
	}
	if !reflect.DeepEqual(got.Unmount, []string{"surface"}) {
		t.Fatalf("unmount events = %#v", got.Unmount)
	}
	if !reflect.DeepEqual(got.Disposed, []string{"surface"}) {
		t.Fatalf("sync disposals = %#v", got.Disposed)
	}
	if !reflect.DeepEqual(got.Aborted, []string{"surface", "pending"}) {
		t.Fatalf("abort order = %#v", got.Aborted)
	}
	if got.StaleDisposed != 1 {
		t.Fatalf("late async handle disposals = %d, want 1", got.StaleDisposed)
	}
	if !reflect.DeepEqual(got.CustomFactories, []string{"custom", "custom-final"}) {
		t.Fatalf("custom factory order = %#v", got.CustomFactories)
	}
	if !reflect.DeepEqual(got.CustomDisposed, []string{"custom"}) {
		t.Fatalf("custom disposals = %#v", got.CustomDisposed)
	}
	if got.CustomReadyCallbacks != 1 {
		t.Fatalf("custom ready callbacks = %d, want 1", got.CustomReadyCallbacks)
	}
	if len(page.PageErrors()) > 0 {
		t.Fatalf("page errors: %v\nconsole:\n%s", page.PageErrors(), page.Console())
	}
}
