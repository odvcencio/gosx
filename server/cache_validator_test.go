package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func cachedRequest(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, nil)
}

func publicCacheState() *CacheState {
	cache := NewCacheState()
	cache.SetPolicy(CachePolicy{Public: true, MaxAge: time.Minute})
	cache.AddTags("docs-pages")
	return cache
}

// TestContentAddressedValidatorSeparatesBodies proves two different bodies
// never share a validator once the caller reports the body.
func TestContentAddressedValidatorSeparatesBodies(t *testing.T) {
	revalidator := NewRevalidator()
	bodies := [][]byte{
		[]byte("<p>first</p>"),
		[]byte("<p>second</p>"),
		[]byte(""),
		[]byte("<p>first</p> "),
	}
	seen := map[string]string{}
	for _, body := range bodies {
		headers := http.Header{}
		ApplyCacheHeadersForBody(cachedRequest("/cached"), headers, http.StatusOK, publicCacheState(), revalidator, body)
		etag := headers.Get("ETag")
		if etag == "" {
			t.Fatalf("body %q produced no validator", body)
		}
		if previous, ok := seen[etag]; ok {
			t.Fatalf("bodies %q and %q share validator %s", previous, body, etag)
		}
		seen[etag] = string(body)
	}
}

// TestContentAddressedValidatorIsStableForOneBody proves the validator repeats
// for the same body, so a conditional request still wins a 304.
func TestContentAddressedValidatorIsStableForOneBody(t *testing.T) {
	revalidator := NewRevalidator()
	body := []byte("<p>stable</p>")

	first := http.Header{}
	ApplyCacheHeadersForBody(cachedRequest("/cached"), first, http.StatusOK, publicCacheState(), revalidator, body)
	etag := first.Get("ETag")
	if etag == "" {
		t.Fatal("expected a validator for the first render")
	}

	second := http.Header{}
	conditional := cachedRequest("/cached")
	conditional.Header.Set("If-None-Match", etag)
	if !ApplyCacheHeadersForBody(conditional, second, http.StatusOK, publicCacheState(), revalidator, body) {
		t.Fatal("expected a 304 for an unchanged body")
	}
}

// TestContentAddressedValidatorRejectsStaleConditional proves a changed body
// beats a stale validator. Before the fix the validator ignored the body, so
// the same request produced a 304 with stale content.
func TestContentAddressedValidatorRejectsStaleConditional(t *testing.T) {
	revalidator := NewRevalidator()

	first := http.Header{}
	ApplyCacheHeadersForBody(cachedRequest("/cached"), first, http.StatusOK, publicCacheState(), revalidator, []byte("<p>render one</p>"))
	stale := first.Get("ETag")
	if stale == "" {
		t.Fatal("expected a validator for the first render")
	}

	second := http.Header{}
	conditional := cachedRequest("/cached")
	conditional.Header.Set("If-None-Match", stale)
	if ApplyCacheHeadersForBody(conditional, second, http.StatusOK, publicCacheState(), revalidator, []byte("<p>render two</p>")) {
		t.Fatal("stale validator won a 304 for a changed body")
	}
	if second.Get("ETag") == stale {
		t.Fatal("changed body kept the stale validator")
	}
}

// TestContentAddressedValidatorRotatesOnRevalidateTag proves an explicit
// revalidation still rotates the validator for an unchanged body.
func TestContentAddressedValidatorRotatesOnRevalidateTag(t *testing.T) {
	revalidator := NewRevalidator()
	body := []byte("<p>same</p>")

	first := http.Header{}
	ApplyCacheHeadersForBody(cachedRequest("/cached"), first, http.StatusOK, publicCacheState(), revalidator, body)
	etag := first.Get("ETag")

	revalidator.RevalidateTag("docs-pages")

	second := http.Header{}
	conditional := cachedRequest("/cached")
	conditional.Header.Set("If-None-Match", etag)
	if ApplyCacheHeadersForBody(conditional, second, http.StatusOK, publicCacheState(), revalidator, body) {
		t.Fatal("expected a 200 after RevalidateTag")
	}
	if next := second.Get("ETag"); next == "" || next == etag {
		t.Fatalf("validator after RevalidateTag = %q, want a new value", next)
	}
}

// TestRequestDerivedValidatorIgnoresBody documents the known limit of the
// bodyless path. Two renders of one request share one validator, so a caller
// that cannot report the body must accept a stale 304 until it revalidates.
func TestRequestDerivedValidatorIgnoresBody(t *testing.T) {
	revalidator := NewRevalidator()

	first := http.Header{}
	ApplyCacheHeaders(cachedRequest("/cached"), first, http.StatusOK, publicCacheState(), revalidator)
	second := http.Header{}
	ApplyCacheHeaders(cachedRequest("/cached"), second, http.StatusOK, publicCacheState(), revalidator)

	if first.Get("ETag") != second.Get("ETag") {
		t.Fatal("expected the bodyless validator to depend on the request only")
	}
	if state := publicCacheState(); state.ContentAddressed() {
		t.Fatal("a bodyless cache state must not report a content-addressed validator")
	}
}

// TestNoStoreAndNoCacheSkipAutomaticValidator proves a response that asks for a
// fresh body carries no automatic validator.
func TestNoStoreAndNoCacheSkipAutomaticValidator(t *testing.T) {
	for name, policy := range map[string]CachePolicy{
		"no-store": {NoStore: true},
		"no-cache": {NoCache: true},
	} {
		t.Run(name, func(t *testing.T) {
			cache := NewCacheState()
			cache.SetPolicy(policy)
			cache.AddTags("docs-pages")
			headers := http.Header{}
			if ApplyCacheHeaders(cachedRequest("/live"), headers, http.StatusOK, cache, NewRevalidator()) {
				t.Fatal("expected no 304 for a no-store or no-cache response")
			}
			if etag := headers.Get("ETag"); etag != "" {
				t.Fatalf("ETag = %q, want none", etag)
			}
		})
	}
}

// TestExplicitETagStaysAuthoritative proves an application ETag still wins.
func TestExplicitETagStaysAuthoritative(t *testing.T) {
	cache := publicCacheState()
	cache.SetETag("build-42")
	headers := http.Header{}
	ApplyCacheHeadersForBody(cachedRequest("/cached"), headers, http.StatusOK, cache, NewRevalidator(), []byte("<p>body</p>"))
	if got := headers.Get("ETag"); got != `"build-42"` {
		t.Fatalf("ETag = %q, want the explicit value", got)
	}
	if !cache.ContentAddressed() {
		t.Fatal("an explicit ETag must count as content addressed")
	}
}
