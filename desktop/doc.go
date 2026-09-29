// Package desktop hosts GoSX applications in native desktop windows.
//
// The Windows backend uses the Win32 API and Microsoft Edge WebView2 without
// cgo. Available checks for the WebView2 loader and runtime, and
// WebView2RuntimeVersion reports the runtime selected by the app. Set
// Options.BrowserExecutableFolder to use a Fixed Version runtime instead of
// the installed Evergreen runtime. ErrWebView2LoaderUnavailable and
// ErrWebView2RuntimeUnavailable identify which dependency is missing; both
// also match ErrWebView2Unavailable.
//
// Options.AdditionalBrowserArguments sets
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS before the WebView2 environment is
// created. WebView2 reads this process-wide variable, so the value applies to
// every WebView2 environment in the process. For example, games can request
// --autoplay-policy=no-user-gesture-required. A value already present in
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS when the app starts is kept and
// placed last, so an operator can override the app's switches. Options.GPU
// adds --force_high_performance_gpu, --force_low_power_gpu, or
// --use-adapter-luid to pick the rendering adapter, and Options.MuteAudio
// adds --mute-audio.
//
// Windows apps can call AcquireSingleInstance at the start of main to reserve
// their app ID before desktop startup; close the returned InstanceLock when
// the process exits. If first is false, call ForwardToFirstInstance with the
// launch arguments and working directory, then exit.
//
// When Options.Debug is false, the backend disables browser accelerator keys,
// browser zoom controls, and the WebView2 status bar. Debug mode leaves these
// controls enabled. Options.OnProcessFailed reports the failed WebView2
// process kind; the application can call App.Reload when recovery is
// appropriate. HTML requestFullscreen calls use borderless fullscreen on the
// window's monitor and restore the previous window state when fullscreen ends.
// The executable's first icon resource is used for the window's large and
// small icons when present.
//
// macOS and Linux currently return ErrUnsupported; darwin/amd64 and
// darwin/arm64 are cross-compiled in CI so the unsupported path stays
// buildable while the native macOS backend is developed.
package desktop
