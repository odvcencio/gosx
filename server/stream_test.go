package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gosx"
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
	fast := strings.Index(body, "FAST-DONE")
	slow := strings.Index(body, "SLOW-DONE")
	if fast == -1 || slow == -1 {
		t.Fatalf("expected both streamed fragments, got %q", body)
	}
	if fast > slow {
		t.Fatalf("expected faster component boundary to stream first, got %q", body)
	}
}
