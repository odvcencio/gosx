package route

import (
	"m31labs.dev/gosx/internal/observationcatalog"
	"m31labs.dev/gosx/server"
)

var _ server.ObservationCatalogProvider = (*builtRouter)(nil)
var _ server.HeadConfigurable = (*builtRouter)(nil)

func (b *builtRouter) ObservationPatterns(limit int) ([]server.ObservationPattern, bool) {
	c := observationcatalog.New(limit)
	var visit func(string, []Route)
	visit = func(prefix string, routes []Route) {
		for _, route := range routes {
			pattern := joinPattern(prefix, route.Pattern)
			if route.Handler != nil {
				c.Register("page", pattern)
				c.Register("error", pattern)
			}
			visit(pattern, route.Children)
		}
	}
	visit("", b.router.routes)
	for _, extra := range b.router.handlers {
		c.Register(extra.kind, extra.pattern)
	}
	return c.Result()
}

func (b *builtRouter) SetHeadDecorators(decorators []server.HeadDecorator) {
	b.router.headDecorators = append([]server.HeadDecorator(nil), decorators...)
}

func (r *Router) decoratePageContext(ctx *RouteContext) {
	if len(r.headDecorators) == 0 {
		return
	}
	view := &server.Context{Request: ctx.Request, Pattern: ctx.pattern, PageState: ctx.PageState}
	for _, decorate := range r.headDecorators {
		if decorate != nil {
			if node, ok := decorate(view); ok {
				view.AddHead(node)
			}
		}
	}
	ctx.PageState = view.PageState
}
