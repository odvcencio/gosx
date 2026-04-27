package hydrate

import (
	"encoding/json"
	"testing"

	"github.com/odvcencio/gosx/engine"
)

func TestManifestCreation(t *testing.T) {
	m := NewManifest()
	if m.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %s", m.Version)
	}
	if len(m.Islands) != 0 {
		t.Errorf("expected 0 islands, got %d", len(m.Islands))
	}
}

func TestManifestAddIsland(t *testing.T) {
	m := NewManifest()
	m.Bundles["main"] = BundleRef{Path: "/app.wasm"}

	type props struct {
		Count int `json:"count"`
	}

	id, err := m.AddIsland("Counter", "main", props{Count: 42})
	if err != nil {
		t.Fatalf("AddIsland failed: %v", err)
	}
	if id != "gosx-island-0" {
		t.Errorf("expected gosx-island-0, got %s", id)
	}
	if len(m.Islands) != 1 {
		t.Fatalf("expected 1 island, got %d", len(m.Islands))
	}

	entry := m.Islands[0]
	if entry.Component != "Counter" {
		t.Errorf("expected Counter, got %s", entry.Component)
	}
	if entry.BundleID != "main" {
		t.Errorf("expected main, got %s", entry.BundleID)
	}

	var p props
	if err := json.Unmarshal(entry.Props, &p); err != nil {
		t.Fatalf("unmarshal props: %v", err)
	}
	if p.Count != 42 {
		t.Errorf("expected count 42, got %d", p.Count)
	}
}

func TestManifestAddComputeIsland(t *testing.T) {
	m := NewManifest()
	id, err := m.AddComputeIsland(
		"FightController",
		"main",
		map[string]string{"match": "abc"},
		[]string{"keyboard", "gamepad"},
		[]string{"wasm"},
	)
	if err != nil {
		t.Fatalf("AddComputeIsland failed: %v", err)
	}
	if id != "gosx-compute-0" {
		t.Fatalf("expected gosx-compute-0, got %s", id)
	}
	if len(m.ComputeIslands) != 1 {
		t.Fatalf("expected 1 compute island, got %d", len(m.ComputeIslands))
	}
	entry := m.ComputeIslands[0]
	if entry.Component != "FightController" {
		t.Fatalf("unexpected component: %s", entry.Component)
	}
	if entry.Capabilities[1] != "gamepad" {
		t.Fatalf("unexpected capabilities: %#v", entry.Capabilities)
	}
	if entry.RequiredCapabilities[0] != "wasm" {
		t.Fatalf("unexpected required capabilities: %#v", entry.RequiredCapabilities)
	}
	var props map[string]string
	if err := json.Unmarshal(entry.Props, &props); err != nil {
		t.Fatalf("unmarshal props: %v", err)
	}
	if props["match"] != "abc" {
		t.Fatalf("unexpected props: %#v", props)
	}
}

func TestManifestMarshal(t *testing.T) {
	m := NewManifest()
	m.Bundles["main"] = BundleRef{Path: "/app.wasm", Hash: "abc123"}
	m.AddIsland("Counter", "main", map[string]int{"initial": 0})

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	m2, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m2.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %s", m2.Version)
	}
	if len(m2.Islands) != 1 {
		t.Errorf("expected 1 island, got %d", len(m2.Islands))
	}
	if m2.Bundles["main"].Hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", m2.Bundles["main"].Hash)
	}
}

func TestMultipleIslands(t *testing.T) {
	m := NewManifest()
	m.Bundles["main"] = BundleRef{Path: "/app.wasm"}

	m.AddIsland("Counter", "main", nil)
	m.AddIsland("FilterBar", "main", nil)
	m.AddIsland("Chart", "main", nil)

	if len(m.Islands) != 3 {
		t.Fatalf("expected 3 islands, got %d", len(m.Islands))
	}
	if m.Islands[0].ID != "gosx-island-0" {
		t.Errorf("expected gosx-island-0, got %s", m.Islands[0].ID)
	}
	if m.Islands[1].ID != "gosx-island-1" {
		t.Errorf("expected gosx-island-1, got %s", m.Islands[1].ID)
	}
	if m.Islands[2].ID != "gosx-island-2" {
		t.Errorf("expected gosx-island-2, got %s", m.Islands[2].ID)
	}
}

func TestManifestRuntimeRef(t *testing.T) {
	m := NewManifest()
	m.Runtime = RuntimeRef{
		Path: "/gosx/runtime.wasm",
		Hash: "abc123",
		Size: 2500000,
	}
	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Runtime.Path != "/gosx/runtime.wasm" {
		t.Fatalf("runtime path: expected /gosx/runtime.wasm, got %s", decoded.Runtime.Path)
	}
	if decoded.Runtime.Hash != "abc123" {
		t.Fatalf("runtime hash: expected abc123, got %s", decoded.Runtime.Hash)
	}
	if decoded.Runtime.Size != 2500000 {
		t.Fatalf("runtime size: expected 2500000, got %d", decoded.Runtime.Size)
	}
}

func TestIslandProgramRef(t *testing.T) {
	m := NewManifest()
	m.Bundles["main"] = BundleRef{Path: "/gosx/runtime.wasm"}
	id, err := m.AddIsland("Counter", "main", map[string]int{"initial": 0})
	if err != nil {
		t.Fatalf("add island: %v", err)
	}
	// Set program ref on the island
	for i := range m.Islands {
		if m.Islands[i].ID == id {
			m.Islands[i].ProgramRef = "/gosx/islands/Counter.json"
			m.Islands[i].ProgramFormat = "json"
			m.Islands[i].ProgramHash = "def456"
		}
	}
	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Islands) != 1 {
		t.Fatalf("expected 1 island, got %d", len(decoded.Islands))
	}
	island := decoded.Islands[0]
	if island.ProgramRef != "/gosx/islands/Counter.json" {
		t.Fatalf("program ref: expected /gosx/islands/Counter.json, got %s", island.ProgramRef)
	}
	if island.ProgramFormat != "json" {
		t.Fatalf("format: expected json, got %s", island.ProgramFormat)
	}
	if island.ProgramHash != "def456" {
		t.Fatalf("hash: expected def456, got %s", island.ProgramHash)
	}
}

func TestManifestAddHub(t *testing.T) {
	m := NewManifest()
	id := m.AddHub("presence", "/gosx/hub/presence", []HubBinding{
		{Event: "snapshot", Signal: "$presence"},
		{Event: "memberJoined", Signal: "$presence"},
	})

	if id != "gosx-hub-0" {
		t.Fatalf("expected gosx-hub-0, got %s", id)
	}
	if len(m.Hubs) != 1 {
		t.Fatalf("expected 1 hub, got %d", len(m.Hubs))
	}
	if m.Hubs[0].Bindings[0].Signal != "$presence" {
		t.Fatalf("unexpected binding %#v", m.Hubs[0].Bindings[0])
	}
}

func TestManifestAddEngineWithPixelSurface(t *testing.T) {
	m := NewManifest()
	vsync := false

	id, err := m.AddEngineWithRuntime(
		"RetroScreen",
		"surface",
		"/gosx/engines/retro.wasm",
		"retro-root",
		"",
		map[string]any{"palette": "amber"},
		[]string{"pixel-surface", "canvas"},
		&engine.PixelSurfaceConfig{
			Width:      160,
			Height:     144,
			Scaling:    engine.ScaleFill,
			ClearColor: [4]uint8{1, 2, 3, 255},
			VSync:      &vsync,
		},
	)
	if err != nil {
		t.Fatalf("AddEngineWithRuntime failed: %v", err)
	}
	if id != "gosx-engine-0" {
		t.Fatalf("expected gosx-engine-0, got %s", id)
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Engines) != 1 {
		t.Fatalf("expected 1 engine, got %d", len(decoded.Engines))
	}
	entry := decoded.Engines[0]
	if entry.PixelSurface == nil {
		t.Fatal("expected pixel surface config")
	}
	if entry.PixelSurface.Width != 160 || entry.PixelSurface.Height != 144 {
		t.Fatalf("unexpected pixel surface size: %#v", entry.PixelSurface)
	}
	if entry.PixelSurface.Scaling != engine.ScaleFill {
		t.Fatalf("unexpected scaling: %q", entry.PixelSurface.Scaling)
	}
	if entry.PixelSurface.ClearColor != [4]uint8{1, 2, 3, 255} {
		t.Fatalf("unexpected clear color: %v", entry.PixelSurface.ClearColor)
	}
	if entry.PixelSurface.VSyncEnabled() {
		t.Fatal("expected vsync disabled")
	}
}

func TestManifestAddEngineWithRequiredCapabilities(t *testing.T) {
	m := NewManifest()

	id, err := m.AddEngineWithRuntimeRequirements(
		"StrictScene",
		"surface",
		"/gosx/engines/strict.wasm",
		"strict-root",
		"shared",
		map[string]any{"mode": "pbr"},
		[]string{"canvas", "webgl"},
		[]string{"wasm", "webgl"},
		nil,
	)
	if err != nil {
		t.Fatalf("AddEngineWithRuntimeRequirements failed: %v", err)
	}
	if id != "gosx-engine-0" {
		t.Fatalf("expected gosx-engine-0, got %s", id)
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := decoded.Engines[0].RequiredCapabilities; len(got) != 2 || got[0] != "wasm" || got[1] != "webgl" {
		t.Fatalf("unexpected required capabilities: %#v", got)
	}
}
