package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/odvcencio/gosx/client/enginevm"
	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

// Bridge manages active island instances and shared state.
type Bridge struct {
	islands        map[string]*vm.Island
	computeIslands map[string]struct{}
	engines        map[string]*enginevm.Runtime
	store          *Store
	patchFn        func(islandID, patchJSON string) // callback to push patches to JS
	signalFn       func(name, valueJSON string)     // callback to notify JS of shared signal changes
	dispatching    string                           // ID of the island currently dispatching
	unsubs         map[string][]func()              // per-island unsubscribe handles for shared signals
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
	signals  map[string]*signal.Signal[vm.Value]
	onChange func(name string, value vm.Value)
}

// NewStore creates an empty shared store.
func NewStore() *Store {
	return &Store{signals: make(map[string]*signal.Signal[vm.Value])}
}

// SetObserver registers a callback invoked whenever any shared signal changes.
func (s *Store) SetObserver(fn func(name string, value vm.Value)) {
	s.onChange = fn
}

// Set creates or updates a shared signal.
func (s *Store) Set(name string, val vm.Value) {
	if sig, ok := s.signals[name]; ok {
		sig.Set(val)
	} else {
		sig := signal.New(val)
		s.signals[name] = sig
		sig.Subscribe(func() {
			if s.onChange != nil {
				s.onChange(name, sig.Get())
			}
		})
		if s.onChange != nil {
			s.onChange(name, sig.Get())
		}
	}
}

// SetBatch creates or updates multiple shared signals in one notification pass.
func (s *Store) SetBatch(values map[string]vm.Value) {
	if len(values) == 0 {
		return
	}
	signal.Batch(func() {
		for name, value := range values {
			s.Set(name, value)
		}
	})
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
	sig.Subscribe(func() {
		if s.onChange != nil {
			s.onChange(name, sig.Get())
		}
	})
	return sig
}

// New creates a new bridge with an empty shared store.
func New() *Bridge {
	b := &Bridge{
		islands:        make(map[string]*vm.Island),
		computeIslands: make(map[string]struct{}),
		engines:        make(map[string]*enginevm.Runtime),
		store:          NewStore(),
		unsubs:         make(map[string][]func()),
	}
	b.store.SetObserver(func(name string, value vm.Value) {
		b.notifySharedSignal(name, value)
	})
	return b
}

// Store returns the shared signal store.
func (b *Bridge) GetStore() *Store {
	return b.store
}

// SetSharedSignalCallback registers the function called when any shared signal changes.
func (b *Bridge) SetSharedSignalCallback(fn func(name, valueJSON string)) {
	b.signalFn = fn
}

// HydrateIsland creates and registers an island from a program and props.
// Shared signals (prefixed with "$") are connected to the bridge's store.
func (b *Bridge) HydrateIsland(id, componentName, propsJSON string, programData []byte, format string) error {
	return b.hydrateIsland(id, componentName, propsJSON, programData, format, false)
}

// HydrateComputeIsland creates a headless island from a program and props.
// It participates in shared signals but never pushes DOM patches.
func (b *Bridge) HydrateComputeIsland(id, componentName, propsJSON string, programData []byte, format string) error {
	return b.hydrateIsland(id, componentName, propsJSON, programData, format, true)
}

func (b *Bridge) hydrateIsland(id, componentName, propsJSON string, programData []byte, format string, compute bool) error {
	prog, err := DecodeProgram(programData, format)
	if err != nil {
		return fmt.Errorf("decode program %q: %w", componentName, err)
	}
	propsJSON, err = normalizePropsJSON(componentName, propsJSON)
	if err != nil {
		return err
	}
	if _, exists := b.islands[id]; exists {
		b.DisposeIsland(id)
	}

	island := vm.NewIsland(prog, propsJSON)
	defs := sharedSignalDefs(prog)
	b.connectSharedSignals(island, defs)
	b.unsubs[id] = b.subscribeSharedSignals(id, defs)

	b.islands[id] = island
	if compute {
		b.computeIslands[id] = struct{}{}
	} else {
		delete(b.computeIslands, id)
	}
	return nil
}

// HydrateEngine creates and registers an engine runtime from a program and props.
func (b *Bridge) HydrateEngine(id, componentName, propsJSON string, programData []byte, format string) ([]rootengine.Command, error) {
	prog, err := DecodeEngineProgram(programData, format)
	if err != nil {
		return nil, fmt.Errorf("decode engine program %q: %w", componentName, err)
	}
	propsJSON, err = normalizePropsJSON(componentName, propsJSON)
	if err != nil {
		return nil, err
	}
	if _, exists := b.engines[id]; exists {
		b.DisposeEngine(id)
	}

	runtime := enginevm.New(prog, propsJSON)
	connectSharedEngineSignals(runtime, b.store, prog.Signals)
	b.engines[id] = runtime

	return runtime.Reconcile(), nil
}

// DispatchAction forwards an event to an island's handler and returns patches.
// If the handler mutates shared signals, other islands re-render automatically
// via their subscriptions and push patches through the patchFn callback.
func (b *Bridge) DispatchAction(islandID, handlerName, eventDataJSON string) ([]vm.PatchOp, error) {
	island, ok := b.islands[islandID]
	if !ok {
		return nil, fmt.Errorf("island %q not found", islandID)
	}

	b.dispatching = islandID
	defer func() {
		if b.dispatching == islandID {
			b.dispatching = ""
		}
	}()

	return island.Dispatch(handlerName, eventDataJSON), nil
}

// SetSharedSignalJSON updates a shared signal from a JSON payload.
// This is used by hub/websocket clients to drive island state from realtime events.
func (b *Bridge) SetSharedSignalJSON(name, valueJSON string) error {
	value, err := decodeSharedSignalValue(name, []byte(valueJSON))
	if err != nil {
		return err
	}
	b.store.Set(name, value)
	return nil
}

// SetSharedSignalBatchJSON updates multiple shared signals from a JSON object.
// The payload shape is {"$signal.name": <json value>, ...}.
func (b *Bridge) SetSharedSignalBatchJSON(batchJSON string) error {
	if batchJSON == "" {
		batchJSON = "{}"
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(batchJSON), &payload); err != nil {
		return fmt.Errorf("decode shared signal batch: %w", err)
	}

	values := make(map[string]vm.Value, len(payload))
	for name, raw := range payload {
		value, err := decodeSharedSignalValue(name, raw)
		if err != nil {
			return err
		}
		values[name] = value
	}

	b.store.SetBatch(values)
	return nil
}

// GetSharedSignalJSON serializes the current shared signal value as JSON.
// Missing signals return JSON null.
func (b *Bridge) GetSharedSignalJSON(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("shared signal name required")
	}
	value, ok := b.store.Get(name)
	if !ok {
		return "null", nil
	}
	return marshalSharedSignalValue(value)
}

// TickEngine reconciles a live engine runtime and returns pending commands.
func (b *Bridge) TickEngine(id string) ([]rootengine.Command, error) {
	runtime, ok := b.engines[id]
	if !ok {
		return nil, fmt.Errorf("engine %q not found", id)
	}
	return runtime.Reconcile(), nil
}

// RenderEngine builds a renderer-facing frame bundle for a live engine runtime.
func (b *Bridge) RenderEngine(id string, width, height int, timeSeconds float64) (rootengine.RenderBundle, error) {
	runtime, ok := b.engines[id]
	if !ok {
		return rootengine.RenderBundle{}, fmt.Errorf("engine %q not found", id)
	}
	return runtime.RenderBundle(width, height, timeSeconds), nil
}

// MarshalPatches serializes patch ops to JSON for the JS patch applier.
func MarshalPatches(patches []vm.PatchOp) (string, error) {
	data, err := json.Marshal(patches)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DisposeIsland cleans up an island and unsubscribes from all shared signals.
func (b *Bridge) DisposeIsland(id string) {
	// Unsubscribe from shared signals first — prevents the subscription
	// callback from firing on a disposed island.
	if unsubs, ok := b.unsubs[id]; ok {
		for _, unsub := range unsubs {
			unsub()
		}
		delete(b.unsubs, id)
	}

	if island, ok := b.islands[id]; ok {
		island.Dispose()
		delete(b.islands, id)
	}
	delete(b.computeIslands, id)
}

// DisposeEngine removes a live engine runtime from the bridge.
func (b *Bridge) DisposeEngine(id string) {
	if runtime, ok := b.engines[id]; ok {
		runtime.Dispose()
		delete(b.engines, id)
	}
}

// IslandCount returns the number of active islands.
func (b *Bridge) IslandCount() int {
	return len(b.islands)
}

// ComputeIslandCount returns the number of active headless compute islands.
func (b *Bridge) ComputeIslandCount() int {
	return len(b.computeIslands)
}

// EngineCount returns the number of active engine runtimes.
func (b *Bridge) EngineCount() int {
	return len(b.engines)
}

func normalizePropsJSON(componentName, propsJSON string) (string, error) {
	if propsJSON == "" {
		propsJSON = "{}"
	}
	if !json.Valid([]byte(propsJSON)) {
		return "", fmt.Errorf("invalid props JSON for %q", componentName)
	}
	return propsJSON, nil
}

func decodeSharedSignalValue(name string, raw []byte) (vm.Value, error) {
	if name == "" {
		return vm.Value{}, fmt.Errorf("shared signal name required")
	}
	if len(raw) == 0 {
		raw = []byte("null")
	}

	value, err := vm.ValueFromJSON(string(raw))
	if err != nil {
		return vm.Value{}, fmt.Errorf("decode shared signal %q: %w", name, err)
	}
	return value, nil
}

func sharedSignalDefs(prog *program.Program) []program.SignalDef {
	if prog == nil || len(prog.Signals) == 0 {
		return nil
	}
	defs := make([]program.SignalDef, 0, len(prog.Signals))
	for _, def := range prog.Signals {
		if isSharedSignal(def.Name) {
			defs = append(defs, def)
		}
	}
	return defs
}

func connectSharedEngineSignals(runtime *enginevm.Runtime, store *Store, defs []program.SignalDef) {
	for _, def := range defs {
		if !isSharedSignal(def.Name) {
			continue
		}
		initVal := runtime.EvalExpr(def.Init)
		sharedSig := store.Signal(def.Name, initVal)
		runtime.SetSharedSignal(def.Name, sharedSig)
	}
}

func isSharedSignal(name string) bool {
	return len(name) > 0 && name[0] == '$'
}

func (b *Bridge) connectSharedSignals(island *vm.Island, defs []program.SignalDef) {
	for _, def := range defs {
		initVal := island.EvalExpr(def.Init)
		sharedSig := b.store.Signal(def.Name, initVal)
		island.SetSharedSignal(def.Name, sharedSig)
	}
}

func (b *Bridge) subscribeSharedSignals(islandID string, defs []program.SignalDef) []func() {
	unsubs := make([]func(), 0, len(defs))
	for _, def := range defs {
		unsubs = append(unsubs, b.subscribeSharedSignal(islandID, def))
	}
	return unsubs
}

func (b *Bridge) subscribeSharedSignal(islandID string, def program.SignalDef) func() {
	sig := b.store.Signal(def.Name, vm.ZeroValue(def.Type))
	return sig.Subscribe(func() {
		b.reconcileSharedIsland(islandID)
	})
}

func (b *Bridge) reconcileSharedIsland(islandID string) {
	if b.dispatching == islandID {
		return
	}
	island, ok := b.islands[islandID]
	if !ok {
		return
	}
	b.pushPatches(islandID, island.Reconcile())
}

func (b *Bridge) pushPatches(islandID string, patches []vm.PatchOp) {
	if _, compute := b.computeIslands[islandID]; compute {
		return
	}
	if len(patches) == 0 || b.patchFn == nil {
		return
	}
	patchJSON, err := MarshalPatches(patches)
	if err == nil {
		b.patchFn(islandID, patchJSON)
	}
}

func (b *Bridge) notifySharedSignal(name string, value vm.Value) {
	if b == nil || b.signalFn == nil || strings.TrimSpace(name) == "" {
		return
	}
	valueJSON, err := marshalSharedSignalValue(value)
	if err != nil {
		return
	}
	b.signalFn(name, valueJSON)
}

func marshalSharedSignalValue(value vm.Value) (string, error) {
	data, err := json.Marshal(value.ToAny())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DecodeEngineProgram decodes a runtime engine program.
func DecodeEngineProgram(data []byte, format string) (*rootengine.Program, error) {
	switch format {
	case "", "json":
		return rootengine.DecodeProgramJSON(data)
	default:
		return nil, fmt.Errorf("unknown engine program format %q", format)
	}
}

// MarshalEngineCommands serializes engine commands to JSON.
func MarshalEngineCommands(commands []rootengine.Command) (string, error) {
	data, err := json.Marshal(commands)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MarshalEngineRenderBundle serializes a render bundle to JSON.
func MarshalEngineRenderBundle(bundle rootengine.RenderBundle) (string, error) {
	data, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
