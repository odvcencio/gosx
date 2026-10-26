//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"net/url"
	"strings"
)

// openURLSchemes are the schemes OpenURL hands to the system. Anything else (a
// custom scheme, javascript:, data:) is refused: the bridge exposes OpenURL to
// the page, and a scheme this list does not name could launch an application the
// host never meant to.
var openURLSchemes = map[string]bool{"http": true, "https": true, "mailto": true, "file": true}

// validateOpenURL checks raw and returns the URL to open: non-empty, with a
// scheme from openURLSchemes, and for http and https a host.
func validateOpenURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: url is empty", ErrInvalidOptions)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: url: %v", ErrInvalidOptions, err)
	}
	if !openURLSchemes[parsed.Scheme] { // Parse lowercases the scheme
		return "", fmt.Errorf("%w: url scheme %q is not one of http, https, mailto or file", ErrInvalidOptions, parsed.Scheme)
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == "" {
		return "", fmt.Errorf("%w: %s url has no host", ErrInvalidOptions, parsed.Scheme)
	}
	return raw, nil
}

// openURLOnWorkspace is [[NSWorkspace sharedWorkspace] openURL:]: the default
// browser for http and https, the mail client for mailto, the default
// application for a file. NO means the system found nothing to open it with, or
// the file does not exist. Tests replace it to see what OpenURL would open.
var openURLOnWorkspace = func(target string) error {
	var err error
	withPool(func() {
		u := nsURL(target)
		if u == 0 {
			err = fmt.Errorf("%w: %q is not a valid URL", ErrInvalidOptions, target)
			return
		}
		workspace := send0(objcClass("NSWorkspace"), objcSel("sharedWorkspace"))
		if workspace == 0 {
			err = fmt.Errorf("%w: NSWorkspace is unavailable", ErrUnsupported)
			return
		}
		if !sendBool1(workspace, objcSel("openURL:"), u) {
			err = fmt.Errorf("desktop: the system could not open %q", target)
		}
	})
	return err
}

// OpenURL asks the system to open rawURL in the application registered for it.
// It runs on the main thread when the app is running, like the other platform
// calls, and on the caller's thread before Run and after it.
func (a *darwinApp) OpenURL(rawURL string) error {
	target, err := validateOpenURL(rawURL)
	if err != nil {
		return err
	}
	return a.onUIThread(func() error { return openURLOnWorkspace(target) })
}
