package editor

import (
	"embed"
	"io/fs"
	"net/http"

	"m31labs.dev/gosx/prose"
)

const (
	// DefaultStylesheetURL is the conventional mount path for the editor CSS.
	DefaultStylesheetURL = "/editor/editor.css"
	// DefaultProseStylesheetURL is the conventional mount path for shared prose CSS.
	DefaultProseStylesheetURL = "/editor/prose.css"
	// DefaultProseScriptURL is the conventional mount path for the core
	// standalone prose runtime composed with the editor's Markdown++ adapter.
	DefaultProseScriptURL = "/editor/prose-runtime.js"
	// DefaultDiagramScriptURL is the conventional mount path for Markdown++ diagram enhancement.
	DefaultDiagramScriptURL = "/editor/mdpp-diagrams.js"
	// DefaultScriptURL is the conventional mount path for the native editor runtime.
	DefaultScriptURL = "/editor/native-editor.js"
)

//go:embed assets/editor.css assets/prose-runtime.js assets/mdpp-diagrams.js assets/native-editor.js
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
	editorAssets := http.FileServer(http.FS(assets))
	proseAssets := prose.AssetHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prose.DefaultStylesheetPath {
			proseAssets.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == prose.DefaultRuntimeScriptPath {
			serveProseRuntime(w, r)
			return
		}
		editorAssets.ServeHTTP(w, r)
	})
}

func serveProseRuntime(w http.ResponseWriter, r *http.Request) {
	adapter, err := fs.ReadFile(embeddedAssets, "assets/prose-runtime.js")
	core := prose.RuntimeScript()
	if err != nil || core == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(core))
	_, _ = w.Write([]byte("\n"))
	_, _ = w.Write(adapter)
}
