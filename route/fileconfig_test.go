package route

import (
	"strings"
	"testing"
	"time"
)

func TestMergeFileRouteCacheConfigOverridesPolicyAndCollapsesEmpty(t *testing.T) {
	base := &FileRouteCacheConfig{
		Public:               boolPointer(true),
		MaxAge:               "30s",
		StaleWhileRevalidate: "2m",
	}
	next := &FileRouteCacheConfig{
		Private:        boolPointer(true),
		Public:         boolPointer(false),
		MaxAge:         "45s",
		StaleIfError:   "10m",
		Immutable:      boolPointer(true),
		MustRevalidate: boolPointer(true),
	}

	merged := mergeFileRouteCacheConfig(base, next)
	if merged == nil {
		t.Fatal("expected merged cache config")
	}
	if merged.Private == nil || !*merged.Private {
		t.Fatalf("expected private override, got %#v", merged.Private)
	}
	if merged.Public == nil || *merged.Public {
		t.Fatalf("expected public to be false after private override, got %#v", merged.Public)
	}
	if merged.MaxAge != "45s" {
		t.Fatalf("expected maxAge override, got %q", merged.MaxAge)
	}
	if merged.StaleWhileRevalidate != "2m" {
		t.Fatalf("expected inherited stale-while-revalidate, got %q", merged.StaleWhileRevalidate)
	}
	if merged.StaleIfError != "10m" {
		t.Fatalf("expected stale-if-error override, got %q", merged.StaleIfError)
	}
	if merged.Immutable == nil || !*merged.Immutable {
		t.Fatalf("expected immutable override, got %#v", merged.Immutable)
	}
	if merged.MustRevalidate == nil || !*merged.MustRevalidate {
		t.Fatalf("expected must-revalidate override, got %#v", merged.MustRevalidate)
	}

	if got := mergeFileRouteCacheConfig(nil, &FileRouteCacheConfig{}); got != nil {
		t.Fatalf("expected empty cache config to collapse to nil, got %#v", got)
	}
}

func TestFileRouteConfigCachePolicyParsesDurationsAndReportsFieldErrors(t *testing.T) {
	cfg := FileRouteConfig{
		Cache: &FileRouteCacheConfig{
			Public:               boolPointer(true),
			MaxAge:               "45s",
			SMaxAge:              "2m",
			StaleWhileRevalidate: "5m",
			StaleIfError:         "10m",
			Immutable:            boolPointer(true),
		},
	}

	policy, ok, err := cfg.CachePolicy()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cache policy to resolve")
	}
	if !policy.Public || policy.Private {
		t.Fatalf("unexpected public/private policy %+v", policy)
	}
	if policy.MaxAge != 45*time.Second || policy.SMaxAge != 2*time.Minute {
		t.Fatalf("unexpected age durations %+v", policy)
	}
	if policy.StaleWhileRevalidate != 5*time.Minute || policy.StaleIfError != 10*time.Minute {
		t.Fatalf("unexpected stale durations %+v", policy)
	}
	if !policy.Immutable {
		t.Fatalf("expected immutable cache policy, got %+v", policy)
	}

	_, ok, err = (FileRouteConfig{
		Cache: &FileRouteCacheConfig{StaleWhileRevalidate: "not-a-duration"},
	}).CachePolicy()
	if !ok {
		t.Fatal("expected invalid cache config to still report attempted policy resolution")
	}
	if err == nil || !strings.Contains(err.Error(), "cache.staleWhileRevalidate") {
		t.Fatalf("expected staleWhileRevalidate parse error, got %v", err)
	}
}

func TestMergeFileRouteConfigTrimsHeadersTagsAndOverridesPrerender(t *testing.T) {
	trueValue := true
	falseValue := false
	base := FileRouteConfig{
		CacheTags: []string{"root"},
		Headers: map[string]string{
			"X-Root": "1",
		},
		Prerender: &trueValue,
	}
	next := FileRouteConfig{
		CacheTags: []string{" docs ", "", "root"},
		Headers: map[string]string{
			" X-Docs ": "2",
			"   ":      "ignored",
		},
		Prerender: &falseValue,
	}

	merged := mergeFileRouteConfig(base, next)
	if len(merged.CacheTags) != 2 || merged.CacheTags[0] != "root" || merged.CacheTags[1] != "docs" {
		t.Fatalf("expected trimmed unique cache tags, got %#v", merged.CacheTags)
	}
	if got := merged.Headers["X-Docs"]; got != "2" {
		t.Fatalf("expected trimmed docs header key, got %#v", merged.Headers)
	}
	if _, ok := merged.Headers["   "]; ok {
		t.Fatalf("expected blank header key to be ignored, got %#v", merged.Headers)
	}
	if merged.Prerender == nil || *merged.Prerender {
		t.Fatalf("expected child prerender override to false, got %#v", merged.Prerender)
	}
}
