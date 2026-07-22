package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"m31labs.dev/gosx/dev"
	"m31labs.dev/gosx/env"
	"m31labs.dev/gosx/island/program"
)

const defaultDevListenAddr = ":3000"

type DevOptions struct {
	SceneInspector bool
}

// RunDev stages local runtime assets, runs the target app on an internal port,
// and fronts it with the GoSX dev proxy for live reload.
func RunDev(dir string) error {
	return RunDevWithOptions(dir, DevOptions{})
}

// RunDevWithOptions stages local runtime assets, runs the target app on an
// internal port, and fronts it with the GoSX dev proxy for live reload.
func RunDevWithOptions(dir string, options DevOptions) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}

	isMain, err := isMainPackage(absDir)
	if err != nil {
		return fmt.Errorf("inspect package: %w", err)
	}
	if !isMain {
		return fmt.Errorf("gosx dev requires a runnable app directory (package main): %s", absDir)
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

	publicAddr := publicListenAddr()
	publicURL := displayListenURL(publicAddr)
	buildDir := filepath.Join(absDir, "build")
	devServer := &dev.Server{
		Dir:            absDir,
		BuildDir:       buildDir,
		ProxyTarget:    internalBaseURL,
		SceneInspector: options.SceneInspector,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "gosx dev: "+format+"\n", args...)
		},
		OnChange: func() error {
			fmt.Fprintln(os.Stderr, "gosx dev: change detected, rebuilding assets and restarting app")
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

	fmt.Fprintf(os.Stderr, "gosx dev: staged assets in %s\n", buildDir)
	fmt.Fprintf(os.Stderr, "gosx dev: proxy %s -> %s\n", publicURL, internalBaseURL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "gosx dev: shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := devServer.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func prepareDevAssets(dir string) error {
	buildDir := filepath.Join(dir, "build")
	islandDir := filepath.Join(buildDir, "islands")
	cssDir := filepath.Join(buildDir, "css")
	if err := os.MkdirAll(islandDir, 0755); err != nil {
		return fmt.Errorf("create island build dir: %w", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		return fmt.Errorf("create css build dir: %w", err)
	}

	gosxRoot, err := resolveGoSXModuleRoot(dir)
	if err != nil {
		return err
	}
	if err := ensureWASMRuntimeDependencies(dir); err != nil {
		return err
	}
	wasmPath := filepath.Join(buildDir, "gosx-runtime.wasm")
	cmd := exec.Command("go", "build", "-o", wasmPath, gosxModuleImportPath+"/client/wasm")
	cmd.Dir = dir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "GOFLAGS="+goModuleCommandFlags)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build runtime wasm: %w", err)
	}

	if err := copyFirstExisting(
		filepath.Join(buildDir, "wasm_exec.js"),
		filepath.Join(getGOROOT(), "lib", "wasm", "wasm_exec.js"),
		filepath.Join(getGOROOT(), "misc", "wasm", "wasm_exec.js"),
	); err != nil {
		return fmt.Errorf("stage wasm_exec.js: %w", err)
	}
	standardGoWASMExec, err := readStandardGoWASMExec()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(buildDir, "standard-go-wasm_exec.js"), wrapStandardGoWASMExec(standardGoWASMExec), 0644); err != nil {
		return fmt.Errorf("stage standard-Go wasm_exec.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "bootstrap.js"), filepath.Join(gosxRoot, "client", "js", "bootstrap.js")); err != nil {
		return fmt.Errorf("stage bootstrap.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "bootstrap-lite.js"), filepath.Join(gosxRoot, "client", "js", "bootstrap-lite.js")); err != nil {
		return fmt.Errorf("stage bootstrap-lite.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "bootstrap-runtime.js"), filepath.Join(gosxRoot, "client", "js", "bootstrap-runtime.js")); err != nil {
		return fmt.Errorf("stage bootstrap-runtime.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "bootstrap-feature-islands.js"), filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-islands.js")); err != nil {
		return fmt.Errorf("stage bootstrap-feature-islands.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "bootstrap-feature-engines.js"), filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-engines.js")); err != nil {
		return fmt.Errorf("stage bootstrap-feature-engines.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "bootstrap-feature-hubs.js"), filepath.Join(gosxRoot, "client", "js", "bootstrap-feature-hubs.js")); err != nil {
		return fmt.Errorf("stage bootstrap-feature-hubs.js: %w", err)
	}
	for _, chunk := range []string{
		"bootstrap-feature-scene3d.js",
		"bootstrap-feature-scene3d-command.js",
		"bootstrap-feature-scene3d-webgpu.js",
		"bootstrap-feature-scene3d-gltf.js",
		"bootstrap-feature-scene3d-animation.js",
	} {
		if err := copyFile(filepath.Join(buildDir, chunk), filepath.Join(gosxRoot, "client", "js", chunk)); err != nil {
			return fmt.Errorf("stage %s: %w", chunk, err)
		}
	}
	if err := copyFile(filepath.Join(buildDir, "patch.js"), filepath.Join(gosxRoot, "client", "js", "patch.js")); err != nil {
		return fmt.Errorf("stage patch.js: %w", err)
	}
	hlsData, err := os.ReadFile(filepath.Join(gosxRoot, "client", "js", "vendor", "hls.min.js"))
	if err != nil {
		return fmt.Errorf("read hls.min.js: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "hls.min.js"), runtimeJSAssetData("hls.min", hlsData), 0644); err != nil {
		return fmt.Errorf("stage hls.min.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "stripe-bridge.js"), filepath.Join(gosxRoot, "client", "js", "stripe-bridge.js")); err != nil {
		return fmt.Errorf("stage stripe-bridge.js: %w", err)
	}
	if err := copyFile(filepath.Join(buildDir, "relay.js"), filepath.Join(gosxRoot, "client", "js", "relay.js")); err != nil {
		return fmt.Errorf("stage relay.js: %w", err)
	}

	if err := compileDevIslands(dir, islandDir); err != nil {
		return err
	}
	if err := stageSidecarCSS(dir, cssDir); err != nil {
		return err
	}
	// Engine surfaces are no longer pre-compiled in dev. The server-side
	// engine/surface.Discover lowers them to shared-VM bytecode at start.
	// See ADR 0003 (supersedure) and ADR 0005 (buildsurface deletion).
	return nil
}

func compileDevIslands(dir, islandDir string) error {
	if err := os.MkdirAll(islandDir, 0755); err != nil {
		return fmt.Errorf("create island dir: %w", err)
	}

	entries, err := os.ReadDir(islandDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				_ = os.Remove(filepath.Join(islandDir, entry.Name()))
			}
		}
	}

	islands, _, err := collectProjectIslandPrograms(dir)
	if err != nil {
		return err
	}

	for _, isl := range islands {
		data, err := program.EncodeJSON(isl)
		if err != nil {
			return fmt.Errorf("encode island %s: %w", isl.Name, err)
		}
		path := filepath.Join(islandDir, isl.Name+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	return nil
}

func stageSidecarCSS(dir, cssDir string) error {
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		return fmt.Errorf("create css dir: %w", err)
	}

	entries, err := os.ReadDir(cssDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".css") {
				_ = os.Remove(filepath.Join(cssDir, entry.Name()))
			}
		}
	}

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && shouldSkipProjectDir(info.Name()) {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, ".css") || strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("relative css path %s: %w", path, err)
		}
		dst := filepath.Join(cssDir, rel)
		return copyFile(dst, path)
	})
}

func isMainPackage(dir string) (bool, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Name}}", ".")
	cmd.Dir = dir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOFLAGS="+goModuleCommandFlags, "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(out)) == "main", nil
}

func copyFirstExisting(dst string, candidates ...string) error {
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return copyFile(dst, candidate)
		}
	}
	return fmt.Errorf("no source file found for %s", filepath.Base(dst))
}

func copyFile(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func shouldSkipProjectDir(name string) bool {
	switch name {
	case ".git", "build", "dist", "node_modules":
		return true
	default:
		return strings.HasPrefix(name, ".tmp")
	}
}

type devRunner struct {
	dir string

	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan error
	stopping bool
}

func (r *devRunner) start(port string) error {
	r.mu.Lock()
	if r.cmd != nil {
		r.mu.Unlock()
		return fmt.Errorf("app already running")
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = r.dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = childProcessAttributes()
	cmd.Env = append(os.Environ(),
		"GOSX_DEV=1",
		"GOSX_APP_ROOT="+r.dir,
		"PORT="+port,
	)
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return err
	}

	r.cmd = cmd
	r.done = done
	r.stopping = false
	r.mu.Unlock()

	go func() {
		err := cmd.Wait()

		r.mu.Lock()
		expected := r.stopping
		if r.cmd == cmd {
			r.cmd = nil
			r.done = nil
		}
		r.stopping = false
		r.mu.Unlock()

		done <- err
		close(done)

		if expected {
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gosx dev: app exited: %v\n", err)
			return
		}
		fmt.Fprintln(os.Stderr, "gosx dev: app exited")
	}()

	return nil
}

func (r *devRunner) restart(port string) error {
	if err := r.stop(); err != nil {
		return err
	}
	return r.start(port)
}

func (r *devRunner) stop() error {
	r.mu.Lock()
	cmd := r.cmd
	done := r.done
	if cmd == nil || done == nil {
		r.mu.Unlock()
		return nil
	}
	r.stopping = true
	r.mu.Unlock()

	_ = interruptProcessTree(cmd.Process.Pid)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = terminateProcessTree(cmd.Process.Pid)
		<-done
	}
	return nil
}

func pickFreePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected listener address %T", ln.Addr())
	}
	return fmt.Sprintf("%d", addr.Port), nil
}

func waitForAppReady(baseURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	paths := []string{"/readyz", "/healthz", "/"}

	for time.Now().Before(deadline) {
		for _, path := range paths {
			resp, err := client.Get(baseURL + path)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode < http.StatusInternalServerError {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", baseURL)
}

func publicListenAddr() string {
	raw := strings.TrimSpace(os.Getenv("PORT"))
	if raw == "" {
		return defaultDevListenAddr
	}
	if strings.HasPrefix(raw, ":") {
		return raw
	}
	if strings.Contains(raw, ":") {
		return raw
	}
	return ":" + raw
}

func displayListenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") {
		return "http://" + addr
	}
	return "http://" + addr
}
