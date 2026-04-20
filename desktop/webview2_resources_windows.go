//go:build windows && (amd64 || arm64)

package desktop

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Vtable indices for the WebView2 resource-interception surface. Stable
// across ICoreWebView2 1.0 — appended-only on later versions.
const (
	// ICoreWebView2.
	webViewAddWebResourceRequestedFilter    = 46
	webViewRemoveWebResourceRequestedFilter = 47
	webViewAddWebResourceRequested          = 48
	webViewRemoveWebResourceRequested       = 49

	// ICoreWebView2Environment.
	environmentCreateWebResourceResponse = 6

	// ICoreWebView2WebResourceRequestedEventArgs.
	resourceArgsGetRequest  = 3
	resourceArgsPutResponse = 5

	// ICoreWebView2WebResourceRequest.
	resourceRequestGetUri    = 3
	resourceRequestGetMethod = 5

	// CoreWebView2WebResourceContext: 0 = ALL
	resourceContextAll = 0
)

var (
	modShlwapi          = syscall.NewLazyDLL("shlwapi.dll")
	procSHCreateMemStream = modShlwapi.NewProc("SHCreateMemStream")
)

// coreWebView2WebResourceRequest / RequestedEventArgs / Response wrap the
// WebView2 event interfaces we interact with. Each carries only the vtbl
// pointer that COM expects as its first field.
type coreWebView2WebResourceRequest struct {
	vtbl uintptr
}

type coreWebView2WebResourceRequestedEventArgs struct {
	vtbl uintptr
}

type coreWebView2WebResourceResponse struct {
	vtbl uintptr
}

// servedRoute is the Go-side registration for one (urlPrefix, handler)
// pair. Held by windowsApp.servedRoutes; read by the resource-requested
// handler on every intercepted request to decide whether to satisfy it.
type servedRoute struct {
	prefix  string
	handler http.Handler
}

// addWebResourceRequestedFilter tells WebView2 which URLs to route through
// the registered WebResourceRequested handler. Supports glob-style `*`
// in the URI; passing `*://app/*` filters everything under the `app`
// host across all schemes, which is the common case.
func (w *coreWebView2) addWebResourceRequestedFilter(uri string) error {
	ptr, err := syscall.UTF16PtrFromString(uri)
	if err != nil {
		return fmt.Errorf("%w: filter uri: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewAddWebResourceRequestedFilter),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(ptr)),
		uintptr(resourceContextAll),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.AddWebResourceRequestedFilter", Code: hr}
	}
	return nil
}

// addWebResourceRequested registers the Go-side resource-requested handler.
// WebView2 fires handler.Invoke for every fetch that matches an earlier
// AddWebResourceRequestedFilter call.
func (w *coreWebView2) addWebResourceRequested(handler *webResourceRequestedHandler) error {
	var token int64
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(w), webViewAddWebResourceRequested),
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(&token)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ICoreWebView2.add_WebResourceRequested", Code: hr}
	}
	return nil
}

// createWebResourceResponse wraps ICoreWebView2Environment.CreateWebResourceResponse.
// The content pointer is a COM IStream — we build one from Go bytes via
// SHCreateMemStream. Headers are passed as a CRLF-joined string per the
// WebView2 ABI, not as a structured collection.
func (e *coreWebView2Environment) createWebResourceResponse(content uintptr, statusCode int, reasonPhrase, headers string) (*coreWebView2WebResourceResponse, error) {
	reasonPtr, err := syscall.UTF16PtrFromString(reasonPhrase)
	if err != nil {
		return nil, fmt.Errorf("%w: reasonPhrase: %v", ErrInvalidOptions, err)
	}
	headersPtr, err := syscall.UTF16PtrFromString(headers)
	if err != nil {
		return nil, fmt.Errorf("%w: headers: %v", ErrInvalidOptions, err)
	}
	var response *coreWebView2WebResourceResponse
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(e), environmentCreateWebResourceResponse),
		uintptr(unsafe.Pointer(e)),
		content,
		uintptr(statusCode),
		uintptr(unsafe.Pointer(reasonPtr)),
		uintptr(unsafe.Pointer(headersPtr)),
		uintptr(unsafe.Pointer(&response)),
	)
	if failedHRESULT(hr) {
		return nil, hresultError{Op: "ICoreWebView2Environment.CreateWebResourceResponse", Code: hr}
	}
	return response, nil
}

// getUri extracts the request URI. WebView2 allocates the string via
// CoTaskMemAlloc; the caller frees it.
func (r *coreWebView2WebResourceRequest) getUri() (string, error) {
	var out *uint16
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(r), resourceRequestGetUri),
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&out)),
	)
	if failedHRESULT(hr) {
		return "", hresultError{Op: "WebResourceRequest.get_Uri", Code: hr}
	}
	if out == nil {
		return "", nil
	}
	uri := utf16PtrToString(out)
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(out)))
	return uri, nil
}

// getMethod extracts the HTTP method ("GET", "POST", etc). Allocation is
// the caller's to free, same as getUri.
func (r *coreWebView2WebResourceRequest) getMethod() (string, error) {
	var out *uint16
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(r), resourceRequestGetMethod),
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&out)),
	)
	if failedHRESULT(hr) {
		return "", hresultError{Op: "WebResourceRequest.get_Method", Code: hr}
	}
	if out == nil {
		return "GET", nil
	}
	method := utf16PtrToString(out)
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(out)))
	return method, nil
}

// getRequest fetches the request object from the event args.
func (a *coreWebView2WebResourceRequestedEventArgs) getRequest() (*coreWebView2WebResourceRequest, error) {
	var req *coreWebView2WebResourceRequest
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(a), resourceArgsGetRequest),
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&req)),
	)
	if failedHRESULT(hr) {
		return nil, hresultError{Op: "ResourceRequestedEventArgs.get_Request", Code: hr}
	}
	if req == nil {
		return nil, fmt.Errorf("%w: request was nil", ErrWebView2Unavailable)
	}
	return req, nil
}

// putResponse installs our handler's response on the event args, which
// short-circuits the normal network path and serves our bytes instead.
func (a *coreWebView2WebResourceRequestedEventArgs) putResponse(resp *coreWebView2WebResourceResponse) error {
	hr, _, _ := syscall.SyscallN(
		comMethod(unsafe.Pointer(a), resourceArgsPutResponse),
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(resp)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "ResourceRequestedEventArgs.put_Response", Code: hr}
	}
	return nil
}

// shCreateMemStream allocates a COM IStream backed by a private copy of
// data. Windows 2000+ ships this in shlwapi.dll. Callers comRelease the
// returned stream; CreateWebResourceResponse takes its own reference so
// our side can release immediately after the response is built.
func shCreateMemStream(data []byte) (uintptr, error) {
	if err := procSHCreateMemStream.Find(); err != nil {
		return 0, fmt.Errorf("SHCreateMemStream unavailable: %w", err)
	}
	var ptr unsafe.Pointer
	if len(data) > 0 {
		ptr = unsafe.Pointer(&data[0])
	}
	ret, _, _ := procSHCreateMemStream.Call(uintptr(ptr), uintptr(len(data)))
	if ret == 0 {
		return 0, fmt.Errorf("%w: SHCreateMemStream returned NULL", ErrWebView2Unavailable)
	}
	return ret, nil
}

// webResourceRequestedHandler implements ICoreWebView2WebResourceRequestedEventHandler.
// One instance per app. Lives for the app's lifetime — WebView2 holds a
// strong reference through add_WebResourceRequested's internal token, so
// we don't need to manage removal explicitly.
type webResourceRequestedHandler struct {
	vtbl *webResourceRequestedHandlerVtbl
	refs uint32
	app  *windowsApp
}

type webResourceRequestedHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

var webResourceRequestedHandlerVtblInstance = webResourceRequestedHandlerVtbl{
	QueryInterface: syscall.NewCallback(webResourceRequestedQueryInterface),
	AddRef:         syscall.NewCallback(webResourceRequestedAddRef),
	Release:        syscall.NewCallback(webResourceRequestedRelease),
	Invoke:         syscall.NewCallback(webResourceRequestedInvoke),
}

func newWebResourceRequestedHandler(app *windowsApp) *webResourceRequestedHandler {
	return &webResourceRequestedHandler{
		vtbl: &webResourceRequestedHandlerVtblInstance,
		refs: 1,
		app:  app,
	}
}

func webResourceRequestedQueryInterface(this, _, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = this
	webResourceRequestedAddRef(this)
	return sOK
}

func webResourceRequestedAddRef(this uintptr) uintptr {
	h := (*webResourceRequestedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&h.refs, 1))
}

func webResourceRequestedRelease(this uintptr) uintptr {
	h := (*webResourceRequestedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&h.refs, ^uint32(0)))
}

// webResourceRequestedInvoke is the per-request hot path. Runs on the
// WebView2 dispatcher thread. We extract URI + method, run the matching
// Go handler to produce a response, build a WebView2 response object,
// and install it on the args.
//
// Errors are swallowed with a 500 — returning an HRESULT failure to the
// Invoke callback could destabilize the webview's resource loader.
func webResourceRequestedInvoke(this, sender, args uintptr) uintptr {
	_ = sender
	h := (*webResourceRequestedHandler)(unsafe.Pointer(this))
	if h == nil || h.app == nil || args == 0 {
		return sOK
	}
	eventArgs := (*coreWebView2WebResourceRequestedEventArgs)(unsafe.Pointer(args))
	h.app.handleResourceRequest(eventArgs)
	return sOK
}

// handleResourceRequest orchestrates the per-request pipeline. Broken out
// of webResourceRequestedInvoke so the Go logic is testable via the
// platformApp interface on non-Windows builds.
func (a *windowsApp) handleResourceRequest(args *coreWebView2WebResourceRequestedEventArgs) {
	req, err := args.getRequest()
	if err != nil {
		return
	}
	defer comRelease(unsafe.Pointer(req))

	uri, err := req.getUri()
	if err != nil || uri == "" {
		return
	}
	method, err := req.getMethod()
	if err != nil {
		method = "GET"
	}

	route := a.matchRoute(uri)
	if route == nil {
		return // no registered handler → WebView2 handles the URL normally
	}

	// Run the handler through httptest.NewRecorder so we get ordinary
	// ResponseWriter semantics without owning the plumbing.
	httpReq, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return
	}
	rw := httptest.NewRecorder()
	route.handler.ServeHTTP(rw, httpReq)
	resp := rw.Result()
	defer resp.Body.Close()

	var body bytes.Buffer
	if _, copyErr := body.ReadFrom(resp.Body); copyErr != nil {
		return
	}
	stream, err := shCreateMemStream(body.Bytes())
	if err != nil {
		return
	}
	defer comRelease(unsafe.Pointer(stream))

	headers := formatResponseHeaders(resp.Header)
	reason := resp.Status
	if idx := strings.IndexByte(reason, ' '); idx >= 0 {
		reason = reason[idx+1:]
	}

	a.mu.Lock()
	env := a.env
	a.mu.Unlock()
	if env == nil {
		return
	}
	response, err := env.createWebResourceResponse(stream, resp.StatusCode, reason, headers)
	if err != nil {
		return
	}
	_ = args.putResponse(response)
	comRelease(unsafe.Pointer(response))
}

// matchRoute finds the first registered handler whose prefix matches uri.
// Priority is registration order — callers register more specific prefixes
// earlier to override broader ones.
func (a *windowsApp) matchRoute(uri string) *servedRoute {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, route := range a.servedRoutes {
		if matchRoutePrefix(route.prefix, uri) {
			return route
		}
	}
	return nil
}

// matchRoutePrefix accepts a filter like "app://assets/*" and reports
// whether uri is covered. The WebView2 filter system also does globbing,
// but we re-check on the Go side so handlers registered with the same
// prefix filter can dispatch deterministically in their registration
// order.
func matchRoutePrefix(prefix, uri string) bool {
	if prefix == "*" {
		return true
	}
	if strings.HasSuffix(prefix, "/*") {
		return strings.HasPrefix(uri, strings.TrimSuffix(prefix, "/*")+"/")
	}
	return uri == prefix
}

// formatResponseHeaders joins http.Header as WebView2 expects: each
// canonical "Name: value" pair separated by CRLF. Multi-value headers
// emit one line per value.
func formatResponseHeaders(h http.Header) string {
	var b strings.Builder
	for name, values := range h {
		for _, v := range values {
			if b.Len() > 0 {
				b.WriteString("\r\n")
			}
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(v)
		}
	}
	return b.String()
}
