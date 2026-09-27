//go:build docs_tutorial

package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/buildmanifest"
	docsamples "m31labs.dev/gosx/examples/gosx-docs/samples"
)

type tutorialFile struct {
	target string
	sample string
}

type tutorialStep struct {
	name    string
	files   []tutorialFile
	want    []string
	hubPath string
	assets  []string
}

func TestFirstAppTutorialBuildsAndServesEveryStep(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve tutorial test location")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	work := t.TempDir()
	appDir := filepath.Join(work, "my-app")
	cli := filepath.Join(work, "gosx")

	runCommand(t, repoRoot, nil, "go", "build", "-o", cli, "./cmd/gosx")
	runCommand(t, repoRoot, nil, cli, "init", appDir)
	runCommand(t, appDir, nil, "go", "mod", "edit", "-replace=m31labs.dev/gosx="+repoRoot)
	quickstartBinary := filepath.Join(work, "app-quickstart")
	runCommand(t, appDir, nil, "go", "build", "-o", quickstartBinary, ".")
	quickstartPort := freeTutorialPort(t)
	quickstartURL := "http://127.0.0.1:" + quickstartPort
	quickstartProcess := exec.Command(quickstartBinary)
	quickstartProcess.Dir = appDir
	quickstartProcess.Env = appendTutorialEnv(os.Environ(), "PORT="+quickstartPort, "PUBLIC_URL="+quickstartURL, "SESSION_SECRET=docs-tutorial-test-session-secret")
	var quickstartLogs bytes.Buffer
	quickstartProcess.Stdout = &quickstartLogs
	quickstartProcess.Stderr = &quickstartLogs
	if err := quickstartProcess.Start(); err != nil {
		t.Fatalf("start scaffolded app: %v", err)
	}
	defer func() {
		_ = quickstartProcess.Process.Kill()
		_ = quickstartProcess.Wait()
	}()
	quickstartBody := waitTutorialHTTP(t, quickstartURL, &quickstartLogs)
	for _, expected := range []string{"My GoSX App", "Starter form", "Submit the starter action"} {
		if !strings.Contains(quickstartBody, expected) {
			t.Errorf("scaffolded quickstart response is missing %q", expected)
		}
	}
	captureTutorialScreenshot(t, quickstartURL, "quickstart-app.jpg")
	_ = quickstartProcess.Process.Kill()
	_ = quickstartProcess.Wait()

	steps := []tutorialStep{
		{
			name: "01-server-data",
			files: []tutorialFile{
				{"app/page.server.go", "tutorial/step-01-page-server.go.sample"},
				{"app/page.gsx", "tutorial/step-01-page.gsx.sample"},
			},
			want: []string{"Hello from Go", "Today’s visitors: 1"},
		},
		{
			name: "02-island",
			files: []tutorialFile{
				{"app/counter_props.go", "tutorial/step-02-counter-props.go.sample"},
				{"app/page.server.go", "tutorial/step-02-page-server.go.sample"},
				{"app/page.gsx", "tutorial/step-02-page.gsx.sample"},
			},
			want:   []string{"Add one: 0"},
			assets: []string{"islands"},
		},
		{
			name: "03-hub",
			files: []tutorialFile{
				{"app/tab_hub.go", "tutorial/step-03-tab-hub.go.sample"},
				{"main.go", "tutorial/step-03-main.go.sample"},
				{"app/page.server.go", "tutorial/step-03-page-server.go.sample"},
				{"app/page.gsx", "tutorial/step-03-page.gsx.sample"},
			},
			want:    []string{"Open tabs: 0"},
			hubPath: "/ws",
			assets:  []string{"hubs"},
		},
		{
			name: "04-scene3d",
			files: []tutorialFile{
				{"app/page.server.go", "tutorial/step-04-page-server.go.sample"},
				{"app/page.gsx", "tutorial/step-04-page.gsx.sample"},
			},
			want:   []string{"Open tabs: 0", "Hello from Go", "data-gosx-scene"},
			assets: []string{"hubs", "scene3d"},
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			for _, file := range step.files {
				body, err := docsamples.Read(file.sample)
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(appDir, filepath.FromSlash(file.target))
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if output := runCommand(t, appDir, nil, cli, "build", "--dev", appDir); output == "" {
				t.Fatal("gosx build produced no output")
			}
			binary := filepath.Join(work, "app-"+step.name)
			runCommand(t, appDir, nil, "go", "build", "-o", binary, ".")
			port := freeTutorialPort(t)
			baseURL := "http://127.0.0.1:" + port
			process := exec.Command(binary)
			process.Dir = appDir
			process.Env = appendTutorialEnv(os.Environ(), "PORT="+port, "PUBLIC_URL="+baseURL, "SESSION_SECRET=docs-tutorial-test-session-secret")
			var logs bytes.Buffer
			process.Stdout = &logs
			process.Stderr = &logs
			if err := process.Start(); err != nil {
				t.Fatalf("start app: %v", err)
			}
			defer func() {
				_ = process.Process.Kill()
				_ = process.Wait()
			}()

			body := waitTutorialHTTP(t, baseURL, &logs)
			for _, expected := range step.want {
				if !strings.Contains(body, expected) {
					t.Errorf("HTTP output for %s is missing %q", step.name, expected)
				}
			}
			captureTutorialScreenshot(t, baseURL, "step-"+strings.SplitN(step.name, "-", 2)[0]+".jpg")
			manifest, err := buildmanifest.Load(filepath.Join(appDir, "dist", "build.json"))
			if err != nil {
				t.Fatal(err)
			}
			for _, asset := range step.assets {
				assetFile := tutorialRuntimeAssetFile(manifest, asset)
				if assetFile == "" {
					t.Errorf("build manifest has no %s runtime asset", asset)
					continue
				}
				assetURL := baseURL + "/gosx/assets/runtime/" + assetFile
				response, err := http.Get(assetURL)
				if err != nil {
					t.Errorf("request runtime asset %s: %v", assetURL, err)
					continue
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Errorf("runtime asset %s status = %d, want 200", assetURL, response.StatusCode)
				}
			}
			if step.hubPath != "" {
				wsURL, err := url.Parse(baseURL)
				if err != nil {
					t.Fatal(err)
				}
				wsURL.Scheme = "ws"
				wsURL.Path = step.hubPath
				header := http.Header{"Origin": []string{baseURL}}
				conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
				if err != nil {
					t.Fatalf("connect tutorial hub: %v", err)
				}
				defer conn.Close()
				waitTutorialHTTPContains(t, baseURL, &logs, "Open tabs: 1")
			}
		})
	}
}

func captureTutorialScreenshot(t *testing.T, pageURL, filename string) {
	t.Helper()
	outputDir := os.Getenv("GOSX_DOCS_TUTORIAL_SCREENSHOTS")
	if outputDir == "" {
		return
	}
	if strings.HasPrefix(filename, "step-") {
		outputDir = filepath.Join(outputDir, "tutorial")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create tutorial screenshot directory: %v", err)
	}
	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()
	ctx, cancel := chromedp.NewContext(allocator)
	defer cancel()
	var screenshot []byte
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(pageURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.CaptureScreenshot(&screenshot),
	); err != nil {
		t.Fatalf("capture tutorial screenshot for %s: %v", pageURL, err)
	}
	image, err := png.Decode(bytes.NewReader(screenshot))
	if err != nil {
		t.Fatalf("decode tutorial screenshot for %s: %v", pageURL, err)
	}
	file, err := os.Create(filepath.Join(outputDir, filename))
	if err != nil {
		t.Fatalf("create tutorial screenshot %s: %v", filename, err)
	}
	if err := jpeg.Encode(file, image, &jpeg.Options{Quality: 94}); err != nil {
		_ = file.Close()
		t.Fatalf("encode tutorial screenshot %s: %v", filename, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close tutorial screenshot %s: %v", filename, err)
	}
}

func tutorialRuntimeAssetFile(manifest *buildmanifest.Manifest, name string) string {
	if manifest == nil {
		return ""
	}
	var asset buildmanifest.HashedAsset
	switch name {
	case "islands":
		asset = manifest.Runtime.BootstrapFeatureIslands
	case "hubs":
		asset = manifest.Runtime.BootstrapFeatureHubs
	case "scene3d":
		asset = manifest.Runtime.BootstrapFeatureScene3D
	default:
		return ""
	}
	return asset.File
}

func runCommand(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = appendTutorialEnv(os.Environ(), extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func appendTutorialEnv(env []string, values ...string) []string {
	out := make([]string, 0, len(env)+len(values))
	for _, item := range env {
		if strings.HasPrefix(item, "GOWORK=") {
			continue
		}
		out = append(out, item)
	}
	out = append(out, "GOWORK=off")
	return append(out, values...)
}

func freeTutorialPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(port)
}

func waitTutorialHTTP(t *testing.T, baseURL string, logs io.Reader) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK {
				return string(body)
			}
			if response.StatusCode >= 500 {
				logBody, _ := io.ReadAll(logs)
				t.Fatalf("app returned HTTP %d at %s:\n%s\n%s", response.StatusCode, baseURL, truncateTutorialOutput(string(body), 2400), truncateTutorialOutput(string(logBody), 2400))
			}
			lastErr = fmt.Errorf("HTTP status %d: %s", response.StatusCode, truncateTutorialOutput(string(body), 1000))
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	logBody, _ := io.ReadAll(logs)
	t.Fatalf("app did not serve %s within 30 seconds: %v\n%s", baseURL, lastErr, logBody)
	return ""
}

func waitTutorialHTTPContains(t *testing.T, baseURL string, logs io.Reader, expected string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var lastBody string
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			lastBody = string(body)
			if readErr == nil && response.StatusCode == http.StatusOK && strings.Contains(lastBody, expected) {
				return
			}
			lastErr = readErr
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	logBody, _ := io.ReadAll(logs)
	t.Fatalf("HTTP output did not contain %q after hub join: %v\n%s\n%s", expected, lastErr, truncateTutorialOutput(lastBody, 2400), truncateTutorialOutput(string(logBody), 2400))
}

func truncateTutorialOutput(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
