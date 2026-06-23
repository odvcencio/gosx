package route

import "m31labs.dev/gosx/query"

// QueryInto decodes the request's URL query parameters into dst (a pointer to a
// struct) using `query` struct tags — the typed companion to Query.
//
// Where Query(name) returns a single raw string, QueryInto binds the entire
// query string into a typed struct, applying tag defaults and reporting a
// field-named error on a malformed value:
//
//	type Filters struct {
//	    Q    string   `query:"q"`
//	    Page int      `query:"page,default=1"`
//	    Tags []string `query:"tags"`
//	}
//	var f Filters
//	if err := ctx.QueryInto(&f); err != nil { /* 400 */ }
//
// The same query package powers island-side URL state, so a server loader and a
// browser island agree on one typed representation of the URL. See package query.
func (ctx *RouteContext) QueryInto(dst any) error {
	if ctx == nil || ctx.Request == nil || ctx.Request.URL == nil {
		return query.Decode(nil, dst)
	}
	return query.Decode(ctx.Request.URL.Query(), dst)
}
