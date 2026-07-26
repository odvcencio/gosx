package game

import (
	"reflect"
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

// World stores entities, typed components, and typed resources.
//
// Storage is dense and ordered. Each component type owns one column of packed
// values, held in ascending entity order. A query copies contiguous memory and
// never sorts. The live entity list is kept in the same ascending order, so
// EntitiesInto never sorts either.
//
// The typed API stores values without boxing them into an interface, so a hot
// system that writes components every frame allocates nothing.
type World struct {
	next EntityID
	// alive maps a live handle to its slot in live.
	alive map[EntityID]int
	// live holds handles in ascending order. A despawned slot holds
	// InvalidEntity until the list compacts.
	live       []EntityID
	liveDead   int
	names      map[string]EntityID
	entityName map[EntityID]string
	components map[reflect.Type]componentStore
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
		alive:      make(map[EntityID]int),
		names:      make(map[string]EntityID),
		entityName: make(map[EntityID]string),
		components: make(map[reflect.Type]componentStore),
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
	// Handles increase, so an append keeps the live list ascending.
	w.alive[id] = len(w.live)
	w.live = append(w.live, id)
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
	pos, ok := w.alive[id]
	if !ok {
		return
	}
	delete(w.alive, id)
	// Mark the slot instead of moving the tail. The list compacts itself once
	// half of the slots are dead, so one despawn costs constant time on average.
	w.live[pos] = InvalidEntity
	w.liveDead++
	w.compactLive()
	if len(w.entityName) > 0 {
		if name := w.entityName[id]; name != "" {
			delete(w.names, name)
			delete(w.entityName, id)
		}
	}
	for _, store := range w.components {
		store.removeEntity(id)
	}
}

// compactLive drops the dead slots once they outnumber the live ones.
func (w *World) compactLive() {
	if w.liveDead < compactFloor || w.liveDead*2 < len(w.live) {
		return
	}
	next := 0
	for _, id := range w.live {
		if id == InvalidEntity {
			continue
		}
		w.live[next] = id
		w.alive[id] = next
		next++
	}
	w.live = w.live[:next]
	w.liveDead = 0
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
// allocations in fixed-step game loops. The live list is already ordered, so
// this copies memory and does not sort.
func EntitiesInto(w *World, dst []EntityID) []EntityID {
	out := dst[:0]
	if w == nil || len(w.live) == 0 {
		return out
	}
	if w.liveDead == 0 {
		return append(out, w.live...)
	}
	for _, id := range w.live {
		if id != InvalidEntity {
			out = append(out, id)
		}
	}
	return out
}

// Set attaches component to id using its concrete Go type as the component key.
// Prefer SetComponent: the typed entry point stores the value without boxing it.
func (w *World) Set(id EntityID, component any) bool {
	if w == nil || component == nil || !w.Alive(id) {
		return false
	}
	typ := reflect.TypeOf(component)
	if w.components == nil {
		w.components = make(map[reflect.Type]componentStore)
	}
	store := w.components[typ]
	if store == nil {
		store = &boxedStore{typ: typ}
		w.components[typ] = store
	}
	return store.setAny(id, component)
}

// Remove deletes component type typ from id.
func (w *World) Remove(id EntityID, typ reflect.Type) bool {
	if w == nil || typ == nil {
		return false
	}
	store := w.components[typ]
	if store == nil {
		return false
	}
	return store.removeEntity(id)
}

func componentType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// SetComponent attaches a typed component to id. The value goes into dense
// storage without a heap box.
func SetComponent[T any](w *World, id EntityID, component T) bool {
	if w == nil || !w.Alive(id) {
		return false
	}
	denseColumn[T](w).set(id, component)
	return true
}

// GetComponent returns the typed component attached to id. Despawn clears every
// column, so a hit implies a live entity.
func GetComponent[T any](w *World, id EntityID) (T, bool) {
	var zero T
	switch store := readColumn[T](w).(type) {
	case *denseStore[T]:
		return store.get(id)
	case *boxedStore:
		boxed, ok := store.getAny(id)
		if !ok {
			return zero, false
		}
		typed, ok := boxed.(T)
		return typed, ok
	}
	return zero, false
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
//
// The column is already ordered and holds only live entities, so this walks
// contiguous memory once. It does not sort and it does not check liveness.
func QueryInto[T any](w *World, dst []ComponentRef[T]) []ComponentRef[T] {
	out := dst[:0]
	switch store := readColumn[T](w).(type) {
	case *denseStore[T]:
		entities := store.entities
		values := store.values
		if cap(out) < store.count() {
			out = make([]ComponentRef[T], 0, store.count())
		}
		if store.deadN == 0 {
			for i := range entities {
				out = append(out, ComponentRef[T]{Entity: entities[i], Value: values[i]})
			}
			return out
		}
		dead := store.dead
		for i := range entities {
			if dead[i] {
				continue
			}
			out = append(out, ComponentRef[T]{Entity: entities[i], Value: values[i]})
		}
	case *boxedStore:
		for i, id := range store.entities {
			if store.dead[i] {
				continue
			}
			if value, ok := store.values[i].(T); ok {
				out = append(out, ComponentRef[T]{Entity: id, Value: value})
			}
		}
	}
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
