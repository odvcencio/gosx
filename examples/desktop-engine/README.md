# GoSX desktop app with a native engine

A small app whose engine runs natively in the desktop host process. An island
on the page shows the engine's clock and counter and has two buttons that call
the engine. The island has no JavaScript and does not know where the engine
runs: the same island works when the engine is a WASM worker in a browser.

| File | What it does |
|---|---|
| `engine.go` | The engine. It publishes through `host.Publisher` and serves calls with `host.ServeCalls`, so it does not know its transport. |
| `panel.gsx` | The island. It reads `$counter.view`, writes `counter.cmd` and reads `counter.cmd.result`. |
| `page.go` | Renders the island and declares the engine as a worker engine named `CounterEngine`. |
| `main_desktop.go` | Opens the window and registers the engine with `desktop/enginehost`. |

## Run

Build the GoSX runtime assets once, from the repository root:

```sh
go run ./cmd/gosx build-runtime build
```

Pass `-worker` to set `enginehost.Options.DisableNative` to `[]string{"*"}` and
run the same engine in the page's WASM Worker instead of in the host process.

Windows window-state persistence is opt-in through `desktop.Options.WindowState`.
When enabled, the window restores its saved placement and flushes updates on close.
On Windows, put `WebView2Loader.dll` next to the executable, then:

```sh
GOOS=windows GOARCH=amd64 go build -o desktop-engine.exe ./examples/desktop-engine
desktop-engine.exe -root <directory that contains build/>
```

`-root` defaults to the executable's directory when it has a `build/` folder,
and otherwise to the repository. `-smoke 10s` closes the window after 10 seconds.

The window shows `running/native`, a clock that advances, and a count that the
Increment button raises. The refused call shows `refused` as its error.

On macOS the same code builds with `CGO_ENABLED=0 go build ./examples/desktop-engine`.
The macOS backend carries the typed bridge through a `window.chrome.webview`
shim over `WKScriptMessageHandler`, so the native channel should work there; it
has not been run on a Mac. Linux has no desktop backend, so the example does not run there.
