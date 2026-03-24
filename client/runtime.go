// Package client provides the client-side WASM runtime for GoSX islands.
//
// This package is compiled to WASM and runs in the browser. It provides:
// - Island hydration entry points
// - Signal/Computed reactive state (re-exports from signal package)
// - DOM manipulation helpers
// - Event binding
// - Action dispatch
//
// The client runtime is intentionally small. Most rendering happens on the server.
package client

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/signal"
)

// IslandComponent is a client-side component that can be hydrated.
type IslandComponent struct {
	// Name is the component identifier matching the manifest.
	Name string

	// Render produces the component's node tree given props.
	Render func(props json.RawMessage) gosx.Node

	// OnMount is called after hydration (optional).
	OnMount func()

	// OnUnmount is called before teardown (optional).
	OnUnmount func()
}

// IslandRegistry holds registered island components for client-side hydration.
var IslandRegistry = &islandRegistry{
	components: make(map[string]*IslandComponent),
}

type islandRegistry struct {
	components map[string]*IslandComponent
}

// Register adds a component to the island registry.
func (r *islandRegistry) Register(comp *IslandComponent) {
	r.components[comp.Name] = comp
}

// Get returns a registered component by name.
func (r *islandRegistry) Get(name string) (*IslandComponent, bool) {
	comp, ok := r.components[name]
	return comp, ok
}

// Hydrate initializes a specific island from the manifest.
func Hydrate(islandID string, componentName string, propsJSON string) error {
	comp, ok := IslandRegistry.Get(componentName)
	if !ok {
		return fmt.Errorf("component %q not registered", componentName)
	}

	node := comp.Render(json.RawMessage(propsJSON))
	html := gosx.RenderHTML(node)

	// The DOM patching is handled by the JS bootstrap calling __gosx_patch
	_ = html

	if comp.OnMount != nil {
		comp.OnMount()
	}

	return nil
}

// IslandState holds the reactive state for a hydrated island.
type IslandState struct {
	signals  map[string]any
	effects  []*signal.Effect
	islandID string
}

// NewIslandState creates state management for an island.
func NewIslandState(islandID string) *IslandState {
	return &IslandState{
		signals:  make(map[string]any),
		islandID: islandID,
	}
}

// Signal creates a new signal scoped to this island.
func Signal[T any](initial T) *signal.Signal[T] {
	return signal.New(initial)
}

// Computed creates a new computed value.
func Computed[T any](fn func() T) *signal.Computed[T] {
	return signal.Derive(fn)
}

// Watch creates an effect that re-runs when dependencies change.
func Watch(fn func()) *signal.Effect {
	return signal.Watch(fn)
}

// Batch coalesces multiple signal updates.
func Batch(fn func()) {
	signal.Batch(fn)
}

// RenderIsland produces HTML for an island's current state.
func RenderIsland(node gosx.Node) string {
	return gosx.RenderHTML(node)
}

// DOMPatch describes a patch operation for the island's DOM.
type DOMPatch struct {
	IslandID string
	HTML     string
}

// BuildPatch creates a DOM patch from the current component state.
func BuildPatch(islandID string, node gosx.Node) DOMPatch {
	return DOMPatch{
		IslandID: islandID,
		HTML:     gosx.RenderHTML(node),
	}
}

// EventHandler wraps a Go function as a client-side event handler.
type EventHandler struct {
	Name    string
	Handler func()
}

// NewHandler creates a named event handler.
func NewHandler(name string, fn func()) EventHandler {
	return EventHandler{Name: name, Handler: fn}
}

// NodeBuilder provides a fluent API for building component trees in client code.
type NodeBuilder struct {
	nodes []gosx.Node
}

// NewBuilder creates a node builder.
func NewBuilder() *NodeBuilder {
	return &NodeBuilder{}
}

// Add appends a node.
func (b *NodeBuilder) Add(n gosx.Node) *NodeBuilder {
	b.nodes = append(b.nodes, n)
	return b
}

// Build returns a fragment containing all nodes.
func (b *NodeBuilder) Build() gosx.Node {
	return gosx.Fragment(b.nodes...)
}

// UnmarshalProps deserializes JSON props into a typed struct.
func UnmarshalProps[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// MarshalProps serializes props to JSON for the hydration manifest.
func MarshalProps(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}

// ValidateSerializable checks that a value can cross the server→client boundary.
func ValidateSerializable(v any) error {
	_, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("value is not serializable: %w", err)
	}
	return nil
}

// Helpers for common DOM patterns

// If returns child if cond is true, otherwise empty.
func If(cond bool, child gosx.Node) gosx.Node {
	if cond {
		return child
	}
	return gosx.Text("")
}

// Map applies fn to each item and returns a fragment.
func Map[T any](items []T, fn func(T, int) gosx.Node) gosx.Node {
	nodes := make([]gosx.Node, len(items))
	for i, item := range items {
		nodes[i] = fn(item, i)
	}
	return gosx.Fragment(nodes...)
}

// Show conditionally renders content.
func Show(cond bool, content gosx.Node, fallback ...gosx.Node) gosx.Node {
	if cond {
		return content
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return gosx.Text("")
}

// Classes builds a class string from conditional class names.
func Classes(classes ...string) string {
	var result []string
	for _, c := range classes {
		if c != "" {
			result = append(result, c)
		}
	}
	return strings.Join(result, " ")
}
