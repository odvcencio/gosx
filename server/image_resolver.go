package server

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// ImageResolver rewrites image URLs for a named backend.
type ImageResolver interface {
	Resolve(src string, transform ImageTransform) (string, bool)
}

// ImageResolverFunc adapts a function into an image resolver.
type ImageResolverFunc func(src string, transform ImageTransform) (string, bool)

// Resolve rewrites an image URL.
func (fn ImageResolverFunc) Resolve(src string, transform ImageTransform) (string, bool) {
	if fn == nil {
		return "", false
	}
	return fn(src, transform)
}

var (
	imageResolverMu sync.RWMutex
	imageResolvers  = map[string]ImageResolver{
		"local": ImageResolverFunc(resolveLocalImageURL),
	}
)

// RegisterImageResolver registers a named image URL resolver.
func RegisterImageResolver(name string, resolver ImageResolver) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("image resolver name is required")
	}
	if resolver == nil {
		return fmt.Errorf("image resolver %q is nil", name)
	}
	imageResolverMu.Lock()
	imageResolvers[name] = resolver
	imageResolverMu.Unlock()
	return nil
}

// MustRegisterImageResolver registers a named image URL resolver or panics.
func MustRegisterImageResolver(name string, resolver ImageResolver) {
	if err := RegisterImageResolver(name, resolver); err != nil {
		panic(err)
	}
}

// ImageURLWithResolver resolves an optimized image URL through a named backend.
func ImageURLWithResolver(name, src string, transform ImageTransform) string {
	src = AssetURL(src)
	resolver := imageResolverNamed(name)
	if resolver == nil {
		return src
	}
	if resolved, ok := resolver.Resolve(src, transform); ok && strings.TrimSpace(resolved) != "" {
		return resolved
	}
	return src
}

func imageResolverNamed(name string) ImageResolver {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "local"
	}
	imageResolverMu.RLock()
	resolver := imageResolvers[name]
	imageResolverMu.RUnlock()
	return resolver
}

func resolveLocalImageURL(src string, transform ImageTransform) (string, bool) {
	if !shouldOptimizeImageSource(src) || transform == (ImageTransform{}) {
		return src, true
	}
	values := url.Values{}
	values.Set("src", src)
	if transform.Width > 0 {
		values.Set("w", strconv.Itoa(transform.Width))
	}
	if transform.Height > 0 {
		values.Set("h", strconv.Itoa(transform.Height))
	}
	if transform.Quality > 0 {
		values.Set("q", strconv.Itoa(transform.Quality))
	}
	if format := normalizeImageFormat(transform.Format); format != "" {
		values.Set("fmt", format)
	}
	return defaultImageEndpoint + "?" + values.Encode(), true
}
