package bridge

import "fmt"

// ReloadProgram hot-swaps the compiled program backing a live island, in
// place, preserving signal state by name. It is the bridge-side seam the
// `gosx dev` reload path drives (and the __gosx_reload_program WASM export
// wraps): decode the new bytecode, look the island up by id, swap, and push
// the reconcile patches to JS so the DOM updates without a page reload.
//
// Unknown islandID is a no-op error — callers (a dev socket, the WASM export)
// surface it rather than silently dropping the reload. A decode failure is
// likewise returned before the live island is touched, so a malformed payload
// never corrupts running state.
//
// Shared signals are re-wired against the new program the same way hydrate
// does: the old per-island subscriptions are torn down, the new program's
// shared SignalDefs are reconnected to the store, and fresh subscriptions are
// installed. Combined with VM.SwapProgram's merge-by-name (which keeps the
// existing shared *signal instance* for any name the new program still
// declares), this preserves cross-island shared state across a reload while
// honoring any signals the new program adds or drops.
//
// Concurrency: like DispatchAction, ReloadProgram must run to completion on the
// runtime's single thread. The unsubscribe-before-swap / resubscribe-after
// sequence below is not individually atomic; its safety relies on no store Set
// (and therefore no subscription-driven reconcile) interleaving mid-swap. That
// holds because the WASM runtime is single-threaded and event-loop driven. A
// future caller that drives reloads from a background goroutine must add its
// own synchronization.
func (b *Bridge) ReloadProgram(islandID string, data []byte, format string) error {
	prog, err := DecodeProgram(data, format)
	if err != nil {
		return fmt.Errorf("reload program %q: %w", islandID, err)
	}

	island, ok := b.islands[islandID]
	if !ok {
		return fmt.Errorf("island %q not found", islandID)
	}

	// Tear down the old island's shared-signal subscriptions before the
	// swap so a store change can't fire a reconcile against a half-swapped
	// island. Mirrors the unsubscribe half of DisposeIsland.
	if unsubs, ok := b.unsubs[islandID]; ok {
		for _, unsub := range unsubs {
			unsub()
		}
		delete(b.unsubs, islandID)
	}

	// Swap the program in place. SwapProgram merges signals by name (keeping
	// the live shared-signal instances connected to the store) and Island.
	// SwapProgram rebuilds the handler map and reconciles, returning the
	// patch set that brings the DOM current.
	patches := island.SwapProgram(prog)

	// Reconnect + re-subscribe shared signals against the new program. New
	// names get connected to the store; names that persisted keep the value
	// merge-by-name preserved.
	defs := sharedSignalDefs(prog)
	b.connectSharedSignals(island, defs)
	b.unsubs[islandID] = b.subscribeSharedSignals(islandID, defs)

	// Forward the reconcile patches to JS (no-op for compute islands or when
	// no patch callback is wired).
	b.pushPatches(islandID, patches)
	return nil
}
