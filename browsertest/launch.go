package browsertest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// FindChrome returns the path to a Chrome or Chromium executable: CHROME_PATH
// when it is set (an error when that file is not executable), then the
// well-known names on PATH, then the platform's default install path.
func FindChrome() (string, error) {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		if isExecutable(p) {
			return p, nil
		}
		return "", errors.New("CHROME_PATH is set but not executable: " + p)
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	var defaults []string
	switch runtime.GOOS {
	case "darwin":
		defaults = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	case "windows":
		defaults = []string{`C:\Program Files\Google\Chrome\Application\chrome.exe`}
	default: // Linux and WSL2
		defaults = []string{"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe"}
	}
	for _, p := range defaults {
		if isExecutable(p) {
			return p, nil
		}
	}
	return "", errors.New("chrome not found: install Chrome/Chromium or set CHROME_PATH")
}

// isExecutable reports whether path exists and is a regular executable file.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// Options choose the browser Launch opens a tab in.
type Options struct {
	// Chrome is the browser executable. Empty means FindChrome.
	Chrome string
	// Flags are extra Chrome flags, such as "--use-angle=swiftshader".
	Flags []string
	// Endpoint is the DevTools WebSocket URL of a browser that is already
	// running, such as a GPU host's. Launch then works in a new tab of that
	// browser, brought to the front (a tab behind another draws no frames),
	// and closes only that tab. Chrome and Flags are not used.
	Endpoint string
}

// Launch returns a bound, active tab for the test and closes it, and the
// browser it started, when the test ends. Without Endpoint it starts Chrome
// with Start; it skips the test when no Chrome is found.
//
// Derive deadlines from the returned context with context.WithCancel or
// WithTimeout. Cancelling the returned context itself closes the tab.
func Launch(t testing.TB, opts Options) context.Context {
	t.Helper()
	if opts.Endpoint != "" {
		alloc, cancel := chromedp.NewRemoteAllocator(context.Background(), opts.Endpoint)
		t.Cleanup(cancel)
		// Cancelling the first context would close the shared browser, so
		// the test gets a second tab and only ever closes that one.
		browser, _ := chromedp.NewContext(alloc)
		if err := chromedp.Run(browser); err != nil {
			t.Fatalf("browsertest: attach to the browser at Endpoint: %v", err)
		}
		tab, closeTab := chromedp.NewContext(browser)
		t.Cleanup(closeTab)
		if err := chromedp.Run(tab, page.BringToFront()); err != nil {
			t.Fatalf("browsertest: bring the tab to the front: %v", err)
		}
		return tab
	}
	chrome := opts.Chrome
	if chrome == "" {
		found, err := FindChrome()
		if err != nil {
			t.Skip("browsertest: " + err.Error())
		}
		chrome = found
	}
	browser, err := Start(context.Background(), chrome, opts.Flags...)
	if err != nil {
		t.Fatalf("browsertest: %v", err)
	}
	t.Cleanup(browser.Close)
	return browser.Context
}
