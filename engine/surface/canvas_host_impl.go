// Slice Y.F — exported CanvasImpl seam for out-of-package hosts.
//
// The package-internal CanvasImpl interface uses lowercase method names
// (width/height/clearRect/...) so the only legal implementations are
// inside the surface package — canvas_js.go (JS-side) and
// canvas_stub_host.go (no-op). That's intentional for the production
// pipeline: every real canvas binds through the JS bootstrap or the
// in-process stub.
//
// However, the Slice Y.F SSIM parity test
// (~/work/hyphae/cmd/hypha-viz/dogfood_parity_test.go) needs to drive
// the native-Go execution of an engine surface's handler bodies against
// a pure-Go rasterizer it owns. Hard-coding the rasterizer inside the
// surface package would couple gosx to the hyphae test infrastructure;
// re-exporting CanvasImpl with capitalized methods would force every
// internal call site to rename. Neither is the right shape.
//
// Instead this file adds a one-page exported seam:
//
//   - HostCanvasImpl is an exported mirror of CanvasImpl with
//     capitalized method names. Any out-of-package type that wants to
//     impersonate a real canvas (for parity tests, CPU-side previews,
//     server-side static rendering, …) satisfies this interface.
//
//   - NewCanvasFromHostImpl wraps a HostCanvasImpl into a *Canvas. The
//     wrapper adapter translates the capitalized methods into the
//     package-private interface so the rest of the surface package
//     stays unchanged.
//
// This seam is additive only — production code paths (canvas_js.go,
// stubImpl) are untouched.

package surface

// HostCanvasImpl is the exported mirror of the package-internal
// CanvasImpl interface. Out-of-package hosts (parity tests,
// preview renderers, server-side static rasterizers) implement this
// interface and pass it through NewCanvasFromHostImpl to obtain a
// *Canvas that engine-surface handlers can draw to.
//
// The method set mirrors CanvasImpl exactly, just capitalized.
// StartLoop is omitted: hosts that want to drive a loop should do so
// from the host side directly (calling stepLayout + draw in test code
// per the Slice Y.E retrospective's Option F.b recommendation, since
// FuncLit closures remain a Phase 4 expression-language gap).
type HostCanvasImpl interface {
	Width() int
	Height() int
	Clear()
	ClearRect(x, y, w, h float64)
	FillRect(x, y, w, h float64)
	BeginPath()
	MoveTo(x, y float64)
	LineTo(x, y float64)
	Arc(x, y, r, start, end float64)
	Stroke()
	Fill()
	FillText(text string, x, y float64)
	SetFillStyle(css string)
	SetStrokeStyle(css string)
	SetLineWidth(w float64)
	SetFont(css string)
	SetTextAlign(align string)
	Save()
	Restore()
	Translate(x, y float64)
	Scale(x, y float64)
	Rotate(rad float64)
	SetTransform(a, b, c2, d, e, f float64)
	RequestFrame()
}

// hostImplAdapter wraps a HostCanvasImpl so it satisfies the
// package-private CanvasImpl interface. Every method forwards to the
// corresponding capitalized method on the host. startLoop is a no-op
// in this adapter — see HostCanvasImpl's doc for the rationale.
type hostImplAdapter struct {
	h HostCanvasImpl
}

func (a *hostImplAdapter) width() int                         { return a.h.Width() }
func (a *hostImplAdapter) height() int                        { return a.h.Height() }
func (a *hostImplAdapter) clear()                             { a.h.Clear() }
func (a *hostImplAdapter) clearRect(x, y, w, h float64)       { a.h.ClearRect(x, y, w, h) }
func (a *hostImplAdapter) fillRect(x, y, w, h float64)        { a.h.FillRect(x, y, w, h) }
func (a *hostImplAdapter) beginPath()                         { a.h.BeginPath() }
func (a *hostImplAdapter) moveTo(x, y float64)                { a.h.MoveTo(x, y) }
func (a *hostImplAdapter) lineTo(x, y float64)                { a.h.LineTo(x, y) }
func (a *hostImplAdapter) arc(x, y, r, start, end float64)    { a.h.Arc(x, y, r, start, end) }
func (a *hostImplAdapter) stroke()                            { a.h.Stroke() }
func (a *hostImplAdapter) fill()                              { a.h.Fill() }
func (a *hostImplAdapter) fillText(text string, x, y float64) { a.h.FillText(text, x, y) }
func (a *hostImplAdapter) setFillStyle(css string)            { a.h.SetFillStyle(css) }
func (a *hostImplAdapter) setStrokeStyle(css string)          { a.h.SetStrokeStyle(css) }
func (a *hostImplAdapter) setLineWidth(w float64)             { a.h.SetLineWidth(w) }
func (a *hostImplAdapter) setFont(css string)                 { a.h.SetFont(css) }
func (a *hostImplAdapter) setTextAlign(align string)          { a.h.SetTextAlign(align) }
func (a *hostImplAdapter) save()                              { a.h.Save() }
func (a *hostImplAdapter) restore()                           { a.h.Restore() }
func (a *hostImplAdapter) translate(x, y float64)             { a.h.Translate(x, y) }
func (a *hostImplAdapter) scale(x, y float64)                 { a.h.Scale(x, y) }
func (a *hostImplAdapter) rotate(rad float64)                 { a.h.Rotate(rad) }
func (a *hostImplAdapter) setTransform(p, q, c2, d, e, f float64) {
	a.h.SetTransform(p, q, c2, d, e, f)
}
func (a *hostImplAdapter) requestFrame()                   { a.h.RequestFrame() }
func (a *hostImplAdapter) startLoop(step func(dt float64)) { /* host drives the loop */ }

// NewCanvasFromHostImpl returns a *Canvas whose drawing methods
// forward to impl. The returned Canvas is fully usable by any
// engine-surface handler — same observable behavior as the production
// JS-backed canvas, just with the host's CPU-side implementation
// instead of CanvasRenderingContext2D.
//
// Passing nil is treated like the host stub (returns a no-op canvas).
func NewCanvasFromHostImpl(impl HostCanvasImpl) *Canvas {
	if impl == nil {
		return newNoopCanvas()
	}
	return &Canvas{impl: &hostImplAdapter{h: impl}}
}
