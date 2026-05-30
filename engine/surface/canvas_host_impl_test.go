// Slice Y.F — coverage for the HostCanvasImpl exported seam.

package surface

import "testing"

// recordingHost is a HostCanvasImpl that records every call. It is the
// minimal proof that an out-of-package type can satisfy the exported
// interface and be wired into a *Canvas — exactly what the hyphae
// SSIM parity test needs.
type recordingHost struct {
	w, h  int
	calls []string
}

func (r *recordingHost) Width() int                         { return r.w }
func (r *recordingHost) Height() int                        { return r.h }
func (r *recordingHost) Clear()                             { r.calls = append(r.calls, "Clear") }
func (r *recordingHost) ClearRect(x, y, w, h float64)       { r.calls = append(r.calls, "ClearRect") }
func (r *recordingHost) FillRect(x, y, w, h float64)        { r.calls = append(r.calls, "FillRect") }
func (r *recordingHost) BeginPath()                         { r.calls = append(r.calls, "BeginPath") }
func (r *recordingHost) MoveTo(x, y float64)                { r.calls = append(r.calls, "MoveTo") }
func (r *recordingHost) LineTo(x, y float64)                { r.calls = append(r.calls, "LineTo") }
func (r *recordingHost) Arc(x, y, rad, s, e float64)        { r.calls = append(r.calls, "Arc") }
func (r *recordingHost) Stroke()                            { r.calls = append(r.calls, "Stroke") }
func (r *recordingHost) Fill()                              { r.calls = append(r.calls, "Fill") }
func (r *recordingHost) FillText(text string, x, y float64) { r.calls = append(r.calls, "FillText") }
func (r *recordingHost) SetFillStyle(css string)            { r.calls = append(r.calls, "SetFillStyle") }
func (r *recordingHost) SetStrokeStyle(css string)          { r.calls = append(r.calls, "SetStrokeStyle") }
func (r *recordingHost) SetLineWidth(w float64)             { r.calls = append(r.calls, "SetLineWidth") }
func (r *recordingHost) SetFont(css string)                 { r.calls = append(r.calls, "SetFont") }
func (r *recordingHost) SetTextAlign(a string)              { r.calls = append(r.calls, "SetTextAlign") }
func (r *recordingHost) Save()                              { r.calls = append(r.calls, "Save") }
func (r *recordingHost) Restore()                           { r.calls = append(r.calls, "Restore") }
func (r *recordingHost) Translate(x, y float64)             { r.calls = append(r.calls, "Translate") }
func (r *recordingHost) Scale(x, y float64)                 { r.calls = append(r.calls, "Scale") }
func (r *recordingHost) Rotate(rad float64)                 { r.calls = append(r.calls, "Rotate") }
func (r *recordingHost) SetTransform(a, b, c, d, e, f float64) {
	r.calls = append(r.calls, "SetTransform")
}
func (r *recordingHost) RequestFrame() { r.calls = append(r.calls, "RequestFrame") }

func TestY_F_NewCanvasFromHostImplForwardsCalls(t *testing.T) {
	host := &recordingHost{w: 400, h: 300}
	c := NewCanvasFromHostImpl(host)
	if c == nil {
		t.Fatal("NewCanvasFromHostImpl returned nil")
	}
	if got := c.Width(); got != 400 {
		t.Errorf("Width = %d, want 400", got)
	}
	if got := c.Height(); got != 300 {
		t.Errorf("Height = %d, want 300", got)
	}
	c.Save()
	c.Translate(10, 20)
	c.Scale(2, 2)
	c.SetFillStyle("#ff0000")
	c.BeginPath()
	c.MoveTo(0, 0)
	c.LineTo(10, 10)
	c.Arc(5, 5, 3, 0, 6.28)
	c.Stroke()
	c.Fill()
	c.FillText("hello", 1, 2)
	c.SetTextAlign("center")
	c.SetStrokeStyle("#00ff00")
	c.SetLineWidth(1.5)
	c.SetFont("bold 11px sans-serif")
	c.ClearRect(0, 0, 100, 100)
	c.FillRect(0, 0, 50, 50)
	c.Rotate(0.1)
	c.SetTransform(1, 0, 0, 1, 0, 0)
	c.RequestFrame()
	c.Restore()
	// StartLoop is intentionally not forwarded (the adapter no-ops it).
	c.StartLoop(func(dt float64) { t.Fatal("step should not fire on host adapter") })

	want := []string{
		"Save", "Translate", "Scale", "SetFillStyle", "BeginPath",
		"MoveTo", "LineTo", "Arc", "Stroke", "Fill", "FillText",
		"SetTextAlign", "SetStrokeStyle", "SetLineWidth", "SetFont",
		"ClearRect", "FillRect", "Rotate", "SetTransform",
		"RequestFrame", "Restore",
	}
	if len(host.calls) != len(want) {
		t.Fatalf("call count = %d, want %d (%v)", len(host.calls), len(want), host.calls)
	}
	for i, got := range host.calls {
		if got != want[i] {
			t.Errorf("call[%d] = %s, want %s", i, got, want[i])
		}
	}
}

// TestY_F_NewCanvasFromHostImplNilReturnsNoop ensures the nil-impl
// fallback yields a usable Canvas instead of panicking, so test
// harnesses that wire a Canvas before they have a rasterizer get the
// stub behavior production code already relies on.
func TestY_F_NewCanvasFromHostImplNilReturnsNoop(t *testing.T) {
	c := NewCanvasFromHostImpl(nil)
	if c == nil {
		t.Fatal("nil host yielded nil Canvas; want noop fallback")
	}
	// All calls should no-op without panicking.
	c.Save()
	c.MoveTo(1, 2)
	c.LineTo(3, 4)
	c.Stroke()
	c.Restore()
	if c.Width() != 0 || c.Height() != 0 {
		t.Errorf("noop canvas dims = (%d, %d), want (0, 0)", c.Width(), c.Height())
	}
}
