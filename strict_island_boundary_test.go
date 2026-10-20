package gosx_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/route"
)

// TestStrictIslandPropsBoundaryFailsClosed proves a strict island is held to
// the identical field-coverage and leaf-type proof as an ordinary strict
// component: a caller-side type mismatch must be rejected at render time,
// not silently coerced or passed through.
func TestStrictIslandPropsBoundaryFailsClosed(t *testing.T) {
	src := []byte(`package app

type BadgeProps struct {
	Count int
}

//gosx:island
component Badge(props: BadgeProps) {
	return <span>{props.Count}</span>
}

component Page() {
	return <main><Badge count={"not-a-number"} /></main>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	renderer := island.NewRenderer("test-bundle")
	_, err = route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		RenderIsland: renderer.RenderIslandFromProgram,
	})
	if err == nil {
		t.Fatalf("expected a boundary error for a wrong-typed prop, got nil")
	}
	if !strings.Contains(err.Error(), "Badge") {
		t.Fatalf("error = %v, want it to name the strict island Badge", err)
	}
	t.Logf("got expected boundary error: %v", err)
}
