// Package engine provides the runtime for GoSX engine components.
//
// Engines are the second browser execution model alongside islands:
//
//	islands: constrained reactive DOM (shared WASM VM, tiny payloads)
//	engines: arbitrary client computation (dedicated WASM, full power)
//
// Three kinds:
//
//	worker:  background compute, no DOM (search, parsing, inference)
//	surface: owns a mount point — canvas, WebGL, container div
//	video:   framework-owned managed video mount
//
// Engines communicate through typed ports: props in, messages out.
// They do NOT touch island DOM or VM internals.
package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Kind identifies the engine execution model.
type Kind string

const (
	KindWorker  Kind = "worker"  // Background compute, no DOM access
	KindSurface Kind = "surface" // Owns a mount point (canvas, WebGL, div)
	KindVideo   Kind = "video"   // Framework-owned managed video mount
)

// Capability declares a browser API the engine requires.
type Capability string

const (
	CapVideo        Capability = "video"
	CapCanvas       Capability = "canvas"
	CapWebGL        Capability = "webgl"
	CapWebGL2       Capability = "webgl2"
	CapWebGPU       Capability = "webgpu"
	CapCompute      Capability = "compute"
	CapWASM         Capability = "wasm"
	CapPixelSurface Capability = "pixel-surface"
	CapAnimation    Capability = "animation"
	CapStorage      Capability = "storage"
	CapFetch        Capability = "fetch"
	CapAudio        Capability = "audio"
	CapWorker       Capability = "worker"
	CapGamepad      Capability = "gamepad"
	CapKeyboard     Capability = "keyboard"
	CapPointer      Capability = "pointer"
	CapPointerLock  Capability = "pointer-lock"
	CapTextInput    Capability = "text-input"

	CapWebGPUTimestampQuery                 Capability = "webgpu:timestamp-query"
	CapWebGPUIndirectFirstInstance          Capability = "webgpu:indirect-first-instance"
	CapWebGPUShaderF16                      Capability = "webgpu:shader-f16"
	CapWebGPUTextureCompressionBC           Capability = "webgpu:texture-compression-bc"
	CapWebGPUTextureCompressionETC2         Capability = "webgpu:texture-compression-etc2"
	CapWebGPUTextureCompressionASTC         Capability = "webgpu:texture-compression-astc"
	CapWebGPUSubgroups                      Capability = "webgpu:subgroups"
	CapWebGPUTextureCompressionBCSliced3D   Capability = "webgpu:texture-compression-bc-sliced-3d"
	CapWebGPUTextureCompressionASTCSliced3D Capability = "webgpu:texture-compression-astc-sliced-3d"
	CapWebGPUDepthClipControl               Capability = "webgpu:depth-clip-control"
	CapWebGPUDepth32FloatStencil8           Capability = "webgpu:depth32float-stencil8"
	CapWebGPUFloat32Filterable              Capability = "webgpu:float32-filterable"
	CapWebGPUFloat32Blendable               Capability = "webgpu:float32-blendable"
	CapWebGPURG11B10UFloatRenderable        Capability = "webgpu:rg11b10ufloat-renderable"
	CapWebGPUBGRA8UnormStorage              Capability = "webgpu:bgra8unorm-storage"
	CapWebGPUClipDistances                  Capability = "webgpu:clip-distances"
	CapWebGPUDualSourceBlending             Capability = "webgpu:dual-source-blending"
	CapWebGPUSubgroupsF16                   Capability = "webgpu:subgroups-f16"
)

// WebGPUFeature returns a normalized WebGPU optional-feature capability such as
// "webgpu:timestamp-query".
func WebGPUFeature(feature string) Capability {
	feature = strings.TrimSpace(strings.ToLower(feature))
	feature = strings.TrimPrefix(feature, "webgpu-feature:")
	feature = strings.TrimPrefix(feature, "webgpu:")
	if feature == "" {
		return CapWebGPU
	}
	return Capability("webgpu:" + feature)
}

// WebGPULimit requires a negotiated WebGPU device limit to be at least minimum.
func WebGPULimit(name string, minimum int) Capability {
	return webGPULimitCapability("webgpu:limit:", name, minimum)
}

// WebGPUDeviceLimit requires a negotiated WebGPU device limit to be at least minimum.
func WebGPUDeviceLimit(name string, minimum int) Capability {
	return webGPULimitCapability("webgpu:device-limit:", name, minimum)
}

// WebGPUAdapterLimit requires the probed WebGPU adapter ceiling to be at least minimum.
func WebGPUAdapterLimit(name string, minimum int) Capability {
	return webGPULimitCapability("webgpu:adapter-limit:", name, minimum)
}

// RequireWebGPU builds a hard-gate capability set that always includes webgpu.
func RequireWebGPU(capabilities ...Capability) []Capability {
	out := []Capability{CapWebGPU}
	seen := map[Capability]struct{}{CapWebGPU: {}}
	for _, capability := range capabilities {
		value := strings.TrimSpace(string(capability))
		normalized := Capability(strings.ToLower(value))
		if strings.HasPrefix(string(normalized), "webgpu-feature:") {
			capability = WebGPUFeature(value)
			value = string(capability)
			normalized = Capability(strings.ToLower(value))
		}
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, Capability(value))
	}
	return out
}

func webGPULimitCapability(prefix, name string, minimum int) Capability {
	name = webGPULimitName(name)
	return Capability(fmt.Sprintf("%s%s>=%d", prefix, name, minimum))
}

func webGPULimitName(name string) string {
	text := strings.TrimSpace(name)
	lower := strings.ToLower(text)
	for _, prefix := range []string{
		"webgpu:adapter-limit:",
		"webgpu:device-limit:",
		"webgpu:limit:",
		"webgpu-limit:",
	} {
		if strings.HasPrefix(lower, prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	if parsed, _, ok := splitWebGPULimitRequirement(text); ok {
		return parsed
	}
	return text
}

// KindNeedsMount reports whether an engine kind attaches to a DOM mount.
func KindNeedsMount(kind Kind) bool {
	return kind == KindSurface || kind == KindVideo
}

// KindSupported reports whether kind is one of the engine kinds understood by
// the compiler, renderer, and bootstrap manifest contract.
func KindSupported(kind Kind) bool {
	switch kind {
	case KindWorker, KindSurface, KindVideo:
		return true
	default:
		return false
	}
}

// ScalingMode controls how a logical pixel buffer maps to the display surface.
type ScalingMode string

const (
	// ScalePixelPerfect uses integer multiples only. The buffer is scaled to
	// the largest integer factor that fits the surface, with letterboxing on
	// the remaining area. No interpolation.
	ScalePixelPerfect ScalingMode = "pixel-perfect"

	// ScaleFill scales the buffer to fill the surface while preserving aspect
	// ratio. Uses linear interpolation.
	ScaleFill ScalingMode = "fill"

	// ScaleStretch scales the buffer to fill the surface without preserving
	// aspect ratio. Rarely what you want.
	ScaleStretch ScalingMode = "stretch"
)

// PixelSurfaceConfig describes a GPU-accelerated pixel framebuffer that the
// framework manages automatically. The engine writes RGBA bytes into a logical
// buffer; the runtime handles upload, scaling, and presentation.
//
// Inspired by github.com/parasyte/pixels — adapted for browser targets.
type PixelSurfaceConfig struct {
	// Width is the logical pixel buffer width.
	Width int `json:"width"`

	// Height is the logical pixel buffer height.
	Height int `json:"height"`

	// Scaling controls how the logical buffer maps to the display surface.
	// Defaults to ScalePixelPerfect.
	Scaling ScalingMode `json:"scaling,omitempty"`

	// ClearColor is the RGBA background color for letterbox regions.
	// Each component is 0-255.
	ClearColor [4]uint8 `json:"clearColor,omitempty"`

	// VSync enables vertical sync. Defaults to true.
	VSync *bool `json:"vsync,omitempty"`
}

// VSyncEnabled reports whether vsync is enabled, defaulting to true.
func (p PixelSurfaceConfig) VSyncEnabled() bool {
	if p.VSync == nil {
		return true
	}
	return *p.VSync
}

// Runtime identifies how an engine instance is driven on the client.
type Runtime string

const (
	RuntimeNone   Runtime = ""
	RuntimeShared Runtime = "shared"
)

// Config describes an engine instance for mounting.
type Config struct {
	// Name is the engine component name.
	Name string `json:"name"`

	// Kind is "worker", "surface", or "video".
	Kind Kind `json:"kind"`

	// WASMPath is the URL to the engine's WASM binary.
	WASMPath string `json:"wasmPath"`

	// MountID is the DOM element ID for mount-bearing engines (ignored for workers).
	MountID string `json:"mountId,omitempty"`

	// MountAttrs are applied to the server-rendered mount element for surface
	// engines. They are runtime-only and are not serialized into manifests.
	MountAttrs map[string]any `json:"-"`

	// Props is the initial props for the engine.
	Props json.RawMessage `json:"props,omitempty"`

	// Capabilities declares browser APIs the engine can use.
	Capabilities []Capability `json:"capabilities,omitempty"`

	// RequiredCapabilities hard-gates browser APIs before the runtime mounts
	// the engine. Missing requirements surface as an unsupported runtime issue
	// instead of a silent downgrade.
	RequiredCapabilities []Capability `json:"requiredCapabilities,omitempty"`

	// Runtime selects an optional shared GoSX client runtime for program-driven
	// engines. Empty means the engine is mounted entirely by its JS factory.
	Runtime Runtime `json:"runtime,omitempty"`

	// PixelSurface configures a managed pixel framebuffer when CapPixelSurface
	// is declared. The runtime creates a canvas at the logical resolution,
	// handles GPU-accelerated scaling to the mount element, and exposes the
	// raw RGBA buffer to the engine via __gosx_engine_frame(id).
	PixelSurface *PixelSurfaceConfig `json:"pixelSurface,omitempty"`
}

// Port is a typed message channel between an engine and the host.
type Port struct {
	// Name identifies the port.
	Name string

	// Direction: "in" (host→engine) or "out" (engine→host).
	Direction string

	// Handler is called when a message arrives on this port.
	Handler func(data json.RawMessage)
}

// MessageBus connects engines to islands and the server via typed messages.
type MessageBus struct {
	handlers map[string][]func(json.RawMessage)
}

// NewMessageBus creates a message bus for engine communication.
func NewMessageBus() *MessageBus {
	return &MessageBus{
		handlers: make(map[string][]func(json.RawMessage)),
	}
}

// On registers a handler for a message type.
func (mb *MessageBus) On(event string, handler func(json.RawMessage)) {
	mb.handlers[event] = append(mb.handlers[event], handler)
}

// Emit sends a message to all handlers registered for the event.
func (mb *MessageBus) Emit(event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	for _, h := range mb.handlers[event] {
		h(raw)
	}
}

// Validate checks that the engine config is safe and well-formed.
func (c Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("engine config requires a Name")
	}
	if c.Kind == "" {
		return fmt.Errorf("engine config requires a Kind")
	}
	if !KindSupported(c.Kind) {
		return fmt.Errorf("unsupported engine kind: %q", c.Kind)
	}
	if KindNeedsMount(c.Kind) && c.MountID == "" {
		return fmt.Errorf("engine kind %q requires a MountID", c.Kind)
	}
	if err := ValidateCapabilities(c.Capabilities); err != nil {
		return err
	}
	return ValidateCapabilities(c.RequiredCapabilities)
}

// Factory is a function that creates an engine instance.
type Factory func() any

var (
	factoryMu sync.RWMutex
	factories = make(map[string]Factory)
)

// RegisterFactory registers an engine factory by name at init time.
func RegisterFactory(name string, factory Factory) error {
	if name == "" {
		return fmt.Errorf("engine factory name is required")
	}
	if factory == nil {
		return fmt.Errorf("engine factory %q is nil", name)
	}
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, exists := factories[name]; exists {
		return fmt.Errorf("engine factory %q already registered", name)
	}
	factories[name] = factory
	return nil
}

// HasFactory reports whether a factory is registered under the given name.
func HasFactory(name string) bool {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	_, ok := factories[name]
	return ok
}

// ClearFactories removes all registered factories (test use only).
func ClearFactories() {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	factories = make(map[string]Factory)
}

// ValidateCapabilities checks if the requested capabilities are supported.
func ValidateCapabilities(requested []Capability) error {
	supported := map[Capability]bool{
		CapVideo: true, CapCanvas: true, CapWebGL: true, CapWebGL2: true, CapWebGPU: true, CapCompute: true, CapWASM: true, CapPixelSurface: true,
		CapAnimation: true, CapStorage: true, CapFetch: true, CapAudio: true,
		CapWorker: true, CapGamepad: true, CapKeyboard: true, CapPointer: true, CapPointerLock: true, CapTextInput: true,
	}
	for _, cap := range requested {
		normalized := Capability(strings.ToLower(strings.TrimSpace(string(cap))))
		if supported[normalized] || webGPUCapabilitySupported(normalized) {
			continue
		}
		return fmt.Errorf("unsupported engine capability: %q", cap)
	}
	return nil
}

func webGPUCapabilitySupported(cap Capability) bool {
	if webGPULimitCapabilitySupported(cap) {
		return true
	}
	switch cap {
	case CapWebGPUTimestampQuery,
		CapWebGPUIndirectFirstInstance,
		CapWebGPUShaderF16,
		CapWebGPUTextureCompressionBC,
		CapWebGPUTextureCompressionETC2,
		CapWebGPUTextureCompressionASTC,
		CapWebGPUSubgroups,
		CapWebGPUTextureCompressionBCSliced3D,
		CapWebGPUTextureCompressionASTCSliced3D,
		CapWebGPUDepthClipControl,
		CapWebGPUDepth32FloatStencil8,
		CapWebGPUFloat32Filterable,
		CapWebGPUFloat32Blendable,
		CapWebGPURG11B10UFloatRenderable,
		CapWebGPUBGRA8UnormStorage,
		CapWebGPUClipDistances,
		CapWebGPUDualSourceBlending,
		CapWebGPUSubgroupsF16:
		return true
	default:
		return false
	}
}

func webGPULimitCapabilitySupported(cap Capability) bool {
	value := string(cap)
	for _, prefix := range []string{
		"webgpu:limit:",
		"webgpu:device-limit:",
		"webgpu:adapter-limit:",
		"webgpu-limit:",
	} {
		if strings.HasPrefix(value, prefix) {
			return validWebGPULimitRequirement(strings.TrimPrefix(value, prefix))
		}
	}
	return false
}

func validWebGPULimitRequirement(requirement string) bool {
	name, rest, ok := splitWebGPULimitRequirement(requirement)
	if !ok || name == "" || rest == "" {
		return false
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' {
			continue
		}
		return false
	}
	hasDigit := false
	for _, ch := range rest {
		if (ch >= '0' && ch <= '9') || ch == '.' {
			if ch >= '0' && ch <= '9' {
				hasDigit = true
			}
			continue
		}
		return false
	}
	return hasDigit && strings.Count(rest, ".") <= 1
}

func splitWebGPULimitRequirement(requirement string) (name, value string, ok bool) {
	for _, op := range []string{">=", "<=", "==", ">", "<", "=", ":"} {
		if idx := strings.Index(requirement, op); idx >= 0 {
			return strings.TrimSpace(requirement[:idx]), strings.TrimSpace(requirement[idx+len(op):]), true
		}
	}
	return "", "", false
}
