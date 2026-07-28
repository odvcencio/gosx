package game

import (
	"reflect"
	"slices"
)

// Component storage for World.
//
// Each component type owns one column. A column keeps parallel dense slices:
// entity handles in ascending order, and the component values in the same order.
// A query therefore walks contiguous memory and needs no sort, because the
// column is already ordered.
//
// Lookup uses a binary search over the entity slice instead of a hash map. The
// column stays compact, and no per-entity map bucket exists to chase.
//
// Removal marks the slot dead instead of moving the tail, so one removal costs
// a binary search and a flag write. The column compacts itself when half of the
// slots are dead, which makes the cost of a removal constant when averaged over
// many removals.
//
// A column pays one memory move when a caller attaches a component to an entity
// that is older than the newest live entry. Games normally attach components at
// spawn time, which appends and moves nothing.

// compactFloor is the smallest number of dead slots that triggers compaction.
// Small columns are cheap to walk, so compacting them early buys nothing.
const compactFloor = 32

// componentStore is the type-erased view World keeps of one column.
type componentStore interface {
	// getAny returns the boxed value attached to id.
	getAny(id EntityID) (any, bool)
	// setAny stores a boxed value. It reports false when the value has the
	// wrong concrete type for this column.
	setAny(id EntityID, value any) bool
	// removeEntity drops id from the column and reports whether it was present.
	removeEntity(id EntityID) bool
	// count returns the number of live entities in the column.
	count() int
}

// denseStore is the unboxed column that the typed API uses.
type denseStore[T any] struct {
	entities []EntityID
	values   []T
	dead     []bool
	deadN    int
}

func (s *denseStore[T]) get(id EntityID) (T, bool) {
	if last := len(s.entities) - 1; last < 0 || id > s.entities[last] {
		var zero T
		return zero, false
	}
	if pos, found := slices.BinarySearch(s.entities, id); found && !s.dead[pos] {
		return s.values[pos], true
	}
	var zero T
	return zero, false
}

func (s *denseStore[T]) set(id EntityID, value T) {
	// Games attach components at spawn time, and handles increase. Check the
	// tail first so the common case skips the binary search.
	if last := len(s.entities) - 1; last < 0 || id > s.entities[last] {
		s.entities = append(s.entities, id)
		s.values = append(s.values, value)
		s.dead = append(s.dead, false)
		return
	}
	pos, found := slices.BinarySearch(s.entities, id)
	if found {
		s.values[pos] = value
		if s.dead[pos] {
			s.dead[pos] = false
			s.deadN--
		}
		return
	}
	if pos == len(s.entities) {
		s.entities = append(s.entities, id)
		s.values = append(s.values, value)
		s.dead = append(s.dead, false)
		return
	}
	s.entities = slices.Insert(s.entities, pos, id)
	s.values = slices.Insert(s.values, pos, value)
	s.dead = slices.Insert(s.dead, pos, false)
}

func (s *denseStore[T]) getAny(id EntityID) (any, bool) {
	value, ok := s.get(id)
	if !ok {
		return nil, false
	}
	return value, true
}

func (s *denseStore[T]) setAny(id EntityID, value any) bool {
	typed, ok := value.(T)
	if !ok {
		return false
	}
	s.set(id, typed)
	return true
}

func (s *denseStore[T]) removeEntity(id EntityID) bool {
	if last := len(s.entities) - 1; last < 0 || id > s.entities[last] {
		return false
	}
	pos, found := slices.BinarySearch(s.entities, id)
	if !found || s.dead[pos] {
		return false
	}
	s.dead[pos] = true
	s.deadN++
	s.compact()
	return true
}

func (s *denseStore[T]) count() int { return len(s.entities) - s.deadN }

// compact drops the dead slots once they outnumber the live ones.
func (s *denseStore[T]) compact() {
	if s.deadN < compactFloor || s.deadN*2 < len(s.entities) {
		return
	}
	next := 0
	for i := range s.entities {
		if s.dead[i] {
			continue
		}
		s.entities[next] = s.entities[i]
		s.values[next] = s.values[i]
		s.dead[next] = false
		next++
	}
	s.entities = s.entities[:next]
	s.values = s.values[:next]
	s.dead = s.dead[:next]
	s.deadN = 0
}

// boxedStore is the column that World.Set creates when it sees a component type
// for the first time. The untyped entry point receives an any, so this column
// cannot avoid the box. The first typed write upgrades the column to a
// denseStore, which removes the box from then on.
type boxedStore struct {
	typ      reflect.Type
	entities []EntityID
	values   []any
	dead     []bool
	deadN    int
}

func (s *boxedStore) getAny(id EntityID) (any, bool) {
	if pos, found := slices.BinarySearch(s.entities, id); found && !s.dead[pos] {
		return s.values[pos], true
	}
	return nil, false
}

func (s *boxedStore) setAny(id EntityID, value any) bool {
	if value == nil || reflect.TypeOf(value) != s.typ {
		return false
	}
	pos, found := slices.BinarySearch(s.entities, id)
	if found {
		s.values[pos] = value
		if s.dead[pos] {
			s.dead[pos] = false
			s.deadN--
		}
		return true
	}
	s.entities = slices.Insert(s.entities, pos, id)
	s.values = slices.Insert(s.values, pos, value)
	s.dead = slices.Insert(s.dead, pos, false)
	return true
}

func (s *boxedStore) removeEntity(id EntityID) bool {
	pos, found := slices.BinarySearch(s.entities, id)
	if !found || s.dead[pos] {
		return false
	}
	s.dead[pos] = true
	s.deadN++
	if s.deadN >= compactFloor && s.deadN*2 >= len(s.entities) {
		next := 0
		for i := range s.entities {
			if s.dead[i] {
				continue
			}
			s.entities[next] = s.entities[i]
			s.values[next] = s.values[i]
			s.dead[next] = false
			next++
		}
		s.entities = s.entities[:next]
		s.values = s.values[:next]
		s.dead = s.dead[:next]
		s.deadN = 0
	}
	return true
}

func (s *boxedStore) count() int { return len(s.entities) - s.deadN }

// denseColumn returns the unboxed column for T, creating it when it is absent.
// A boxed column left behind by World.Set is migrated in place, so the typed
// API always ends up on unboxed storage.
func denseColumn[T any](w *World) *denseStore[T] {
	typ := componentType[T]()
	if w.components == nil {
		w.components = make(map[reflect.Type]componentStore)
	}
	switch existing := w.components[typ].(type) {
	case *denseStore[T]:
		return existing
	case *boxedStore:
		store := &denseStore[T]{
			entities: append([]EntityID(nil), existing.entities...),
			values:   make([]T, 0, len(existing.values)),
			dead:     append([]bool(nil), existing.dead...),
			deadN:    existing.deadN,
		}
		for _, boxed := range existing.values {
			typed, _ := boxed.(T)
			store.values = append(store.values, typed)
		}
		w.components[typ] = store
		return store
	}
	store := &denseStore[T]{}
	w.components[typ] = store
	return store
}

// readColumn returns the column for T without creating one.
func readColumn[T any](w *World) componentStore {
	if w == nil || w.components == nil {
		return nil
	}
	return w.components[componentType[T]()]
}
