// Package surface provides first-class canvas engine surface support for GoSX.
//
// Devs author canvas engine surfaces in .gsx + pure Go with zero hand-authored JS.
// The surface package exposes the types devs write against (Context, Canvas, event
// structs), plus the server-side Renderer that emits <canvas> placeholder nodes.
//
// Usage in a .gsx file:
//
//	//gosx:engine surface
//	//gosx:caps canvas,animation
//	func Graph(props GraphProps) Node { return <canvas onMount={Mount} onClick={OnSelect} /> }
//
// In the companion Go file:
//
//	func Mount(ctx *surface.Context, c *surface.Canvas) {
//	    var p GraphProps
//	    ctx.PropsInto(&p)
//	    c.StartLoop(func(dt float64) { draw(c, p) })
//	}
//
//	func OnSelect(ctx *surface.Context, c *surface.Canvas, ev surface.PointerEvent) {
//	    // handle click
//	}
//
// On the server side, emit the placeholder:
//
//	r := surface.NewRenderer("Graph")
//	node := r.Mount(props)
package surface

import (
	"encoding/json"
	"fmt"

	gosx "m31labs.dev/gosx"
)

// Modifier is a bitfield of keyboard modifier keys active during an event.
type Modifier uint8

const (
	ModShift Modifier = 1 << iota
	ModCtrl
	ModAlt
	ModMeta
)

// PointerEvent holds the state of a pointer (mouse/touch) event.
type PointerEvent struct {
	X, Y    float64
	Button  int
	Buttons int
	Modifier Modifier
}

// WheelEvent holds the state of a wheel (scroll) event.
type WheelEvent struct {
	X, Y           float64
	DeltaX, DeltaY float64
	Modifier       Modifier
}

// KeyEvent holds the state of a keyboard event.
type KeyEvent struct {
	Key      string
	Code     string
	Modifier Modifier
}

// ResizeEvent is fired when the canvas dimensions change.
type ResizeEvent struct {
	Width, Height int
	DPR           float64
}

// Context is the per-mount execution context passed to surface handlers.
// It is opaque to user code; only its methods are public.
// On the WASM target the fields are backed by live JS state; on the host
// they hold in-memory stubs.
type Context struct {
	props []byte
	done  chan struct{}
}

// NewContext creates a Context from a raw JSON props slice.
// Called by the runtime package (canvas_js.go) to create a Context bound to
// a live WASM mount.
func NewContext(props []byte) *Context {
	return &Context{
		props: props,
		done:  make(chan struct{}),
	}
}

// newContext is the package-internal alias.
func newContext(props []byte) *Context { return NewContext(props) }

// Props returns the raw JSON props bytes delivered at mount time.
func (ctx *Context) Props() json.RawMessage {
	return json.RawMessage(ctx.props)
}

// PropsInto unmarshals the props JSON into out.
func (ctx *Context) PropsInto(out any) error {
	if len(ctx.props) == 0 {
		return nil
	}
	return json.Unmarshal(ctx.props, out)
}

// Done returns a channel that is closed when the surface is disposed.
// Use this to stop goroutines started in OnMount.
func (ctx *Context) Done() <-chan struct{} {
	return ctx.done
}

// Close signals disposal by closing the Done channel.
// Called by the runtime package when the surface is torn down.
func (ctx *Context) Close() {
	select {
	case <-ctx.done:
	default:
		close(ctx.done)
	}
}

// close is the package-internal alias.
func (ctx *Context) close() { ctx.Close() }

// Canvas is the 2-D drawing surface available to surface handlers.
// On the WASM target this wraps the browser CanvasRenderingContext2D via
// syscall/js; on the host the methods are no-ops provided by canvas_stub.go.
type Canvas struct {
	impl CanvasImpl
}

// CanvasImpl is the interface that the platform-specific files must satisfy.
// canvas_js.go provides the real implementation; canvas_stub.go provides no-ops.
type CanvasImpl interface {
	width() int
	height() int
	clear()
	clearRect(x, y, w, h float64)
	fillRect(x, y, w, h float64)
	beginPath()
	moveTo(x, y float64)
	lineTo(x, y float64)
	arc(x, y, r, start, end float64)
	stroke()
	fill()
	fillText(s string, x, y float64)
	setFillStyle(css string)
	setStrokeStyle(css string)
	setLineWidth(w float64)
	setFont(css string)
	setTextAlign(s string)
	save()
	restore()
	translate(x, y float64)
	scale(x, y float64)
	rotate(rad float64)
	setTransform(a, b, c2, d, e, f float64)
	requestFrame()
	startLoop(step func(dt float64))
}

// Width returns the current canvas width in CSS pixels.
func (c *Canvas) Width() int { return c.impl.width() }

// Height returns the current canvas height in CSS pixels.
func (c *Canvas) Height() int { return c.impl.height() }

// Clear fills the entire canvas with transparent black.
func (c *Canvas) Clear() { c.impl.clear() }

// ClearRect clears the given rectangle to transparent.
func (c *Canvas) ClearRect(x, y, w, h float64) { c.impl.clearRect(x, y, w, h) }

// FillRect paints a filled rectangle using the current fill style.
func (c *Canvas) FillRect(x, y, w, h float64) { c.impl.fillRect(x, y, w, h) }

// BeginPath starts a new path.
func (c *Canvas) BeginPath() { c.impl.beginPath() }

// MoveTo moves the pen to (x, y) without drawing.
func (c *Canvas) MoveTo(x, y float64) { c.impl.moveTo(x, y) }

// LineTo draws a line from the current point to (x, y).
func (c *Canvas) LineTo(x, y float64) { c.impl.lineTo(x, y) }

// Arc adds an arc to the current path.
func (c *Canvas) Arc(x, y, r, start, end float64) { c.impl.arc(x, y, r, start, end) }

// Stroke strokes the current path.
func (c *Canvas) Stroke() { c.impl.stroke() }

// Fill fills the current path.
func (c *Canvas) Fill() { c.impl.fill() }

// FillText draws text at (x, y).
func (c *Canvas) FillText(s string, x, y float64) { c.impl.fillText(s, x, y) }

// SetFillStyle sets the fill color/style. Accepts any CSS color string.
func (c *Canvas) SetFillStyle(css string) { c.impl.setFillStyle(css) }

// SetStrokeStyle sets the stroke color/style.
func (c *Canvas) SetStrokeStyle(css string) { c.impl.setStrokeStyle(css) }

// SetLineWidth sets the stroke line width in pixels.
func (c *Canvas) SetLineWidth(w float64) { c.impl.setLineWidth(w) }

// SetFont sets the CSS font string (e.g. "14px sans-serif").
func (c *Canvas) SetFont(css string) { c.impl.setFont(css) }

// SetTextAlign sets the text alignment ("left", "center", "right", etc.).
func (c *Canvas) SetTextAlign(s string) { c.impl.setTextAlign(s) }

// Save pushes the current drawing state onto the stack.
func (c *Canvas) Save() { c.impl.save() }

// Restore pops the top drawing state from the stack.
func (c *Canvas) Restore() { c.impl.restore() }

// Translate moves the canvas origin by (x, y).
func (c *Canvas) Translate(x, y float64) { c.impl.translate(x, y) }

// Scale multiplies the canvas scale factors.
func (c *Canvas) Scale(x, y float64) { c.impl.scale(x, y) }

// Rotate rotates the canvas by rad radians clockwise.
func (c *Canvas) Rotate(rad float64) { c.impl.rotate(rad) }

// SetTransform replaces the current transform matrix.
// Parameters are the six values of the 2D transform matrix [a b c d e f].
func (c *Canvas) SetTransform(a, b, c2, d, e, f float64) { c.impl.setTransform(a, b, c2, d, e, f) }

// RequestFrame schedules one animation frame, calling the handler registered
// via StartLoop (if any).
func (c *Canvas) RequestFrame() { c.impl.requestFrame() }

// StartLoop begins a continuous animation loop. The step callback receives the
// elapsed time in milliseconds since the previous frame. Call RequestFrame()
// from within step to drive a fixed-rate loop instead.
func (c *Canvas) StartLoop(step func(dt float64)) { c.impl.startLoop(step) }

// --- Server-side renderer ---

// registryEntry is the in-memory record for a discovered surface component.
type registryEntry struct {
	wasmURL      string
	hash         string
	propsType    string
	capabilities []string
	mountAttrs   map[string]string
	// stale is true when this entry is serving a prior cached WASM because
	// the most-recent build failed. The Mount renderer surfaces this to the
	// client bootstrap via data-gosx-engine-stale="1" (spec §B).
	stale bool
}

// registry holds the per-process surface component entries populated by Discover.
var registry = &surfaceRegistry{}

// Renderer is the server-side renderer for a surface component.
// Obtain one via NewRenderer; it is safe for concurrent use.
type Renderer struct {
	component string
}

// NewRenderer returns a Renderer for the named surface component.
// The component must have been registered by a prior call to Discover (or by
// an injected test entry via injectRegistryEntry).
func NewRenderer(component string) *Renderer {
	return &Renderer{component: component}
}

// Mount emits the <canvas> placeholder node that the client-side bootstrap
// uses to mount the WASM engine. The returned gosx.Node includes:
//
//   - data-gosx-engine-component — the component name
//   - data-gosx-engine-wasm — absolute URL to the compiled WASM file
//   - data-gosx-engine-props — base64-encoded JSON of props
//   - data-gosx-engine-caps — comma-joined capabilities list
//   - any static MountAttrs forwarded from the .gsx source
//
// If the component has not been discovered, a <canvas> with only the
// component name attr is returned (safe fallback).
func (r *Renderer) Mount(props any) gosx.Node {
	entry, ok := registry.lookup(r.component)

	attrPairs := []any{
		gosx.Attr("data-gosx-engine-component", r.component),
	}

	// Forward any static mount attributes (id, tabindex, …) from the .gsx
	// source regardless of registry presence; they affect layout/focus and
	// the bootstrap may rely on them (e.g. id="graph-canvas" anchors the
	// hyphae-viz CSS).
	if ok {
		for k, v := range entry.mountAttrs {
			attrPairs = append(attrPairs, gosx.Attr(k, v))
		}
	}

	// Defect 4 (spec §D): when the registry entry is missing OR its wasmURL
	// is empty, do NOT emit data-gosx-engine-wasm="" — that confuses the
	// bootstrap into trying to fetch the empty URL. Emit a status attribute
	// instead so the bootstrap can paint a "surface unavailable" placeholder
	// without losing the layout slot.
	if !ok || entry.wasmURL == "" {
		attrPairs = append(attrPairs, gosx.Attr("data-gosx-engine-status", "missing"))
		return gosx.El("canvas", gosx.Attrs(attrPairs...))
	}

	propsJSON := encodeProps(props)
	attrPairs = append(attrPairs,
		gosx.Attr("data-gosx-engine-wasm", entry.wasmURL),
		gosx.Attr("data-gosx-engine-props", propsJSON),
	)
	if len(entry.capabilities) > 0 {
		attrPairs = append(attrPairs,
			gosx.Attr("data-gosx-engine-caps", joinStrings(entry.capabilities, ",")),
		)
	}
	// Defect 2 (spec §B): a stale entry is the previous build's WASM; the
	// bootstrap still mounts it but can render a corner badge so the user
	// knows the most-recent build failed.
	if entry.stale {
		attrPairs = append(attrPairs, gosx.Attr("data-gosx-engine-stale", "1"))
	}

	return gosx.El("canvas", gosx.Attrs(attrPairs...))
}

// PageHead returns an optional <link rel="preload"> node for the WASM asset.
// Compose into the page <head> to start the WASM fetch before the bootstrap runs.
// Returns an empty fragment if the component has not been discovered.
func (r *Renderer) PageHead() gosx.Node {
	entry, ok := registry.lookup(r.component)
	if !ok {
		return gosx.Fragment()
	}
	return gosx.El("link",
		gosx.Attrs(
			gosx.Attr("rel", "preload"),
			gosx.Attr("href", entry.wasmURL),
			gosx.Attr("as", "fetch"),
			gosx.Attr("crossorigin", "anonymous"),
		),
	)
}

// encodeProps marshals props to JSON and base64-encodes the result.
func encodeProps(props any) string {
	if props == nil {
		return ""
	}
	var raw []byte
	switch v := props.(type) {
	case json.RawMessage:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		var err error
		raw, err = json.Marshal(props)
		if err != nil {
			return ""
		}
	}
	return fmt.Sprintf("%s", encodeBase64(raw))
}

// encodeBase64 base64-encodes src without importing encoding/base64 — this
// avoids an otherwise-unused import while keeping the function self-contained.
func encodeBase64(src []byte) []byte {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	n := len(src)
	dst := make([]byte, (n+2)/3*4)
	di, si := 0, 0
	for n2 := n - n%3; si < n2; si += 3 {
		val := uint(src[si])<<16 | uint(src[si+1])<<8 | uint(src[si+2])
		dst[di+0] = enc[val>>18&0x3F]
		dst[di+1] = enc[val>>12&0x3F]
		dst[di+2] = enc[val>>6&0x3F]
		dst[di+3] = enc[val>>0&0x3F]
		di += 4
	}
	rem := n % 3
	if rem == 2 {
		val := uint(src[si])<<16 | uint(src[si+1])<<8
		dst[di+0] = enc[val>>18&0x3F]
		dst[di+1] = enc[val>>12&0x3F]
		dst[di+2] = enc[val>>6&0x3F]
		dst[di+3] = '='
	} else if rem == 1 {
		val := uint(src[si]) << 16
		dst[di+0] = enc[val>>18&0x3F]
		dst[di+1] = enc[val>>12&0x3F]
		dst[di+2] = '='
		dst[di+3] = '='
	}
	return dst
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += sep + s
	}
	return out
}
