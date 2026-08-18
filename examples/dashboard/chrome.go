package main

import (
	"log"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

// chromeProgram is chrome.gsx's compiled IR, loaded once at startup. Every
// route.RenderProgramComponent call below reads through the same cached
// program (route.LoadFileProgram caches by path and mtime), so an edit to
// chrome.gsx invalidates it exactly the way an edit to a file-routed page
// does — there is no second, independently-stale copy.
var chromeProgram = mustLoadChromeProgram()

func mustLoadChromeProgram() *ir.Program {
	_, thisFile, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(thisFile), "chrome.gsx")
	prog, err := route.LoadFileProgram(path)
	if err != nil {
		log.Fatalf("load chrome.gsx: %v", err)
	}
	return prog
}

// chrome renders a zero-prop strict component from chrome.gsx as a Node the
// caller splices next to Go-computed content (an island's rendered HTML,
// primarily) that chrome.gsx itself has no way to accept — see the package
// comment in chrome.gsx.
func chrome(component string) gosx.Node {
	html, err := route.RenderProgramComponent(chromeProgram, component, route.ProgramRenderEnv{})
	if err != nil {
		log.Fatalf("render chrome.gsx %s: %v", component, err)
	}
	return gosx.RawHTML(html)
}
