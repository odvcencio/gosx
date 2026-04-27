package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestAppUseEdgeRegistersDescriptorAndMiddleware(t *testing.T) {
	app := New()
	app.UseEdge(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsEdgeRequest(r) {
				w.Header().Set("X-Test-Edge", "yes")
			}
			next.ServeHTTP(w, r)
		})
	}, EdgeMiddlewareOptions{
		Name:    "geo",
		Pattern: "/docs/*",
		Source:  "edge/geo.js",
	})
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.Text("home")
	})

	descriptors := app.EdgeMiddleware()
	if len(descriptors) != 1 {
		t.Fatalf("expected one edge descriptor, got %#v", descriptors)
	}
	if descriptors[0].Name != "geo" || descriptors[0].Pattern != "/docs/*" || descriptors[0].Runtime != EdgeRuntimeWorker || descriptors[0].Source != "edge/geo.js" {
		t.Fatalf("unexpected edge descriptor %#v", descriptors[0])
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-gosx-edge", "1")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if got := w.Header().Get("X-Test-Edge"); got != "yes" {
		t.Fatalf("expected edge-aware middleware header, got %q", got)
	}
}

func TestWithEdgeRequestMarksRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if IsEdgeRequest(req) {
		t.Fatal("expected plain request not to be marked as edge")
	}
	if !IsEdgeRequest(WithEdgeRequest(req)) {
		t.Fatal("expected context-marked request to be edge")
	}
}
