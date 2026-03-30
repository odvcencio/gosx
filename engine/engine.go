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
	CapWebGPU       Capability = "webgpu"
	CapPixelSurface Capability = "pixel-surface"
	CapAnimation    Capability = "animation"
	CapStorage      Capability = "storage"
	CapFetch        Capability = "fetch"
	CapAudio        Capability = "audio"
	CapWorker       Capability = "worker"
	CapGamepad      Capability = "gamepad"
	CapKeyboard     Capability = "keyboard"
	CapPointer      Capability = "pointer"
)

// KindNeedsMount reports whether an engine kind attaches to a DOM mount.
func KindNeedsMount(kind Kind) bool {
	return kind == KindSurface || kind == KindVideo
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

	// JSPath is an optional JS entrypoint for engines that opt into the
	// unrestricted client runtime.
	JSPath string `json:"jsPath,omitempty"`

	// JSExport is the factory name published in window.__gosx_engine_factories.
	JSExport string `json:"jsExport,omitempty"`

	// MountID is the DOM element ID for mount-bearing engines (ignored for workers).
	MountID string `json:"mountId,omitempty"`

	// MountAttrs are applied to the server-rendered mount element for surface
	// engines. They are runtime-only and are not serialized into manifests.
	MountAttrs map[string]any `json:"-"`

	// Props is the initial props for the engine.
	Props json.RawMessage `json:"props,omitempty"`

	// Capabilities lists required browser APIs.
	Capabilities []Capability `json:"capabilities,omitempty"`

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

// ValidateCapabilities checks if the requested capabilities are supported.
func ValidateCapabilities(requested []Capability) error {
	supported := map[Capability]bool{
		CapVideo: true, CapCanvas: true, CapWebGL: true, CapWebGPU: true, CapPixelSurface: true,
		CapAnimation: true, CapStorage: true, CapFetch: true, CapAudio: true,
		CapWorker: true, CapGamepad: true, CapKeyboard: true, CapPointer: true,
	}
	for _, cap := range requested {
		if !supported[cap] {
			return fmt.Errorf("unsupported engine capability: %q", cap)
		}
	}
	return nil
}
