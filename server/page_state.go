package server

import (
	"net/http"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
)

// PageState carries shared request-scoped page response state used by both
// server.Context and route.RouteContext.
type PageState struct {
	headers  http.Header
	status   int
	metadata Metadata
	head     []gosx.Node
	deferred *DeferredRegistry
	cache    *CacheState
	runtime  *PageRuntime
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

// AddHead appends arbitrary head nodes to the response document.
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

// Head renders metadata and appended head nodes into a fragment.
func (s *PageState) Head() gosx.Node {
	if s == nil {
		return gosx.Text("")
	}
	nodes := []gosx.Node{}
	if metaHead := s.metadata.Head(); !metaHead.IsZero() {
		nodes = append(nodes, metaHead)
	}
	nodes = append(nodes, s.head...)
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
