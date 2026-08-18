package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckFileAcceptsStrictCallingTypedLegacy closes the last seam of
// gosx#240. That change made a strict body calling a TYPED legacy component
// legal at lower time and correct at render time, but `gosx check` still
// refused it:
//
//	./page.gsx:12: undefined: Chip
//
// The strict projection retained only strict declarations, so the projected
// Go named a function the projection did not carry, and the Go compiler
// reported an undefined symbol against the author's own component. Two of
// the three seams accepted the composition and the third refused it, which
// means the seams disagreed about what the language is.
//
// transpile.emitTypedLegacyStub now projects a typed legacy component as a
// signature with a stub body. The body stays a stub deliberately:
// emitStrictSourceFile omits legacy bodies because they name data, request,
// and application helpers that ordinary Go cannot resolve and the legacy
// runtime interprets later. Only the caller's reference needs to resolve
// here, and a signature carries that.
func TestCheckFileAcceptsStrictCallingTypedLegacy(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type ChipProps struct {
	Label string
}
func Chip(props ChipProps) Node {
	return <span class="chip">{props.Label}</span>
}
component Page() {
	return <main><Chip Label="hello"/></main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile rejected a strict body calling a typed legacy component: %v", err)
	}
}

// TestCheckFileStillRejectsStrictCallingUntypedLegacy pins the other half.
// An untyped legacy component must stay out of the projection: a strict body
// cannot call one at all, so no projected reference to it can exist. The
// author must get the lower-time diagnostic naming the remedy, never a bare
// undefined-symbol error from the Go compiler.
func TestCheckFileStillRejectsStrictCallingUntypedLegacy(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
func Chip(props any) Node {
	return <span class="chip">{props.label}</span>
}
component Page() {
	return <main><Chip label="hello"/></main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil {
		t.Fatal("CheckFile accepted a strict body calling an untyped legacy component")
	}
	if got := err.Error(); !strings.Contains(got, "strict component cannot call untyped legacy component Chip") {
		t.Errorf("want the lower-time category diagnostic, got: %v", got)
	}
	if got := err.Error(); strings.Contains(got, "undefined: Chip") {
		t.Errorf("untyped legacy leaked into the Go type check instead of failing at lower time: %v", got)
	}
}
