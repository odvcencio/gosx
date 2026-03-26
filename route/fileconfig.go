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
	if next.Public != nil {
		merged.Public = cloneBoolPointer(next.Public)
		if *next.Public {
			merged.Private = boolPointer(false)
		}
	}
	if next.Private != nil {
		merged.Private = cloneBoolPointer(next.Private)
		if *next.Private {
			merged.Public = boolPointer(false)
		}
	}
	if next.NoStore != nil {
		merged.NoStore = cloneBoolPointer(next.NoStore)
	}
	if next.NoCache != nil {
		merged.NoCache = cloneBoolPointer(next.NoCache)
	}
	if next.MaxAge != "" {
		merged.MaxAge = next.MaxAge
	}
	if next.SMaxAge != "" {
		merged.SMaxAge = next.SMaxAge
	}
	if next.StaleWhileRevalidate != "" {
		merged.StaleWhileRevalidate = next.StaleWhileRevalidate
	}
	if next.StaleIfError != "" {
		merged.StaleIfError = next.StaleIfError
	}
	if next.MustRevalidate != nil {
		merged.MustRevalidate = cloneBoolPointer(next.MustRevalidate)
	}
	if next.Immutable != nil {
		merged.Immutable = cloneBoolPointer(next.Immutable)
	}
	if !merged.hasValues() {
		return nil
	}
	return merged
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
	policy := server.CachePolicy{}
	if c.Cache.Public != nil {
		policy.Public = *c.Cache.Public
	}
	if c.Cache.Private != nil {
		policy.Private = *c.Cache.Private
	}
	if c.Cache.NoStore != nil {
		policy.NoStore = *c.Cache.NoStore
	}
	if c.Cache.NoCache != nil {
		policy.NoCache = *c.Cache.NoCache
	}
	if c.Cache.MustRevalidate != nil {
		policy.MustRevalidate = *c.Cache.MustRevalidate
	}
	if c.Cache.Immutable != nil {
		policy.Immutable = *c.Cache.Immutable
	}

	var err error
	if policy.MaxAge, err = parseFileRouteDuration(c.Cache.MaxAge); err != nil {
		return server.CachePolicy{}, true, fmt.Errorf("cache.maxAge: %w", err)
	}
	if policy.SMaxAge, err = parseFileRouteDuration(c.Cache.SMaxAge); err != nil {
		return server.CachePolicy{}, true, fmt.Errorf("cache.sMaxAge: %w", err)
	}
	if policy.StaleWhileRevalidate, err = parseFileRouteDuration(c.Cache.StaleWhileRevalidate); err != nil {
		return server.CachePolicy{}, true, fmt.Errorf("cache.staleWhileRevalidate: %w", err)
	}
	if policy.StaleIfError, err = parseFileRouteDuration(c.Cache.StaleIfError); err != nil {
		return server.CachePolicy{}, true, fmt.Errorf("cache.staleIfError: %w", err)
	}
	return policy, true, nil
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
