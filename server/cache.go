package server

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CachePolicy describes HTTP caching directives for a response.
type CachePolicy struct {
	Public               bool
	Private              bool
	NoStore              bool
	NoCache              bool
	MaxAge               time.Duration
	SMaxAge              time.Duration
	StaleWhileRevalidate time.Duration
	StaleIfError         time.Duration
	MustRevalidate       bool
	Immutable            bool
}

// PublicCache returns a public cache policy with the provided max-age.
func PublicCache(maxAge time.Duration) CachePolicy {
	return CachePolicy{Public: true, MaxAge: maxAge}
}

// PrivateCache returns a private cache policy with the provided max-age.
func PrivateCache(maxAge time.Duration) CachePolicy {
	return CachePolicy{Private: true, MaxAge: maxAge}
}

// NoStoreCache returns a no-store cache policy.
func NoStoreCache() CachePolicy {
	return CachePolicy{NoStore: true}
}

func (p CachePolicy) headerValue() string {
	directives := []string{}
	switch {
	case p.NoStore:
		directives = append(directives, "no-store")
	case p.NoCache:
		directives = append(directives, "no-cache")
	default:
		switch {
		case p.Public:
			directives = append(directives, "public")
		case p.Private:
			directives = append(directives, "private")
		}
		if p.MaxAge > 0 {
			directives = append(directives, "max-age="+strconv.FormatInt(int64(p.MaxAge/time.Second), 10))
		}
		if p.SMaxAge > 0 {
			directives = append(directives, "s-maxage="+strconv.FormatInt(int64(p.SMaxAge/time.Second), 10))
		}
		if p.StaleWhileRevalidate > 0 {
			directives = append(directives, "stale-while-revalidate="+strconv.FormatInt(int64(p.StaleWhileRevalidate/time.Second), 10))
		}
		if p.StaleIfError > 0 {
			directives = append(directives, "stale-if-error="+strconv.FormatInt(int64(p.StaleIfError/time.Second), 10))
		}
		if p.MustRevalidate {
			directives = append(directives, "must-revalidate")
		}
		if p.Immutable {
			directives = append(directives, "immutable")
		}
	}
	return strings.Join(directives, ", ")
}

// CacheState tracks response caching configuration for a single request.
type CacheState struct {
	policySet    bool
	policy       CachePolicy
	tags         []string
	keys         []string
	etag         string
	lastModified time.Time
}

// NewCacheState creates an empty request cache state.
func NewCacheState() *CacheState {
	return &CacheState{}
}

// SetPolicy stores the response cache policy.
func (c *CacheState) SetPolicy(policy CachePolicy) {
	if c == nil {
		return
	}
	c.policySet = true
	c.policy = policy
}

// AddTags appends revalidation tags used when computing automatic ETags.
func (c *CacheState) AddTags(tags ...string) {
	if c == nil {
		return
	}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		c.tags = append(c.tags, tag)
	}
}

// AddKeys appends cache key dimensions used when computing automatic ETags.
func (c *CacheState) AddKeys(parts ...string) {
	if c == nil {
		return
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		c.keys = append(c.keys, part)
	}
}

// SetETag overrides the automatically derived ETag for the response.
func (c *CacheState) SetETag(etag string) {
	if c == nil {
		return
	}
	etag = strings.TrimSpace(etag)
	if etag == "" {
		c.etag = ""
		return
	}
	c.etag = normalizeETag(etag)
}

// SetLastModified stores the resource modification time used for conditional requests.
func (c *CacheState) SetLastModified(at time.Time) {
	if c == nil {
		return
	}
	c.lastModified = at.UTC().Truncate(time.Second)
}

func (c *CacheState) lastModifiedTime() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.lastModified
}

func (c *CacheState) shouldApply() bool {
	if c == nil {
		return false
	}
	return c.policySet || c.etag != "" || !c.lastModified.IsZero() || len(c.tags) > 0 || len(c.keys) > 0
}

func (c *CacheState) effectiveETag(r *http.Request, status int, revalidator *Revalidator) string {
	if c == nil {
		return ""
	}
	if c.etag != "" {
		return c.etag
	}
	if !c.policySet && c.lastModified.IsZero() && len(c.tags) == 0 && len(c.keys) == 0 {
		return ""
	}
	parts := []string{
		"status=" + strconv.Itoa(status),
	}
	if r != nil && r.URL != nil {
		parts = append(parts,
			"method="+r.Method,
			"path="+cleanCachePath(r.URL.Path),
			"query="+r.URL.RawQuery,
		)
	}
	keys := append([]string(nil), c.keys...)
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, "key="+key)
	}
	if !c.lastModified.IsZero() {
		parts = append(parts, "modified="+c.lastModified.UTC().Format(time.RFC3339Nano))
	}
	if revalidator != nil && r != nil && r.URL != nil {
		parts = append(parts, "path-version="+strconv.FormatUint(revalidator.pathVersion(r.URL.Path), 10))
		tags := append([]string(nil), c.tags...)
		sort.Strings(tags)
		for _, tag := range tags {
			parts = append(parts, "tag="+tag+":"+strconv.FormatUint(revalidator.tagVersion(tag), 10))
		}
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf(`W/"gosx-%s"`, hex.EncodeToString(sum[:8]))
}

// Revalidator tracks path and tag revisions used to invalidate automatic ETags.
type Revalidator struct {
	store RevalidationStore
}

// NewRevalidator creates a revalidator backed by the default in-memory store.
func NewRevalidator() *Revalidator {
	return NewRevalidatorWithStore(nil)
}

// NewRevalidatorWithStore creates a revalidator backed by the provided store.
// A nil store falls back to the default in-memory implementation.
func NewRevalidatorWithStore(store RevalidationStore) *Revalidator {
	if store == nil {
		store = NewInMemoryRevalidationStore()
	}
	return &Revalidator{store: store}
}

// SetStore replaces the backing revalidation store.
func (r *Revalidator) SetStore(store RevalidationStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewInMemoryRevalidationStore()
	}
	r.store = store
}

// Store returns the underlying revalidation store, initializing the default
// in-memory store when needed.
func (r *Revalidator) Store() RevalidationStore {
	if r == nil {
		return nil
	}
	if r.store == nil {
		r.store = NewInMemoryRevalidationStore()
	}
	return r.store
}

// RevalidatePath invalidates cached responses for the provided path prefix.
func (r *Revalidator) RevalidatePath(target string) uint64 {
	if r == nil {
		return 0
	}
	return r.Store().RevalidatePath(target)
}

// RevalidateTag invalidates cached responses associated with the provided tag.
func (r *Revalidator) RevalidateTag(tag string) uint64 {
	if r == nil {
		return 0
	}
	return r.Store().RevalidateTag(tag)
}

func (r *Revalidator) pathVersion(requestPath string) uint64 {
	if r == nil {
		return 0
	}
	return r.Store().PathVersion(requestPath)
}

func (r *Revalidator) tagVersion(tag string) uint64 {
	if r == nil {
		return 0
	}
	return r.Store().TagVersion(tag)
}

// ApplyCacheHeaders writes cache validators into headers and reports whether
// the request should short-circuit as a 304 Not Modified response.
func ApplyCacheHeaders(r *http.Request, headers http.Header, status int, cache *CacheState, revalidator *Revalidator) bool {
	if cache == nil || !cache.shouldApply() || headers == nil {
		return false
	}
	if value := cache.policy.headerValue(); value != "" {
		headers.Set("Cache-Control", value)
	}
	if lastModified := cache.lastModifiedTime(); !lastModified.IsZero() {
		headers.Set("Last-Modified", lastModified.Format(http.TimeFormat))
	}
	etag := cache.effectiveETag(r, status, revalidator)
	if etag != "" {
		headers.Set("ETag", etag)
	}
	if !isConditionalCacheRequest(r, status) {
		return false
	}
	if etag != "" && matchETag(r.Header.Get("If-None-Match"), etag) {
		return true
	}
	if !cache.lastModified.IsZero() {
		if modifiedSince, err := http.ParseTime(r.Header.Get("If-Modified-Since")); err == nil && !cache.lastModified.After(modifiedSince) {
			return true
		}
	}
	return false
}

// WriteNotModified writes a 304 response with the provided headers.
func WriteNotModified(w http.ResponseWriter, headers http.Header) {
	if w == nil {
		return
	}
	copyHeaders(w.Header(), headers)
	w.WriteHeader(http.StatusNotModified)
}

func isConditionalCacheRequest(r *http.Request, status int) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return status == http.StatusOK
}

func normalizeETag(etag string) string {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return ""
	}
	if strings.HasPrefix(etag, `W/"`) || strings.HasPrefix(etag, `"`) {
		return etag
	}
	return `"` + etag + `"`
}

func matchETag(headerValue, etag string) bool {
	if strings.TrimSpace(headerValue) == "" || strings.TrimSpace(etag) == "" {
		return false
	}
	for _, candidate := range strings.Split(headerValue, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
		if stripWeakETag(candidate) == stripWeakETag(etag) {
			return true
		}
	}
	return false
}

func stripWeakETag(etag string) string {
	etag = strings.TrimSpace(etag)
	return strings.TrimPrefix(etag, "W/")
}

func cleanCachePath(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "/"
	}
	cleaned := path.Clean(target)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func cachePathMatches(target, requestPath string) bool {
	target = cleanCachePath(target)
	requestPath = cleanCachePath(requestPath)
	if target == "/" {
		return true
	}
	return requestPath == target || strings.HasPrefix(requestPath, target+"/")
}
