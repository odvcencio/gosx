package desktop

import (
	"fmt"
	"strings"
)

// WindowOptions configures a desktop window created after the primary app
// window. The current Windows backend exposes the API shape but only creates
// the primary window; NewWindow returns ErrUnsupported until shared
// WebView2-environment multi-window support lands.
type WindowOptions struct {
	Title  string
	Width  int
	Height int
	URL    string
	HTML   string
}

// Window describes a native window surfaced through lifecycle callbacks.
type Window struct {
	handle         uintptr
	options        WindowOptions
	primary        bool
	setContextMenu func(Menu) error
}

// Options returns the normalized options used for the native window.
func (w *Window) Options() WindowOptions {
	if w == nil {
		return WindowOptions{}
	}
	return w.options
}

// Primary reports whether this is the app's primary window.
func (w *Window) Primary() bool {
	return w != nil && w.primary
}

// ContextMenu installs or replaces the native context menu for this window.
func (w *Window) ContextMenu(menu Menu) error {
	if w == nil || w.setContextMenu == nil {
		return fmt.Errorf("%w: window cannot set context menu", ErrInvalidOptions)
	}
	if _, err := BuildMenuPlan(menu); err != nil {
		return err
	}
	return w.setContextMenu(menu)
}

func newPrimaryWindow(handle uintptr, options Options, setContextMenu func(Menu) error) *Window {
	return &Window{
		handle: handle,
		options: WindowOptions{
			Title:  options.Title,
			Width:  options.Width,
			Height: options.Height,
			URL:    options.URL,
			HTML:   options.HTML,
		},
		primary:        true,
		setContextMenu: setContextMenu,
	}
}

func normalizeWindowOptions(base Options, options WindowOptions) (WindowOptions, error) {
	options.Title = strings.TrimSpace(options.Title)
	options.URL = strings.TrimSpace(options.URL)
	if options.URL != "" && options.HTML != "" {
		return WindowOptions{}, fmt.Errorf("%w: url and html are mutually exclusive",
			ErrInvalidOptions)
	}
	if options.Title == "" {
		options.Title = base.Title
	}
	if options.Width <= 0 {
		options.Width = base.Width
	}
	if options.Height <= 0 {
		options.Height = base.Height
	}
	if options.URL == "" {
		options.URL = defaultURL
	}
	for name, value := range map[string]string{
		"title": options.Title,
		"url":   options.URL,
		"html":  options.HTML,
	} {
		if strings.ContainsRune(value, '\x00') {
			return WindowOptions{}, fmt.Errorf("%w: %s contains NUL",
				ErrInvalidOptions, name)
		}
	}
	return options, nil
}
