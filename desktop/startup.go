package desktop

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// StartupTimeline records when each phase of desktop startup finished,
// measured from the call to New. A zero duration means the phase has not
// finished yet (or the backend does not report it).
type StartupTimeline struct {
	// WindowCreated is when the native window exists, just before
	// Options.OnWindowCreated runs.
	WindowCreated time.Duration
	// WindowShown is when the native window was first shown.
	WindowShown time.Duration
	// EnvironmentReady is when the WebView2 environment (browser process)
	// finished starting.
	EnvironmentReady time.Duration
	// ControllerReady is when the WebView2 controller finished starting,
	// just before the first navigation.
	ControllerReady time.Duration
	// FirstNavigationCompleted is when the first top-level navigation
	// finished, successfully or not.
	FirstNavigationCompleted time.Duration
}

// NavigationCompleted describes a finished top-level navigation.
type NavigationCompleted struct {
	// ID is the WebView2 navigation ID.
	ID uint64
	// Success reports whether the navigation succeeded.
	Success bool
	// WebErrorStatus is the COREWEBVIEW2_WEB_ERROR_STATUS value when
	// Success is false (for example 9 for a refused connection).
	WebErrorStatus int
}

type startupTimelineReporter interface {
	StartupTimeline() StartupTimeline
}

// StartupTimeline reports how long each startup phase took. Apps can log it
// from Options.OnNavigationCompleted to find where launch time goes.
func (a *App) StartupTimeline() StartupTimeline {
	if a == nil {
		return StartupTimeline{}
	}
	if reporter, ok := a.impl.(startupTimelineReporter); ok {
		return reporter.StartupTimeline()
	}
	return StartupTimeline{}
}

// rgbColor is a parsed Options.BackgroundColor.
type rgbColor struct {
	R, G, B uint8
}

// parseBackgroundColor accepts "#RGB" or "#RRGGBB" (case-insensitive) and
// returns the color and its canonical "#rrggbb" form. The empty string means
// "no background color".
func parseBackgroundColor(value string) (rgbColor, string, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return rgbColor{}, "", false, nil
	}
	hex, ok := strings.CutPrefix(value, "#")
	if !ok || (len(hex) != 3 && len(hex) != 6) {
		return rgbColor{}, "", false, fmt.Errorf("%w: background color %q must be #RGB or #RRGGBB", ErrInvalidOptions, value)
	}
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return rgbColor{}, "", false, fmt.Errorf("%w: background color %q must be #RGB or #RRGGBB", ErrInvalidOptions, value)
	}
	color := rgbColor{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}
	return color, fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B), true, nil
}
