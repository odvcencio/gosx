//go:build windows

// Command desktop-app is a template for a GoSX desktop app: a native window
// hosting web UI, a sidecar engine process, native menus and dialogs, typed
// Go services for the page, and single-instance launch handling.
//
// Run it with `go run ./examples/desktop-app` on Windows (WebView2Loader.dll
// must sit next to the executable or on PATH).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"time"
	"unicode/utf8"

	"m31labs.dev/gosx/desktop"
	"m31labs.dev/gosx/desktop/sidecar"
)

// filesService is bound for the page as window.gosxDesktop.service("files").
type filesService struct{ app *desktop.App }

type openedFile struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Text  string `json:"text"`
}

// Open shows the native Open dialog and returns the first 4 KiB of the file.
func (s *filesService) Open() (*openedFile, error) {
	paths, err := s.app.OpenFileDialog(desktop.OpenFileOptions{
		Title:   "Open a text file",
		Filters: []desktop.FileFilter{{Name: "Text files", Pattern: "*.txt;*.md;*.json"}, {Name: "All files", Pattern: "*.*"}},
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return readPreview(paths[0])
}

func readPreview(path string) (*openedFile, error) {
	const previewLimit = 4096
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	// Read at most the preview, however large the file is.
	preview, err := io.ReadAll(io.LimitReader(file, previewLimit))
	if err != nil {
		return nil, err
	}
	// The cut can split a multibyte character; drop only that incomplete
	// trailing character. Invalid bytes elsewhere still fail below.
	if int64(len(preview)) < info.Size() {
		preview = trimIncompleteRune(preview)
	}
	if !utf8.Valid(preview) {
		return nil, errors.New("not a UTF-8 text file")
	}
	return &openedFile{Path: path, Bytes: int(info.Size()), Text: string(preview)}, nil
}

// trimIncompleteRune removes a UTF-8 sequence cut off at the end of b.
func trimIncompleteRune(b []byte) []byte {
	for back := 1; back < utf8.UTFMax && back <= len(b); back++ {
		start := len(b) - back
		if utf8.RuneStart(b[start]) {
			if !utf8.FullRune(b[start:]) {
				return b[:start]
			}
			return b
		}
	}
	return b
}

func main() {
	serve := flag.Bool("serve", false, "run the engine sidecar (the host starts this itself)")
	smoke := flag.Bool("smoke", false, "exit five seconds after the engine starts and log startup timings (tests)")
	flag.Parse()
	if *serve {
		if err := serveEngine(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := runHost(*smoke); err != nil {
		// ShowMessage works before and without a window.
		_, _ = desktop.ShowMessage(desktop.MessageOptions{Title: "GoSX desktop app", Text: err.Error(), Kind: desktop.MessageError})
		os.Exit(1)
	}
}

func runHost(smoke bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	var app *desktop.App
	app, err = desktop.New(desktop.Options{
		Title:  "GoSX desktop app",
		Width:  960,
		Height: 640,
		AppID:  "dev.gosx.example.desktop-app",
		// Paint the window in the page's color before WebView2 draws.
		BackgroundColor: "#10151c",
		HTML:            `<body style="background:#10151c;color:#dde6f0;font:16px sans-serif;padding:32px">Starting the engine...</body>`,
		// Games usually want the discrete GPU on laptops.
		GPU: desktop.GPUOptions{Preference: desktop.GPUPreferenceHighPerformance},
		// A second launch forwards its arguments here instead of opening a
		// second window.
		SingleInstance: true,
		OnSecondInstance: func(message desktop.InstanceMessage) {
			log.Printf("second launch forwarded: %q", message.Args)
		},
		// Window.gosxDesktop (dialogs, window control, bound services) is
		// for trusted content only: here, our own loopback engine.
		NativeBridge: true,
		// Pause game loops or audio when the window loses focus.
		OnFocusChanged: func(focused bool) {
			log.Printf("window focused: %v", focused)
		},
		OnNavigationCompleted: func(event desktop.NavigationCompleted) {
			timeline := app.StartupTimeline()
			log.Printf("navigation %d success=%v; startup: window %v, WebView2 %v, first page %v",
				event.ID, event.Success, timeline.WindowShown, timeline.ControllerReady, timeline.FirstNavigationCompleted)
		},
	})
	if err != nil {
		return err
	}
	if _, err := app.Bind("files", &filesService{app: app}); err != nil {
		return err
	}
	if err := app.SetMenuBar(desktop.Menu{Items: []desktop.MenuItem{
		{ID: "file", Label: "&File", Submenu: &desktop.Menu{Items: []desktop.MenuItem{
			{ID: "file.open", Label: "&Open...", OnClick: func() {
				// Menu actions run on the window thread; dialogs are fine here.
				file, err := (&filesService{app: app}).Open()
				if err != nil || file == nil {
					return
				}
				_ = app.Bridge().Emit("app.fileOpened", file)
			}},
			{Separator: true},
			{ID: "file.exit", Label: "E&xit", OnClick: func() { _ = app.Close() }},
		}}},
	}}); err != nil {
		return err
	}

	// Start the engine while the window and WebView2 start. The sidecar
	// has no console window and ends with this process, even after a crash.
	engines := make(chan *sidecar.Process, 1)
	go func() {
		engine, err := sidecar.Start(context.Background(), sidecar.Options{
			Path:      self,
			Args:      []string{"-serve"},
			Output:    os.Stderr,
			ReadyLine: regexp.MustCompile(`^` + regexp.QuoteMeta(readyPrefix) + `(http://\S+)`),
		})
		if err != nil {
			_ = app.SetHTML(fmt.Sprintf("<body style='background:#10151c;color:#f88;padding:32px'>Engine failed: %v</body>", err))
			return
		}
		engines <- engine
		// Navigate from a goroutine: GoSX runs it on the window thread.
		_ = app.Navigate(engine.Ready())
		if smoke {
			time.Sleep(5 * time.Second)
			_ = app.Close()
		}
	}()
	runErr := app.Run()
	select {
	case engine := <-engines:
		_ = engine.Stop(2 * time.Second)
	default:
	}
	return runErr
}
