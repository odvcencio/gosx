// Package engine provides the runtime for GoSX engine components.
//
// Engines are the second browser execution model alongside islands:
//
//	islands: constrained reactive DOM (shared WASM VM, tiny payloads)
//	engines: arbitrary client computation (dedicated WASM, full power)
//
// Two kinds:
//
//	worker:  background compute, no DOM (search, parsing, inference)
//	surface: owns a mount point — canvas, WebGL, container div
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
)

// Capability declares a browser API the engine requires.
type Capability string

const (
	CapCanvas    Capability = "canvas"
	CapWebGL     Capability = "webgl"
	CapAnimation Capability = "animation"
	CapStorage   Capability = "storage"
	CapFetch     Capability = "fetch"
	CapAudio     Capability = "audio"
	CapWorker    Capability = "worker"
)

// Config describes an engine instance for mounting.
type Config struct {
	// Name is the engine component name.
	Name string `json:"name"`

	// Kind is "worker" or "surface".
	Kind Kind `json:"kind"`

	// WASMPath is the URL to the engine's WASM binary.
	WASMPath string `json:"wasmPath"`

	// JSPath is an optional JS entrypoint for engines that opt into the
	// unrestricted client runtime.
	JSPath string `json:"jsPath,omitempty"`

	// JSExport is the factory name published in window.__gosx_engine_factories.
	JSExport string `json:"jsExport,omitempty"`

	// MountID is the DOM element ID for surface engines (ignored for workers).
	MountID string `json:"mountId,omitempty"`

	// Props is the initial props for the engine.
	Props json.RawMessage `json:"props,omitempty"`

	// Capabilities lists required browser APIs.
	Capabilities []Capability `json:"capabilities,omitempty"`
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
		CapCanvas: true, CapWebGL: true, CapAnimation: true,
		CapStorage: true, CapFetch: true, CapAudio: true, CapWorker: true,
	}
	for _, cap := range requested {
		if !supported[cap] {
			return fmt.Errorf("unsupported engine capability: %q", cap)
		}
	}
	return nil
}
