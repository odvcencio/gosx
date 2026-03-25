package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/odvcencio/gosx"
)

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
