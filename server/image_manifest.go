package server

import (
	"strings"
	"sync"

	"m31labs.dev/gosx/buildmanifest"
)

// gosxAssetBaseURL is the public URL prefix serveRuntimeAsset (registered at
// "GET /gosx/") answers dist/assets from — see runtimeManifestDirectAssetPath,
// which strips this "gosx/" mount and resolves the remainder under
// <root>/assets or <root>/dist/assets regardless of bucket ("islands",
// "css", "runtime", or "images"). staticExportImageVariant already hardcoded
// this same string; this constant gives image_manifest.go's own lookup the
// identical base without a second, potentially divergent literal.
const gosxAssetBaseURL = "/gosx/assets"

// imageManifestLookupMu and imageManifestLookup mirror the "local"
// ImageResolver registry in image_resolver.go: process-global, single-App-
// per-process state that the most recently Build()-ed App owns (see
// registerStaticExportImageResolver's doc comment for the accepted
// convention this follows). A file-routed <Image> tag renders through
// route.RenderProgramComponent, which has no *App of its own to read a
// build manifest from -- server cannot import route (route already imports
// server), so a registered function pointer, not a passed-in reference, is
// the seam.
var (
	imageManifestLookupMu sync.RWMutex
	imageManifestLookup   func(src string) (buildmanifest.ImageAsset, bool)
)

// LookupImageManifestAsset returns the build-time responsive/format variants
// gosx build's imagepipe stage recorded for src (already normalized through
// AssetURL) in the buildmanifest.Manifest.Images bucket, if any App
// registered a manifest source by calling Build().
//
// It returns ok=false, always, in every one of these cases: no App has
// called Build() yet in this process; that App has no build.json at its
// resolved runtime root (an ordinary `gosx dev` run with no prior `gosx
// build`); the loaded manifest predates issue #200 (Images is nil); or src
// has no recorded ImageAsset. route's file-router <Image> renderer treats
// every one of those, uniformly, as "fall back to the #199 runtime
// optimizer/passthrough path" -- never a render failure.
func LookupImageManifestAsset(src string) (buildmanifest.ImageAsset, bool) {
	imageManifestLookupMu.RLock()
	lookup := imageManifestLookup
	imageManifestLookupMu.RUnlock()
	if lookup == nil {
		return buildmanifest.ImageAsset{}, false
	}
	return lookup(src)
}

// ImageManifestVariantURL returns the public URL for a build-time image
// variant, through the same "/gosx/assets" base every other manifest-hashed
// asset (runtime, islands, CSS) already resolves through.
func ImageManifestVariantURL(variant buildmanifest.ImageVariantAsset) string {
	return buildmanifest.AssetURL(gosxAssetBaseURL, "images", variant.File)
}

// registerImageManifestLookup installs a's own manifest lookup as the
// process-global one, exactly mirroring registerStaticExportImageResolver's
// "last App built wins" convention. Called unconditionally from
// registerBuiltinRoutes on every Build(), not gated on GOSX_STATIC_EXPORT:
// unlike the URL-rewriting resolver (whose whole job is answering the
// static-export subprocess), a manifest-backed <picture> is a real,
// permanent perf win for a normally served app too -- reading pre-built
// hashed variants instead of paying a CPU-bound resize on every request the
// runtime optimizer would otherwise serve.
func registerImageManifestLookup(a *App) {
	imageManifestLookupMu.Lock()
	imageManifestLookup = a.lookupImageManifestAsset
	imageManifestLookupMu.Unlock()
}

// lookupImageManifestAsset reads only the buildmanifest.Manifest.Images
// bucket this App already loaded (via runtimeBuildManifest) for every other
// hashed asset it serves. It never touches package imagepipe or its WebP
// encoder -- both are cmd/gosx-only (and, as of gosx#201, strictcheck's own
// check-time stage); see TestServerPackageTreeNeverImportsImagepipe.
func (a *App) lookupImageManifestAsset(src string) (buildmanifest.ImageAsset, bool) {
	if a == nil || strings.TrimSpace(src) == "" {
		return buildmanifest.ImageAsset{}, false
	}
	root := a.effectiveRuntimeRoot()
	if root == "" {
		return buildmanifest.ImageAsset{}, false
	}
	manifest, ok := a.runtimeBuildManifest(root)
	if !ok || manifest == nil {
		return buildmanifest.ImageAsset{}, false
	}
	return manifest.ImageAssetBySource(src)
}
