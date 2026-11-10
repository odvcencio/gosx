//go:build e2e && drivespike

package drivespike

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/animation"
	"github.com/chromedp/chromedp"
)

type cssEvent struct {
	Type    string
	V       float64
	Elapsed float64
}

type cssState struct {
	TR, KF   float64
	Events   []cssEvent
	Timeline *float64
	Anims    []struct {
		Name  string
		T     *float64
		State string
		Rate  float64
	}
}

func (s cssState) has(kind string) (cssEvent, bool) {
	for _, e := range s.Events {
		if e.Type == kind {
			return e, true
		}
	}
	return cssEvent{}, false
}

// Experiment 3: CSS transitions and keyframe animations under the virtual clock.
// Mechanism "freeze" is the spec's: Animation.setPlaybackRate(0) freezes the
// document timeline and the probe adds each step to every animation's
// currentTime. Mechanism "pause" is the one alternative: no freeze; the probe
// pauses each animation, sets currentTime from the clock and calls finish().
func TestExp3CSS(t *testing.T) {
	url := testdataServer(t)
	b := startBrowser(t)
	modes := []string{"freeze"}
	if os.Getenv("DRIVESPIKE_CSS_ALT") == "1" {
		modes = append(modes, "pause") // the alternative, needed only if "freeze" fails
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			res := newResults(t, "3/"+mode)
			defer res.summary()
			tb := b.newTab(t, time.Minute)
			tb.addProbe(map[string]any{"cssSeek": true, "cssMode": mode})
			if mode == "freeze" {
				if err := chromedp.Run(tb.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					if err := animation.Enable().Do(ctx); err != nil {
						return err
					}
					return animation.SetPlaybackRate(0).Do(ctx)
				})); err != nil {
					t.Fatalf("Animation.setPlaybackRate(0): %v", err)
				}
			}
			tb.navigate(url + "/css.html")
			tb.eval(`startTransition()`, nil)
			time.Sleep(500 * time.Millisecond)
			var s0 cssState
			tb.eval(`cssState()`, &s0)
			_, trEnd := s0.has("transitionend")
			_, kfEnd := s0.has("animationend")
			res.check("frozen: 500 ms of real time moves neither animation", s0.TR == 0 && s0.KF == 0 && !trEnd && !kfEnd, "state=%s", jsonString(s0))

			tb.step(150)
			var s1 cssState
			tb.eval(`cssState()`, &s1)
			res.check("step 150 ms: transition at 150 of 300 px", near(s1.TR, 150, 0.5), "tr=%v", s1.TR)
			res.check("step 150 ms: keyframes at 150 of 1000 px", near(s1.KF, 150, 0.5), "kf=%v", s1.KF)

			tb.step(150)
			time.Sleep(200 * time.Millisecond) // events dispatch in a real rendering update
			var s2 cssState
			tb.eval(`cssState()`, &s2)
			e, ok := s2.has("transitionend")
			res.check("step to 300 ms: transition at its end", near(s2.TR, 300, 0.5), "tr=%v", s2.TR)
			res.check("transitionend fires when stepped past the end", ok, "event=%+v events=%s", e, jsonString(s2.Events))
			res.check("transitionend fires at the end instant (V=300)", ok && near(e.V, 300, 1e-6), "event=%+v", e)
			res.check("keyframes at 300 of 1000 px", near(s2.KF, 300, 0.5), "kf=%v", s2.KF)

			tb.step(700)
			time.Sleep(200 * time.Millisecond)
			var s3 cssState
			tb.eval(`cssState()`, &s3)
			e, ok = s3.has("animationend")
			res.check("step to 1000 ms: keyframes at the end", near(s3.KF, 1000, 0.5), "kf=%v", s3.KF)
			res.check("animationend fires when stepped past the end", ok, "event=%+v events=%s", e, jsonString(s3.Events))
			res.check("animationend fires at the end instant (V=1000)", ok && near(e.V, 1000, 1e-6), "event=%+v", e)
			t.Logf("final state: %s", jsonString(s3))

			// After the end: a real 500 ms must not change anything either.
			time.Sleep(500 * time.Millisecond)
			var s4 cssState
			tb.eval(`cssState()`, &s4)
			res.check("after the end nothing moves without a step", s4.TR == s3.TR && s4.KF == s3.KF && len(s4.Events) == len(s3.Events), "before=%s after=%s", jsonString(s3), jsonString(s4))
			if mode == "freeze" {
				// Does the frozen timeline survive a reload (a new document)?
				if err := chromedp.Run(tb.ctx, chromedp.Reload()); err != nil {
					t.Fatalf("reload: %v", err)
				}
				time.Sleep(500 * time.Millisecond)
				var s5 cssState
				tb.eval(`cssState()`, &s5)
				res.check("after a reload the timeline is still frozen", s5.KF == 0, "kf=%v timeline=%s", s5.KF, jsonString(s5.Timeline))
				tb.step(250)
				tb.eval(`cssState()`, &s5)
				res.check("after a reload stepping 250 ms moves the keyframes 250 px", near(s5.KF, 250, 0.5), "kf=%v", s5.KF)
			}
			if errs := tb.Errors(); len(errs) > 0 {
				t.Logf("page errors: %v", errs)
			}
		})
	}
}
