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
	"time"
	"unsafe"
)

type windowsApp struct {
	options Options

	mu            sync.Mutex
	primaryWindow *Window
	hwnd          uintptr

	// dispatch runs WebView2 calls on the window thread (see onUIThread).
	dispatch        uiDispatcher
	iconLarge       uintptr
	iconSmall       uintptr
	env             *coreWebView2Environment
	envRef          *comReference
	controller      *coreWebView2Controller
	controllerRef   *comReference
	webview         *coreWebView2
	webviewRef      *comReference
	settings        *coreWebView2Settings
	settingsRef     *comReference
	runErr          error
	webviewReleased bool
	singleLock      *InstanceLock

	envHandler           *environmentCompletedHandler
	envHandlerRef        *comReference
	controllerHandler    *controllerCompletedHandler
	controllerHandlerRef *comReference
	webMsgHandler        *webMessageReceivedHandler
	webMsgHandlerRef     *comReference
	webMsgToken          int64
	webMsgRegistered     bool
	processFailedHandler *processFailedEventHandler
	processFailedRef     *comReference
	processFailedToken   int64
	processFailedAdded   bool
	fullscreenHandler    *containsFullScreenElementChangedEventHandler
	fullscreenRef        *comReference
	fullscreenToken      int64
	fullscreenAdded      bool
	navCompletedHandler  *navigationCompletedEventHandler
	navCompletedRef      *comReference
	navCompletedToken    int64
	navCompletedAdded    bool
	permissionHandler    *permissionRequestedEventHandler
	permissionRef        *comReference
	permissionToken      int64
	permissionAdded      bool

	// created is when New built this app; timeline durations are measured
	// from it. backgroundBrush paints WM_ERASEBKGND when BackgroundColor is
	// set.
	created              time.Time
	timeline             StartupTimeline
	backgroundBrush      uintptr
	resHandler           *webResourceRequestedHandler
	resHandlerRef        *comReference
	resHandlerToken      int64
	resHandlerRegistered bool

	// Pending bootstrap script to register on the next controller creation.
	// Cached because AddScriptToExecuteOnDocumentCreated requires a live
	// webview; callers may queue before Run starts.
	pendingBootstrap string

	// Registered (prefix, handler) routes for the app:// (or any scheme)
	// web-resource filter. Ordered list: first match wins. Initially
	// empty; populated via App.Serve, consumed by the WebView2
	// resource-requested event handler on every matching request.
	servedRoutes []*servedRoute

	// Fullscreen events and public toggles can arrive on different goroutines,
	// so their source state and saved window state use fullscreenMu. Min/max
	// dimensions remain protected by mu.
	fullscreenMu     sync.Mutex
	fullscreen       fullscreenState
	fullscreenManual bool
	fullscreenHTML   bool
	minWidth         int32
	minHeight        int32
	maxWidth         int32
	maxHeight        int32

	// Native UI integration owned by the UI thread. The maps are guarded
	// by mu because public App methods may install menus before Run.
	pendingMenuBar      *Menu
	menuBar             uintptr
	contextMenus        map[uintptr]uintptr
	pendingTray         *TrayOptions
	focusTracker        focusStateTracker
	tray                *windowsTray
	nextNativeCommandID uint16
	menuActions         map[uint16]func()
}

func newPlatformApp(options Options) (platformApp, error) {
	return &windowsApp{options: options, created: time.Now()}, nil
}

// markStartup records the first time a startup phase finished.
func (a *windowsApp) markStartup(phase *time.Duration) {
	a.mu.Lock()
	if *phase == 0 {
		*phase = time.Since(a.created)
	}
	a.mu.Unlock()
}

// StartupTimeline implements startupTimelineReporter.
func (a *windowsApp) StartupTimeline() StartupTimeline {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.timeline
}

func (a *windowsApp) onNavigationCompleted(event NavigationCompleted) {
	a.markStartup(&a.timeline.FirstNavigationCompleted)
	a.mu.Lock()
	cb := a.options.OnNavigationCompleted
	a.mu.Unlock()
	if cb != nil {
		cb(event)
	}
}

func platformAvailable() error {
	return platformAvailableFor("")
}

func platformAvailableFor(browserExecutableFolder string) error {
	_, err := WebView2RuntimeVersion(browserExecutableFolder)
	return err
}

func (a *windowsApp) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if a.options.SingleInstance && !instanceLockHeld(a.options.AppID) {
		lock, owned, err := acquireSingleInstanceLock(a.options.AppID)
		if err != nil {
			return err
		}
		if !owned {
			defer lock.Close()
			return forwardCurrentLaunch(a.options.AppID)
		}
		heldLock := newInstanceLock(a.options.AppID, lock.Close)
		a.mu.Lock()
		a.singleLock = heldLock
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			lock := a.singleLock
			a.singleLock = nil
			a.mu.Unlock()
			if lock != nil {
				_ = lock.Close()
			}
		}()
	}

	// DPI awareness and AppUserModelID must be set before native shell
	// surfaces are created.
	setDPIAwareness(a.options.DPIAwareness)
	if err := setCurrentProcessAppUserModelID(a.options.AppID); err != nil {
		return err
	}

	if err := platformAvailableFor(a.options.BrowserExecutableFolder); err != nil {
		return err
	}
	if err := coInitializeApartment(); err != nil {
		return err
	}
	defer coUninitialize()

	if color, _, ok, _ := parseBackgroundColor(a.options.BackgroundColor); ok {
		brush, err := createSolidBrush(color)
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.backgroundBrush = brush
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			brush := a.backgroundBrush
			a.backgroundBrush = 0
			a.mu.Unlock()
			deleteGDIObject(brush)
		}()
	}

	hwnd, err := createDesktopWindow(a.options.Title, a.options.Width, a.options.Height, a)
	if err != nil {
		return err
	}
	a.markStartup(&a.timeline.WindowCreated)
	a.mu.Lock()
	a.hwnd = hwnd
	a.mu.Unlock()
	a.startDispatch(hwnd)
	large, small := setExecutableWindowIcons(hwnd)
	a.mu.Lock()
	a.iconLarge = large
	a.iconSmall = small
	a.mu.Unlock()
	enableFileDrop(hwnd, true)
	if err := applyWindowAccessibility(hwnd, a.options); err != nil {
		destroyWindow(hwnd)
		return err
	}
	defer a.releaseWebView()
	if err := a.installPendingNativeUI(hwnd); err != nil {
		destroyWindow(hwnd)
		return err
	}
	a.fireWindowCreated(hwnd)

	showWindow(hwnd)
	a.markStartup(&a.timeline.WindowShown)
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

func (a *windowsApp) navigateOnUI(url string) error {
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

func (a *windowsApp) setHTMLOnUI(html string) error {
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

func (a *windowsApp) reloadOnUI() error {
	a.mu.Lock()
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return fmt.Errorf("%w: webview not ready", ErrWebView2Unavailable)
	}
	return webview.reload()
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
	browserExecutableFolder, err := optionalUTF16Ptr(a.options.BrowserExecutableFolder)
	if err != nil {
		return err
	}
	// Merge the app's switches with any value the operator already set, so
	// field and test overrides (for example a GPU adapter) still apply.
	// Write the composed value every time: an earlier app in this process
	// may have left its own switches in the process-wide variable.
	if err := setBrowserArgumentsEnv(composeBrowserArguments(a.options, operatorBrowserArguments())); err != nil {
		return err
	}

	handler := newEnvironmentCompletedHandler(a)
	a.mu.Lock()
	a.envHandler = handler
	a.envHandlerRef = ownCOMReference(unsafe.Pointer(handler), func(ptr unsafe.Pointer) {
		environmentRelease(uintptr(ptr))
	})
	a.mu.Unlock()
	hr, _, _ := procCreateCoreWebView2EnvironmentWithOptions.Call(
		uintptr(unsafe.Pointer(browserExecutableFolder)),
		uintptr(unsafe.Pointer(userDataDir)),
		0,
		uintptr(unsafe.Pointer(handler)),
	)
	if failedHRESULT(hr) {
		err := hresultError{Op: "CreateCoreWebView2EnvironmentWithOptions", Code: hr}
		a.releaseWebView()
		return err
	}
	return nil
}

func (a *windowsApp) onEnvironmentCreated(hr uintptr, env *coreWebView2Environment) {
	var envRef *comReference
	if env != nil {
		envRef = retainCOMReference(unsafe.Pointer(env), comAddRef, comRelease)
	}
	if failedHRESULT(hr) {
		envRef.Release()
		a.failSetup(hresultError{Op: "CreateCoreWebView2EnvironmentWithOptions callback", Code: hr})
		return
	}
	if env == nil {
		a.failSetup(fmt.Errorf("%w: WebView2 environment was nil", ErrWebView2Unavailable))
		return
	}

	a.mu.Lock()
	if a.webviewReleased {
		a.mu.Unlock()
		envRef.Release()
		return
	}
	a.env = env
	a.envRef = envRef
	if a.timeline.EnvironmentReady == 0 {
		a.timeline.EnvironmentReady = time.Since(a.created)
	}
	hwnd := a.hwnd
	handler := newControllerCompletedHandler(a)
	a.controllerHandler = handler
	a.controllerHandlerRef = ownCOMReference(unsafe.Pointer(handler), func(ptr unsafe.Pointer) {
		controllerRelease(uintptr(ptr))
	})
	a.mu.Unlock()

	if err := env.createController(hwnd, handler); err != nil {
		a.failSetup(err)
	}
}

func (a *windowsApp) onControllerCreated(hr uintptr, controller *coreWebView2Controller) {
	var controllerRef *comReference
	if controller != nil {
		controllerRef = retainCOMReference(unsafe.Pointer(controller), comAddRef, comRelease)
	}
	if failedHRESULT(hr) {
		controllerRef.Release()
		a.failSetup(hresultError{Op: "CreateCoreWebView2Controller callback", Code: hr})
		return
	}
	if controller == nil {
		a.failSetup(fmt.Errorf("%w: WebView2 controller was nil", ErrWebView2Unavailable))
		return
	}
	a.mu.Lock()
	closed := a.webviewReleased
	a.mu.Unlock()
	if closed {
		controllerRef.Release()
		return
	}

	webview, err := controller.coreWebView2()
	if err != nil {
		controller.close()
		controllerRef.Release()
		a.failSetup(err)
		return
	}
	webviewRef := ownCOMReference(unsafe.Pointer(webview), comRelease)

	// Fetch + configure settings before any page loads so the first
	// navigation observes the desired policy (web-message enabled, dev
	// tools per Options.Debug, etc).
	settings, err := webview.getSettings()
	if err != nil {
		controller.close()
		webviewRef.Release()
		controllerRef.Release()
		a.failSetup(err)
		return
	}
	settingsRef := ownCOMReference(unsafe.Pointer(settings), comRelease)
	if err := configureDefaultSettings(settings, a.options); err != nil {
		controller.close()
		settingsRef.Release()
		webviewRef.Release()
		controllerRef.Release()
		a.failSetup(err)
		return
	}

	msgHandler := newWebMessageReceivedHandler(a)
	msgHandlerRef := ownCOMReference(unsafe.Pointer(msgHandler), func(ptr unsafe.Pointer) {
		webMessageReceivedRelease(uintptr(ptr))
	})
	resHandler := newWebResourceRequestedHandler(a)
	resHandlerRef := ownCOMReference(unsafe.Pointer(resHandler), func(ptr unsafe.Pointer) {
		webResourceRequestedRelease(uintptr(ptr))
	})
	processFailedHandler := newProcessFailedEventHandler(a)
	processFailedRef := ownCOMReference(unsafe.Pointer(processFailedHandler), func(ptr unsafe.Pointer) {
		processFailedRelease(uintptr(ptr))
	})
	fullscreenHandler := newContainsFullScreenElementChangedEventHandler(a)
	fullscreenRef := ownCOMReference(unsafe.Pointer(fullscreenHandler), func(ptr unsafe.Pointer) {
		fullscreenChangedRelease(uintptr(ptr))
	})
	navCompletedHandler := newNavigationCompletedEventHandler(a)
	navCompletedRef := ownCOMReference(unsafe.Pointer(navCompletedHandler), func(ptr unsafe.Pointer) {
		navigationCompletedRelease(uintptr(ptr))
	})
	var permissionHandler *permissionRequestedEventHandler
	var permissionRef *comReference
	if a.options.OnPermissionRequested != nil {
		permissionHandler = newPermissionRequestedEventHandler(a)
		permissionRef = ownCOMReference(unsafe.Pointer(permissionHandler), func(ptr unsafe.Pointer) {
			permissionRequestedRelease(uintptr(ptr))
		})
	}
	var msgToken, resToken, processFailedToken, fullscreenToken, navCompletedToken, permissionToken int64
	var msgRegistered, resRegistered, processFailedAdded, fullscreenAdded, navCompletedAdded, permissionAdded bool
	cleanupLocal := func() {
		if permissionAdded {
			_ = webview.removePermissionRequested(permissionToken)
		}
		if navCompletedAdded {
			_ = webview.removeNavigationCompleted(navCompletedToken)
		}
		if fullscreenAdded {
			_ = webview.removeFullscreenChanged(fullscreenToken)
		}
		if processFailedAdded {
			_ = webview.removeProcessFailed(processFailedToken)
		}
		if resRegistered {
			_ = webview.removeWebResourceRequested(resToken)
		}
		if msgRegistered {
			_ = webview.removeWebMessageReceived(msgToken)
		}
		controller.close()
		permissionRef.Release()
		navCompletedRef.Release()
		fullscreenRef.Release()
		processFailedRef.Release()
		resHandlerRef.Release()
		msgHandlerRef.Release()
		settingsRef.Release()
		webviewRef.Release()
		controllerRef.Release()
	}
	failLocal := func(err error) {
		cleanupLocal()
		a.failSetup(err)
	}

	// Register the JS→Go message bridge handler. Must happen BEFORE the
	// first navigation — otherwise early postMessage calls from the page
	// can race with our subscription and silently drop.
	msgToken, err = webview.addWebMessageReceived(msgHandler)
	if err != nil {
		failLocal(err)
		return
	}
	msgRegistered = true

	// Register the resource-requested handler that powers App.Serve.
	// Filter installation happens per-route as Serve is called; the
	// event handler is shared across all filters.
	resToken, err = webview.addWebResourceRequested(resHandler)
	if err != nil {
		failLocal(err)
		return
	}
	resRegistered = true
	processFailedToken, err = webview.addProcessFailed(processFailedHandler)
	if err != nil {
		failLocal(err)
		return
	}
	processFailedAdded = true
	fullscreenToken, err = webview.addFullscreenChanged(fullscreenHandler)
	if err != nil {
		failLocal(err)
		return
	}
	fullscreenAdded = true
	navCompletedToken, err = webview.addNavigationCompleted(navCompletedHandler)
	if err != nil {
		failLocal(err)
		return
	}
	navCompletedAdded = true
	if permissionHandler != nil {
		permissionToken, err = webview.addPermissionRequested(permissionHandler)
		if err != nil {
			failLocal(err)
			return
		}
		permissionAdded = true
	}
	// Replay any filters registered before the webview came up.
	a.mu.Lock()
	pendingRoutes := append([]*servedRoute(nil), a.servedRoutes...)
	a.mu.Unlock()
	for _, route := range pendingRoutes {
		if err := webview.addWebResourceRequestedFilter(filterURI(route.prefix)); err != nil {
			failLocal(err)
			return
		}
	}

	a.mu.Lock()
	a.controller = controller
	a.controllerRef = controllerRef
	a.webview = webview
	a.webviewRef = webviewRef
	a.settings = settings
	a.settingsRef = settingsRef
	a.webMsgHandler = msgHandler
	a.webMsgHandlerRef = msgHandlerRef
	a.webMsgToken = msgToken
	a.webMsgRegistered = msgRegistered
	a.processFailedHandler = processFailedHandler
	a.processFailedRef = processFailedRef
	a.processFailedToken = processFailedToken
	a.processFailedAdded = processFailedAdded
	a.fullscreenHandler = fullscreenHandler
	a.fullscreenRef = fullscreenRef
	a.fullscreenToken = fullscreenToken
	a.fullscreenAdded = fullscreenAdded
	a.navCompletedHandler = navCompletedHandler
	a.navCompletedRef = navCompletedRef
	a.navCompletedToken = navCompletedToken
	a.navCompletedAdded = navCompletedAdded
	a.permissionHandler = permissionHandler
	a.permissionRef = permissionRef
	a.permissionToken = permissionToken
	a.permissionAdded = permissionAdded
	a.resHandler = resHandler
	a.resHandlerRef = resHandlerRef
	a.resHandlerToken = resToken
	a.resHandlerRegistered = resRegistered
	html := a.options.HTML
	url := a.options.URL
	bootstrap := a.pendingBootstrap
	a.mu.Unlock()
	if a.options.MuteAudio {
		bootstrap = muteMediaAudioScript + bootstrap
	}

	if bootstrap != "" {
		if err := webview.addScriptToExecuteOnDocumentCreated(bootstrap); err != nil {
			a.failSetup(err)
			return
		}
	}

	if err := a.resizeWebView(); err != nil {
		a.failSetup(err)
		return
	}
	if color, _, ok, _ := parseBackgroundColor(a.options.BackgroundColor); ok {
		// Older runtimes lack ICoreWebView2Controller2; the window brush
		// still covers the area until the page paints.
		_ = controller.setDefaultBackgroundColor(color)
	}
	if err := controller.setVisible(true); err != nil {
		a.failSetup(err)
		return
	}
	a.markStartup(&a.timeline.ControllerReady)
	if html != "" {
		err = webview.navigateToString(html)
	} else {
		err = webview.navigate(url)
	}
	if err != nil {
		a.failSetup(err)
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

func (a *windowsApp) failSetup(err error) {
	a.releaseWebView()
	a.failRun(err)
}

func (a *windowsApp) releaseWebView() {
	a.mu.Lock()
	a.webviewReleased = true
	controller := a.controller
	controllerRef := a.controllerRef
	webview := a.webview
	webviewRef := a.webviewRef
	settingsRef := a.settingsRef
	envRef := a.envRef
	envHandlerRef := a.envHandlerRef
	controllerHandlerRef := a.controllerHandlerRef
	webMsgHandlerRef := a.webMsgHandlerRef
	resHandlerRef := a.resHandlerRef
	processFailedRef := a.processFailedRef
	fullscreenRef := a.fullscreenRef
	webMsgToken, webMsgRegistered := a.webMsgToken, a.webMsgRegistered
	resHandlerToken, resHandlerRegistered := a.resHandlerToken, a.resHandlerRegistered
	processFailedToken, processFailedAdded := a.processFailedToken, a.processFailedAdded
	fullscreenToken, fullscreenAdded := a.fullscreenToken, a.fullscreenAdded
	navCompletedRef := a.navCompletedRef
	navCompletedToken, navCompletedAdded := a.navCompletedToken, a.navCompletedAdded
	permissionRef := a.permissionRef
	permissionToken, permissionAdded := a.permissionToken, a.permissionAdded
	a.permissionHandler = nil
	a.permissionRef = nil
	a.permissionToken = 0
	a.permissionAdded = false
	a.navCompletedHandler = nil
	a.navCompletedRef = nil
	a.navCompletedToken = 0
	a.navCompletedAdded = false
	a.controller = nil
	a.controllerRef = nil
	a.webview = nil
	a.webviewRef = nil
	a.settings = nil
	a.settingsRef = nil
	a.env = nil
	a.envRef = nil
	a.envHandler = nil
	a.envHandlerRef = nil
	a.controllerHandler = nil
	a.controllerHandlerRef = nil
	a.webMsgHandler = nil
	a.webMsgHandlerRef = nil
	a.webMsgRegistered = false
	a.processFailedHandler = nil
	a.processFailedRef = nil
	a.processFailedToken = 0
	a.processFailedAdded = false
	a.fullscreenHandler = nil
	a.fullscreenRef = nil
	a.fullscreenToken = 0
	a.fullscreenAdded = false
	a.resHandler = nil
	a.resHandlerRef = nil
	a.resHandlerRegistered = false
	a.mu.Unlock()

	if webview != nil {
		if permissionAdded {
			_ = webview.removePermissionRequested(permissionToken)
		}
		if navCompletedAdded {
			_ = webview.removeNavigationCompleted(navCompletedToken)
		}
		if fullscreenAdded {
			_ = webview.removeFullscreenChanged(fullscreenToken)
		}
		if processFailedAdded {
			_ = webview.removeProcessFailed(processFailedToken)
		}
		if resHandlerRegistered {
			_ = webview.removeWebResourceRequested(resHandlerToken)
		}
		if webMsgRegistered {
			_ = webview.removeWebMessageReceived(webMsgToken)
		}
	}
	if controller != nil {
		controller.close()
	}
	settingsRef.Release()
	webviewRef.Release()
	controllerRef.Release()
	envRef.Release()
	permissionRef.Release()
	navCompletedRef.Release()
	fullscreenRef.Release()
	processFailedRef.Release()
	resHandlerRef.Release()
	webMsgHandlerRef.Release()
	controllerHandlerRef.Release()
	envHandlerRef.Release()
}

func (a *windowsApp) releaseWindowIcons(hwnd uintptr) {
	a.mu.Lock()
	large, small := a.iconLarge, a.iconSmall
	a.iconLarge = 0
	a.iconSmall = 0
	a.mu.Unlock()
	releaseExecutableWindowIcons(hwnd, large, small)
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
	// Dev tools follow Options.Debug or the narrower Options.DevTools flag.
	// The dev host leaves this off by default in production builds to hide
	// the UA and reduce attack surface while still allowing field diagnosis.
	if err := s.setBool(settingsPutAreDevToolsEnabled, "Settings.put_AreDevToolsEnabled", devToolsEnabled(o)); err != nil {
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
	policy := settingsPolicy(o.Debug)
	if err := s.setBool(settingsPutIsStatusBarEnabled, "Settings.put_IsStatusBarEnabled", policy.statusBar); err != nil {
		return err
	}
	if err := s.setBool(settingsPutIsZoomControlEnabled, "Settings.put_IsZoomControlEnabled", policy.zoomControl); err != nil {
		return err
	}
	if err := s.setBrowserAcceleratorKeysEnabled(policy.browserAcceleratorKeys); err != nil {
		return err
	}
	return nil
}

// PostMessage sends a string payload to the webview's chrome.webview event
// listener. Falls back silently when the webview hasn't finished creation
// — the caller can retry after OnWebMessage confirms the bridge is live.
func (a *windowsApp) postMessageOnUI(message string) error {
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
func (a *windowsApp) executeScriptOnUI(script string) error {
	a.mu.Lock()
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return fmt.Errorf("%w: webview not ready", ErrWebView2Unavailable)
	}
	return webview.executeScript(script)
}

// OpenDevTools pops the Chromium dev-tools inspector in a separate
// window. Requires Options.Debug or Options.DevTools; otherwise returns
// an error because the underlying setting disables the call.
func (a *windowsApp) openDevToolsOnUI() error {
	if !devToolsEnabled(a.options) {
		return fmt.Errorf("%w: Options.Debug or Options.DevTools must be true to open dev tools",
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

func (a *windowsApp) openDevToolsFromShortcut() {
	if !devToolsEnabled(a.options) {
		return
	}
	_ = a.OpenDevTools()
}

// PrependBootstrapScript queues a JS snippet that will run before every
// document load inside the webview. Calls queued before Run are stored
// in pendingBootstrap and registered once the controller completes.
func (a *windowsApp) prependBootstrapScriptOnUI(script string) error {
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
func (a *windowsApp) serveOnUI(prefix string, handler http.Handler) error {
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
	return a.setFullscreenSource(false, enabled)
}

func (a *windowsApp) onHTMLFullscreenChanged(enabled bool) error {
	return a.setFullscreenSource(true, enabled)
}

func (a *windowsApp) setFullscreenSource(html, enabled bool) error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("%w: fullscreen requires a live window", ErrInvalidOptions)
	}
	a.fullscreenMu.Lock()
	defer a.fullscreenMu.Unlock()
	if html {
		a.fullscreenHTML = enabled
	} else {
		a.fullscreenManual = enabled
	}
	return applyFullscreen(hwnd, &a.fullscreen, a.fullscreenManual || a.fullscreenHTML)
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

func (a *windowsApp) NewWindow(WindowOptions) (*Window, error) {
	return nil, fmt.Errorf("%w: multiple windows are not implemented by the Windows backend",
		ErrUnsupported)
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

func (a *windowsApp) onProcessFailed(kind ProcessFailedKind) {
	a.mu.Lock()
	cb := a.options.OnProcessFailed
	a.mu.Unlock()
	if cb != nil {
		cb(kind)
	}
}

func (a *windowsApp) fireWindowCreated(hwnd uintptr) {
	a.mu.Lock()
	cb := a.options.OnWindowCreated
	options := a.options
	window := a.primaryWindow
	if window == nil {
		window = newPrimaryWindow(hwnd, options, func(menu Menu) error {
			return a.setWindowContextMenu(hwnd, menu)
		})
		a.primaryWindow = window
	}
	a.mu.Unlock()
	if cb != nil {
		cb(window)
	}
}

// clearPrimaryWindow forgets the destroyed primary window, so App.Window
// returns nil and App.ShowMessage (for example from OnClose) does not use a
// handle Windows may reuse.
func (a *windowsApp) clearPrimaryWindow() {
	a.mu.Lock()
	a.primaryWindow = nil
	a.mu.Unlock()
}

func (a *windowsApp) PrimaryWindow() *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.primaryWindow
}

func (a *windowsApp) onFocusChanged(focused bool) {
	if !a.focusTracker.Update(focused) {
		return
	}
	a.mu.Lock()
	cb := a.options.OnFocusChanged
	a.mu.Unlock()
	if cb != nil {
		cb(focused)
	}
}

func (a *windowsApp) onSuspend() {
	a.mu.Lock()
	cb := a.options.OnSuspend
	a.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (a *windowsApp) onResume() {
	a.mu.Lock()
	cb := a.options.OnResume
	a.mu.Unlock()
	if cb != nil {
		cb()
	}
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

// Navigate runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) Navigate(url string) error {
	return a.onUIThread(func() error { return a.navigateOnUI(url) })
}

// SetHTML runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) SetHTML(html string) error {
	return a.onUIThread(func() error { return a.setHTMLOnUI(html) })
}

// Reload runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) Reload() error {
	return a.onUIThread(a.reloadOnUI)
}

// PostMessage runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) PostMessage(message string) error {
	return a.onUIThread(func() error { return a.postMessageOnUI(message) })
}

// ExecuteScript runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) ExecuteScript(script string) error {
	return a.onUIThread(func() error { return a.executeScriptOnUI(script) })
}

// OpenDevTools runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) OpenDevTools() error {
	return a.onUIThread(a.openDevToolsOnUI)
}

// PrependBootstrapScript runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) PrependBootstrapScript(script string) error {
	return a.onUIThread(func() error { return a.prependBootstrapScriptOnUI(script) })
}

// Serve runs on the window thread; WebView2 rejects calls from other threads.
func (a *windowsApp) Serve(prefix string, handler http.Handler) error {
	return a.onUIThread(func() error { return a.serveOnUI(prefix, handler) })
}
