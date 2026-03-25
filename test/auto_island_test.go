package test

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island"
	"github.com/odvcencio/gosx/island/program"
)

// TestAutoIslandFromGSX proves the fully automatic island path:
// .gsx source → compile → lower → RenderIslandFromProgram → HTML with auto wiring
// Zero manual wiring. Zero gosx.El(). Zero EventSlot declarations.
func TestAutoIslandFromGSX(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	decrement := func() { count.Set(count.Get() - 1) }
	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span class="count">{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}
`)
	// 1. Compile
	irProg, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// 2. Lower to IslandProgram
	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
		}
	}
	if islandProg == nil {
		t.Fatal("no island found")
	}

	// 3. Render via RenderIslandFromProgram — fully automatic
	renderer := island.NewRenderer("test")
	renderer.SetBundle("test", "/gosx/runtime.wasm")
	renderer.SetRuntime("/gosx/runtime.wasm", "", 0)
	renderer.SetProgramDir("/gosx/islands")

	node := renderer.RenderIslandFromProgram(islandProg, nil)
	html := gosx.RenderHTML(node)

	t.Logf("Auto-rendered HTML:\n%s", html)

	// Verify: island wrapper
	if !strings.Contains(html, `data-gosx-island="Counter"`) {
		t.Fatal("missing data-gosx-island attribute")
	}

	// Verify: event delegation attributes
	if !strings.Contains(html, `data-gosx-handler="decrement"`) {
		t.Fatal("missing data-gosx-handler for decrement")
	}
	if !strings.Contains(html, `data-gosx-handler="increment"`) {
		t.Fatal("missing data-gosx-handler for increment")
	}
	if !strings.Contains(html, `data-gosx-on-click="decrement"`) {
		t.Fatal("missing data-gosx-on-click for decrement")
	}
	if !strings.Contains(html, `data-gosx-on-click="increment"`) {
		t.Fatal("missing data-gosx-on-click for increment")
	}
	if !strings.Contains(html, `data-gosx-path="`) {
		t.Fatal("missing stable event target path")
	}

	// Verify: static content rendered
	if !strings.Contains(html, `class="counter"`) {
		t.Fatal("missing counter class")
	}
	if !strings.Contains(html, ">-</button>") {
		t.Fatal("missing - button text")
	}
	if !strings.Contains(html, ">+</button>") {
		t.Fatal("missing + button text")
	}

	// Verify: manifest has event slots
	manifest := renderer.Manifest()
	if len(manifest.Islands) != 1 {
		t.Fatalf("expected 1 island, got %d", len(manifest.Islands))
	}
	entry := manifest.Islands[0]
	if len(entry.Events) == 0 {
		t.Fatal("expected auto-extracted event slots")
	}

	// Check event slot details
	foundDec := false
	foundInc := false
	for _, ev := range entry.Events {
		if ev.HandlerName == "decrement" && ev.EventType == "click" {
			foundDec = true
		}
		if ev.HandlerName == "increment" && ev.EventType == "click" {
			foundInc = true
		}
	}
	if !foundDec {
		t.Fatal("missing decrement click event slot")
	}
	if !foundInc {
		t.Fatal("missing increment click event slot")
	}

	// Verify: programRef set
	if entry.ProgramRef == "" {
		t.Fatal("missing programRef")
	}
	if !strings.Contains(entry.ProgramRef, "Counter") {
		t.Fatalf("programRef should reference Counter, got %s", entry.ProgramRef)
	}

	t.Logf("Events: %+v", entry.Events)
	t.Logf("ProgramRef: %s", entry.ProgramRef)
	t.Log("SUCCESS: .gsx → compile → lower → RenderIslandFromProgram → HTML with auto events")
}

func TestAutoIslandSupportsNonClickEvents(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Form() Node {
	name := signal.New("")
	updateName := func() { name.Set("typing") }
	submit := func() { name.Set("submitted") }
	return <form onSubmit={submit}>
		<div onInput={updateName}>type here</div>
		<p>{name.Get()}</p>
	</form>
}
`)

	irProg, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
		}
	}
	if islandProg == nil {
		t.Fatal("no island found")
	}

	renderer := island.NewRenderer("test")
	renderer.SetProgramDir("/gosx/islands")
	node := renderer.RenderIslandFromProgram(islandProg, nil)
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, `data-gosx-on-submit="submit"`) {
		t.Fatal("missing data-gosx-on-submit")
	}
	if !strings.Contains(html, `data-gosx-on-input="updateName"`) {
		t.Fatal("missing data-gosx-on-input")
	}
	if strings.Contains(html, `data-gosx-handler="updateName"`) {
		t.Fatal("non-click handlers should not use the click shorthand attribute")
	}

	entry := renderer.Manifest().Islands[0]
	foundInput := false
	foundSubmit := false
	for _, ev := range entry.Events {
		if ev.HandlerName == "updateName" && ev.EventType == "input" && ev.TargetSelector != "" {
			foundInput = true
		}
		if ev.HandlerName == "submit" && ev.EventType == "submit" && ev.TargetSelector != "" {
			foundSubmit = true
		}
	}
	if !foundInput {
		t.Fatal("missing input event slot with selector")
	}
	if !foundSubmit {
		t.Fatal("missing submit event slot with selector")
	}
}
