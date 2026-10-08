package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/basepath"
	"m31labs.dev/gosx/internal/bundlepolicy"
	"m31labs.dev/gosx/internal/localapp"
	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/route"
)

type exportManifest struct {
	BasePath  string        `json:"basePath,omitempty"`
	Pages     []string      `json:"pages"`
	Routes    []exportRoute `json:"routes,omitempty"`
	AssetRefs []string      `json:"-"`
}

type exportRoute struct {
	Path              string            `json:"path"`
	File              string            `json:"file"`
	RevalidateSeconds int64             `json:"revalidateSeconds,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Capabilities      routeCapabilities `json:"capabilities"`
}

type routeCapabilities struct {
	ComputeIslands int    `json:"computeIslands,omitempty"`
	Controllers    int    `json:"controllers,omitempty"`
	Runtime        string `json:"runtime,omitempty"`
	Navigation     bool   `json:"navigation"`
	Bootstrap      bool   `json:"bootstrap"`
	BootstrapMode  string `json:"bootstrapMode,omitempty"`
	WASM           bool   `json:"wasm"`
	Islands        int    `json:"islands,omitempty"`
	Engines        int    `json:"engines,omitempty"`
	Hubs           int    `json:"hubs,omitempty"`
	Scene3D        bool   `json:"scene3d,omitempty"`
	Video          bool   `json:"video,omitempty"`
	Motion         bool   `json:"motion,omitempty"`
}

type staticExportOptions struct {
	AppRoot      string
	OutputDir    string
	BinaryPath   string
	BundlePolicy bundlepolicy.Config
	StageAssets  func(outputDir string, manifest exportManifest) error
}

var errPrivateExportPage = errors.New("response is not shared-cacheable")
var errDynamicExportPage = errors.New("route requires request-time rendering")

type exportMount struct {
	prefix string
	strips bool
}

// publicPath converts a known internal route. It must prepend the mount even
// when that route itself equals or starts with the prefix.
func (m exportMount) publicPath(internal string) string {
	return m.prefix + internal
}

func (m exportMount) upstreamURL(public string) string {
	if m.strips && (public == m.prefix || strings.HasPrefix(public, m.prefix+"/")) {
		public = strings.TrimPrefix(public, m.prefix)
		if public == "" {
			return "/"
		}
	}
	return public
}

func discoverExportMount(client *http.Client, origin string) (exportMount, error) {
	for _, discoveryPath := range []string{"/readyz", "/"} {
		res, err := client.Get(origin + discoveryPath)
		if err != nil {
			return exportMount{}, fmt.Errorf("discover export base path: %w", err)
		}
		prefix, err := basepath.Normalize(res.Header.Get(basepath.ExportPrefixHeader))
		res.Body.Close()
		if err != nil {
			return exportMount{}, err
		}
		if prefix != "" {
			return exportMount{prefix: prefix, strips: res.Header.Get(basepath.ExportStripHeader) == "1"}, nil
		}
	}
	return exportMount{}, nil
}

func prerenderStaticBundle(opts staticExportOptions) (exportManifest, error) {
	appRoot, err := filepath.Abs(opts.AppRoot)
	if err != nil {
		return exportManifest{}, fmt.Errorf("resolve app root %s: %w", opts.AppRoot, err)
	}
	outputDir, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return exportManifest{}, fmt.Errorf("resolve output dir %s: %w", opts.OutputDir, err)
	}
	routes, err := staticExportRoutes(filepath.Join(appRoot, "app"))
	if err != nil {
		return exportManifest{}, err
	}
	pages := make([]string, 0, len(routes))
	if len(routes) == 0 {
		if err := prepareStaticExportDirectory(opts, appRoot, outputDir, ""); err != nil {
			return exportManifest{}, err
		}
		return completeStaticExport(opts, outputDir, exportManifest{Pages: pages})
	}
	if strings.TrimSpace(opts.BinaryPath) == "" {
		return exportManifest{}, fmt.Errorf("static export binary path is required")
	}
	binaryPath, err := filepath.Abs(opts.BinaryPath)
	if err != nil {
		return exportManifest{}, fmt.Errorf("resolve binary path %s: %w", opts.BinaryPath, err)
	}

	internalPort, err := pickFreePort()
	if err != nil {
		return exportManifest{}, fmt.Errorf("pick export port: %w", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", internalPort)

	cmd := exec.Command(binaryPath)
	cmd.Dir = appRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	localEnv, err := localapp.Environment(os.Environ(), internalPort)
	if err != nil {
		return exportManifest{}, err
	}
	cmd.Env = append(localEnv,
		"GOSX_APP_ROOT="+appRoot,
		"GOSX_STATIC_EXPORT=1",
	)
	if err := cmd.Start(); err != nil {
		return exportManifest{}, fmt.Errorf("start export app: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	if err := waitForAppReady(baseURL, 20*time.Second); err != nil {
		return exportManifest{}, fmt.Errorf("wait for export app ready: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	mount, err := discoverExportMount(client, baseURL)
	if err != nil {
		return exportManifest{}, err
	}
	contentDir := filepath.Join(outputDir, filepath.FromSlash(strings.TrimPrefix(mount.prefix, "/")))
	if err := prepareStaticExportDirectory(opts, appRoot, outputDir, mount.prefix); err != nil {
		return exportManifest{}, err
	}
	assetRefs := map[string]struct{}{}
	fileCSSAssets := map[string]bool{}
	exportedRoutes := make([]exportRoute, 0, len(routes))
	for _, entry := range routes {
		internalPath := entry.Path
		entry.Path = mount.publicPath(internalPath)
		entry.File = buildmanifest.ExportFilePath(entry.Path)
		pageHTML, status, headers, err := fetchExportPageResponse(client, baseURL+mount.upstreamURL(entry.Path))
		if errors.Is(err, errDynamicExportPage) {
			continue
		}
		if err == nil && status != http.StatusOK {
			err = fmt.Errorf("unexpected status %d", status)
		}
		if errors.Is(err, errPrivateExportPage) {
			// Keep session-creating and private pages dynamic. A build visitor's
			// token or personalized state must never reach a shared artifact.
			fmt.Fprintf(os.Stderr, "gosx export: keeping %s dynamic (private response)\n", entry.Path)
			continue
		}
		if err != nil {
			return exportManifest{}, fmt.Errorf("export %s: %w", entry.Path, err)
		}
		if headers.Get("X-GoSX-Prerender") == "load" && entry.RevalidateSeconds == 0 {
			fmt.Fprintln(os.Stderr, prerenderLoadWarning(entry.Path))
		}
		entry.Capabilities, err = routeCapabilitiesFromHTML(pageHTML)
		if err != nil {
			return exportManifest{}, fmt.Errorf("export capabilities: %w", err)
		}
		if err := stageExportFileCSS(client, baseURL, outputDir, pageHTML, fileCSSAssets, mount); err != nil {
			return exportManifest{}, fmt.Errorf("export %s stylesheets: %w", entry.Path, err)
		}
		addExportRuntimeAssetRefs(assetRefs, pageHTML)
		pageHTML, err = rewriteStaticExportHTML(entry.Path, pageHTML)
		if err != nil {
			return exportManifest{}, fmt.Errorf("rewrite %s: %w", entry.Path, err)
		}
		if err := writeExportPage(contentDir, internalPath, pageHTML); err != nil {
			return exportManifest{}, err
		}
		pages = append(pages, entry.Path)
		exportedRoutes = append(exportedRoutes, entry)
	}

	if missingHTML, status, err := fetchExportPageWithStatus(client, baseURL+mount.upstreamURL(mount.publicPath("/__gosx_export_missing__"))); err == nil && status == http.StatusNotFound {
		if err := stageExportFileCSS(client, baseURL, outputDir, missingHTML, fileCSSAssets, mount); err != nil {
			return exportManifest{}, fmt.Errorf("export 404 stylesheets: %w", err)
		}
		addExportRuntimeAssetRefs(assetRefs, missingHTML)
		missingHTML, err = rewriteStaticExportHTML(mount.publicPath("/"), missingHTML)
		if err != nil {
			return exportManifest{}, fmt.Errorf("rewrite 404 page: %w", err)
		}
		if err := os.WriteFile(filepath.Join(contentDir, "404.html"), []byte(missingHTML), 0644); err != nil {
			return exportManifest{}, fmt.Errorf("write 404.html: %w", err)
		}
	}

	manifest := exportManifest{BasePath: mount.prefix, Pages: pages, Routes: exportedRoutes, AssetRefs: sortedExportRuntimeAssetRefs(assetRefs)}
	for _, ref := range manifest.AssetRefs {
		if ref != runtimehost.NavigationRuntimePath {
			continue
		}
		data, err := fetchExportPage(client, baseURL+mount.upstreamURL(mount.publicPath(ref)))
		if err != nil {
			return exportManifest{}, fmt.Errorf("export navigation runtime: %w", err)
		}
		dst, ok := exportRuntimeOutputPath(contentDir, ref)
		if !ok {
			return exportManifest{}, fmt.Errorf("invalid navigation runtime path %q", ref)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return exportManifest{}, err
		}
		if err := os.WriteFile(dst, []byte(data), 0644); err != nil {
			return exportManifest{}, err
		}
	}
	return completeStaticExport(opts, outputDir, manifest)
}

func prepareStaticExportDirectory(opts staticExportOptions, appRoot, outputDir, prefix string) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("clear export dir: %w", err)
	}
	// Assets and pages share the public mount on a static host.
	contentDir := filepath.Join(outputDir, filepath.FromSlash(strings.TrimPrefix(prefix, "/")))
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		return fmt.Errorf("create export dir: %w", err)
	}
	if err := bundlepolicy.CopyTree(filepath.Join(appRoot, "public"), contentDir, bundlepolicy.RootPublic, opts.BundlePolicy); err != nil {
		return fmt.Errorf("copy public assets: %w", err)
	}
	return nil
}

func completeStaticExport(opts staticExportOptions, outputDir string, manifest exportManifest) (exportManifest, error) {
	if opts.StageAssets != nil {
		contentDir := filepath.Join(outputDir, filepath.FromSlash(strings.TrimPrefix(manifest.BasePath, "/")))
		if err := opts.StageAssets(contentDir, manifest); err != nil {
			return exportManifest{}, err
		}
	}
	if err := writeTextSidecars(outputDir, opts.BundlePolicy); err != nil {
		return exportManifest{}, fmt.Errorf("compress static export: %w", err)
	}

	return manifest, nil
}

func addExportRuntimeAssetRefs(refs map[string]struct{}, input string) {
	if refs == nil || input == "" {
		return
	}
	for {
		idx := strings.Index(input, "/gosx/")
		if idx < 0 {
			return
		}
		input = input[idx:]
		end := 0
		for end < len(input) && isExportRuntimeAssetURLByte(input[end]) {
			end++
		}
		addExportRuntimeAssetRef(refs, input[:end])
		input = input[end:]
	}
}

func isExportRuntimeAssetURLByte(ch byte) bool {
	switch {
	case ch >= 'a' && ch <= 'z':
		return true
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= '0' && ch <= '9':
		return true
	}
	switch ch {
	case '/', '.', '_', '-', '~', '%', '?', '&', '=', ':', '+', '#', ';', ',', '@', '!', '$', '\'', '(', ')', '*':
		return true
	default:
		return false
	}
}

func addExportRuntimeAssetRef(refs map[string]struct{}, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme != "" || parsed.Host != "" {
		return
	}
	targetPath := path.Clean("/" + strings.TrimLeft(parsed.Path, "/"))
	if !strings.HasPrefix(targetPath, "/gosx/") {
		return
	}
	refs[targetPath] = struct{}{}
}

func sortedExportRuntimeAssetRefs(refs map[string]struct{}) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func routeCapabilitiesFromHTML(input string) (routeCapabilities, error) {
	c, err := pagecaps.FromHTML([]byte(input))
	if err != nil {
		return routeCapabilities{}, err
	}
	mode := c.BootstrapMode
	if mode == "none" {
		mode = ""
	}
	runtime := c.Runtime
	if runtime == "none" {
		runtime = ""
	}
	return routeCapabilities{
		Navigation: c.Navigation, Bootstrap: c.Bootstrap, BootstrapMode: mode, WASM: c.WASM,
		Islands: c.Islands, ComputeIslands: c.ComputeIslands, Engines: c.Engines, Hubs: c.Hubs,
		Controllers: c.Controllers, Scene3D: c.Scene3D, Video: c.Video, Motion: c.Motion, Runtime: runtime,
	}, nil
}

func writeExportManifest(path string, manifest exportManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write export manifest: %w", err)
	}
	return nil
}

func staticExportPages(appDir string) ([]string, error) {
	routes, err := staticExportRoutes(appDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(routes))
	for _, route := range routes {
		paths = append(paths, route.Path)
	}
	return compactPaths(paths), nil
}

func staticExportRoutes(appDir string) ([]exportRoute, error) {
	bundle, err := route.ScanDir(appDir)
	if err != nil {
		return nil, fmt.Errorf("scan file routes: %w", err)
	}

	routes := []exportRoute{}
	for _, page := range bundle.Pages {
		if len(page.Params) > 0 {
			continue
		}
		if !page.Config.PrerenderEnabled(true) {
			continue
		}
		routes = append(routes, exportRoute{
			Path:              page.RoutePath,
			File:              buildmanifest.ExportFilePath(page.RoutePath),
			RevalidateSeconds: exportRouteRevalidateSeconds(page.Config),
			Tags:              append([]string(nil), page.Config.CacheTags...),
		})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].File < routes[j].File
		}
		return routes[i].Path < routes[j].Path
	})
	return compactExportRoutes(routes), nil
}

func compactPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	last := ""
	for _, path := range paths {
		if path == "" {
			path = "/"
		}
		if path == last {
			continue
		}
		out = append(out, path)
		last = path
	}
	return out
}

func compactExportRoutes(routes []exportRoute) []exportRoute {
	if len(routes) == 0 {
		return nil
	}
	out := make([]exportRoute, 0, len(routes))
	last := ""
	for _, route := range routes {
		if route.Path == "" {
			route.Path = "/"
		}
		if route.Path == last {
			continue
		}
		last = route.Path
		if len(route.Tags) == 0 {
			route.Tags = nil
		}
		out = append(out, route)
	}
	return out
}

func exportRouteRevalidateSeconds(config route.FileRouteConfig) int64 {
	policy, ok, err := config.CachePolicy()
	if err != nil || !ok {
		return 0
	}
	if policy.NoStore || policy.Immutable || policy.Private {
		return 0
	}
	ttl := policy.SMaxAge
	if ttl <= 0 {
		ttl = policy.MaxAge
	}
	if ttl <= 0 {
		return 0
	}
	return int64(ttl / time.Second)
}

func fetchExportPage(client *http.Client, url string) (string, error) {
	body, status, err := fetchExportPageWithStatus(client, url)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", status)
	}
	return body, nil
}

func fetchExportPageWithStatus(client *http.Client, url string) (string, int, error) {
	body, status, _, err := fetchExportPageResponse(client, url)
	return body, status, err
}

func prerenderLoadWarning(routePath string) string {
	return fmt.Sprintf("gosx prerender: warning: %s has Load and RevalidateSeconds=0; build-time data stays unchanged until rebuild or explicit invalidation (set a public cache lifetime in route.config.json, or disable prerender)", routePath)
}

func fetchExportPageResponse(client *http.Client, url string) (string, int, http.Header, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", 0, nil, err
	}
	req.Header.Set("Accept", "text/html")
	// Export the identity representation, then compress the rewritten files.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent && resp.Header.Get("X-GoSX-Prerender") == "skip" {
		return "", resp.StatusCode, resp.Header, errDynamicExportPage
	}
	if len(resp.Header.Values("Set-Cookie")) > 0 || exportResponseIsPrivate(resp.Header) {
		return "", resp.StatusCode, resp.Header, errPrivateExportPage
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, resp.Header, err
	}
	return string(data), resp.StatusCode, resp.Header, nil
}

func exportResponseIsPrivate(headers http.Header) bool {
	for _, value := range headers.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(directive), "=")
			switch strings.ToLower(name) {
			case "private", "no-store", "no-cache":
				return true
			}
		}
	}
	return false
}

func writeExportPage(outputDir, routePath, html string) error {
	target := filepath.Join(outputDir, buildmanifest.ExportFilePath(routePath))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("create export dir for %s: %w", routePath, err)
	}
	if err := os.WriteFile(target, []byte(html), 0644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func rewriteStaticExportHTML(routePath, input string) (string, error) {
	root, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return "", err
	}

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for i := range node.Attr {
				key := strings.ToLower(node.Attr[i].Key)
				switch key {
				case "href", "src", "action", "poster":
					node.Attr[i].Val = rewriteStaticExportURL(routePath, node.Attr[i].Val)
				case "srcset":
					node.Attr[i].Val = rewriteStaticExportSrcset(routePath, node.Attr[i].Val)
				case "content":
					if looksLikeExportURLValue(node.Attr, node.Attr[i].Val) {
						node.Attr[i].Val = rewriteStaticExportURL(routePath, node.Attr[i].Val)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)

	var b strings.Builder
	if err := html.Render(&b, root); err != nil {
		return "", err
	}
	return b.String(), nil
}

func looksLikeExportURLValue(attrs []html.Attribute, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return false
	}
	for _, attr := range attrs {
		key := strings.ToLower(attr.Key)
		val := strings.ToLower(strings.TrimSpace(attr.Val))
		if key == "name" && val == "gosx-base-path" {
			return false
		}
		if key != "name" && key != "property" && key != "itemprop" {
			continue
		}
		if strings.Contains(val, "image") || strings.Contains(val, "url") {
			return true
		}
	}
	return true
}

func rewriteStaticExportSrcset(routePath, raw string) string {
	parts := strings.Split(raw, ",")
	for i, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		fields[0] = rewriteStaticExportURL(routePath, fields[0])
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}

func rewriteStaticExportURL(routePath, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "//") {
		return raw
	}

	parsed, err := neturl.Parse(raw)
	if err != nil || parsed == nil {
		return raw
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return raw
	}

	targetPath := parsed.Path
	if strings.HasPrefix(targetPath, "/_gosx/image") {
		if src := strings.TrimSpace(parsed.Query().Get("src")); strings.HasPrefix(src, "/") {
			targetPath = src
			parsed.RawQuery = ""
		} else {
			return raw
		}
	}
	if targetPath == "" || !strings.HasPrefix(targetPath, "/") {
		return raw
	}

	parsed.Path = relativeStaticExportPath(routePath, targetPath)
	return parsed.String()
}

func relativeStaticExportPath(routePath, targetPath string) string {
	targetPath = path.Clean("/" + strings.TrimSpace(targetPath))
	currentDir := filepath.ToSlash(filepath.Dir(buildmanifest.ExportFilePath(routePath)))
	if currentDir == "." {
		currentDir = ""
	}
	base := currentDir
	if base == "" {
		base = "."
	}

	if targetPath == "/" {
		rel, err := filepath.Rel(filepath.FromSlash(base), ".")
		if err != nil || rel == "" {
			return "./"
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return "./"
		}
		if strings.HasSuffix(rel, "/") {
			return rel
		}
		return rel + "/"
	}

	rel, err := filepath.Rel(filepath.FromSlash(base), filepath.FromSlash(strings.TrimPrefix(targetPath, "/")))
	if err != nil || rel == "" {
		return targetPath
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "./"
	}
	return rel
}
