package headless

import (
	"image/color"
	"testing"

	"github.com/odvcencio/gosx/render/gpu"
)

// TestFramebufferReflectsClearValue verifies that a render pass with a
// color-clear on the surface attachment writes the clear color into the
// headless framebuffer.
func TestFramebufferReflectsClearValue(t *testing.T) {
	d, surface := New(4, 4)

	view, err := d.AcquireSurfaceView(surface)
	if err != nil {
		t.Fatalf("AcquireSurfaceView: %v", err)
	}
	enc := d.CreateCommandEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 1.0, G: 0.5, B: 0.0, A: 1.0},
		}},
	})
	pass.End()
	d.Queue().Submit(enc.Finish())

	fb := d.Framebuffer()
	got := fb.RGBAAt(2, 2)
	want := color.RGBA{R: 255, G: 127, B: 0, A: 255}
	if got.R != want.R || got.G != want.G || got.B != want.B || got.A != want.A {
		t.Errorf("framebuffer pixel: want %+v, got %+v", want, got)
	}
}

// TestBufferWriteReadRoundTrip verifies that WriteBuffer + ReadAsync on a
// headless buffer round-trip bytes correctly. Confirms the path used by
// pick readback tests on the headless backend.
func TestBufferWriteReadRoundTrip(t *testing.T) {
	d, _ := New(2, 2)
	buf, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  16,
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	input := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	d.Queue().WriteBuffer(buf, 0, input)

	got, err := buf.ReadAsync(len(input))
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	for i, v := range input {
		if got[i] != v {
			t.Errorf("byte %d: want %d, got %d", i, v, got[i])
		}
	}
}

// TestDeviceLostOnHeadlessIsNoOp confirms the device-lost path is a
// no-op on headless — the backend has nothing to lose.
func TestDeviceLostOnHeadlessIsNoOp(t *testing.T) {
	d, _ := New(1, 1)
	called := false
	d.OnLost(func(reason, message string) {
		called = true
	})
	if called {
		t.Error("OnLost callback fired unexpectedly on headless")
	}
}
