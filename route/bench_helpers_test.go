package route

import (
	"net/http"

	"github.com/odvcencio/gosx"
)

// benchStaticRouter returns a router with a few static routes for measuring
// the dispatch overhead on a no-loader, no-params handler.
func benchStaticRouter() http.Handler {
	r := NewRouter()
	r.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body", body))
	})
	r.Add(
		Route{Pattern: "/", Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("home") }},
		Route{Pattern: "/about", Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("about") }},
		Route{Pattern: "/contact", Handler: func(ctx *RouteContext) gosx.Node { return gosx.Text("contact") }},
	)
	return r.Build()
}

// benchParamRouter exercises the param-extraction hot path with two
// path-value parameters per request.
func benchParamRouter() http.Handler {
	r := NewRouter()
	r.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body", body))
	})
	r.Add(Route{
		Pattern: "/users/{userID}/posts/{slug}",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.Text("user " + ctx.Param("userID") + " post " + ctx.Param("slug"))
		},
	})
	return r.Build()
}

// benchNestedRouter exercises a small layout chain (root layout wraps
// dashboard layout wraps page) so we can see the layout-composition cost
// per request.
func benchNestedRouter() http.Handler {
	r := NewRouter()
	r.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body",
			gosx.Attrs(gosx.Attr("class", "root")), body))
	})
	r.Add(Route{
		Pattern: "/dashboard",
		Layout: func(ctx *RouteContext, body gosx.Node) gosx.Node {
			return gosx.El("section",
				gosx.Attrs(gosx.Attr("class", "dashboard")),
				gosx.El("nav", gosx.Text("dashboard nav")),
				body,
			)
		},
		Children: []Route{
			{
				Pattern: "/dashboard/settings",
				Handler: func(ctx *RouteContext) gosx.Node {
					return gosx.El("main", gosx.Text("settings page body"))
				},
			},
		},
	})
	return r.Build()
}
