//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/buildmanifest"
)

// TestProductionBuildHydratesStrictComposition proves the production browser
// seam for same-file strict island composition with typed props, children, and
// named slots. It deliberately does not put children or slots on the root
// island calls: ir.LowerIsland rejects those in v1. Page may use a shared SSR
// wrapper outside the islands, but imported components are not composed inside
// the island program.
func TestProductionBuildHydratesStrictComposition(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "strict-composition")
	copyFixtureTree(t, filepath.Join(root, "e2e", "testdata", "strict-composition"), fixture)
	assertStrictCompositionFixtureHasNoAppJS(t, fixture)

	module := fmt.Sprintf("module example.com/gosx-strict-composition\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}

	output, err := runBoundedCommandOutput(t, 3*time.Minute, root, append(os.Environ(), "GOWORK=off"), "go", "run", "./cmd/gosx", "build", "--prod", fixture)
	if err != nil {
		t.Fatalf("gosx build --prod: %v\n%s", err, output)
	}
	t.Logf("gosx build --prod output:\n%s", output)
	if !strings.Contains(string(output), "Counter") {
		t.Fatalf("gosx build --prod did not report the Counter island:\n%s", output)
	}

	dist := filepath.Join(fixture, "dist")
	manifest, err := buildmanifest.Load(filepath.Join(dist, "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Runtime.BootstrapRuntime.File == "" || manifest.Runtime.BootstrapFeatureIslands.File == "" {
		t.Fatalf("production runtime island assets missing from manifest: %#v", manifest.Runtime)
	}
	if len(manifest.Islands) != 1 || manifest.Islands[0].Name != "Counter" || manifest.Islands[0].File == "" {
		t.Fatalf("expected one shared Counter island program for two instances, got %#v", manifest.Islands)
	}
	if manifest.Runtime.WASMIslands.File == "" {
		t.Fatalf("island-only WASM runtime missing from manifest: %#v", manifest.Runtime)
	}
	runtimeURLs := manifest.RuntimeURLs("/gosx/assets")
	selectedRuntime, ok := manifest.Runtime.WASMVariants["core"]
	if !ok || selectedRuntime.File == "" || selectedRuntime.Hash == "" || selectedRuntime.Size <= 0 {
		t.Fatalf("core runtime variant missing or incomplete: %#v", manifest.Runtime.WASMVariants)
	}
	selectedRuntimeURL := runtimeURLs.WASMVariants["core"]
	if selectedRuntimeURL == "" {
		t.Fatalf("core runtime URL missing from manifest RuntimeURLs: %#v", runtimeURLs.WASMVariants)
	}
	counterIslandURL := manifest.IslandURL("/gosx/assets", manifest.Islands[0])
	t.Logf("strict composition fixture=%s dist=%s buildManifest=%s", fixture, dist, filepath.Join(dist, "build.json"))
	logHashedAsset(t, "runtime bootstrap", manifest.Runtime.BootstrapRuntime)
	logHashedAsset(t, "island feature", manifest.Runtime.BootstrapFeatureIslands)
	logHashedAsset(t, "selected core runtime wasm", selectedRuntime.HashedAsset)
	logHashedAsset(t, "compatibility island wasm", manifest.Runtime.WASMIslands)
	logHashedAsset(t, "Counter island", manifest.Islands[0].HashedAsset)
	t.Logf("selected production URLs runtime=%s island=%s", selectedRuntimeURL, counterIslandURL)

	port := freeE2EPort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	logs := startBuiltFixture(t, dist, port)
	if err := waitForHealthy(baseURL+"/", 45*time.Second); err != nil {
		t.Fatalf("%v\n%s", err, logs.String())
	}

	client := &http.Client{Timeout: 5 * time.Second}
	pageResp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	pageHTML, readErr := readBoundedResponse(pageResp, 4<<20)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("root status = %d bodyPrefix=%q\n%s", pageResp.StatusCode, string(pageHTML[:min(len(pageHTML), 200)]), logs.String())
	}
	pageText := string(pageHTML)
	t.Logf("root HTML decoded bytes=%d hash=%s", len(pageHTML), buildmanifest.ContentHash(pageHTML))
	for _, snippet := range []string{
		`data-ssr-shared="chrome"`,
		`Alpha desk`,
		`Beta desk`,
		`seed 2`,
		`seed 11`,
		runtimeURLs.BootstrapRuntime,
		runtimeURLs.BootstrapFeatureIslands,
		selectedRuntimeURL,
		counterIslandURL,
	} {
		if !strings.Contains(pageText, snippet) {
			t.Fatalf("initial production HTML omitted %q\n%s", snippet, pageText)
		}
	}

	for _, asset := range []struct {
		label string
		url   string
		asset buildmanifest.HashedAsset
	}{
		{label: "runtime bootstrap", url: runtimeURLs.BootstrapRuntime, asset: manifest.Runtime.BootstrapRuntime},
		{label: "island feature", url: runtimeURLs.BootstrapFeatureIslands, asset: manifest.Runtime.BootstrapFeatureIslands},
		{label: "selected runtime", url: selectedRuntimeURL, asset: selectedRuntime.HashedAsset},
		{label: "Counter island", url: counterIslandURL, asset: manifest.Islands[0].HashedAsset},
	} {
		assertServedHashedAsset(t, client, baseURL, asset.label, asset.url, asset.asset.Hash, asset.asset.Size)
	}

	page := newBrowserPage(t, chrome, nil, 1024, 768, "", 75*time.Second)
	if status := page.navigate(t, baseURL+"/"); status != http.StatusOK {
		t.Fatalf("fixture status %d\n%s", status, logs.String())
	}
	if err := chromedp.Run(page.ctx,
		chromedp.WaitVisible(`#strict-counter-a`, chromedp.ByQuery),
		chromedp.WaitVisible(`#strict-counter-b`, chromedp.ByQuery),
		chromedp.Poll(`(() => {
  const roots = [...document.querySelectorAll("[data-gosx-island='Counter']")];
  return window.__gosx && window.__gosx.islands && window.__gosx.islands.size === 2 &&
    roots.length === 2 &&
    roots.some((root) => root.querySelector("#strict-counter-a")) &&
    roots.some((root) => root.querySelector("#strict-counter-b"));
})()`, nil, chromedp.WithPollingTimeout(20*time.Second)),
	); err != nil {
		t.Fatalf("wait for strict composition islands: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
	}

	page.eval(t, `new Promise((resolve) => setTimeout(resolve, 250))`, nil)
	page.eval(t, `window.__e1bCompositionDocToken = "doc-" + Date.now(); window.__e1bCompositionTimeOrigin = performance.timeOrigin;`, nil)
	stateScript := `(() => ({
  owners: window.__gosx && window.__gosx.islands ? window.__gosx.islands.size : -1,
  aText: document.querySelector("#strict-counter-a")?.textContent || "",
  bText: document.querySelector("#strict-counter-b")?.textContent || "",
  aValue: document.querySelector("#strict-counter-a-value")?.textContent?.trim() || "",
  bValue: document.querySelector("#strict-counter-b-value")?.textContent?.trim() || "",
  aFooter: document.querySelector("#strict-counter-a .strict-counter-total")?.textContent?.trim() || "",
  bFooter: document.querySelector("#strict-counter-b .strict-counter-total")?.textContent?.trim() || "",
  aSeed: document.querySelector("#strict-counter-a-seed")?.textContent?.trim() || "",
  bSeed: document.querySelector("#strict-counter-b-seed")?.textContent?.trim() || "",
  aButton: document.querySelector("#strict-counter-a-button")?.textContent?.trim() || "",
  bButton: document.querySelector("#strict-counter-b-button")?.textContent?.trim() || "",
  aDisabled: !!document.querySelector("#strict-counter-a-button")?.disabled,
  bDisabled: !!document.querySelector("#strict-counter-b-button")?.disabled,
  href: location.href,
  pathname: location.pathname,
  docToken: window.__e1bCompositionDocToken || "",
  timeOrigin: window.__e1bCompositionTimeOrigin || 0,
  roots: [...document.querySelectorAll("[data-gosx-island='Counter']")].map((el) => ({id: el.id, ownsA: !!el.querySelector("#strict-counter-a"), ownsB: !!el.querySelector("#strict-counter-b")})).sort((a, b) => a.id.localeCompare(b.id))
}))()`
	var before struct {
		Owners     int     `json:"owners"`
		AText      string  `json:"aText"`
		BText      string  `json:"bText"`
		AValue     string  `json:"aValue"`
		BValue     string  `json:"bValue"`
		AFooter    string  `json:"aFooter"`
		BFooter    string  `json:"bFooter"`
		ASeed      string  `json:"aSeed"`
		BSeed      string  `json:"bSeed"`
		AButton    string  `json:"aButton"`
		BButton    string  `json:"bButton"`
		ADisabled  bool    `json:"aDisabled"`
		BDisabled  bool    `json:"bDisabled"`
		Href       string  `json:"href"`
		Pathname   string  `json:"pathname"`
		DocToken   string  `json:"docToken"`
		TimeOrigin float64 `json:"timeOrigin"`
		Roots      []struct {
			ID    string `json:"id"`
			OwnsA bool   `json:"ownsA"`
			OwnsB bool   `json:"ownsB"`
		} `json:"roots"`
	}
	page.eval(t, stateScript, &before)
	if before.Owners != 2 || before.AValue != "2" || before.BValue != "11" ||
		before.AFooter != "Current: 2" || before.BFooter != "Current: 11" ||
		before.ASeed != "seed 2" || before.BSeed != "seed 11" ||
		!strings.Contains(before.AText, "Alpha desk") || !strings.Contains(before.BText, "Beta desk") ||
		before.AButton != "Increment a" || before.BButton != "Increment b" ||
		before.ADisabled || before.BDisabled || before.Pathname != "/" || before.DocToken == "" || before.TimeOrigin == 0 ||
		len(before.Roots) != 2 || before.Roots[0].ID == "strict-counter-a" || before.Roots[1].ID == "strict-counter-b" ||
		!(before.Roots[0].OwnsA != before.Roots[0].OwnsB && before.Roots[1].OwnsA != before.Roots[1].OwnsB) {
		t.Fatalf("initial browser state = %#v\nconsole:\n%s\npage errors: %v", before, page.Console(), page.PageErrors())
	}

	requestsBeforeClick := len(page.Requests())
	page.eval(t, `document.querySelector("#strict-counter-a-seed").click()`, nil)
	page.eval(t, `new Promise((resolve) => setTimeout(resolve, 200))`, nil)
	var inert struct {
		AValue     string  `json:"aValue"`
		BValue     string  `json:"bValue"`
		Requests   int     `json:"requests"`
		DocToken   string  `json:"docToken"`
		TimeOrigin float64 `json:"timeOrigin"`
	}
	page.eval(t, `(() => ({
  aValue: document.querySelector("#strict-counter-a-value")?.textContent?.trim() || "",
  bValue: document.querySelector("#strict-counter-b-value")?.textContent?.trim() || "",
  requests: performance.getEntriesByType("resource").length,
  docToken: window.__e1bCompositionDocToken || "",
  timeOrigin: window.__e1bCompositionTimeOrigin || 0
}))()`, &inert)
	if inert.AValue != "2" || inert.BValue != "11" || inert.DocToken != before.DocToken || inert.TimeOrigin != before.TimeOrigin || len(page.Requests()) != requestsBeforeClick {
		t.Fatalf("static slot click mutated state/navigation/network: inert=%#v beforeRequests=%d afterRequests=%d requests=%#v", inert, requestsBeforeClick, len(page.Requests()), page.Requests())
	}
	assertBrowserStillSettled(t, page, requestsBeforeClick, before.DocToken, before.TimeOrigin, "static slot click")

	page.eval(t, `document.querySelector("#strict-counter-a-button").click()`, nil)
	if err := chromedp.Run(page.ctx, chromedp.Poll(
		`document.querySelector("#strict-counter-a-value")?.textContent.trim() === "3" &&
document.querySelector("#strict-counter-a .strict-counter-total")?.textContent.trim() === "Current: 3" &&
document.querySelector("#strict-counter-b-value")?.textContent.trim() === "11" &&
document.querySelector("#strict-counter-b .strict-counter-total")?.textContent.trim() === "Current: 11"`,
		nil,
		chromedp.WithPollingTimeout(10*time.Second),
	)); err != nil {
		t.Fatalf("wait for A increment: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
	}
	requestsAfterA := len(page.Requests())
	if requestsAfterA != requestsBeforeClick {
		t.Fatalf("A click issued network requests: before=%d after=%d requests=%#v", requestsBeforeClick, requestsAfterA, page.Requests())
	}
	assertBrowserStillSettled(t, page, requestsBeforeClick, before.DocToken, before.TimeOrigin, "A click")

	page.eval(t, `document.querySelector("#strict-counter-b-button").click()`, nil)
	if err := chromedp.Run(page.ctx, chromedp.Poll(
		`document.querySelector("#strict-counter-a-value")?.textContent.trim() === "3" &&
document.querySelector("#strict-counter-a .strict-counter-total")?.textContent.trim() === "Current: 3" &&
document.querySelector("#strict-counter-b-value")?.textContent.trim() === "12" &&
document.querySelector("#strict-counter-b .strict-counter-total")?.textContent.trim() === "Current: 12"`,
		nil,
		chromedp.WithPollingTimeout(10*time.Second),
	)); err != nil {
		t.Fatalf("wait for B increment: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
	}
	if len(page.Requests()) != requestsBeforeClick {
		t.Fatalf("B click issued network requests: before=%d after=%d requests=%#v", requestsBeforeClick, len(page.Requests()), page.Requests())
	}
	assertBrowserStillSettled(t, page, requestsBeforeClick, before.DocToken, before.TimeOrigin, "B click")
	if len(page.PageErrors()) != 0 {
		t.Fatalf("unexpected page errors: %v\nconsole:\n%s", page.PageErrors(), page.Console())
	}
}

func assertStrictCompositionFixtureHasNoAppJS(t *testing.T, fixture string) {
	t.Helper()
	err := filepath.WalkDir(fixture, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".mjs") || strings.HasSuffix(name, ".ts") {
			return fmt.Errorf("fixture includes app-authored script source %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertBrowserStillSettled(t *testing.T, page *browserPage, wantRequests int, wantDocToken string, wantTimeOrigin float64, label string) {
	t.Helper()
	page.eval(t, `new Promise((resolve) => setTimeout(resolve, 200))`, nil)
	var state struct {
		Pathname   string  `json:"pathname"`
		DocToken   string  `json:"docToken"`
		TimeOrigin float64 `json:"timeOrigin"`
	}
	page.eval(t, `(() => ({ pathname: location.pathname, docToken: window.__e1bCompositionDocToken || "", timeOrigin: window.__e1bCompositionTimeOrigin || 0 }))()`, &state)
	if state.Pathname != "/" || state.DocToken != wantDocToken || state.TimeOrigin != wantTimeOrigin || len(page.Requests()) != wantRequests {
		t.Fatalf("%s changed document/navigation/network: state=%#v wantDocToken=%q wantTimeOrigin=%v wantRequests=%d gotRequests=%d requests=%#v", label, state, wantDocToken, wantTimeOrigin, wantRequests, len(page.Requests()), page.Requests())
	}
}

func assertServedHashedAsset(t *testing.T, client *http.Client, baseURL, label, path, wantHash string, wantSize int64) {
	t.Helper()
	if strings.TrimSpace(path) == "" || strings.TrimSpace(wantHash) == "" || wantSize <= 0 {
		t.Fatalf("%s missing path/hash/size: path=%q hash=%q size=%d", label, path, wantHash, wantSize)
	}
	resp, err := client.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s %s: %v", label, path, err)
	}
	data, readErr := readBoundedResponse(resp, 16<<20)
	if readErr != nil {
		t.Fatalf("read %s %s: %v", label, path, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s status = %d bodyPrefix=%q", label, path, resp.StatusCode, string(data[:min(len(data), 200)]))
	}
	if int64(len(data)) != wantSize {
		t.Fatalf("%s %s size = %d, want manifest size %d", label, path, len(data), wantSize)
	}
	if got := buildmanifest.ContentHash(data); got != wantHash {
		t.Fatalf("%s %s hash = %s, want %s", label, path, got, wantHash)
	}
}

func logHashedAsset(t *testing.T, label string, asset buildmanifest.HashedAsset) {
	t.Helper()
	t.Logf("%s asset file=%s hash=%s size=%d", label, asset.File, asset.Hash, asset.Size)
}

func runBoundedCommandOutput(t *testing.T, timeout time.Duration, dir string, env []string, name string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	outputFile, err := os.CreateTemp(t.TempDir(), "command-output-*.log")
	if err != nil {
		return nil, err
	}
	outputPath := outputFile.Name()
	cmd.Stdout = outputFile
	cmd.Stderr = outputFile
	if err := cmd.Start(); err != nil {
		_ = outputFile.Close()
		return readCommandOutput(outputPath), err
	}
	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid || pgid <= 0 {
		_ = cmd.Process.Kill()
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			_ = outputFile.Close()
			return readCommandOutput(outputPath), fmt.Errorf("failed to validate process group for pid %d and command did not reap after Kill", pid)
		}
		_ = outputFile.Close()
		if err == nil {
			err = fmt.Errorf("unexpected process group %d for pid %d", pgid, pid)
		}
		return readCommandOutput(outputPath), err
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		_ = outputFile.Close()
		err = errors.Join(err, ensureProcessGroupGone(pgid, name))
		if ctx.Err() != nil {
			err = errors.Join(context.DeadlineExceeded, err)
			return readCommandOutput(outputPath), fmt.Errorf("%s timed out after %s: %w", name, timeout, err)
		}
		return readCommandOutput(outputPath), err
	case <-ctx.Done():
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		select {
		case err := <-waitDone:
			_ = outputFile.Close()
			err = errors.Join(context.DeadlineExceeded, err, ensureProcessGroupGone(pgid, name))
			return readCommandOutput(outputPath), fmt.Errorf("%s timed out after %s after TERM: %w", name, timeout, err)
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			select {
			case err := <-waitDone:
				_ = outputFile.Close()
				err = errors.Join(context.DeadlineExceeded, err, ensureProcessGroupGone(pgid, name))
				return readCommandOutput(outputPath), fmt.Errorf("%s timed out after %s and required KILL: %w", name, timeout, err)
			case <-time.After(5 * time.Second):
				_ = outputFile.Close()
				return []byte("[output unavailable: process was not reaped after KILL]\n"), fmt.Errorf("%s timed out after %s and did not exit after KILL: %w", name, timeout, context.DeadlineExceeded)
			}
		}
	}
}

func ensureProcessGroupGone(pgid int, label string) error {
	if pgid <= 0 {
		return fmt.Errorf("%s unresolved process group %d", label, pgid)
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("%s process group %d liveness check failed: %w", label, pgid, err)
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(-pgid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("%s left process group %d alive after command exit; sent TERM", label, pgid)
		}
		return fmt.Errorf("%s process group %d liveness check after TERM failed: %w", label, pgid, err)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(-pgid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("%s left process group %d alive after command exit; sent TERM/KILL", label, pgid)
		}
		return fmt.Errorf("%s process group %d liveness check after KILL failed: %w", label, pgid, err)
	}
	return fmt.Errorf("%s left process group %d alive after command exit; sent TERM/KILL", label, pgid)
}

func readCommandOutput(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return []byte(fmt.Sprintf("[output unavailable: %v]\n", err))
	}
	return data
}

func readBoundedResponse(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeded %d bytes", limit)
	}
	return data, nil
}
