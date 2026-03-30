package engine

import (
	"encoding/json"
	"testing"
)

func TestEngineConfig(t *testing.T) {
	cfg := Config{
		Name:         "Whiteboard",
		Kind:         KindSurface,
		WASMPath:     "/assets/engines/Whiteboard.abc123.wasm",
		JSPath:       "/assets/engines/Whiteboard.js",
		JSExport:     "Whiteboard",
		MountID:      "canvas-root",
		Capabilities: []Capability{CapCanvas, CapAnimation},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Config
	json.Unmarshal(data, &decoded)
	if decoded.Name != "Whiteboard" {
		t.Fatal("wrong name")
	}
	if decoded.Kind != KindSurface {
		t.Fatal("wrong kind")
	}
	if decoded.JSPath != "/assets/engines/Whiteboard.js" {
		t.Fatal("wrong js path")
	}
	if decoded.JSExport != "Whiteboard" {
		t.Fatal("wrong js export")
	}
	if len(decoded.Capabilities) != 2 {
		t.Fatal("wrong capabilities count")
	}
}

func TestEngineWorkerConfig(t *testing.T) {
	cfg := Config{
		Name:     "SearchIndexer",
		Kind:     KindWorker,
		WASMPath: "/assets/engines/SearchIndexer.wasm",
	}
	if cfg.Kind != KindWorker {
		t.Fatal("expected worker")
	}
	if cfg.MountID != "" {
		t.Fatal("worker should have no mount")
	}
}

func TestEngineVideoConfigNeedsMount(t *testing.T) {
	cfg := Config{
		Name:     "Player",
		Kind:     KindVideo,
		MountID:  "player-root",
		WASMPath: "",
	}
	if cfg.Kind != KindVideo {
		t.Fatal("expected video")
	}
	if cfg.MountID == "" {
		t.Fatal("video should keep mount id")
	}
	if !KindNeedsMount(cfg.Kind) {
		t.Fatal("video should need a mount")
	}
	if KindNeedsMount(KindWorker) {
		t.Fatal("worker should not need a mount")
	}
}

func TestMessageBus(t *testing.T) {
	bus := NewMessageBus()

	var received string
	bus.On("update", func(data json.RawMessage) {
		json.Unmarshal(data, &received)
	})

	bus.Emit("update", "hello from engine")

	if received != "hello from engine" {
		t.Fatalf("expected 'hello from engine', got %q", received)
	}
}

func TestMessageBusMultipleHandlers(t *testing.T) {
	bus := NewMessageBus()
	count := 0

	bus.On("tick", func(data json.RawMessage) { count++ })
	bus.On("tick", func(data json.RawMessage) { count++ })

	bus.Emit("tick", nil)
	if count != 2 {
		t.Fatalf("expected 2 handler calls, got %d", count)
	}
}

func TestValidateCapabilities(t *testing.T) {
	// Valid
	err := ValidateCapabilities([]Capability{CapVideo, CapCanvas, CapWebGL, CapWebGPU, CapPixelSurface, CapPointer, CapKeyboard, CapGamepad})
	if err != nil {
		t.Fatal(err)
	}

	// Invalid
	err = ValidateCapabilities([]Capability{"teleport"})
	if err == nil {
		t.Fatal("expected error for unsupported capability")
	}
}
