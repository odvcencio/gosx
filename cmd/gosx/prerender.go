package main

import (
	"encoding/json"
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

	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/route"
	"golang.org/x/net/html"
)

type exportManifest struct {
	Pages  []string      `json:"pages"`
	Routes []exportRoute `json:"routes,omitempty"`
}

type exportRoute struct {
	Path              string            `json:"path"`
	File              string            `json:"file"`
	RevalidateSeconds int64             `json:"revalidateSeconds,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Capabilities      routeCapabilities `json:"capabilities"`
}

type routeCapabilities struct {
	Navigation    bool   `json:"navigation"`
	Bootstrap     bool   `json:"bootstrap"`
	BootstrapMode string `json:"bootstrapMode,omitempty"`
	WASM          bool   `json:"wasm"`
	Islands       int    `json:"islands,omitempty"`
	Engines       int    `json:"engines,omitempty"`
	Hubs          int    `json:"hubs,omitempty"`
	Scene3D       bool   `json:"scene3d,omitempty"`
	Video         bool   `json:"video,omitempty"`
	Motion        bool   `json:"motion,omitempty"`
}

type staticExportOptions struct {
	AppRoot     string
	OutputDir   string
	BinaryPath  string
	StageAssets func(outputDir string) error
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
	if strings.TrimSpace(opts.BinaryPath) == "" {
		return exportManifest{}, fmt.Errorf("static export binary path is required")
	}
	binaryPath, err := filepath.Abs(opts.BinaryPath)
	if err != nil {
		return exportManifest{}, fmt.Errorf("resolve binary path %s: %w", opts.BinaryPath, err)
	}

	routes, err := staticExportRoutes(filepath.Join(appRoot, "app"))
	if err != nil {
		return exportManifest{}, err
	}
	pages := make([]string, 0, len(routes))
	for _, entry := range routes {
		pages = append(pages, entry.Path)
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
	cmd.Env = append(os.Environ(),
		"PORT="+internalPort,
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

	if err := os.RemoveAll(outputDir); err != nil {
		return exportManifest{}, fmt.Errorf("clear export dir: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return exportManifest{}, fmt.Errorf("create export dir: %w", err)
	}
	if err := copyDirIfPresent(filepath.Join(appRoot, "public"), outputDir); err != nil {
		return exportManifest{}, fmt.Errorf("copy public assets: %w", err)
	}
	if opts.StageAssets != nil {
		if err := opts.StageAssets(outputDir); err != nil {
			return exportManifest{}, err
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	for i, entry := range routes {
		pageHTML, err := fetchExportPage(client, baseURL+entry.Path)
		if err != nil {
			return exportManifest{}, fmt.Errorf("export %s: %w", entry.Path, err)
		}
		routes[i].Capabilities = routeCapabilitiesFromHTML(pageHTML)
		pageHTML, err = rewriteStaticExportHTML(entry.Path, pageHTML)
		if err != nil {
			return exportManifest{}, fmt.Errorf("rewrite %s: %w", entry.Path, err)
		}
		if err := writeExportPage(outputDir, entry.Path, pageHTML); err != nil {
			return exportManifest{}, err
		}
	}

	if missingHTML, status, err := fetchExportPageWithStatus(client, baseURL+"/__gosx_export_missing__"); err == nil && status == http.StatusNotFound {
		missingHTML, err = rewriteStaticExportHTML("/__gosx_export_missing__", missingHTML)
		if err != nil {
			return exportManifest{}, fmt.Errorf("rewrite 404 page: %w", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "404.html"), []byte(missingHTML), 0644); err != nil {
			return exportManifest{}, fmt.Errorf("write 404.html: %w", err)
		}
	}

	return exportManifest{Pages: pages, Routes: routes}, nil
}

func routeCapabilitiesFromHTML(input string) routeCapabilities {
	root, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return routeCapabilities{}
	}
	caps := routeCapabilities{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			applyRouteCapabilityElement(&caps, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return caps
}

func applyRouteCapabilityElement(caps *routeCapabilities, node *html.Node) {
	attrs := nodeAttrMap(node.Attr)
	if _, ok := attrs["data-gosx-navigation"]; ok {
		caps.Navigation = true
	}
	if _, ok := attrs["data-gosx-motion"]; ok {
		caps.Motion = true
	}
	if value := attrs["data-gosx-enhance"]; strings.EqualFold(value, "motion") {
		caps.Motion = true
	}
	if engineName := strings.TrimSpace(attrs["data-gosx-engine"]); engineName != "" {
		if strings.EqualFold(engineName, "GoSXScene3D") {
			caps.Scene3D = true
		}
	}
	if _, ok := attrs["data-gosx-scene3d"]; ok {
		caps.Scene3D = true
	}
	if strings.EqualFold(strings.TrimSpace(attrs["data-gosx-engine-kind"]), "video") {
		caps.Video = true
	}
	if script := strings.TrimSpace(attrs["data-gosx-script"]); script != "" {
		caps.Bootstrap = caps.Bootstrap || script == "bootstrap"
		switch script {
		case "bootstrap":
			caps.BootstrapMode = strings.TrimSpace(attrs["data-gosx-bootstrap-mode"])
		case "wasm-exec":
			caps.WASM = true
		case "feature-scene3d":
			caps.Scene3D = true
		}
	}
	if node.Data == "script" && attrs["id"] == "gosx-manifest" {
		applyRouteCapabilityManifest(caps, nodeText(node))
	}
}

func applyRouteCapabilityManifest(caps *routeCapabilities, raw string) {
	var manifest hydrate.Manifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return
	}
	caps.Islands = len(manifest.Islands)
	caps.Engines = max(caps.Engines, len(manifest.Engines))
	caps.Hubs = len(manifest.Hubs)
	if strings.TrimSpace(manifest.Runtime.Path) != "" {
		caps.WASM = true
	}
	if len(manifest.Islands) > 0 || len(manifest.Engines) > 0 || len(manifest.Hubs) > 0 {
		caps.Bootstrap = true
	}
	for _, entry := range manifest.Engines {
		if strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			caps.Scene3D = true
		}
		if strings.EqualFold(strings.TrimSpace(entry.Kind), "video") {
			caps.Video = true
		}
	}
}

func nodeAttrMap(attrs []html.Attribute) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		out[strings.ToLower(strings.TrimSpace(attr.Key))] = attr.Val
	}
	return out
}

func nodeText(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
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
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(data), resp.StatusCode, nil
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
