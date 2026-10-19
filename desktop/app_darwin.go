//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// darwinApp is the macOS backend: one NSWindow holding one WKWebView, driven by
// a Go-pumped AppKit event loop on the main thread. DT0 slice of
// hypha://m31labs/gosx/initiatives/2026-10-17-darwin-desktop-backend.
//
// Thread model. The main goroutine, locked to the main OS thread by init
// (lifecycle_darwin.go), runs Run. Run creates the AppKit objects and then loops
// on -[NSApplication nextEventMatchingMask:untilDate:inMode:dequeue:], which
// blocks in AppKit with the main run loop serviced (so WebKit's IPC and delegate
// calls are delivered) until an event arrives. After every event it drains two
// queues: the native event ring that the assembly delegate methods fill
// (drainNativeEvents), and the uiDispatcher queue that other goroutines post to
// (ui_thread_darwin.go). Every AppKit and WebKit call happens on this thread.
type darwinApp struct {
	options Options

	mu            sync.Mutex
	primaryWindow *Window

	// Native handles. They are written and used only on the main thread; the
	// handle is mirrored under mu for the few readers on other goroutines.
	nsapp    uintptr
	window   uintptr
	webview  uintptr
	delegate uintptr
	ring     *nativeRing

	// mode is NSDefaultRunLoopMode ("kCFRunLoopDefaultMode"), retained for the
	// life of one Run.
	mode *objcRef

	// dispatch runs AppKit and WebKit calls on the main thread (see onUIThread).
	dispatch uiDispatcher

	// created is when New built this app; timeline durations are measured from
	// it.
	created  time.Time
	timeline StartupTimeline
	runErr   error

	// pendingBootstrap is the combined preload script. App.PrependBootstrapScript
	// passes the whole list every time, so it replaces rather than appends.
	pendingBootstrap string

	// appName is the application's display name: the title the app was created
	// with, which SetTitle does not change. It names the application menu.
	appName string

	// Content-area size limits in points, set by SetMinSize and SetMaxSize and
	// applied to the window when it exists. 0 is unset.
	minWidth, minHeight int
	maxWidth, maxHeight int

	// menuBar is the latest menu SetMenuBar was given, which setup installs on
	// every Run. menuInstalled records that this app replaced the default menu
	// bar, so teardown puts the default back (main thread only).
	menuBar       *Menu
	menuInstalled bool

	running bool   // Run is in progress
	closing bool   // the window close sequence has started
	done    bool   // the event loop should exit
	navID   uint64 // NavigationCompleted.ID counter (main thread only)
}

func newPlatformApp(options Options) (platformApp, error) {
	return &darwinApp{options: options, created: time.Now(), appName: applicationName(options.Title)}, nil
}

// activeApp is the app whose Run is in progress. The package-level functions that
// must reach the main thread without an App in hand (ShowMessage) use its
// dispatcher. Only one Run can be in progress: it occupies the main thread.
var activeApp atomic.Pointer[darwinApp]

// platformAvailable reports whether AppKit and WebKit are linked and their
// classes exist. It does not check that Run is called from the main thread.
func platformAvailable() error {
	if !frameworksLinked() {
		return fmt.Errorf("%w: AppKit or WebKit import was not linked", ErrUnsupported)
	}
	for _, name := range []string{"NSApplication", "NSWindow", "WKWebView"} {
		if objcClass(name) == 0 {
			return fmt.Errorf("%w: class %s is not available", ErrUnsupported, name)
		}
	}
	return nil
}

// darwinUnsupported is the error for a platformApp method a later slice
// implements.
func darwinUnsupported(feature string) error {
	return fmt.Errorf("%w: %s is not implemented by the darwin backend yet", ErrUnsupported, feature)
}

// AcquireSingleInstance is not implemented by the darwin backend yet. A bundle
// launched through LaunchServices is already deduplicated by the system; a
// bare-binary lock arrives with the single-instance slice (DT2).
func AcquireSingleInstance(string) (*InstanceLock, bool, error) {
	return nil, false, darwinUnsupported("single-instance locking")
}

// ForwardToFirstInstance is not implemented by the darwin backend yet (DT2).
func ForwardToFirstInstance(string, []string, string) error {
	return darwinUnsupported("forwarding to the first instance")
}

// WebView2RuntimeVersion has no meaning on macOS, where WKWebView ships with the
// operating system.
func WebView2RuntimeVersion(string) (string, error) {
	return "", fmt.Errorf("%w: WebView2 is a Windows component; macOS hosts WKWebView", ErrUnsupported)
}

// snapshotOptions returns a copy of the options taken under mu.
func (a *darwinApp) snapshotOptions() Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.options
}

// markStartup records the first time a startup phase finished.
func (a *darwinApp) markStartup(phase *time.Duration) {
	a.mu.Lock()
	if *phase == 0 {
		*phase = time.Since(a.created)
	}
	a.mu.Unlock()
}

// StartupTimeline implements startupTimelineReporter. EnvironmentReady stays 0:
// WebKit has no separate environment-creation step to time.
func (a *darwinApp) StartupTimeline() StartupTimeline {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.timeline
}

func (a *darwinApp) onNavigationCompleted(event NavigationCompleted) {
	a.markStartup(&a.timeline.FirstNavigationCompleted)
	a.mu.Lock()
	cb := a.options.OnNavigationCompleted
	a.mu.Unlock()
	if cb != nil {
		cb(event)
	}
}

// Run creates the window and web view, pumps the AppKit event loop until the
// window closes, and returns. It must be called on the main thread.
func (a *darwinApp) Run() error {
	if pthreadMainNP() == 0 {
		return errNotMainThread()
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("%w: Run is already in progress", ErrInvalidOptions)
	}
	a.running = true
	a.closing, a.done, a.runErr = false, false, nil
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	if err := platformAvailable(); err != nil {
		return err
	}
	activeApp.Store(a)
	defer activeApp.CompareAndSwap(a, nil)
	// Threads AppKit and WebKit start from here on inherit this mask (see
	// lifecycle_darwin.go), so process-directed signals reach only Go's threads.
	defer blockAsyncSignalsOnThread()()
	ring, err := sharedRing()
	if err != nil {
		return err
	}
	cls, err := delegateClass()
	if err != nil {
		return err
	}
	a.ring = ring
	// Records left by an earlier Run (or by a delegate callback that arrived
	// after it) belong to objects that no longer exist.
	ring.drain(nil)

	var setupErr error
	withPool(func() { setupErr = a.setup(cls, ring) })
	if setupErr != nil {
		a.teardown()
		return setupErr
	}

	a.pump()

	a.teardown()

	// Fire OnClose after the loop exits and the window is torn down, on the main
	// thread.
	a.mu.Lock()
	onClose := a.options.OnClose
	runErr := a.runErr
	a.mu.Unlock()
	if onClose != nil {
		onClose()
	}
	return runErr
}

// appLaunched records that -[NSApplication finishLaunching] has run in this
// process. NSApplication is a process singleton, and a second Run reuses it.
// Only the main thread reads or writes it.
var appLaunched bool

// setup builds the application, the window and the web view and starts the
// first load. It runs on the main thread inside an autorelease pool.
func (a *darwinApp) setup(delegateCls uintptr, ring *nativeRing) error {
	// Navigate and SetHTML may run on other goroutines until the dispatcher
	// starts, and they write a.options under mu: read one consistent snapshot.
	options := a.snapshotOptions()
	app := send0(objcClass("NSApplication"), objcSel("sharedApplication"))
	if app == 0 {
		return fmt.Errorf("%w: NSApplication is unavailable", ErrUnsupported)
	}
	delegate, err := newDelegate(delegateCls, ring)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.nsapp = app
	a.delegate = delegate
	a.mode = ownObjC(objcRetain(nsString("kCFRunLoopDefaultMode")))
	a.mu.Unlock()
	if !ring.wakeArmed() {
		// The recorder wakes the event pump with this event after every record
		// (ctrl_darwin.go). One retained event serves the life of the process. If
		// it cannot be built, the pump's idle timeout still drains the ring.
		if event := objcRetain(newWakeEvent()); event != 0 {
			ring.armWake(app, event)
		}
	}

	// A bare binary gets a Dock tile and a menu bar only with the Regular policy.
	send1(app, objcSel("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
	send1(app, objcSel("setDelegate:"), delegate)
	if !appLaunched {
		// The SDL shape: finish launching without entering -[NSApplication run],
		// then pump events ourselves.
		installApplicationMenu(app, a.appName)
		send0(app, objcSel("finishLaunching"))
		appLaunched = true
	}

	// The window. The content rectangle is in points; the style is a titled,
	// closable, miniaturizable, resizable window.
	rect := nsRect{0, 0, float64(options.Width), float64(options.Height)}
	window := sendRect(send0(objcClass("NSWindow"), objcSel("alloc")),
		objcSel("initWithContentRect:styleMask:backing:defer:"), &rect,
		nsWindowStyleDefault, nsBackingStoreBuffered, 0)
	if window == 0 {
		return fmt.Errorf("desktop: NSWindow initWithContentRect failed")
	}
	a.mu.Lock()
	a.window = window
	a.mu.Unlock()
	// Go owns the window (it came from alloc). -close must not release it.
	send1(window, objcSel("setReleasedWhenClosed:"), 0)
	send1(window, objcSel("setTitle:"), nsString(options.Title))
	send1(window, objcSel("setDelegate:"), delegate)
	background := uintptr(0)
	if color, _, ok, _ := parseBackgroundColor(options.BackgroundColor); ok {
		background = nsColorSRGB(float64(color.R)/255, float64(color.G)/255, float64(color.B)/255, 1)
		send1(window, objcSel("setBackgroundColor:"), background)
	}
	send0(window, objcSel("center"))
	a.markStartup(&a.timeline.WindowCreated)
	a.startDispatch()
	// SetMenuBar, SetMinSize and SetMaxSize may have run before there was a
	// window or an NSApplication to give them to. They record what they were
	// given and apply it only on the main thread, and once the dispatcher is
	// running every later call is queued behind setup, so what they recorded
	// before this point is read here and nothing is lost in between.
	if err := a.installMenuBarOnUI(); err != nil {
		return err
	}
	a.applyWindowLimitsOnUI()
	a.fireWindowCreated(window)

	// The window is shown before the web view exists, as on Windows, so the
	// window color (not a white web view) is the first thing on screen.
	send1(window, objcSel("makeKeyAndOrderFront:"), 0)
	send1(app, objcSel("activateIgnoringOtherApps:"), 1)
	a.markStartup(&a.timeline.WindowShown)

	return a.createWebView(window, delegate, rect, background, devToolsEnabled(options))
}

// createWebView builds the WKWebView, installs the preload scripts and starts
// the first load.
func (a *darwinApp) createWebView(window, delegate uintptr, frame nsRect, background uintptr, devTools bool) error {
	if objcClass("WKWebViewConfiguration") == 0 || objcClass("WKWebView") == 0 {
		return fmt.Errorf("%w: WebKit is unavailable", ErrUnsupported)
	}
	config := ownObjC(objcNew(objcClass("WKWebViewConfiguration")))
	defer config.Release()
	if config == nil {
		return fmt.Errorf("desktop: WKWebViewConfiguration init failed")
	}

	// Preferences. JavaScript is on by default. Dev tools follow
	// Options.Debug / Options.DevTools; developerExtrasEnabled adds "Inspect
	// Element" to the context menu and the Safari Develop menu entry.
	prefs := send0(config.obj, objcSel("preferences"))
	if prefs != 0 {
		send1(prefs, objcSel("setTabFocusesLinks:"), 1)
		if devTools {
			send2(prefs, objcSel("setValue:forKey:"), nsNumberBool(true), nsString("developerExtrasEnabled"))
		}
	}

	// initWithFrame:configuration: copies the configuration, so config can go
	// when this function returns. The view comes from alloc, so Go owns it until
	// teardown releases it; the window's view hierarchy holds its own reference.
	view := sendRect(send0(objcClass("WKWebView"), objcSel("alloc")),
		objcSel("initWithFrame:configuration:"), &frame, config.obj, 0, 0)
	if view == 0 {
		return fmt.Errorf("desktop: WKWebView initWithFrame failed")
	}
	a.mu.Lock()
	a.webview = view
	a.mu.Unlock()
	// AppKit may constrain the window to its screen when it is shown. Use
	// the resulting content bounds so the initial view fits, and subsequent
	// autoresizing does not preserve an oversized requested frame. KVC boxes
	// the NSRect, avoiding a structure-return Objective-C call.
	content := send0(window, objcSel("contentView"))
	bounds := send1(content, objcSel("valueForKey:"), nsString("bounds"))
	send2(view, objcSel("setValue:forKey:"), bounds, nsString("frame"))
	controller := send0(config.obj, objcSel("userContentController"))
	send2(controller, objcSel("addScriptMessageHandler:name:"), delegate, nsString(darwinMessageHandler))

	send1(view, objcSel("setAutoresizingMask:"), nsViewWidthSizable|nsViewHeightSizable)
	send1(view, objcSel("setNavigationDelegate:"), delegate)
	send1(view, objcSel("setUIDelegate:"), delegate)
	if background != 0 && respondsTo(view, "setUnderPageBackgroundColor:") {
		// macOS 12+: the color WebKit paints under the page, including before
		// the first paint, so a dark app does not flash white.
		send1(view, objcSel("setUnderPageBackgroundColor:"), background)
	}
	if devTools && respondsTo(view, "setInspectable:") {
		send1(view, objcSel("setInspectable:"), 1) // macOS 13.3+
	}
	send1(content, objcSel("addSubview:"), view)
	if err := a.applyBootstrapScriptsOnUI(); err != nil {
		return err
	}
	a.markStartup(&a.timeline.ControllerReady)

	a.mu.Lock()
	html, url := a.options.HTML, a.options.URL
	a.mu.Unlock()
	if html != "" {
		return a.loadHTML(view, html)
	}
	return a.loadURL(view, url)
}

func (a *darwinApp) loadHTML(web uintptr, html string) error {
	send2(web, objcSel("loadHTMLString:baseURL:"), nsString(html), 0)
	return nil
}

func (a *darwinApp) loadURL(web uintptr, url string) error {
	u := nsURL(url)
	if u == 0 {
		return fmt.Errorf("%w: %q is not a valid URL", ErrInvalidOptions, url)
	}
	request := send1(objcClass("NSURLRequest"), objcSel("requestWithURL:"), u)
	send1(web, objcSel("loadRequest:"), request)
	return nil
}

// pump is the event loop. Each iteration blocks until AppKit has an event,
// dispatches it, then drains the native event ring and the dispatcher queue.
func (a *darwinApp) pump() {
	app, mode := a.nsapp, a.mode.obj
	for !a.loopDone() {
		withPool(func() {
			// [NSApp nextEventMatchingMask:NSEventMaskAny untilDate:... inMode:
			// NSDefaultRunLoopMode dequeue:YES]. While it waits, AppKit runs the
			// main run loop, which services WebKit's IPC and invokes the delegate
			// methods (the assembly recorder). The recorder and the dispatcher
			// both post an NSEvent to make it return; the idle timeout is a
			// backstop so that nothing can stay undrained for longer than it.
			deadline := [4]float64{pumpIdleTimeout.Seconds()}
			until := sendFloats(objcClass("NSDate"), objcSel("dateWithTimeIntervalSinceNow:"), &deadline)
			event := send4(app, objcSel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, until, mode, 1)
			if event != 0 {
				send1(app, objcSel("sendEvent:"), event)
			}
			a.drainNativeEvents()
			a.drainDispatchQueue()
		})
	}
}

// pumpIdleTimeout bounds one wait in the event pump. Wakes arrive as events, so
// an idle app wakes this often and no more.
const pumpIdleTimeout = time.Second

// loopDone reports whether the event loop should exit.
func (a *darwinApp) loopDone() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.done
}

// drainNativeEvents turns the records the assembly delegate methods appended
// into Go callbacks. It runs on the main thread, right after the event pump
// returns, which is where the records were written.
func (a *darwinApp) drainNativeEvents() {
	a.ring.drain(a.handleNativeEvent)
}

func (a *darwinApp) handleNativeEvent(ev nativeEvent) {
	switch ev.kind {
	case evNone:
		// A zeroed slot: nothing was recorded.
	case evCloseRequested, evQuitRequested:
		// windowShouldClose: answered NO and applicationShouldTerminate:
		// answered NSTerminateCancel; the Go close path decides.
		a.closeOnUI()
	case evNavigationFinished:
		a.navID++
		a.onNavigationCompleted(NavigationCompleted{ID: a.navID, Success: true})
	case evMenuAction:
		// The recorder retained the sender; it lives until this record is
		// released after we return.
		a.runMenuAction(ev.args[0])
	case evOpenFiles, evOpenURLs:
		a.openDocuments(ev.args[1], ev.kind == evOpenURLs)
	case evWebMessage:
		a.receiveWebMessage(ev.args[1])
	case evWebFilePanel, evWebAlert, evWebConfirm, evWebPrompt:
		a.handleWebUI(ev)
	case evNavigationFailed:
		a.navID++
		status := 0
		if nsError := ev.args[2]; nsError != 0 {
			// The recorder retained the NSError; it lives until this record is
			// released after we return.
			status = int(int64(send0(nsError, objcSel("code"))))
		}
		a.onNavigationCompleted(NavigationCompleted{ID: a.navID, Success: false, WebErrorStatus: status})
	}
}

// closeOnUI runs the close sequence on the main thread: OnBeforeClose, then
// -[NSWindow close], then the loop exits. -close does not ask the window
// delegate again.
func (a *darwinApp) closeOnUI() {
	a.mu.Lock()
	window := a.window
	callback := a.options.OnBeforeClose
	if window == 0 || a.closing {
		a.mu.Unlock()
		return
	}
	a.closing = true
	a.mu.Unlock()
	if callback != nil {
		// The window's placement is read by a later slice (DT2); until then the
		// callback gets the zero placement, which Options.InitialPlacement
		// treats as "no saved placement".
		callback(WindowPlacement{})
	}
	send0(window, objcSel("close"))
	a.mu.Lock()
	a.done = true
	a.mu.Unlock()
}

// teardown releases the native objects on the main thread, after the loop.
func (a *darwinApp) teardown() {
	// Run calls already queued while the web view still exists, release it on
	// this thread, and only then let later calls run on their own threads: by
	// then they find no web view and report "not ready" instead of touching it
	// off the main thread.
	a.drainDispatchQueue()
	a.mu.Lock()
	app, window, web, delegate := a.nsapp, a.window, a.webview, a.delegate
	a.mu.Unlock()
	withPool(func() {
		// Detach the delegate everywhere first, so no callback can reach it once
		// it is released.
		if web != 0 {
			controller := send0(send0(web, objcSel("configuration")), objcSel("userContentController"))
			send1(controller, objcSel("removeScriptMessageHandlerForName:"), nsString(darwinMessageHandler))
			send1(web, objcSel("setNavigationDelegate:"), 0)
			send1(web, objcSel("setUIDelegate:"), 0)
			// Resolve queued UI requests while WebKit and its view still exist.
			if a.ring != nil {
				a.ring.drain(nil)
			}
			send0(web, objcSel("stopLoading"))
			send0(web, objcSel("removeFromSuperview"))
		}
		if window != 0 {
			send1(window, objcSel("setDelegate:"), 0)
			send0(window, objcSel("close"))
		}
		if app != 0 {
			send1(app, objcSel("setDelegate:"), 0)
			a.restoreApplicationMenu(app)
		}
	})
	// A call that arrives from here on, on any thread, must find no window and no
	// web view (and report "not ready") rather than a pointer that is about to be
	// released: the dispatcher runs calls queued during the teardown when it
	// stops, before the handles are cleared below.
	a.mu.Lock()
	a.window, a.webview = 0, 0
	a.mu.Unlock()
	objcRelease(web)
	objcRelease(window)
	objcRelease(delegate)
	a.settle(app)
	a.mode.Release()
	a.stopDispatch()
	a.mu.Lock()
	a.window, a.webview, a.delegate, a.nsapp = 0, 0, 0, 0
	a.primaryWindow = nil
	a.mu.Unlock()
	if a.ring != nil {
		a.ring.drain(nil)
	}
}

// settle runs the event pump briefly without blocking, after the window and web
// view are released. Tearing them down schedules deferred work (the window is
// deallocated only once WebKit and AppKit have dropped their last references, on
// later run-loop passes), and Run should not return with that work pending: the
// next Run in the same process, or the process exit, would otherwise meet
// half-torn-down windows. It stops after settleIdleTurns consecutive passes that
// found nothing to do, or after settleBudget.
func (a *darwinApp) settle(app uintptr) {
	if app == 0 || a.mode == nil {
		return
	}
	deadline := time.Now().Add(settleBudget)
	for idle := 0; idle < settleIdleTurns && time.Now().Before(deadline); {
		busy := false
		withPool(func() {
			wait := [4]float64{settleTurn.Seconds()}
			until := sendFloats(objcClass("NSDate"), objcSel("dateWithTimeIntervalSinceNow:"), &wait)
			if event := send4(app, objcSel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, until, a.mode.obj, 1); event != 0 {
				send1(app, objcSel("sendEvent:"), event)
				busy = true
			}
		})
		if busy {
			idle = 0
		} else {
			idle++
		}
	}
}

const (
	settleTurn      = 5 * time.Millisecond // one wait for an event
	settleIdleTurns = 4                    // quiet waits in a row that end the settle
	settleBudget    = 250 * time.Millisecond
)

// Close requests that the window close. It may be called from any goroutine; the
// close runs on the main thread. It does nothing before Run or after the window
// is gone.
func (a *darwinApp) Close() error {
	return a.onUIThread(func() error {
		a.closeOnUI()
		return nil
	})
}

// failRun records the first error and closes the window; Run returns the error
// after OnClose.
func (a *darwinApp) failRun(err error) {
	a.mu.Lock()
	if a.runErr == nil {
		a.runErr = err
	}
	a.mu.Unlock()
	_ = a.onUIThread(func() error {
		a.closeOnUI()
		return nil
	})
}

func (a *darwinApp) webviewHandle() (uintptr, error) {
	a.mu.Lock()
	web := a.webview
	a.mu.Unlock()
	if web == 0 {
		return 0, fmt.Errorf("%w: webview not ready", ErrWindowNotReady)
	}
	return web, nil
}

func (a *darwinApp) navigateOnUI(url string) error {
	options := a.snapshotOptions()
	normalized, err := normalizeOptions(Options{Title: options.Title, Width: options.Width, Height: options.Height, URL: url})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.options.URL = normalized.URL
	a.options.HTML = ""
	web := a.webview
	a.mu.Unlock()
	if web == 0 {
		return nil
	}
	var loadErr error
	withPool(func() { loadErr = a.loadURL(web, normalized.URL) })
	return loadErr
}

func (a *darwinApp) setHTMLOnUI(html string) error {
	options := a.snapshotOptions()
	if _, err := normalizeOptions(Options{Title: options.Title, Width: options.Width, Height: options.Height, HTML: html}); err != nil {
		return err
	}
	a.mu.Lock()
	a.options.HTML = html
	web := a.webview
	a.mu.Unlock()
	if web == 0 {
		return nil
	}
	withPool(func() { _ = a.loadHTML(web, html) })
	return nil
}

func (a *darwinApp) reloadOnUI() error {
	web, err := a.webviewHandle()
	if err != nil {
		return err
	}
	withPool(func() { send0(web, objcSel("reload")) })
	return nil
}

// executeScriptOnUI runs JavaScript in the top frame. The completion handler is
// nil, so, as on Windows, the result is discarded; callers that need a value
// round-trip through the page.
func (a *darwinApp) executeScriptOnUI(script string) error {
	web, err := a.webviewHandle()
	if err != nil {
		return err
	}
	withPool(func() {
		send2(web, objcSel("evaluateJavaScript:completionHandler:"), nsString(script), 0)
	})
	return nil
}

// prependBootstrapScriptOnUI records the preload script and installs it on the
// live web view. WKUserContentController has no per-script handle, so the whole
// set is replaced.
func (a *darwinApp) prependBootstrapScriptOnUI(script string) error {
	a.mu.Lock()
	previous := a.pendingBootstrap
	a.pendingBootstrap = script
	web := a.webview
	a.mu.Unlock()
	if web == 0 {
		return nil
	}
	if err := a.applyBootstrapScriptsOnUI(); err != nil {
		a.mu.Lock()
		a.pendingBootstrap = previous
		a.mu.Unlock()
		_ = a.applyBootstrapScriptsOnUI()
		return err
	}
	return nil
}

// applyBootstrapScriptsOnUI replaces the web view's user scripts with the
// bootstrap script, to run at document start in every frame (the behavior of
// AddScriptToExecuteOnDocumentCreated).
func (a *darwinApp) applyBootstrapScriptsOnUI() error {
	web, err := a.webviewHandle()
	if err != nil {
		return err
	}
	a.mu.Lock()
	script := darwinTransportBootstrap + "\n" + a.pendingBootstrap
	a.mu.Unlock()
	var applyErr error
	withPool(func() {
		controller := send0(send0(web, objcSel("configuration")), objcSel("userContentController"))
		if controller == 0 {
			applyErr = fmt.Errorf("desktop: WKWebView has no user content controller")
			return
		}
		send0(controller, objcSel("removeAllUserScripts"))
		if script == "" {
			return
		}
		userScript := ownObjC(send3(send0(objcClass("WKUserScript"), objcSel("alloc")),
			objcSel("initWithSource:injectionTime:forMainFrameOnly:"),
			nsString(script), wkUserScriptInjectionTimeAtDocumentStart, boolArg(false)))
		defer userScript.Release()
		if userScript == nil {
			applyErr = fmt.Errorf("desktop: WKUserScript initWithSource failed")
			return
		}
		send1(controller, objcSel("addUserScript:"), userScript.obj)
	})
	return applyErr
}

func (a *darwinApp) setTitleOnUI(title string) error {
	a.mu.Lock()
	a.options.Title = title
	window := a.window
	a.mu.Unlock()
	if window == 0 {
		return nil
	}
	withPool(func() { send1(window, objcSel("setTitle:"), nsString(title)) })
	return nil
}

// SetMinSize sets the smallest content size, in points, that resizing the window
// can reach. Both dimensions must be positive. Before Run it is recorded and
// applied when the window is created; it can be called from any goroutine.
func (a *darwinApp) SetMinSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%w: minimum window size %dx%d must be positive", ErrInvalidOptions, width, height)
	}
	a.mu.Lock()
	a.minWidth, a.minHeight = width, height
	a.mu.Unlock()
	return a.onUIThread(func() error { a.applyWindowLimitsOnUI(); return nil })
}

// SetMaxSize sets the largest content size, in points, that resizing the window
// can reach. A zero dimension leaves that axis unbounded, so 0, 0 removes the
// limit; a negative one is an error. Before Run it is recorded and applied when
// the window is created; it can be called from any goroutine.
func (a *darwinApp) SetMaxSize(width, height int) error {
	if width < 0 || height < 0 {
		return fmt.Errorf("%w: maximum window size %dx%d must not be negative", ErrInvalidOptions, width, height)
	}
	a.mu.Lock()
	a.maxWidth, a.maxHeight = width, height
	a.mu.Unlock()
	return a.onUIThread(func() error { a.applyWindowLimitsOnUI(); return nil })
}

// unboundedContentSize is the NSWindow default for an unlimited maximum
// dimension: FLT_MAX.
const unboundedContentSize = math.MaxFloat32

// applyWindowLimitsOnUI gives the window the size limits recorded by SetMinSize
// and SetMaxSize. With no window yet, or off the main thread, there is nothing
// to do: setup applies the limits once it has made the window and started the
// dispatcher.
func (a *darwinApp) applyWindowLimitsOnUI() {
	a.mu.Lock()
	window := a.window
	minW, minH, maxW, maxH := a.minWidth, a.minHeight, a.maxWidth, a.maxHeight
	a.mu.Unlock()
	if window == 0 || pthreadMainNP() == 0 {
		return
	}
	bound := func(v int) float64 {
		if v == 0 {
			return unboundedContentSize
		}
		return float64(v)
	}
	withPool(func() {
		sendSize(window, objcSel("setContentMinSize:"), float64(minW), float64(minH))
		sendSize(window, objcSel("setContentMaxSize:"), bound(maxW), bound(maxH))
	})
}

// fireWindowCreated builds the primary Window and calls OnWindowCreated. The
// handle is the NSWindow pointer.
func (a *darwinApp) fireWindowCreated(window uintptr) {
	a.mu.Lock()
	cb := a.options.OnWindowCreated
	options := a.options
	primary := a.primaryWindow
	if primary == nil {
		primary = newPrimaryWindow(window, options, func(Menu) error {
			return darwinUnsupported("window context menus")
		})
		a.primaryWindow = primary
	}
	a.mu.Unlock()
	if cb != nil {
		cb(primary)
	}
}

func (a *darwinApp) PrimaryWindow() *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.primaryWindow
}

// Navigate runs on the main thread; AppKit and WebKit reject calls from others.
func (a *darwinApp) Navigate(url string) error {
	return a.onUIThread(func() error { return a.navigateOnUI(url) })
}

// SetHTML runs on the main thread.
func (a *darwinApp) SetHTML(html string) error {
	return a.onUIThread(func() error { return a.setHTMLOnUI(html) })
}

// Reload runs on the main thread.
func (a *darwinApp) Reload() error {
	return a.onUIThread(a.reloadOnUI)
}

// ExecuteScript runs on the main thread.
func (a *darwinApp) ExecuteScript(script string) error {
	return a.onUIThread(func() error { return a.executeScriptOnUI(script) })
}

// PrependBootstrapScript runs on the main thread.
func (a *darwinApp) PrependBootstrapScript(script string) error {
	return a.onUIThread(func() error { return a.prependBootstrapScriptOnUI(script) })
}

// SetTitle runs on the main thread.
func (a *darwinApp) SetTitle(title string) error {
	return a.onUIThread(func() error { return a.setTitleOnUI(title) })
}

// The rest of the platformApp surface belongs to later slices (see the
// initiative, section 6). Each reports ErrUnsupported with the feature named.

func (a *darwinApp) OpenDevTools() error {
	// WebKit has no public API to open the inspector; the user opens it from the
	// context menu when Options.DevTools is set.
	return darwinUnsupported("OpenDevTools")
}
func (a *darwinApp) Minimize() error { return darwinUnsupported("Minimize") }
func (a *darwinApp) Maximize() error { return darwinUnsupported("Maximize") }
func (a *darwinApp) Restore() error  { return darwinUnsupported("Restore") }
func (a *darwinApp) Focus() error    { return darwinUnsupported("Focus") }
func (a *darwinApp) Serve(string, http.Handler) error {
	return darwinUnsupported("Serve")
}
func (a *darwinApp) Clipboard() (string, error) { return "", darwinUnsupported("Clipboard") }
func (a *darwinApp) SetClipboard(string) error  { return darwinUnsupported("SetClipboard") }
func (a *darwinApp) SetFullscreen(bool) error   { return darwinUnsupported("SetFullscreen") }
func (a *darwinApp) WindowPlacement() (WindowPlacement, error) {
	return WindowPlacement{}, darwinUnsupported("WindowPlacement")
}
func (a *darwinApp) NewWindow(WindowOptions) (*Window, error) {
	return nil, darwinUnsupported("NewWindow")
}
func (a *darwinApp) RegisterProtocol(string) error { return darwinUnsupported("RegisterProtocol") }
func (a *darwinApp) RegisterFileType(string, string, string) error {
	return darwinUnsupported("RegisterFileType")
}
func (a *darwinApp) SetTray(TrayOptions) error { return darwinUnsupported("SetTray") }
func (a *darwinApp) CloseTray() error          { return darwinUnsupported("CloseTray") }
func (a *darwinApp) Notify(Notification) error { return darwinUnsupported("Notify") }
func (a *darwinApp) SetFileDropHandler(func([]string)) error {
	return darwinUnsupported("SetFileDropHandler")
}
