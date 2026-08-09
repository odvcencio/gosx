package server

import (
	_ "embed"
	"net/http"
)

// DevtoolsLanternPath is the runtime asset path that serves the Lantern
// Scene3D inspector overlay. Apps opt in by including a script tag for this
// path — for example only when devtools are enabled or a debug query param
// is present. The script is read-only: it consumes the Scene3D debug
// registry that already ships in the runtime bundle.
const DevtoolsLanternPath = "/gosx/devtools-lantern.js"

//go:embed devtools_lantern.js
var devtoolsLanternJS []byte

// DevtoolsLanternScriptTag returns the HTML script tag that loads the
// Lantern inspector. Include it before the Scene3D feature chunk so the
// inspector's cull readback gate exists before the scene mounts.
func DevtoolsLanternScriptTag() string {
	return `<script defer src="` + DevtoolsLanternPath + `"></script>`
}

func serveDevtoolsLantern(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	MarkObservedRequest(r, "runtime", DevtoolsLanternPath)
	w.Write(devtoolsLanternJS)
}
