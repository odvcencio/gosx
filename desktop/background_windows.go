//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modGdi32             = syscall.NewLazyDLL("gdi32.dll")
	procCreateSolidBrush = modGdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")
	procFillRect         = modUser32.NewProc("FillRect")
)

const (
	// ICoreWebView2Controller2 vtable: ICoreWebView2Controller ends at
	// get_CoreWebView2 (25), then get_/put_DefaultBackgroundColor.
	controller2PutDefaultBackgroundColor = 27
)

func createSolidBrush(color rgbColor) (uintptr, error) {
	// COLORREF is 0x00BBGGRR.
	colorRef := uintptr(color.R) | uintptr(color.G)<<8 | uintptr(color.B)<<16
	brush, _, callErr := procCreateSolidBrush.Call(colorRef)
	if brush == 0 {
		return 0, fmt.Errorf("CreateSolidBrush: %w", callErr)
	}
	return brush, nil
}

func deleteGDIObject(object uintptr) {
	if object != 0 {
		procDeleteObject.Call(object)
	}
}

// paintBackground fills the client area with the app's background brush for
// WM_ERASEBKGND. It reports false when no BackgroundColor is set, so the
// default class brush is used.
func (a *windowsApp) paintBackground(hwnd, hdc uintptr) bool {
	a.mu.Lock()
	brush := a.backgroundBrush
	a.mu.Unlock()
	if brush == 0 || hdc == 0 {
		return false
	}
	r, err := clientRect(hwnd)
	if err != nil {
		return false
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
	return true
}

// setDefaultBackgroundColor sets ICoreWebView2Controller2's default
// background, which WebView2 paints before and behind page content.
func (c *coreWebView2Controller) setDefaultBackgroundColor(color rgbColor) error {
	controller2, err := queryCOMInterface(unsafe.Pointer(c), iidCoreWebView2Controller2)
	if err != nil {
		return err
	}
	defer comRelease(controller2)
	// COREWEBVIEW2_COLOR is {A, R, G, B} bytes passed by value in one
	// register on amd64 and arm64.
	packed := uintptr(0xff) | uintptr(color.R)<<8 | uintptr(color.G)<<16 | uintptr(color.B)<<24
	hr, _, _ := syscall.SyscallN(comMethod(controller2, controller2PutDefaultBackgroundColor),
		uintptr(controller2), packed)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2Controller2.put_DefaultBackgroundColor", Code: hr}
	}
	return nil
}
