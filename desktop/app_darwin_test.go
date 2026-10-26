//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// The darwin counterpart of app_unsupported_test.go. The platformApp methods
// the implemented slices leave to later ones must say so, not report success.
// SetMinSize, SetMaxSize, OpenURL, SetMenuBar and ShowMessage are implemented
// (DT1) and have tests of their own.
func TestDarwinAppUnsupportedSurface(t *testing.T) {
	impl, err := newPlatformApp(Options{})
	if err != nil {
		t.Fatal(err)
	}

	_, clipErr := impl.Clipboard()
	_, placementErr := impl.WindowPlacement()
	_, windowErr := impl.NewWindow(WindowOptions{})

	for _, c := range []struct {
		name string
		err  error
	}{
		{"OpenDevTools", impl.OpenDevTools()},
		{"Minimize", impl.Minimize()},
		{"Maximize", impl.Maximize()},
		{"Restore", impl.Restore()},
		{"Focus", impl.Focus()},
		{"Serve", impl.Serve("app://assets/*", http.NotFoundHandler())},
		{"Clipboard", clipErr},
		{"SetClipboard", impl.SetClipboard("ok")},
		{"SetFullscreen", impl.SetFullscreen(true)},
		{"WindowPlacement", placementErr},
		{"NewWindow", windowErr},
		{"RegisterProtocol", impl.RegisterProtocol("gosx-test")},
		{"RegisterFileType", impl.RegisterFileType(".gsx", "", "GoSX Component")},
		{"SetTray", impl.SetTray(TrayOptions{})},
		{"CloseTray", impl.CloseTray()},
		{"Notify", impl.Notify(Notification{Title: "hello"})},
		{"SetFileDropHandler", impl.SetFileDropHandler(func([]string) {})},
		{"AcquireSingleInstance", func() error { _, _, err := AcquireSingleInstance("com.example.test"); return err }()},
		{"ForwardToFirstInstance", ForwardToFirstInstance("com.example.test", nil, "")},
		{"WebView2RuntimeVersion", func() error { _, err := WebView2RuntimeVersion(""); return err }()},
		{"GPUAdapters", func() error { _, err := GPUAdapters(); return err }()},
	} {
		if !errors.Is(c.err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", c.name, c.err)
		}
	}
	var lock *InstanceLock
	if err := lock.Close(); err != nil {
		t.Errorf("Close() on a nil InstanceLock = %v, want nil", err)
	}
}

// Before Run there is no window and no web view: the calls that need one say so,
// and the ones that only record state (a later Run applies it) succeed.
func TestDarwinAppBeforeRun(t *testing.T) {
	impl, err := newPlatformApp(Options{Title: "t", Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := impl.Reload(); !errors.Is(err, ErrWindowNotReady) {
		t.Errorf("Reload before Run = %v, want ErrWindowNotReady", err)
	}
	if err := impl.ExecuteScript("1"); !errors.Is(err, ErrWindowNotReady) {
		t.Errorf("ExecuteScript before Run = %v, want ErrWindowNotReady", err)
	}
	if err := impl.PostMessage("noop"); !errors.Is(err, ErrWindowNotReady) {
		t.Errorf("PostMessage before Run = %v, want ErrWindowNotReady", err)
	}
	for name, err := range map[string]error{
		"Close":                  impl.Close(),
		"Navigate":               impl.Navigate("http://127.0.0.1:1/"),
		"SetHTML":                impl.SetHTML("<p>x</p>"),
		"SetTitle":               impl.SetTitle("renamed"),
		"PrependBootstrapScript": impl.PrependBootstrapScript("window.x = 1"),
	} {
		if err != nil {
			t.Errorf("%s before Run = %v, want nil", name, err)
		}
	}
	if impl.PrimaryWindow() != nil {
		t.Error("PrimaryWindow before Run is not nil")
	}
	da := impl.(*darwinApp)
	if opts := da.options; opts.Title != "renamed" || opts.HTML != "<p>x</p>" || opts.URL != "http://127.0.0.1:1/" {
		t.Errorf("recorded options = title %q, html %q, url %q", opts.Title, opts.HTML, opts.URL)
	}
	if da.pendingBootstrap != "window.x = 1" {
		t.Errorf("pendingBootstrap = %q", da.pendingBootstrap)
	}
	if tl := da.StartupTimeline(); tl != (StartupTimeline{}) {
		t.Errorf("StartupTimeline before Run = %+v, want zero", tl)
	}
}

func TestDarwinRunRequiresTheMainThread(t *testing.T) {
	impl, err := newPlatformApp(Options{Title: "t", Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	// A test goroutine is not on the main thread (TestMain keeps that one).
	err = impl.Run()
	if err == nil {
		t.Fatal("Run on a non-main thread succeeded")
	}
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "main thread") {
		t.Errorf("Run error = %v, want a clear main-thread error that matches ErrUnsupported", err)
	}
	// The failed attempt must not leave the app looking busy.
	if err := impl.Run(); err == nil || !strings.Contains(err.Error(), "main thread") {
		t.Errorf("second Run error = %v", err)
	}
}

func TestDarwinFailRunKeepsTheFirstError(t *testing.T) {
	impl, _ := newPlatformApp(Options{})
	da := impl.(*darwinApp)
	first, second := errors.New("first"), errors.New("second")
	da.failRun(first)
	da.failRun(second)
	if da.runErr != first {
		t.Errorf("runErr = %v, want the first error", da.runErr)
	}
}

// closedLoopbackURL returns a URL on a loopback port nothing listens on.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return "http://" + addr + "/"
}

// frameOf reads obj.frame by key-value coding. +[NSValue valueWithRect:] has
// the argument shim; reading a rect back by value needs a return shim (DT2), so
// the tests read the NSValue's bytes instead.
func frameOf(obj uintptr) nsRect {
	var r nsRect
	value := send1(obj, objcSel("valueForKey:"), nsString("frame"))
	getValue(value, unsafe.Pointer(&r), unsafe.Sizeof(r))
	return r
}

func componentsOf(color uintptr) [4]float64 {
	var c [4]float64
	syscall_syscall6(fnobjc_msgSend, color, objcSel("getRed:green:blue:alpha:"),
		uintptr(unsafe.Pointer(&c[0])), uintptr(unsafe.Pointer(&c[1])),
		uintptr(unsafe.Pointer(&c[2])), uintptr(unsafe.Pointer(&c[3])))
	return c
}

// windowReport is what a live window looks like from AppKit, captured on the
// main thread.
type windowReport struct {
	content, web, frame nsRect
	styleMask, backing  uintptr
	title               string
	visible             bool
	background          [4]float64
	appWindows          uintptr
	activation          uintptr
	scale               float64 // backingScaleFactor
}

func reportWindow(da *darwinApp) windowReport {
	da.mu.Lock()
	window, web, app := da.window, da.webview, da.nsapp
	da.mu.Unlock()
	// backingScaleFactor is a CGFloat return, which needs a return shim (DT2);
	// key-value coding boxes it in an NSNumber whose bytes getValue:size: reads.
	var scale float64
	getValue(send1(window, objcSel("valueForKey:"), nsString("backingScaleFactor")), unsafe.Pointer(&scale), unsafe.Sizeof(scale))
	return windowReport{
		scale:      scale,
		content:    frameOf(send0(window, objcSel("contentView"))),
		web:        frameOf(web),
		frame:      frameOf(window),
		styleMask:  send0(window, objcSel("styleMask")),
		backing:    send0(window, objcSel("backingType")),
		title:      goString(send0(window, objcSel("title"))),
		visible:    sendBool0(window, objcSel("isVisible")),
		background: componentsOf(send0(window, objcSel("backgroundColor"))),
		appWindows: send0(send0(app, objcSel("windows")), objcSel("count")),
		activation: send0(app, objcSel("activationPolicy")),
	}
}

// One window, the whole slice: geometry through the NSRect shim, the startup
// timeline and callbacks, the web view operations, calls from many goroutines
// through the dispatcher, a refused connection reaching OnNavigationCompleted
// with the NSError's code, and a clean Close from a goroutine.
func TestDarwinWindowAndWebViewLifecycle(t *testing.T) {
	requireGUI(t)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			mu.Lock()
			hits[r.URL.RawQuery]++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>page</title><body style="background:#10151c;color:#dde6f0">hello</body>`)
	}))
	defer server.Close()
	pinged := func(key string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[key]
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	type navigation struct {
		event  NavigationCompleted
		thread int
	}
	navs := make(chan navigation, 32)
	var report windowReport
	var reported atomic.Bool
	var created []*Window
	var createdThread, closed int32
	var beforeClose []WindowPlacement
	var da *darwinApp

	app, err := New(Options{
		Title:           "GoSX darwin test",
		Width:           640,
		Height:          480,
		AppID:           "dev.gosx.test.darwin",
		URL:             server.URL,
		BackgroundColor: "#10151c",
		OnWindowCreated: func(w *Window) {
			created = append(created, w)
			atomic.StoreInt32(&createdThread, int32(currentThreadID()))
		},
		OnNavigationCompleted: func(e NavigationCompleted) {
			if reported.CompareAndSwap(false, true) && e.Success {
				report = reportWindow(da)
			}
			navs <- navigation{e, int(currentThreadID())}
		},
		OnBeforeClose: func(p WindowPlacement) { beforeClose = append(beforeClose, p) },
		OnClose:       func() { atomic.AddInt32(&closed, 1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	da = app.impl.(*darwinApp)
	if err := app.PrependBootstrapScript("fetch('/ping?bootstrap=1')"); err != nil {
		t.Fatalf("PrependBootstrapScript before Run: %v", err)
	}
	if app.Window() != nil {
		t.Error("Window() before Run is not nil")
	}

	nextNav := func() navigation {
		t.Helper()
		select {
		case n := <-navs:
			return n
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for OnNavigationCompleted")
			return navigation{}
		}
	}

	driven := make(chan struct{})
	go func() {
		defer close(driven)
		defer app.Close()

		first := nextNav()
		if !first.event.Success || first.event.ID != 1 {
			t.Errorf("first navigation = %+v, want ID 1 success", first.event)
		}
		if first.thread != 1 {
			t.Errorf("OnNavigationCompleted ran on thread %d, want the main thread (1)", first.thread)
		}
		waitFor("the bootstrap script to run on the first load", func() bool { return pinged("bootstrap=1") == 1 })

		// ExecuteScript with a nil completion handler: the effect is a request.
		if err := app.ExecuteScript("fetch('/ping?exec=1')"); err != nil {
			t.Errorf("ExecuteScript: %v", err)
		}
		waitFor("ExecuteScript's request", func() bool { return pinged("exec=1") == 1 })

		// Many goroutines at once, through the dispatcher.
		var wg sync.WaitGroup
		var failures atomic.Int32
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					if err := app.SetTitle(fmt.Sprintf("GoSX darwin test %d.%d", g, i)); err != nil {
						failures.Add(1)
					}
				}
			}(g)
		}
		wg.Wait()
		if n := failures.Load(); n != 0 {
			t.Errorf("%d concurrent SetTitle calls failed", n)
		}
		if err := app.SetTitle("Renamed"); err != nil {
			t.Errorf("SetTitle: %v", err)
		}
		var title string
		da.onUIThread(func() error {
			title = goString(send0(da.window, objcSel("title")))
			return nil
		})
		if title != "Renamed" {
			t.Errorf("window title = %q, want Renamed", title)
		}

		// Reload runs the preload script again: it is a new document.
		if err := app.Reload(); err != nil {
			t.Errorf("Reload: %v", err)
		}
		if n := nextNav(); !n.event.Success || n.event.ID != 2 {
			t.Errorf("reload navigation = %+v", n.event)
		}
		waitFor("the bootstrap script to run again after Reload", func() bool { return pinged("bootstrap=1") == 2 })

		// App.PrependBootstrapScript passes the whole accumulated set each time, so
		// the backend must replace what it installed, not add to it: the next
		// document runs the first script once (not twice) and the new one once.
		if err := app.PrependBootstrapScript("fetch('/ping?second=1')"); err != nil {
			t.Errorf("PrependBootstrapScript after Run: %v", err)
		}
		if err := app.Navigate(server.URL + "/next"); err != nil {
			t.Errorf("Navigate: %v", err)
		}
		if n := nextNav(); !n.event.Success || n.event.ID != 3 {
			t.Errorf("Navigate navigation = %+v", n.event)
		}
		waitFor("the replaced preload set to run", func() bool { return pinged("second=1") >= 1 })
		if got := pinged("bootstrap=1"); got != 3 {
			t.Errorf("bootstrap script ran %d times over three loads, want 3: the set was appended to, not replaced", got)
		}
		if got := pinged("second=1"); got != 1 {
			t.Errorf("second script ran %d times, want 1", got)
		}

		if err := app.SetHTML("<!doctype html><body>from SetHTML</body>"); err != nil {
			t.Errorf("SetHTML: %v", err)
		}
		if n := nextNav(); !n.event.Success || n.event.ID != 4 {
			t.Errorf("SetHTML navigation = %+v", n.event)
		}

		// A refused connection reaches the callback with the NSError's code. The
		// port is one the OS just released: WebKit refuses restricted ports such
		// as 1 without a connection and reports an empty page instead.
		refused := closedLoopbackURL(t)
		if err := app.Navigate(refused); err != nil {
			t.Errorf("Navigate: %v", err)
		}
		bad := nextNav()
		if bad.event.Success || bad.event.WebErrorStatus != -1004 || bad.event.ID != 5 { // NSURLErrorCannotConnectToHost
			t.Errorf("refused navigation = %+v, want failure with code -1004", bad.event)
		}
	}()

	var runErr error
	onMain(func() { runErr = app.Run() })
	<-driven
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}

	if got := atomic.LoadInt32(&closed); got != 1 {
		t.Errorf("OnClose ran %d times, want 1", got)
	}
	if len(beforeClose) != 1 || !beforeClose[0].IsZero() {
		t.Errorf("OnBeforeClose calls = %+v, want one with the zero placement", beforeClose)
	}
	if len(created) != 1 || created[0] == nil || !created[0].Primary() || created[0].Handle() == 0 {
		t.Errorf("OnWindowCreated windows = %+v", created)
	}
	if atomic.LoadInt32(&createdThread) != 1 {
		t.Error("OnWindowCreated did not run on the main thread")
	}
	if app.Window() != nil {
		t.Error("Window() after Run is not nil")
	}

	// Geometry. The content rectangle goes through the NSRect shim twice (the
	// window and the web view); on amd64 it travels on the stack.
	if report.content.w != 640 || report.content.h != 480 {
		t.Errorf("content view = %+v, want 640x480", report.content)
	}
	if report.web != (nsRect{0, 0, 640, 480}) {
		t.Errorf("web view frame = %+v, want {0 0 640 480}", report.web)
	}
	if report.frame.w != 640 || report.frame.h < 480 {
		t.Errorf("window frame = %+v, want 640 wide and at least 480 high", report.frame)
	}
	if report.styleMask&nsWindowStyleDefault != nsWindowStyleDefault {
		t.Errorf("styleMask = %#x, want titled|closable|miniaturizable|resizable", report.styleMask)
	}
	// AppKit accepts only buffered backing and reports 2, or 3 once the window
	// is composited by Metal.
	if report.backing != nsBackingStoreBuffered && report.backing != 3 {
		t.Errorf("backingType = %d, want buffered (2, or 3 when Metal-backed)", report.backing)
	}
	if report.title != "GoSX darwin test" {
		t.Errorf("title = %q", report.title)
	}
	if !report.visible && !screenLocked() {
		t.Error("the window was not visible after the first navigation")
	}
	if report.activation != nsApplicationActivationPolicyRegular {
		t.Errorf("activation policy = %d, want regular", report.activation)
	}
	wantBackground := [4]float64{0x10 / 255.0, 0x15 / 255.0, 0x1c / 255.0, 1}
	for i := range wantBackground {
		if math.Abs(report.background[i]-wantBackground[i]) > 1e-6 {
			t.Errorf("background = %v, want %v: BackgroundColor did not reach the window", report.background, wantBackground)
			break
		}
	}

	// Timeline: EnvironmentReady has no WebKit counterpart and stays zero.
	tl := app.StartupTimeline()
	if tl.EnvironmentReady != 0 {
		t.Errorf("EnvironmentReady = %v, want 0 on macOS", tl.EnvironmentReady)
	}
	if !(0 < tl.WindowCreated && tl.WindowCreated <= tl.WindowShown && tl.WindowShown <= tl.ControllerReady && tl.ControllerReady <= tl.FirstNavigationCompleted) {
		t.Errorf("timeline = %+v, want 0 < created <= shown <= controller <= first navigation", tl)
	}
	t.Logf("timeline: %+v", tl)
	// R9 of the design: a bare binary has no Info.plist, so record whether its
	// window is rendered at the display's scale or at 1x.
	if report.scale < 1 {
		t.Errorf("backingScaleFactor = %v", report.scale)
	}
	t.Logf("backingScaleFactor = %v (a bare binary; 2 on a Retina display means no Info.plist is needed for HiDPI)", report.scale)
}

// plainWindowRetention reports the fraction of plain NSWindows (no web view, no
// GoSX code) that are still listed by -[NSApp windows] after being ordered front,
// closed and released in this session: 0 when AppKit frees them, 1 when it keeps
// them, as it does while the screen is locked.
func plainWindowRetention(t *testing.T) float64 {
	t.Helper()
	const made = 4
	var kept uintptr
	onMain(func() {
		withPool(func() {
			app := send0(objcClass("NSApplication"), objcSel("sharedApplication"))
			count := func() uintptr { return send0(send0(app, objcSel("windows")), objcSel("count")) }
			before := count()
			for i := 0; i < made; i++ {
				withPool(func() {
					rect := nsRect{0, 0, 200, 100}
					w := sendRect(send0(objcClass("NSWindow"), objcSel("alloc")),
						objcSel("initWithContentRect:styleMask:backing:defer:"), &rect, nsWindowStyleDefault, nsBackingStoreBuffered, 0)
					send1(w, objcSel("setReleasedWhenClosed:"), 0)
					send1(w, objcSel("makeKeyAndOrderFront:"), 0)
					send0(w, objcSel("close"))
					objcRelease(w)
				})
			}
			if after := count(); after > before {
				kept = after - before
			}
		})
	})
	return float64(kept) / made
}

// maxLingeringWindows bounds the closed windows AppKit may still list after Run
// returns (measured: 1 to 3, flat over 30 cycles).
const maxLingeringWindows = 4

// Open and close the window repeatedly in one process, the way the lifecycle
// gate asks: every cycle must end Run, call OnClose once, and leave nothing
// behind: the native event ring stays empty and unflooded, every object the
// recorder retained is released, and no window outlives its Run.
// GOSX_DESKTOP_CYCLES sets the count (30 for the gate; 3 by default).
func TestDarwinRunLifecycleCycles(t *testing.T) {
	requireGUI(t)
	cycles := 3
	if v := os.Getenv("GOSX_DESKTOP_CYCLES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("GOSX_DESKTOP_CYCLES=%q", v)
		}
		cycles = n
	}
	ring, err := sharedRing()
	if err != nil {
		t.Fatal(err)
	}
	calls0, dropped0, retains0, releases0 := ring.stats()

	var windows []uintptr
	windowCount := func() (n uintptr) {
		onMain(func() {
			withPool(func() {
				n = send0(send0(send0(objcClass("NSApplication"), objcSel("sharedApplication")), objcSel("windows")), objcSel("count"))
			})
		})
		return n
	}
	for i := 0; i < cycles; i++ {
		var navigations, failures, closed atomic.Int32
		firstNav := make(chan struct{}, 1)
		app, err := New(Options{
			Title:  fmt.Sprintf("cycle %d", i),
			Width:  320,
			Height: 200,
			HTML:   `<!doctype html><body style="background:#10151c">cycle</body>`,
			OnNavigationCompleted: func(e NavigationCompleted) {
				navigations.Add(1)
				if !e.Success {
					failures.Add(1)
				}
				select {
				case firstNav <- struct{}{}:
				default:
				}
			},
			OnClose: func() { closed.Add(1) },
		})
		if err != nil {
			t.Fatal(err)
		}
		da := app.impl.(*darwinApp)
		refused := closedLoopbackURL(t)
		go func() {
			defer app.Close()
			select {
			case <-firstNav:
			case <-time.After(10 * time.Second):
				return
			}
			// A failed load makes the recorder retain an NSError that Go must
			// release: this is what the retains == releases check below counts.
			app.Navigate(refused)
			for deadline := time.Now().Add(10 * time.Second); failures.Load() == 0 && time.Now().Before(deadline); {
				time.Sleep(5 * time.Millisecond)
			}
		}()
		var runErr error
		started := time.Now()
		onMain(func() { runErr = app.Run() })
		if runErr != nil {
			t.Fatalf("cycle %d: Run: %v", i, runErr)
		}
		if got := closed.Load(); got != 1 {
			t.Fatalf("cycle %d: OnClose ran %d times, want 1", i, got)
		}
		if got := navigations.Load(); got < 2 || failures.Load() != 1 {
			t.Fatalf("cycle %d: %d navigations, %d failures; want a success and one refused connection", i, got, failures.Load())
		}
		if da.StartupTimeline().FirstNavigationCompleted == 0 {
			t.Fatalf("cycle %d: FirstNavigationCompleted is zero", i)
		}
		if app.Window() != nil {
			t.Fatalf("cycle %d: Window() is not nil after Run", i)
		}
		if pending := ring.pending(); pending != 0 {
			t.Fatalf("cycle %d: %d native events left in the ring", i, pending)
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("cycle %d took %v", i, elapsed)
		}
		windows = append(windows, windowCount())
	}
	// AppKit deallocates a closed window on a later run-loop pass, so the last one
	// or two can still be listed when Run returns. A leak would grow with the
	// cycle count. How much AppKit itself keeps depends on the session (while the
	// screen is locked it keeps every window it ordered front), so the allowance
	// is what plain windows do in this session, measured here, plus a constant.
	perWindow := plainWindowRetention(t)
	for i, got := range windows {
		if allowed := windows[0] + uintptr(float64(i)*perWindow+0.5) + maxLingeringWindows; got > allowed {
			t.Errorf("cycle %d: %d windows alive, want at most %d (first cycle %d, plain windows keep %.0f%% in this session): a window outlived its Run",
				i, got, allowed, windows[0], perWindow*100)
			break
		}
	}
	t.Logf("windows alive after each cycle: %v (plain windows are kept %.0f%% of the time in this session)", windows, perWindow*100)
	calls, dropped, retains, releases := ring.stats()
	if dropped != dropped0 {
		t.Errorf("native event ring dropped %d records over %d cycles", dropped-dropped0, cycles)
	}
	if calls-calls0 < uint64(cycles) {
		t.Errorf("the recorder ran %d times over %d cycles: the delegate is not wired", calls-calls0, cycles)
	}
	if retains-retains0 != releases-releases0 {
		t.Errorf("recorder retained %d objects and Go released %d", retains-retains0, releases-releases0)
	}
	if retains-retains0 < uint64(cycles) {
		t.Errorf("recorder retained %d objects over %d cycles, want at least one NSError per cycle", retains-retains0, cycles)
	}
	t.Logf("%d cycles: %d records, %d dropped, %d retained, %d released", cycles, calls-calls0, dropped-dropped0, retains-retains0, releases-releases0)
}

// Process-directed signals (a child's SIGCHLD, a SIGTERM from a terminal) may be
// delivered to any thread that does not block them, and AppKit and WebKit start
// threads the Go runtime has never seen. A CGO_ENABLED=0 runtime that receives
// one on such a thread prints "fatal: bad g in signal handler" and exits. The
// kernel should pick the main thread (the first thread created); this test
// sends a storm, including real child exits, while the window is up, then a
// SIGTERM that the host turns into a clean Close. A failure here kills the test
// binary, loudly.
func TestDarwinSurvivesProcessDirectedSignals(t *testing.T) {
	requireGUI(t)
	loaded := make(chan struct{}, 1)
	var closed atomic.Int32
	app, err := New(Options{
		Title: "signals", Width: 320, Height: 200,
		HTML: `<!doctype html><body style="background:#10151c">signals</body>`,
		OnNavigationCompleted: func(NavigationCompleted) {
			select {
			case loaded <- struct{}{}:
			default:
			}
		},
		OnClose: func() { closed.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	terminated := make(chan os.Signal, 1)
	signal.Notify(terminated, syscall.SIGTERM)
	defer signal.Stop(terminated)

	pid := os.Getpid()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer app.Close()
		select {
		case <-loaded:
		case <-time.After(10 * time.Second):
			t.Error("the page never loaded")
			return
		}
		deadline := time.Now().Add(3 * time.Second)
		sent := 0
		for time.Now().Before(deadline) {
			for i := 0; i < 250; i++ {
				syscall.Kill(pid, syscall.SIGCHLD)
				sent++
			}
			// Real SIGCHLDs: a child that starts and exits.
			if err := exec.Command("/usr/bin/true").Run(); err != nil {
				t.Errorf("child: %v", err)
				return
			}
			if err := app.SetTitle(fmt.Sprintf("signals %d", sent)); err != nil {
				t.Errorf("SetTitle during the storm: %v", err)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("sent %d SIGCHLD plus real child exits", sent)
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			t.Errorf("kill SIGTERM: %v", err)
			return
		}
		select {
		case <-terminated:
		case <-time.After(5 * time.Second):
			t.Error("SIGTERM never reached the Go handler")
		}
	}()

	var runErr error
	onMain(func() { runErr = app.Run() })
	<-done
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if closed.Load() != 1 {
		t.Errorf("OnClose ran %d times, want 1", closed.Load())
	}
}

// Every way a Mac window ends must end Run within a second, call OnBeforeClose
// and OnClose once each, and go through the Go close path: the delegate answers
// NO to windowShouldClose: and NSTerminateCancel to applicationShouldTerminate:
// and Go does the closing. Each trigger below calls the same AppKit entry point
// the user's gesture reaches: the Quit menu item's action (Command-Q), the
// application's terminate: (Dock > Quit, logout), the window's performClose:
// (the red button) and App.Close.
func TestDarwinEveryCloseGestureEndsRun(t *testing.T) {
	requireGUI(t)

	// onUI runs fn on the main thread inside a pool.
	onUI := func(da *darwinApp, fn func()) {
		da.onUIThread(func() error { withPool(fn); return nil })
	}
	for _, tc := range []struct {
		name    string
		trigger func(t *testing.T, app *App, da *darwinApp)
	}{
		{"Quit menu item (Command-Q)", func(t *testing.T, app *App, da *darwinApp) {
			onUI(da, func() {
				// The application menu is the first item of the main menu; Quit is
				// its last item, Command-Q, action terminate:.
				mainMenu := send0(da.nsapp, objcSel("mainMenu"))
				if mainMenu == 0 {
					t.Error("the application has no main menu: Command-Q reaches nothing")
					return
				}
				appMenu := send0(send1(mainMenu, objcSel("itemAtIndex:"), 0), objcSel("submenu"))
				last := send0(appMenu, objcSel("numberOfItems")) - 1
				quit := send1(appMenu, objcSel("itemAtIndex:"), last)
				if key := goString(send0(quit, objcSel("keyEquivalent"))); key != "q" {
					t.Errorf("Quit key equivalent = %q, want q", key)
				}
				if mask := send0(quit, objcSel("keyEquivalentModifierMask")); mask&0x100000 == 0 { // NSEventModifierFlagCommand
					t.Errorf("Quit modifier mask = %#x, want Command", mask)
				}
				if action := send0(quit, objcSel("action")); action != objcSel("terminate:") {
					t.Errorf("Quit action = %#x, want terminate:", action)
				}
				if title := goString(send0(quit, objcSel("title"))); !strings.HasPrefix(title, "Quit ") {
					t.Errorf("Quit title = %q", title)
				}
				// Choosing the item is what the key equivalent does.
				send1(appMenu, objcSel("performActionForItemAtIndex:"), last)
			})
		}},
		{"terminate: (Dock Quit, logout)", func(t *testing.T, app *App, da *darwinApp) {
			onUI(da, func() { send1(da.nsapp, objcSel("terminate:"), 0) })
		}},
		{"performClose: (the red button)", func(t *testing.T, app *App, da *darwinApp) {
			onUI(da, func() { send1(da.window, objcSel("performClose:"), 0) })
		}},
		{"App.Close from a goroutine", func(t *testing.T, app *App, da *darwinApp) {
			if err := app.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded := make(chan struct{}, 1)
			var before, closed atomic.Int32
			var runReturned atomic.Int64
			app, err := New(Options{
				Title: "closing", Width: 320, Height: 200,
				HTML: `<!doctype html><body style="background:#10151c">closing</body>`,
				OnNavigationCompleted: func(NavigationCompleted) {
					select {
					case loaded <- struct{}{}:
					default:
					}
				},
				OnBeforeClose: func(WindowPlacement) { before.Add(1) },
				OnClose:       func() { closed.Add(1) },
			})
			if err != nil {
				t.Fatal(err)
			}
			da := app.impl.(*darwinApp)
			triggered := make(chan time.Time, 1)
			go func() {
				select {
				case <-loaded:
				case <-time.After(10 * time.Second):
					t.Error("the page never loaded")
					app.Close()
					return
				}
				triggered <- time.Now()
				tc.trigger(t, app, da)
				// If the gesture did not end Run, fail loudly instead of hanging.
				time.Sleep(3 * time.Second)
				if runReturned.Load() == 0 {
					t.Error("Run was still going 3s after the gesture")
					app.Close()
				}
			}()
			var runErr error
			onMain(func() {
				runErr = app.Run()
				runReturned.Store(time.Now().UnixNano())
			})
			at := <-triggered
			if runErr != nil {
				t.Fatalf("Run: %v", runErr)
			}
			if took := time.Unix(0, runReturned.Load()).Sub(at); took > time.Second {
				t.Errorf("Run took %v to end after the gesture, want under 1s", took)
			} else {
				t.Logf("Run ended %v after the gesture", took.Round(time.Millisecond))
			}
			if before.Load() != 1 || closed.Load() != 1 {
				t.Errorf("OnBeforeClose ran %d times and OnClose %d times, want once each", before.Load(), closed.Load())
			}
			if app.Window() != nil {
				t.Error("Window() after Run is not nil")
			}
		})
	}
}
