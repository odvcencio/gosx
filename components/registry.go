package components

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
)

// Props are dynamic component props used by registry-backed component libraries.
type Props map[string]any

// RenderFunc renders a registered component.
type RenderFunc func(Props, ...gosx.Node) gosx.Node

// Metadata describes a component library entry.
type Metadata struct {
	Package     string
	Description string
	Styles      []string
	Scripts     []string
	Tags        []string
}

// Definition is a registered component.
type Definition struct {
	Name string
	Metadata
	Render RenderFunc
}

// Registry stores component definitions by stable name.
type Registry struct {
	mu   sync.RWMutex
	defs map[string]Definition
}

// NewRegistry creates an empty component registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}}
}

// Register adds a component definition.
func (r *Registry) Register(def Definition) error {
	if r == nil {
		return fmt.Errorf("component registry is nil")
	}
	name := normalizeName(def.Name)
	if name == "" {
		return fmt.Errorf("component name is required")
	}
	if def.Render == nil {
		return fmt.Errorf("component %q render function is required", name)
	}
	def.Name = name
	def.Metadata = cloneMetadata(def.Metadata)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defs == nil {
		r.defs = map[string]Definition{}
	}
	if _, exists := r.defs[name]; exists {
		return fmt.Errorf("component %q already registered", name)
	}
	r.defs[name] = def
	return nil
}

// RegisterFunc adds a component render function with metadata.
func (r *Registry) RegisterFunc(name string, render RenderFunc, meta Metadata) error {
	return r.Register(Definition{Name: name, Render: render, Metadata: meta})
}

// MustRegister adds a component definition or panics.
func (r *Registry) MustRegister(def Definition) {
	if err := r.Register(def); err != nil {
		panic(err)
	}
}

// RegisterLibrary imports another registry under an optional namespace prefix.
func (r *Registry) RegisterLibrary(prefix string, library *Registry) error {
	if library == nil {
		return fmt.Errorf("component library is nil")
	}
	for _, def := range library.List() {
		def.Name = ScopedName(prefix, def.Name)
		if err := r.Register(def); err != nil {
			return err
		}
	}
	return nil
}

// Lookup returns a component definition by name.
func (r *Registry) Lookup(name string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	name = normalizeName(name)
	r.mu.RLock()
	def, ok := r.defs[name]
	r.mu.RUnlock()
	if !ok {
		return Definition{}, false
	}
	def.Metadata = cloneMetadata(def.Metadata)
	return def, true
}

// Render renders a registered component by name.
func (r *Registry) Render(name string, props Props, children ...gosx.Node) (gosx.Node, bool) {
	def, ok := r.Lookup(name)
	if !ok || def.Render == nil {
		return gosx.Node{}, false
	}
	return def.Render(cloneProps(props), children...), true
}

// Bindings returns route.FileTemplateBindings-compatible component functions.
func (r *Registry) Bindings() map[string]any {
	defs := r.List()
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]any, len(defs))
	for _, def := range defs {
		def := def
		out[def.Name] = func(props map[string]any) gosx.Node {
			node, _ := renderDefinition(def, props)
			return node
		}
	}
	return out
}

// List returns all registered component definitions in name order.
func (r *Registry) List() []Definition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Definition, 0, len(r.defs))
	for _, def := range r.defs {
		def.Metadata = cloneMetadata(def.Metadata)
		out = append(out, def)
	}
	r.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// ScopedName joins a library namespace and component name.
func ScopedName(prefix, name string) string {
	prefix = normalizeName(prefix)
	name = normalizeName(name)
	if prefix == "" {
		return name
	}
	if name == "" {
		return prefix
	}
	return prefix + "." + name
}

func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	name = strings.ReplaceAll(name, "/", ".")
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	return name
}

func cloneMetadata(meta Metadata) Metadata {
	meta.Styles = append([]string(nil), meta.Styles...)
	meta.Scripts = append([]string(nil), meta.Scripts...)
	meta.Tags = append([]string(nil), meta.Tags...)
	return meta
}

func cloneProps(props Props) Props {
	if props == nil {
		return Props{}
	}
	out := make(Props, len(props))
	for key, value := range props {
		out[key] = value
	}
	return out
}

func renderDefinition(def Definition, raw map[string]any) (gosx.Node, bool) {
	if def.Render == nil {
		return gosx.Node{}, false
	}
	props := make(Props, len(raw))
	for key, value := range raw {
		props[key] = value
	}
	return def.Render(props, componentChildren(raw)...), true
}

func componentChildren(props map[string]any) []gosx.Node {
	if props == nil {
		return nil
	}
	value, ok := props["children"]
	if !ok {
		value = props["Children"]
	}
	switch children := value.(type) {
	case gosx.Node:
		if children.IsZero() {
			return nil
		}
		return []gosx.Node{children}
	case *gosx.Node:
		if children == nil || children.IsZero() {
			return nil
		}
		return []gosx.Node{*children}
	case []gosx.Node:
		return append([]gosx.Node(nil), children...)
	default:
		return nil
	}
}
