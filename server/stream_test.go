package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func TestAppSuspenseStreamsComponentBoundariesByCompletion(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.El("main",
			ctx.Suspense(
				gosx.El("p", gosx.Text("slow pending")),
				func() (gosx.Node, error) {
					time.Sleep(25 * time.Millisecond)
					return gosx.El("section", gosx.Text("SLOW-DONE")), nil
				},
			),
			ctx.Suspense(
				gosx.El("p", gosx.Text("fast pending")),
				func() (gosx.Node, error) {
					return gosx.El("section", gosx.Text("FAST-DONE")), nil
				},
			),
		)
	})

	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	body := w.Body.String()
	if !strings.Contains(body, `data-gosx-stream-boundary="component"`) {
		t.Fatalf("expected component stream boundary marker, got %q", body)
	}
	if !strings.Contains(body, `data-gosx-stream-template data-gosx-stream-target=`) {
		t.Fatalf("expected declarative stream target marker, got %q", body)
	}
	fast := strings.Index(body, "FAST-DONE")
	slow := strings.Index(body, "SLOW-DONE")
	if fast == -1 || slow == -1 {
		t.Fatalf("expected both streamed fragments, got %q", body)
	}
	if fast > slow {
		t.Fatalf("expected faster component boundary to stream first, got %q", body)
	}
}

func TestRenderDeferredChunkUsesCoreLifecycleWithNativeFallback(t *testing.T) {
	body := renderDeferredChunk("slot-1", `<section>resolved</section>`)
	if !strings.Contains(body, `window.__gosx&&window.__gosx.dom&&typeof window.__gosx.dom.replaceFragment==="function"`) {
		t.Fatalf("expected streamed chunk to prefer the core fragment lifecycle, got %q", body)
	}
	if !strings.Contains(body, `if(!replaced){slot.replaceWith(content);}`) {
		t.Fatalf("expected streamed chunk to retain native replacement fallback, got %q", body)
	}
	if !strings.Contains(body, `tpl.remove()`) {
		t.Fatalf("expected streamed chunk to remove its declarative template, got %q", body)
	}
}
