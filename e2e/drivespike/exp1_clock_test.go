//go:build e2e && drivespike

package drivespike

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type logEntry struct {
	Name   string
	Perf   float64
	Date   float64
	Origin float64
	V      *float64
}

type firstScript struct {
	Probe   bool
	Patched bool
	Shared  *bool
	Perf    float64
	Date    float64
	Origin  float64
	DocID   string
	Cross   bool
}

func testdataServer(t *testing.T) string {
	return serve(t, http.FileServer(http.Dir(filepath.Join(repoRoot(t), "e2e", "drivespike", "testdata"))))
}

// Experiment 1: probe install and the basic clock.
func TestExp1ProbeClock(t *testing.T) {
	res := newResults(t, "1")
	defer res.summary()
	url := testdataServer(t)
	b := startBrowser(t)
	tb := b.newTab(t, 2*time.Minute)
	wall := float64(time.Now().UnixMilli())
	tb.addProbe(map[string]any{"wall": wall})
	tb.navigate(url + "/clock.html")

	var first firstScript
	tb.eval(`window.first`, &first)
	res.check("probe runs before the first page script", first.Probe && first.Patched, "first=%s", jsonString(first))
	res.check("first script sees virtual time 0", first.Perf == 0 && first.Date == wall, "perf=%v date=%v wall=%v", first.Perf, first.Date, wall)

	var same firstScript
	tb.eval(`document.getElementById("same").contentWindow.first`, &same)
	res.check("probe present in a same-origin iframe", same.Probe, "child=%s", jsonString(same))
	res.check("same-origin iframe shares the top clock", same.Shared != nil && *same.Shared, "child=%s", jsonString(same))

	var childMsgs []map[string]any
	tb.waitFor(`childMsgs.some(m => m.first && m.first.cross)`, 5*time.Second)
	tb.eval(`childMsgs`, &childMsgs)
	var crossFirst map[string]any
	for _, m := range childMsgs {
		if f, ok := m["first"].(map[string]any); ok && f["cross"] == true {
			crossFirst = f
		}
	}
	if crossFirst == nil {
		t.Logf("RESULT exp=1 browser=%s check=%q NOTRUN the cross-site iframe did not load or post in 5 s; msgs=%s", browserLabel(), "probe present in a cross-site iframe", jsonString(childMsgs))
	} else {
		// Report, not judge: whether a cross-site iframe gets the probe depends on site isolation.
		t.Logf("RESULT exp=1 browser=%s check=%q INFO probe=%v shared=%v (cross-site frame; its own clock when not shared)", browserLabel(), "probe present in a cross-site iframe", crossFirst["probe"], crossFirst["shared"])
	}

	// Nothing advances until a step.
	time.Sleep(1000 * time.Millisecond)
	var idle struct {
		Log    []logEntry
		Rafs   []float64
		Perf   float64
		Date   float64
		V      float64
		Child  int
		Stamp  any
	}
	tb.eval(`({log, rafs, perf: performance.now(), date: Date.now(), v: __drive.now(), child: document.getElementById("same").contentWindow.log.length, stamp: window.stamp || null})`, &idle)
	res.check("nothing advances for 1 s of real time without a step", len(idle.Log) == 0 && len(idle.Rafs) == 0 && idle.Perf == 0 && idle.Date == wall && idle.V == 0 && idle.Child == 0,
		"log=%d rafs=%d perf=%v date-wall=%v V=%v child=%d", len(idle.Log), len(idle.Rafs), idle.Perf, idle.Date-wall, idle.V, idle.Child)

	r := tb.step(1000)
	t.Logf("step(1000): %s wall=%.1fms", jsonString(r), r.WallMS)
	var after struct {
		Log   []logEntry
		Rafs  []float64
		Stamp struct{ TS, Perf float64 }
		Child []struct{ Perf, Date, V float64 }
		Perf  float64
		Date  float64
		V     float64
	}
	tb.eval(`({log, rafs, stamp: window.stamp, child: document.getElementById("same").contentWindow.log, perf: performance.now(), date: Date.now(), v: __drive.now()})`, &after)
	var names []string
	var perfs []float64
	for _, e := range after.Log {
		names = append(names, e.Name)
		perfs = append(perfs, e.Perf)
	}
	wantNames := []string{"t0", "idle", "t100", "t250a", "t250b", "interval300", "t500", "t500+50", "interval300", "interval300", "t999", "t1000"}
	wantPerf := []float64{0, frameAt(1), 100, 250, 250, 300, 500, 550, 600, 900, 999, 1000}
	res.check("step 1000 ms runs exactly the due timers in due order", strings.Join(names, ",") == strings.Join(wantNames, ","), "got=%v want=%v", names, wantNames)
	okPerf := len(perfs) == len(wantPerf)
	for i := range perfs {
		if okPerf && !near(perfs[i], wantPerf[i], 1e-6) {
			okPerf = false
		}
	}
	res.check("each timer sees performance.now() equal to its due time", okPerf, "perf=%v want=%v", perfs, wantPerf)
	agree := true
	for _, e := range after.Log {
		if !near(e.Date, e.Origin+e.Perf, 1) || e.V == nil || !near(*e.V, e.Perf, 1e-6) || !near(e.Date, wall+e.Perf, 1) {
			agree = false
		}
	}
	res.check("Date.now() and performance.now() agree with the virtual clock", agree && after.V == 1000 && after.Perf == 1000 && after.Date == wall+1000,
		"V=%v perf=%v date-wall=%v entries=%s", after.V, after.Perf, after.Date-wall, jsonString(after.Log))
	okRaf := len(after.Rafs) == 60
	for i := range after.Rafs {
		if okRaf && !near(after.Rafs[i], frameAt(i+1), 1e-6) {
			okRaf = false
		}
	}
	res.check("animation frames: 60 in 1000 ms at virtual timestamps", okRaf, "n=%d first=%v last=%v", len(after.Rafs), first3(after.Rafs), last3(after.Rafs))
	res.check("Event.timeStamp reads the virtual clock", after.Stamp.TS == 700 && after.Stamp.Perf == 700, "stamp=%+v", after.Stamp)
	res.check("same-origin iframe timer runs on the shared clock", len(after.Child) == 1 && after.Child[0].Perf == 200 && after.Child[0].V == 200, "child=%s", jsonString(after.Child))

	// Reload: the probe is present again and the clock carries on from 1000.
	if err := chromedp.Run(tb.ctx, chromedp.Reload()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	var re struct {
		First firstScript
		Stats struct {
			Loads       int
			RestoredFrom *float64
			V           float64
		}
	}
	tb.eval(`({first, stats: __drive.stats()})`, &re)
	res.check("probe present again after a reload", re.First.Probe && re.First.Patched, "first=%s", jsonString(re.First))
	res.check("clock survives the reload", re.Stats.Loads == 2 && re.Stats.RestoredFrom != nil && *re.Stats.RestoredFrom == 1000 && re.Stats.V == 1000,
		"stats=%s", jsonString(re.Stats))
	res.check("new document: performance.now() restarts at 0, timeOrigin and Date carry on", re.First.Perf == 0 && near(re.First.Origin, wall+1000, 1e-6) && re.First.Date == wall+1000,
		"perf=%v origin-wall=%v date-wall=%v", re.First.Perf, re.First.Origin-wall, re.First.Date-wall)
	time.Sleep(300 * time.Millisecond)
	var n int
	tb.eval(`log.length + rafs.length`, &n)
	res.check("after reload nothing advances without a step", n == 0, "log+rafs=%d", n)
	tb.step(100)
	var re2 []logEntry
	tb.eval(`log`, &re2)
	ok := len(re2) >= 3 && re2[0].Name == "t0" && re2[0].Perf == 0 && re2[0].V != nil && *re2[0].V == 1000 && re2[len(re2)-1].Name == "t100" && re2[len(re2)-1].Perf == 100
	res.check("after reload the old document's timers are gone and new ones run from V=1000", ok, "log=%s", jsonString(re2))
	if errs := tb.Errors(); len(errs) > 0 {
		t.Logf("page errors: %v", errs)
	}
}

func first3(v []float64) []float64 {
	if len(v) > 3 {
		return v[:3]
	}
	return v
}

func last3(v []float64) []float64 {
	if len(v) > 3 {
		return v[len(v)-3:]
	}
	return v
}
