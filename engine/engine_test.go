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

func TestValidateCapabilities_Clipboard(t *testing.T) {
	if err := ValidateCapabilities([]Capability{CapClipboard}); err != nil {
		t.Fatalf("CapClipboard should be valid, got: %v", err)
	}
}

func TestValidateCapabilities(t *testing.T) {
	// Valid
	err := ValidateCapabilities([]Capability{CapVideo, CapCanvas, CapWebGL, CapWebGL2, CapWebGPU, CapCompute, CapWASM, CapPixelSurface, CapPointer, CapPointerLock, CapKeyboard, CapGamepad, CapWebGPUTimestampQuery, CapWebGPUShaderF16, CapWebGPUTextureCompressionBC, CapWebGPUTextureCompressionBCSliced3D, CapWebGPUDualSourceBlending, CapWebGPUSubgroupsF16, "webgpu:limit:maxTextureDimension2D>=4096", "webgpu:adapter-limit:maxBufferSize>=1048576", "webgpu-feature:future-rendering-mode"})
	if err != nil {
		t.Fatal(err)
	}

	// Invalid
	err = ValidateCapabilities([]Capability{"teleport"})
	if err == nil {
		t.Fatal("expected error for unsupported capability")
	}
	err = ValidateCapabilities([]Capability{"webgpu:bad_feature"})
	if err == nil {
		t.Fatal("expected error for malformed WebGPU feature capability")
	}
}

func TestWebGPUCapabilityHelpers(t *testing.T) {
	required := RequireWebGPU(
		WebGPUFeature("timestamp-query"),
		WebGPUFeature("webgpu-feature:shader-f16"),
		WebGPULimit("maxTextureDimension2D", 4096),
		WebGPUAdapterLimit("webgpu:adapter-limit:maxBufferSize>=1048576", 1048576),
		CapWebGPU,
	)
	want := []Capability{
		CapWebGPU,
		CapWebGPUTimestampQuery,
		CapWebGPUShaderF16,
		"webgpu:limit:maxTextureDimension2D>=4096",
		"webgpu:adapter-limit:maxBufferSize>=1048576",
	}
	if len(required) != len(want) {
		t.Fatalf("expected %d required capabilities, got %#v", len(want), required)
	}
	for i := range want {
		if required[i] != want[i] {
			t.Fatalf("unexpected required capability %d: got %q want %q", i, required[i], want[i])
		}
	}
	if err := ValidateCapabilities(required); err != nil {
		t.Fatalf("expected helper output to validate: %v", err)
	}
	if err := ValidateCapabilities([]Capability{WebGPUDeviceLimit("", 1)}); err == nil {
		t.Fatal("expected empty WebGPU limit helper input to remain invalid")
	}
}

func TestRegisterFactory(t *testing.T) {
	ClearFactories()
	if err := RegisterFactory("test-surface", func() any { return nil }); err != nil {
		t.Fatal(err)
	}
	if !HasFactory("test-surface") {
		t.Fatal("factory not registered")
	}
}

func TestRegisterFactory_RejectsDuplicate(t *testing.T) {
	ClearFactories()
	if err := RegisterFactory("dup", func() any { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := RegisterFactory("dup", func() any { return nil }); err == nil {
		t.Fatal("expected duplicate registration to fail")
	}
}

func TestRegisterFactory_RejectsInvalidInput(t *testing.T) {
	ClearFactories()
	if err := RegisterFactory("", func() any { return nil }); err == nil {
		t.Fatal("expected empty factory name to fail")
	}
	if err := RegisterFactory("nil-factory", nil); err == nil {
		t.Fatal("expected nil factory to fail")
	}
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

	goWASM := Config{
		Name:     "GoWASM",
		Kind:     KindWorker,
		Runtime:  RuntimeGoWASM,
		WASMPath: "/assets/engine.wasm",
	}
	if err := goWASM.Validate(); err != nil {
		t.Fatalf("expected valid Go-WASM config, got %v", err)
	}
	goWASM.WASMPath = ""
	if err := goWASM.Validate(); err == nil {
		t.Fatal("expected Go-WASM runtime without WASMPath to fail")
	}

	unsupportedRuntime := Config{
		Name:    "BadRuntime",
		Kind:    KindWorker,
		Runtime: Runtime("javascript-eval"),
	}
	if err := unsupportedRuntime.Validate(); err == nil {
		t.Fatal("expected unsupported runtime to fail")
	}
}
