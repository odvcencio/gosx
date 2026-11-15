//go:build js && wasm

package boot

import (
	"strconv"
	"syscall/js"

	"m31labs.dev/gosx/game/storage"
)

// BrowserMarker returns the marker and key for the automatic reload: browser
// sessionStorage, keyed by the page path and query, the same key the
// bootpage watchdog script uses, so the script and the Go gate share one
// automatic reload.
func BrowserMarker() (Marker, string) {
	loc := js.Global().Get("location")
	return storage.Session(), "gosx-boot:" + loc.Get("pathname").String() + loc.Get("search").String()
}

// BrowserReload reloads the page.
func BrowserReload() { js.Global().Get("location").Call("reload") }

// DOMObserver mirrors the gate onto root as attributes and a CSS variable:
// data-boot (loading, recovering, failed, ready), aria-busy, and --boot-progress
// (0 to 1). The kitui loading screen reads them. It returns the OnChange
// function to put in Config, so create the Gate with it:
//
//	cfg.OnChange = boot.DOMObserver(root)
func DOMObserver(root js.Value) func(Snapshot) {
	return func(s Snapshot) {
		if !root.Truthy() {
			return
		}
		root.Get("dataset").Set("boot", s.Phase.String())
		root.Call("setAttribute", "aria-busy", strconv.FormatBool(s.Phase != PhaseReady))
		root.Get("style").Call("setProperty", "--boot-progress", strconv.FormatFloat(s.Progress, 'f', 3, 64))
	}
}
