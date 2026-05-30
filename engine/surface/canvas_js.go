//go:build js && wasm

package surface

import (
	"strings"
	"syscall/js"
)

// jsCanvas wraps a browser CanvasRenderingContext2D js.Value and satisfies
// the CanvasImpl interface. Instances are created by the runtime package via
// NewJSCanvas and wrapped in a *Canvas by NewCanvas.
type jsCanvas struct {
	el     js.Value // HTMLCanvasElement
	ctx2d  js.Value // CanvasRenderingContext2D
	stepFn func(dt float64)
	lastTS float64
}

// NewJSCanvas creates a *Canvas bound to an HTMLCanvasElement js.Value.
// Called by the runtime package during mount.
func NewJSCanvas(el js.Value) *Canvas {
	jsc := &jsCanvas{
		el:    el,
		ctx2d: el.Call("getContext", "2d"),
	}
	return &Canvas{impl: jsc}
}

// SplitKeyPayloadStr splits a "key\tcode" string into (key, code).
// Exported for use by the runtime package.
func SplitKeyPayloadStr(s string) (string, string) {
	idx := strings.IndexByte(s, '\t')
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// DecodeBase64Props decodes a base64 string into JSON bytes via the browser
// atob function. Exported for use by the runtime package.
func DecodeBase64Props(b64 string) []byte {
	if b64 == "" {
		return nil
	}
	decoded := js.Global().Call("atob", b64)
	return []byte(decoded.String())
}

// RequestFrameJS signals to the JS bootstrap via the exported surface hook
// that this canvas wants the next animation frame. Exported for runtime use.
func RequestFrameJS(el js.Value) {
	js.Global().Call("__gosx_surface_request_frame", el.Get("id"))
}

// El returns the underlying HTMLCanvasElement js.Value.
func (c *jsCanvas) El() js.Value { return c.el }

func (c *jsCanvas) width() int  { return c.el.Get("width").Int() }
func (c *jsCanvas) height() int { return c.el.Get("height").Int() }

func (c *jsCanvas) clear() {
	c.ctx2d.Call("clearRect", 0, 0, c.width(), c.height())
}
func (c *jsCanvas) clearRect(x, y, w, h float64) {
	c.ctx2d.Call("clearRect", x, y, w, h)
}
func (c *jsCanvas) fillRect(x, y, w, h float64) {
	c.ctx2d.Call("fillRect", x, y, w, h)
}
func (c *jsCanvas) beginPath()          { c.ctx2d.Call("beginPath") }
func (c *jsCanvas) moveTo(x, y float64) { c.ctx2d.Call("moveTo", x, y) }
func (c *jsCanvas) lineTo(x, y float64) { c.ctx2d.Call("lineTo", x, y) }
func (c *jsCanvas) arc(x, y, r, s, e float64) {
	c.ctx2d.Call("arc", x, y, r, s, e)
}
func (c *jsCanvas) stroke() { c.ctx2d.Call("stroke") }
func (c *jsCanvas) fill()   { c.ctx2d.Call("fill") }
func (c *jsCanvas) fillText(text string, x, y float64) {
	c.ctx2d.Call("fillText", text, x, y)
}
func (c *jsCanvas) setFillStyle(css string)   { c.ctx2d.Set("fillStyle", css) }
func (c *jsCanvas) setStrokeStyle(css string) { c.ctx2d.Set("strokeStyle", css) }
func (c *jsCanvas) setLineWidth(w float64)    { c.ctx2d.Set("lineWidth", w) }
func (c *jsCanvas) setFont(css string)        { c.ctx2d.Set("font", css) }
func (c *jsCanvas) setTextAlign(align string) { c.ctx2d.Set("textAlign", align) }
func (c *jsCanvas) save()                     { c.ctx2d.Call("save") }
func (c *jsCanvas) restore()                  { c.ctx2d.Call("restore") }
func (c *jsCanvas) translate(x, y float64)    { c.ctx2d.Call("translate", x, y) }
func (c *jsCanvas) scale(x, y float64)        { c.ctx2d.Call("scale", x, y) }
func (c *jsCanvas) rotate(rad float64)        { c.ctx2d.Call("rotate", rad) }
func (c *jsCanvas) setTransform(a, b, c2, d, e, f float64) {
	c.ctx2d.Call("setTransform", a, b, c2, d, e, f)
}
func (c *jsCanvas) requestFrame() {
	RequestFrameJS(c.el)
}
func (c *jsCanvas) startLoop(step func(dt float64)) {
	c.stepFn = step
	c.requestFrame()
}

// SetStepFn sets the animation loop step function. Called by the runtime frame ticker.
func (c *Canvas) SetStepFn(fn func(dt float64)) {
	if jsc, ok := c.impl.(*jsCanvas); ok {
		jsc.stepFn = fn
	}
}

// TickFrame advances the animation loop with a new timestamp (milliseconds).
// Called by the runtime __gosx_surface_frame handler.
func (c *Canvas) TickFrame(ts float64) {
	jsc, ok := c.impl.(*jsCanvas)
	if !ok {
		return
	}
	dt := ts - jsc.lastTS
	jsc.lastTS = ts
	if jsc.stepFn != nil {
		jsc.stepFn(dt)
	}
}
