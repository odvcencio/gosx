package main

import (
	"log"

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
	prog, err := route.LoadFileProgramHere("chrome.gsx")
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

// chromeCard renders chrome.gsx's Card component around children — a
// request-time mix of a chrome() header, a Go-computed island node, and
// sometimes a no-JS fallback — through RenderProgramComponentNode (gosx#226,
// gosx#246). Before that function existed this div.card wrapper had to be
// built by hand with gosx.El in main.go, once per card.
func chromeCard(children ...gosx.Node) gosx.Node {
	node, err := route.RenderProgramComponentNode(chromeProgram, "Card", route.ProgramRenderEnv{}, children...)
	if err != nil {
		log.Fatalf("render chrome.gsx Card: %v", err)
	}
	return node
}
