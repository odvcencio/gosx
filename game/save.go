package game

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// WorldSaveVersion is the format version written into every WorldSave.
const WorldSaveVersion = 1

// WorldSave is the serializable form of a World: its entities, the component
// columns registered with a SaveCodec, and the registered resources. It
// encodes with encoding/json, and the same World always gives the same bytes.
type WorldSave struct {
	Version int `json:"version"`
	// Next is the last entity ID handed out, so IDs of despawned entities are
	// never reused after a load.
	Next      EntityID                   `json:"next"`
	Entities  []EntitySave               `json:"entities,omitempty"`
	Columns   []ColumnSave               `json:"columns,omitempty"`
	Resources map[string]json.RawMessage `json:"resources,omitempty"`
}

// EntitySave is one live entity.
type EntitySave struct {
	ID   EntityID `json:"id"`
	Name string   `json:"name,omitempty"`
}

// ColumnSave is one component column: Entities[i] owns Values[i]. Entities
// are in ascending order, as in the World.
type ColumnSave struct {
	Name     string            `json:"name"`
	Entities []EntityID        `json:"entities"`
	Values   []json.RawMessage `json:"values"`
}

// ValueCodec converts one component or resource type to bytes and back. A nil
// field falls back to encoding/json.
type ValueCodec[T any] struct {
	Marshal   func(T) ([]byte, error)
	Unmarshal func([]byte) (T, error)
}

func (c ValueCodec[T]) marshal(v T) ([]byte, error) {
	if c.Marshal != nil {
		return c.Marshal(v)
	}
	return json.Marshal(v)
}

func (c ValueCodec[T]) unmarshal(data []byte) (T, error) {
	if c.Unmarshal != nil {
		return c.Unmarshal(data)
	}
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}

type columnCodec struct {
	name   string
	typ    reflect.Type
	export func(*World) (ColumnSave, error)
	// decode validates and decodes a column; the returned func applies it.
	decode func(ColumnSave) (func(*World), error)
}

type resourceCodec struct {
	name   string
	typ    reflect.Type
	export func(*World) (json.RawMessage, bool, error)
	decode func(json.RawMessage) (func(*World), error)
}

// SaveCodec saves and loads World content for the component and resource types
// an application registers. Only registered types are saved: the World keeps
// its columns private, so the codec needs a name and an encoding for each type
// to write it. Register every persistent type under a stable name; the name is
// the on-disk key, so renaming a Go type does not break old saves.
type SaveCodec struct {
	// IncludeWorldInState adds the world to the State payload that sim.Runner
	// broadcasts every tick. Leave it false for large worlds; Snapshot always
	// carries the world. State never carries the fields that only a restore
	// needs (see Runtime.State).
	IncludeWorldInState bool
	// SkipUnknown makes ImportWorld ignore columns and resources whose names
	// are not registered, instead of returning an error. Use it to load saves
	// written by a newer build.
	SkipUnknown bool

	columns   []columnCodec
	resources []resourceCodec
	names     map[string]bool
	types     map[reflect.Type]bool
}

// NewSaveCodec creates an empty codec.
func NewSaveCodec() *SaveCodec {
	return &SaveCodec{names: map[string]bool{}, types: map[reflect.Type]bool{}}
}

func (c *SaveCodec) claim(kind, name string, typ reflect.Type) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("game: save %s name is empty", kind)
	}
	if c.names[kind+":"+name] {
		return fmt.Errorf("game: save %s %q already registered", kind, name)
	}
	if c.types[typ] {
		return fmt.Errorf("game: save type %s already registered", typ)
	}
	c.names[kind+":"+name] = true
	c.types[typ] = true
	return nil
}

// RegisterComponent registers component type T under name. Pass at most one
// ValueCodec to replace the JSON encoding.
func RegisterComponent[T any](c *SaveCodec, name string, codec ...ValueCodec[T]) error {
	if c == nil {
		return fmt.Errorf("game: nil SaveCodec")
	}
	typ := componentType[T]()
	if err := c.claim("component", name, typ); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	var vc ValueCodec[T]
	if len(codec) > 0 {
		vc = codec[0]
	}
	c.columns = append(c.columns, columnCodec{
		name: name, typ: typ,
		export: func(w *World) (ColumnSave, error) {
			col := ColumnSave{Name: name, Entities: []EntityID{}, Values: []json.RawMessage{}}
			for _, ref := range Query[T](w) {
				data, err := vc.marshal(ref.Value)
				if err != nil {
					return col, fmt.Errorf("game: save component %q entity %d: %w", name, ref.Entity, err)
				}
				col.Entities = append(col.Entities, ref.Entity)
				col.Values = append(col.Values, data)
			}
			return col, nil
		},
		decode: func(col ColumnSave) (func(*World), error) {
			if len(col.Entities) != len(col.Values) {
				return nil, fmt.Errorf("game: save component %q has %d entities and %d values", name, len(col.Entities), len(col.Values))
			}
			values := make([]T, len(col.Values))
			for i, raw := range col.Values {
				v, err := vc.unmarshal(raw)
				if err != nil {
					return nil, fmt.Errorf("game: load component %q entity %d: %w", name, col.Entities[i], err)
				}
				values[i] = v
			}
			return func(w *World) {
				for i, id := range col.Entities {
					SetComponent(w, id, values[i])
				}
			}, nil
		},
	})
	return nil
}

// RegisterResource registers resource type T under name. Pass at most one
// ValueCodec to replace the JSON encoding.
func RegisterResource[T any](c *SaveCodec, name string, codec ...ValueCodec[T]) error {
	if c == nil {
		return fmt.Errorf("game: nil SaveCodec")
	}
	typ := componentType[T]()
	if err := c.claim("resource", name, typ); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	var vc ValueCodec[T]
	if len(codec) > 0 {
		vc = codec[0]
	}
	c.resources = append(c.resources, resourceCodec{
		name: name, typ: typ,
		export: func(w *World) (json.RawMessage, bool, error) {
			v, ok := GetResource[T](w)
			if !ok {
				return nil, false, nil
			}
			data, err := vc.marshal(v)
			if err != nil {
				return nil, false, fmt.Errorf("game: save resource %q: %w", name, err)
			}
			return data, true, nil
		},
		decode: func(raw json.RawMessage) (func(*World), error) {
			v, err := vc.unmarshal(raw)
			if err != nil {
				return nil, fmt.Errorf("game: load resource %q: %w", name, err)
			}
			return func(w *World) { SetResource(w, v) }, nil
		},
	})
	return nil
}

// ExportWorld captures w. Call it between steps, not from another goroutine
// while systems run.
func (c *SaveCodec) ExportWorld(w *World) (WorldSave, error) {
	save := WorldSave{Version: WorldSaveVersion}
	if w == nil {
		return save, nil
	}
	save.Next = w.next
	for _, id := range w.Entities() {
		save.Entities = append(save.Entities, EntitySave{ID: id, Name: w.entityName[id]})
	}
	cols := append([]columnCodec(nil), c.columns...)
	sort.Slice(cols, func(i, j int) bool { return cols[i].name < cols[j].name })
	for _, codec := range cols {
		col, err := codec.export(w)
		if err != nil {
			return WorldSave{}, err
		}
		if len(col.Entities) > 0 {
			save.Columns = append(save.Columns, col)
		}
	}
	for _, codec := range c.resources {
		data, ok, err := codec.export(w)
		if err != nil {
			return WorldSave{}, err
		}
		if ok {
			if save.Resources == nil {
				save.Resources = map[string]json.RawMessage{}
			}
			save.Resources[codec.name] = data
		}
	}
	return save, nil
}

// ImportWorld replaces the content of w with save: every entity, every
// registered component column and every registered resource. Components and
// resources of unregistered types that w held are removed. If save fails to
// decode, w is left unchanged.
func (c *SaveCodec) ImportWorld(w *World, save WorldSave) error {
	if w == nil {
		return fmt.Errorf("game: nil World")
	}
	if save.Version != WorldSaveVersion {
		return fmt.Errorf("game: world save version %d is not supported (want %d)", save.Version, WorldSaveVersion)
	}
	known := make(map[EntityID]bool, len(save.Entities))
	var prev EntityID
	for _, e := range save.Entities {
		if e.ID == InvalidEntity || e.ID <= prev || e.ID > save.Next {
			return fmt.Errorf("game: world save entity %d is invalid or out of order", e.ID)
		}
		prev = e.ID
		known[e.ID] = true
	}
	byName := make(map[string]columnCodec, len(c.columns))
	for _, codec := range c.columns {
		byName[codec.name] = codec
	}
	var applies []func(*World)
	for _, col := range save.Columns {
		codec, ok := byName[col.Name]
		if !ok {
			if c.SkipUnknown {
				continue
			}
			return fmt.Errorf("game: world save has unregistered component %q", col.Name)
		}
		for _, id := range col.Entities {
			if !known[id] {
				return fmt.Errorf("game: component %q refers to unknown entity %d", col.Name, id)
			}
		}
		apply, err := codec.decode(col)
		if err != nil {
			return err
		}
		applies = append(applies, apply)
	}
	resByName := make(map[string]resourceCodec, len(c.resources))
	for _, codec := range c.resources {
		resByName[codec.name] = codec
	}
	for name, raw := range save.Resources {
		codec, ok := resByName[name]
		if !ok {
			if c.SkipUnknown {
				continue
			}
			return fmt.Errorf("game: world save has unregistered resource %q", name)
		}
		apply, err := codec.decode(raw)
		if err != nil {
			return err
		}
		applies = append(applies, apply)
	}
	// Everything decoded; now replace the world.
	*w = *NewWorld()
	w.next = save.Next
	for _, e := range save.Entities {
		w.alive[e.ID] = len(w.live)
		w.live = append(w.live, e.ID)
		if name := strings.TrimSpace(e.Name); name != "" {
			w.names[name] = e.ID
			w.entityName[e.ID] = name
		}
	}
	for _, apply := range applies {
		apply(w)
	}
	return nil
}

// SnapshotCodec returns a SnapshotCodec for Config.Snapshot that saves the
// runtime clock, physics and the registered world content, so Runtime.Snapshot
// and Runtime.Restore (and sim.Runner joins, saves and replays) round trip the
// world. It reads and writes the RuntimeState JSON with an added "world" key.
// Errors while encoding return a nil snapshot; call ExportWorld directly to see
// them. Runtime.RestoreSnapshot returns the errors of a restore, and a failed
// restore changes neither the world nor the clock.
func (c *SaveCodec) SnapshotCodec() SnapshotCodec {
	restore := func(r *Runtime, data []byte) error {
		// Check the clock before the import: ImportWorld replaces the world, and
		// nothing can undo that if the clock then fails.
		state, err := decodeRuntimeState(data)
		if err != nil {
			return err
		}
		if state.World != nil {
			if err := c.ImportWorld(r.world, *state.World); err != nil {
				return err
			}
		}
		r.applyRuntimeState(state)
		return nil
	}
	return SnapshotCodecFuncs{
		SnapshotFunc: func(r *Runtime) []byte { return c.encode(r, true, true) },
		StateFunc:    func(r *Runtime) []byte { return c.encode(r, c.IncludeWorldInState, false) },
		// RestoreFunc is for code that predates RestoreSnapshotFunc and builds a
		// codec of its own from these functions. Where both are set, every restore
		// runs RestoreSnapshotFunc.
		RestoreFunc:         func(r *Runtime, data []byte) { _ = restore(r, data) },
		RestoreSnapshotFunc: restore,
	}
}

// encode returns the state JSON. withWorld adds the world. restorable adds what
// only a restore needs, which State leaves out so a broadcast stays small.
func (c *SaveCodec) encode(r *Runtime, withWorld, restorable bool) []byte {
	if r == nil {
		return nil
	}
	state := r.runtimeState(restorable)
	if withWorld {
		save, err := c.ExportWorld(r.world)
		if err != nil {
			return nil
		}
		state.World = &save
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil
	}
	return data
}
