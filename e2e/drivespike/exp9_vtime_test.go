//go:build e2e && drivespike

package drivespike

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// startHeadedBrowser launches Chrome without --headless (Linux under xvfb-run).
func startHeadedBrowser(t *testing.T) *spikeBrowser {
	opts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(chromePath(t)),
		chromedp.NoFirstRun, chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.UserDataDir(t.TempDir()),
		chromedp.WindowSize(1000, 800),
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(t.Context(), opts...)
	ctx, cancel := chromedp.NewContext(alloc)
	t.Cleanup(func() { cancel(); cancelAlloc() })
	sb := &spikeBrowser{ctx: ctx}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, product, _, ua, _, err := browser.GetVersion().Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		sb.Product, sb.UA = product, ua
		return err
	})); err != nil {
		t.Fatalf("headed Chrome: %v", err)
	}
	t.Logf("CHROME browser=%s product=%q headlessUA=%v headed=true ua=%q", browserLabel(), sb.Product, strings.Contains(sb.UA, "Headless"), sb.UA)
	return sb
}

type vtab struct {
	*tab
	expired chan struct{}
}

func newVTab(t *testing.T, b *spikeBrowser) *vtab {
	tb := b.newTab(t, 2*time.Minute)
	v := &vtab{tab: tb, expired: make(chan struct{}, 16)}
	chromedp.ListenTarget(tb.ctx, func(ev any) {
		if _, ok := ev.(*emulation.EventVirtualTimeBudgetExpired); ok {
			v.expired <- struct{}{}
		}
	})
	return v
}

func (v *vtab) policy(p emulation.VirtualTimePolicy, budget float64) error {
	return chromedp.Run(v.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		a := emulation.SetVirtualTimePolicy(p)
		if budget > 0 {
			a = a.WithBudget(budget)
		}
		_, err := a.Do(ctx)
		return err
	}))
}

// advance grants budget ms of virtual time and waits for budgetExpired.
func (v *vtab) advance(budget float64) (time.Duration, bool) {
	for len(v.expired) > 0 {
		<-v.expired
	}
	start := time.Now()
	if err := v.policy(emulation.VirtualTimePolicyAdvance, budget); err != nil {
		v.t.Fatalf("advance: %v", err)
	}
	select {
	case <-v.expired:
		return time.Since(start), true
	case <-time.After(15 * time.Second):
		return time.Since(start), false
	}
}

// navigatePaused sets the pause policy and loads url, with a bound.
func (v *vtab) navigatePaused(url string) error {
	if err := v.policy(emulation.VirtualTimePolicyPause, 0); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	defer cancel()
	return chromedp.Run(ctx, chromedp.Navigate(url))
}

// Experiment 9: Chrome's virtual time (Emulation.setVirtualTimePolicy) on the
// fixtures of experiments 1, 3 and 5, without the probe.
func TestExp9VirtualTime(t *testing.T) {
	url := testdataServer(t)
	var b *spikeBrowser
	mode := "headless"
	if os.Getenv("DRIVESPIKE_HEADED") == "1" {
		b, mode = startHeadedBrowser(t), "headed"
	} else {
		b = startBrowser(t)
	}

	t.Run("timers-and-frames", func(t *testing.T) {
		res := newResults(t, "9/"+mode+"/clock")
		defer res.summary()
		v := newVTab(t, b)
		if err := v.navigatePaused(url + "/clock.html"); err != nil {
			res.check("page loads while virtual time is paused", false, "%v", err)
			return
		}
		res.check("page loads while virtual time is paused", true, "")
		type st struct {
			Log        []logEntry
			Rafs       int
			Perf, Date float64
			RealPerf   float64
		}
		read := func() st {
			var s st
			v.eval(`({log, rafs: rafs.length, perf: performance.now(), date: Date.now()})`, &s)
			return s
		}
		s0 := read()
		time.Sleep(1000 * time.Millisecond)
		s1 := read()
		res.check("paused: no timer runs in 1 s of real time", len(s1.Log) == len(s0.Log), "before=%d after=%d", len(s0.Log), len(s1.Log))
		res.check("paused: performance.now and Date stay still", s1.Perf == s0.Perf && s1.Date == s0.Date, "perf %v->%v date %v->%v", s0.Perf, s1.Perf, s0.Date, s1.Date)
		res.check("paused: no animation frame runs in 1 s of real time", s1.Rafs == s0.Rafs, "rafs %d->%d", s0.Rafs, s1.Rafs)
		d, ok := v.advance(1000)
		s2 := read()
		res.check("advance budget 1000: budget expires", ok, "after %s real", d.Round(time.Millisecond))
		var names []string
		for _, e := range s2.Log[len(s1.Log):] {
			names = append(names, e.Name)
		}
		res.check("advance budget 1000: performance.now and Date move by about 1000", near(s2.Perf-s1.Perf, 1000, 20) && near(s2.Date-s1.Date, 1000, 20), "perf +%.3f date +%.0f", s2.Perf-s1.Perf, s2.Date-s1.Date)
		t.Logf("RESULT exp=9/%s/clock browser=%s check=%q INFO timers run=%v rafs during the budget=%d (60 expected for 1000 ms of frames) realTime=%s", mode, browserLabel(), "what the budget ran", names, s2.Rafs-s1.Rafs, d.Round(time.Millisecond))
		res.check("advance budget 1000: animation frames advance with virtual time", s2.Rafs-s1.Rafs >= 55 && s2.Rafs-s1.Rafs <= 61, "rafs +%d", s2.Rafs-s1.Rafs)
		time.Sleep(500 * time.Millisecond)
		s3 := read()
		res.check("paused again after the budget", len(s3.Log) == len(s2.Log) && s3.Rafs == s2.Rafs && s3.Perf == s2.Perf, "log %d->%d rafs %d->%d perf %v->%v", len(s2.Log), len(s3.Log), s2.Rafs, s3.Rafs, s2.Perf, s3.Perf)
	})

	t.Run("css", func(t *testing.T) {
		res := newResults(t, "9/"+mode+"/css")
		defer res.summary()
		v := newVTab(t, b)
		if err := v.navigatePaused(url + "/css.html"); err != nil {
			res.check("page loads while virtual time is paused", false, "%v", err)
			return
		}
		v.eval(`startTransition()`, nil)
		var s0, s1, s2, s3 cssState
		v.eval(`cssState()`, &s0)
		time.Sleep(500 * time.Millisecond)
		v.eval(`cssState()`, &s1)
		res.check("paused: CSS animations stay still in 500 ms of real time", s1.TR == s0.TR && s1.KF == s0.KF, "tr %v->%v kf %v->%v", s0.TR, s1.TR, s0.KF, s1.KF)
		_, ok := v.advance(150)
		v.eval(`cssState()`, &s2)
		res.check("advance 150: transition and keyframes move by about 150 ms", ok && near(s2.TR-s1.TR, 150, 20) && near(s2.KF-s1.KF, 150, 20), "tr %v->%v kf %v->%v", s1.TR, s2.TR, s1.KF, s2.KF)
		_, ok2 := v.advance(1000)
		v.eval(`cssState()`, &s3)
		_, te := s3.has("transitionend")
		_, ae := s3.has("animationend")
		res.check("advance past the ends: transitionend and animationend fire", ok2 && te && ae, "tr=%v kf=%v events=%s", s3.TR, s3.KF, jsonString(s3.Events))
	})

	t.Run("worker", func(t *testing.T) {
		res := newResults(t, "9/"+mode+"/worker")
		defer res.summary()
		v := newVTab(t, b)
		if err := v.navigatePaused(url + "/worker.html"); err != nil {
			res.check("page loads while virtual time is paused", false, "%v", err)
			return
		}
		v.eval(`startWorker()`, nil)
		time.Sleep(1000 * time.Millisecond)
		var log []wmsg
		v.eval(`wlog`, &log)
		var kinds []string
		for _, m := range log {
			kinds = append(kinds, m.Kind)
		}
		res.check("paused: worker timers do not run in 1 s of real time", len(log) <= 1, "messages=%v", kinds)
		_, ok := v.advance(300)
		time.Sleep(200 * time.Millisecond)
		v.eval(`wlog`, &log)
		kinds = kinds[:0]
		for _, m := range log {
			kinds = append(kinds, m.Kind)
		}
		t.Logf("RESULT exp=9/%s/worker browser=%s check=%q INFO budgetExpired=%v messages after advance(300)=%v", mode, browserLabel(), "worker under a budget", ok, kinds)
		if len(log) > 1 {
			f := log[0].First
			last := log[len(log)-1]
			t.Logf("worker first=%s last=%s", jsonString(f), jsonString(last))
		}
	})
}
