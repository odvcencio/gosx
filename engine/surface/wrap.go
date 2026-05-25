package surface

import (
	"encoding/json"
)

// --- Public payload constructors for use by the runtime package (canvas_js.go) ---

// NewMountPayload constructs the payload passed to WrapMount handlers.
func NewMountPayload(ctx *Context, c *Canvas, props []byte) any {
	return &mountPayload{Ctx: ctx, Canvas: c}
}

// NewPointerPayload constructs a pointer event payload from a float64 packet.
// Packet layout: [x, y, button, buttons, mod]
func NewPointerPayload(ctx *Context, c *Canvas, f []float64) any {
	ev := PointerEvent{}
	if len(f) >= 5 {
		ev.X = f[0]
		ev.Y = f[1]
		ev.Button = int(f[2])
		ev.Buttons = int(f[3])
		ev.Modifier = Modifier(f[4])
	}
	return &pointerPayload{Ctx: ctx, Canvas: c, Event: ev}
}

// NewWheelPayload constructs a wheel event payload from a float64 packet.
// Packet layout: [x, y, deltaX, deltaY, mod]
func NewWheelPayload(ctx *Context, c *Canvas, f []float64) any {
	ev := WheelEvent{}
	if len(f) >= 5 {
		ev.X = f[0]
		ev.Y = f[1]
		ev.DeltaX = f[2]
		ev.DeltaY = f[3]
		ev.Modifier = Modifier(f[4])
	}
	return &wheelPayload{Ctx: ctx, Canvas: c, Event: ev}
}

// NewKeyPayload constructs a key event payload.
// Packet layout: [mod]; key and code come from the string payload.
func NewKeyPayload(ctx *Context, c *Canvas, f []float64, key, code string) any {
	ev := KeyEvent{Key: key, Code: code}
	if len(f) >= 1 {
		ev.Modifier = Modifier(f[0])
	}
	return &keyPayload{Ctx: ctx, Canvas: c, Event: ev}
}

// NewResizePayload constructs a resize event payload from a float64 packet.
// Packet layout: [width, height, dpr]
func NewResizePayload(ctx *Context, c *Canvas, f []float64) any {
	ev := ResizeEvent{}
	if len(f) >= 3 {
		ev.Width = int(f[0])
		ev.Height = int(f[1])
		ev.DPR = f[2]
	}
	return &resizePayload{Ctx: ctx, Canvas: c, Event: ev}
}

// NewDisposePayload constructs a dispose payload.
func NewDisposePayload(ctx *Context) any {
	return &disposePayload{Ctx: ctx}
}

// WrapMount lifts a typed mount handler into the runtime registration shape.
//
// The generated main.go calls:
//
//	runtime.Surface{OnMount: surface.WrapMount(user.Mount)}
//
// At runtime, OnMount receives a json.RawMessage as any. WrapMount decodes it
// into a Context and constructs a Canvas bound to the platform impl, then calls fn.
func WrapMount(fn func(*Context, *Canvas)) func(any) {
	return func(v any) {
		ctx, c := unpackMountPayload(v)
		fn(ctx, c)
	}
}

// WrapPointer lifts a typed pointer event handler.
func WrapPointer(fn func(*Context, *Canvas, PointerEvent)) func(any) {
	return func(v any) {
		ctx, c, ev := unpackPointerPayload(v)
		fn(ctx, c, ev)
	}
}

// WrapWheel lifts a typed wheel event handler.
func WrapWheel(fn func(*Context, *Canvas, WheelEvent)) func(any) {
	return func(v any) {
		ctx, c, ev := unpackWheelPayload(v)
		fn(ctx, c, ev)
	}
}

// WrapKey lifts a typed keyboard event handler.
func WrapKey(fn func(*Context, *Canvas, KeyEvent)) func(any) {
	return func(v any) {
		ctx, c, ev := unpackKeyPayload(v)
		fn(ctx, c, ev)
	}
}

// WrapResize lifts a typed resize event handler.
func WrapResize(fn func(*Context, *Canvas, ResizeEvent)) func(any) {
	return func(v any) {
		ctx, c, ev := unpackResizePayload(v)
		fn(ctx, c, ev)
	}
}

// WrapDispose lifts a zero-argument dispose handler.
// The dispose handler receives neither a Canvas nor event; it is called when
// the surface is torn down to allow cleanup.
func WrapDispose(fn func(*Context)) func(any) {
	return func(v any) {
		ctx := unpackContextOnly(v)
		fn(ctx)
	}
}

// --- internal payload types ---

// mountPayload is the runtime-side payload for an OnMount call.
// The runtime package constructs this and passes it as any to WrapMount.
type mountPayload struct {
	Ctx    *Context
	Canvas *Canvas
}

// pointerPayload is the runtime-side payload for pointer events.
type pointerPayload struct {
	Ctx    *Context
	Canvas *Canvas
	Event  PointerEvent
}

// wheelPayload is the runtime-side payload for wheel events.
type wheelPayload struct {
	Ctx    *Context
	Canvas *Canvas
	Event  WheelEvent
}

// keyPayload is the runtime-side payload for key events.
type keyPayload struct {
	Ctx    *Context
	Canvas *Canvas
	Event  KeyEvent
}

// resizePayload is the runtime-side payload for resize events.
type resizePayload struct {
	Ctx    *Context
	Canvas *Canvas
	Event  ResizeEvent
}

// disposePayload is the runtime-side payload for dispose.
type disposePayload struct {
	Ctx *Context
}

// --- unpack helpers ---
// These perform type assertions against the structs above. If the value is a
// json.RawMessage (as might be produced in tests), they fall back to a minimal
// JSON-based decode so that unit tests can exercise the wrappers without a live
// WASM runtime.

func unpackMountPayload(v any) (*Context, *Canvas) {
	if p, ok := v.(*mountPayload); ok {
		return p.Ctx, p.Canvas
	}
	// JSON fallback: decode props bytes into a bare context.
	props := asRawMessage(v)
	return newContext(props), newNoopCanvas()
}

func unpackPointerPayload(v any) (*Context, *Canvas, PointerEvent) {
	if p, ok := v.(*pointerPayload); ok {
		return p.Ctx, p.Canvas, p.Event
	}
	return newContext(nil), newNoopCanvas(), PointerEvent{}
}

func unpackWheelPayload(v any) (*Context, *Canvas, WheelEvent) {
	if p, ok := v.(*wheelPayload); ok {
		return p.Ctx, p.Canvas, p.Event
	}
	return newContext(nil), newNoopCanvas(), WheelEvent{}
}

func unpackKeyPayload(v any) (*Context, *Canvas, KeyEvent) {
	if p, ok := v.(*keyPayload); ok {
		return p.Ctx, p.Canvas, p.Event
	}
	return newContext(nil), newNoopCanvas(), KeyEvent{}
}

func unpackResizePayload(v any) (*Context, *Canvas, ResizeEvent) {
	if p, ok := v.(*resizePayload); ok {
		return p.Ctx, p.Canvas, p.Event
	}
	return newContext(nil), newNoopCanvas(), ResizeEvent{}
}

func unpackContextOnly(v any) *Context {
	if p, ok := v.(*disposePayload); ok {
		return p.Ctx
	}
	return newContext(nil)
}

func asRawMessage(v any) []byte {
	switch x := v.(type) {
	case json.RawMessage:
		return []byte(x)
	case []byte:
		return x
	case string:
		return []byte(x)
	}
	return nil
}
