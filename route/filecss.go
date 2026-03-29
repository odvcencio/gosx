package route

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
	gosxcss "github.com/odvcencio/gosx/css"
)

var fileCSSNodeCache sync.Map

type fileCSSLayer string

const (
	fileCSSLayerGlobal  fileCSSLayer = "global"
	fileCSSLayerLayout  fileCSSLayer = "layout"
	fileCSSLayerPage    fileCSSLayer = "page"
	fileCSSLayerRuntime fileCSSLayer = "runtime"
)

func addRouteFileCSSHead(ctx *RouteContext, page FilePage) {
	if ctx == nil {
		return
	}
	order := 0
	if node, ok := fileCSSNode(page.Root, globalCSSPath(page.Root), fileCSSLayerGlobal, order); ok && !node.IsZero() {
		ctx.AddHead(node)
		order++
	}
	for _, file := range page.Layouts {
		node, ok := fileCSSNode(page.Root, sidecarCSSPath(file), fileCSSLayerLayout, order)
		if !ok || node.IsZero() {
			continue
		}
		ctx.AddHead(node)
		order++
	}
	if node, ok := fileCSSNode(page.Root, sidecarCSSPath(page.FilePath), fileCSSLayerPage, order); ok && !node.IsZero() {
		ctx.AddHead(node)
	}
}

func addFileCSSHead(ctx *RouteContext, files ...string) {
	if ctx == nil {
		return
	}
	for _, file := range files {
		node, ok := fileCSSNode("", sidecarCSSPath(file), fileCSSLayerPage, 0)
		if !ok || node.IsZero() {
			continue
		}
		ctx.AddHead(node)
	}
}

func globalCSSPath(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	candidate := filepath.Join(root, "global.css")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

func fileCSSCacheKey(cssPath string, layer fileCSSLayer, order int) string {
	if cssPath == "" {
		return ""
	}
	return strings.Join([]string{cssPath, string(layer), strconv.Itoa(order)}, "\x00")
}

func fileCSSNode(root, cssPath string, layer fileCSSLayer, order int) (gosx.Node, bool) {
	if cssPath == "" {
		return gosx.Node{}, false
	}
	cacheKey := fileCSSCacheKey(cssPath, layer, order)
	if cached, ok := fileCSSNodeCache.Load(cacheKey); ok {
		node, _ := cached.(gosx.Node)
		return node, !node.IsZero()
	}

	data, err := os.ReadFile(cssPath)
	if err != nil {
		fileCSSNodeCache.Store(cacheKey, gosx.Node{})
		return gosx.Node{}, false
	}

	source := fileCSSSource(root, cssPath)
	scopeID := ""
	cssText := string(data)
	if fileCSSLayerNeedsScope(layer) {
		scopeID = fileCSSScopeID(cssPath)
		cssText = gosxcss.ScopeCSS(cssText, scopeID)
	}

	node := gosx.El("style",
		gosx.Attrs(
			gosx.Attr("data-gosx-file-css", filepath.ToSlash(filepath.Base(cssPath))),
			gosx.Attr("data-gosx-css-layer", string(layer)),
			gosx.Attr("data-gosx-css-owner", "route-file"),
			gosx.Attr("data-gosx-css-source", source),
			gosx.Attr("data-gosx-css-order", strconv.Itoa(order)),
			gosx.Attr("data-gosx-css-scope", scopeID),
			gosx.Attr("data-gosx-file-css-scope", scopeID),
		),
		gosx.RawHTML(cssText),
	)
	fileCSSNodeCache.Store(cacheKey, node)
	return node, true
}

func fileCSSLayerNeedsScope(layer fileCSSLayer) bool {
	return layer == fileCSSLayerLayout || layer == fileCSSLayerPage
}

func fileCSSSource(root, cssPath string) string {
	root = strings.TrimSpace(root)
	cssPath = strings.TrimSpace(cssPath)
	if root != "" {
		if rel, err := filepath.Rel(root, cssPath); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Base(cssPath))
}

func sidecarCSSPath(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	ext := filepath.Ext(file)
	if ext == "" {
		return ""
	}
	candidate := strings.TrimSuffix(file, ext) + ".css"
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

func fileCSSScopeID(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	return gosxcss.ScopeID(filepath.ToSlash(file))
}
