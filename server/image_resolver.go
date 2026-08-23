package server

import (
	"fmt"
	"net/url"
	"os"
	"path"
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

// MustRegisterImageResolver registers a named image URL resolver.
//
// Deprecated: use RegisterImageResolver and check the returned error. This
// method no longer panics so resolver configuration cannot crash requests.
func MustRegisterImageResolver(name string, resolver ImageResolver) error {
	return RegisterImageResolver(name, resolver)
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
	if strings.TrimSpace(os.Getenv("GOSX_STATIC_EXPORT")) != "" {
		return src, true
	}
	if !shouldOptimizeImageSource(src) || transform == (ImageTransform{}) {
		return src, true
	}
	return resolveImageEndpointURL(defaultImageEndpoint, src, transform), true
}

func resolveImageEndpointURL(endpoint, src string, transform ImageTransform) string {
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
	return endpoint + "?" + values.Encode()
}

// NewImageEndpointResolver maps one public URL prefix onto a dedicated local
// optimizer endpoint. It lets CMS uploads or other non-public/ directories use
// Image without exposing arbitrary filesystem paths or teaching the default
// public-asset handler about application storage.
//
// The returned resolver accepts only sources beneath sourcePrefix and strips
// that prefix before sending the source to an ImageHandler rooted at the
// corresponding storage directory.
func NewImageEndpointResolver(endpoint, sourcePrefix string) (ImageResolver, error) {
	endpoint = strings.TrimSpace(endpoint)
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.IsAbs() || parsedEndpoint.Host != "" || parsedEndpoint.RawQuery != "" || parsedEndpoint.Fragment != "" || !strings.HasPrefix(parsedEndpoint.Path, "/") {
		return nil, fmt.Errorf("image endpoint must be a root-relative path")
	}
	endpoint = path.Clean(parsedEndpoint.Path)
	if endpoint == "." || endpoint == "/" {
		return nil, fmt.Errorf("image endpoint must name a route below /")
	}

	sourcePrefix = path.Clean("/" + strings.TrimSpace(sourcePrefix))
	if sourcePrefix == "/" || sourcePrefix == "." {
		return nil, fmt.Errorf("image source prefix must name a path below /")
	}

	return ImageResolverFunc(func(src string, transform ImageTransform) (string, bool) {
		parsedSource, err := url.Parse(strings.TrimSpace(src))
		if err != nil || parsedSource.IsAbs() || parsedSource.Host != "" {
			return "", false
		}
		cleanSource := path.Clean("/" + strings.TrimSpace(parsedSource.Path))
		prefixWithSlash := strings.TrimRight(sourcePrefix, "/") + "/"
		if !strings.HasPrefix(cleanSource, prefixWithSlash) {
			return "", false
		}
		relative := "/" + strings.TrimPrefix(cleanSource, prefixWithSlash)
		return resolveImageEndpointURL(endpoint, relative, transform), true
	}), nil
}

// registerStaticExportImageResolver overrides the "local" image resolver
// with one bound to a, so a GOSX_STATIC_EXPORT=1 run of this App's own
// binary (the subprocess gosx build/export starts to prerender static
// pages) can answer real, build-time-generated image variant URLs instead
// of resolveLocalImageURL's passthrough (issue #200).
//
// This is process-global state -- imageResolvers already was, for every
// named resolver -- so it assumes the same single-App-per-process
// convention the rest of this file's package-level state (for example
// imageTransformSlots) already assumes. Build() calls this once per App it
// builds; the last App built in a process wins, exactly as a second
// RegisterImageResolver("local", ...) call already would.
func registerStaticExportImageResolver(a *App) {
	_ = RegisterImageResolver("local", ImageResolverFunc(a.resolveLocalImageURL))
}

// resolveLocalImageURL resolves an <img> URL for a. During a
// GOSX_STATIC_EXPORT=1 run it first tries staticExportImageVariant; a match
// there is a real, build-time-generated variant URL, not the plain
// passthrough src the package-level resolveLocalImageURL always returns in
// export mode. Every other case -- static export with no matching variant,
// and ordinary (non-export) resolution -- falls back to the unmodified
// package-level resolveLocalImageURL, so this method changes nothing about
// existing behavior beyond adding that one new match.
func (a *App) resolveLocalImageURL(src string, transform ImageTransform) (string, bool) {
	if strings.TrimSpace(os.Getenv("GOSX_STATIC_EXPORT")) != "" {
		if resolved, ok := a.staticExportImageVariant(src, transform); ok {
			return resolved, true
		}
	}
	return resolveLocalImageURL(src, transform)
}

// staticExportImageVariant looks up the build-time image variant gosx
// build's imagepipe stage already generated for src at transform.Width,
// reading only the buildmanifest.Manifest.Images bucket this App already
// loaded (via runtimeBuildManifest) for every other hashed asset it serves.
// It never touches package imagepipe -- build-time only -- or any encoder
// a project may have registered with it; see
// TestServerPackageTreeNeverImportsImagepipe at the repo root for the
// enforced isolation proof.
func (a *App) staticExportImageVariant(src string, transform ImageTransform) (string, bool) {
	if a == nil {
		return "", false
	}
	root := a.effectiveRuntimeRoot()
	if root == "" {
		return "", false
	}
	manifest, ok := a.runtimeBuildManifest(root)
	if !ok || manifest == nil {
		return "", false
	}
	asset, ok := manifest.ImageVariant(src, transform.Width, transform.Format)
	if !ok || strings.TrimSpace(asset.File) == "" {
		return "", false
	}
	return manifest.ImageURL("/gosx/assets", asset), true
}
