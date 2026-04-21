package desktop

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	options Options
	impl    platformApp
	bridge  *bridge.Router
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
// in the hosted webview. Useful for injecting the postMessage event
// subscription so the page code can assume a live Go↔JS channel from the
// very first navigation. Successive calls replace the previous snippet.
func (a *App) PrependBootstrapScript(script string) error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	return a.impl.PrependBootstrapScript(script)
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
