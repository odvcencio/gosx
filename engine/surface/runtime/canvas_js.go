//go:build js && wasm

package runtime

import (
	"strings"
	"sync"
	"syscall/js"

	"github.com/odvcencio/gosx/engine/surface"
)

// instanceRecord tracks the live state of one mounted surface instance.
type instanceRecord struct {
	ctx    *surface.Context
	canvas *surface.Canvas
	surf   Surface
}

var (
	instancesMu sync.Mutex
	instances   = make(map[string]*instanceRecord)
	registered  = make(map[string]Surface)
)

func registerSurface(name string, s Surface) {
	instancesMu.Lock()
	registered[name] = s
	instancesMu.Unlock()
}

func init() {
	global := js.Global()

	// __gosx_surface_register(name)
	// Called by the JS bootstrap after WASM instantiation to confirm names.
	// Registration happens via Register(); this is an informational hook only.
	global.Set("__gosx_surface_register", js.FuncOf(func(this js.Value, args []js.Value) any {
		return nil
	}))

	// __gosx_surface_mount(id, name, canvasEl, propsBase64)
	// Called by the JS bootstrap to mount a component onto a canvas element.
	global.Set("__gosx_surface_mount", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 4 {
			return nil
		}
		id := args[0].String()
		name := args[1].String()
		canvasEl := args[2]
		propsB64 := args[3].String()

		instancesMu.Lock()
		surf, ok := registered[name]
		instancesMu.Unlock()
		if !ok {
			return nil
		}

		props := surface.DecodeBase64Props(propsB64)
		ctx := surface.NewContext(props)
		c := surface.NewJSCanvas(canvasEl)

		rec := &instanceRecord{ctx: ctx, canvas: c, surf: surf}
		instancesMu.Lock()
		instances[id] = rec
		instancesMu.Unlock()

		if surf.OnMount != nil {
			surf.OnMount(buildMountPayload(ctx, c, props))
		}
		return nil
	}))

	// __gosx_surface_event(id, kind, payloadBuf, payloadStr)
	global.Set("__gosx_surface_event", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 4 {
			return nil
		}
		id := args[0].String()
		kind := args[1].Int()
		payloadBuf := args[2]
		payloadStr := args[3].String()

		instancesMu.Lock()
		rec, ok := instances[id]
		instancesMu.Unlock()
		if !ok {
			return nil
		}

		floats := jsFloat64Array(payloadBuf)
		dispatchEvent(rec, kind, floats, payloadStr)
		return nil
	}))

	// __gosx_surface_frame(id, timestamp)
	global.Set("__gosx_surface_frame", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return nil
		}
		id := args[0].String()
		ts := args[1].Float()

		instancesMu.Lock()
		rec, ok := instances[id]
		instancesMu.Unlock()
		if !ok {
			return nil
		}

		rec.canvas.TickFrame(ts)
		return nil
	}))

	// __gosx_surface_dispose(id)
	global.Set("__gosx_surface_dispose", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return nil
		}
		id := args[0].String()

		instancesMu.Lock()
		rec, ok := instances[id]
		if ok {
			delete(instances, id)
		}
		instancesMu.Unlock()

		if !ok {
			return nil
		}
		if rec.surf.OnDispose != nil {
			rec.surf.OnDispose(buildDisposePayload(rec.ctx))
		}
		rec.ctx.Close()
		return nil
	}))
}

// dispatchEvent routes an event to the correct Surface handler.
func dispatchEvent(rec *instanceRecord, kind int, floats []float64, payloadStr string) {
	s := rec.surf
	ctx := rec.ctx
	c := rec.canvas

	switch kind {
	case 1: // click
		if s.OnClick != nil {
			s.OnClick(buildPointerPayload(ctx, c, floats))
		}
	case 2: // dblclick
		if s.OnDblClick != nil {
			s.OnDblClick(buildPointerPayload(ctx, c, floats))
		}
	case 3: // pointerdown
		if s.OnPointerDown != nil {
			s.OnPointerDown(buildPointerPayload(ctx, c, floats))
		}
	case 4: // pointermove
		if s.OnPointerMove != nil {
			s.OnPointerMove(buildPointerPayload(ctx, c, floats))
		}
	case 5: // pointerup
		if s.OnPointerUp != nil {
			s.OnPointerUp(buildPointerPayload(ctx, c, floats))
		}
	case 6: // pointercancel
		if s.OnPointerCancel != nil {
			s.OnPointerCancel(buildPointerPayload(ctx, c, floats))
		}
	case 7: // wheel
		if s.OnWheel != nil {
			s.OnWheel(buildWheelPayload(ctx, c, floats))
		}
	case 8: // keydown
		if s.OnKeyDown != nil {
			s.OnKeyDown(buildKeyPayload(ctx, c, floats, payloadStr))
		}
	case 9: // keyup
		if s.OnKeyUp != nil {
			s.OnKeyUp(buildKeyPayload(ctx, c, floats, payloadStr))
		}
	case 10: // resize
		if s.OnResize != nil {
			s.OnResize(buildResizePayload(ctx, c, floats))
		}
	case 11: // dispose handled by __gosx_surface_dispose
	}
}

// --- payload builder helpers ---
// These construct the runtime-internal payload structs that WrapMount /
// WrapPointer etc. type-assert against. The structs are defined in
// engine/surface/wrap.go (unexported); to avoid redeclaring them here, we use
// the exported wrap functions indirectly — we pass the payloads as the exported
// surface.*Payload types. However, since those wrap.go types are unexported, we
// use the public surface.New*Payload constructors provided below.

func buildMountPayload(ctx *surface.Context, c *surface.Canvas, props []byte) any {
	return surface.NewMountPayload(ctx, c, props)
}
func buildPointerPayload(ctx *surface.Context, c *surface.Canvas, f []float64) any {
	return surface.NewPointerPayload(ctx, c, f)
}
func buildWheelPayload(ctx *surface.Context, c *surface.Canvas, f []float64) any {
	return surface.NewWheelPayload(ctx, c, f)
}
func buildKeyPayload(ctx *surface.Context, c *surface.Canvas, f []float64, s string) any {
	key, code := surface.SplitKeyPayloadStr(s)
	return surface.NewKeyPayload(ctx, c, f, key, code)
}
func buildResizePayload(ctx *surface.Context, c *surface.Canvas, f []float64) any {
	return surface.NewResizePayload(ctx, c, f)
}
func buildDisposePayload(ctx *surface.Context) any {
	return surface.NewDisposePayload(ctx)
}

// jsFloat64Array reads a JS Float64Array into a []float64.
func jsFloat64Array(v js.Value) []float64 {
	if v.IsNull() || v.IsUndefined() {
		return nil
	}
	n := v.Get("length").Int()
	if n <= 0 {
		return nil
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = v.Index(i).Float()
	}
	return out
}

// splitKeyPayloadStr is the WASM-side split for any internal use.
func splitKeyPayloadStr(s string) (string, string) {
	idx := strings.IndexByte(s, '\t')
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}
