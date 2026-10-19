//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"testing"
	"unsafe"
)

// AppKit constrains oversized windows to their screen when showing them. The
// web view must start at the resulting content bounds, not the requested size;
// otherwise autoresizing preserves the initial overflow on subsequent resizes.
func TestDarwinWebViewFitsConstrainedWindow(t *testing.T) {
	requireGUI(t)
	var screen nsRect
	onMain(func() {
		withPool(func() {
			screen = frameOf(send0(objcClass("NSScreen"), objcSel("mainScreen")))
		})
	})
	if screen.w <= 0 || screen.h <= 0 {
		t.Fatal("main screen has no usable frame")
	}
	width, height := int(screen.w)+320, int(screen.h)+240
	runTestApp(t, Options{Width: width, Height: height}, func(app *App, da *darwinApp) {
		check := func(stage string) nsRect {
			t.Helper()
			var bounds, web nsRect
			if err := da.onUIThread(func() error {
				withPool(func() {
					content := send0(da.window, objcSel("contentView"))
					value := send1(content, objcSel("valueForKey:"), nsString("bounds"))
					getValue(value, unsafe.Pointer(&bounds), unsafe.Sizeof(bounds))
					web = frameOf(da.webview)
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: requested=%dx%d content=%+v web=%+v", stage, width, height, bounds, web)
			if web != bounds {
				t.Errorf("%s: web frame = %+v, want content bounds %+v", stage, web, bounds)
			}
			return bounds
		}
		bounds := check("initial")
		if bounds.w >= float64(width) && bounds.h >= float64(height) {
			t.Fatal("oversized request was not constrained; regression condition was not exercised")
		}
		setContentSizeOf(da, bounds.w-80, bounds.h-60)
		resized := check("resized")
		if resized.w >= bounds.w || resized.h >= bounds.h {
			t.Fatalf("content did not shrink: before %+v, after %+v", bounds, resized)
		}
	})
}
