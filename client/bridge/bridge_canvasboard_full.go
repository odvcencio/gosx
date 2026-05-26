//go:build !gosx_tiny_islands_only

package bridge

import (
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
// commands. Mirrors TickEngine for the canvas2d surface.
func (b *Bridge) TickCanvasBoard(id string) ([]rootengine.Command, error) {
	adapter, ok := b.boards[id]
	if !ok {
		return nil, fmt.Errorf("canvas board %q not found", id)
	}
	return adapter.Reconcile(), nil
}

// RenderCanvasBoard builds a 2D-mode render bundle for the named board.
// The bundle's camera is always OrthoCamera2D per Section A.
func (b *Bridge) RenderCanvasBoard(id string, width, height int, timeSeconds float64) (rootengine.RenderBundle, error) {
	adapter, ok := b.boards[id]
	if !ok {
		return rootengine.RenderBundle{}, fmt.Errorf("canvas board %q not found", id)
	}
	return adapter.RenderBundle(width, height, timeSeconds), nil
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
func DecodeCanvasBoardProgram(data []byte, format string) (*rootengine.Program, error) {
	switch format {
	case "", "json":
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
