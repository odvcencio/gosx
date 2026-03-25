package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/signal"
)

// Bridge manages active island instances and shared state.
type Bridge struct {
	islands      map[string]*vm.Island
	store        *Store
	patchFn      func(islandID, patchJSON string) // callback to push patches to JS
	dispatching  string                            // ID of the island currently dispatching (skip its subscription)
}

// SetPatchCallback registers the function called when shared signal changes
// trigger a re-render on an island. In WASM, this calls __gosx_apply_patches.
func (b *Bridge) SetPatchCallback(fn func(islandID, patchJSON string)) {
	b.patchFn = fn
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

// Signal returns a shared signal, creating it with the given initial value
// if it doesn't exist yet. If it already exists, the init value is ignored
// (first island to declare wins).
func (s *Store) Signal(name string, init vm.Value) *signal.Signal[vm.Value] {
	if sig, ok := s.signals[name]; ok {
		return sig
	}
	sig := signal.New(init)
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
			// Evaluate the init expression to get the typed initial value
			initVal := island.EvalExpr(def.Init)
			sharedSig := b.store.Signal(def.Name, initVal)
			island.SetSharedSignal(def.Name, sharedSig)
		}
	}

	// Subscribe to shared signals: when any shared signal changes,
	// re-render this island and push patches to JS.
	// Skip if this island is the one that triggered the change (it handles
	// its own reconcile in Dispatch).
	islandID := id
	for _, def := range prog.Signals {
		if len(def.Name) > 0 && def.Name[0] == '$' {
			sig := b.store.Signal(def.Name, vm.ZeroValue(def.Type))
			sig.Subscribe(func() {
				if b.dispatching == islandID {
					return // the dispatching island reconciles itself
				}
				isl, ok := b.islands[islandID]
				if !ok {
					return
				}
				patches := isl.Reconcile()
				if len(patches) > 0 && b.patchFn != nil {
					patchJSON, err := MarshalPatches(patches)
					if err == nil {
						b.patchFn(islandID, patchJSON)
					}
				}
			})
		}
	}

	b.islands[id] = island
	return nil
}

// DispatchAction forwards an event to an island's handler and returns patches.
// If the handler mutates shared signals, other islands re-render automatically
// via their subscriptions and push patches through the patchFn callback.
func (b *Bridge) DispatchAction(islandID, handlerName, eventDataJSON string) ([]vm.PatchOp, error) {
	island, ok := b.islands[islandID]
	if !ok {
		return nil, fmt.Errorf("island %q not found", islandID)
	}

	// Mark this island as the dispatcher so its subscription callback
	// skips reconciliation (Dispatch handles it).
	b.dispatching = islandID
	patches := island.Dispatch(handlerName, eventDataJSON)
	b.dispatching = ""

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
