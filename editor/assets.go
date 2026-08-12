package editor

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

const (
	// DefaultStylesheetURL is the conventional mount path for the editor CSS.
	DefaultStylesheetURL = "/editor/editor.css"
	// DefaultDiagramScriptURL is the conventional mount path for Markdown++ diagram enhancement.
	DefaultDiagramScriptURL = "/editor/mdpp-diagrams.js"
	// DefaultScriptURL is the conventional mount path for the native editor runtime.
	DefaultScriptURL = "/editor/native-editor.js"
)

//go:embed assets/editor.css assets/mdpp-diagrams.ts assets/native-editor.ts assets/collaborative-editor.ts assets/code-intelligence.ts
var embeddedAssets embed.FS

// AssetHandler serves the optional native editor browser assets.
//
// Mount it under /editor/ with http.StripPrefix:
//
//	app.Mount("/editor/", http.StripPrefix("/editor/", editor.AssetHandler()))
func AssetHandler() http.Handler {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep the public asset URLs stable while the embedded source authorities
		// are typed. The browser still receives JavaScript at the historical .js
		// paths; only the Go embed lookup uses the .ts source names.
		for _, name := range []string{"mdpp-diagrams", "native-editor", "collaborative-editor", "code-intelligence"} {
			if r.URL.Path == "/"+name+".js" {
				clone := r.Clone(r.Context())
				url := *r.URL
				url.Path = "/" + name + ".ts"
				clone.URL = &url
				files.ServeHTTP(w, clone)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, ".ts") {
			// Typed source names are not part of the public asset surface.
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
