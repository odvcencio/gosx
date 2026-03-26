package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/gosx/env"
	"github.com/odvcencio/gosx/route"
)

type exportManifest struct {
	Pages []string `json:"pages"`
}

// RunExport prerenders static file-routed pages into dist/static.
func RunExport(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}

	isMain, err := isMainPackage(absDir)
	if err != nil {
		return fmt.Errorf("inspect package: %w", err)
	}
	if !isMain {
		return fmt.Errorf("gosx export requires a runnable app directory (package main): %s", absDir)
	}

	if err := env.LoadDir(absDir, ""); err != nil {
		return fmt.Errorf("load env: %w", err)
	}
	if err := prepareDevAssets(absDir); err != nil {
		return err
	}

	pages, err := staticExportPages(filepath.Join(absDir, "app"))
	if err != nil {
		return err
	}

	internalPort, err := pickFreePort()
	if err != nil {
		return fmt.Errorf("pick export port: %w", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", internalPort)

	binDir, err := os.MkdirTemp("", "gosx-export-bin-*")
	if err != nil {
		return fmt.Errorf("create temp bin dir: %w", err)
	}
	defer os.RemoveAll(binDir)

	binaryPath := filepath.Join(binDir, "app")
	built, err := buildServerBinaryIfPresent(absDir, binaryPath)
	if err != nil {
		return fmt.Errorf("build export binary: %w", err)
	}
	if !built {
		return fmt.Errorf("gosx export requires a runnable app binary: %s", absDir)
	}

	cmd := exec.Command(binaryPath)
	cmd.Dir = absDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"PORT="+internalPort,
		"GOSX_APP_ROOT="+absDir,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start export app: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	if err := waitForAppReady(baseURL, 20*time.Second); err != nil {
		return fmt.Errorf("wait for export app ready: %w", err)
	}

	outputDir := filepath.Join(absDir, "dist", "static")
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("clear export dir: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create export dir: %w", err)
	}

	if err := copyDirIfPresent(filepath.Join(absDir, "public"), outputDir); err != nil {
		return fmt.Errorf("copy public assets: %w", err)
	}
	if err := copyExportRuntime(filepath.Join(absDir, "build"), outputDir); err != nil {
		return fmt.Errorf("copy runtime assets: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	for _, pagePath := range pages {
		html, err := fetchExportPage(client, baseURL+pagePath)
		if err != nil {
			return fmt.Errorf("export %s: %w", pagePath, err)
		}
		if err := writeExportPage(outputDir, pagePath, html); err != nil {
			return err
		}
	}

	if html, status, err := fetchExportPageWithStatus(client, baseURL+"/__gosx_export_missing__"); err == nil && status == http.StatusNotFound {
		if err := os.WriteFile(filepath.Join(outputDir, "404.html"), []byte(html), 0644); err != nil {
			return fmt.Errorf("write 404.html: %w", err)
		}
	}

	manifestData, err := json.MarshalIndent(exportManifest{Pages: pages}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(absDir, "dist", "export.json"), manifestData, 0644); err != nil {
		return fmt.Errorf("write export manifest: %w", err)
	}

	fmt.Fprintf(os.Stderr, "gosx export: wrote %d pages to %s\n", len(pages), outputDir)
	return nil
}

func staticExportPages(appDir string) ([]string, error) {
	bundle, err := route.ScanDir(appDir)
	if err != nil {
		return nil, fmt.Errorf("scan file routes: %w", err)
	}

	paths := []string{}
	for _, page := range bundle.Pages {
		if len(page.Params) > 0 {
			continue
		}
		if !page.Config.PrerenderEnabled(true) {
			continue
		}
		paths = append(paths, page.RoutePath)
	}
	sort.Strings(paths)
	return compactPaths(paths), nil
}

func compactPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	last := ""
	for _, path := range paths {
		if path == "" {
			path = "/"
		}
		if path == last {
			continue
		}
		out = append(out, path)
		last = path
	}
	return out
}

func copyExportRuntime(buildDir, outputDir string) error {
	gosxDir := filepath.Join(outputDir, "gosx")
	if err := os.MkdirAll(gosxDir, 0755); err != nil {
		return err
	}
	for _, asset := range []struct {
		src string
		dst string
	}{
		{src: filepath.Join(buildDir, "gosx-runtime.wasm"), dst: filepath.Join(gosxDir, "runtime.wasm")},
		{src: filepath.Join(buildDir, "wasm_exec.js"), dst: filepath.Join(gosxDir, "wasm_exec.js")},
		{src: filepath.Join(buildDir, "bootstrap.js"), dst: filepath.Join(gosxDir, "bootstrap.js")},
		{src: filepath.Join(buildDir, "patch.js"), dst: filepath.Join(gosxDir, "patch.js")},
	} {
		if err := copyFile(asset.dst, asset.src); err != nil {
			return err
		}
	}
	if err := copyDirIfPresent(filepath.Join(buildDir, "islands"), filepath.Join(gosxDir, "islands")); err != nil {
		return err
	}
	if err := copyDirIfPresent(filepath.Join(buildDir, "css"), filepath.Join(gosxDir, "css")); err != nil {
		return err
	}
	return nil
}

func fetchExportPage(client *http.Client, url string) (string, error) {
	body, status, err := fetchExportPageWithStatus(client, url)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", status)
	}
	return body, nil
}

func fetchExportPageWithStatus(client *http.Client, url string) (string, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(data), resp.StatusCode, nil
}

func writeExportPage(outputDir, routePath, html string) error {
	target := filepath.Join(outputDir, exportFilePath(routePath))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("create export dir for %s: %w", routePath, err)
	}
	if err := os.WriteFile(target, []byte(html), 0644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func exportFilePath(routePath string) string {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" || routePath == "/" {
		return "index.html"
	}
	clean := strings.Trim(routePath, "/")
	return filepath.Join(filepath.FromSlash(clean), "index.html")
}
