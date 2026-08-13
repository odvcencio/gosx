package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"m31labs.dev/gosx/buildmanifest"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	demospages "m31labs.dev/gosx/examples/gosx-docs/app/demos"
	"m31labs.dev/gosx/server"
)

const siteAPIVersion = "1"

// isrBypassHeader is an internal dispatch signal understood by the GoSX ISR
// layer. Public requests must never be allowed to supply it themselves: doing
// so would turn every cached route into an attacker-controlled render bypass.
const isrBypassHeader = "X-GoSX-ISR-Revalidate"

type siteBuildInfo struct {
	Site             string `json:"site"`
	Status           string `json:"status"`
	APIVersion       string `json:"apiVersion"`
	Framework        string `json:"framework"`
	FrameworkVersion string `json:"frameworkVersion"`
	Revision         string `json:"revision"`
	BuiltAt          string `json:"builtAt,omitempty"`
	Runtime          string `json:"runtime"`
	PublicURL        string `json:"publicURL"`
}

type siteActionProbe struct {
	OK       bool   `json:"ok"`
	Revision string `json:"revision"`
}

func currentSiteBuildInfo() siteBuildInfo {
	build := docsapp.SiteBuildInfo()
	return siteBuildInfo{
		Site:             "gosx-docs",
		Status:           "ok",
		APIVersion:       siteAPIVersion,
		Framework:        "GoSX",
		FrameworkVersion: build["frameworkVersion"],
		Revision:         build["revision"],
		BuiltAt:          build["builtAt"],
		Runtime:          runtime.Version(),
		PublicURL:        build["publicURL"],
	}
}

func mountSiteDocuments(app *server.App, root string) {
	app.API("GET /api/site", func(ctx *server.Context) (any, error) {
		// Deployment identity must never be replayed from the previous rollout.
		ctx.NoStore()
		return currentSiteBuildInfo(), nil
	})
	app.API("POST /api/site/probe", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return siteActionProbe{OK: true, Revision: currentSiteBuildInfo().Revision}, nil
	})
	app.Mount("GET /sitemap.xml", server.SitemapHandler(func(*http.Request) (string, error) {
		return buildSitemapXML(publicSiteRoutes()), nil
	}))
	app.Mount("GET /robots.txt", server.RobotsHandler(func(*http.Request) (string, error) {
		return "User-agent: *\nAllow: /\nSitemap: " + docsapp.PublicSiteURL("/sitemap.xml") + "\n", nil
	}))
}

func docsSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the wire form before any internal middleware can opt a trusted
		// route out of ISR. canonicalDocsIndex runs immediately after this layer
		// and adds the marker back only for the query-backed /docs index.
		r.Header.Del(isrBypassHeader)
		w.Header().Set("Content-Security-Policy", "base-uri 'self'; object-src 'none'; frame-ancestors 'self'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}

// canonicalDocsIndex keeps the query-backed documentation index dynamic even
// in a staged bundle. ISR intentionally canonicalizes exported pages to a
// trailing slash, but a cached /docs artifact cannot represent search queries.
// Bypassing ISR for this one route lets the file router render the query while
// every ordinary guide continues to use its immutable prerendered artifact.
func canonicalDocsIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/docs/":
			target := "/docs"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		case "/docs":
			r.Header.Set(isrBypassHeader, "docs-index-query")
		}
		next.ServeHTTP(w, r)
	})
}

func configureProductionReadiness(app *server.App) {
	if strings.TrimSpace(os.Getenv("GOSX_DEV")) != "" {
		return
	}
	root := strings.TrimSpace(os.Getenv("GOSX_APP_ROOT"))
	if root == "" {
		return
	}
	var once sync.Once
	var readyErr error
	app.UseReadyCheck("build-assets", server.ReadyCheckFunc(func(context.Context) error {
		once.Do(func() {
			// The production root is an immutable image layer. Validate it once
			// instead of re-hashing every runtime artifact on each five-second
			// Kubernetes readiness probe.
			readyErr = verifyBuildAssets(root)
		})
		return readyErr
	}))
}

func verifyBuildAssets(root string) error {
	manifest, err := buildmanifest.Load(filepath.Join(root, "build.json"))
	if err != nil {
		return err
	}
	for _, asset := range buildAssetFiles(manifest) {
		cleanFile := filepath.Clean(asset.file)
		if cleanFile == "." || filepath.IsAbs(cleanFile) || cleanFile == ".." || strings.HasPrefix(cleanFile, ".."+string(filepath.Separator)) {
			return fmt.Errorf("build manifest contains unsafe %s asset path %q", asset.bucket, asset.file)
		}
		assetPath := filepath.Join(root, "assets", asset.bucket, cleanFile)
		info, statErr := os.Stat(assetPath)
		if statErr != nil {
			return fmt.Errorf("build asset %s/%s: %w", asset.bucket, asset.file, statErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("build asset %s/%s is not a regular file", asset.bucket, asset.file)
		}
		if asset.size > 0 && info.Size() != asset.size {
			return fmt.Errorf("build asset %s/%s size %d does not match manifest %d", asset.bucket, asset.file, info.Size(), asset.size)
		}
		if asset.hash != "" {
			data, readErr := os.ReadFile(assetPath)
			if readErr != nil {
				return fmt.Errorf("read build asset %s/%s: %w", asset.bucket, asset.file, readErr)
			}
			digest := sha256.Sum256(data)
			actual := hex.EncodeToString(digest[:])
			expected := strings.ToLower(strings.TrimSpace(asset.hash))
			if len(expected) > len(actual) || actual[:len(expected)] != expected {
				return fmt.Errorf("build asset %s/%s hash does not match manifest", asset.bucket, asset.file)
			}
		}
	}
	if manifest.SceneAssets != nil && strings.TrimSpace(manifest.SceneAssets.File) != "" {
		if err := verifyRootBuildFile(root, manifest.SceneAssets.File); err != nil {
			return fmt.Errorf("scene asset manifest: %w", err)
		}
	}
	return nil
}

func verifyRootBuildFile(root, name string) error {
	cleanFile := filepath.Clean(strings.TrimSpace(name))
	if cleanFile == "." || filepath.IsAbs(cleanFile) || cleanFile == ".." || strings.HasPrefix(cleanFile, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe build path %q", name)
	}
	info, err := os.Stat(filepath.Join(root, cleanFile))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	return nil
}

type buildAssetRef struct {
	bucket string
	file   string
	hash   string
	size   int64
}

func buildAssetFiles(manifest *buildmanifest.Manifest) []buildAssetRef {
	if manifest == nil {
		return nil
	}
	refs := make([]buildAssetRef, 0, 24+len(manifest.Runtime.WASMVariants)+len(manifest.CSS)+len(manifest.Islands))
	add := func(bucket string, asset buildmanifest.HashedAsset) {
		if strings.TrimSpace(asset.File) != "" {
			refs = append(refs, buildAssetRef{bucket: bucket, file: asset.File, hash: asset.Hash, size: asset.Size})
		}
	}
	runtimeAssets := manifest.Runtime
	for _, asset := range []buildmanifest.HashedAsset{
		runtimeAssets.WASM, runtimeAssets.WASMIslands, runtimeAssets.WASMExec,
		runtimeAssets.StandardGoWASMExec, runtimeAssets.Bootstrap, runtimeAssets.BootstrapLite,
		runtimeAssets.BootstrapRuntime, runtimeAssets.BootstrapFeatureIslands,
		runtimeAssets.BootstrapFeatureEngines, runtimeAssets.BootstrapFeatureHubs,
		runtimeAssets.BootstrapFeatureControllers, runtimeAssets.BootstrapFeatureTextlayout,
		runtimeAssets.BootstrapFeatureScene3D, runtimeAssets.BootstrapFeatureScene3DCommand,
		runtimeAssets.BootstrapFeatureScene3DWebGPU, runtimeAssets.BootstrapFeatureScene3DWebGL,
		runtimeAssets.BootstrapFeatureScene3DGLTF, runtimeAssets.BootstrapFeatureScene3DAnimation,
		runtimeAssets.BootstrapFeatureScene3DCompute, runtimeAssets.BootstrapFeatureScene3DDecompress,
		runtimeAssets.Patch, runtimeAssets.VideoHLS, runtimeAssets.StripeBridge, runtimeAssets.Relay,
	} {
		add("runtime", asset)
	}
	for _, asset := range runtimeAssets.WASMVariants {
		add("runtime", asset.HashedAsset)
	}
	for _, asset := range manifest.CSS {
		add("css", asset.HashedAsset)
	}
	for _, asset := range manifest.Islands {
		add("islands", asset.HashedAsset)
	}
	return refs
}

func publicSiteRoutes() []string {
	routes := []string{"/", "/demos"}
	routes = append(routes, docsapp.DocsCatalogRoutes()...)
	for _, demo := range demospages.Demos() {
		routes = append(routes, "/demos/"+demo.Slug)
	}
	seen := make(map[string]struct{}, len(routes))
	public := make([]string, 0, len(routes))
	for _, routePath := range routes {
		routePath = strings.TrimSpace(routePath)
		if routePath == "" || strings.HasPrefix(routePath, "/test/") {
			continue
		}
		if _, ok := seen[routePath]; ok {
			continue
		}
		seen[routePath] = struct{}{}
		public = append(public, routePath)
	}
	sort.Strings(public)
	return public
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Location string `xml:"loc"`
}

func buildSitemapXML(routes []string) string {
	set := sitemapURLSet{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, routePath := range routes {
		set.URLs = append(set.URLs, sitemapURL{Location: docsapp.PublicSiteURL(routePath)})
	}
	data, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(data) + "\n"
}
