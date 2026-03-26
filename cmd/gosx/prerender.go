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

	"github.com/odvcencio/gosx/route"
	"golang.org/x/net/html"
)

type exportManifest struct {
	Pages []string `json:"pages"`
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

	pages, err := staticExportPages(filepath.Join(appRoot, "app"))
	if err != nil {
		return exportManifest{}, err
	}

	internalPort, err := pickFreePort()
	if err != nil {
		return exportManifest{}, fmt.Errorf("pick export port: %w", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", internalPort)

	cmd := exec.Command(opts.BinaryPath)
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
	for _, pagePath := range pages {
		pageHTML, err := fetchExportPage(client, baseURL+pagePath)
		if err != nil {
			return exportManifest{}, fmt.Errorf("export %s: %w", pagePath, err)
		}
		pageHTML, err = rewriteStaticExportHTML(pagePath, pageHTML)
		if err != nil {
			return exportManifest{}, fmt.Errorf("rewrite %s: %w", pagePath, err)
		}
		if err := writeExportPage(outputDir, pagePath, pageHTML); err != nil {
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

	return exportManifest{Pages: pages}, nil
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
	bundle, err := route.ScanDir(appDir)
	if err != nil {
		return nil, fmt.Errorf("scan file routes: %w", err)
	}

	paths := []string{}
	for _, page := range bundle.Pages {
		if len(page.Params) > 0 {
			continue
		}
		if !page.Config.PrerenderEnabled(true) {
			continue
		}
		paths = append(paths, page.RoutePath)
	}
	sort.Strings(paths)
	return compactPaths(paths), nil
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
	target := filepath.Join(outputDir, exportFilePath(routePath))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("create export dir for %s: %w", routePath, err)
	}
	if err := os.WriteFile(target, []byte(html), 0644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func exportFilePath(routePath string) string {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" || routePath == "/" {
		return "index.html"
	}
	clean := strings.Trim(routePath, "/")
	return filepath.Join(filepath.FromSlash(clean), "index.html")
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
	currentDir := filepath.ToSlash(filepath.Dir(exportFilePath(routePath)))
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
