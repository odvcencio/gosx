package route

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/gosx/server"
)

// FileRouteConfig describes inheritable directory-scoped configuration loaded
// from `route.config.json`.
type FileRouteConfig struct {
	Cache     *FileRouteCacheConfig `json:"cache,omitempty"`
	CacheTags []string              `json:"cacheTags,omitempty"`
	Headers   map[string]string     `json:"headers,omitempty"`
	Prerender *bool                 `json:"prerender,omitempty"`
}

// PrerenderEnabled reports whether routes in this scope should be exported
// when the caller provides a default policy.
func (c FileRouteConfig) PrerenderEnabled(defaultValue bool) bool {
	if c.Prerender != nil {
		return *c.Prerender
	}
	return defaultValue
}

// FileRouteCacheConfig maps `route.config.json` cache directives onto
// `server.CachePolicy`.
type FileRouteCacheConfig struct {
	Public               *bool  `json:"public,omitempty"`
	Private              *bool  `json:"private,omitempty"`
	NoStore              *bool  `json:"noStore,omitempty"`
	NoCache              *bool  `json:"noCache,omitempty"`
	MaxAge               string `json:"maxAge,omitempty"`
	SMaxAge              string `json:"sMaxAge,omitempty"`
	StaleWhileRevalidate string `json:"staleWhileRevalidate,omitempty"`
	StaleIfError         string `json:"staleIfError,omitempty"`
	MustRevalidate       *bool  `json:"mustRevalidate,omitempty"`
	Immutable            *bool  `json:"immutable,omitempty"`
}

func readFileRouteConfig(path string) (FileRouteConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FileRouteConfig{}, err
	}

	var config FileRouteConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return FileRouteConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return config, nil
}

func collectFileRouteConfig(dir string, dirs map[string]*fileRouteDir) FileRouteConfig {
	var merged FileRouteConfig
	for _, current := range parentFileRouteDirs(dir) {
		entry := dirs[current]
		if entry == nil || !entry.HasConfig {
			continue
		}
		merged = mergeFileRouteConfig(merged, entry.Config)
	}
	return merged
}

func applyFileRouteConfig(ctx *RouteContext, config FileRouteConfig) error {
	if ctx == nil {
		return nil
	}
	policy, ok, err := config.cachePolicy()
	if err != nil {
		return err
	}
	if ok {
		ctx.Cache(policy)
	}
	for _, tag := range config.CacheTags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		ctx.CacheTag(tag)
	}
	if len(config.Headers) > 0 {
		headers := ctx.Header()
		for key, value := range config.Headers {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			headers.Set(key, value)
		}
	}
	return nil
}

func routeConfigPath(dir string) string {
	return filepath.Join(dir, "route.config.json")
}

func mergeFileRouteConfig(base, next FileRouteConfig) FileRouteConfig {
	merged := cloneFileRouteConfig(base)
	merged.Cache = mergeFileRouteCacheConfig(merged.Cache, next.Cache)
	merged.CacheTags = appendUniqueStrings(merged.CacheTags, next.CacheTags...)
	if len(next.Headers) > 0 {
		if merged.Headers == nil {
			merged.Headers = make(map[string]string, len(next.Headers))
		}
		for key, value := range next.Headers {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			merged.Headers[key] = value
		}
	}
	if next.Prerender != nil {
		merged.Prerender = cloneBoolPointer(next.Prerender)
	}
	return merged
}

func cloneFileRouteConfig(src FileRouteConfig) FileRouteConfig {
	dst := FileRouteConfig{
		Cache:     cloneFileRouteCacheConfig(src.Cache),
		CacheTags: append([]string(nil), src.CacheTags...),
		Prerender: cloneBoolPointer(src.Prerender),
	}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string]string, len(src.Headers))
		for key, value := range src.Headers {
			dst.Headers[key] = value
		}
	}
	return dst
}

func mergeFileRouteCacheConfig(base, next *FileRouteCacheConfig) *FileRouteCacheConfig {
	if base == nil && next == nil {
		return nil
	}
	merged := cloneFileRouteCacheConfig(base)
	if merged == nil {
		merged = &FileRouteCacheConfig{}
	}
	if next == nil {
		return merged
	}
	overrideExclusiveCacheBool(&merged.Public, &merged.Private, next.Public)
	overrideExclusiveCacheBool(&merged.Private, &merged.Public, next.Private)
	overrideCacheBool(&merged.NoStore, next.NoStore)
	overrideCacheBool(&merged.NoCache, next.NoCache)
	overrideCacheString(&merged.MaxAge, next.MaxAge)
	overrideCacheString(&merged.SMaxAge, next.SMaxAge)
	overrideCacheString(&merged.StaleWhileRevalidate, next.StaleWhileRevalidate)
	overrideCacheString(&merged.StaleIfError, next.StaleIfError)
	overrideCacheBool(&merged.MustRevalidate, next.MustRevalidate)
	overrideCacheBool(&merged.Immutable, next.Immutable)
	if !merged.hasValues() {
		return nil
	}
	return merged
}

func overrideExclusiveCacheBool(dst, opposite **bool, next *bool) {
	if next == nil {
		return
	}
	*dst = cloneBoolPointer(next)
	if *next {
		*opposite = boolPointer(false)
	}
}

func overrideCacheBool(dst **bool, next *bool) {
	if next != nil {
		*dst = cloneBoolPointer(next)
	}
}

func overrideCacheString(dst *string, next string) {
	if next != "" {
		*dst = next
	}
}

func cloneFileRouteCacheConfig(src *FileRouteCacheConfig) *FileRouteCacheConfig {
	if src == nil {
		return nil
	}
	return &FileRouteCacheConfig{
		Public:               cloneBoolPointer(src.Public),
		Private:              cloneBoolPointer(src.Private),
		NoStore:              cloneBoolPointer(src.NoStore),
		NoCache:              cloneBoolPointer(src.NoCache),
		MaxAge:               src.MaxAge,
		SMaxAge:              src.SMaxAge,
		StaleWhileRevalidate: src.StaleWhileRevalidate,
		StaleIfError:         src.StaleIfError,
		MustRevalidate:       cloneBoolPointer(src.MustRevalidate),
		Immutable:            cloneBoolPointer(src.Immutable),
	}
}

func (c FileRouteConfig) cachePolicy() (server.CachePolicy, bool, error) {
	if c.Cache == nil || !c.Cache.hasValues() {
		return server.CachePolicy{}, false, nil
	}
	policy := cachePolicyFromFileRouteCacheConfig(c.Cache)
	if err := applyFileRouteDuration(&policy.MaxAge, c.Cache.MaxAge, "cache.maxAge"); err != nil {
		return server.CachePolicy{}, true, err
	}
	if err := applyFileRouteDuration(&policy.SMaxAge, c.Cache.SMaxAge, "cache.sMaxAge"); err != nil {
		return server.CachePolicy{}, true, err
	}
	if err := applyFileRouteDuration(&policy.StaleWhileRevalidate, c.Cache.StaleWhileRevalidate, "cache.staleWhileRevalidate"); err != nil {
		return server.CachePolicy{}, true, err
	}
	if err := applyFileRouteDuration(&policy.StaleIfError, c.Cache.StaleIfError, "cache.staleIfError"); err != nil {
		return server.CachePolicy{}, true, err
	}
	return policy, true, nil
}

func cachePolicyFromFileRouteCacheConfig(cache *FileRouteCacheConfig) server.CachePolicy {
	if cache == nil {
		return server.CachePolicy{}
	}
	policy := server.CachePolicy{}
	if cache.Public != nil {
		policy.Public = *cache.Public
	}
	if cache.Private != nil {
		policy.Private = *cache.Private
	}
	if cache.NoStore != nil {
		policy.NoStore = *cache.NoStore
	}
	if cache.NoCache != nil {
		policy.NoCache = *cache.NoCache
	}
	if cache.MustRevalidate != nil {
		policy.MustRevalidate = *cache.MustRevalidate
	}
	if cache.Immutable != nil {
		policy.Immutable = *cache.Immutable
	}
	return policy
}

func applyFileRouteDuration(dst *time.Duration, value, field string) error {
	parsed, err := parseFileRouteDuration(value)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	*dst = parsed
	return nil
}

// CachePolicy exposes the resolved cache policy for a file-route config.
func (c FileRouteConfig) CachePolicy() (server.CachePolicy, bool, error) {
	return c.cachePolicy()
}

func (c *FileRouteCacheConfig) hasValues() bool {
	if c == nil {
		return false
	}
	return c.Public != nil ||
		c.Private != nil ||
		c.NoStore != nil ||
		c.NoCache != nil ||
		c.MaxAge != "" ||
		c.SMaxAge != "" ||
		c.StaleWhileRevalidate != "" ||
		c.StaleIfError != "" ||
		c.MustRevalidate != nil ||
		c.Immutable != nil
}

func parseFileRouteDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func cloneBoolPointer(src *bool) *bool {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}
