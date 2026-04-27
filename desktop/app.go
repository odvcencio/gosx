package desktop

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/odvcencio/gosx/desktop/bridge"
)

const (
	defaultTitle  = "GoSX"
	defaultWidth  = 1024
	defaultHeight = 768
	defaultURL    = "about:blank"
)

var (
	// ErrUnsupported reports that the current OS/architecture does not have a
	// desktop backend yet.
	ErrUnsupported = errors.New("desktop backend unsupported")

	// ErrInvalidOptions reports invalid desktop app configuration.
	ErrInvalidOptions = errors.New("invalid desktop options")

	// ErrWebView2Unavailable reports that the Windows WebView2 loader/runtime
	// needed by the desktop backend cannot be found.
	ErrWebView2Unavailable = errors.New("webview2 unavailable")
)

// Options configures a native desktop window.
type Options struct {
	Title          string
	Width          int
	Height         int
	AppID          string
	Version        string
	UpdateFeed     string
	URL            string
	HTML           string
	Debug          bool
	UserDataDir    string
	SingleInstance bool
	DPIAwareness   DPIAwareness
	Accessibility  AccessibilityOptions
	CrashReporter  CrashReporterOptions

	// DevTools enables the Chromium inspector. Independent of Debug so a
	// production build can temporarily flip dev-tools on for field
	// diagnosis without enabling the rest of the Debug surface (default
	// context menus, relaxed UA, etc.). When Debug is true, DevTools is
	// implicitly on.
	DevTools bool

	// BridgeLimit caps inbound message rate and size for the typed IPC
	// bridge. The zero value applies bridge.DefaultLimit (64 KiB / 200
	// msgs/s / burst 400). Set any field to use a custom partial limit;
	// in that mode zero fields disable their corresponding checks.
	BridgeLimit bridge.Limit

	// NativeBridge registers built-in window, dialog, clipboard, shell, and
	// notification methods on the typed desktop bridge. Keep this off for
	// arbitrary remote URLs; enable it for trusted app content such as a
	// packaged app://gosx bundle.
	NativeBridge bool

	// OnWebMessage receives every chrome.webview.postMessage payload as
	// a raw string, after the typed bridge has had a chance to dispatch
	// it. Prefer App.Bridge().Register for typed IPC; OnWebMessage is a
	// lower-level escape hatch for inspection or legacy message shapes.
	// Runs on the platform's webview dispatcher thread — keep it short.
	OnWebMessage func(message string)

	// OnSecondInstance fires in the already-running process when
	// SingleInstance is true and a later launch forwards its argv payload.
	OnSecondInstance func(message InstanceMessage)

	// OnSuspend and OnResume mirror OS lifecycle notifications where the
	// backend exposes them. Windows wires these to WM_POWERBROADCAST.
	OnSuspend func()
	OnResume  func()

	// OnWindowCreated fires after a native window handle has been created.
	// The initial Windows backend invokes it for the primary window.
	OnWindowCreated func(window *Window)

	// OnFileDrop fires when the platform backend receives shell file-drop
	// paths for the native window.
	OnFileDrop func(paths []string)

	// OnClose fires once the native window has been closed by the user or
	// the OS. Use it for graceful shutdown: flushing state, disposing
	// background workers, etc. Nil disables the callback.
	OnClose func()
}

// App is a native desktop host for a GoSX application or HTML document.
type App struct {
	options        Options
	impl           platformApp
	bridge         *bridge.Router
	preloadMu      sync.Mutex
	preloadScripts []string
	serviceMu      sync.RWMutex
	services       map[string]ServiceBinding
}

type platformApp interface {
	Run() error
	Close() error
	Navigate(url string) error
	SetHTML(html string) error
	PostMessage(message string) error
	ExecuteScript(script string) error
	OpenDevTools() error
	PrependBootstrapScript(script string) error
	Minimize() error
	Maximize() error
	Restore() error
	Focus() error
	SetTitle(title string) error
	Serve(prefix string, handler http.Handler) error
	OpenFileDialog(opts OpenFileOptions) ([]string, error)
	SaveFileDialog(opts SaveFileOptions) (string, error)
	Clipboard() (string, error)
	SetClipboard(text string) error
	OpenURL(url string) error
	SetFullscreen(enabled bool) error
	SetMinSize(width, height int) error
	SetMaxSize(width, height int) error
	NewWindow(options WindowOptions) (*Window, error)
	RegisterProtocol(scheme string) error
	RegisterFileType(ext, icon, handler string) error
	SetMenuBar(menu Menu) error
	SetTray(options TrayOptions) error
	CloseTray() error
	Notify(notification Notification) error
	SetFileDropHandler(handler func([]string)) error
}

// New validates options and constructs a platform desktop app.
func New(options Options) (*App, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}

	// Keep the caller's raw-message callback so the bridge can chain to
	// it after the typed router has had a chance to dispatch.
	userCallback := normalized.OnWebMessage

	app := &App{options: normalized}

	// Install the preprocessor wrapper. The wrapper is what the platform
	// impl actually invokes; it fans the message out through the bridge
	// router first, then into the user's raw callback.
	normalized.OnWebMessage = func(msg string) {
		if app.bridge != nil {
			_ = app.bridge.Dispatch(msg)
		}
		if userCallback != nil {
			userCallback(msg)
		}
	}
	app.options = normalized

	impl, err := newPlatformApp(normalized)
	if err != nil {
		return nil, err
	}
	app.impl = impl

	app.bridge = bridge.NewRouter(func(raw string) error {
		return impl.PostMessage(raw)
	}, normalized.BridgeLimit)
	app.registerServiceBridgeMethods()
	if normalized.NativeBridge {
		app.registerNativeBridgeMethods()
	}

	if err := app.addPreloadScript(bridge.BootstrapScript()); err != nil {
		return nil, err
	}

	return app, nil
}

// Run constructs a desktop app and blocks until the native window closes.
func Run(options Options) error {
	app, err := New(options)
	if err != nil {
		return err
	}
	return app.Run()
}

// Available reports whether the current platform backend is available.
func Available() error {
	return platformAvailable()
}

// Options returns the normalized options used to construct the app.
func (a *App) Options() Options {
	if a == nil {
		return Options{}
	}
	return a.options
}

// Bridge returns the typed IPC router for this app. Handlers register
// via Bridge().Register; host→page events go out via Bridge().Emit.
// Inbound chrome.webview.postMessage payloads are dispatched through
// this router before the legacy OnWebMessage raw callback fires.
func (a *App) Bridge() *bridge.Router {
	if a == nil {
		return nil
	}
	return a.bridge
}

// Run starts the native event loop and blocks until the window closes.
func (a *App) Run() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	if a.options.CrashReporter.Enabled {
		return runWithCrashReporter(a.options.CrashReporter, func() error {
			return a.impl.Run()
		})
	}
	return a.impl.Run()
}

// Close requests that the native window close.
func (a *App) Close() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Close()
}

// Navigate loads a URL in the hosted webview.
func (a *App) Navigate(url string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Navigate(url)
}

// SetHTML loads an in-memory HTML document in the hosted webview.
func (a *App) SetHTML(html string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.SetHTML(html)
}

// PostMessage delivers a string payload to the webview via the
// chrome.webview.onmessage event. The JS side receives it through:
//
//	window.chrome.webview.addEventListener("message", e => { ... e.data ... });
//
// Returns ErrWebView2Unavailable if the webview hasn't finished creation;
// callers should await the first OnWebMessage callback from the page
// before posting outbound messages if ordering matters.
func (a *App) PostMessage(message string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.PostMessage(message)
}

// ExecuteScript runs arbitrary JavaScript in the hosted webview's top
// frame. Use it sparingly — PostMessage is the preferred channel because
// the JS side can intercept it with an event listener rather than relying
// on mutable globals for side effects.
func (a *App) ExecuteScript(script string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.ExecuteScript(script)
}

// OpenDevTools pops the Chromium inspector in a separate window. It works
// when Options.Debug or Options.DevTools is true, which toggles
// AreDevToolsEnabled on the underlying settings object.
func (a *App) OpenDevTools() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.OpenDevTools()
}

// PrependBootstrapScript registers JS to run before every document load
// in the hosted webview. The built-in desktop bridge preload is installed
// automatically by New; additional calls append app-owned preload code after
// that bridge so page code can assume window.gosxDesktop is available from the
// first navigation.
func (a *App) PrependBootstrapScript(script string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.addPreloadScript(script)
}

func (a *App) addPreloadScript(script string) error {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	a.preloadMu.Lock()
	for _, existing := range a.preloadScripts {
		if existing == script {
			a.preloadMu.Unlock()
			return nil
		}
	}
	a.preloadScripts = append(a.preloadScripts, script)
	combined := strings.Join(a.preloadScripts, "\n;\n")
	a.preloadMu.Unlock()
	return a.impl.PrependBootstrapScript(combined)
}

func (a *App) registerNativeBridgeMethods() {
	if a == nil || a.bridge == nil {
		return
	}
	a.bridge.Register("gosx.desktop.app.info", func(ctx *bridge.Context) error {
		opts := a.Options()
		return ctx.Respond(map[string]any{
			"title":          opts.Title,
			"width":          opts.Width,
			"height":         opts.Height,
			"appID":          opts.AppID,
			"version":        opts.Version,
			"url":            opts.URL,
			"debug":          opts.Debug,
			"devTools":       opts.DevTools,
			"singleInstance": opts.SingleInstance,
			"nativeBridge":   opts.NativeBridge,
		})
	})
	a.bridge.Register("gosx.desktop.app.close", func(ctx *bridge.Context) error {
		if err := ctx.Respond(nil); err != nil {
			return err
		}
		return a.Close()
	})
	a.bridge.Register("gosx.desktop.window.minimize", func(ctx *bridge.Context) error {
		return bridgeVoid(ctx, a.Minimize())
	})
	a.bridge.Register("gosx.desktop.window.maximize", func(ctx *bridge.Context) error {
		return bridgeVoid(ctx, a.Maximize())
	})
	a.bridge.Register("gosx.desktop.window.restore", func(ctx *bridge.Context) error {
		return bridgeVoid(ctx, a.Restore())
	})
	a.bridge.Register("gosx.desktop.window.focus", func(ctx *bridge.Context) error {
		return bridgeVoid(ctx, a.Focus())
	})
	a.bridge.Register("gosx.desktop.window.setTitle", func(ctx *bridge.Context) error {
		var req struct {
			Title string `json:"title"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.SetTitle(req.Title))
	})
	a.bridge.Register("gosx.desktop.window.setFullscreen", func(ctx *bridge.Context) error {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.SetFullscreen(req.Enabled))
	})
	a.bridge.Register("gosx.desktop.window.setMinSize", func(ctx *bridge.Context) error {
		var req struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.SetMinSize(req.Width, req.Height))
	})
	a.bridge.Register("gosx.desktop.window.setMaxSize", func(ctx *bridge.Context) error {
		var req struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.SetMaxSize(req.Width, req.Height))
	})
	a.bridge.Register("gosx.desktop.dialog.openFile", func(ctx *bridge.Context) error {
		var opts OpenFileOptions
		if err := ctx.Decode(&opts); err != nil {
			return err
		}
		paths, err := a.OpenFileDialog(opts)
		if err != nil {
			return err
		}
		return ctx.Respond(paths)
	})
	a.bridge.Register("gosx.desktop.dialog.saveFile", func(ctx *bridge.Context) error {
		var opts SaveFileOptions
		if err := ctx.Decode(&opts); err != nil {
			return err
		}
		path, err := a.SaveFileDialog(opts)
		if err != nil {
			return err
		}
		return ctx.Respond(path)
	})
	a.bridge.Register("gosx.desktop.clipboard.readText", func(ctx *bridge.Context) error {
		text, err := a.Clipboard()
		if err != nil {
			return err
		}
		return ctx.Respond(text)
	})
	a.bridge.Register("gosx.desktop.clipboard.writeText", func(ctx *bridge.Context) error {
		var req struct {
			Text string `json:"text"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.SetClipboard(req.Text))
	})
	a.bridge.Register("gosx.desktop.shell.openExternal", func(ctx *bridge.Context) error {
		var req struct {
			URL string `json:"url"`
		}
		if err := ctx.Decode(&req); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.OpenURL(req.URL))
	})
	a.bridge.Register("gosx.desktop.notification.show", func(ctx *bridge.Context) error {
		var notification Notification
		if err := ctx.Decode(&notification); err != nil {
			return err
		}
		return bridgeVoid(ctx, a.Notify(notification))
	})
}

func bridgeVoid(ctx *bridge.Context, err error) error {
	if err != nil {
		return err
	}
	return ctx.Respond(nil)
}

// Minimize hides the native window without destroying the hosted webview.
func (a *App) Minimize() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Minimize()
}

// Maximize snaps the native window to the platform's maximized state.
func (a *App) Maximize() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Maximize()
}

// Restore returns the native window from minimized, maximized, or fullscreen
// state to its normal state where the backend supports it.
func (a *App) Restore() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Restore()
}

// Focus asks the OS to bring the native window to the foreground.
func (a *App) Focus() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Focus()
}

// SetTitle updates the native window title and the app's cached options.
func (a *App) SetTitle(title string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	title = strings.TrimSpace(title)
	if strings.ContainsRune(title, '\x00') {
		return fmt.Errorf("%w: title contains NUL", ErrInvalidOptions)
	}
	if title == "" {
		title = defaultTitle
	}
	a.options.Title = title
	return a.impl.SetTitle(title)
}

// Serve registers handler for requests whose webview URI starts with prefix,
// such as app://gosx/* for packaged local assets.
func (a *App) Serve(prefix string, handler http.Handler) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Serve(prefix, handler)
}

// OpenFileDialog shows a native open-file picker.
func (a *App) OpenFileDialog(opts OpenFileOptions) ([]string, error) {
	if a == nil || a.impl == nil {
		return nil, fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.OpenFileDialog(opts)
}

// SaveFileDialog shows a native save-file picker.
func (a *App) SaveFileDialog(opts SaveFileOptions) (string, error) {
	if a == nil || a.impl == nil {
		return "", fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.SaveFileDialog(opts)
}

// Clipboard returns the current text clipboard contents.
func (a *App) Clipboard() (string, error) {
	if a == nil || a.impl == nil {
		return "", fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.Clipboard()
}

// SetClipboard replaces the text clipboard contents.
func (a *App) SetClipboard(text string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.SetClipboard(text)
}

// OpenURL asks the platform shell to open url externally.
func (a *App) OpenURL(url string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.OpenURL(url)
}

// SetFullscreen toggles native fullscreen mode where supported.
func (a *App) SetFullscreen(enabled bool) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.SetFullscreen(enabled)
}

// SetMinSize configures the native window's minimum resize dimensions.
func (a *App) SetMinSize(width, height int) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.SetMinSize(width, height)
}

// SetMaxSize configures the native window's maximum resize dimensions.
func (a *App) SetMaxSize(width, height int) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.SetMaxSize(width, height)
}

// NewWindow requests an additional native window. The public API shape is
// available for apps to compile against; the current Windows backend returns
// ErrUnsupported until shared WebView2-environment multi-window support lands.
func (a *App) NewWindow(options WindowOptions) (*Window, error) {
	if a == nil || a.impl == nil {
		return nil, fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	normalized, err := normalizeWindowOptions(a.options, options)
	if err != nil {
		return nil, err
	}
	return a.impl.NewWindow(normalized)
}

// SetMenuBar installs or replaces the native menu bar for the app window.
func (a *App) SetMenuBar(menu Menu) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	if _, err := BuildMenuPlan(menu); err != nil {
		return err
	}
	return a.impl.SetMenuBar(menu)
}

// Tray registers a shell tray icon for the app. The returned Tray can remove
// the registration with Close.
func (a *App) Tray(options TrayOptions) (*Tray, error) {
	if a == nil || a.impl == nil {
		return nil, fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	if _, err := BuildTrayRegistration(a.options.AppID, options); err != nil {
		return nil, err
	}
	if err := a.impl.SetTray(options); err != nil {
		return nil, err
	}
	return &Tray{app: a}, nil
}

func (a *App) closeTray() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.CloseTray()
}

// Notify sends a native notification through the platform backend.
func (a *App) Notify(notification Notification) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	if _, err := BuildToastPayload(notification); err != nil {
		return err
	}
	return a.impl.Notify(notification)
}

// OnFileDrop replaces the file-drop callback after app construction.
func (a *App) OnFileDrop(handler func([]string)) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	a.options.OnFileDrop = handler
	return a.impl.SetFileDropHandler(handler)
}

// UpdateCheck reads the configured AppInstaller feed and compares its main
// package version against Options.Version. It does not install anything.
func (a *App) UpdateCheck() (UpdateInfo, error) {
	if a == nil {
		return UpdateInfo{}, fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return CheckAppInstallerUpdate(a.options.UpdateFeed, a.options.Version)
}

// UpdateApply opens the configured AppInstaller feed with the platform shell.
// On Windows this hands control to App Installer for the user-approved update.
func (a *App) UpdateApply() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	feed := strings.TrimSpace(a.options.UpdateFeed)
	if feed == "" {
		return fmt.Errorf("%w: update feed is empty", ErrInvalidOptions)
	}
	return a.impl.OpenURL(feed)
}

// RegisterProtocol registers scheme as a per-user URL protocol for the
// current application executable.
func (a *App) RegisterProtocol(scheme string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.RegisterProtocol(scheme)
}

// RegisterFileType registers ext as a per-user file association for the
// current application executable. icon is optional. handler is used as the
// file type description; pass an empty string for the default description.
func (a *App) RegisterFileType(ext, icon, handler string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.RegisterFileType(ext, icon, handler)
}

func devToolsEnabled(options Options) bool {
	return options.Debug || options.DevTools
}

func normalizeOptions(options Options) (Options, error) {
	options.Title = strings.TrimSpace(options.Title)
	options.AppID = strings.TrimSpace(options.AppID)
	options.Version = strings.TrimSpace(options.Version)
	options.UpdateFeed = strings.TrimSpace(options.UpdateFeed)
	options.URL = strings.TrimSpace(options.URL)
	options.UserDataDir = strings.TrimSpace(options.UserDataDir)
	options.CrashReporter.DumpDir = strings.TrimSpace(options.CrashReporter.DumpDir)
	options.CrashReporter.UploadEndpoint = strings.TrimSpace(options.CrashReporter.UploadEndpoint)
	options.Accessibility.Name = strings.TrimSpace(options.Accessibility.Name)
	options.Accessibility.Description = strings.TrimSpace(options.Accessibility.Description)

	if options.URL != "" && options.HTML != "" {
		return Options{}, fmt.Errorf("%w: url and html are mutually exclusive", ErrInvalidOptions)
	}
	if options.Title == "" {
		options.Title = defaultTitle
	}
	if options.AppID == "" {
		options.AppID = defaultAppID(options.Title)
	}
	if err := validateAppID(options.AppID); err != nil {
		return Options{}, err
	}
	if options.Width <= 0 {
		options.Width = defaultWidth
	}
	if options.Height <= 0 {
		options.Height = defaultHeight
	}
	if options.URL == "" {
		options.URL = defaultURL
	}
	dpi, err := normalizeDPIAwareness(options.DPIAwareness)
	if err != nil {
		return Options{}, err
	}
	options.DPIAwareness = dpi
	if options.Accessibility.Enabled && options.Accessibility.Name == "" {
		options.Accessibility.Name = options.Title
	}

	for name, value := range map[string]string{
		"title":                        options.Title,
		"appID":                        options.AppID,
		"version":                      options.Version,
		"updateFeed":                   options.UpdateFeed,
		"url":                          options.URL,
		"html":                         options.HTML,
		"userDataDir":                  options.UserDataDir,
		"crashReporter.dumpDir":        options.CrashReporter.DumpDir,
		"crashReporter.uploadEndpoint": options.CrashReporter.UploadEndpoint,
		"accessibility.name":           options.Accessibility.Name,
		"accessibility.description":    options.Accessibility.Description,
	} {
		if strings.ContainsRune(value, '\x00') {
			return Options{}, fmt.Errorf("%w: %s contains NUL", ErrInvalidOptions, name)
		}
	}

	return options, nil
}
