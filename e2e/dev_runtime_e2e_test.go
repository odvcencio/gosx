//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/hydrate"
)

type devRuntimeStep struct {
	Input    string `json:"input"`
	Expected any    `json:"expected"`
	Observed any    `json:"observed"`
}

type devRuntimeProof struct {
	Baseline          json.RawMessage    `json:"baseline,omitempty"`
	Revision          string             `json:"revision"`
	Tree              string             `json:"tree"`
	Dirty             bool               `json:"dirty"`
	SnapshotSHA256    string             `json:"snapshotSHA256"`
	FixtureSHA256     string             `json:"fixtureSHA256"`
	Go                string             `json:"go"`
	Browser           string             `json:"browser"`
	Runtime           hydrate.RuntimeRef `json:"runtime"`
	ServedSHA256      string             `json:"servedSHA256"`
	NoProductionBuild bool               `json:"noProductionBuild"`
	Steps             []devRuntimeStep   `json:"steps"`
	Passed            bool               `json:"passed"`
}

func newRuntimeBrowserProof(t *testing.T, fixtureFile string) devRuntimeProof {
	t.Helper()
	root := e2eRepoRoot(t)
	git := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "gc.auto=0", "-c", "maintenance.auto=false"}, args...)...)
		cmd.Dir = root
		data, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	fixtureBytes, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	fixtureHash := sha256.Sum256(fixtureBytes)
	snapshot := sha256.New()
	snapshot.Write(git("diff", "--binary", "HEAD"))
	for _, name := range strings.Split(string(git("ls-files", "--others", "--exclude-standard", "-z")), "\x00") {
		if name == "" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Write([]byte(name))
		snapshot.Write([]byte{0})
		snapshot.Write(body)
	}
	return devRuntimeProof{
		Revision:       strings.TrimSpace(string(git("rev-parse", "HEAD"))),
		Tree:           strings.TrimSpace(string(git("rev-parse", "HEAD^{tree}"))),
		Dirty:          len(git("status", "--porcelain")) > 0,
		SnapshotSHA256: hex.EncodeToString(snapshot.Sum(nil)),
		FixtureSHA256:  hex.EncodeToString(fixtureHash[:]),
		Go:             runtime.Version(),
	}
}

func writeRuntimeBrowserProof(t *testing.T, name string, proof *devRuntimeProof) {
	t.Helper()
	proof.Passed = !t.Failed()
	body, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Error(err)
		return
	}
	if dir := os.Getenv("GOSX_E2E_PROOF_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0644); err != nil {
			t.Error(err)
		}
	}
	t.Logf("browser runtime evidence: %s", body)
}

// A real Go-only app must hydrate on its first dev start. Every browser input
// below uses the mouse; no application script or verification opt-out is added.
func TestColdDevRuntimeHydratesAndRejectsTampering(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(root, "e2e", "testdata", "dev-runtime")
	proof := newRuntimeBrowserProof(t, filepath.Join(fixture, "main.go"))
	proof.NoProductionBuild = true
	baseline, err := os.ReadFile(filepath.Join(fixture, "baseline-observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	proof.Baseline = baseline
	defer writeRuntimeBrowserProof(t, "cold-dev-runtime.json", &proof)
	appDir := t.TempDir()
	copyFixtureTree(t, fixture, appDir)
	module := "module example.invalid/gosx-dev-proof\n\ngo 1.26.0\n\nrequire m31labs.dev/gosx v" + strings.TrimPrefix(gosx.Version, "v") + "\n\nreplace m31labs.dev/gosx => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"build.json", filepath.Join("dist", "build.json")} {
		if _, err := os.Stat(filepath.Join(appDir, path)); !os.IsNotExist(err) {
			t.Fatalf("fixture already has a production manifest: %s", path)
		}
	}
	app := startDevApp(t, appDir, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t)))
	page := newBrowserPage(t, chrome, nil, 1280, 800, "", 120*time.Second)
	page.eval(t, "navigator.userAgent", &proof.Browser)
	checkCounter := func(label string) hydrate.RuntimeRef {
		t.Helper()
		if status := page.navigate(t, app.baseURL); status != http.StatusOK {
			t.Fatalf("%s: HTTP %d", label, status)
		}
		page.waitFor(t, `typeof window.__gosx_hydrate === "function" && window.__gosx.ready === true`, 15*time.Second, label+" native hydration")
		var manifest hydrate.Manifest
		page.eval(t, `JSON.parse(document.getElementById("gosx-manifest").textContent)`, &manifest)
		if !strings.HasPrefix(manifest.Runtime.Path, "/gosx/runtime.wasm?v=") || len(manifest.Runtime.Hash) != 64 {
			t.Fatalf("%s: unverified runtime %+v", label, manifest.Runtime)
		}
		if err := chromedp.Run(page.ctx, chromedp.Click(`#gosx-island-0 [data-gosx-handler="increment"]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		page.waitFor(t, `document.querySelector("#gosx-island-0 .counter").textContent === "-1+"`, 5*time.Second, label+" counter increment")
		var observed string
		page.eval(t, `document.querySelector("#gosx-island-0 .counter").textContent`, &observed)
		proof.Steps = append(proof.Steps, devRuntimeStep{Input: label + ": load fresh page and click +", Expected: "-1+", Observed: observed})
		return manifest.Runtime
	}
	proof.Runtime = checkCounter("cold development, no production output")
	response, err := http.Get(app.baseURL + proof.Runtime.Path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("native artifact: HTTP %d, %v", response.StatusCode, err)
	}
	digest := sha256.Sum256(body)
	proof.ServedSHA256 = hex.EncodeToString(digest[:])
	if proof.ServedSHA256 != proof.Runtime.Hash {
		t.Fatal("manifest digest does not match served native bytes")
	}
	if err := os.MkdirAll(filepath.Join(appDir, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "dist", "build.json"), []byte(`{"runtime":{"wasm":{"file":"stale.wasm","hash":"stale"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if ref := checkCounter("stale production output present"); ref.Hash != proof.Runtime.Hash {
		t.Fatal("stale production metadata overrode development")
	}
	metaPath := filepath.Join(appDir, "build", buildmanifest.DevRuntimeManifestFile)
	original, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata buildmanifest.Manifest
	if err := json.Unmarshal(original, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Runtime.WASM.Hash = strings.Repeat("0", 64)
	bad, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, bad, 0644); err != nil {
		t.Fatal(err)
	}
	if status := page.navigate(t, app.baseURL); status != http.StatusOK {
		t.Fatal(status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(page.Console(), "runtime asset hash mismatch") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(page.Console(), "runtime asset hash mismatch") {
		t.Fatalf("tampered metadata was not rejected: %s", page.Console())
	}
	var nativeAvailable bool
	page.eval(t, `typeof window.__gosx_hydrate === "function"`, &nativeAvailable)
	if nativeAvailable {
		t.Fatal("unverified native module executed")
	}
	proof.Steps = append(proof.Steps, devRuntimeStep{Input: "replace expected native digest and reload", Expected: "reject before native execution", Observed: "runtime asset hash mismatch; native hydration export absent"})
	if err := os.WriteFile(metaPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	if ref := checkCounter("restored verified metadata"); ref.Hash != proof.Runtime.Hash {
		t.Fatal("restored metadata did not recover")
	}
}
