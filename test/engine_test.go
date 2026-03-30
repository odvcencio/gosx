package test

import (
	"testing"

	"github.com/odvcencio/gosx/hydrate"
)

func TestEngineDirectiveWorker(t *testing.T) {
	source := []byte(`package main

//gosx:engine worker
func SearchIndexer(props IndexerProps) Engine {
	return <div>indexer</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if !comp.IsEngine {
		t.Fatal("expected IsEngine=true")
	}
	if comp.EngineKind != "worker" {
		t.Fatalf("expected kind 'worker', got %q", comp.EngineKind)
	}
	if comp.IsIsland {
		t.Fatal("engine should not be marked as island")
	}
	t.Logf("Engine: %s, kind=%s", comp.Name, comp.EngineKind)
}

func TestEngineDirectiveSurface(t *testing.T) {
	source := []byte(`package main

//gosx:engine surface
//gosx:capabilities canvas animation webgl
func Whiteboard(props BoardProps) Engine {
	return <div>canvas</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if !comp.IsEngine {
		t.Fatal("expected IsEngine=true")
	}
	if comp.EngineKind != "surface" {
		t.Fatalf("expected kind 'surface', got %q", comp.EngineKind)
	}
	if len(comp.EngineCapabilities) != 3 {
		t.Fatalf("expected 3 capabilities, got %d: %v", len(comp.EngineCapabilities), comp.EngineCapabilities)
	}
	if comp.EngineCapabilities[0] != "canvas" {
		t.Fatalf("expected first capability 'canvas', got %q", comp.EngineCapabilities[0])
	}
	t.Logf("Engine: %s, kind=%s, capabilities=%v", comp.Name, comp.EngineKind, comp.EngineCapabilities)
}

func TestEngineDirectiveVideo(t *testing.T) {
	source := []byte(`package main

//gosx:engine video
func Player(props PlayerProps) Engine {
	return <div>video</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if !comp.IsEngine {
		t.Fatal("expected IsEngine=true")
	}
	if comp.EngineKind != "video" {
		t.Fatalf("expected kind 'video', got %q", comp.EngineKind)
	}
	if len(comp.EngineCapabilities) != 3 {
		t.Fatalf("expected 3 auto capabilities, got %d: %v", len(comp.EngineCapabilities), comp.EngineCapabilities)
	}
	if comp.EngineCapabilities[0] != "video" || comp.EngineCapabilities[1] != "fetch" || comp.EngineCapabilities[2] != "audio" {
		t.Fatalf("unexpected video capabilities: %v", comp.EngineCapabilities)
	}
}

func TestEngineManifest(t *testing.T) {
	// Test that engines appear in the manifest
	m := newTestManifest()
	id, err := m.AddEngine("Whiteboard", "surface", "/assets/engines/Whiteboard.abc123.wasm", nil, []string{"canvas", "animation"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "gosx-engine-0" {
		t.Fatalf("expected gosx-engine-0, got %s", id)
	}
	if len(m.Engines) != 1 {
		t.Fatal("expected 1 engine")
	}
	if m.Engines[0].Kind != "surface" {
		t.Fatalf("expected surface, got %s", m.Engines[0].Kind)
	}

	// Marshal and verify
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Manifest with engine: %s", data)
}

func newTestManifest() *hydrate.Manifest {
	return hydrate.NewManifest()
}
