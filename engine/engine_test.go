package engine

import (
	"encoding/json"
	"testing"
)

func TestEngineConfig(t *testing.T) {
	cfg := Config{
		Name:                 "Whiteboard",
		Kind:                 KindSurface,
		WASMPath:             "/assets/engines/Whiteboard.abc123.wasm",
		MountID:              "canvas-root",
		Capabilities:         []Capability{CapCanvas, CapAnimation},
		RequiredCapabilities: []Capability{CapWASM, CapCanvas},
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
	if len(decoded.Capabilities) != 2 {
		t.Fatal("wrong capabilities count")
	}
	if len(decoded.RequiredCapabilities) != 2 {
		t.Fatal("wrong required capabilities count")
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

func TestValidateCapabilities_TextInput(t *testing.T) {
	err := ValidateCapabilities([]Capability{CapTextInput})
	if err != nil {
		t.Fatalf("CapTextInput should be valid, got: %v", err)
	}
}

func TestValidateCapabilities(t *testing.T) {
	// Valid
	err := ValidateCapabilities([]Capability{CapVideo, CapCanvas, CapWebGL, CapWebGL2, CapWebGPU, CapCompute, CapWASM, CapPixelSurface, CapPointer, CapKeyboard, CapGamepad})
	if err != nil {
		t.Fatal(err)
	}

	// Invalid
	err = ValidateCapabilities([]Capability{"teleport"})
	if err == nil {
		t.Fatal("expected error for unsupported capability")
	}
}

func TestRegisterFactory(t *testing.T) {
	ClearFactories()
	RegisterFactory("test-surface", func() any { return nil })
	if !HasFactory("test-surface") {
		t.Fatal("factory not registered")
	}
}

func TestRegisterFactory_RejectsDuplicate(t *testing.T) {
	ClearFactories()
	RegisterFactory("dup", func() any { return nil })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	RegisterFactory("dup", func() any { return nil })
}

func TestEngineConfig_Validate(t *testing.T) {
	// Valid surface config passes.
	cfg := Config{
		Name:     "ValidEngine",
		Kind:     KindSurface,
		WASMPath: "/assets/engines/Valid.wasm",
		MountID:  "root",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}

	// Surface engine without MountID fails.
	noMount := Config{
		Name:     "BadEngine",
		Kind:     KindSurface,
		WASMPath: "/assets/engines/Bad.wasm",
	}
	if err := noMount.Validate(); err == nil {
		t.Fatal("expected error for missing MountID on surface engine, got nil")
	}

	// Missing Name fails.
	noName := Config{
		Kind:    KindWorker,
		MountID: "",
	}
	if err := noName.Validate(); err == nil {
		t.Fatal("expected error for missing Name, got nil")
	}

	badKind := Config{
		Name: "UnknownEngine",
		Kind: Kind("teleport"),
	}
	if err := badKind.Validate(); err == nil {
		t.Fatal("expected error for unsupported engine kind, got nil")
	}
}
