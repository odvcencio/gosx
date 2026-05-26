package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"m31labs.dev/gosx/client/enginevm"
	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Bridge manages active island instances and shared state.
type Bridge struct {
	islands        map[string]*vm.Island
	computeIslands map[string]struct{}
	engines        map[string]*enginevm.Runtime
	// boards holds canvas2d adapter instances (Phase 2). Kept in a separate
	// typed map from engines because *vm.CanvasBoardAdapter and
	// *enginevm.Runtime (alias for *vm.SceneAdapter) are different concrete
	// types — the unification across both lives in reconcilers below.
	boards map[string]*vm.CanvasBoardAdapter
	// reconcilers is the unified lifecycle map populated alongside the
	// surface-specific maps above. It exists so callers can count, look up,
	// and (eventually) dispose any reconciler by id without knowing whether
	// it is a DOM island or a scene engine. Surface-specific dispatch
	// (Hydrate*, Dispatch*, Tick*, Render*) still goes through the typed
	// maps — collapsing those is Phase 1d work.
	reconcilers map[string]vm.Reconciler
	store       *Store
	patchFn     func(islandID, patchJSON string) // callback to push patches to JS
	signalFn    func(name, valueJSON string)     // callback to notify JS of shared signal changes
	dispatching string                           // ID of the island currently dispatching
	unsubs      map[string][]func()              // per-island unsubscribe handles for shared signals
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
//
// Per ADR 0007, name is run through signal.ResolveAlias so legacy
// $scene.event.* writes are redirected to the canonical $surface.event.*
// target. Renderer-driven writes already use the canonical names; the alias
// pass here is defensive for hand-rolled callers that haven't migrated.
func (s *Store) Set(name string, val vm.Value) {
	name = signal.ResolveAlias(name)
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
//
// Per ADR 0007, name is run through signal.ResolveAlias so legacy
// $scene.event.* reads transparently forward to the canonical
// $surface.event.* target. A subscriber reading $scene.event.selectedID
// after a renderer-driven write to $surface.event.selectedID gets the
// fresh value.
func (s *Store) Get(name string) (vm.Value, bool) {
	name = signal.ResolveAlias(name)
	if sig, ok := s.signals[name]; ok {
		return sig.Get(), true
	}
	return vm.ZeroValue(0), false
}

// Signal returns a shared signal, creating it with the given initial value
// if it doesn't exist yet. If it already exists, the init value is ignored
// (first island to declare wins).
//
// Per ADR 0007 the name is run through signal.ResolveAlias before lookup, so
// an island declaring a dependency on the legacy $scene.event.X automatically
// hooks into the canonical $surface.event.X signal.
func (s *Store) Signal(name string, init vm.Value) *signal.Signal[vm.Value] {
	name = signal.ResolveAlias(name)
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
		boards:         make(map[string]*vm.CanvasBoardAdapter),
		reconcilers:    make(map[string]vm.Reconciler),
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

// Surface kinds accepted by HydrateReconciler. Treat these strings as the
// stable cross-VM contract — the JS bootstrap pins them too.
const (
	SurfaceKindDOM      = "dom"
	SurfaceKindScene3D  = "scene3d"
	SurfaceKindCanvas2D = "canvas2d"
)

// HydrateReconciler is the unified hydrate entry point introduced in Phase 1d.
// It dispatches by surfaceKind to the appropriate adapter constructor:
//
//   - "dom"      → existing island path (DOM patches via HydrateIsland)
//   - "scene3d"  → existing scene-engine path (engine commands via HydrateEngine)
//   - "canvas2d" → CanvasBoardAdapter path (Phase 2 — <CanvasBoard> primitive)
//
// The scene3d and canvas2d paths are gated by build tag — in islands-only
// builds they return an error rather than pulling in the engine reconciler.
// See bridge_reconciler_full.go vs bridge_reconciler_islands.go.
//
// Engine commands produced by the scene3d / canvas2d paths are discarded
// here; the legacy HydrateEngine remains for callers that need the initial
// command stream. Phase 2 will widen the return shape if needed.
func (b *Bridge) HydrateReconciler(surfaceKind, id, componentName, propsJSON string, programData []byte, format string) error {
	switch surfaceKind {
	case SurfaceKindDOM:
		return b.HydrateIsland(id, componentName, propsJSON, programData, format)
	case SurfaceKindScene3D:
		return b.hydrateScene3D(id, componentName, propsJSON, programData, format)
	case SurfaceKindCanvas2D:
		return b.hydrateCanvas2D(id, componentName, propsJSON, programData, format)
	default:
		return fmt.Errorf("unknown surfaceKind %q (expected one of: %q, %q, %q)",
			surfaceKind, SurfaceKindDOM, SurfaceKindScene3D, SurfaceKindCanvas2D)
	}
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
	b.reconcilers[id] = island
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
	b.reconcilers[id] = runtime

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
	delete(b.reconcilers, id)
}

// DisposeEngine removes a live engine runtime from the bridge.
func (b *Bridge) DisposeEngine(id string) {
	if runtime, ok := b.engines[id]; ok {
		runtime.Dispose()
		delete(b.engines, id)
	}
	delete(b.reconcilers, id)
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

// CanvasBoardCount reports the number of live canvas2d adapters. Available
// in every build (returns 0 in islands-only where boards are never
// constructed).
func (b *Bridge) CanvasBoardCount() int {
	return len(b.boards)
}

// ReconcilerCount returns the number of active reconcilers across all surface
// kinds (islands + compute islands + engines). This is the unified lifecycle
// view introduced in Phase 1b; the surface-specific count methods above stay
// for callers that need to distinguish kinds.
func (b *Bridge) ReconcilerCount() int {
	return len(b.reconcilers)
}

// LookupReconciler returns the reconciler registered under id, if any. The
// returned value satisfies the vm.Reconciler lifecycle interface; callers
// that need surface-specific behavior (PatchOp emission, Command emission)
// must type-assert to *vm.Island or *enginevm.Runtime.
func (b *Bridge) LookupReconciler(id string) (vm.Reconciler, bool) {
	r, ok := b.reconcilers[id]
	return r, ok
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
