package prose

import (
	"embed"
	"io/fs"
	"net/http"
)

const (
	// DefaultStylesheetPath is the conventional URL for the shared prose CSS.
	DefaultStylesheetPath = "/prose.css"
	// DefaultRuntimeScriptPath is the standalone generic prose-stream runtime.
	DefaultRuntimeScriptPath = "/prose-runtime.js"
)

//go:embed assets/prose.css assets/prose-runtime.js
var embeddedAssets embed.FS

// RuntimeScript returns the standalone generic prose-stream runtime source.
// Optional shells can compose it with a domain-specific adapter while keeping
// keyed reconciliation in the core prose package.
func RuntimeScript() string {
	data, err := fs.ReadFile(embeddedAssets, "assets/prose-runtime.js")
	if err != nil {
		return ""
	}
	return string(data)
}

// AssetHandler serves the shared prose stylesheet, standalone runtime, and
// other GoSX prose assets without importing the editor module.
func AssetHandler() http.Handler {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(assets))
}
