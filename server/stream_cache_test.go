package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func TestStreamExplicitValidatorsAndHEADDoNotLaunchWork(t *testing.T) {
	modified := time.Unix(100, 0).UTC()
	for _, tc := range []struct {
		name, method, etag, conditional, date string
		wantStatus                            int
		wantCalls                             int32
	}{
		{"matching-version", "GET", "v1", `"v1"`, "", 304, 0},
		{"changed-version", "GET", "v2", `"v1"`, "", 200, 1},
		{"etag-over-date", "GET", "v2", `"v1"`, modified.Format(http.TimeFormat), 200, 1},
		{"matching-date", "GET", "", "", modified.Format(http.TimeFormat), 304, 0},
		{"head", "HEAD", "", "", "", 200, 0},
		{"conditional-head", "HEAD", "v1", `"v1"`, "", 304, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewDeferredRegistry()
			var calls atomic.Int32
			node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { calls.Add(1); return gosx.Text("result"), nil })
			cache := NewCacheState()
			cache.SetPolicy(PrivateCache(time.Minute))
			cache.SetETag(tc.etag)
			if tc.date != "" {
				cache.SetLastModified(modified)
			}
			req := httptest.NewRequest(tc.method, "/stream", nil)
			req.Header.Set("If-None-Match", tc.conditional)
			req.Header.Set("If-Modified-Since", tc.date)
			w := httptest.NewRecorder()
			WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Request: req, Cache: cache, Headers: make(http.Header)})
			if w.Code != tc.wantStatus || calls.Load() != tc.wantCalls || (tc.wantCalls == 0 && w.Body.Len() != 0) {
				t.Fatalf("status=%d calls=%d body=%q", w.Code, calls.Load(), w.Body.String())
			}
		})
	}
}

func TestStreamUnknownRepresentationDoesNotReuseShellDigest(t *testing.T) {
	cache := NewCacheState()
	cache.SetPolicy(PublicCache(time.Minute))
	cache.SetRepresentation([]byte("shell"))
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	header := make(http.Header)
	ApplyCacheHeaders(req, header, 200, cache, nil)
	old := header.Get("ETag")
	req.Header.Set("If-None-Match", old)
	if ApplyCacheHeadersForBody(req, header, 200, cache, nil, nil) || header.Get("ETag") != "" {
		t.Fatal("unknown stream reused shell digest")
	}
	if !cache.HasRepresentation() {
		t.Fatal("unknown representation mutated caller state")
	}
	req.Header.Del("If-None-Match")
	ApplyCacheHeadersForBody(req, header, 200, cache, nil, []byte{})
	if tag := header.Get("ETag"); tag == "" || tag == old {
		t.Fatal("known empty body did not receive its own validator")
	}
}

func TestStreamSharedFragmentsStripCapturedNonce(t *testing.T) {
	r := NewDeferredRegistry()
	node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
		return gosx.RawHTML(`<script nonce="private-nonce">/* synthetic */</script>`), nil
	})
	cache := NewCacheState()
	cache.SetPolicy(PublicCache(time.Minute))
	w := httptest.NewRecorder()
	WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Cache: cache, Headers: make(http.Header), Request: httptest.NewRequest(http.MethodGet, "/stream", nil), Nonce: "private-nonce"})
	if strings.Contains(w.Body.String(), "private-nonce") || strings.Contains(w.Header().Get("Content-Security-Policy"), "nonce-") {
		t.Fatalf("shared stream retained per-request nonce: headers=%v body=%q", w.Header(), w.Body.String())
	}
}
