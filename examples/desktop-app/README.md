# GoSX desktop app template

A starting point for a desktop app on the GoSX desktop runtime. Copy this
folder and rename things; every GoSX desktop feature it uses is marked in
`main_desktop.go`.

What it shows:

| Feature | API |
|---|---|
| Native window with web UI (WebView2) | `desktop.New`, `App.Run` |
| No white flash at startup | `Options.BackgroundColor` |
| Discrete GPU on laptops | `Options.GPU` |
| A separate engine process that ends with the app | `desktop/sidecar` |
| Calling Go from the page | `App.Bind` and `window.gosxDesktop.service(name)` |
| Native menus and file dialogs | `App.SetMenuBar`, `App.OpenFileDialog` |
| Pushing events to the page | `App.Bridge().Emit` |
| One window per user | `Options.SingleInstance`, `OnSecondInstance` |
| Focus changes (pause a game) | `Options.OnFocusChanged` |
| Startup timings | `Options.OnNavigationCompleted`, `App.StartupTimeline` |
| Error dialog before any window exists | `desktop.ShowMessage` |

The same executable is both the host and the engine: the host starts itself
with `-serve` as a sidecar, waits for the engine's address line, and opens the
page it serves. Replace `serveEngine` with your server; keep the ready line.

## Run

On Windows, with `WebView2Loader.dll` (from the WebView2 SDK) next to the
executable:

```sh
GOOS=windows GOARCH=amd64 go build -o desktop-app.exe ./examples/desktop-app
desktop-app.exe
```

`-smoke` closes the app five seconds after the engine starts and logs the
startup timings to standard error.

On macOS (amd64 or arm64) the backend uses AppKit and WKWebView, which ship with
the system. Nothing else is required, and no cgo or Xcode:

```sh
CGO_ENABLED=0 go build -o desktop-app ./examples/desktop-app
./desktop-app
```

The macOS backend is being built in slices. Today it provides the window, the
web view, `Navigate`, `SetHTML`, `Reload`, `ExecuteScript`, `SetTitle`, the
background color, `OnWindowCreated`, `OnNavigationCompleted`, `OnClose` and the
startup timeline (`EnvironmentReady` stays zero: WebKit has no separate
environment step), and a Hide/Quit application menu. This example runs on it
and works around what is not there yet: `SetMenuBar`, the file dialogs, the
message box and the typed bridge to the page report `desktop.ErrUnsupported`
until later slices land, and `GPU`, `SingleInstance` and `OnFocusChanged` have
no effect. `App.Run` must be called from the main goroutine (the `desktop`
package pins it to the main thread when it is imported). SIGINT and SIGTERM
close the window and stop the engine.

## Package

Stage the executable, `WebView2Loader.dll`, and your files in a folder, then
run `gosx desktop package --input <folder> --config <config.json> --manifest-key <key-file>`
to build a per-user `Setup.exe`, a portable ZIP, and a `latest.json` update
manifest signed with your Ed25519 key. Without `--manifest-key` (or
`--manifest-sign-cmd`) the manifest is unsigned. See
`gosx desktop package --help`.

On Linux the example runs only the engine and prints its address; the desktop
runtime supports Windows and macOS today.
