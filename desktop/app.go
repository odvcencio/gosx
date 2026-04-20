package desktop

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	Title       string
	Width       int
	Height      int
	URL         string
	HTML        string
	Debug       bool
	UserDataDir string

	// OnWebMessage is invoked on each chrome.webview.postMessage call from
	// JS. The callback runs on the platform's webview dispatcher thread —
	// keep it short; hand off to a goroutine or a channel for anything
	// expensive. Nil disables the JS→Go bridge.
	OnWebMessage func(message string)

	// OnClose fires once the native window has been closed by the user or
	// the OS. Use it for graceful shutdown: flushing state, disposing
	// background workers, etc. Nil disables the callback.
	OnClose func()
}

// App is a native desktop host for a GoSX application or HTML document.
type App struct {
	options Options
	impl    platformApp
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
}

// New validates options and constructs a platform desktop app.
func New(options Options) (*App, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	impl, err := newPlatformApp(normalized)
	if err != nil {
		return nil, err
	}
	return &App{options: normalized, impl: impl}, nil
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

// Run starts the native event loop and blocks until the window closes.
func (a *App) Run() error {
	if a == nil || a.impl == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
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

// OpenDevTools pops the Chromium inspector in a separate window. Only
// works when Options.Debug = true, which toggles AreDevToolsEnabled on
// the underlying settings object.
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

func normalizeOptions(options Options) (Options, error) {
	options.Title = strings.TrimSpace(options.Title)
	options.URL = strings.TrimSpace(options.URL)
	options.UserDataDir = strings.TrimSpace(options.UserDataDir)

	if options.URL != "" && options.HTML != "" {
		return Options{}, fmt.Errorf("%w: url and html are mutually exclusive", ErrInvalidOptions)
	}
	if options.Title == "" {
		options.Title = defaultTitle
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

	for name, value := range map[string]string{
		"title":       options.Title,
		"url":         options.URL,
		"html":        options.HTML,
		"userDataDir": options.UserDataDir,
	} {
		if strings.ContainsRune(value, '\x00') {
			return Options{}, fmt.Errorf("%w: %s contains NUL", ErrInvalidOptions, name)
		}
	}

	return options, nil
}
