package webrtc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/webrtc/internal/duel"
)

const browserPage = `<!doctype html><meta charset="utf-8"><title>netplay</title>
<script>
// Test scaffolding only: the standard Go WASM boot. A GoSX app gets this from
// the framework and writes no JavaScript.
const q = new URLSearchParams(location.search);
const load = src => new Promise((ok, bad) => { const s = document.createElement("script"); s.src = src; s.onload = ok; s.onerror = bad; document.head.appendChild(s); });
(async () => {
  if (q.get("binding") !== "0") await load("/bootstrap-feature-netplay.js");
  await load("/wasm_exec.js");
  const go = new Go();
  const r = await WebAssembly.instantiateStreaming(fetch("/peer.wasm"), go.importObject);
  go.run(r.instance);
})();
</script>`

type browserResult struct {
	Player    int               `json:"player"`
	Frames    int64             `json:"frames"`
	Mode      string            `json:"mode"`
	Modes     []string          `json:"modes"`
	Degraded  bool              `json:"degraded"`
	RTT       float64           `json:"rttMs"`
	MaxRTT    float64           `json:"maxRttMs"`
	Rollbacks int               `json:"rollbacks"`
	Sums      map[string]uint64 `json:"sums"`
	Err       string            `json:"err"`
}

type browserOutput struct {
	Result browserResult
	Raw    string   `json:"result"`
	UA     string   `json:"ua"`
	Error  string   `json:"error"`
	Log    []string `json:"log"`
}

// TestRealBrowser plays a match between a native Go peer (WebRTC) and a Go
// WASM peer in headless Chromium through this package's relay. It needs
// Chromium, Playwright in the repo's node_modules and a Go toolchain with
// GOOS=js, so it runs only with NETPLAY_BROWSER=1 (make test-netplay-browser).
func TestRealBrowser(t *testing.T) {
	if os.Getenv("NETPLAY_BROWSER") == "" {
		t.Skip("set NETPLAY_BROWSER=1 to run the real-browser test")
	}
	for _, sc := range []struct {
		name     string
		binding  bool
		fallback time.Duration
		wantMode string
	}{
		{"webrtc", true, 6 * time.Second, "webrtc"},
		// Without the netplay chunk the page has no WebRTC binding, so the
		// client degrades to the WebSocket. The timeout stays under the
		// 5 s stall timeout of the native peer that waits for the browser.
		{"fallback", false, 1500 * time.Millisecond, "websocket"},
	} {
		sc := sc
		t.Run(sc.name, func(t *testing.T) { realBrowserMatch(t, sc.name, sc.binding, sc.fallback, sc.wantMode) })
	}
}

func realBrowserMatch(t *testing.T, name string, binding bool, fallback time.Duration, wantMode string) {
	dir := t.TempDir()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "peer.wasm"), "./internal/browserpeer")
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the WASM peer: %v\n%s", err, out)
	}
	goroot := strings.TrimSpace(runCmd(t, "go", "env", "GOROOT"))
	copyFile(t, filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"), filepath.Join(dir, "wasm_exec.js"))
	copyFile(t, filepath.Join(repo, "client", "js", "bootstrap-feature-netplay.js"), filepath.Join(dir, "bootstrap-feature-netplay.js"))

	srv, err := NewServer(Config{ConfigureICE: loopbackICE, FallbackAfter: fallback})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := hub.New("netplay-browser")
	srv.AttachHub(h)
	var mu sync.Mutex
	var ids []string
	h.On("join", func(ctx *hub.Context) { mu.Lock(); ids = append(ids, ctx.Client.ID); mu.Unlock() })
	mux := http.NewServeMux()
	mux.Handle("/hub", h)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(browserPage))
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(r.URL.Path)))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/hub"

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	native, err := DialHub(ctx, wsURL, ClientConfig{Session: "native0", ConfigureICE: loopbackICE})
	if err != nil {
		t.Fatalf("native peer: %v", err)
	}
	defer native.Close()

	const frames = 300
	pageURL := ts.URL + "/?player=1&frames=" + strconv.Itoa(frames) + "&fallbackMs=" + strconv.Itoa(int(fallback/time.Millisecond)) + "&hub=" + wsURL
	if !binding {
		pageURL += "&binding=0"
	}
	outCh := make(chan []byte, 1)
	go func() {
		cmd := exec.CommandContext(ctx, "node", "internal/browserpeer/run.mjs", pageURL, "70000")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if len(out) == 0 {
			out, _ = json.Marshal(map[string]string{"error": fmt.Sprint(err, " ", stderr.String())})
		}
		outCh <- out
	}()

	var games [2]*duel.Game
	var sess [2]*netplay.Session[duel.Pad]
	sums := map[int64]uint64{}
	var transports [2]netplay.Transport
	var relay *netplay.Relay[duel.Pad]
	games[0] = duel.NewGame()
	nativeLink := func() *Link {
		mu.Lock()
		defer mu.Unlock()
		if len(ids) == 0 {
			return nil
		}
		return srv.Link(ids[0])
	}
	sess[0], err = netplay.NewSession(netplay.Config[duel.Pad]{
		Game: games[0], Codec: duel.Codec{}, Transport: native, LocalPlayer: 0, InputDelay: 2, Relay: true,
		OnChecksum: func(f int64, s uint64) { sums[f] = s },
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	var next time.Duration
	var maxRTT time.Duration
	var browserOut []byte
	finished := false
	deadline := time.Now().Add(85 * time.Second)
	for !finished {
		<-tick.C
		now := time.Since(start)
		if time.Now().After(deadline) {
			t.Fatal("match did not finish in time")
		}
		if relay == nil {
			mu.Lock()
			n := len(ids)
			mu.Unlock()
			if n >= 2 {
				transports = [2]netplay.Transport{nativeLink(), srv.Link(ids[1])}
				relay, err = netplay.NewRelay(netplay.RelayConfig[duel.Pad]{
					NewGame: func() netplay.Game[duel.Pad] { return duel.NewGame() },
					Codec:   duel.Codec{}, Peers: transports,
				})
				if err != nil {
					t.Fatal(err)
				}
				// The handshake time base starts now so the native peer does not race ahead.
				start = time.Now()
				next = 0
				now = 0
			}
		}
		if relay != nil {
			relay.Tick(now)
		}
		if now >= next && relay != nil {
			next += time.Second / 60
			if sess[0].Frame() < frames+30 {
				sess[0].Advance(now, duel.Input(7, 0, sess[0].Frame()))
				if r := sess[0].RTT(); r > maxRTT {
					maxRTT = r
				}
				if err := sess[0].Err(); err != nil {
					t.Fatalf("native session ended: %v", err)
				}
			}
		}
		select {
		case browserOut = <-outCh:
			finished = true
		default:
		}
	}

	var out browserOutput
	if err := json.Unmarshal(browserOut, &out); err != nil {
		t.Fatalf("browser output %q: %v", browserOut, err)
	}
	if out.Raw != "" {
		if err := json.Unmarshal([]byte(out.Raw), &out.Result); err != nil {
			t.Fatalf("browser result %q: %v", out.Raw, err)
		}
	}
	if out.Error != "" || out.Result.Err != "" {
		t.Fatalf("browser failed: %s %s\nlog: %v", out.Error, out.Result.Err, out.Log)
	}
	if out.Result.Mode != wantMode {
		t.Fatalf("browser ended in mode %q, want %q (modes %v)\nlog: %v", out.Result.Mode, wantMode, out.Result.Modes, out.Log)
	}
	if out.Result.Frames < frames {
		t.Fatalf("browser reached frame %d, want %d", out.Result.Frames, frames)
	}
	rep := relay.Report()
	if len(rep.DesyncFrames) != 0 || rep.Resyncs != 0 {
		t.Fatalf("desyncs %v resyncs %d", rep.DesyncFrames, rep.Resyncs)
	}
	common := 0
	for fs, bs := range out.Result.Sums {
		f, _ := strconv.ParseInt(fs, 10, 64)
		if ns, ok := sums[f]; ok {
			common++
			if ns != bs {
				t.Fatalf("checksum differs at frame %d", f)
			}
		}
	}
	if common < 5 {
		t.Fatalf("only %d common checksums", common)
	}
	t.Logf("%s on %s/%s: browser %q modes=%v frames=%d rtt=%.1f ms (max %.1f) rollbacks=%d | native rtt=%v max=%v | common checksums=%d relay verified=%d desyncs=%d | %s",
		name, runtime.GOOS, runtime.GOARCH, out.Result.Mode, out.Result.Modes, out.Result.Frames, out.Result.RTT, out.Result.MaxRTT, out.Result.Rollbacks,
		sess[0].RTT(), maxRTT, common, rep.VerifiedFrames, len(rep.DesyncFrames), out.UA)
}

func runCmd(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return string(out)
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
