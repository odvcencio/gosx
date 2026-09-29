//go:build windows && (amd64 || arm64)

package desktop

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const processFailedArgsGetKind = 3

type coreWebView2ProcessFailedEventArgs struct {
	vtbl uintptr
}

func (a *coreWebView2ProcessFailedEventArgs) kind() (ProcessFailedKind, error) {
	var value int32
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(a), processFailedArgsGetKind),
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&value)),
	)
	if failedHRESULT(hr) {
		return "", hresultError{Op: "ProcessFailedEventArgs.get_ProcessFailedKind", Code: hr}
	}
	return processFailedKindFromWebView2(value), nil
}

type processFailedEventHandler struct {
	vtbl *processFailedEventHandlerVtbl
	refs uint32
	app  *windowsApp
}

type processFailedEventHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

var processFailedEventHandlerVtblInstance = processFailedEventHandlerVtbl{
	QueryInterface: syscall.NewCallback(processFailedQueryInterface),
	AddRef:         syscall.NewCallback(processFailedAddRef),
	Release:        syscall.NewCallback(processFailedRelease),
	Invoke:         syscall.NewCallback(processFailedInvoke),
}

func newProcessFailedEventHandler(app *windowsApp) *processFailedEventHandler {
	handler := &processFailedEventHandler{vtbl: &processFailedEventHandlerVtblInstance, refs: 1, app: app}
	rootCOMHandler(uintptr(unsafe.Pointer(handler)), handler)
	return handler
}

func processFailedQueryInterface(this, iid, ppv uintptr) uintptr {
	return queryInterfaceHandler(this, iid, ppv, iidProcessFailedEventHandler, processFailedAddRef)
}

func processFailedAddRef(this uintptr) uintptr {
	handler := (*processFailedEventHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, 1))
}

func processFailedRelease(this uintptr) uintptr {
	handler := (*processFailedEventHandler)(unsafe.Pointer(this))
	refs := atomic.AddUint32(&handler.refs, ^uint32(0))
	if refs == 0 {
		unrootCOMHandler(this)
	}
	runtime.KeepAlive(handler)
	return uintptr(refs)
}

func processFailedInvoke(this, _sender, args uintptr) uintptr {
	handler := (*processFailedEventHandler)(unsafe.Pointer(this))
	if handler == nil || handler.app == nil || args == 0 {
		return sOK
	}
	kind, err := (*coreWebView2ProcessFailedEventArgs)(unsafe.Pointer(args)).kind()
	if err == nil {
		handler.app.onProcessFailed(kind)
	}
	return sOK
}

type containsFullScreenElementChangedEventHandler struct {
	vtbl *containsFullScreenElementChangedEventHandlerVtbl
	refs uint32
	app  *windowsApp
}

type containsFullScreenElementChangedEventHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

var containsFullScreenElementChangedEventHandlerVtblInstance = containsFullScreenElementChangedEventHandlerVtbl{
	QueryInterface: syscall.NewCallback(fullscreenChangedQueryInterface),
	AddRef:         syscall.NewCallback(fullscreenChangedAddRef),
	Release:        syscall.NewCallback(fullscreenChangedRelease),
	Invoke:         syscall.NewCallback(fullscreenChangedInvoke),
}

func newContainsFullScreenElementChangedEventHandler(app *windowsApp) *containsFullScreenElementChangedEventHandler {
	handler := &containsFullScreenElementChangedEventHandler{
		vtbl: &containsFullScreenElementChangedEventHandlerVtblInstance,
		refs: 1,
		app:  app,
	}
	rootCOMHandler(uintptr(unsafe.Pointer(handler)), handler)
	return handler
}

func fullscreenChangedQueryInterface(this, iid, ppv uintptr) uintptr {
	return queryInterfaceHandler(this, iid, ppv,
		iidContainsFullScreenElementChangedEventHandler, fullscreenChangedAddRef)
}

func fullscreenChangedAddRef(this uintptr) uintptr {
	handler := (*containsFullScreenElementChangedEventHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, 1))
}

func fullscreenChangedRelease(this uintptr) uintptr {
	handler := (*containsFullScreenElementChangedEventHandler)(unsafe.Pointer(this))
	refs := atomic.AddUint32(&handler.refs, ^uint32(0))
	if refs == 0 {
		unrootCOMHandler(this)
	}
	runtime.KeepAlive(handler)
	return uintptr(refs)
}

func fullscreenChangedInvoke(this, sender, _args uintptr) uintptr {
	handler := (*containsFullScreenElementChangedEventHandler)(unsafe.Pointer(this))
	if handler == nil || handler.app == nil || sender == 0 {
		return sOK
	}
	contains, err := (*coreWebView2)(unsafe.Pointer(sender)).containsFullScreenElement()
	if err == nil {
		_ = handler.app.onHTMLFullscreenChanged(contains)
	}
	return sOK
}

const (
	navigationCompletedArgsGetIsSuccess      = 3
	navigationCompletedArgsGetWebErrorStatus = 4
	navigationCompletedArgsGetNavigationID   = 5
)

type coreWebView2NavigationCompletedEventArgs struct {
	vtbl uintptr
}

func (a *coreWebView2NavigationCompletedEventArgs) read() NavigationCompleted {
	var result NavigationCompleted
	var success int32
	if hr, _, _ := syscall.SyscallN(comMethod(unsafe.Pointer(a), navigationCompletedArgsGetIsSuccess),
		uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&success))); !failedHRESULT(hr) {
		result.Success = success != 0
	}
	var status int32
	if hr, _, _ := syscall.SyscallN(comMethod(unsafe.Pointer(a), navigationCompletedArgsGetWebErrorStatus),
		uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&status))); !failedHRESULT(hr) {
		result.WebErrorStatus = int(status)
	}
	var id uint64
	if hr, _, _ := syscall.SyscallN(comMethod(unsafe.Pointer(a), navigationCompletedArgsGetNavigationID),
		uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&id))); !failedHRESULT(hr) {
		result.ID = id
	}
	return result
}

type navigationCompletedEventHandler struct {
	vtbl *navigationCompletedEventHandlerVtbl
	refs uint32
	app  *windowsApp
}

type navigationCompletedEventHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

var navigationCompletedEventHandlerVtblInstance = navigationCompletedEventHandlerVtbl{
	QueryInterface: syscall.NewCallback(navigationCompletedQueryInterface),
	AddRef:         syscall.NewCallback(navigationCompletedAddRef),
	Release:        syscall.NewCallback(navigationCompletedRelease),
	Invoke:         syscall.NewCallback(navigationCompletedInvoke),
}

func newNavigationCompletedEventHandler(app *windowsApp) *navigationCompletedEventHandler {
	handler := &navigationCompletedEventHandler{vtbl: &navigationCompletedEventHandlerVtblInstance, refs: 1, app: app}
	rootCOMHandler(uintptr(unsafe.Pointer(handler)), handler)
	return handler
}

func navigationCompletedQueryInterface(this, iid, ppv uintptr) uintptr {
	return queryInterfaceHandler(this, iid, ppv, iidNavigationCompletedEventHandler, navigationCompletedAddRef)
}

func navigationCompletedAddRef(this uintptr) uintptr {
	handler := (*navigationCompletedEventHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&handler.refs, 1))
}

func navigationCompletedRelease(this uintptr) uintptr {
	handler := (*navigationCompletedEventHandler)(unsafe.Pointer(this))
	refs := atomic.AddUint32(&handler.refs, ^uint32(0))
	if refs == 0 {
		unrootCOMHandler(this)
	}
	runtime.KeepAlive(handler)
	return uintptr(refs)
}

func navigationCompletedInvoke(this, _sender, args uintptr) uintptr {
	handler := (*navigationCompletedEventHandler)(unsafe.Pointer(this))
	if handler == nil || handler.app == nil || args == 0 {
		return sOK
	}
	handler.app.onNavigationCompleted((*coreWebView2NavigationCompletedEventArgs)(unsafe.Pointer(args)).read())
	return sOK
}
