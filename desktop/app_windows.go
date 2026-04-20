//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type windowsApp struct {
	options Options

	mu         sync.Mutex
	hwnd       uintptr
	env        *coreWebView2Environment
	controller *coreWebView2Controller
	webview    *coreWebView2
	settings   *coreWebView2Settings
	runErr     error

	envHandler        *environmentCompletedHandler
	controllerHandler *controllerCompletedHandler
	webMsgHandler     *webMessageReceivedHandler
	resHandler        *webResourceRequestedHandler

	// Pending bootstrap script to register on the next controller creation.
	// Cached because AddScriptToExecuteOnDocumentCreated requires a live
	// webview; callers may queue before Run starts.
	pendingBootstrap string

	// Registered (prefix, handler) routes for the app:// (or any scheme)
	// web-resource filter. Ordered list: first match wins. Initially
	// empty; populated via App.Serve, consumed by the WebView2
	// resource-requested event handler on every matching request.
	servedRoutes []*servedRoute

	// Fullscreen + min/max-size state, all edited only from the
	// WebView2-owning OS thread but read from app-public methods — hence
	// guarded by the same mu as the rest of the struct.
	fullscreen  fullscreenState
	minWidth    int32
	minHeight   int32
	maxWidth    int32
	maxHeight   int32
}

func newPlatformApp(options Options) (platformApp, error) {
	return &windowsApp{options: options}, nil
}

func platformAvailable() error {
	if err := procCreateCoreWebView2EnvironmentWithOptions.Find(); err != nil {
		return fmt.Errorf("%w: WebView2Loader.dll: %v", ErrWebView2Unavailable, err)
	}
	return nil
}

func (a *windowsApp) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// DPI awareness must be set before the window is created so the HWND
	// participates in per-monitor scaling.
	setDPIAware()

	if err := platformAvailable(); err != nil {
		return err
	}
	if err := coInitializeApartment(); err != nil {
		return err
	}
	defer coUninitialize()

	hwnd, err := createDesktopWindow(a.options.Title, a.options.Width, a.options.Height, a)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.hwnd = hwnd
	a.mu.Unlock()

	showWindow(hwnd)
	if err := a.createWebView(); err != nil {
		destroyWindow(hwnd)
		return err
	}
	if err := runMessageLoop(); err != nil {
		return err
	}

	// Fire OnClose after the message loop exits — ensures the callback
	// runs on the locked OS thread with the window already torn down.
	a.mu.Lock()
	onClose := a.options.OnClose
	a.mu.Unlock()
	if onClose != nil {
		onClose()
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runErr
}

func (a *windowsApp) Close() error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd != 0 {
		postMessage(hwnd, wmClose, 0, 0)
	}
	return nil
}

func (a *windowsApp) Navigate(url string) error {
	normalized, err := normalizeOptions(Options{Title: a.options.Title, Width: a.options.Width, Height: a.options.Height, URL: url})
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.options.URL = normalized.URL
	a.options.HTML = ""
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return nil
	}
	return webview.navigate(normalized.URL)
}

func (a *windowsApp) SetHTML(html string) error {
	if _, err := normalizeOptions(Options{Title: a.options.Title, Width: a.options.Width, Height: a.options.Height, HTML: html}); err != nil {
		return err
	}

	a.mu.Lock()
	a.options.HTML = html
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return nil
	}
	return webview.navigateToString(html)
}

func (a *windowsApp) createWebView() error {
	userDataDirValue, err := resolveUserDataDir(a.options.UserDataDir)
	if err != nil {
		return err
	}
	userDataDir, err := optionalUTF16Ptr(userDataDirValue)
	if err != nil {
		return err
	}

	a.envHandler = newEnvironmentCompletedHandler(a)
	hr, _, _ := procCreateCoreWebView2EnvironmentWithOptions.Call(
		0,
		uintptr(unsafe.Pointer(userDataDir)),
		0,
		uintptr(unsafe.Pointer(a.envHandler)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "CreateCoreWebView2EnvironmentWithOptions", Code: hr}
	}
	return nil
}

func (a *windowsApp) onEnvironmentCreated(hr uintptr, env *coreWebView2Environment) {
	if failedHRESULT(hr) {
		a.failRun(hresultError{Op: "CreateCoreWebView2EnvironmentWithOptions callback", Code: hr})
		return
	}
	if env == nil {
		a.failRun(fmt.Errorf("%w: WebView2 environment was nil", ErrWebView2Unavailable))
		return
	}

	a.mu.Lock()
	a.env = env
	hwnd := a.hwnd
	a.controllerHandler = newControllerCompletedHandler(a)
	handler := a.controllerHandler
	a.mu.Unlock()

	if err := env.createController(hwnd, handler); err != nil {
		a.failRun(err)
	}
}

func (a *windowsApp) onControllerCreated(hr uintptr, controller *coreWebView2Controller) {
	if failedHRESULT(hr) {
		a.failRun(hresultError{Op: "CreateCoreWebView2Controller callback", Code: hr})
		return
	}
	if controller == nil {
		a.failRun(fmt.Errorf("%w: WebView2 controller was nil", ErrWebView2Unavailable))
		return
	}

	webview, err := controller.coreWebView2()
	if err != nil {
		a.failRun(err)
		return
	}

	// Fetch + configure settings before any page loads so the first
	// navigation observes the desired policy (web-message enabled, dev
	// tools per Options.Debug, etc).
	settings, err := webview.getSettings()
	if err != nil {
		a.failRun(err)
		return
	}
	if err := configureDefaultSettings(settings, a.options); err != nil {
		a.failRun(err)
		return
	}

	// Register the JS→Go message bridge handler. Must happen BEFORE the
	// first navigation — otherwise early postMessage calls from the page
	// can race with our subscription and silently drop.
	msgHandler := newWebMessageReceivedHandler(a)
	if err := webview.addWebMessageReceived(msgHandler); err != nil {
		a.failRun(err)
		return
	}

	// Register the resource-requested handler that powers App.Serve.
	// Filter installation happens per-route as Serve is called; the
	// event handler is shared across all filters.
	resHandler := newWebResourceRequestedHandler(a)
	if err := webview.addWebResourceRequested(resHandler); err != nil {
		a.failRun(err)
		return
	}
	// Replay any filters registered before the webview came up.
	a.mu.Lock()
	pendingRoutes := append([]*servedRoute(nil), a.servedRoutes...)
	a.mu.Unlock()
	for _, route := range pendingRoutes {
		if err := webview.addWebResourceRequestedFilter(filterURI(route.prefix)); err != nil {
			a.failRun(err)
			return
		}
	}

	a.mu.Lock()
	a.controller = controller
	a.webview = webview
	a.settings = settings
	a.webMsgHandler = msgHandler
	a.resHandler = resHandler
	html := a.options.HTML
	url := a.options.URL
	bootstrap := a.pendingBootstrap
	a.mu.Unlock()

	if bootstrap != "" {
		if err := webview.addScriptToExecuteOnDocumentCreated(bootstrap); err != nil {
			a.failRun(err)
			return
		}
	}

	if err := a.resizeWebView(); err != nil {
		a.failRun(err)
		return
	}
	if err := controller.setVisible(true); err != nil {
		a.failRun(err)
		return
	}
	if html != "" {
		err = webview.navigateToString(html)
	} else {
		err = webview.navigate(url)
	}
	if err != nil {
		a.failRun(err)
	}
}

func (a *windowsApp) resizeWebView() error {
	a.mu.Lock()
	hwnd := a.hwnd
	controller := a.controller
	a.mu.Unlock()
	if hwnd == 0 || controller == nil {
		return nil
	}
	bounds, err := clientRect(hwnd)
	if err != nil {
		return err
	}
	return controller.setBounds(bounds)
}

func (a *windowsApp) failRun(err error) {
	a.mu.Lock()
	if a.runErr == nil {
		a.runErr = err
	}
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd != 0 {
		postMessage(hwnd, wmClose, 0, 0)
	}
}

func (a *windowsApp) releaseWebView() {
	a.mu.Lock()
	controller := a.controller
	webview := a.webview
	settings := a.settings
	env := a.env
	a.controller = nil
	a.webview = nil
	a.settings = nil
	a.env = nil
	a.mu.Unlock()

	if controller != nil {
		controller.close()
	}
	if settings != nil {
		comRelease(unsafe.Pointer(settings))
	}
	if webview != nil {
		comRelease(unsafe.Pointer(webview))
	}
	if controller != nil {
		comRelease(unsafe.Pointer(controller))
	}
	if env != nil {
		comRelease(unsafe.Pointer(env))
	}
}

// configureDefaultSettings applies the Windows-specific settings policy
// derived from the Options struct. Called once immediately after the
// WebView2 settings object is obtained, before any navigation.
func configureDefaultSettings(s *coreWebView2Settings, o Options) error {
	// JavaScript must always be on — the webview's whole purpose is to
	// host a GoSX .gsx app.
	if err := s.setBool(settingsPutIsScriptEnabled, "Settings.put_IsScriptEnabled", true); err != nil {
		return err
	}
	// chrome.webview.postMessage needs this flag to fire events the host
	// listens for. Without it, our JS→Go bridge is a no-op.
	if err := s.setBool(settingsPutIsWebMessageEnabled, "Settings.put_IsWebMessageEnabled", true); err != nil {
		return err
	}
	// Dev tools follow Options.Debug — F12 + "Inspect Element" both require
	// this flag to be true. The dev host leaves it off in production builds
	// to hide the UA and reduce attack surface.
	if err := s.setBool(settingsPutAreDevToolsEnabled, "Settings.put_AreDevToolsEnabled", o.Debug); err != nil {
		return err
	}
	// Default context menu (right-click → Reload, Inspect, etc). Disabled
	// when not in Debug so the app feels like a native desktop app rather
	// than a browser. Apps that want richer context menus implement their
	// own via the JS bridge.
	if err := s.setBool(settingsPutAreDefaultContextMenusEnabled,
		"Settings.put_AreDefaultContextMenusEnabled", o.Debug); err != nil {
		return err
	}
	return nil
}

// PostMessage sends a string payload to the webview's chrome.webview event
// listener. Falls back silently when the webview hasn't finished creation
// — the caller can retry after OnWebMessage confirms the bridge is live.
func (a *windowsApp) PostMessage(message string) error {
	a.mu.Lock()
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return fmt.Errorf("%w: webview not ready", ErrWebView2Unavailable)
	}
	return webview.postWebMessageAsString(message)
}

// ExecuteScript runs arbitrary JavaScript in the top-level frame. The
// completion handler form is ignored for simplicity — callers that need
// the return value can use PostMessage to round-trip through JS.
func (a *windowsApp) ExecuteScript(script string) error {
	a.mu.Lock()
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return fmt.Errorf("%w: webview not ready", ErrWebView2Unavailable)
	}
	return webview.executeScript(script)
}

// OpenDevTools pops the Chromium dev-tools inspector in a separate
// window. Requires Options.Debug = true; otherwise returns an error
// because the underlying setting disables the call.
func (a *windowsApp) OpenDevTools() error {
	if !a.options.Debug {
		return fmt.Errorf("%w: Options.Debug must be true to open dev tools",
			ErrInvalidOptions)
	}
	a.mu.Lock()
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return fmt.Errorf("%w: webview not ready", ErrWebView2Unavailable)
	}
	return webview.openDevToolsWindow()
}

// PrependBootstrapScript queues a JS snippet that will run before every
// document load inside the webview. Calls queued before Run are stored
// in pendingBootstrap and registered once the controller completes.
func (a *windowsApp) PrependBootstrapScript(script string) error {
	a.mu.Lock()
	webview := a.webview
	a.pendingBootstrap = script
	a.mu.Unlock()
	if webview == nil {
		return nil
	}
	return webview.addScriptToExecuteOnDocumentCreated(script)
}

// Minimize hides the native window in the taskbar without destroying the
// webview.
func (a *windowsApp) Minimize() error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("%w: window not ready", ErrInvalidOptions)
	}
	showWindowState(hwnd, swMinimize)
	return nil
}

// Maximize snaps the native window to the full work-area of the monitor.
func (a *windowsApp) Maximize() error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("%w: window not ready", ErrInvalidOptions)
	}
	showWindowState(hwnd, swShowMaximized)
	return nil
}

// Restore returns the window from minimized or maximized state back to
// its prior normal bounds.
func (a *windowsApp) Restore() error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("%w: window not ready", ErrInvalidOptions)
	}
	showWindowState(hwnd, swRestore)
	return nil
}

// Focus brings the native window to the foreground and gives it keyboard
// focus. The OS may veto the call under foreground-lock rules.
func (a *windowsApp) Focus() error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	return focusWindow(hwnd)
}

// SetTitle updates both the in-process Options cache and the live window
// caption, so subsequent GetOptions reads observe the new title.
func (a *windowsApp) SetTitle(title string) error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.options.Title = title
	a.mu.Unlock()
	return setWindowTitle(hwnd, title)
}

// Serve registers a handler for URIs whose scheme+host+path start with
// prefix. Called any time during the app lifecycle — if invoked before
// Run the filter is cached and replayed on webview creation; if after,
// the filter is installed immediately.
//
// Prefix semantics match Go's http.ServeMux loosely: "app://assets/*"
// matches everything rooted at "app://assets/". Registrations are
// first-match-wins in insertion order, so specific prefixes should be
// registered before generic ones.
func (a *windowsApp) Serve(prefix string, handler http.Handler) error {
	if strings.TrimSpace(prefix) == "" {
		return fmt.Errorf("%w: serve prefix must be non-empty", ErrInvalidOptions)
	}
	if handler == nil {
		return fmt.Errorf("%w: serve handler must be non-nil", ErrInvalidOptions)
	}

	a.mu.Lock()
	webview := a.webview
	route := &servedRoute{prefix: prefix, handler: handler}
	a.servedRoutes = append(a.servedRoutes, route)
	a.mu.Unlock()

	if webview == nil {
		return nil
	}
	return webview.addWebResourceRequestedFilter(filterURI(prefix))
}

// filterURI normalizes a Go-side prefix into WebView2's filter-string
// format. WV2 matches on a URI with optional `*` wildcards; our "app://x/*"
// style happens to work directly, but we still pass through a function so
// future scheme mappings (like mapping Go's "/assets/" to "https://app/assets/*")
// can land here without touching callers.
func filterURI(prefix string) string {
	return prefix
}

// SetFullscreen toggles borderless-fullscreen mode for the hosted window.
// On entry, saves the current chrome style + bounds so the reverse call
// restores the user's pre-fullscreen window rect rather than a maximized
// approximation.
func (a *windowsApp) SetFullscreen(enabled bool) error {
	a.mu.Lock()
	hwnd := a.hwnd
	state := &a.fullscreen
	a.mu.Unlock()
	return applyFullscreen(hwnd, state, enabled)
}

// SetMinSize configures the minimum resize dimensions the window enforces
// via WM_GETMINMAXINFO. A zero value means "no minimum" for that axis.
// Called before or after Run; takes effect the next time the user drags
// a resize grip.
func (a *windowsApp) SetMinSize(width, height int) error {
	a.mu.Lock()
	a.minWidth = int32(width)
	a.minHeight = int32(height)
	a.mu.Unlock()
	return nil
}

// SetMaxSize caps the maximum resize dimensions. Zero = no cap on that
// axis. As with SetMinSize, the constraint is applied by the default
// window procedure on each resize event.
func (a *windowsApp) SetMaxSize(width, height int) error {
	a.mu.Lock()
	a.maxWidth = int32(width)
	a.maxHeight = int32(height)
	a.mu.Unlock()
	return nil
}

// applyMinMaxTo stamps the MINMAXINFO response with our cached constraints.
// Runs on the UI thread from inside wndProc; the lock is short because
// the values are cheap scalars.
func (a *windowsApp) applyMinMaxTo(info *mINMAXINFO) {
	a.mu.Lock()
	minW, minH := a.minWidth, a.minHeight
	maxW, maxH := a.maxWidth, a.maxHeight
	a.mu.Unlock()
	if minW > 0 {
		info.ptMinTrackSize.X = minW
	}
	if minH > 0 {
		info.ptMinTrackSize.Y = minH
	}
	if maxW > 0 {
		info.ptMaxTrackSize.X = maxW
	}
	if maxH > 0 {
		info.ptMaxTrackSize.Y = maxH
	}
}

// onWebMessage dispatches an incoming JS→Go message to the user callback
// registered on Options.OnWebMessage. Runs on the WebView2 dispatcher
// thread — the callback must be short and non-blocking.
func (a *windowsApp) onWebMessage(message string) {
	a.mu.Lock()
	cb := a.options.OnWebMessage
	a.mu.Unlock()
	if cb == nil {
		return
	}
	cb(message)
}

func optionalUTF16Ptr(value string) (*uint16, error) {
	if value == "" {
		return nil, nil
	}
	ptr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	return ptr, nil
}

func resolveUserDataDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", nil
	}
	dir := filepath.Join(cacheDir, "GoSX", "WebView2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create WebView2 user data dir: %w", err)
	}
	return dir, nil
}
