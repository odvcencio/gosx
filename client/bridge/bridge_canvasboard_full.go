//go:build !gosx_tiny_islands_only

package bridge

import (
	"bytes"
	"fmt"

	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// hydrateCanvas2D constructs a *vm.CanvasBoardAdapter for the canvas2d
// surface kind and registers it in the boards + reconcilers maps. Build-tag
// paired with bridge_canvasboard_islands.go (which stubs out the path so
// the islands-only WASM build can drop the adapter entirely).
//
// The adapter operates on the same shared signal store the scene3d path
// uses, so a single <CanvasBoard> page picks up cross-island reactivity
// without extra wiring. Pick events flow into $surface.event.* per ADR 0007.
func (b *Bridge) hydrateCanvas2D(id, componentName, propsJSON string, programData []byte, format string) error {
	prog, err := DecodeCanvasBoardProgram(programData, format)
	if err != nil {
		return fmt.Errorf("decode canvas2d program %q: %w", componentName, err)
	}
	propsJSON, err = normalizePropsJSON(componentName, propsJSON)
	if err != nil {
		return err
	}
	if existing, ok := b.boards[id]; ok {
		existing.Dispose()
		delete(b.boards, id)
		delete(b.reconcilers, id)
	}

	adapter := vm.NewCanvasBoardAdapter(prog, propsJSON)
	connectSharedBoardSignals(adapter, b.store, prog.Signals)

	b.boards[id] = adapter
	b.reconcilers[id] = adapter
	return nil
}

// connectSharedBoardSignals wires every $-prefixed signal declared by prog
// into the shared store. Mirrors connectSharedEngineSignals but for the
// CanvasBoardAdapter type. Pulled out into its own helper so the islands-only
// stub can avoid pulling in the dependency.
func connectSharedBoardSignals(adapter *vm.CanvasBoardAdapter, store *Store, defs []islandprogram.SignalDef) {
	for _, def := range defs {
		if !isSharedSignal(def.Name) {
			continue
		}
		initVal := adapter.EvalExpr(def.Init)
		sharedSig := store.Signal(def.Name, initVal)
		adapter.SetSharedSignal(def.Name, sharedSig)
	}
}

// TickCanvasBoard reconciles a live board adapter and returns pending
// commands. Mirrors TickEngine for the canvas2d surface. The boards map
// stores values as the vm.Reconciler interface (so the islands-only build
// can drop the concrete adapter from the binary); this entry point casts
// back to the typed adapter for the Reconcile call's Command return.
func (b *Bridge) TickCanvasBoard(id string) ([]rootengine.Command, error) {
	entry, ok := b.boards[id]
	if !ok {
		return nil, fmt.Errorf("canvas board %q not found", id)
	}
	adapter, ok := entry.(*vm.CanvasBoardAdapter)
	if !ok {
		return nil, fmt.Errorf("board %q is not a canvas board adapter", id)
	}
	return adapter.Reconcile(), nil
}

// RenderCanvasBoard builds a 2D-mode render bundle for the named board.
// The bundle's camera is always OrthoCamera2D per Section A.
func (b *Bridge) RenderCanvasBoard(id string, width, height int, timeSeconds float64) (rootengine.RenderBundle, error) {
	entry, ok := b.boards[id]
	if !ok {
		return rootengine.RenderBundle{}, fmt.Errorf("canvas board %q not found", id)
	}
	adapter, ok := entry.(*vm.CanvasBoardAdapter)
	if !ok {
		return rootengine.RenderBundle{}, fmt.Errorf("board %q is not a canvas board adapter", id)
	}
	return adapter.RenderBundle(width, height, timeSeconds), nil
}

// SetCanvasBoardBackend routes the named board's per-frame RenderBundle to a
// render backend. "webgpu" makes every subsequent RenderCanvasBoard carry GPU
// geometry (boardgpu.AttachBoardGPUGeometry) for the 16a JS WebGPU renderer;
// any other value (including "") keeps the painter bundle the 26b1 2D-context
// painter consumes — byte-for-byte unchanged. The JS surface calls this through
// __gosx_canvas_set_backend AFTER hydration, only when its canvas2d element
// opted into WebGPU and the GPU path is genuinely available (probe + factory
// present); a failed probe leaves the painter default in place. Mirrors
// CanvasBoardEvent's per-board, post-hydrate mutation shape — the established
// channel for the JS side to configure an already-hydrated board (NOT a new
// hydrate arg). Returns an error for an unknown board id.
func (b *Bridge) SetCanvasBoardBackend(id, backend string) error {
	adapter, err := b.canvasBoardAdapter(id)
	if err != nil {
		return err
	}
	adapter.SetRenderBackend(backend)
	return nil
}

// UpdateCanvasBoardHTMLMarkup patches one static html node's markup on a live
// board. It is intentionally scoped to existing CanvasBoard html nodes; callers
// cannot create new nodes or mutate geometry through this hook.
func (b *Bridge) UpdateCanvasBoardHTMLMarkup(id, htmlID, markup string) error {
	adapter, err := b.canvasBoardAdapter(id)
	if err != nil {
		return err
	}
	if !adapter.UpdateHTMLMarkup(htmlID, markup) {
		return fmt.Errorf("canvas board %q html node %q not found", id, htmlID)
	}
	return nil
}

// DisposeCanvasBoard tears down a canvas2d adapter. Idempotent.
func (b *Bridge) DisposeCanvasBoard(id string) {
	if adapter, ok := b.boards[id]; ok {
		adapter.Dispose()
		delete(b.boards, id)
	}
	delete(b.reconcilers, id)
}

// DecodeCanvasBoardProgram decodes a wire-format Canvas2D program. Mirrors
// DecodeEngineProgram but tags the result as SurfaceCanvas2D so downstream
// code can route correctly even on minimal programs.
//
// A static CanvasBoard is a no-code primitive: gosx.CanvasBoard emits no
// program data, and the browser bootstrap passes a valid-empty "{}" for
// canvas2d placeholders. To keep that path crash-free, an empty (nil, "", or
// whitespace-only) payload is treated as the empty object "{}" rather than
// surfaced as json.Unmarshal's "unexpected end of JSON input". Genuinely
// malformed JSON still errors.
func DecodeCanvasBoardProgram(data []byte, format string) (*rootengine.Program, error) {
	switch format {
	case "", "json":
		if len(bytes.TrimSpace(data)) == 0 {
			data = []byte("{}")
		}
		prog, err := rootengine.DecodeProgramJSON(data)
		if err != nil {
			return nil, err
		}
		prog.Surface = islandprogram.SurfaceCanvas2D
		return prog, nil
	default:
		return nil, fmt.Errorf("unknown canvas2d program format %q", format)
	}
}
