//go:build linux && (amd64 || arm64)

package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// linuxApp owns one GTK window. GTK and WebKit run on the locked Run thread;
// all other goroutines dispatch work through its GLib main context.
type linuxApp struct {
	unsupportedApp
	mu                    sync.Mutex
	options               Options
	gtk                   *gtkAPI
	window, view, manager uintptr
	primary               *Window
	bootstrap, token      string
	running, failed       bool
	mainLoop              bool
	box, menuBar          uintptr
	menu                  *Menu
	menuActions           map[uintptr]func()
	nextAction            uintptr
	minWidth, minHeight   int
	navID                 uint64
	created               time.Time
	timeline              StartupTimeline
	dispatch              uiDispatcher
}

var activeLinuxApp atomic.Pointer[linuxApp]
var linuxDestroyCallback = purego.NewCallback(func(_ uintptr, _ uintptr) {
	if a := activeLinuxApp.Load(); a != nil {
		a.onDestroy()
	}
})
var linuxLoadCallback = purego.NewCallback(func(_ uintptr, event int32, _ uintptr) {
	if a := activeLinuxApp.Load(); a != nil {
		a.onLoad(event)
	}
})
var linuxFailedCallback = purego.NewCallback(func(_ uintptr, _ int32, _ uintptr, _ uintptr, _ uintptr) int32 {
	if a := activeLinuxApp.Load(); a != nil {
		a.mu.Lock()
		a.failed = true
		a.mu.Unlock()
	}
	return 0
})
var linuxMessageCallback = purego.NewCallback(func(_ uintptr, result uintptr, _ uintptr) {
	if a := activeLinuxApp.Load(); a != nil {
		a.onMessage(result)
	}
})
var linuxDispatchCallback = purego.NewCallback(func(_ uintptr) int32 {
	if a := activeLinuxApp.Load(); a != nil {
		a.dispatch.drain()
	}
	return 0
})

func newPlatformApp(options Options) (platformApp, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	return &linuxApp{options: options, token: hex.EncodeToString(secret[:]), created: time.Now()}, nil
}
func platformAvailable() error                                           { _, err := loadGTK(); return err }
func showPlatformMessage(MessageOptions, uintptr) (MessageResult, error) { return "", ErrUnsupported }
func AcquireSingleInstance(string) (*InstanceLock, bool, error)          { return nil, false, ErrUnsupported }
func ForwardToFirstInstance(string, []string, string) error              { return ErrUnsupported }
func WebView2RuntimeVersion(string) (string, error)                      { return "", ErrUnsupported }

func (a *linuxApp) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !activeLinuxApp.CompareAndSwap(nil, a) {
		return fmt.Errorf("%w: another Linux desktop window is running", ErrInvalidOptions)
	}
	defer activeLinuxApp.CompareAndSwap(a, nil)
	gtk, err := loadGTK()
	if err != nil {
		return err
	}
	if gtk.gtkInitCheck(0, 0) == 0 {
		return fmt.Errorf("%w: GTK could not connect to a display", ErrUnsupported)
	}
	a.mu.Lock()
	a.gtk = gtk
	a.mu.Unlock()
	a.mu.Lock()
	profile := a.options.UserDataDir
	a.mu.Unlock()
	window := gtk.gtkWindowNew(0)
	if window == 0 {
		return fmt.Errorf("desktop: create GTK window")
	}
	var manager, view uintptr
	if profile == "" {
		manager = gtk.managerNew()
		if manager != 0 {
			view = gtk.viewNew(manager)
		}
	} else {
		dataDir := filepath.Join(profile, "data")
		cacheDir := filepath.Join(profile, "cache")
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			gtk.gtkWidgetDestroy(window)
			return err
		}
		if err := os.MkdirAll(cacheDir, 0700); err != nil {
			gtk.gtkWidgetDestroy(window)
			return err
		}
		data := gtk.websiteDataNew("base-data-directory", dataDir, "base-cache-directory", cacheDir, 0)
		if data == 0 {
			gtk.gtkWidgetDestroy(window)
			return fmt.Errorf("desktop: create WebKit data manager")
		}
		defer gtk.objectUnref(data)
		context := gtk.contextNewWithData(data)
		if context == 0 {
			gtk.gtkWidgetDestroy(window)
			return fmt.Errorf("desktop: create WebKit context")
		}
		defer gtk.objectUnref(context)
		view = gtk.viewNewWithContext(context)
		if view != 0 {
			manager = gtk.viewGetManager(view)
			if manager != 0 {
				gtk.objectRef(manager)
			}
		}
	}
	if manager == 0 || view == 0 {
		if manager != 0 {
			gtk.objectUnref(manager)
		}
		gtk.gtkWidgetDestroy(window)
		return fmt.Errorf("desktop: create WebKit view and content manager")
	}
	defer gtk.objectUnref(manager)
	if gtk.managerRegister(manager, "gosxDesktop") == 0 {
		gtk.gtkWidgetDestroy(window)
		return fmt.Errorf("desktop: register WebKit message handler")
	}
	a.mu.Lock()
	a.window = window
	a.view = view
	a.manager = manager
	a.running = true
	options := a.options
	a.primary = newPrimaryWindow(window, options, nil)
	a.timeline.WindowCreated = time.Since(a.created)
	a.mu.Unlock()
	gtk.signalConnect(window, "destroy", linuxDestroyCallback, 0, 0, 0)
	gtk.signalConnect(view, "load-changed", linuxLoadCallback, 0, 0, 0)
	gtk.signalConnect(view, "load-failed", linuxFailedCallback, 0, 0, 0)
	gtk.signalConnect(manager, "script-message-received::gosxDesktop", linuxMessageCallback, 0, 0, 0)
	a.dispatch.current = func() uint32 { return uint32(syscall.Gettid()) }
	a.dispatch.wake = func() error {
		if gtk.idleAdd(0, linuxDispatchCallback, 0, 0) == 0 {
			return fmt.Errorf("GLib idle queue unavailable")
		}
		return nil
	}
	a.dispatch.start(uint32(syscall.Gettid()))
	gtk.gtkWindowSetTitle(window, options.Title)
	gtk.gtkWindowSetDefaultSize(window, int32(options.Width), int32(options.Height))
	box := gtk.gtkBoxNew(1, 0)
	gtk.gtkContainerAdd(window, box)
	gtk.gtkBoxPackStart(box, view, 1, 1, 0)
	a.mu.Lock()
	a.box = box
	minWidth, minHeight := a.minWidth, a.minHeight
	a.mu.Unlock()
	if minWidth > 0 || minHeight > 0 {
		gtk.gtkWidgetSetSizeRequest(window, int32(minWidth), int32(minHeight))
	}
	a.installMenu()
	if options.MuteAudio {
		gtk.viewSetMuted(view, 1)
	}
	a.installBootstrap()
	if options.OnWindowCreated != nil {
		options.OnWindowCreated(a.primary)
	}
	a.mu.Lock()
	alive := a.running
	a.mu.Unlock()
	if !alive {
		a.dispatch.stop()
		return nil
	}
	gtk.gtkWidgetShowAll(window)
	a.mu.Lock()
	a.timeline.WindowShown = time.Since(a.created)
	a.timeline.ControllerReady = a.timeline.WindowShown
	options = a.options
	a.mu.Unlock()
	if options.HTML != "" {
		gtk.viewLoadHTML(view, options.HTML, 0)
	} else {
		gtk.viewLoadURI(view, options.URL)
	}
	a.mu.Lock()
	runLoop := a.running
	a.mainLoop = runLoop
	a.mu.Unlock()
	if runLoop {
		gtk.gtkMain()
	}
	a.mu.Lock()
	a.mainLoop = false
	a.box = 0
	a.menuBar = 0
	a.menuActions = nil
	a.mu.Unlock()
	a.mu.Lock()
	a.running = false
	a.window = 0
	a.view = 0
	a.manager = 0
	a.primary = nil
	a.mu.Unlock()
	a.dispatch.stop()
	return nil
}

func (a *linuxApp) onDestroy() {
	a.mu.Lock()
	wasRunning := a.running
	a.running = false
	cb := a.options.OnClose
	gtk := a.gtk
	mainLoop := a.mainLoop
	a.mu.Unlock()
	if wasRunning && cb != nil {
		cb()
	}
	if mainLoop {
		gtk.gtkMainQuit()
	}
}
func (a *linuxApp) onLoad(event int32) {
	a.mu.Lock()
	if event == 0 {
		a.failed = false
		a.navID++
	}
	if event != 3 {
		a.mu.Unlock()
		return
	}
	if a.timeline.FirstNavigationCompleted == 0 {
		a.timeline.FirstNavigationCompleted = time.Since(a.created)
	}
	cb := a.options.OnNavigationCompleted
	ev := NavigationCompleted{ID: a.navID, Success: !a.failed}
	a.mu.Unlock()
	if cb != nil {
		cb(ev)
	}
}
func (a *linuxApp) onMessage(result uintptr) {
	if result == 0 {
		return
	}
	value := a.gtk.resultValue(result)
	if value == 0 || a.gtk.valueIsString(value) == 0 {
		return
	}
	ptr := a.gtk.valueString(value)
	if ptr == 0 {
		return
	}
	defer a.gtk.free(ptr)
	size := a.gtk.strlen(ptr)
	if size > 1<<20 {
		return
	}
	raw := unsafe.String((*byte)(unsafe.Pointer(ptr)), int(size))
	var envelope struct {
		Token   string `json:"token"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil || envelope.Token != a.token {
		return
	}
	a.mu.Lock()
	cb := a.options.OnWebMessage
	running := a.running
	a.mu.Unlock()
	if running && cb != nil {
		cb(envelope.Message)
	}
}
func (a *linuxApp) installBootstrap() {
	a.mu.Lock()
	gtk, manager, script, token := a.gtk, a.manager, a.bootstrap, a.token
	a.mu.Unlock()
	if manager == 0 {
		return
	}
	encoded, _ := json.Marshal(token)
	transport := `(function(){const token=` + string(encoded) + `;const t=new EventTarget();t.postMessage=function(m){if(typeof m!=="string")throw new TypeError("desktop messages must be strings");window.webkit.messageHandlers.gosxDesktop.postMessage(JSON.stringify({token:token,message:m}));};window.__gosxNativeReceive=function(m){const e=new MessageEvent("message",{data:m});t.dispatchEvent(e);if(typeof t.onmessage==="function")t.onmessage(e);};window.chrome=window.chrome||{};window.chrome.webview=t;})();`
	gtk.managerRemoveScripts(manager)
	userScript := gtk.scriptNew(transport+"\n;\n"+script, 1, 0, 0, 0)
	if userScript != 0 {
		gtk.managerAddScript(manager, userScript)
		gtk.scriptUnref(userScript)
	}
}
func (a *linuxApp) onUI(fn func(*gtkAPI, uintptr, uintptr) error) error {
	return a.dispatch.run(func() error {
		a.mu.Lock()
		gtk, window, view, running := a.gtk, a.window, a.view, a.running
		a.mu.Unlock()
		if !running || window == 0 {
			return ErrWindowNotReady
		}
		return fn(gtk, window, view)
	})
}
func (a *linuxApp) Close() error {
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.gtkWidgetDestroy(w); return nil })
}
func (a *linuxApp) Reload() error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.viewReload(v); return nil })
}
func (a *linuxApp) Navigate(url string) error {
	if url == "" {
		return fmt.Errorf("%w: empty url", ErrInvalidOptions)
	}
	a.mu.Lock()
	a.options.URL = url
	a.options.HTML = ""
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.viewLoadURI(v, url); return nil })
}
func (a *linuxApp) SetHTML(html string) error {
	a.mu.Lock()
	a.options.HTML = html
	a.options.URL = ""
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.viewLoadHTML(v, html, 0); return nil })
}
func (a *linuxApp) ExecuteScript(script string) error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.viewEvaluate(v, script, -1, 0, 0, 0, 0, 0); return nil })
}
func (a *linuxApp) PostMessage(message string) error {
	b, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return a.ExecuteScript("window.__gosxNativeReceive(" + string(b) + ")")
}
func (a *linuxApp) PrependBootstrapScript(script string) error {
	a.mu.Lock()
	a.bootstrap = script
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { a.installBootstrap(); return nil })
}
func (a *linuxApp) Minimize() error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.gtkWindowIconify(w); return nil })
}
func (a *linuxApp) Maximize() error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.gtkWindowMaximize(w); return nil })
}
func (a *linuxApp) Restore() error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.gtkWindowUnmaximize(w); g.gtkWindowPresent(w); return nil })
}
func (a *linuxApp) Focus() error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.gtkWindowPresent(w); return nil })
}
func (a *linuxApp) SetTitle(title string) error {
	a.mu.Lock()
	a.options.Title = title
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(g *gtkAPI, w, v uintptr) error { g.gtkWindowSetTitle(w, title); return nil })
}
func (a *linuxApp) SetFullscreen(on bool) error {
	return a.onUI(func(g *gtkAPI, w, v uintptr) error {
		if on {
			g.gtkWindowFullscreen(w)
		} else {
			g.gtkWindowUnfullscreen(w)
		}
		return nil
	})
}
func (a *linuxApp) PrimaryWindow() *Window { a.mu.Lock(); defer a.mu.Unlock(); return a.primary }
func (a *linuxApp) StartupTimeline() StartupTimeline {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.timeline
}
