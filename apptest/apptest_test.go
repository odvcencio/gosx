package apptest

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func TestAppHelperCanDecodeJSON(t *testing.T) {
	app := server.New()
	app.API("GET /api/meta", func(ctx *server.Context) (any, error) {
		return map[string]any{
			"ok":   true,
			"name": "gosx",
		}, nil
	})

	res := App(t, app, Request(http.MethodGet, "/api/meta", nil, WithAcceptJSON()))
	res.AssertStatus(t, http.StatusOK)

	var payload map[string]any
	res.DecodeJSON(t, &payload)
	if payload["name"] != "gosx" {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestRouterHelperSupportsFormPosts(t *testing.T) {
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return body
	})
	router.Add(route.Route{
		Pattern: "POST /submit",
		Handler: func(ctx *route.RouteContext) gosx.Node {
			if err := ctx.Request.ParseForm(); err != nil {
				return gosx.Text(err.Error())
			}
			return gosx.Text(ctx.Request.Form.Get("name"))
		},
	})

	res := Router(t, router, FormRequest(http.MethodPost, "/submit", url.Values{"name": {"GoSX"}}))
	res.AssertStatus(t, http.StatusOK)
	res.AssertContains(t, "GoSX")
}

func TestRequestOptionsAttachHeadersAndCookies(t *testing.T) {
	req := Request(http.MethodGet, "/hello", nil,
		WithHeader("X-Test", "yes"),
		WithCookie(&http.Cookie{Name: "session", Value: "abc"}),
	)

	if got := req.Header.Get("X-Test"); got != "yes" {
		t.Fatalf("expected header to be applied, got %q", got)
	}
	cookie, err := req.Cookie("session")
	if err != nil {
		t.Fatalf("expected cookie to be applied: %v", err)
	}
	if cookie.Value != "abc" {
		t.Fatalf("expected cookie value abc, got %q", cookie.Value)
	}
}
