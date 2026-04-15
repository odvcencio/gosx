package route

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
	gosxcss "github.com/odvcencio/gosx/css"
	"github.com/odvcencio/gosx/server"
)

var fileCSSNodeCache sync.Map
var fileScene3DStyleCache sync.Map

func addRouteFileCSSHead(ctx *RouteContext, page FilePage) {
	if ctx == nil {
		return
	}
	order := 0
	if node, ok := fileCSSNode(page.Root, globalCSSPath(page.Root), server.CSSLayerGlobal, order); ok && !node.IsZero() {
		ctx.AddHead(node)
		order++
	}
	for _, file := range page.Layouts {
		node, ok := fileCSSNode(page.Root, sidecarCSSPath(file), server.CSSLayerLayout, order)
		if !ok || node.IsZero() {
			continue
		}
		ctx.AddHead(node)
		order++
	}
	if node, ok := fileCSSNode(page.Root, sidecarCSSPath(page.FilePath), server.CSSLayerPage, order); ok && !node.IsZero() {
		ctx.AddHead(node)
	}
}

func addFileCSSHead(ctx *RouteContext, files ...string) {
	if ctx == nil {
		return
	}
	for _, file := range files {
		node, ok := fileCSSNode("", sidecarCSSPath(file), server.CSSLayerPage, 0)
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

func fileCSSCacheKey(cssPath string, layer server.CSSLayer, order int) string {
	if cssPath == "" {
		return ""
	}
	return strings.Join([]string{cssPath, string(layer), strconv.Itoa(order)}, "\x00")
}

func fileCSSNode(root, cssPath string, layer server.CSSLayer, order int) (gosx.Node, bool) {
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
	cssText, _ = gosxcss.ExtractScene3DStyles(cssText)
	if fileCSSLayerNeedsScope(layer) {
		scopeID = fileCSSScopeID(cssPath)
		cssText = gosxcss.ScopeCSS(cssText, scopeID)
	}

	node := gosx.El("style",
		gosx.Attrs(
			gosx.Attr("data-gosx-file-css", filepath.ToSlash(filepath.Base(cssPath))),
			gosx.Attr("data-gosx-css-layer", string(layer)),
			gosx.Attr("data-gosx-css-owner", server.FileStylesheetOwner(layer)),
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

func fileAncestorScene3DStyles(page FilePage) gosxcss.Scene3DStylesheet {
	var sheet gosxcss.Scene3DStylesheet
	sheet = sheet.Merge(fileScene3DStyles(globalCSSPath(page.Root)))
	for _, file := range page.Layouts {
		sheet = sheet.Merge(fileScene3DStyles(sidecarCSSPath(file)))
	}
	return sheet
}

func fileScene3DStyles(cssPath string) gosxcss.Scene3DStylesheet {
	if cssPath == "" {
		return gosxcss.Scene3DStylesheet{}
	}
	if cached, ok := fileScene3DStyleCache.Load(cssPath); ok {
		sheet, _ := cached.(gosxcss.Scene3DStylesheet)
		return sheet
	}
	data, err := os.ReadFile(cssPath)
	if err != nil {
		fileScene3DStyleCache.Store(cssPath, gosxcss.Scene3DStylesheet{})
		return gosxcss.Scene3DStylesheet{}
	}
	_, sheet := gosxcss.ExtractScene3DStyles(string(data))
	fileScene3DStyleCache.Store(cssPath, sheet)
	return sheet
}

func fileCSSLayerNeedsScope(layer server.CSSLayer) bool {
	return layer == server.CSSLayerLayout || layer == server.CSSLayerPage
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
