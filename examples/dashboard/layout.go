package main

import (
	"log"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

// layoutProgram is layout.gsx's compiled IR, loaded once at startup —
// chromeProgram's counterpart for the document shell (chrome.go). Every
// route.RenderProgramComponentNode call in main.go reads through the same
// cached program (route.LoadFileProgramHere caches by path and mtime), so
// an edit to layout.gsx invalidates it exactly the way an edit to
// chrome.gsx does.
var layoutProgram = mustLoadLayoutProgram()

func mustLoadLayoutProgram() *ir.Program {
	prog, err := route.LoadFileProgramHere("layout.gsx")
	if err != nil {
		log.Fatalf("load layout.gsx: %v", err)
	}
	return prog
}
