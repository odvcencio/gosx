package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/odvcencio/gosx"
)

// PageHandler renders an HTML page for a given request context.
type PageHandler func(ctx *Context) gosx.Node

// APIHandler renders a JSON API response for a given request context.
type APIHandler func(ctx *Context) (any, error)

// ErrorHandler renders an HTML error page for a given request context.
type ErrorHandler func(ctx *Context, err error) gosx.Node

// PageRoute registers an HTML page route with optional route middleware.
type PageRoute struct {
	Pattern    string
	Handler    PageHandler
	Middleware []Middleware
}

// APIRoute registers a JSON API route with optional route middleware.
type APIRoute struct {
	Pattern    string
	Handler    APIHandler
	Middleware []Middleware
}

// RedirectRoute redirects matching requests to a new destination.
type RedirectRoute struct {
	Pattern     string
	Destination string
	Status      int
}

// RewriteRoute rewrites matching requests to a different internal path.
type RewriteRoute struct {
	Pattern     string
	Destination string
}

// DocumentFunc renders a full HTML document from a page body and head metadata.
type DocumentFunc func(doc *DocumentContext) gosx.Node

// DocumentContext captures the fully prepared page state used to render a
// document shell.
type DocumentContext struct {
	Request  *http.Request
	Pattern  string
	Status   int
	Title    string
	Metadata Metadata
	Head     gosx.Node
	Body     gosx.Node
}

// DeferredResolver resolves a streamed page fragment after the initial HTML
// shell has been written.
type DeferredResolver func() (gosx.Node, error)

// DeferredOptions configures a streamed placeholder region.
type DeferredOptions struct {
	ID    string
	Tag   string
	Class string
}

type deferredBlock struct {
	id      string
	resolve DeferredResolver
}

// DeferredRegistry tracks deferred page fragments for streaming responses.
type DeferredRegistry struct {
	blocks []deferredBlock
	nextID int
}

// Context carries request-scoped page metadata, headers, and status.
type Context struct {
	Request  *http.Request
	headers  http.Header
	status   int
	metadata Metadata
	head     []gosx.Node
	deferred *DeferredRegistry
	cache    *CacheState
}

func newContext(r *http.Request) *Context {
	return &Context{
		Request:  r,
		headers:  make(http.Header),
		deferred: NewDeferredRegistry(),
		cache:    NewCacheState(),
	}
}

// Header returns the response headers to apply when the request completes.
func (c *Context) Header() http.Header {
	if c.headers == nil {
		c.headers = make(http.Header)
	}
	return c.headers
}

// SetStatus sets the HTTP status code for the response.
func (c *Context) SetStatus(status int) {
	c.status = status
}

// Cache stores HTTP caching directives for the response.
func (c *Context) Cache(policy CachePolicy) {
	if c == nil {
		return
	}
	if c.cache == nil {
		c.cache = NewCacheState()
	}
	c.cache.SetPolicy(policy)
}

// CachePublic marks the response as publicly cacheable for the provided duration.
func (c *Context) CachePublic(maxAge time.Duration) {
	c.Cache(PublicCache(maxAge))
}

// CachePrivate marks the response as privately cacheable for the provided duration.
func (c *Context) CachePrivate(maxAge time.Duration) {
	c.Cache(PrivateCache(maxAge))
}

// NoStore disables response storage by caches.
func (c *Context) NoStore() {
	c.Cache(NoStoreCache())
}

// CacheTag associates one or more revalidation tags with the response.
func (c *Context) CacheTag(tags ...string) {
	if c == nil {
		return
	}
	if c.cache == nil {
		c.cache = NewCacheState()
	}
	c.cache.AddTags(tags...)
}

// CacheKey appends cache key dimensions used when deriving automatic ETags.
func (c *Context) CacheKey(parts ...string) {
	if c == nil {
		return
	}
	if c.cache == nil {
		c.cache = NewCacheState()
	}
	c.cache.AddKeys(parts...)
}

// SetETag overrides the automatically derived ETag for the response.
func (c *Context) SetETag(etag string) {
	if c == nil {
		return
	}
	if c.cache == nil {
		c.cache = NewCacheState()
	}
	c.cache.SetETag(etag)
}

// SetLastModified sets the resource modification timestamp for conditional requests.
func (c *Context) SetLastModified(at time.Time) {
	if c == nil {
		return
	}
	if c.cache == nil {
		c.cache = NewCacheState()
	}
	c.cache.SetLastModified(at)
}

// SetMetadata merges page metadata into the request context.
func (c *Context) SetMetadata(meta Metadata) {
	c.metadata = mergeMetadata(c.metadata, meta)
}

// AddHead appends arbitrary head nodes to the response document.
func (c *Context) AddHead(nodes ...gosx.Node) {
	for _, node := range nodes {
		if node.IsZero() {
			continue
		}
		c.head = append(c.head, node)
	}
}

// Defer renders fallback content immediately, then streams the resolved node
// into place once the resolver finishes.
func (c *Context) Defer(fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	return c.DeferWithOptions(DeferredOptions{}, fallback, resolve)
}

// DeferWithOptions renders fallback content immediately, then streams the
// resolved node into place once the resolver finishes.
func (c *Context) DeferWithOptions(opts DeferredOptions, fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	if c.deferred == nil {
		c.deferred = NewDeferredRegistry()
	}
	return c.deferred.DeferWithOptions(opts, fallback, resolve)
}

func (c *Context) documentContext(pattern, defaultTitle string, body gosx.Node) *DocumentContext {
	title := c.metadata.Title
	if title == "" {
		title = defaultTitle
	}
	return &DocumentContext{
		Request:  c.Request,
		Pattern:  pattern,
		Status:   c.status,
		Title:    title,
		Metadata: c.metadata,
		Head:     c.headNode(),
		Body:     body,
	}
}

func (c *Context) headNode() gosx.Node {
	nodes := []gosx.Node{}
	if metaHead := c.metadata.Head(); !metaHead.IsZero() {
		nodes = append(nodes, metaHead)
	}
	nodes = append(nodes, c.head...)
	if len(nodes) == 0 {
		return gosx.Text("")
	}
	return gosx.Fragment(nodes...)
}

// Metadata describes common document head fields for a server-rendered page.
type Metadata struct {
	Title       string
	Description string
	Canonical   string
	Meta        []MetaTag
	Links       []LinkTag
}

// MetaTag represents a generic <meta> tag.
type MetaTag struct {
	Name     string
	Property string
	Content  string
}

// LinkTag represents a generic <link> tag.
type LinkTag struct {
	Rel         string
	Href        string
	Type        string
	Sizes       string
	Media       string
	As          string
	CrossOrigin string
}

// Head renders metadata into head nodes. Title is handled by the document shell.
func (m Metadata) Head() gosx.Node {
	nodes := []gosx.Node{}
	if m.Description != "" {
		nodes = append(nodes, renderMetaTag(MetaTag{Name: "description", Content: m.Description}))
	}
	if m.Canonical != "" {
		nodes = append(nodes, LinkTag{Rel: "canonical", Href: m.Canonical}.Node())
	}
	for _, tag := range m.Meta {
		nodes = append(nodes, renderMetaTag(tag))
	}
	for _, link := range m.Links {
		nodes = append(nodes, link.Node())
	}
	if len(nodes) == 0 {
		return gosx.Text("")
	}
	return gosx.Fragment(nodes...)
}

// Node renders the link tag as a GoSX node.
func (l LinkTag) Node() gosx.Node {
	attrs := map[string]any{}
	if l.Rel != "" {
		attrs["rel"] = l.Rel
	}
	if l.Href != "" {
		attrs["href"] = l.Href
	}
	if l.Type != "" {
		attrs["type"] = l.Type
	}
	if l.Sizes != "" {
		attrs["sizes"] = l.Sizes
	}
	if l.Media != "" {
		attrs["media"] = l.Media
	}
	if l.As != "" {
		attrs["as"] = l.As
	}
	if l.CrossOrigin != "" {
		attrs["crossorigin"] = l.CrossOrigin
	}
	return gosx.El("link", gosx.Spread(attrs))
}

func mergeMetadata(base, extra Metadata) Metadata {
	if extra.Title != "" {
		base.Title = extra.Title
	}
	if extra.Description != "" {
		base.Description = extra.Description
	}
	if extra.Canonical != "" {
		base.Canonical = extra.Canonical
	}
	if len(extra.Meta) > 0 {
		base.Meta = append(base.Meta, extra.Meta...)
	}
	if len(extra.Links) > 0 {
		base.Links = append(base.Links, extra.Links...)
	}
	return base
}

func renderMetaTag(tag MetaTag) gosx.Node {
	attrs := map[string]any{}
	if tag.Name != "" {
		attrs["name"] = tag.Name
	}
	if tag.Property != "" {
		attrs["property"] = tag.Property
	}
	if tag.Content != "" {
		attrs["content"] = tag.Content
	}
	return gosx.El("meta", gosx.Spread(attrs))
}

// NewDeferredRegistry creates an empty deferred fragment registry.
func NewDeferredRegistry() *DeferredRegistry {
	return &DeferredRegistry{}
}

// HasDeferred reports whether any deferred fragments have been registered.
func (r *DeferredRegistry) HasDeferred() bool {
	return r != nil && len(r.blocks) > 0
}

// Defer renders fallback content immediately, then streams the resolved node
// into place once the resolver finishes.
func (r *DeferredRegistry) Defer(fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	return r.DeferWithOptions(DeferredOptions{}, fallback, resolve)
}

// DeferWithOptions renders fallback content immediately, then streams the
// resolved node into place once the resolver finishes.
func (r *DeferredRegistry) DeferWithOptions(opts DeferredOptions, fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	if resolve == nil {
		return fallback
	}
	if r == nil {
		return fallback
	}

	id := opts.ID
	if id == "" {
		r.nextID++
		id = "gosx-deferred-" + strconv.Itoa(r.nextID)
	}

	tag := opts.Tag
	if tag == "" {
		tag = "div"
	}

	r.blocks = append(r.blocks, deferredBlock{
		id:      id,
		resolve: resolve,
	})

	attrs := []any{
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.BoolAttr("data-gosx-deferred"),
		),
	}
	if opts.Class != "" {
		attrs = append(attrs, gosx.Attrs(gosx.Attr("class", opts.Class)))
	}
	attrs = append(attrs, fallback)
	return gosx.El(tag, attrs...)
}

func (r *DeferredRegistry) snapshot() []deferredBlock {
	if r == nil || len(r.blocks) == 0 {
		return nil
	}
	out := make([]deferredBlock, len(r.blocks))
	copy(out, r.blocks)
	return out
}
