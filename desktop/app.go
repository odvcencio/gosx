package desktop

import (
	"errors"
	"fmt"
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
