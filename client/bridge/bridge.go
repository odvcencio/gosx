package bridge

import (
	"encoding/json"
	"errors"
	"fmt"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/client/videosync"
	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Bridge manages active island instances and shared state.
type Bridge struct {
	islands        map[string]*vm.Island
	computeIslands map[string]struct{}
	engines        map[string]*vm.SceneAdapter
	// boards holds canvas2d adapter instances (Phase 2). Typed as the
	// Reconciler interface so the islands-only build can elide the concrete
	// *vm.CanvasBoardAdapter from the binary entirely; the full build's
	// hydrateCanvas2D casts back to *vm.CanvasBoardAdapter for the typed
	// TickCanvasBoard / RenderCanvasBoard entry points.
	boards map[string]vm.Reconciler
	// reconcilers is the unified lifecycle map populated alongside the
	// surface-specific maps above. It exists so callers can count, look up,
	// and (eventually) dispose any reconciler by id without knowing whether
	// it is a DOM island or a scene engine. Surface-specific dispatch
	// (Hydrate*, Dispatch*, Tick*, Render*) still goes through the typed
	// maps — collapsing those is Phase 1d work.
	reconcilers    map[string]vm.Reconciler
	store          *Store
	patchFn        func(islandID, patchJSON string)      // compatibility callback to push JSON patches to JS
	patchMailboxFn func(islandID string, mailbox []byte) // direct binary patch callback
	patchRequestID uint32                                // monotonically increasing id for direct mailbox frames
	signalFn       func(name, valueJSON string)          // callback to notify JS of shared signal changes
	signalBatchFn  func(updates []SignalUpdate)          // callback that hands JS every change of one batch together
	collecting     bool                                  // batch notifications wait in collected for one handoff
	collected      []SignalUpdate                        // changes from the batch being applied, in notification order
	senderNames    []string                              // names the batch sender spelled as aliases, to tell the page under that spelling
	jsonScratch    []byte                                // buffer the notifications are encoded into
	dispatching    map[string]uint32                     // active handler depth by island (same-island depth is capped at one)
	unsubs         map[string][]func()                   // per-island unsubscribe handles for shared signals
	pendingReaders []*sharedIslandReader                 // shared readers awaiting the outermost store update
	hostFactories  map[string]HostReceiverFactory        // receiver name -> per-island capability factory
	hostReceivers  map[string]map[string]vm.HostReceiver // island id -> live receiver bindings

	// engineSurfaces holds live engine-surface VM+canvas+receiver
	// triples keyed by mount id. Populated by HydrateEngineSurface
	// (bridge_engine_surface.go); cleared by DisposeEngineSurface.
	// Stored as a map of opaque interface values so the islands-only
	// build can elide the *engineSurfaceInstance type (and its
	// engine/surface dependency) without changing the Bridge layout.
	engineSurfaces map[string]any

	// videoSyncEngines holds live drift-correction Engine instances keyed by
	// mount id. Populated by NewVideoSync (bridge_video_sync.go); cleared by
	// DisposeVideoSync. Nil until the first NewVideoSync call (lazy init).
	videoSyncEngines map[string]*videosync.Engine

	// crossFrameRelays records the prefix → allowed-origin bindings opted-in
	// via EnableCrossFrameRelay. See cross_frame.go. Both the
	// outbound observer path (relaySharedSignal) and the inbound message
	// listener (DispatchInboundSignal) consult these.
	crossFrameRelays []CrossFrameRelayConfig
	// relaySendFn is invoked when a local write to a relayed-prefix signal
	// needs to postMessage to peer frames. Wired by the wasm-side; see
	// client/wasm/cross_frame.go.
	relaySendFn func(name, valueJSON string)
	// relayInboundDepth suppresses outbound relay during DispatchInboundSignal.
	// Prevents a peer's write from looping back through our outbound relay.
	relayInboundDepth int
}

// sharedIslandReader groups an island's subscriptions. A queued callback from
// a disposed or reloaded program must never render its replacement.
type sharedIslandReader struct {
	id      string
	island  *vm.Island
	pending bool
}

// SetPatchCallback registers the function called when shared signal changes
// trigger a re-render on an island. In WASM, this calls __gosx_apply_patches.
func (b *Bridge) SetPatchCallback(fn func(islandID, patchJSON string)) {
	b.patchFn = fn
}

// SetPatchMailboxCallback registers the direct binary patch channel. When it
// is present, shared-signal reconciliation and action results use the mailbox
// instead of allocating a JSON string. The JSON callback remains available as
// an explicit compatibility fallback for older patch.js assets.
func (b *Bridge) SetPatchMailboxCallback(fn func(islandID string, mailbox []byte)) {
	b.patchMailboxFn = fn
}

// EmitPatches sends an already-reconciled patch list through the selected
// browser boundary. It is exported for WASM action adapters that receive the
// list synchronously rather than through a shared-signal observer.
func (b *Bridge) EmitPatches(islandID string, patches []vm.PatchOp) {
	b.pushPatches(islandID, patches)
}

// Store is a shared signal store that enables cross-island state.
// Any island can read/write shared signals. When a shared signal changes,
// all islands that reference it are re-rendered — like Redux, but reactive.
type Store struct {
	signals     map[string]*signal.Signal[vm.Value]
	onChange    func(name string, value vm.Value)
	updating    bool
	afterUpdate func()
	// afterNotify runs when SetBatch has sent every notification and the
	// update has not settled yet, so readers are still held back.
	afterNotify func()
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
// name is run through signal.ResolveAlias so legacy
// $scene.event.* writes are redirected to the canonical $surface.event.*
// target. Renderer-driven writes already use the canonical names; the alias
// pass here is defensive for hand-rolled callers that haven't migrated.
func (s *Store) Set(name string, val vm.Value) {
	name = signal.ResolveAlias(name)
	if !s.updating {
		s.updating = true
		defer s.endUpdate()
	}
	if sig, ok := s.signals[name]; ok {
		sig.Set(val)
	} else {
		sig := s.Signal(name, val)
		if s.onChange != nil {
			s.onChange(name, sig.Get())
		}
	}
}

// SetBatch creates or updates multiple shared signals in one notification pass.
// Islands reconcile after the outermost batch's notifications have settled,
// including derived values and writes from observers. Signal notifications
// retain their individual delivery semantics.
func (s *Store) SetBatch(values map[string]vm.Value) {
	if len(values) == 0 {
		return
	}
	if !s.updating {
		s.updating = true
		defer s.endUpdate()
	}
	signal.Batch(func() {
		var created []string
		for name, value := range values {
			name = signal.ResolveAlias(name)
			if sig, exists := s.signals[name]; exists {
				sig.Set(value)
			} else {
				s.Signal(name, value)
				created = append(created, name)
			}
		}
		// Newly created signals have no Set notification to defer. Publish
		// their first notification only after the complete batch is installed.
		for _, name := range created {
			if s.onChange != nil {
				s.onChange(name, s.signals[name].Get())
			}
		}
	})
	if s.afterNotify != nil {
		s.afterNotify()
	}
}

// endUpdate keeps readers behind the complete notification pass for both
// individual writes and batches, including reentrant observer writes.
func (s *Store) endUpdate() {
	s.updating = false
	if s.afterUpdate != nil {
		s.afterUpdate()
	}
}

// Get reads a shared signal value.
//
// name is run through signal.ResolveAlias so legacy
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
// The name is run through signal.ResolveAlias before lookup, so
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
		engines:        make(map[string]*vm.SceneAdapter),
		boards:         make(map[string]vm.Reconciler),
		reconcilers:    make(map[string]vm.Reconciler),
		engineSurfaces: make(map[string]any),
		store:          NewStore(),
		unsubs:         make(map[string][]func()),
		hostFactories:  make(map[string]HostReceiverFactory),
		hostReceivers:  make(map[string]map[string]vm.HostReceiver),
	}
	b.store.SetObserver(b.notifySharedSignal)
	b.store.afterUpdate = b.flushSharedIslands
	b.store.afterNotify = b.deliverCollected
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

// SignalUpdate is one shared signal change handed to a browser callback: the
// signal name and the JSON text of its new value.
type SignalUpdate struct {
	Name string
	JSON string
}

// SetSharedSignalBatchCallback registers the function that receives all the
// changes of one shared signal batch in a single call, in notification order.
// Until the batch has applied, nothing reaches the callback, so a subscriber it
// triggers reads the complete new state. The callback also replaces the
// per-signal callback for those changes; any other change still goes to the
// per-signal callback at once, including a write the callback itself makes. A
// signal written that way and also in the batch reaches the browser before the
// batch's own, older, text for it does.
//
// A sender that spelled a signal with a legacy alias ($scene.event.x for
// $surface.event.x) is also told the value under that spelling, after the
// batch's own changes.
func (b *Bridge) SetSharedSignalBatchCallback(fn func(updates []SignalUpdate)) {
	b.signalBatchFn = fn
}

// Surface kinds accepted by HydrateReconciler. Treat these strings as the
// stable cross-VM contract — the JS bootstrap pins them too.
const (
	SurfaceKindDOM      = "dom"
	SurfaceKindScene3D  = "scene3d"
	SurfaceKindCanvas2D = "canvas2d"
)

// HydrateReconciler is the compatibility, error-only generic hydrate entry
// point. It dispatches DOM and Canvas2D through HydrateReconcilerOutput.
//
//   - "dom"      → existing island path (DOM patches via HydrateIsland)
//   - "scene3d"  → rejected before mutation because its output is required
//   - "canvas2d" → CanvasBoardAdapter path (Phase 2 — <CanvasBoard> primitive)
//
// The scene3d and canvas2d paths are gated by build tag — in islands-only
// builds they return an error rather than pulling in the engine reconciler.
// See bridge_reconciler_full.go vs bridge_reconciler_islands.go.
func (b *Bridge) HydrateReconciler(surfaceKind, id, componentName, propsJSON string, programData []byte, format string) error {
	if surfaceKind == SurfaceKindScene3D {
		return errors.New("scene3d hydration requires HydrateReconcilerOutput")
	}
	_, err := b.HydrateReconcilerOutput(surfaceKind, id, componentName, propsJSON, programData, format)
	return err
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
	b.bindIslandHosts(id, island, prog)
	defs := sharedSignalDefs(prog)
	var hydrationPatches []vm.PatchOp
	if len(defs) > 0 {
		// Keep NewIsland's local-init tree as the SSR/DOM baseline, bind every
		// live store instance, then reconcile once against the final shared and
		// computed values. Overwriting prev during binding would make an
		// existing store value invisible to the browser until a later write.
		b.bindSharedSignals(island, defs)
		hydrationPatches = island.Reconcile()
	}
	b.islands[id] = island
	b.unsubs[id] = b.subscribeSharedSignals(id, defs)
	b.reconcilers[id] = island
	if compute {
		b.computeIslands[id] = struct{}{}
	} else {
		delete(b.computeIslands, id)
	}
	b.pushPatches(id, hydrationPatches)
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

	runtime := vm.NewSceneAdapter(prog, propsJSON)
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
	if b.dispatching[islandID] > 0 {
		return nil, fmt.Errorf("island %q is already dispatching", islandID)
	}
	if b.dispatching == nil {
		b.dispatching = make(map[string]uint32)
	}

	// Keep peers behind the complete handler notification pass. Deliver them
	// before the writer reconciles, so peer callbacks can update its result.
	ownsUpdate := !b.store.updating
	var flush func()
	if ownsUpdate {
		b.store.updating = true
		flush = b.store.afterUpdate
	}
	b.dispatching[islandID]++
	defer func() {
		if b.dispatching[islandID] <= 1 {
			delete(b.dispatching, islandID)
		} else {
			b.dispatching[islandID]--
		}
	}()
	// Flush failed handlers before releasing their writer guard. The separate
	// cleanup defer still runs if an observer panics during this fallback.
	defer func() {
		if ownsUpdate && b.store.updating {
			b.store.endUpdate()
		}
	}()

	return island.DispatchWithFlush(handlerName, eventDataJSON, flush), nil
}

// SetSharedSignalJSON updates a shared signal from a JSON payload.
// This is used by hub/websocket clients to drive island state from realtime events.
func (b *Bridge) SetSharedSignalJSON(name, valueJSON string) error {
	value, err := decodeSharedSignalValue(name, valueJSON)
	if err != nil {
		return err
	}
	b.store.Set(name, value)
	return nil
}

// SetSharedSignalBatchJSON updates multiple shared signals from a JSON object.
// The payload shape is {"$signal.name": <json value>, ...}.
func (b *Bridge) SetSharedSignalBatchJSON(batchJSON string) error {
	return b.setSharedSignalBatchJSON(batchJSON, false)
}

// SetChangedSharedSignalBatchJSON applies a state snapshot, notifying only
// signals whose current value differs. Event/pulse writes should continue to
// use SetSharedSignalBatchJSON. Both paths validate the whole batch first.
func (b *Bridge) SetChangedSharedSignalBatchJSON(batchJSON string) error {
	return b.setSharedSignalBatchJSON(batchJSON, true)
}

func (b *Bridge) setSharedSignalBatchJSON(batchJSON string, changedOnly bool) error {
	if batchJSON == "" {
		batchJSON = "{}"
	}

	// Use the same number-preserving decoder as individual writes. Decoding
	// into map[string]any with json.Unmarshal rounds wide integers to float64
	// before the VM can represent them. Reuse the resulting VM map directly.
	payload, err := vm.ValueFromJSON(batchJSON)
	if err != nil {
		return fmt.Errorf("decode shared signal batch: %w", err)
	}
	if !payload.IsMap() {
		// Preserve null as an empty batch, matching map decoding. All other
		// non-object JSON values are invalid batch payloads.
		if payload.Type == program.TypeAny && !payload.IsList() {
			return nil
		}
		return errors.New("decode shared signal batch: expected JSON object")
	}

	values := payload.Map()
	var senderNames []string
	for name, value := range values {
		// A blank name is refused, as the page refused it before it sent a
		// batch here.
		if blankSignalName(name) {
			return errors.New("shared signal name required")
		}
		if signal.ResolveAlias(name) != name {
			senderNames = append(senderNames, name)
		}
		if changedOnly {
			if current, ok := b.store.Get(name); ok && vm.EqualJSON(current, value) {
				delete(values, name)
			}
		}
	}
	if len(values) == 0 && len(senderNames) == 0 {
		return nil
	}
	// The store keeps one signal under the canonical name, and the page keeps
	// one value for each name it has read. A sender that spelled a name as a
	// legacy alias ($scene.event.x for $surface.event.x) is told the value under
	// that spelling as well, so the page's copy of it does not go stale.
	for i := 1; i < len(senderNames); i++ {
		for j := i; j > 0 && senderNames[j] < senderNames[j-1]; j-- {
			senderNames[j], senderNames[j-1] = senderNames[j-1], senderNames[j]
		}
	}

	defer b.beginBatchNotify()()
	b.senderNames = senderNames
	if len(values) > 0 {
		b.store.SetBatch(values)
	} else {
		b.deliverCollected()
	}
	return nil
}

// blankSignalName reports whether name is empty once the white space that
// JavaScript's String.prototype.trim removes is gone. The page trims signal
// names with trim, so the two sides must refuse the same names.
func blankSignalName(name string) bool {
	for _, r := range name {
		if !isJSSpace(r) {
			return false
		}
	}
	return true
}

// isJSSpace is ECMAScript's WhiteSpace and LineTerminator set: tab, line feed,
// vertical tab, form feed, carriage return, space, no-break space, the byte
// order mark, the line and paragraph separators, and the other code points in
// the Unicode category Zs (U+1680, U+2000 to U+200A, U+202F, U+205F, U+3000). It
// differs from unicode.IsSpace, which trims U+0085 and does not trim U+FEFF.
// TestBlankSignalNameUsesTheWhiteSpaceJavaScriptTrims checks the set against
// the 25 code points that String.prototype.trim removes.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// beginBatchNotify prepares the notifications of one batch and returns the
// function that ends it. While the batch applies, the browser hears nothing;
// every change waits and then leaves together through the batch callback. The
// returned function sends whatever is still waiting, so an observer panic
// cannot leave the browser behind the store.
func (b *Bridge) beginBatchNotify() func() {
	collecting, collected, senderNames := b.collecting, b.collected, b.senderNames
	b.collecting = b.signalBatchFn != nil
	b.collected = nil
	b.senderNames = nil
	return func() {
		b.deliverCollected()
		b.collecting, b.collected, b.senderNames = collecting, collected, senderNames
	}
}

// deliverCollected hands the waiting batch changes to the batch callback. The
// callback may write signals; those changes go out one by one, not into the
// batch being delivered. A signal the callback writes that is also in the batch
// reaches the browser before the batch does, so the browser must treat the
// batch's copy of it as out of date (the page's gosxNotifySharedSignals does).
func (b *Bridge) deliverCollected() {
	b.collectSenderNames()
	if len(b.collected) == 0 {
		return
	}
	updates := b.collected
	b.collected = nil
	collecting := b.collecting
	b.collecting = false
	defer func() { b.collecting = collecting }()
	b.signalBatchFn(updates)
}

// collectSenderNames adds the legacy spellings the batch sender used to the
// changes that are waiting, each with the current value of its signal. Without a
// batch callback they go to the per-signal callback at once.
func (b *Bridge) collectSenderNames() {
	names := b.senderNames
	b.senderNames = nil
	for _, name := range names {
		value, ok := b.store.Get(name)
		if !ok {
			continue
		}
		text, err := b.marshalValue(value)
		if err != nil {
			continue
		}
		if b.collecting {
			b.collected = append(b.collected, SignalUpdate{Name: name, JSON: text})
		} else if b.signalFn != nil {
			b.signalFn(name, text)
		}
	}
}

// GetSharedSignalJSON serializes the current shared signal value as JSON.
// Missing signals return JSON null.
func (b *Bridge) GetSharedSignalJSON(name string) (string, error) {
	if blankSignalName(name) {
		return "", errors.New("shared signal name required")
	}
	value, ok := b.store.Get(name)
	if !ok {
		return "null", nil
	}
	return b.marshalValue(value)
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
//
// The encoder is hand-rolled and byte-identical to encoding/json — see
// patch_json.go for why, and patch_json_test.go for the equality proof. The
// error return stays for source compatibility; this path cannot fail.
func MarshalPatches(patches []vm.PatchOp) (string, error) {
	buf := make([]byte, 0, patchesJSONSize(patches))
	return string(appendPatchesJSON(buf, patches)), nil
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
		b.disposeIslandHosts(id, island)
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
// must type-assert to *vm.Island or *vm.SceneAdapter.
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

func decodeSharedSignalValue(name, raw string) (vm.Value, error) {
	if name == "" {
		return vm.Value{}, errors.New("shared signal name required")
	}
	if raw == "" {
		raw = "null"
	}

	value, err := vm.ValueFromJSON(raw)
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

func connectSharedEngineSignals(runtime *vm.SceneAdapter, store *Store, defs []program.SignalDef) {
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

func (b *Bridge) bindSharedSignals(island *vm.Island, defs []program.SignalDef) {
	for _, def := range defs {
		initVal := island.EvalExpr(def.Init)
		sharedSig := b.store.Signal(def.Name, initVal)
		island.BindSharedSignal(def.Name, sharedSig)
	}
}

func (b *Bridge) subscribeSharedSignals(islandID string, defs []program.SignalDef) []func() {
	if len(defs) == 0 {
		return nil
	}
	reader := &sharedIslandReader{id: islandID, island: b.islands[islandID]}
	notify := func() { b.reconcileSharedIsland(reader) }
	unsubs := make([]func(), 0, len(defs)+1)
	unsubs = append(unsubs, func() { reader.island = nil })
	for _, def := range defs {
		sig := b.store.Signal(def.Name, vm.ZeroValue(def.Type))
		unsubs = append(unsubs, sig.Subscribe(notify))
	}
	return unsubs
}

func (b *Bridge) reconcileSharedIsland(reader *sharedIslandReader) {
	if reader.island == nil || b.dispatching[reader.id] > 0 {
		return
	}
	if b.store.updating {
		if !reader.pending {
			reader.pending = true
			b.pendingReaders = append(b.pendingReaders, reader)
		}
		return
	}
	b.pushPatches(reader.id, reader.island.Reconcile())
}

func (b *Bridge) flushSharedIslands() {
	b.store.updating = false
	// Detach before rendering: patch callbacks may start a separate batch.
	// Its pending readers belong to that batch, not the one being drained.
	pending := b.pendingReaders
	b.pendingReaders = nil
	for _, reader := range pending {
		reader.pending = false
	}
	for i, reader := range pending {
		pending[i] = nil
		b.reconcileSharedIsland(reader)
	}
	if b.pendingReaders == nil {
		b.pendingReaders = pending[:0]
	}
}

func (b *Bridge) pushPatches(islandID string, patches []vm.PatchOp) {
	if _, compute := b.computeIslands[islandID]; compute {
		return
	}
	if len(patches) == 0 {
		return
	}
	if b.patchMailboxFn != nil {
		b.patchRequestID++
		mailbox, err := runtimewasm.EncodePatchMailbox(islandID, b.patchRequestID, patches)
		if err == nil {
			b.patchMailboxFn(islandID, mailbox)
		}
		return
	}
	if b.patchFn == nil {
		return
	}
	patchJSON, err := MarshalPatches(patches)
	if err == nil {
		b.patchFn(islandID, patchJSON)
	}
}

func (b *Bridge) notifySharedSignal(name string, value vm.Value) {
	if b == nil || blankSignalName(name) {
		return
	}
	valueJSON, err := b.marshalValue(value)
	if err != nil {
		return
	}
	if b.collecting {
		b.collected = append(b.collected, SignalUpdate{Name: name, JSON: valueJSON})
	} else if b.signalFn != nil {
		b.signalFn(name, valueJSON)
	}
	// Outbound cross-frame relay fans out for matching prefixes only.
	// Separate from the JS notify path so single-frame consumers are
	// unaffected; see cross_frame.go.
	b.relaySharedSignal(name, valueJSON)
}

// marshalValue returns the JSON text of a stored value: what json.Marshal gives
// for value.ToAny(), written without the tree of plain Go values that call
// builds. The bridge keeps one buffer for it, so a notification costs the text
// itself and nothing else.
func (b *Bridge) marshalValue(value vm.Value) (string, error) {
	data, err := vm.AppendJSON(b.jsonScratch[:0], value)
	if err != nil {
		return "", err
	}
	// Keep the buffer for the next value unless one outsized value grew it.
	if cap(data) <= maxJSONScratch {
		b.jsonScratch = data
	} else {
		b.jsonScratch = nil
	}
	return string(data), nil
}

// maxJSONScratch is the largest buffer the bridge keeps between notifications.
const maxJSONScratch = 1 << 20

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
