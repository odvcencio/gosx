package game

import (
	"reflect"
	"slices"
	"strings"
)

// EntityID is a stable runtime entity handle. The zero value is invalid.
type EntityID uint64

const InvalidEntity EntityID = 0

// Vec3 is a small vector component for common simulation state.
type Vec3 struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// Transform is the default spatial component for entity-driven simulations.
type Transform struct {
	Position Vec3 `json:"position,omitzero"`
	Rotation Vec3 `json:"rotation,omitzero"`
	Scale    Vec3 `json:"scale,omitzero"`
}

// Velocity stores linear and angular velocity in world units per second.
type Velocity struct {
	Linear  Vec3 `json:"linear,omitzero"`
	Angular Vec3 `json:"angular,omitzero"`
}

// World stores entities, typed components, and typed resources. It is small
// and deterministic by design: systems choose their own storage-heavy
// structures when they need tighter cache behavior.
type World struct {
	next       EntityID
	alive      map[EntityID]struct{}
	names      map[string]EntityID
	entityName map[EntityID]string
	components map[reflect.Type]map[EntityID]any
	resources  map[reflect.Type]any
}

// EntityOption configures an entity as it is spawned.
type EntityOption func(*entityOptions)

type entityOptions struct {
	name string
}

// WithName assigns a stable lookup name to a spawned entity.
func WithName(name string) EntityOption {
	return func(opts *entityOptions) {
		opts.name = strings.TrimSpace(name)
	}
}

// NewWorld creates an empty entity/component world.
func NewWorld() *World {
	return &World{
		alive:      make(map[EntityID]struct{}),
		names:      make(map[string]EntityID),
		entityName: make(map[EntityID]string),
		components: make(map[reflect.Type]map[EntityID]any),
		resources:  make(map[reflect.Type]any),
	}
}

// Spawn creates a new entity and returns its handle.
func (w *World) Spawn(opts ...EntityOption) EntityID {
	if w == nil {
		return InvalidEntity
	}
	if w.alive == nil {
		*w = *NewWorld()
	}
	var cfg entityOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	w.next++
	id := w.next
	w.alive[id] = struct{}{}
	if cfg.name != "" {
		if previous := w.names[cfg.name]; previous != InvalidEntity {
			delete(w.entityName, previous)
		}
		w.names[cfg.name] = id
		w.entityName[id] = cfg.name
	}
	return id
}

// Despawn removes an entity and all of its components.
func (w *World) Despawn(id EntityID) {
	if w == nil || id == InvalidEntity {
		return
	}
	delete(w.alive, id)
	if name := w.entityName[id]; name != "" {
		delete(w.names, name)
		delete(w.entityName, id)
	}
	for _, bucket := range w.components {
		delete(bucket, id)
	}
}

// Alive reports whether id still refers to a live entity.
func (w *World) Alive(id EntityID) bool {
	if w == nil || id == InvalidEntity {
		return false
	}
	_, ok := w.alive[id]
	return ok
}

// Entity returns the entity handle registered with name.
func (w *World) Entity(name string) (EntityID, bool) {
	if w == nil {
		return InvalidEntity, false
	}
	id, ok := w.names[strings.TrimSpace(name)]
	if !ok || !w.Alive(id) {
		return InvalidEntity, false
	}
	return id, true
}

// Entities returns all live entities in spawn order.
func (w *World) Entities() []EntityID {
	return EntitiesInto(w, nil)
}

// EntitiesInto appends all live entities to dst in spawn order. The returned
// slice reuses dst's backing array when capacity allows, which avoids per-frame
// allocations in fixed-step game loops.
func EntitiesInto(w *World, dst []EntityID) []EntityID {
	out := dst[:0]
	if w == nil || len(w.alive) == 0 {
		return out
	}
	for id := range w.alive {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Set attaches component to id using its concrete Go type as the component key.
func (w *World) Set(id EntityID, component any) bool {
	if w == nil || component == nil || !w.Alive(id) {
		return false
	}
	typ := reflect.TypeOf(component)
	if w.components == nil {
		w.components = make(map[reflect.Type]map[EntityID]any)
	}
	bucket := w.components[typ]
	if bucket == nil {
		bucket = make(map[EntityID]any)
		w.components[typ] = bucket
	}
	bucket[id] = component
	return true
}

// Remove deletes component type typ from id.
func (w *World) Remove(id EntityID, typ reflect.Type) bool {
	if w == nil || typ == nil {
		return false
	}
	bucket := w.components[typ]
	if bucket == nil {
		return false
	}
	if _, ok := bucket[id]; !ok {
		return false
	}
	delete(bucket, id)
	return true
}

func componentType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// SetComponent attaches a typed component to id.
func SetComponent[T any](w *World, id EntityID, component T) bool {
	if w == nil || !w.Alive(id) {
		return false
	}
	typ := componentType[T]()
	if w.components == nil {
		w.components = make(map[reflect.Type]map[EntityID]any)
	}
	bucket := w.components[typ]
	if bucket == nil {
		bucket = make(map[EntityID]any)
		w.components[typ] = bucket
	}
	bucket[id] = component
	return true
}

// GetComponent returns the typed component attached to id.
func GetComponent[T any](w *World, id EntityID) (T, bool) {
	var zero T
	if w == nil || !w.Alive(id) {
		return zero, false
	}
	bucket := w.components[componentType[T]()]
	if bucket == nil {
		return zero, false
	}
	value, ok := bucket[id]
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// UpdateComponent edits a component in place through a copy/writeback cycle.
func UpdateComponent[T any](w *World, id EntityID, update func(*T)) bool {
	if update == nil {
		return false
	}
	value, ok := GetComponent[T](w, id)
	if !ok {
		return false
	}
	update(&value)
	return SetComponent(w, id, value)
}

// RemoveComponent deletes the typed component attached to id.
func RemoveComponent[T any](w *World, id EntityID) bool {
	if w == nil {
		return false
	}
	return w.Remove(id, componentType[T]())
}

// ComponentRef is one row returned by Query.
type ComponentRef[T any] struct {
	Entity EntityID
	Value  T
}

// Query returns all live entities carrying component T.
func Query[T any](w *World) []ComponentRef[T] {
	return QueryInto[T](w, nil)
}

// QueryInto appends all live entities carrying component T to dst. The returned
// slice is sorted by entity ID and reuses dst's backing array when possible.
// Prefer this in hot systems that run every fixed frame.
func QueryInto[T any](w *World, dst []ComponentRef[T]) []ComponentRef[T] {
	out := dst[:0]
	if w == nil {
		return out
	}
	bucket := w.components[componentType[T]()]
	if len(bucket) == 0 {
		return out
	}
	for id, raw := range bucket {
		if !w.Alive(id) {
			continue
		}
		if value, ok := raw.(T); ok {
			out = append(out, ComponentRef[T]{Entity: id, Value: value})
		}
	}
	slices.SortFunc(out, func(a, b ComponentRef[T]) int {
		if a.Entity < b.Entity {
			return -1
		}
		if a.Entity > b.Entity {
			return 1
		}
		return 0
	})
	return out
}

// SetResource stores a singleton typed resource.
func SetResource[T any](w *World, resource T) bool {
	if w == nil {
		return false
	}
	if w.resources == nil {
		w.resources = make(map[reflect.Type]any)
	}
	w.resources[componentType[T]()] = resource
	return true
}

// GetResource returns a singleton typed resource.
func GetResource[T any](w *World) (T, bool) {
	var zero T
	if w == nil {
		return zero, false
	}
	value, ok := w.resources[componentType[T]()]
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	return typed, ok
}
