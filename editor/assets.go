package editor

import (
	"embed"
	"io/fs"
	"net/http"
)

const (
	// DefaultStylesheetURL is the conventional mount path for the editor CSS.
	DefaultStylesheetURL = "/editor/editor.css"
	// DefaultDiagramScriptURL is the conventional mount path for Markdown++ diagram enhancement.
	DefaultDiagramScriptURL = "/editor/mdpp-diagrams.js"
	// DefaultScriptURL is the conventional mount path for the native editor runtime.
	DefaultScriptURL = "/editor/native-editor.js"
)

//go:embed assets/editor.css assets/mdpp-diagrams.js assets/native-editor.js
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
	return http.FileServer(http.FS(assets))
}
