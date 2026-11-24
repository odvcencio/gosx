//go:build chrome

package hosttest

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/perf"
)

// padPage is a built page: a static HTML file in a temp directory, the way the
// game ships (the wasm is inline, because a file page cannot fetch files).
type padPage struct {
	dir  string
	html string
}

// buildPadPage builds testdata/padpage for js/wasm, with or without the
// gosx_padtest tag, and wraps it in a generated HTML file with GOROOT's
// wasm_exec.js beside it.
func buildPadPage(t *testing.T, tags string) padPage {
	t.Helper()
	dir := t.TempDir()
	wasm := filepath.Join(dir, "padpage.wasm")
	args := []string{"build", "-o", wasm}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "./testdata/padpage")
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build padpage: %v\n%s", err, out)
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	execJS, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wasm_exec.js"), execJS, 0o600); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(wasm)
	if err != nil {
		t.Fatal(err)
	}
	html := filepath.Join(dir, "padpage.html")
	page := `<!doctype html><meta charset="utf-8"><title>padpage</title>
<script src="wasm_exec.js"></script>
<script>
const bytes = Uint8Array.from(atob("` + base64.StdEncoding.EncodeToString(bin) + `"), c => c.charCodeAt(0));
const go = new Go();
WebAssembly.instantiate(bytes, go.importObject).then(r => go.run(r.instance));
</script>
`
	if err := os.WriteFile(html, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	return padPage{dir: dir, html: html}
}

// renderFlags mirrors the game's page checks (scripts/rig_test.go): software
// drawing on a machine with no GPU, the browser's own on a Mac.
func renderFlags() []chromedp.ExecAllocatorOption {
	if runtime.GOOS == "darwin" && os.Getenv("IF2112_SOFTWARE_GL") == "" {
		return nil
	}
	return []chromedp.ExecAllocatorOption{
		chromedp.Flag("use-angle", "swiftshader"),
		chromedp.Flag("enable-unsafe-swiftshader", true),
	}
}

// openPage opens the page at the file URL in a fresh headless Chrome with the
// game's options and its offline rule: http and https blocked, ws not.
func openPage(t *testing.T, page url.URL) context.Context {
	t.Helper()
	chrome, err := perf.FindChrome()
	if err != nil {
		t.Skip("no Chrome found: " + err.Error())
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:len(chromedp.DefaultExecAllocatorOptions):len(chromedp.DefaultExecAllocatorOptions)],
		chromedp.ExecPath(chrome))
	opts = append(opts, renderFlags()...)
	allocator, stop := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(stop)
	tab, closeTab := chromedp.NewContext(allocator)
	t.Cleanup(closeTab)
	ctx, cancel := context.WithTimeout(tab, 90*time.Second)
	t.Cleanup(cancel)
	err = chromedp.Run(ctx,
		network.Enable(),
		network.SetBlockedURLs().WithURLPatterns([]*network.BlockPattern{
			{URLPattern: "http://*:*/*", Block: true},
			{URLPattern: "https://*:*/*", Block: true},
		}),
		chromedp.EmulateViewport(1024, 768),
		emulation.SetFocusEmulationEnabled(true),
		chromedp.Navigate(page.String()),
	)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func eval(t *testing.T, ctx context.Context, js string) string {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
		t.Fatalf("evaluate %q: %v", js, err)
	}
	return out
}

func waitEval(t *testing.T, ctx context.Context, what, js, want string) {
	t.Helper()
	waitEvalFor(t, ctx, 20*time.Second, what, js, want)
}

func waitEvalFor(t *testing.T, ctx context.Context, limit time.Duration, what, js, want string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	var got string
	for time.Now().Before(deadline) {
		got = eval(t, ctx, js)
		if got == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: %s is %q, want %q", what, js, got, want)
}

const (
	statusJS = `document.documentElement.getAttribute("data-padtest") || "absent"`
	titleJS  = `document.title`
)

func fileURL(p padPage) url.URL { return url.URL{Scheme: "file", Path: filepath.ToSlash(p.html)} }

// The proof: a file page, with the game's Chrome flags and http/https blocked,
// opens the loopback WebSocket, and a test drives two pads through the real
// reader and poller on the engine's frame dispatcher.
func TestChromeFilePageDrivenPads(t *testing.T) {
	page := buildPadPage(t, "gosx_padtest")
	srv := startServer(t)
	u := srv.PageURL(fileURL(page))
	ctx := openPage(t, u)
	rc, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.WaitReady(rc); err != nil {
		t.Fatalf("WaitReady: %v (page status %q, title %q)", err, eval(t, ctx, statusJS), eval(t, ctx, titleJS))
	}
	if got := eval(t, ctx, statusJS); got != "ready" {
		t.Fatalf("data-padtest %q, want ready", got)
	}
	run, cancelRun := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelRun()

	if _, err := srv.Plug(run, 0, "Stub A (STANDARD GAMEPAD)", "standard"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Plug(run, 1, "Stub B (STANDARD GAMEPAD)", "standard"); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, ctx, titleJS); !strings.HasPrefix(got, "pads=[0,1] held=[]") {
		t.Fatalf("after plugging two pads the page saw %q", got)
	}

	// The receipt arrives after the page's poll, so the page has already seen
	// the press when Press returns: no wait before this read.
	r, err := srv.Press(run, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("press receipt: frame %d sampled %d", r.Frame, r.Sampled)
	if got := eval(t, ctx, titleJS); !strings.Contains(got, "held=[pad:0/button0]") {
		t.Fatalf("after Press the page saw %q", got)
	}
	if _, err := srv.Release(run, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, ctx, titleJS); !strings.Contains(got, "held=[]") {
		t.Fatalf("after Release the page saw %q", got)
	}

	// Unplug one of two pads while the other holds a button.
	if _, err := srv.Press(run, 1, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Press(run, 0, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Unplug(run, 0); err != nil {
		t.Fatal(err)
	}
	got := eval(t, ctx, titleJS)
	if !strings.HasPrefix(got, "pads=[1] held=[pad:1/button3]") {
		t.Fatalf("after unplugging slot 0 the page saw %q (want slot 1 kept at index 1, its button held, slot 0's button released)", got)
	}
	if status := eval(t, ctx, statusJS); status != "ready" {
		t.Fatalf("data-padtest %q after the run", status)
	}
}

// An ordinary build never connects, even with the field in the URL.
func TestChromeUntaggedBuildNeverConnects(t *testing.T) {
	page := buildPadPage(t, "")
	srv := startServer(t)
	ctx := openPage(t, srv.PageURL(fileURL(page)))
	waitEval(t, ctx, "page runs", `(Number(document.documentElement.getAttribute("data-frames")) > 10) ? "yes" : "no"`, "yes")
	rc, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.WaitReady(rc)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an untagged build became ready: %v", err)
	}
	if got := eval(t, ctx, statusJS); got != "absent" {
		t.Fatalf("untagged page has data-padtest %q", got)
	}
}

// With the field but nothing to connect to, the page says so and exposes no
// pads. A bad field value does the same.
func TestChromeTaggedBuildFailsVisibly(t *testing.T) {
	page := buildPadPage(t, "gosx_padtest")
	srv := startServer(t)
	good := srv.PageURL(fileURL(page))
	if err := srv.Close(); err != nil {
		t.Log("close:", err)
	}
	for name, u := range map[string]url.URL{
		"unreachable endpoint": good,
		"bad value":            {Scheme: "file", Path: filepath.ToSlash(page.html), RawQuery: "gosx-padtest=nonsense"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := openPage(t, u)
			// With no server at all the page keeps trying for its whole
			// 60 s join window before it says "failed"; a bad value fails
			// at once.
			limit := 20 * time.Second
			if name == "unreachable endpoint" {
				limit = 75 * time.Second
			}
			waitEvalFor(t, ctx, limit, name, statusJS, "failed")
			waitEval(t, ctx, "page runs", `(Number(document.documentElement.getAttribute("data-frames")) > 5) ? "yes" : "no"`, "yes")
			if got := eval(t, ctx, titleJS); !strings.HasPrefix(got, "pads=[] held=[]") {
				t.Fatalf("a failed page exposed pads: %q", got)
			}
		})
	}
}

// The server drops the first socket after a valid hello, with no answer. The
// page opens a second one, reaches ready, and a press sent by the test reaches
// the game.
func TestChromePageJoinsAfterItsFirstSocketIsDropped(t *testing.T) {
	page := buildPadPage(t, "gosx_padtest")
	srv, err := StartWith(context.Background(), Options{dropFirstHello: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	ctx := openPage(t, srv.PageURL(fileURL(page)))
	rc, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.WaitReady(rc); err != nil {
		t.Fatalf("WaitReady: %v (page status %q)", err, eval(t, ctx, statusJS))
	}
	srv.mu.Lock()
	dropped := srv.dropped
	srv.mu.Unlock()
	if !dropped {
		t.Fatal("the server never dropped a socket: the test proved nothing")
	}
	if got := eval(t, ctx, statusJS); got != "ready" {
		t.Fatalf("data-padtest %q, want ready", got)
	}
	run, cancelRun := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelRun()
	if _, err := srv.Plug(run, 0, "Stub A (STANDARD GAMEPAD)", "standard"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Press(run, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, ctx, titleJS); !strings.Contains(got, "held=[pad:0/button0]") {
		t.Fatalf("after Press the page saw %q", got)
	}
}
