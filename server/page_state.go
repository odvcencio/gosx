package server

import (
	"net/http"
	"strings"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
)

// PageState carries shared request-scoped page response state used by both
// server.Context and route.RouteContext.
type PageState struct {
	requestPath    string
	headers        http.Header
	status         int
	metadata       Metadata
	head           []gosx.Node
	bodyAttrs      gosx.AttrList
	deferred       *DeferredRegistry
	cache          *CacheState
	runtime        *PageRuntime
	nonce          string
	language       string
	navigationHead func(nonce string) gosx.Node
}

// NewPageState creates an empty shared page-state container.
//
// The headers map, deferred registry, and cache state are lazily
// instantiated on first access via Header() / DeferredRegistry() /
// CacheState(). That moves three guaranteed per-request allocations
// off the hot path for handlers that never need them — most of which
// is true for short response paths like 304 Not Modified or static
// passthrough.
func NewPageState() *PageState {
	return &PageState{}
}

// NewPageStateForRequest creates page state whose automatic metadata URLs use
// the request's route path. NewPageState intentionally remains pathless so
// standalone metadata rendering keeps its root-path default.
func NewPageStateForRequest(r *http.Request) *PageState {
	state := NewPageState()
	if r != nil && r.URL != nil {
		state.requestPath = r.URL.Path
	}
	return state
}

// Header returns the response headers to apply when the request completes.
func (s *PageState) Header() http.Header {
	if s == nil {
		return nil
	}
	if s.headers == nil {
		s.headers = make(http.Header)
	}
	return s.headers
}

// StatusCode returns the currently selected response status.
func (s *PageState) StatusCode() int {
	if s == nil {
		return 0
	}
	return s.status
}

// SetStatus sets the HTTP status code for the response.
func (s *PageState) SetStatus(status int) {
	if s == nil {
		return
	}
	s.status = status
}

// Cache stores HTTP caching directives for the response.
func (s *PageState) Cache(policy CachePolicy) {
	if s == nil {
		return
	}
	s.CacheState().SetPolicy(policy)
}

// ApplyCacheProfile applies a higher-level cache profile to the response.
func (s *PageState) ApplyCacheProfile(profile CacheProfile) {
	ApplyCacheProfile(s, profile)
}

// CachePublic marks the response as publicly cacheable for the provided duration.
func (s *PageState) CachePublic(maxAge time.Duration) {
	s.Cache(PublicCache(maxAge))
}

// CachePrivate marks the response as privately cacheable for the provided duration.
func (s *PageState) CachePrivate(maxAge time.Duration) {
	s.Cache(PrivateCache(maxAge))
}

// NoStore disables response storage by caches.
func (s *PageState) NoStore() {
	s.Cache(NoStoreCache())
}

// CacheDynamic disables storage for fully dynamic responses.
func (s *PageState) CacheDynamic() {
	s.ApplyCacheProfile(DynamicPage())
}

// CacheStatic marks the response as immutable and publicly cacheable.
func (s *PageState) CacheStatic(tags ...string) {
	s.ApplyCacheProfile(StaticPage(tags...))
}

// CacheRevalidate marks a page as publicly cacheable with revalidation.
func (s *PageState) CacheRevalidate(maxAge, staleWhileRevalidate time.Duration, tags ...string) {
	s.ApplyCacheProfile(RevalidatePage(maxAge, staleWhileRevalidate, tags...))
}

// CacheData marks shared data as publicly cacheable.
func (s *PageState) CacheData(maxAge time.Duration, tags ...string) {
	s.ApplyCacheProfile(PublicData(maxAge, tags...))
}

// CachePrivateData marks user-scoped data as privately cacheable.
func (s *PageState) CachePrivateData(maxAge time.Duration, tags ...string) {
	s.ApplyCacheProfile(PrivateData(maxAge, tags...))
}

// CacheTag associates one or more revalidation tags with the response.
func (s *PageState) CacheTag(tags ...string) {
	if s == nil {
		return
	}
	s.CacheState().AddTags(tags...)
}

// CacheKey appends cache key dimensions used when deriving automatic ETags.
func (s *PageState) CacheKey(parts ...string) {
	if s == nil {
		return
	}
	s.CacheState().AddKeys(parts...)
}

// SetETag overrides the automatically derived ETag for the response.
func (s *PageState) SetETag(etag string) {
	if s == nil {
		return
	}
	s.CacheState().SetETag(etag)
}

// SetLastModified sets the resource modification timestamp for conditional requests.
func (s *PageState) SetLastModified(at time.Time) {
	if s == nil {
		return
	}
	s.CacheState().SetLastModified(at)
}

// SetNonce records the per-request Content-Security-Policy script nonce so
// document and runtime helpers can attach it to GoSX-owned script elements.
func (s *PageState) SetNonce(nonce string) {
	if s == nil {
		return
	}
	s.nonce = nonce
}

// SetLanguage records the BCP 47 language tag for the document shell.
// Language is request-scoped; PageState deliberately has no implicit global
// language default so applications can choose the locale they serve.
func (s *PageState) SetLanguage(language string) {
	if s == nil {
		return
	}
	s.language = strings.TrimSpace(language)
}

// Language returns the document language configured for this page.
func (s *PageState) Language() string {
	if s == nil {
		return ""
	}
	return s.language
}

// Nonce returns the per-request Content-Security-Policy script nonce set via
// SetNonce, or an empty string when none was set.
func (s *PageState) Nonce() string {
	if s == nil {
		return ""
	}
	return s.nonce
}

// InlineScript renders request-owned executable JavaScript with the nonce
// generated for this page. The helper also escapes authored closing script
// sequences, so callers do not need to hand-build RawHTML.
func (s *PageState) InlineScript(source string) gosx.Node {
	return gosx.InlineScript(source, s.Nonce())
}

// JSONScript renders request-owned JSON data with this page's CSP nonce and
// safe script-text escaping.
func (s *PageState) JSONScript(id string, value any) gosx.Node {
	return gosx.JSONScript(id, s.Nonce(), value)
}

// SetMetadata merges page metadata into the request context.
func (s *PageState) SetMetadata(meta Metadata) {
	if s == nil {
		return
	}
	s.metadata = mergeMetadata(s.metadata, meta)
}

// MetadataValue returns the merged metadata snapshot.
func (s *PageState) MetadataValue() Metadata {
	if s == nil {
		return Metadata{}
	}
	return s.metadata
}

// SetNavigationHead registers the framework-owned navigation-runtime head
// builder. App.EnableNavigation and the route mount seam are the only callers;
// applications should not add a navigation runtime through AddHead because a
// second runtime cannot safely share the request nonce or lifecycle state.
func (s *PageState) SetNavigationHead(fn func(nonce string) gosx.Node) {
	if s == nil {
		return
	}
	s.navigationHead = fn
}

// NavigationEnabled reports whether the framework-owned navigation head
// builder is attached to this page state.
func (s *PageState) NavigationEnabled() bool {
	return s != nil && s.navigationHead != nil
}

// AddHead appends arbitrary head nodes to the response document. The
// framework-owned navigation runtime is deliberately not inferred from these
// nodes; SetNavigationHead is its sole owner.
func (s *PageState) AddHead(nodes ...gosx.Node) {
	if s == nil {
		return
	}
	for _, node := range nodes {
		if node.IsZero() {
			continue
		}
		s.head = append(s.head, node)
	}
}

// BodyAttrs appends attributes to the rendered <body> element.
//
// Multiple calls accumulate rather than clobber, the same rule AddHead
// follows for head nodes: a layout can set a heartbeat attribute and a
// nested page can add another, and both reach the final <body> tag.
// Escaping runs through gosx.RenderAttrs — the same helper El uses for
// every other element's attributes — so a value set here carries the same
// guarantees an attribute written directly on a gosx.El node would.
//
// This exists because HTMLDocument owns the <body> element; application
// code never renders it directly, so it has had no supported way to put an
// attribute there. The first consumer is a body-level
// NavigationHeartbeatAttr — the client runtime's element scan walks
// document.body itself, not just its children (see findElement in
// client/runtime/host/navigation.ts), so an attribute set here is exactly
// as visible to the runtime as one set on any other element. Before this,
// the only way to reach <body> was to render the whole page body inside a
// wrapper gosx.El carrying the attributes and a `display:contents` rule to
// keep it out of layout — see the migration note on NavigationHeartbeatAttr.
func (s *PageState) BodyAttrs(pairs ...any) {
	if s == nil {
		return
	}
	s.bodyAttrs = append(s.bodyAttrs, gosx.Attrs(pairs...)...)
}

// BodyAttrsValue returns the accumulated body attributes.
func (s *PageState) BodyAttrsValue() gosx.AttrList {
	if s == nil {
		return nil
	}
	return s.bodyAttrs
}

// Head renders metadata and appended head nodes into a fragment.
//
// The runtime head (engine manifest + bootstrap scripts) is resolved HERE,
// at Head() call time, rather than being pre-rendered into s.head by the
// page pipeline. The document shell calls Head() after every inner layout
// has rendered, so engines registered by layouts (not just pages) make it
// into the manifest — the manifest is marshaled when Head() runs.
func (s *PageState) Head() gosx.Node {
	if s == nil {
		return gosx.Text("")
	}
	nodes := []gosx.Node{}
	if metaHead := s.metadata.head(SiteMetadata{}, s.requestPath); !metaHead.IsZero() {
		nodes = append(nodes, metaHead)
	}
	nodes = append(nodes, s.head...)
	if s.navigationHead != nil {
		if nav := s.navigationHead(s.nonce); !nav.IsZero() {
			nodes = append(nodes, nav)
		}
	}
	if s.runtime != nil {
		if runtimeHead := s.runtime.HeadWithNonce(s.nonce); !runtimeHead.IsZero() {
			nodes = append(nodes, runtimeHead)
		}
	}
	if len(nodes) == 0 {
		return gosx.Text("")
	}
	return gosx.Fragment(nodes...)
}

// Title returns the current metadata title or a default fallback.
func (s *PageState) Title(fallback string) string {
	if s == nil {
		return fallback
	}
	if title := resolveTitle(s.metadata.Title); title != "" {
		return title
	}
	return fallback
}

// Runtime returns the page-scoped runtime registry for client engines.
func (s *PageState) Runtime() *PageRuntime {
	if s == nil {
		return nil
	}
	if s.runtime == nil {
		s.runtime = NewPageRuntime()
	}
	return s.runtime
}

// RuntimeState returns the current runtime registry without forcing initialization.
func (s *PageState) RuntimeState() *PageRuntime {
	if s == nil {
		return nil
	}
	return s.runtime
}

// Engine registers a client engine for this page and returns its mount shell.
func (s *PageState) Engine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if s == nil {
		return fallback
	}
	return s.Runtime().Engine(cfg, fallback)
}

// ComputeIsland registers a headless island program for the current page.
func (s *PageState) ComputeIsland(cfg island.ComputeIslandConfig) string {
	if s == nil {
		return ""
	}
	return s.Runtime().ComputeIsland(cfg)
}

// Controller registers a declarative headless browser controller for the
// current page.
func (s *PageState) Controller(cfg controller.Config) string {
	if s == nil {
		return ""
	}
	return s.Runtime().Controller(cfg)
}

// TextBlock renders a managed text-layout node for the current page.
func (s *PageState) TextBlock(props TextBlockProps, args ...any) gosx.Node {
	if s == nil {
		return TextBlock(props, args...)
	}
	return s.Runtime().TextBlock(props, args...)
}

// ManagedScript appends a GoSX-managed external script to the page runtime.
func (s *PageState) ManagedScript(src string, opts ManagedScriptOptions, args ...any) {
	if s == nil {
		return
	}
	s.Runtime().ManagedScript(src, opts, args...)
}

// LifecycleScript appends a lifecycle helper script after runtime bootstrap
// assets so it can safely chain onto GoSX page hooks.
func (s *PageState) LifecycleScript(src string, args ...any) {
	if s == nil {
		return
	}
	s.Runtime().LifecycleScript(src, args...)
}

// Defer renders fallback content immediately, then streams the resolved node
// into place once the resolver finishes.
func (s *PageState) Defer(fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	return s.DeferWithOptions(DeferredOptions{}, fallback, resolve)
}

// Suspense renders a component-level streaming boundary. It streams with the
// same completion-order behavior as Defer while marking the boundary for tools.
func (s *PageState) Suspense(fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	return s.SuspenseWithOptions(DeferredOptions{}, fallback, resolve)
}

// SuspenseWithOptions renders a component-level streaming boundary with
// explicit placeholder options.
func (s *PageState) SuspenseWithOptions(opts DeferredOptions, fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	if s == nil {
		return fallback
	}
	return s.DeferredRegistry().SuspenseWithOptions(opts, fallback, resolve)
}

// DeferWithOptions renders fallback content immediately, then streams the
// resolved node into place once the resolver finishes.
func (s *PageState) DeferWithOptions(opts DeferredOptions, fallback gosx.Node, resolve DeferredResolver) gosx.Node {
	if s == nil {
		return fallback
	}
	return s.DeferredRegistry().DeferWithOptions(opts, fallback, resolve)
}

// DeferredRegistry returns the page-scoped deferred fragment registry.
func (s *PageState) DeferredRegistry() *DeferredRegistry {
	if s == nil {
		return nil
	}
	if s.deferred == nil {
		s.deferred = NewDeferredRegistry()
	}
	return s.deferred
}

// CacheState returns the page-scoped cache state.
func (s *PageState) CacheState() *CacheState {
	if s == nil {
		return nil
	}
	if s.cache == nil {
		s.cache = NewCacheState()
	}
	return s.cache
}
