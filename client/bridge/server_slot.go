package bridge

import (
	"encoding/json"
	"fmt"
	"strconv"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

type serverSlotStage struct {
	previous *vm.Island
	bridge   *Bridge
	token    string
	prog     *program.Program
	shared   map[string]serverSlotSharedCapture
}

type serverSlotSharedCapture struct {
	value  vm.Value
	exists bool
}

// ServerSlotAdoption contains only VM-owned shell metadata and buffered patches.
// It never transports caller content or replaces the registered owner.
type ServerSlotAdoption struct {
	Token   string            `json:"token"`
	Slots   []ServerSlotShell `json:"slots"`
	Patches []vm.PatchOp      `json:"patches"`
}

type ServerSlotShell struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Hidden bool   `json:"hidden"`
}

func (b *Bridge) PrepareServerSlots(id, name, props string, data []byte, format string) (*ServerSlotAdoption, error) {
	prog, err := DecodeProgram(data, format)
	if err != nil {
		return nil, err
	}
	if !prog.HasServerSlots() {
		return nil, fmt.Errorf("ServerSlot: program declares no outlets")
	}
	if b.serverSlotStages == nil {
		b.serverSlotStages = make(map[string]*serverSlotStage)
	}
	if stage := b.serverSlotStages[id]; stage != nil {
		return nil, fmt.Errorf("ServerSlot: adoption already pending for %s", id)
	}
	staged := New()
	captured := make(map[string]serverSlotSharedCapture)
	for _, def := range sharedSignalDefs(prog) {
		value, exists := b.store.Get(def.Name)
		copy, err := copyServerSlotValue(value, 0)
		if err != nil {
			return nil, err
		}
		captured[def.Name] = serverSlotSharedCapture{copy, exists}
		if exists {
			private, err := copyServerSlotValue(copy, 0)
			if err != nil {
				return nil, err
			}
			staged.store.Signal(def.Name, private)
		}
	}
	staged.hostFactories = b.hostFactories
	staged.preparingServerSlots = true
	output := &ServerSlotAdoption{Patches: []vm.PatchOp{}}
	staged.SetPatchCallback(func(_ string, raw string) {
		var ops []vm.PatchOp
		if json.Unmarshal([]byte(raw), &ops) == nil {
			output.Patches = append(output.Patches, ops...)
		}
	})
	if err := staged.HydrateIsland(id, name, props, data, format); err != nil {
		staged.DisposeIsland(id)
		return nil, err
	}
	if err := staged.islands[id].ValidateServerSlotValues(); err != nil {
		staged.DisposeIsland(id)
		return nil, err
	}
	tree := staged.islands[id].CurrentTree()
	var walk func(int, string)
	walk = func(idx int, path string) {
		node := tree.Nodes[idx]
		if node.ServerSlot != "" {
			shell := ServerSlotShell{Name: node.ServerSlot, Path: path}
			for _, attr := range node.Attrs {
				if attr.Name == "hidden" {
					shell.Hidden = attr.Bool || attr.Value == "true"
				}
			}
			output.Slots = append(output.Slots, shell)
			return
		}
		for i, child := range node.Children {
			next := strconv.Itoa(i)
			if path != "" {
				next = path + "/" + next
			}
			walk(child, next)
		}
	}
	if tree == nil || len(tree.Nodes) == 0 {
		staged.DisposeIsland(id)
		return nil, fmt.Errorf("ServerSlot: empty staged tree")
	}
	walk(0, "")
	b.serverSlotToken++
	output.Token = strconv.FormatUint(b.serverSlotToken, 10)
	b.serverSlotStages[id] = &serverSlotStage{previous: b.islands[id], bridge: staged, token: output.Token, prog: prog, shared: captured}
	return output, nil
}

func (b *Bridge) AbortServerSlots(id, token string) error {
	stage := b.serverSlotStages[id]
	if stage == nil || stage.token != token {
		return fmt.Errorf("ServerSlot: stale adoption token")
	}
	delete(b.serverSlotStages, id)
	stage.bridge.DisposeIsland(id)
	return nil
}

func (b *Bridge) CommitServerSlots(id, token string) error {
	stage := b.serverSlotStages[id]
	if stage == nil || stage.token != token {
		return fmt.Errorf("ServerSlot: stale adoption token")
	}
	// The browser calls this only after complete DOM and patch validation.
	if b.islands[id] != stage.previous {
		return fmt.Errorf("ServerSlot: owner changed during adoption")
	}
	for name, captured := range stage.shared {
		current, exists := b.store.Get(name)
		if exists != captured.exists || (exists && !current.Eq(captured.value).Truth()) {
			return fmt.Errorf("ServerSlot: shared signal %s changed during adoption", name)
		}
	}
	delete(b.serverSlotStages, id)
	b.DisposeIsland(id)
	owner := stage.bridge.islands[id]
	for _, unsub := range stage.bridge.unsubs[id] {
		unsub()
	}
	for _, def := range sharedSignalDefs(stage.prog) {
		value, _ := stage.bridge.store.Get(def.Name)
		owner.BindSharedSignal(def.Name, b.store.Signal(def.Name, value))
	}
	b.islands[id] = owner
	b.reconcilers[id] = owner
	if b.hostReceivers == nil {
		b.hostReceivers = make(map[string]map[string]vm.HostReceiver)
	}
	b.hostReceivers[id] = stage.bridge.hostReceivers[id]
	b.unsubs[id] = b.subscribeSharedSignals(id, sharedSignalDefs(stage.prog))
	delete(stage.bridge.islands, id)
	delete(stage.bridge.hostReceivers, id)
	delete(stage.bridge.unsubs, id)
	delete(b.serverSlotStages, id)
	return nil
}
