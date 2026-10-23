//go:build windows || darwin

// Command desktop-engine shows a GoSX desktop app whose engine runs natively in
// the host process. The page is a normal GoSX page: an island reads the
// engine's shared signals and writes its call signal, exactly as it does for a
// WASM engine worker. The desktop adapter (desktop/enginehost) carries the same
// signals and calls over desktop IPC, so the island has no JavaScript and does
// not know where the engine runs.
//
// Run it from a checkout, after `go run ./cmd/gosx build-runtime build`:
//
//	GOOS=windows go build -o desktop-engine.exe ./examples/desktop-engine
//	desktop-engine.exe -root <dir containing build/>
//
// On Windows, WebView2Loader.dll must sit next to the executable or on PATH.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"m31labs.dev/gosx/desktop"
	"m31labs.dev/gosx/desktop/enginehost"
	"m31labs.dev/gosx/engine/host"
)

func main() {
	root := flag.String("root", "", "directory with build/ from `gosx build-runtime build` (default: next to the executable, else the repository)")
	smoke := flag.Duration("smoke", 0, "close the window after this long (tests); 0 keeps it open")
	flag.Parse()
	if err := run(*root, *smoke); err != nil {
		_, _ = desktop.ShowMessage(desktop.MessageOptions{Title: "GoSX native engine", Text: err.Error(), Kind: desktop.MessageError})
		os.Exit(1)
	}
}

func runtimeRoot(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "build")); err == nil {
			return dir, nil
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		repo := filepath.Join(filepath.Dir(file), "..", "..")
		if _, err := os.Stat(filepath.Join(repo, "build")); err == nil {
			return repo, nil
		}
	}
	return "", errors.New("no runtime assets: run `go run ./cmd/gosx build-runtime build` and pass -root")
}

func run(rootFlag string, smoke time.Duration) error {
	root, err := runtimeRoot(rootFlag)
	if err != nil {
		return err
	}
	handler, err := newHandler(root)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	url := fmt.Sprintf("http://%s/", ln.Addr())

	app, err := desktop.New(desktop.Options{
		Title:           "GoSX native engine",
		Width:           720,
		Height:          520,
		AppID:           "dev.gosx.example.desktop-engine",
		BackgroundColor: "#10151c",
		HTML:            `<body style="background:#10151c;color:#dde6f0;font:16px sans-serif;padding:32px">Starting...</body>`,
		// window.gosxDesktop carries the engine's signals and calls. Enable it
		// only for content you serve yourself, as here.
		NativeBridge: true,
		OnNavigationCompleted: func(e desktop.NavigationCompleted) {
			log.Printf("navigation %d success=%v status=%d", e.ID, e.Success, e.WebErrorStatus)
		},
	})
	if err != nil {
		return err
	}

	// The engine runs here, in this process. The page attaches to it when it
	// mounts the engine whose component name is "CounterEngine".
	engines := enginehost.New(app.Bridge(), enginehost.Options{Log: log.Printf})
	defer engines.Close()
	if err := engines.Register(component, func(in *enginehost.Instance) (host.Handle, error) {
		return Mount(in, 50*time.Millisecond)
	}); err != nil {
		return err
	}
	// App.Bridge().Register does not refresh the page's method list, which the
	// engine attach does not need: the page asks the host directly.

	go func() {
		// Navigate from a goroutine: GoSX runs it on the window thread.
		if err := app.Navigate(url); err != nil {
			log.Printf("navigate: %v", err)
			return
		}
		if smoke > 0 {
			time.Sleep(smoke)
			_ = app.Close()
		}
	}()
	return app.Run()
}
