//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// WebView2 COM vtable indices beyond the minimal set in webview2_windows.go.
// Indices are stable across ICoreWebView2 1.0 — new methods only ever
// append, so as long as we only reach into documented positions the ABI
// contract holds.
const (
	// ICoreWebView2 additions.
	webViewGetSettings                         = 3
	webViewAddScriptToExecuteOnDocumentCreated = 27
	webViewExecuteScript                       = 29
	webViewPostWebMessageAsString              = 33
	webViewAddWebMessageReceived               = 34
	webViewRemoveWebMessageReceived            = 35
	webViewOpenDevToolsWindow                  = 44

	// ICoreWebView2Settings.
	settingsPutIsScriptEnabled               = 4
	settingsPutIsWebMessageEnabled           = 6
	settingsPutAreDevToolsEnabled            = 12
	settingsPutAreDefaultContextMenusEnabled = 14

	// ICoreWebView2WebMessageReceivedEventArgs.
	webMessageArgsTryGetAsString = 5
)

var procCoTaskMemFree = modOle32.NewProc("CoTaskMemFree")

// coreWebView2Settings wraps ICoreWebView2Settings, obtained via
// coreWebView2.getSettings().
type coreWebView2Settings struct {
	vtbl uintptr
}

// getSettings fetches the ICoreWebView2Settings object for this webview.
// The returned settings object carries its own refcount — caller must
// comRelease when finished with it.
func (w *coreWebView2) getSettings() (*coreWebView2Settings, error) {
	var settings *coreWebView2Settings
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewGetSettings),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(&settings)),
	)
	if failedHRESULT(hr) {
		return nil, hresultError{Op: "ICoreWebView2.get_Settings", Code: hr}
	}
	if settings == nil {
		return nil, fmt.Errorf("%w: ICoreWebView2Settings was nil", ErrWebView2Unavailable)
	}
	return settings, nil
}

// setBool invokes a setter at the given vtbl index with a BOOL argument.
func (s *coreWebView2Settings) setBool(index uintptr, op string, value bool) error {
	var v uintptr
	if value {
		v = 1
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(s), index),
		uintptr(unsafe.Pointer(s)),
		v,
	)
	if failedHRESULT(hr) {
		return hresultError{Op: op, Code: hr}
	}
	return nil
}

// executeScript runs arbitrary JS in the top-level frame. The completion
// handler form is ignored (pass nil) — the simplest use case for a desktop
// app is fire-and-forget script evaluation from Go.
func (w *coreWebView2) executeScript(script string) error {
	ptr, err := syscall.UTF16PtrFromString(script)
	if err != nil {
		return fmt.Errorf("%w: script: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewExecuteScript),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(ptr)),
		0, // ICoreWebView2ExecuteScriptCompletedHandler* (ignored)
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.ExecuteScript", Code: hr}
	}
	return nil
}

// postWebMessageAsString delivers a raw string to the page's
// chrome.webview event listener. Strings survive COM boundary as UTF-16.
// For structured data, callers can JSON-encode on the Go side and
// JSON.parse on the JS side; there's no PostWebMessageAsJson variant
// exposed here because postWebMessageAsString is friendlier to the
// one-way Go→JS fire-and-forget pattern.
func (w *coreWebView2) postWebMessageAsString(message string) error {
	ptr, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return fmt.Errorf("%w: message: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewPostWebMessageAsString),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(ptr)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.PostWebMessageAsString", Code: hr}
	}
	return nil
}

// addScriptToExecuteOnDocumentCreated registers a JS snippet run before
// every document load. Useful for bootstrapping the Go↔JS bridge — the
// preamble script can attach chrome.webview.addEventListener('message',...)
// so the page's app code sees messages synchronously from load.
func (w *coreWebView2) addScriptToExecuteOnDocumentCreated(script string) error {
	ptr, err := syscall.UTF16PtrFromString(script)
	if err != nil {
		return fmt.Errorf("%w: bootstrap script: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewAddScriptToExecuteOnDocumentCreated),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(ptr)),
		0, // ICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandler* (ignored)
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.AddScriptToExecuteOnDocumentCreated", Code: hr}
	}
	return nil
}

// openDevToolsWindow opens the Chromium DevTools in a separate window.
// Requires Settings.AreDevToolsEnabled = true to be effective.
func (w *coreWebView2) openDevToolsWindow() error {
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewOpenDevToolsWindow),
		uintptr(unsafe.Pointer(w)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.OpenDevToolsWindow", Code: hr}
	}
	return nil
}

// Message-received event plumbing --------------------------------------

// coreWebView2WebMessageReceivedEventArgs wraps the args structure the
// WebView2 runtime hands to our registered WebMessageReceived handler.
type coreWebView2WebMessageReceivedEventArgs struct {
	vtbl uintptr
}

// tryGetAsString pulls a UTF-16 string from a web message args struct and
// copies it back as a Go string. The runtime allocates the string via
// CoTaskMemAlloc; we free it immediately after conversion.
func (a *coreWebView2WebMessageReceivedEventArgs) tryGetAsString() (string, error) {
	var out *uint16
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(a), webMessageArgsTryGetAsString),
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&out)),
	)
	if failedHRESULT(hr) {
		return "", hresultError{Op: "WebMessageReceivedEventArgs.TryGetWebMessageAsString", Code: hr}
	}
	if out == nil {
		// Page posted a non-string (object); ignore for now.
		return "", nil
	}
	msg := utf16PtrToString(out)
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(out)))
	return msg, nil
}

// webMessageReceivedHandler implements ICoreWebView2WebMessageReceivedEventHandler.
// One instance per app. Never released while the app is alive; WebView2
// keeps its own reference to the handler vtable via add_WebMessageReceived.
type webMessageReceivedHandler struct {
	vtbl *webMessageReceivedHandlerVtbl
	refs uint32
	app  *windowsApp
}

type webMessageReceivedHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

var webMessageReceivedHandlerVtblInstance = webMessageReceivedHandlerVtbl{
	QueryInterface: syscall.NewCallback(webMessageReceivedQueryInterface),
	AddRef:         syscall.NewCallback(webMessageReceivedAddRef),
	Release:        syscall.NewCallback(webMessageReceivedRelease),
	Invoke:         syscall.NewCallback(webMessageReceivedInvoke),
}

func newWebMessageReceivedHandler(app *windowsApp) *webMessageReceivedHandler {
	return &webMessageReceivedHandler{
		vtbl: &webMessageReceivedHandlerVtblInstance,
		refs: 1,
		app:  app,
	}
}

func webMessageReceivedQueryInterface(this, _, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = this
	webMessageReceivedAddRef(this)
	return sOK
}

func webMessageReceivedAddRef(this uintptr) uintptr {
	h := (*webMessageReceivedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&h.refs, 1))
}

func webMessageReceivedRelease(this uintptr) uintptr {
	h := (*webMessageReceivedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&h.refs, ^uint32(0)))
}

// webMessageReceivedInvoke is the single entry point WebView2 hits for
// every postMessage originating in JS. Signature:
//
//	HRESULT Invoke(ICoreWebView2 *sender, ICoreWebView2WebMessageReceivedEventArgs *args)
func webMessageReceivedInvoke(this, sender, args uintptr) uintptr {
	_ = sender
	h := (*webMessageReceivedHandler)(unsafe.Pointer(this))
	if h == nil || h.app == nil || args == 0 {
		return sOK
	}
	a := (*coreWebView2WebMessageReceivedEventArgs)(unsafe.Pointer(args))
	msg, err := a.tryGetAsString()
	if err != nil {
		return sOK // swallow — can't surface to JS, don't want to break event loop
	}
	h.app.onWebMessage(msg)
	return sOK
}

// addWebMessageReceived registers a handler for chrome.webview.postMessage
// calls from JS. Returns a u64 event-registration token; for the desktop
// app we don't currently unregister (handler lives as long as the webview).
func (w *coreWebView2) addWebMessageReceived(handler *webMessageReceivedHandler) error {
	var token int64
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewAddWebMessageReceived),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(&token)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.add_WebMessageReceived", Code: hr}
	}
	return nil
}

// utf16PtrToString walks a null-terminated UTF-16 string at ptr and
// returns it as a Go string. Reimplemented here because
// syscall.UTF16PtrToString was introduced in Go 1.15 but the signature
// differs across versions — using a local copy avoids version pinning.
func utf16PtrToString(ptr *uint16) string {
	if ptr == nil {
		return ""
	}
	// Walk to the null terminator.
	start := uintptr(unsafe.Pointer(ptr))
	size := uintptr(0)
	for {
		c := *(*uint16)(unsafe.Pointer(start + size*2))
		if c == 0 {
			break
		}
		size++
		if size > 1<<20 {
			// Defensive cap — 1M UTF-16 code units is absurdly large for
			// a postMessage payload, bail rather than scan forever.
			break
		}
	}
	if size == 0 {
		return ""
	}
	slice := unsafe.Slice(ptr, size)
	return syscall.UTF16ToString(slice)
}
