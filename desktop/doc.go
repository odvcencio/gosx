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
// Use Options.InitialPlacement and App.WindowPlacement to persist a window's
// normal bounds and maximized state. Windows clamps restored bounds to
// monitor work areas, and Options.OnBeforeClose reports the placement before
// the native window is destroyed.
//
// App.Window returns the primary window once it exists, and Window.Handle
// returns its native handle (an HWND on Windows). Options.OnFocusChanged
// reports when the window gains or loses focus; with NativeBridge it also
// sends the page a "gosx.window.focus" event. ShowMessage shows a native
// message box and works before New; App.ShowMessage owns the box by the app
// window.
//
// On Windows, MenuItem.Accelerator is a display label only; route keystrokes
// through the page command registry.
//
// App.OfferSignedUpdate checks the signed update feed and asks before opening
// a publisher's allowlisted download page; it never downloads or installs an update.
//
// The macOS backend (darwin/amd64 and darwin/arm64) hosts a WKWebView in an
// NSWindow, also without cgo: Objective-C is reached through load-time imports
// and objc_msgSend, and the delegate callbacks are small assembly routines that
// record events for Go to handle on the main thread. App.Run must be called from
// the main goroutine, which this package pins to the main thread when it is
// imported. The backend currently provides the window, the web view, Navigate,
// SetHTML, Reload, ExecuteScript, PostMessage, OnWebMessage, SetTitle, PrependBootstrapScript,
// Options.OnWindowCreated, OnNavigationCompleted, OnBeforeClose (with a zero
// placement) and OnClose, and the startup timeline, where EnvironmentReady stays
// zero because WebKit has no separate environment step.
//
// On macOS, SetMenuBar installs the application's menu bar, before or after Run.
// The first menu of a Mac bar belongs to the application, which a cross-platform
// Menu (File, Edit, ...) does not carry: unless its first submenu is named for the
// app (Options.Title), SetMenuBar puts a standard application menu (About,
// Services, Hide, Hide Others, Show All, Quit) in front of it. A "&" mnemonic
// marker in a label is dropped. A MenuItem.Accelerator that reads as a chord
// ("Cmd+N", "Ctrl+Shift+S", "Alt+F4", "F5") also becomes the item's key
// equivalent, which AppKit offers once the page has declined the key; "Ctrl" is
// the Control key, so write "Cmd" (or "Mod") for the Command key, and a plain
// letter with no modifier is left as a label. MenuItem.Role sends standard
// editing actions (Cut, Copy, Paste, Select All, Undo, Redo) to the focused
// responder. Apps with their own document history can omit native Undo/Redo
// accelerators and keep those keys in their page command registry.
// SetMinSize and SetMaxSize limit
// the content size of the window, OpenURL hands http, https, mailto and file URLs
// to the system, and ShowMessage shows an application-modal NSAlert on the main
// thread. OpenFileDialog and SaveFileDialog use native AppKit panels; cancelling
// returns no selection and no error. NSSavePanel always confirms overwrites.
// Options.OnOpenFiles receives Finder's document paths, both at launch and when
// reopening the app. Declare CFBundleDocumentTypes in the app's Info.plist to
// offer it as a document editor. The desktop string transport and typed bridge
// work through a main-frame WKScriptMessageHandler. The clipboard API, window placement,
// single-instance launch handling and the other features report ErrUnsupported
// until later slices land. Options.UserDataDir, GPU, AdditionalBrowserArguments,
// BrowserExecutableFolder, DPIAwareness, MuteAudio and SingleInstance have no
// effect on macOS yet.
//
// Linux currently returns ErrUnsupported.
package desktop
