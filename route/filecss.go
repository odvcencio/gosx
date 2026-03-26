package route

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
)

var fileCSSNodeCache sync.Map

func addFileCSSHead(ctx *RouteContext, files ...string) {
	if ctx == nil {
		return
	}
	for _, file := range files {
		node, ok := fileCSSNode(file)
		if !ok || node.IsZero() {
			continue
		}
		ctx.AddHead(node)
	}
}

func fileCSSNode(file string) (gosx.Node, bool) {
	cssPath := sidecarCSSPath(file)
	if cssPath == "" {
		return gosx.Node{}, false
	}
	if cached, ok := fileCSSNodeCache.Load(cssPath); ok {
		node, _ := cached.(gosx.Node)
		return node, !node.IsZero()
	}

	data, err := os.ReadFile(cssPath)
	if err != nil {
		fileCSSNodeCache.Store(cssPath, gosx.Node{})
		return gosx.Node{}, false
	}

	node := gosx.El("style",
		gosx.Attrs(gosx.Attr("data-gosx-file-css", filepath.ToSlash(filepath.Base(cssPath)))),
		gosx.RawHTML(string(data)),
	)
	fileCSSNodeCache.Store(cssPath, node)
	return node, true
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
