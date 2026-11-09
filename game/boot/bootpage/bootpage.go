package bootpage

import (
	_ "embed"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
)

//go:embed watchdog.js
var watchdogJS string

// WatchdogSource returns the watchdog script text. It is stable for a given
// build of GoSX, so a Content-Security-Policy hash of it can be computed once.
func WatchdogSource() string { return watchdogJS }

// Watchdog returns an inline script that watches the element marked
// data-boot-root. It reads these attributes from that element:
//
//	data-boot                 "loading" at first; the Go gate sets "ready" (or the page does)
//	data-boot-budget-ms       stall budget, default 12000
//	data-boot-recovering-title / -copy, data-boot-failed-title / -copy
//	                          text for [data-boot-title] and [data-boot-copy]
//
// When the budget runs out before data-boot becomes "ready", the script
// reloads the page once (guarded by sessionStorage, shared with the Go gate)
// and then shows the failed state. The [data-boot-retry] button clears the
// guard and reloads. The timer pauses while the tab is hidden.
func Watchdog() gosx.Node {
	return gosx.El("script", gosx.Attrs(gosx.Attr("id", "gosx-boot-watchdog")), gosx.RawHTML(watchdogJS))
}

// PreloadOptions tunes PreloadLinks.
type PreloadOptions struct {
	// Integrity adds the manifest's Subresource Integrity value to the links.
	// Use it only when the page's own script tag carries the same value, or
	// the browser will not reuse the preloaded response.
	Integrity bool
}

// PreloadLinks returns <link rel="preload"> tags for the standard Go
// wasm_exec script and the named Go WASM module, with URLs from the build
// manifest (the same URLs the framework uses). It returns an empty fragment
// when the manifest has no such module, so a development build without
// build.json renders nothing. assetBase is the asset URL prefix, normally
// "/gosx/assets".
func PreloadLinks(m *buildmanifest.Manifest, assetBase, module string, opt PreloadOptions) gosx.Node {
	if m == nil {
		return gosx.Fragment()
	}
	var nodes []gosx.Node
	if exec := m.Runtime.StandardGoWASMExec; exec.File != "" && !strings.Contains(exec.File, "/") {
		nodes = append(nodes, link(buildmanifest.AssetURL(assetBase, "runtime", exec.File), "script", "", exec.Integrity, opt))
	}
	if url := m.GoWASMURL(assetBase, module); url != "" {
		nodes = append(nodes, link(url, "fetch", "application/wasm", m.GoWASM[module].Integrity, opt))
	}
	return gosx.Fragment(nodes...)
}

func link(href, as, typ, integrity string, opt PreloadOptions) gosx.Node {
	attrs := []any{
		gosx.Attr("rel", "preload"), gosx.Attr("as", as), gosx.Attr("href", href),
		gosx.Attr("crossorigin", "anonymous"), gosx.Attr("fetchpriority", "high"),
	}
	if typ != "" {
		attrs = append(attrs, gosx.Attr("type", typ))
	}
	if opt.Integrity && integrity != "" {
		attrs = append(attrs, gosx.Attr("integrity", integrity))
	}
	return gosx.El("link", gosx.Attrs(attrs...))
}
