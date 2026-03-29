package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	Request       *http.Request
	Pattern       string
	Status        int
	Title         string
	PageID        string
	Path          string
	RequestID     string
	Metadata      Metadata
	Bootstrap     bool
	RuntimeActive bool
	Runtime       PageRuntimeSummary
	Navigation    bool
	Head          gosx.Node
	Body          gosx.Node
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
	Request *http.Request
	PageState
}

func newContext(r *http.Request) *Context {
	return &Context{
		Request:   r,
		PageState: *NewPageState(),
	}
}

func (c *Context) documentContext(pattern, defaultTitle string, body gosx.Node, navigation bool) *DocumentContext {
	c = ensureContext(c)
	title := c.Title(defaultTitle)
	path := documentContextPath(c.Request)
	metadata := c.MetadataValue()
	doc := &DocumentContext{
		Request:    c.Request,
		Pattern:    pattern,
		Status:     c.StatusCode(),
		Title:      title,
		PageID:     documentPageID(pattern, path),
		Path:       path,
		RequestID:  RequestID(c.Request),
		Metadata:   metadata,
		Navigation: navigation,
		Body:       body,
	}
	if runtime := c.RuntimeState(); runtime != nil {
		doc.Runtime = runtime.Summary()
		doc.Bootstrap = doc.Runtime.Bootstrap
		doc.RuntimeActive = doc.Runtime.Runtime
	}
	doc.Head = gosx.Fragment(
		c.Head(),
		documentContractNode(doc),
	)
	return doc
}

func ensureContext(c *Context) *Context {
	if c != nil {
		return c
	}
	return newContext(nil)
}

func documentContextPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	if requestURI := strings.TrimSpace(r.URL.RequestURI()); requestURI != "" {
		return requestURI
	}
	return "/"
}

type documentContract struct {
	Version     int                         `json:"version"`
	Page        documentContractPage        `json:"page"`
	Enhancement documentContractEnhancement `json:"enhancement"`
	Assets      documentContractAssets      `json:"assets"`
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

type documentContractAssets struct {
	BootstrapMode string `json:"bootstrapMode"`
	Manifest      bool   `json:"manifest"`
	RuntimePath   string `json:"runtimePath,omitempty"`
	WASMExecPath  string `json:"wasmExecPath,omitempty"`
	PatchPath     string `json:"patchPath,omitempty"`
	BootstrapPath string `json:"bootstrapPath,omitempty"`
	Islands       int    `json:"islands,omitempty"`
	Engines       int    `json:"engines,omitempty"`
	Hubs          int    `json:"hubs,omitempty"`
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
			Bootstrap:  doc.Bootstrap || doc.Navigation,
			Runtime:    doc.RuntimeActive,
			Navigation: doc.Navigation,
		},
		Assets: documentContractAssets{
			BootstrapMode: documentBootstrapMode(doc.Runtime.BootstrapMode),
			Manifest:      doc.Runtime.Manifest,
			RuntimePath:   doc.Runtime.RuntimePath,
			WASMExecPath:  doc.Runtime.WASMExecPath,
			PatchPath:     doc.Runtime.PatchPath,
			BootstrapPath: doc.Runtime.BootstrapPath,
			Islands:       doc.Runtime.Islands,
			Engines:       doc.Runtime.Engines,
			Hubs:          doc.Runtime.Hubs,
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

func documentBootstrapMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "lite", "full":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "none"
	}
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
	return gosx.El("link", gosx.Attrs(linkTagAttrs(l)...))
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
	return gosx.El("meta", gosx.Attrs(metaTagAttrs(tag)...))
}

func linkTagAttrs(l LinkTag) []any {
	attrs := []any{}
	attrs = appendNonEmptyAttr(attrs, "rel", l.Rel)
	attrs = appendNonEmptyAttr(attrs, "href", AssetURL(l.Href))
	attrs = appendNonEmptyAttr(attrs, "type", l.Type)
	attrs = appendNonEmptyAttr(attrs, "sizes", l.Sizes)
	attrs = appendNonEmptyAttr(attrs, "media", l.Media)
	attrs = appendNonEmptyAttr(attrs, "as", l.As)
	attrs = appendNonEmptyAttr(attrs, "crossorigin", l.CrossOrigin)
	if linkTagIsStylesheet(l.Rel) {
		attrs = appendStylesheetLinkAttrs(attrs, l)
	}
	return attrs
}

func appendStylesheetLinkAttrs(attrs []any, l LinkTag) []any {
	layer := normalizeCSSLayer(l.Layer)
	attrs = append(attrs,
		gosx.Attr("data-gosx-css-layer", string(layer)),
		gosx.Attr("data-gosx-css-owner", NormalizeStylesheetOwner(layer, l.Owner)),
		gosx.Attr("data-gosx-css-source", linkTagSource(l)),
	)
	return attrs
}

func linkTagSource(l LinkTag) string {
	if source := strings.TrimSpace(l.Source); source != "" {
		return source
	}
	return stylesheetSource(l.Href)
}

func linkTagIsStylesheet(rel string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(rel)), "stylesheet")
}

func metaTagAttrs(tag MetaTag) []any {
	attrs := []any{}
	attrs = appendNonEmptyAttr(attrs, "name", tag.Name)
	attrs = appendNonEmptyAttr(attrs, "property", tag.Property)
	attrs = appendNonEmptyAttr(attrs, "content", tag.Content)
	return attrs
}

func appendNonEmptyAttr(attrs []any, name, value string) []any {
	if value == "" {
		return attrs
	}
	return append(attrs, gosx.Attr(name, value))
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
