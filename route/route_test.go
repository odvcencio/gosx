package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/server"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
	r.ResponseRecorder.Flush()
}

func TestRouterBasic(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return body
	})

	called := false
	router.Add(Route{
		Pattern: "/test",
		Handler: func(ctx *RouteContext) gosx.Node {
			called = true
			return gosx.Text("hello")
		},
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("handler not called")
	}
}

func TestRouterDataLoader(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return body
	})

	router.Add(Route{
		Pattern: "/data",
		DataLoader: func(ctx *RouteContext) (any, error) {
			return "loaded", nil
		},
		Handler: func(ctx *RouteContext) gosx.Node {
			data := ctx.Data.(string)
			return gosx.Text(data)
		},
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "loaded" {
		t.Fatalf("expected body 'loaded', got %q", w.Body.String())
	}
}

func TestRouterDataLoaderNotFoundUsesNotFoundPage(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return body
	})
	router.SetNotFound(func(ctx *RouteContext) gosx.Node {
		return gosx.Text("missing-page")
	})

	called := false
	router.Add(Route{
		Pattern: "/posts/{slug}",
		DataLoader: func(ctx *RouteContext) (any, error) {
			return nil, NotFound("post missing")
		},
		Handler: func(ctx *RouteContext) gosx.Node {
			called = true
			return gosx.Text("post")
		},
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/posts/ghost", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Fatal("handler should not be called after not-found loader result")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing-page") {
		t.Fatalf("expected not-found page, got %q", w.Body.String())
	}
}

func TestRouteContextParam(t *testing.T) {
	ctx := &RouteContext{
		Params: map[string]string{"id": "42"},
	}
	if ctx.Param("id") != "42" {
		t.Fatalf("expected '42', got %q", ctx.Param("id"))
	}
	if ctx.Param("missing") != "" {
		t.Fatalf("expected empty string for missing param, got %q", ctx.Param("missing"))
	}
}

func TestRouteContextQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?page=3", nil)
	ctx := &RouteContext{
		Request: req,
		Params:  map[string]string{},
	}
	if ctx.Query("page") != "3" {
		t.Fatalf("expected '3', got %q", ctx.Query("page"))
	}
}

func TestRouteContextParentData(t *testing.T) {
	ctx := &RouteContext{}
	if ctx.ParentData("key") != nil {
		t.Fatal("expected nil for nil parentData map")
	}
}

func TestRouterMultipleRoutes(t *testing.T) {
	router := NewRouter()

	router.Add(
		Route{
			Pattern: "/a",
			Handler: func(ctx *RouteContext) gosx.Node {
				return gosx.Text("route-a")
			},
		},
		Route{
			Pattern: "/b",
			Handler: func(ctx *RouteContext) gosx.Node {
				return gosx.Text("route-b")
			},
		},
	)

	handler := router.Build()

	reqA := httptest.NewRequest("GET", "/a", nil)
	wA := httptest.NewRecorder()
	handler.ServeHTTP(wA, reqA)
	if wA.Body.String() != "route-a" {
		t.Fatalf("expected 'route-a', got %q", wA.Body.String())
	}

	reqB := httptest.NewRequest("GET", "/b", nil)
	wB := httptest.NewRecorder()
	handler.ServeHTTP(wB, reqB)
	if wB.Body.String() != "route-b" {
		t.Fatalf("expected 'route-b', got %q", wB.Body.String())
	}
}

func TestRouterCustomNotFoundWinsOverRootRoute(t *testing.T) {
	router := NewRouter()
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.Text("home")
		},
	})
	router.SetNotFound(func(ctx *RouteContext) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: "Missing"})
		return gosx.Text("missing")
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing") {
		t.Fatalf("expected custom not found page, got %q", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "home") {
		t.Fatalf("expected exact root route, got %q", w.Body.String())
	}
}

func TestRouterErrorHandlerHandlesPanics(t *testing.T) {
	router := NewRouter()
	router.SetError(func(ctx *RouteContext, err error) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: "Broken"})
		return gosx.Text("error:" + err.Error())
	})
	router.Add(Route{
		Pattern: "/panic",
		Handler: func(ctx *RouteContext) gosx.Node {
			panic("boom")
		},
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "error:boom") {
		t.Fatalf("unexpected error body %q", w.Body.String())
	}
}

func TestRouterContextMetadataCanDriveLayout(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Fallback"), ctx.Head(), body)
	})
	router.Add(Route{
		Pattern: "/docs",
		Handler: func(ctx *RouteContext) gosx.Node {
			ctx.SetMetadata(server.Metadata{
				Title:       "Docs",
				Description: "Route metadata",
			})
			ctx.AddHead(gosx.El("link", gosx.Attrs(gosx.Attr("rel", "stylesheet"), gosx.Attr("href", "/docs.css"))))
			return gosx.Text("docs-body")
		},
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		"<title>Docs</title>",
		"docs-body",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`name="description"`,
		`content="Route metadata"`,
		`rel="stylesheet"`,
		`href="/docs.css"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestRouterDeferredRegionStreamsIntoHTMLDocument(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Deferred"), ctx.Head(), body)
	})
	router.Add(Route{
		Pattern: "/stream",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("main",
				ctx.Defer(
					gosx.El("p", gosx.Text("loading-route")),
					func() (gosx.Node, error) {
						return gosx.El("section", gosx.Text("resolved-route")), nil
					},
				),
			)
		},
	})

	handler := router.Build()
	req := httptest.NewRequest("GET", "/stream", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !w.flushed {
		t.Fatal("expected streaming response to flush")
	}
	for _, snippet := range []string{
		"loading-route",
		"resolved-route",
		`data-gosx-deferred`,
		`data-gosx-stream-template`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
	if strings.Contains(body, "<!--gosx-stream-tail-->") {
		t.Fatalf("expected stream tail marker to be removed, got %q", body)
	}
}

func TestRouterCacheHeadersAndRevalidateTag(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return body
	})
	router.Add(Route{
		Pattern: "/cached",
		Handler: func(ctx *RouteContext) gosx.Node {
			ctx.Cache(server.CachePolicy{
				Public: true,
				MaxAge: 45 * time.Second,
			})
			ctx.CacheTag("docs-pages")
			return gosx.Text("cached-route")
		},
	})

	handler := router.Build()

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if cacheControl := w.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "max-age=45") {
		t.Fatalf("expected cache header, got %q", cacheControl)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected etag in %v", w.Header())
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	notModifiedReq.Header.Set("If-None-Match", etag)
	notModifiedRes := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedRes, notModifiedReq)
	if notModifiedRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", notModifiedRes.Code, notModifiedRes.Body.String())
	}

	router.RevalidateTag("docs-pages")

	updatedReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	updatedReq.Header.Set("If-None-Match", etag)
	updatedRes := httptest.NewRecorder()
	handler.ServeHTTP(updatedRes, updatedReq)
	if updatedRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after revalidate, got %d: %s", updatedRes.Code, updatedRes.Body.String())
	}
	if nextETag := updatedRes.Header().Get("ETag"); nextETag == "" || nextETag == etag {
		t.Fatalf("expected new etag after revalidate, got %q", nextETag)
	}
}

func TestRouterObserverCapturesRouteMetadata(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return body
	})
	var events []server.RequestEvent
	router.UseObserver(server.RequestObserverFunc(func(event server.RequestEvent) {
		events = append(events, event)
	}))
	router.Add(Route{
		Pattern: "/docs/{slug}",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.Text("docs")
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/docs/intro", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	event := events[0]
	if event.Kind != "page" || event.Pattern != "/docs/{slug}" {
		t.Fatalf("unexpected event %#v", event)
	}
	if event.Path != "/docs/intro" || event.Status != http.StatusOK {
		t.Fatalf("unexpected event %#v", event)
	}
}
