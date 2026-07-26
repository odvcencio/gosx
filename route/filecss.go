package route

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"m31labs.dev/gosx"
	gosxcss "m31labs.dev/gosx/css"
	"m31labs.dev/gosx/server"
)

// cssFile names a stylesheet on disk together with the freshness data that one
// os.Stat supplies.
//
// WHY it carries the stat data: the caches below used to key on the path alone,
// so an edited stylesheet never reached the next render and a restart was the
// only way to see it. The path lookup already pays one stat to decide whether
// the file exists, so the modification time and the size come free.
type cssFile struct {
	path        string
	modTimeNano int64
	size        int64
}

func (f cssFile) empty() bool { return f.path == "" }

// fileCSSNodeCache maps a cache key to one cssNodeEntry. The entry replaces the
// old one when the file changes, so the map holds one entry per stylesheet and
// per layer, not one per edit.
var fileCSSNodeCache sync.Map

// fileScene3DStyleCache maps a stylesheet path to one scene3DStyleEntry.
var fileScene3DStyleCache sync.Map

type cssNodeEntry struct {
	modTimeNano int64
	size        int64
	node        gosx.Node
}

type scene3DStyleEntry struct {
	modTimeNano int64
	size        int64
	sheet       gosxcss.Scene3DStylesheet
}

func addRouteFileCSSHead(ctx *RouteContext, page FilePage) {
	if ctx == nil {
		return
	}
	order := 0
	if node, ok := fileCSSNode(page.Root, globalCSSFile(page.Root), server.CSSLayerGlobal, order); ok && !node.IsZero() {
		ctx.AddHead(node)
		order++
	}
	for _, file := range page.Layouts {
		node, ok := fileCSSNode(page.Root, sidecarCSSFile(file), server.CSSLayerLayout, order)
		if !ok || node.IsZero() {
			continue
		}
		ctx.AddHead(node)
		order++
	}
	if node, ok := fileCSSNode(page.Root, sidecarCSSFile(page.FilePath), server.CSSLayerPage, order); ok && !node.IsZero() {
		ctx.AddHead(node)
	}
}

func addFileCSSHead(ctx *RouteContext, files ...string) {
	if ctx == nil {
		return
	}
	for _, file := range files {
		node, ok := fileCSSNode("", sidecarCSSFile(file), server.CSSLayerPage, 0)
		if !ok || node.IsZero() {
			continue
		}
		ctx.AddHead(node)
	}
}

// statCSSFile returns the stylesheet at candidate when it exists.
func statCSSFile(candidate string) (cssFile, bool) {
	if candidate == "" {
		return cssFile{}, false
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return cssFile{}, false
	}
	return cssFile{
		path:        candidate,
		modTimeNano: info.ModTime().UnixNano(),
		size:        info.Size(),
	}, true
}

func globalCSSFile(root string) cssFile {
	root = strings.TrimSpace(root)
	if root == "" {
		return cssFile{}
	}
	file, _ := statCSSFile(filepath.Join(root, "global.css"))
	return file
}

func fileCSSCacheKey(cssPath string, layer server.CSSLayer, order int) string {
	if cssPath == "" {
		return ""
	}
	return strings.Join([]string{cssPath, string(layer), strconv.Itoa(order)}, "\x00")
}

func fileCSSNode(root string, css cssFile, layer server.CSSLayer, order int) (gosx.Node, bool) {
	if css.empty() {
		return gosx.Node{}, false
	}
	cacheKey := fileCSSCacheKey(css.path, layer, order)
	if cached, ok := fileCSSNodeCache.Load(cacheKey); ok {
		entry, _ := cached.(cssNodeEntry)
		if entry.modTimeNano == css.modTimeNano && entry.size == css.size {
			return entry.node, !entry.node.IsZero()
		}
	}

	data, err := os.ReadFile(css.path)
	if err != nil {
		fileCSSNodeCache.Store(cacheKey, cssNodeEntry{modTimeNano: css.modTimeNano, size: css.size})
		return gosx.Node{}, false
	}

	source := fileCSSSource(root, css.path)
	scopeID := ""
	cssText := string(data)
	cssText, _ = gosxcss.ExtractScene3DStyles(cssText)
	if fileCSSLayerNeedsScope(layer) {
		scopeID = fileCSSScopeID(css.path)
		cssText = gosxcss.ScopeCSS(cssText, scopeID)
	}

	node := gosx.El("style",
		gosx.Attrs(
			gosx.Attr("data-gosx-file-css", filepath.ToSlash(filepath.Base(css.path))),
			gosx.Attr("data-gosx-css-layer", string(layer)),
			gosx.Attr("data-gosx-css-owner", server.FileStylesheetOwner(layer)),
			gosx.Attr("data-gosx-css-source", source),
			gosx.Attr("data-gosx-css-order", strconv.Itoa(order)),
			gosx.Attr("data-gosx-css-scope", scopeID),
			gosx.Attr("data-gosx-file-css-scope", scopeID),
		),
		gosx.RawHTML(cssText),
	)
	fileCSSNodeCache.Store(cacheKey, cssNodeEntry{
		modTimeNano: css.modTimeNano,
		size:        css.size,
		node:        node,
	})
	return node, true
}

func fileAncestorScene3DStyles(page FilePage) gosxcss.Scene3DStylesheet {
	var sheet gosxcss.Scene3DStylesheet
	sheet = sheet.Merge(fileScene3DStyles(globalCSSFile(page.Root)))
	for _, file := range page.Layouts {
		sheet = sheet.Merge(fileScene3DStyles(sidecarCSSFile(file)))
	}
	return sheet
}

func fileScene3DStyles(css cssFile) gosxcss.Scene3DStylesheet {
	if css.empty() {
		return gosxcss.Scene3DStylesheet{}
	}
	if cached, ok := fileScene3DStyleCache.Load(css.path); ok {
		entry, _ := cached.(scene3DStyleEntry)
		if entry.modTimeNano == css.modTimeNano && entry.size == css.size {
			return entry.sheet
		}
	}
	data, err := os.ReadFile(css.path)
	if err != nil {
		fileScene3DStyleCache.Store(css.path, scene3DStyleEntry{modTimeNano: css.modTimeNano, size: css.size})
		return gosxcss.Scene3DStylesheet{}
	}
	_, sheet := gosxcss.ExtractScene3DStyles(string(data))
	fileScene3DStyleCache.Store(css.path, scene3DStyleEntry{
		modTimeNano: css.modTimeNano,
		size:        css.size,
		sheet:       sheet,
	})
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

// sidecarCSSFile returns the stylesheet that sits beside a page or layout file.
func sidecarCSSFile(file string) cssFile {
	file = strings.TrimSpace(file)
	if file == "" {
		return cssFile{}
	}
	ext := filepath.Ext(file)
	if ext == "" {
		return cssFile{}
	}
	css, _ := statCSSFile(file[:len(file)-len(ext)] + ".css")
	return css
}

func fileCSSScopeID(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	return gosxcss.ScopeID(filepath.ToSlash(file))
}
