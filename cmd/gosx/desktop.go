package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/odvcencio/gosx/desktop"
	desktopbridge "github.com/odvcencio/gosx/desktop/bridge"
	"github.com/odvcencio/gosx/dev"
	"github.com/odvcencio/gosx/env"
)

// DesktopRunOptions configures the native desktop development host.
type DesktopRunOptions struct {
	Title          string
	Width          int
	Height         int
	AppID          string
	Addr           string
	URL            string
	HTML           string
	UserDataDir    string
	Debug          bool
	DevTools       bool
	SingleInstance bool
	NativeSmoke    bool
}

func cmdDesktop() {
	var options DesktopRunOptions
	fs := flag.NewFlagSet("desktop", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&options.Title, "title", "GoSX", "native window title")
	fs.IntVar(&options.Width, "width", 1280, "native window width")
	fs.IntVar(&options.Height, "height", 800, "native window height")
	fs.StringVar(&options.AppID, "app-id", "", "desktop app id for shell integration and single-instance lock")
	fs.StringVar(&options.Addr, "addr", "", "desktop dev proxy listen address")
	fs.StringVar(&options.URL, "url", "", "open a URL directly instead of running a GoSX app")
	fs.StringVar(&options.HTML, "html", "", "open an inline HTML document directly instead of running a GoSX app")
	fs.StringVar(&options.UserDataDir, "user-data-dir", "", "WebView2 user data directory")
	fs.BoolVar(&options.Debug, "debug", false, "enable backend debug mode where supported")
	fs.BoolVar(&options.DevTools, "devtools", false, "enable Chromium devtools and the F12 inspector shortcut")
	fs.BoolVar(&options.SingleInstance, "single-instance", false, "forward later launches to the first instance")
	fs.BoolVar(&options.NativeSmoke, "native-smoke", false, "enable tray, notification, menu, and file-drop smoke hooks")
	parseArgs, devAlias := desktopArgsBeforeParse(os.Args[2:])
	if err := fs.Parse(parseArgs); err != nil {
		os.Exit(2)
	}
	args, parsedDevAlias := desktopArgsAfterParse(fs.Args())
	devAlias = devAlias || parsedDevAlias
	if len(args) > 1 || (desktopDirectMode(options) && (len(args) > 0 || devAlias)) {
		fmt.Fprintln(os.Stderr, "Usage: gosx desktop [flags] [dev [dir]]")
		os.Exit(1)
	}
	if strings.TrimSpace(options.URL) != "" && options.HTML != "" {
		fmt.Fprintln(os.Stderr, "gosx desktop: --url and --html are mutually exclusive")
		os.Exit(1)
	}
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	if err := RunDesktop(dir, options); err != nil {
		fmt.Fprintf(os.Stderr, "gosx desktop: %v\n", err)
		os.Exit(1)
	}
}

func desktopArgsBeforeParse(args []string) ([]string, bool) {
	if len(args) > 0 && args[0] == "dev" {
		return append([]string(nil), args[1:]...), true
	}
	return args, false
}

func desktopArgsAfterParse(args []string) ([]string, bool) {
	if len(args) > 0 && args[0] == "dev" {
		return args[1:], true
	}
	return args, false
}

// RunDesktop runs a GoSX app through the dev proxy and opens it in a native
// desktop host. The first backend is Windows WebView2; unsupported platforms
// return desktop.ErrUnsupported before doing build work.
func RunDesktop(dir string, options DesktopRunOptions) error {
	if err := desktop.Available(); err != nil {
		return err
	}
	if desktopDirectMode(options) {
		return runDesktopHost(options)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	isMain, err := isMainPackage(absDir)
	if err != nil {
		return fmt.Errorf("inspect package: %w", err)
	}
	if !isMain {
		return fmt.Errorf("gosx desktop requires a runnable app directory (package main): %s", absDir)
	}
	if err := syncModulesPackage(absDir); err != nil {
		return err
	}
	if err := ensureModuleDependencies(absDir); err != nil {
		return err
	}
	if err := env.LoadDir(absDir, ""); err != nil {
		return fmt.Errorf("load env: %w", err)
	}
	if err := prepareDevAssets(absDir); err != nil {
		return err
	}

	internalPort, err := pickFreePort()
	if err != nil {
		return fmt.Errorf("pick internal port: %w", err)
	}
	internalBaseURL := fmt.Sprintf("http://127.0.0.1:%s", internalPort)

	runner := &devRunner{dir: absDir}
	if err := runner.start(internalPort); err != nil {
		return err
	}
	defer runner.stop()

	if err := waitForAppReady(internalBaseURL, 20*time.Second); err != nil {
		_ = runner.stop()
		return fmt.Errorf("wait for app ready: %w", err)
	}

	var appMu sync.RWMutex
	var desktopApp *desktop.App
	setDesktopApp := func(app *desktop.App) {
		appMu.Lock()
		desktopApp = app
		appMu.Unlock()
	}
	getDesktopApp := func() *desktop.App {
		appMu.RLock()
		defer appMu.RUnlock()
		return desktopApp
	}

	publicAddr, err := desktopListenAddr(options.Addr)
	if err != nil {
		return err
	}
	publicURL := displayListenURL(publicAddr)
	buildDir := filepath.Join(absDir, "build")
	devServer := &dev.Server{
		Dir:         absDir,
		BuildDir:    buildDir,
		ProxyTarget: internalBaseURL,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "gosx desktop: "+format+"\n", args...)
		},
		OnChange: func() error {
			fmt.Fprintln(os.Stderr, "gosx desktop: change detected, rebuilding assets and restarting app")
			if err := prepareDevAssets(absDir); err != nil {
				return fmt.Errorf("build assets: %w", err)
			}
			if err := runner.restart(internalPort); err != nil {
				return fmt.Errorf("restart app: %w", err)
			}
			if err := waitForAppReady(internalBaseURL, 20*time.Second); err != nil {
				return fmt.Errorf("wait for app ready: %w", err)
			}
			if err := emitDesktopDevReload(getDesktopApp(), "file_change"); err != nil {
				fmt.Fprintf(os.Stderr, "gosx desktop: host reload notification failed: %v\n", err)
			}
			return nil
		},
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- devServer.ListenAndServe(publicAddr)
	}()
	if err := waitForDesktopProxyReady(publicURL, serverErr, 5*time.Second); err != nil {
		shutdownDesktopDevServer(devServer)
		return err
	}

	desktopOptions := desktop.Options{
		Title:            options.Title,
		Width:            options.Width,
		Height:           options.Height,
		AppID:            options.AppID,
		URL:              publicURL,
		Debug:            options.Debug,
		DevTools:         options.DevTools,
		UserDataDir:      options.UserDataDir,
		SingleInstance:   options.SingleInstance,
		OnSecondInstance: desktopSecondInstanceCallback(getDesktopApp),
	}
	if options.NativeSmoke {
		configureDesktopNativeSmokeOptions(&desktopOptions, getDesktopApp)
	}
	app, err := desktop.New(desktopOptions)
	if err != nil {
		shutdownDesktopDevServer(devServer)
		return err
	}
	if err := app.PrependBootstrapScript(desktopbridge.BootstrapScript()); err != nil {
		shutdownDesktopDevServer(devServer)
		return err
	}
	if options.NativeSmoke {
		if err := installDesktopNativeSmoke(app); err != nil {
			shutdownDesktopDevServer(devServer)
			return err
		}
	}
	setDesktopApp(app)

	fmt.Fprintf(os.Stderr, "gosx desktop: staged assets in %s\n", buildDir)
	fmt.Fprintf(os.Stderr, "gosx desktop: proxy %s -> %s\n", publicURL, internalBaseURL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	fatalServerErr := make(chan error, 1)
	go func() {
		select {
		case <-sigCh:
			_ = app.Close()
		case err := <-serverErr:
			if err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "gosx desktop: dev server exited: %v\n", err)
				fatalServerErr <- err
				_ = app.Close()
			}
		}
	}()

	appErr := app.Run()
	if shutdownErr := shutdownDesktopDevServer(devServer); shutdownErr != nil && appErr == nil {
		appErr = shutdownErr
	}
	select {
	case err := <-fatalServerErr:
		if appErr == nil {
			appErr = err
		}
	default:
	}
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed && appErr == nil {
			appErr = err
		}
	default:
	}
	return appErr
}

func runDesktopHost(options DesktopRunOptions) error {
	var app *desktop.App
	getApp := func() *desktop.App { return app }
	desktopOptions := desktop.Options{
		Title:            options.Title,
		Width:            options.Width,
		Height:           options.Height,
		AppID:            options.AppID,
		URL:              strings.TrimSpace(options.URL),
		HTML:             options.HTML,
		Debug:            options.Debug,
		DevTools:         options.DevTools,
		UserDataDir:      options.UserDataDir,
		SingleInstance:   options.SingleInstance,
		OnSecondInstance: desktopSecondInstanceCallback(getApp),
	}
	if options.NativeSmoke {
		configureDesktopNativeSmokeOptions(&desktopOptions, getApp)
	}
	app, err := desktop.New(desktopOptions)
	if err != nil {
		return err
	}
	if err := app.PrependBootstrapScript(desktopbridge.BootstrapScript()); err != nil {
		return err
	}
	if options.NativeSmoke {
		if err := installDesktopNativeSmoke(app); err != nil {
			return err
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		_ = app.Close()
	}()
	return app.Run()
}

func desktopSecondInstanceCallback(getApp func() *desktop.App) func(desktop.InstanceMessage) {
	return func(message desktop.InstanceMessage) {
		fmt.Fprintf(os.Stderr, "gosx desktop: second instance args=%q cwd=%q\n",
			message.Args, message.WorkingDir)
		if getApp == nil {
			return
		}
		app := getApp()
		if app == nil || app.Bridge() == nil {
			return
		}
		if err := app.Bridge().Emit("gosx.desktop.second_instance", message); err != nil {
			fmt.Fprintf(os.Stderr, "gosx desktop: second instance notification failed: %v\n", err)
		}
	}
}

func configureDesktopNativeSmokeOptions(options *desktop.Options, getApp func() *desktop.App) {
	if options == nil {
		return
	}
	priorWindowCreated := options.OnWindowCreated
	options.OnWindowCreated = func(window *desktop.Window) {
		if priorWindowCreated != nil {
			priorWindowCreated(window)
		}
		if window != nil {
			if err := window.ContextMenu(desktopNativeSmokeContextMenu(getApp)); err != nil {
				fmt.Fprintf(os.Stderr, "gosx desktop: native smoke context menu failed: %v\n", err)
			}
		}
		go func() {
			time.Sleep(750 * time.Millisecond)
			app := getDesktopAppForSmoke(getApp)
			if app == nil {
				return
			}
			if err := app.Notify(desktop.Notification{
				Title: "GoSX native smoke",
				Body:  "Tray, notification, menu, and file-drop hooks are active.",
			}); err != nil {
				fmt.Fprintf(os.Stderr, "gosx desktop: native smoke notification failed: %v\n", err)
			}
		}()
	}
	options.OnFileDrop = func(paths []string) {
		fmt.Fprintf(os.Stderr, "gosx desktop: file drop paths=%q\n", paths)
		app := getDesktopAppForSmoke(getApp)
		if app == nil || app.Bridge() == nil {
			return
		}
		if err := app.Bridge().Emit("gosx.desktop.file_drop", map[string][]string{"paths": paths}); err != nil {
			fmt.Fprintf(os.Stderr, "gosx desktop: file drop notification failed: %v\n", err)
		}
	}
}

func installDesktopNativeSmoke(app *desktop.App) error {
	if app == nil {
		return nil
	}
	menu := desktopNativeSmokeMenu(func() *desktop.App { return app })
	if err := app.SetMenuBar(menu); err != nil {
		return fmt.Errorf("install native smoke menu bar: %w", err)
	}
	if _, err := app.Tray(desktop.TrayOptions{
		Tooltip: "GoSX native smoke",
		Menu:    menu,
		OnClick: func(event desktop.TrayEvent) {
			fmt.Fprintf(os.Stderr, "gosx desktop: tray event=%s\n", event)
		},
	}); err != nil {
		return fmt.Errorf("install native smoke tray: %w", err)
	}
	return nil
}

func desktopNativeSmokeMenu(getApp func() *desktop.App) desktop.Menu {
	return desktop.Menu{Items: []desktop.MenuItem{
		{
			Label: "File",
			Submenu: &desktop.Menu{Items: []desktop.MenuItem{
				{ID: "notify", Label: "Notify", OnClick: func() {
					notifyDesktopNativeSmoke(getDesktopAppForSmoke(getApp))
				}},
				{Separator: true},
				{ID: "close", Label: "Close", OnClick: func() {
					if app := getDesktopAppForSmoke(getApp); app != nil {
						_ = app.Close()
					}
				}},
			}},
		},
		{
			Label: "Help",
			Submenu: &desktop.Menu{Items: []desktop.MenuItem{
				{ID: "native-smoke", Label: "Native Smoke", OnClick: func() {
					fmt.Fprintln(os.Stderr, "gosx desktop: native smoke menu clicked")
				}},
			}},
		},
	}}
}

func desktopNativeSmokeContextMenu(getApp func() *desktop.App) desktop.Menu {
	return desktop.Menu{Items: []desktop.MenuItem{
		{ID: "notify", Label: "Notify", OnClick: func() {
			notifyDesktopNativeSmoke(getDesktopAppForSmoke(getApp))
		}},
	}}
}

func notifyDesktopNativeSmoke(app *desktop.App) {
	if app == nil {
		return
	}
	if err := app.Notify(desktop.Notification{
		Title: "GoSX native smoke",
		Body:  "Native menu action fired.",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gosx desktop: native smoke notification failed: %v\n", err)
	}
}

func getDesktopAppForSmoke(getApp func() *desktop.App) *desktop.App {
	if getApp == nil {
		return nil
	}
	return getApp()
}

func emitDesktopDevReload(app *desktop.App, reason string) error {
	if app == nil || app.Bridge() == nil {
		return nil
	}
	return app.Bridge().Emit("gosx.dev.reload", map[string]string{
		"reason": reason,
		"time":   time.Now().Format(time.RFC3339Nano),
	})
}

func desktopDirectMode(options DesktopRunOptions) bool {
	return strings.TrimSpace(options.URL) != "" || options.HTML != ""
}

func desktopListenAddr(configured string) (string, error) {
	value := strings.TrimSpace(configured)
	if value == "" {
		port, err := pickFreePort()
		if err != nil {
			return "", fmt.Errorf("pick desktop proxy port: %w", err)
		}
		return "127.0.0.1:" + port, nil
	}
	if strings.HasPrefix(value, ":") {
		return "127.0.0.1" + value, nil
	}
	return value, nil
}

func waitForDesktopProxyReady(baseURL string, serverErr <-chan error, timeout time.Duration) error {
	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-serverErr:
			if err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("desktop proxy failed: %w", err)
			}
			return fmt.Errorf("desktop proxy stopped before it was ready")
		default:
		}

		resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/gosx/dev/info")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for desktop proxy %s", baseURL)
}

func shutdownDesktopDevServer(server *dev.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
