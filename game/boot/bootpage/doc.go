// Package bootpage renders the server-side parts of the boot gate: preload
// links for the Go WASM module and its wasm_exec script, and a small
// JavaScript watchdog that runs before the framework scripts.
//
// The watchdog must not depend on the WASM module. A module that fails to
// download is the failure it exists to catch.
//
// Put the output in the page head:
//
//	bootpage.PreloadLinks(manifest, "/gosx/assets", "game", bootpage.PreloadOptions{Integrity: true})
//	bootpage.Watchdog()
//
// and mark the page root with the data attributes listed on Watchdog.
package bootpage
