package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/signal"
)

// Bridge manages active island instances and shared state.
type Bridge struct {
	islands map[string]*vm.Island
	store   *Store
}

// Store is a shared signal store that enables cross-island state.
// Any island can read/write shared signals. When a shared signal changes,
// all islands that reference it are re-rendered — like Redux, but reactive.
type Store struct {
	signals map[string]*signal.Signal[vm.Value]
}

// NewStore creates an empty shared store.
func NewStore() *Store {
	return &Store{signals: make(map[string]*signal.Signal[vm.Value])}
}

// Set creates or updates a shared signal.
func (s *Store) Set(name string, val vm.Value) {
	if sig, ok := s.signals[name]; ok {
		sig.Set(val)
	} else {
		s.signals[name] = signal.New(val)
	}
}

// Get reads a shared signal value.
func (s *Store) Get(name string) (vm.Value, bool) {
	if sig, ok := s.signals[name]; ok {
		return sig.Get(), true
	}
	return vm.ZeroValue(0), false
}

// Signal returns the raw signal for subscription.
func (s *Store) Signal(name string) *signal.Signal[vm.Value] {
	if sig, ok := s.signals[name]; ok {
		return sig
	}
	// Auto-create with zero value
	sig := signal.New(vm.StringVal(""))
	s.signals[name] = sig
	return sig
}

// New creates a new bridge with an empty shared store.
func New() *Bridge {
	return &Bridge{
		islands: make(map[string]*vm.Island),
		store:   NewStore(),
	}
}

// Store returns the shared signal store.
func (b *Bridge) GetStore() *Store {
	return b.store
}

// HydrateIsland creates and registers an island from a program and props.
// Shared signals (prefixed with "$") are connected to the bridge's store.
func (b *Bridge) HydrateIsland(id, componentName, propsJSON string, programData []byte, format string) error {
	prog, err := DecodeProgram(programData, format)
	if err != nil {
		return fmt.Errorf("decode program %q: %w", componentName, err)
	}

	island := vm.NewIsland(prog, propsJSON)

	// Connect shared signals: any signal whose name starts with "$" is
	// stored in the bridge's shared store, not the island's local state.
	// This means multiple islands can read/write the same signal.
	for _, def := range prog.Signals {
		if len(def.Name) > 0 && def.Name[0] == '$' {
			sharedSig := b.store.Signal(def.Name)
			island.SetSharedSignal(def.Name, sharedSig)
		}
	}

	b.islands[id] = island
	return nil
}

// DispatchAction forwards an event to an island's handler and returns patches.
func (b *Bridge) DispatchAction(islandID, handlerName, eventDataJSON string) ([]vm.PatchOp, error) {
	island, ok := b.islands[islandID]
	if !ok {
		return nil, fmt.Errorf("island %q not found", islandID)
	}

	patches := island.Dispatch(handlerName, eventDataJSON)
	return patches, nil
}

// MarshalPatches serializes patch ops to JSON for the JS patch applier.
func MarshalPatches(patches []vm.PatchOp) (string, error) {
	data, err := json.Marshal(patches)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DisposeIsland cleans up an island.
func (b *Bridge) DisposeIsland(id string) {
	if island, ok := b.islands[id]; ok {
		island.Dispose()
		delete(b.islands, id)
	}
}

// IslandCount returns the number of active islands.
func (b *Bridge) IslandCount() int {
	return len(b.islands)
}
