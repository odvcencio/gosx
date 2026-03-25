// Package hydrate defines the hydration manifest and island metadata
// that connects server-rendered HTML to client-side WASM islands.
package hydrate

import "encoding/json"

// Manifest describes all islands and engines on a page.
type Manifest struct {
	// Version of the manifest format.
	Version string `json:"version"`

	// Islands lists every island instance on the page.
	Islands []IslandEntry `json:"islands"`

	// Engines lists every engine instance on the page.
	Engines []EngineEntry `json:"engines,omitempty"`

	// Bundles maps bundle IDs to WASM asset paths.
	Bundles map[string]BundleRef `json:"bundles"`

	// Runtime points to the shared island WASM runtime.
	Runtime RuntimeRef `json:"runtime"`
}

// EngineEntry describes a single engine instance.
// Engines are arbitrary client compute modules — separate from islands.
type EngineEntry struct {
	// ID is the DOM anchor ID (e.g., "gosx-engine-0").
	ID string `json:"id"`

	// Component is the engine function name.
	Component string `json:"component"`

	// Kind is "worker" (background compute) or "surface" (owns a DOM mount point).
	Kind string `json:"kind"`

	// ProgramRef is the URL path to the engine's WASM bundle.
	ProgramRef string `json:"programRef"`

	// Props is the JSON-serialized props snapshot.
	Props json.RawMessage `json:"props"`

	// Capabilities declares what browser APIs the engine needs.
	Capabilities []string `json:"capabilities,omitempty"`
}

// RuntimeRef points to the shared WASM runtime.
type RuntimeRef struct {
	// Path to the shared runtime .wasm file.
	Path string `json:"path"`

	// Hash for cache busting.
	Hash string `json:"hash,omitempty"`

	// Size in bytes (compressed).
	Size int64 `json:"size,omitempty"`
}

// IslandEntry describes a single island instance in the rendered HTML.
type IslandEntry struct {
	// ID is the stable DOM anchor ID (e.g., "gosx-island-0").
	ID string `json:"id"`

	// Component is the fully qualified component name.
	Component string `json:"component"`

	// BundleID references an entry in Manifest.Bundles.
	BundleID string `json:"bundleId"`

	// Props is the JSON-serialized props snapshot.
	Props json.RawMessage `json:"props"`

	// Events lists the event bindings for this island.
	Events []EventSlot `json:"events,omitempty"`

	// Static is true if the island has no dynamic content and can skip hydration.
	Static bool `json:"static,omitempty"`

	// Checksum is a hash of the component source for cache invalidation.
	Checksum string `json:"checksum,omitempty"`

	// ProgramRef is the URL path to the IslandProgram asset.
	ProgramRef string `json:"programRef,omitempty"`

	// ProgramFormat is "json" (dev) or "bin" (prod).
	ProgramFormat string `json:"programFormat,omitempty"`

	// ProgramHash is a content hash for cache busting.
	ProgramHash string `json:"programHash,omitempty"`
}

// BundleRef points to a compiled WASM bundle.
type BundleRef struct {
	// Path is the URL path to the .wasm file.
	Path string `json:"path"`

	// Size is the compressed size in bytes.
	Size int64 `json:"size,omitempty"`

	// Hash is a content hash for cache busting.
	Hash string `json:"hash,omitempty"`
}

// EventSlot describes a single event binding within an island.
type EventSlot struct {
	// SlotID is a stable identifier for this handler.
	SlotID string `json:"slotId"`

	// EventType is the DOM event name (click, input, submit, etc.).
	EventType string `json:"eventType"`

	// TargetSelector identifies the DOM element within the island.
	TargetSelector string `json:"targetSelector,omitempty"`

	// HandlerName is the Go function name in the WASM bundle.
	HandlerName string `json:"handlerName"`

	// ServerAction is true if this event triggers a server round-trip.
	ServerAction bool `json:"serverAction,omitempty"`
}

// Marshal serializes the manifest to JSON.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Unmarshal deserializes a manifest from JSON.
func Unmarshal(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// NewManifest creates an empty manifest.
func NewManifest() *Manifest {
	return &Manifest{
		Version: "0.1.0",
		Bundles: make(map[string]BundleRef),
	}
}

// AddIsland adds an island entry and returns the assigned ID.
func (m *Manifest) AddIsland(component string, bundleID string, props any) (string, error) {
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return "", err
	}
	id := islandID(len(m.Islands))
	entry := IslandEntry{
		ID:        id,
		Component: component,
		BundleID:  bundleID,
		Props:     propsJSON,
	}
	m.Islands = append(m.Islands, entry)
	return id, nil
}

// AddEngine adds an engine entry and returns the assigned ID.
func (m *Manifest) AddEngine(component, kind, programRef string, props any, capabilities []string) (string, error) {
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return "", err
	}
	id := engineID(len(m.Engines))
	entry := EngineEntry{
		ID:           id,
		Component:    component,
		Kind:         kind,
		ProgramRef:   programRef,
		Props:        propsJSON,
		Capabilities: capabilities,
	}
	m.Engines = append(m.Engines, entry)
	return id, nil
}

func engineID(n int) string {
	return "gosx-engine-" + itoa(n)
}

func islandID(n int) string {
	return "gosx-island-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
