//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	sOK      = uintptr(0)
	ePointer = uintptr(0x80004003)

	iUnknownRelease = 2

	environmentCreateController = 3

	controllerPutIsVisible   = 4
	controllerPutBounds      = 6
	controllerClose          = 24
	controllerGetCoreWebView = 25

	webViewNavigate         = 5
	webViewNavigateToString = 6
)

var (
	modWebView2 = syscall.NewLazyDLL("WebView2Loader.dll")

	procCreateCoreWebView2EnvironmentWithOptions = modWebView2.NewProc("CreateCoreWebView2EnvironmentWithOptions")
)

type coreWebView2Environment struct {
	vtbl uintptr
}

type coreWebView2Controller struct {
	vtbl uintptr
}

type coreWebView2 struct {
	vtbl uintptr
}

type environmentCompletedHandler struct {
	vtbl *environmentCompletedHandlerVtbl
	refs uint32
	app  *windowsApp
}

type environmentCompletedHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

type controllerCompletedHandler struct {
	vtbl *controllerCompletedHandlerVtbl
	refs uint32
	app  *windowsApp
}

type controllerCompletedHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

var (
	environmentHandlerVtbl = environmentCompletedHandlerVtbl{
		QueryInterface: syscall.NewCallback(environmentQueryInterface),
		AddRef:         syscall.NewCallback(environmentAddRef),
		Release:        syscall.NewCallback(environmentRelease),
		Invoke:         syscall.NewCallback(environmentInvoke),
	}
	controllerHandlerVtbl = controllerCompletedHandlerVtbl{
		QueryInterface: syscall.NewCallback(controllerQueryInterface),
		AddRef:         syscall.NewCallback(controllerAddRef),
		Release:        syscall.NewCallback(controllerRelease),
		Invoke:         syscall.NewCallback(controllerInvoke),
	}
)

type hresultError struct {
	Op   string
	Code uintptr
}

func (e hresultError) Error() string {
	return fmt.Sprintf("%s failed: HRESULT 0x%08x", e.Op, uint32(e.Code))
}

func failedHRESULT(hr uintptr) bool {
	return uint32(hr)&0x80000000 != 0
}

func newEnvironmentCompletedHandler(app *windowsApp) *environmentCompletedHandler {
	return &environmentCompletedHandler{vtbl: &environmentHandlerVtbl, refs: 1, app: app}
}

func newControllerCompletedHandler(app *windowsApp) *controllerCompletedHandler {
	return &controllerCompletedHandler{vtbl: &controllerHandlerVtbl, refs: 1, app: app}
}

func (e *coreWebView2Environment) createController(hwnd uintptr, handler *controllerCompletedHandler) error {
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(e), environmentCreateController),
		uintptr(unsafe.Pointer(e)),
		hwnd,
		uintptr(unsafe.Pointer(handler)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2Environment.CreateCoreWebView2Controller", Code: hr}
	}
	return nil
}

func (c *coreWebView2Controller) setVisible(visible bool) error {
	var value uintptr
	if visible {
		value = 1
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(c), controllerPutIsVisible),
		uintptr(unsafe.Pointer(c)),
		value,
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2Controller.put_IsVisible", Code: hr}
	}
	return nil
}

func (c *coreWebView2Controller) setBounds(bounds rect) error {
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(c), controllerPutBounds),
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&bounds)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2Controller.put_Bounds", Code: hr}
	}
	return nil
}

func (c *coreWebView2Controller) close() {
	syscall.SyscallN(comMethod(unsafe.Pointer(c), controllerClose), uintptr(unsafe.Pointer(c)))
}

func (c *coreWebView2Controller) coreWebView2() (*coreWebView2, error) {
	var webview *coreWebView2
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(c), controllerGetCoreWebView),
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(&webview)),
	)
	if failedHRESULT(hr) {
		return nil, hresultError{Op: "ICoreWebView2Controller.get_CoreWebView2", Code: hr}
	}
	if webview == nil {
		return nil, fmt.Errorf("%w: ICoreWebView2 was nil", ErrWebView2Unavailable)
	}
	return webview, nil
}

func (w *coreWebView2) navigate(url string) error {
	urlPtr, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return fmt.Errorf("%w: url: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewNavigate),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(urlPtr)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.Navigate", Code: hr}
	}
	return nil
}

func (w *coreWebView2) navigateToString(html string) error {
	htmlPtr, err := syscall.UTF16PtrFromString(html)
	if err != nil {
		return fmt.Errorf("%w: html: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewNavigateToString),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(htmlPtr)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.NavigateToString", Code: hr}
	}
	return nil
}

func comMethod(obj unsafe.Pointer, index uintptr) uintptr {
	vtbl := *(*uintptr)(obj)
	return *(*uintptr)(unsafe.Pointer(vtbl + index*unsafe.Sizeof(uintptr(0))))
}

func comRelease(obj unsafe.Pointer) {
	syscall.SyscallN(comMethod(obj, iUnknownRelease), uintptr(obj))
}

func environmentQueryInterface(this, _, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = this
	environmentAddRef(this)
	return sOK
}

func environmentAddRef(this uintptr) uintptr {
	handler := (*environmentCompletedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, 1))
}

func environmentRelease(this uintptr) uintptr {
	handler := (*environmentCompletedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, ^uint32(0)))
}

func environmentInvoke(this, errorCode, result uintptr) uintptr {
	handler := (*environmentCompletedHandler)(unsafe.Pointer(this))
	handler.app.onEnvironmentCreated(errorCode, (*coreWebView2Environment)(unsafe.Pointer(result)))
	return sOK
}

func controllerQueryInterface(this, _, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = this
	controllerAddRef(this)
	return sOK
}

func controllerAddRef(this uintptr) uintptr {
	handler := (*controllerCompletedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, 1))
}

func controllerRelease(this uintptr) uintptr {
	handler := (*controllerCompletedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, ^uint32(0)))
}

func controllerInvoke(this, errorCode, result uintptr) uintptr {
	handler := (*controllerCompletedHandler)(unsafe.Pointer(this))
	handler.app.onControllerCreated(errorCode, (*coreWebView2Controller)(unsafe.Pointer(result)))
	return sOK
}
