package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/odvcencio/gosx/client/vm"
)

// Bridge manages active island instances.
type Bridge struct {
	islands map[string]*vm.Island
}

// New creates a new bridge.
func New() *Bridge {
	return &Bridge{
		islands: make(map[string]*vm.Island),
	}
}

// HydrateIsland creates and registers an island from a program and props.
func (b *Bridge) HydrateIsland(id, componentName, propsJSON string, programData []byte, format string) error {
	prog, err := DecodeProgram(programData, format)
	if err != nil {
		return fmt.Errorf("decode program %q: %w", componentName, err)
	}

	island := vm.NewIsland(prog, propsJSON)
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
