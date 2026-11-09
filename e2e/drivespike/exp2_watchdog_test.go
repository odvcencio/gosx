//go:build e2e && drivespike

package drivespike

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/boot/bootpage"
	"m31labs.dev/gosx/game/kitui"
)

// watchdogServer serves the boot watchdog page of TestGameKitBrowserAdapters
// (e2e/game_kit_e2e_test.go startKitServer, mode=watchdog): the real bootpage
// watchdog script and kitui boot root with a 500 ms budget, and with ready=1 a
// 100 ms timer that marks the page ready. It counts document loads.
type watchdogServer struct {
	URL  string
	mu   sync.Mutex
	hits int
}

func (s *watchdogServer) Hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func startWatchdogServer(t *testing.T) *watchdogServer {
	srv := &watchdogServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		srv.mu.Lock()
		srv.hits++
		srv.mu.Unlock()
		head := gosx.RenderHTML(kitui.Styles()) + gosx.RenderHTML(kitui.Script()) + gosx.RenderHTML(bootpage.Watchdog())
		body := gosx.RenderHTML(kitui.BootRoot(kitui.LoadingProps{BudgetMS: 500}))
		if r.URL.Query().Get("ready") == "1" {
			body += `<script>setTimeout(()=>document.querySelector('[data-boot-root]').dataset.boot='ready',100)</script>`
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8">%s</head><body style="margin:0"><script>window.kitEvents=[]</script>%s</body></html>`, head, body)
	})
	srv.URL = serve(t, mux)
	return srv
}

type bootState struct {
	Boot   string
	Title  string
	Marker string
	V      float64
	Loads  int
	Docs   string
}

const bootExpr = `(() => { const r = document.querySelector('[data-boot-root]'); const s = __drive.stats();
  return { boot: r ? r.dataset.boot : '', title: (document.querySelector('[data-boot-title]')||{}).textContent || '',
    marker: Object.keys(sessionStorage).filter(k => k.startsWith('gosx-boot:')).map(k => k + '=' + sessionStorage.getItem(k)).join(),
    v: s.V, loads: s.loads, docs: s.docId }; })()`

const finalHashExpr = `(() => { const keys = Object.keys(sessionStorage).sort();
  return document.documentElement.outerHTML + '|' + keys.map(k => k + '=' + sessionStorage.getItem(k)).join(';') + '|' + __drive.now(); })()`

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func runsFromEnv(def int) int {
	if n, err := strconv.Atoi(os.Getenv("DRIVESPIKE_RUNS")); err == nil && n > 0 {
		return n
	}
	return def
}

// watchdogReady is the scripted "stays quiet when the page becomes ready" run.
// It returns the final DOM hash.
func watchdogReady(t *testing.T, res *results, b *spikeBrowser, srv *watchdogServer, realWait time.Duration) string {
	tb := b.newTab(t, time.Minute)
	defer tb.cancel()
	tb.addProbe(map[string]any{"wall": 1.7e12})
	before := srv.Hits()
	tb.navigate(srv.URL + "/app?mode=watchdog&ready=1")
	time.Sleep(realWait)
	var st bootState
	tb.eval(bootExpr, &st)
	res.quiet("ready: no timer runs before the clock moves", st.Boot == "loading" && st.V == 0 && srv.Hits()-before == 1, "state=%+v loads=%d", st, srv.Hits()-before)
	tb.step(99)
	tb.eval(bootExpr, &st)
	res.quiet("ready: still loading at 99 ms", st.Boot == "loading", "state=%+v", st)
	tb.step(1)
	tb.eval(bootExpr, &st)
	res.quiet("ready: ready at 100 ms", st.Boot == "ready" && st.V == 100, "state=%+v", st)
	tb.step(10_000)
	tb.eval(bootExpr, &st)
	res.quiet("ready: 10 s later no reload, no marker", st.Boot == "ready" && srv.Hits()-before == 1 && st.Marker == "" && st.Loads == 1 && st.V == 10_100,
		"state=%+v loads=%d", st, srv.Hits()-before)
	var dom string
	tb.eval(finalHashExpr, &dom)
	return hashString(dom)
}

// watchdogStall is the scripted "stall, one reload, failed, retry button" run.
func watchdogStall(t *testing.T, res *results, b *spikeBrowser, srv *watchdogServer, realWait time.Duration) string {
	tb := b.newTab(t, time.Minute)
	defer tb.cancel()
	tb.addProbe(map[string]any{"wall": 1.7e12})
	before := srv.Hits()
	loads := func() int { return srv.Hits() - before }
	tb.navigate(srv.URL + "/app?mode=watchdog")
	time.Sleep(realWait)
	var st bootState
	tb.eval(bootExpr, &st)
	res.quiet("stall: the watchdog cannot fire before the clock moves", st.Boot == "loading" && loads() == 1, "state=%+v loads=%d", st, loads())
	tb.step(499)
	tb.eval(bootExpr, &st)
	res.quiet("stall: loading at 499 ms", st.Boot == "loading" && loads() == 1, "state=%+v loads=%d", st, loads())
	tb.step(1)
	tb.eval(bootExpr, &st)
	res.quiet("stall: recovering at 500 ms, reload not yet", st.Boot == "recovering" && loads() == 1, "state=%+v loads=%d", st, loads())
	tb.step(249)
	res.quiet("stall: no reload at 749 ms", loads() == 1, "loads=%d", loads())
	r := tb.step(1)
	tb.eval(bootExpr, &st)
	res.quiet("stall: exactly one reload at 750 ms and the clock survives it", r.Navigations == 1 && loads() == 2 && st.Loads == 2 && st.V == 750 && st.Boot == "loading",
		"step=%s state=%+v loads=%d", jsonString(r), st, loads())
	tb.step(499)
	tb.eval(bootExpr, &st)
	res.quiet("stall: still loading 499 ms after the reload", st.Boot == "loading" && loads() == 2, "state=%+v loads=%d", st, loads())
	tb.step(1)
	tb.eval(bootExpr, &st)
	res.quiet("stall: failed at 1250 ms with the failed title", st.Boot == "failed" && st.Title == "Could not start" && loads() == 2, "state=%+v loads=%d", st, loads())
	r = tb.step(10_000)
	res.quiet("stall: 10 s later no second automatic reload", loads() == 2 && r.Navigations == 0, "loads=%d step=%s", loads(), jsonString(r))
	// Retry: the click handler reloads at once (input is not clocked); the next stall gets its reload again.
	var doc string
	tb.eval(`__drive.docId`, &doc)
	if err := chromedp.Run(tb.ctx, chromedp.Click(`[data-boot-retry]`, chromedp.ByQuery)); err != nil {
		res.quiet("stall: retry click", false, "%v", err)
		return ""
	}
	if !tb.waitFor(fmt.Sprintf(`!!window.__drive && __drive.docId !== %q && document.readyState !== "loading"`, doc), 10*time.Second) {
		res.quiet("stall: retry reload", false, "no new document")
		return ""
	}
	r = tb.step(750)
	r2 := tb.step(500)
	tb.eval(bootExpr, &st)
	res.quiet("stall: retry cycle reloads once more, then fails", loads() == 4 && st.Boot == "failed" && r.Navigations == 1 && r2.Navigations == 0 && st.V == 10_000+1250+1250,
		"loads=%d state=%+v step=%s", loads(), st, jsonString(r))
	var dom string
	tb.eval(finalHashExpr, &dom)
	return hashString(dom)
}

// Experiment 2: the boot watchdog page under the probe, each scripted run 50 times.
func TestExp2Watchdog(t *testing.T) {
	res := newResults(t, "2")
	defer res.summary()
	srv := startWatchdogServer(t)
	b := startBrowser(t)
	runs := runsFromEnv(50)
	realWait := 300 * time.Millisecond
	for _, kind := range []string{"ready", "stall"} {
		hashes := map[string]int{}
		start := time.Now()
		failsBefore := countFails(res)
		for i := 0; i < runs; i++ {
			var h string
			if kind == "ready" {
				h = watchdogReady(t, res, b, srv, realWait)
			} else {
				h = watchdogStall(t, res, b, srv, realWait)
			}
			hashes[h]++
		}
		fails := countFails(res) - failsBefore
		per := time.Since(start) / time.Duration(runs)
		res.check(kind+": all runs pass", fails == 0, "runs=%d failedChecks=%d perRun=%s (includes %s real wait) distinctFinalDOM=%d", runs, fails, per.Round(time.Millisecond), realWait, len(hashes))
	}
}

func countFails(r *results) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.n {
		n += c[1]
	}
	return n
}
