// Package runtime is the surface engine runtime registry.
//
// It is imported by the generated main.go (produced by internal/buildsurface)
// that Chunk C compiles into a WASM module. The package has two responsibilities:
//
//  1. On the WASM target (js+wasm): expose globalThis.__gosx_surface_* symbols
//     that the JS bootstrap calls to drive mounting, events, and animation.
//  2. On the host (native): compile to a no-op stub so that packages that
//     import surface/runtime for test purposes are buildable without a browser.
//
// # Event payload protocol
//
// The JS bootstrap sends events to WASM via __gosx_surface_event(id, kind, payload, payloadStr).
// The id is an opaque string identifying the mounted surface instance.
// The kind is a uint8 byte selecting the event type (see table below).
// The payload is a Float64Array transferred as a JS typed array.
// The payloadStr is a JS string carrying variable-length string data.
//
// | kind | event           | payload []float64        | payloadStr         |
// |------|-----------------|--------------------------|-------------------|
// |  0   | mount           | (none)                   | (none) — props in data-attr |
// |  1   | click           | [x,y,button,buttons,mod] | ""                |
// |  2   | dblclick        | [x,y,button,buttons,mod] | ""                |
// |  3   | pointerdown     | [x,y,button,buttons,mod] | ""                |
// |  4   | pointermove     | [x,y,button,buttons,mod] | ""                |
// |  5   | pointerup       | [x,y,button,buttons,mod] | ""                |
// |  6   | pointercancel   | [x,y,button,buttons,mod] | ""                |
// |  7   | wheel           | [x,y,dx,dy,mod]          | ""                |
// |  8   | keydown         | [mod]                    | key+"\t"+code     |
// |  9   | keyup           | [mod]                    | key+"\t"+code     |
// | 10   | resize          | [w,h,dpr]                | ""                |
// | 11   | dispose         | (none)                   | ""                |
//
// The mod float64 encodes the Modifier bitfield as an integer:
// bit 0 = Shift, bit 1 = Ctrl, bit 2 = Alt, bit 3 = Meta.
//
// Animation frames arrive via __gosx_surface_frame(id, timestamp float64).
// The timestamp is the DOMHighResTimeStamp in milliseconds.
//
// # JS bootstrap expectations (integration risks)
//
// Chunk C's JS bootstrap must:
//   - Call __gosx_surface_register(name) for each mounted component name after
//     the WASM module is instantiated. The name must match the string passed to
//     Register() in the generated main.go.
//   - Call __gosx_surface_event(id, kind, payload, payloadStr) for user events.
//     payload must be a Float64Array view; payloadStr must be a JS string.
//   - Call __gosx_surface_frame(id, ts) on each requestAnimationFrame tick when
//     the surface has requested a frame via __gosx_surface_request_frame(id).
//   - Call __gosx_surface_dispose(id) when the mount element is removed from the DOM.
package runtime

// Surface holds the typed user event handlers for a single surface component.
// All fields are optional; nil handlers are silently ignored at dispatch time.
// The generated main.go populates this struct using surface.Wrap* adapters.
//
// Each handler receives a typed payload constructed by the runtime dispatch layer.
// The actual Go types are defined in engine/surface (PointerEvent, etc.) — this
// package carries func(any) to avoid an import cycle.
type Surface struct {
	// OnMount is called once when the canvas is attached and props are available.
	// Payload type: *surface.mountPayload (ctx + canvas + props).
	OnMount func(any)

	// OnClick is called on a left-button click.
	// Payload type: *surface.pointerPayload.
	OnClick func(any)

	// OnDblClick is called on a double-click.
	OnDblClick func(any)

	// OnPointerDown is called when a pointer button is pressed.
	OnPointerDown func(any)

	// OnPointerMove is called when the pointer moves over the canvas.
	OnPointerMove func(any)

	// OnPointerUp is called when a pointer button is released.
	OnPointerUp func(any)

	// OnPointerCancel is called when a pointer event is cancelled.
	OnPointerCancel func(any)

	// OnWheel is called on a wheel/scroll event.
	// Payload type: *surface.wheelPayload.
	OnWheel func(any)

	// OnKeyDown is called when a key is pressed (canvas must have focus).
	// Payload type: *surface.keyPayload.
	OnKeyDown func(any)

	// OnKeyUp is called when a key is released.
	OnKeyUp func(any)

	// OnResize is called when the canvas element is resized.
	// Payload type: *surface.resizePayload.
	OnResize func(any)

	// OnDispose is called just before the canvas is detached from the DOM.
	// Payload type: *surface.disposePayload.
	OnDispose func(any)
}

// Register registers a surface component by name.
// Called by the generated main.go exactly once per component.
// The name must match the component name attribute emitted in the SSR placeholder.
func Register(name string, s Surface) {
	registerSurface(name, s)
}
