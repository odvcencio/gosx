package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
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
	Request       *http.Request
	Pattern       string
	Status        int
	Title         string
	PageID        string
	Path          string
	RequestID     string
	Metadata      Metadata
	RuntimeActive bool
	Navigation    bool
	Head          gosx.Node
	Body          gosx.Node
}

// DocumentAttrs returns the baseline html element attributes for a GoSX page
// document so custom document shells can preserve the framework's document and
// navigation contract.
func DocumentAttrs(doc *DocumentContext) gosx.AttrList {
	attrs := []any{
		gosx.Attr("data-gosx-document", "true"),
	}
	if doc == nil {
		return gosx.Attrs(attrs...)
	}
	if pageID := strings.TrimSpace(doc.PageID); pageID != "" {
		attrs = append(attrs, gosx.Attr("data-gosx-document-id", pageID))
	}
	if path := strings.TrimSpace(doc.Path); path != "" {
		attrs = append(attrs, gosx.Attr("data-gosx-document-path", path))
	}
	if doc.Navigation {
		attrs = append(attrs,
			gosx.Attr("data-gosx-navigation-state", "idle"),
			gosx.Attr("data-gosx-navigation-current-path", documentCurrentPath(doc)),
		)
	}
	return gosx.Attrs(attrs...)
}

// DocumentBodyAttrs returns the baseline body element attributes for a GoSX
// page document so custom document shells can preserve enhancement and
// navigation state.
func DocumentBodyAttrs(doc *DocumentContext) gosx.AttrList {
	attrs := []any{
		gosx.Attr("data-gosx-document-body", "true"),
		gosx.Attr("data-gosx-enhancement-layer", "html"),
	}
	if doc == nil {
		return gosx.Attrs(attrs...)
	}
	if pageID := strings.TrimSpace(doc.PageID); pageID != "" {
		attrs = append(attrs, gosx.Attr("data-gosx-document-id", pageID))
	}
	if doc.Navigation {
		attrs = append(attrs,
			gosx.Attr("data-gosx-navigation-state", "idle"),
			gosx.Attr("data-gosx-navigation-current-path", documentCurrentPath(doc)),
		)
	}
	return gosx.Attrs(attrs...)
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
	runtime  *PageRuntime
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

// ApplyCacheProfile applies a higher-level cache profile to the response.
func (c *Context) ApplyCacheProfile(profile CacheProfile) {
	ApplyCacheProfile(c, profile)
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

// CacheDynamic disables storage for fully dynamic responses.
func (c *Context) CacheDynamic() {
	c.ApplyCacheProfile(DynamicPage())
}

// CacheStatic marks the response as immutable and publicly cacheable.
func (c *Context) CacheStatic(tags ...string) {
	c.ApplyCacheProfile(StaticPage(tags...))
}

// CacheRevalidate marks a page as publicly cacheable with revalidation.
func (c *Context) CacheRevalidate(maxAge, staleWhileRevalidate time.Duration, tags ...string) {
	c.ApplyCacheProfile(RevalidatePage(maxAge, staleWhileRevalidate, tags...))
}

// CacheData marks shared data as publicly cacheable.
func (c *Context) CacheData(maxAge time.Duration, tags ...string) {
	c.ApplyCacheProfile(PublicData(maxAge, tags...))
}

// CachePrivateData marks user-scoped data as privately cacheable.
func (c *Context) CachePrivateData(maxAge time.Duration, tags ...string) {
	c.ApplyCacheProfile(PrivateData(maxAge, tags...))
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

// Runtime returns the page-scoped runtime registry for client engines.
func (c *Context) Runtime() *PageRuntime {
	if c == nil {
		return nil
	}
	if c.runtime == nil {
		c.runtime = NewPageRuntime()
	}
	return c.runtime
}

// Engine registers a client engine for this page and returns its mount shell.
func (c *Context) Engine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if c == nil {
		return fallback
	}
	return c.Runtime().Engine(cfg, fallback)
}

// TextBlock renders a managed text-layout node for the current page.
func (c *Context) TextBlock(props TextBlockProps, args ...any) gosx.Node {
	if c == nil {
		return TextBlock(props, args...)
	}
	return c.Runtime().TextBlock(props, args...)
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

func (c *Context) documentContext(pattern, defaultTitle string, body gosx.Node, navigation bool) *DocumentContext {
	title := c.metadata.Title
	if title == "" {
		title = defaultTitle
	}
	path := "/"
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.RequestURI()
	}
	doc := &DocumentContext{
		Request:       c.Request,
		Pattern:       pattern,
		Status:        c.status,
		Title:         title,
		PageID:        documentPageID(pattern, path),
		Path:          path,
		RequestID:     RequestID(c.Request),
		Metadata:      c.metadata,
		RuntimeActive: c != nil && c.runtime != nil && c.runtime.Active(),
		Navigation:    navigation,
		Body:          body,
	}
	doc.Head = gosx.Fragment(
		c.headNode(),
		documentContractNode(doc),
	)
	return doc
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

type documentContract struct {
	Version     int                         `json:"version"`
	Page        documentContractPage        `json:"page"`
	Enhancement documentContractEnhancement `json:"enhancement"`
}

type documentContractPage struct {
	ID        string `json:"id"`
	Pattern   string `json:"pattern"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	RequestID string `json:"requestID,omitempty"`
}

type documentContractEnhancement struct {
	Bootstrap  bool `json:"bootstrap"`
	Runtime    bool `json:"runtime"`
	Navigation bool `json:"navigation"`
}

func documentContractNode(doc *DocumentContext) gosx.Node {
	if doc == nil {
		return gosx.Text("")
	}
	payload, err := json.Marshal(documentContract{
		Version: 1,
		Page: documentContractPage{
			ID:        doc.PageID,
			Pattern:   doc.Pattern,
			Path:      doc.Path,
			Title:     doc.Title,
			Status:    doc.Status,
			RequestID: doc.RequestID,
		},
		Enhancement: documentContractEnhancement{
			Bootstrap:  doc.RuntimeActive,
			Runtime:    doc.RuntimeActive,
			Navigation: doc.Navigation,
		},
	})
	if err != nil {
		return gosx.Text("")
	}
	safe := strings.NewReplacer(
		"<", "\\u003c",
		">", "\\u003e",
		"&", "\\u0026",
	).Replace(string(payload))
	return gosx.RawHTML(`<script id="gosx-document" type="application/json" data-gosx-document-contract>` + safe + `</script>`)
}

func documentPageID(pattern, path string) string {
	source := strings.TrimSpace(pattern)
	if source == "" {
		source = strings.TrimSpace(path)
	}
	if source == "" {
		source = "page"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(source) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	value := strings.Trim(b.String(), "-")
	if value == "" {
		value = "page"
	}
	return "gosx-doc-" + value
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
	Layer       CSSLayer
	Owner       string
	Source      string
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
	if strings.Contains(strings.ToLower(strings.TrimSpace(l.Rel)), "stylesheet") {
		layer := normalizeCSSLayer(l.Layer)
		attrs["data-gosx-css-layer"] = string(layer)
		attrs["data-gosx-css-owner"] = NormalizeStylesheetOwner(layer, l.Owner)
		if source := strings.TrimSpace(l.Source); source != "" {
			attrs["data-gosx-css-source"] = source
		} else {
			attrs["data-gosx-css-source"] = stylesheetSource(l.Href)
		}
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
