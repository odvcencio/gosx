//go:build gosx_tiny_islands_only

// Engine-surface hydration stubs — islands-only build.
//
// The islands-only build omits the engine/surface and engine packages
// entirely; this file substitutes no-op entry points so the bridge API
// surface remains consistent. Any caller that lands in a tiny build
// receives a clean "unavailable" error from HydrateEngineSurface; the
// tick / dispatch / dispose paths are silent no-ops mirroring the
// production semantics for unknown ids.

package bridge

import "fmt"

// engineSurfaceInstance is an opaque placeholder so the bridge struct
// layout matches the full build. Methods that take it are unreachable
// in this build because HydrateEngineSurface always errors out before
// inserting an entry.
type engineSurfaceInstance struct{}

// EngineSurfaceEventKind matches the full-build enum so JS callers can
// share a single binding regardless of build flavor.
type EngineSurfaceEventKind int

const (
	EngineSurfaceEventMount         EngineSurfaceEventKind = 0
	EngineSurfaceEventClick         EngineSurfaceEventKind = 1
	EngineSurfaceEventDblClick      EngineSurfaceEventKind = 2
	EngineSurfaceEventPointerDown   EngineSurfaceEventKind = 3
	EngineSurfaceEventPointerMove   EngineSurfaceEventKind = 4
	EngineSurfaceEventPointerUp     EngineSurfaceEventKind = 5
	EngineSurfaceEventPointerCancel EngineSurfaceEventKind = 6
	EngineSurfaceEventWheel         EngineSurfaceEventKind = 7
	EngineSurfaceEventKeyDown       EngineSurfaceEventKind = 8
	EngineSurfaceEventKeyUp         EngineSurfaceEventKind = 9
	EngineSurfaceEventResize        EngineSurfaceEventKind = 10
	EngineSurfaceEventDispose       EngineSurfaceEventKind = 11
)

// HydrateEngineSurface returns an error in the islands-only build —
// the engine/surface dependency is intentionally elided. canvasPlaceholder
// is typed as any so the islands-only caller doesn't need to import
// engine/surface either.
func (b *Bridge) HydrateEngineSurface(id, componentName, propsJSON string, programData []byte, format string, canvas any) error {
	return fmt.Errorf("engine surface hydration unavailable in islands-only build")
}

// DispatchEngineSurfaceEvent is a silent no-op in the islands-only
// build — there is no instance to dispatch into.
func (b *Bridge) DispatchEngineSurfaceEvent(id string, kind EngineSurfaceEventKind, floats []float64, payloadStr string) error {
	return nil
}

// TickEngineSurface is a silent no-op in the islands-only build.
func (b *Bridge) TickEngineSurface(id string, n int) error { return nil }

// DisposeEngineSurface is a silent no-op in the islands-only build.
func (b *Bridge) DisposeEngineSurface(id string) {}

// EngineSurfaceCount always returns 0 in the islands-only build.
func (b *Bridge) EngineSurfaceCount() int { return 0 }
