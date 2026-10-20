// Package httpcache shares framework response classifications with middleware.
package httpcache

import (
	"context"
	"mime"
	"net/http"
	"strings"
)

const (
	Immutable = "public, max-age=31536000, immutable"
	Private   = "private, no-store"
)

type policyKey struct{}

// Policy belongs to one response. Request copies retain the same classification.
// Only framework handlers serving session-independent bytes may mark an asset.
type Policy struct {
	immutableAsset bool
	contentType    string
}

func WithPolicy(r *http.Request) (*http.Request, *Policy) {
	policy := &Policy{}
	return r.WithContext(context.WithValue(r.Context(), policyKey{}, policy)), policy
}

// MarkImmutableAsset classifies a resolved, content-addressed framework asset.
// A public cache header or an asset-looking URL alone never grants this class.
// Set the media type first so bodyless conditional responses can be checked.
func MarkImmutableAsset(w http.ResponseWriter, r *http.Request) {
	if policy, ok := r.Context().Value(policyKey{}).(*Policy); ok {
		policy.immutableAsset = true
		policy.contentType = w.Header().Get("Content-Type")
	}
	if disallowsSharedCache(w.Header()) || !assetContentType(w.Header().Get("Content-Type")) {
		w.Header().Set("Cache-Control", Private)
		return
	}
	w.Header().Set("Cache-Control", Immutable)
}

func (p *Policy) IsImmutableAsset() bool {
	return p != nil && p.immutableAsset
}

// AllowsImmutable rechecks headers when they are committed. Cookies, private
// policies, session variance, HTML, data and errors fail closed.
func (p *Policy) AllowsImmutable(headers http.Header, status int) bool {
	if !p.IsImmutableAsset() || disallowsSharedCache(headers) {
		return false
	}
	switch status {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
	default:
		return false
	}
	contentType := headers.Get("Content-Type")
	if contentType == "" && status == http.StatusNotModified {
		// net/http removes representation headers on a 304 response.
		contentType = p.contentType
	}
	return assetContentType(contentType)
}

func disallowsSharedCache(headers http.Header) bool {
	if len(headers.Values("Set-Cookie")) > 0 {
		return true
	}
	for _, value := range headers.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(directive), "=")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "private", "no-store", "no-cache":
				return true
			}
		}
	}
	for _, value := range headers.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(field)) {
			case "", "accept-encoding":
			default:
				return true
			}
		}
	}
	return false
}

func assetContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	switch mediaType {
	case "application/javascript", "text/javascript", "text/css", "application/wasm", "application/octet-stream":
		return true
	}
	for _, prefix := range []string{"image/", "font/", "audio/", "video/", "model/"} {
		if strings.HasPrefix(mediaType, prefix) {
			return true
		}
	}
	return false
}
