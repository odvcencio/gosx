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
	"syscall"
	"time"

	"github.com/odvcencio/gosx/desktop"
	"github.com/odvcencio/gosx/dev"
	"github.com/odvcencio/gosx/env"
)

// DesktopRunOptions configures the native desktop development host.
type DesktopRunOptions struct {
	Title       string
	Width       int
	Height      int
	Addr        string
	URL         string
	HTML        string
	UserDataDir string
	Debug       bool
}

func cmdDesktop() {
	var options DesktopRunOptions
	fs := flag.NewFlagSet("desktop", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&options.Title, "title", "GoSX", "native window title")
	fs.IntVar(&options.Width, "width", 1280, "native window width")
	fs.IntVar(&options.Height, "height", 800, "native window height")
	fs.StringVar(&options.Addr, "addr", "", "desktop dev proxy listen address")
	fs.StringVar(&options.URL, "url", "", "open a URL directly instead of running a GoSX app")
	fs.StringVar(&options.HTML, "html", "", "open an inline HTML document directly instead of running a GoSX app")
	fs.StringVar(&options.UserDataDir, "user-data-dir", "", "WebView2 user data directory")
	fs.BoolVar(&options.Debug, "debug", false, "enable backend debug mode where supported")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 1 || (desktopDirectMode(options) && fs.NArg() > 0) {
		fmt.Fprintln(os.Stderr, "Usage: gosx desktop [flags] [dir]")
		os.Exit(1)
	}
	if strings.TrimSpace(options.URL) != "" && options.HTML != "" {
		fmt.Fprintln(os.Stderr, "gosx desktop: --url and --html are mutually exclusive")
		os.Exit(1)
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	if err := RunDesktop(dir, options); err != nil {
		fmt.Fprintf(os.Stderr, "gosx desktop: %v\n", err)
		os.Exit(1)
	}
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

	app, err := desktop.New(desktop.Options{
		Title:       options.Title,
		Width:       options.Width,
		Height:      options.Height,
		URL:         publicURL,
		Debug:       options.Debug,
		UserDataDir: options.UserDataDir,
	})
	if err != nil {
		shutdownDesktopDevServer(devServer)
		return err
	}

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
	app, err := desktop.New(desktop.Options{
		Title:       options.Title,
		Width:       options.Width,
		Height:      options.Height,
		URL:         strings.TrimSpace(options.URL),
		HTML:        options.HTML,
		Debug:       options.Debug,
		UserDataDir: options.UserDataDir,
	})
	if err != nil {
		return err
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
