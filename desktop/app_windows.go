//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type windowsApp struct {
	options Options

	mu         sync.Mutex
	hwnd       uintptr
	env        *coreWebView2Environment
	controller *coreWebView2Controller
	webview    *coreWebView2
	runErr     error

	envHandler        *environmentCompletedHandler
	controllerHandler *controllerCompletedHandler
}

func newPlatformApp(options Options) (platformApp, error) {
	return &windowsApp{options: options}, nil
}

func platformAvailable() error {
	if err := procCreateCoreWebView2EnvironmentWithOptions.Find(); err != nil {
		return fmt.Errorf("%w: WebView2Loader.dll: %v", ErrWebView2Unavailable, err)
	}
	return nil
}

func (a *windowsApp) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := platformAvailable(); err != nil {
		return err
	}
	if err := coInitializeApartment(); err != nil {
		return err
	}
	defer coUninitialize()

	hwnd, err := createDesktopWindow(a.options.Title, a.options.Width, a.options.Height, a)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.hwnd = hwnd
	a.mu.Unlock()

	showWindow(hwnd)
	if err := a.createWebView(); err != nil {
		destroyWindow(hwnd)
		return err
	}
	if err := runMessageLoop(); err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runErr
}

func (a *windowsApp) Close() error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd != 0 {
		postMessage(hwnd, wmClose, 0, 0)
	}
	return nil
}

func (a *windowsApp) Navigate(url string) error {
	normalized, err := normalizeOptions(Options{Title: a.options.Title, Width: a.options.Width, Height: a.options.Height, URL: url})
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.options.URL = normalized.URL
	a.options.HTML = ""
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return nil
	}
	return webview.navigate(normalized.URL)
}

func (a *windowsApp) SetHTML(html string) error {
	if _, err := normalizeOptions(Options{Title: a.options.Title, Width: a.options.Width, Height: a.options.Height, HTML: html}); err != nil {
		return err
	}

	a.mu.Lock()
	a.options.HTML = html
	webview := a.webview
	a.mu.Unlock()
	if webview == nil {
		return nil
	}
	return webview.navigateToString(html)
}

func (a *windowsApp) createWebView() error {
	userDataDirValue, err := resolveUserDataDir(a.options.UserDataDir)
	if err != nil {
		return err
	}
	userDataDir, err := optionalUTF16Ptr(userDataDirValue)
	if err != nil {
		return err
	}

	a.envHandler = newEnvironmentCompletedHandler(a)
	hr, _, _ := procCreateCoreWebView2EnvironmentWithOptions.Call(
		0,
		uintptr(unsafe.Pointer(userDataDir)),
		0,
		uintptr(unsafe.Pointer(a.envHandler)),
	)
	if failedHRESULT(hr) {
		return hresultError{Op: "CreateCoreWebView2EnvironmentWithOptions", Code: hr}
	}
	return nil
}

func (a *windowsApp) onEnvironmentCreated(hr uintptr, env *coreWebView2Environment) {
	if failedHRESULT(hr) {
		a.failRun(hresultError{Op: "CreateCoreWebView2EnvironmentWithOptions callback", Code: hr})
		return
	}
	if env == nil {
		a.failRun(fmt.Errorf("%w: WebView2 environment was nil", ErrWebView2Unavailable))
		return
	}

	a.mu.Lock()
	a.env = env
	hwnd := a.hwnd
	a.controllerHandler = newControllerCompletedHandler(a)
	handler := a.controllerHandler
	a.mu.Unlock()

	if err := env.createController(hwnd, handler); err != nil {
		a.failRun(err)
	}
}

func (a *windowsApp) onControllerCreated(hr uintptr, controller *coreWebView2Controller) {
	if failedHRESULT(hr) {
		a.failRun(hresultError{Op: "CreateCoreWebView2Controller callback", Code: hr})
		return
	}
	if controller == nil {
		a.failRun(fmt.Errorf("%w: WebView2 controller was nil", ErrWebView2Unavailable))
		return
	}

	webview, err := controller.coreWebView2()
	if err != nil {
		a.failRun(err)
		return
	}

	a.mu.Lock()
	a.controller = controller
	a.webview = webview
	html := a.options.HTML
	url := a.options.URL
	a.mu.Unlock()

	if err := a.resizeWebView(); err != nil {
		a.failRun(err)
		return
	}
	if err := controller.setVisible(true); err != nil {
		a.failRun(err)
		return
	}
	if html != "" {
		err = webview.navigateToString(html)
	} else {
		err = webview.navigate(url)
	}
	if err != nil {
		a.failRun(err)
	}
}

func (a *windowsApp) resizeWebView() error {
	a.mu.Lock()
	hwnd := a.hwnd
	controller := a.controller
	a.mu.Unlock()
	if hwnd == 0 || controller == nil {
		return nil
	}
	bounds, err := clientRect(hwnd)
	if err != nil {
		return err
	}
	return controller.setBounds(bounds)
}

func (a *windowsApp) failRun(err error) {
	a.mu.Lock()
	if a.runErr == nil {
		a.runErr = err
	}
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd != 0 {
		postMessage(hwnd, wmClose, 0, 0)
	}
}

func (a *windowsApp) releaseWebView() {
	a.mu.Lock()
	controller := a.controller
	webview := a.webview
	env := a.env
	a.controller = nil
	a.webview = nil
	a.env = nil
	a.mu.Unlock()

	if controller != nil {
		controller.close()
	}
	if webview != nil {
		comRelease(unsafe.Pointer(webview))
	}
	if controller != nil {
		comRelease(unsafe.Pointer(controller))
	}
	if env != nil {
		comRelease(unsafe.Pointer(env))
	}
}

func optionalUTF16Ptr(value string) (*uint16, error) {
	if value == "" {
		return nil, nil
	}
	ptr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOptions, err)
	}
	return ptr, nil
}

func resolveUserDataDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", nil
	}
	dir := filepath.Join(cacheDir, "GoSX", "WebView2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create WebView2 user data dir: %w", err)
	}
	return dir, nil
}
