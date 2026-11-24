package browsertest

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// ClockBound is how long Clock.Step waits for the page, in wall time, when
// Clock.Bound is zero. It is not stretched by Page.Slow: a page on the host's
// clock does not depend on how fast it draws.
const ClockBound = 30 * time.Second

// whileFrames is how many frames StepWhile grants before it gives up on a
// change that the page does not answer.
const whileFrames = 20

// whileLate is how long StepWhile waits for a change to return after each
// granted frame before it grants the next one.
var whileLate = 3 * time.Second

// Clock is the host side of the capture clock of a page built with the
// gosx_capture tag (GoSX game/loop). The host writes data-gosx-capture-step
// (milliseconds of game time per granted frame; 0 gives the clock back) and
// data-gosx-capture-grant (frames granted in total) on the document element,
// and the page writes data-gosx-capture-done once it has stepped and drawn the
// granted frames. A check that steps a Clock gives the same result on a loaded
// machine, because the game advances by frames the check grants and not by the
// time the machine allows.
//
// A Clock is made by Page.Clock and is used from one goroutine at a time.
// Its methods return errors; they do not fail the test, so a check can add its
// own description of the page.
type Clock struct {
	// Bound is how long Step waits, in wall time, for the page to step the
	// frames it was granted. Zero means ClockBound.
	Bound time.Duration

	p       *Page
	granted uint64
}

// Clock returns the page's capture clock. A page has one; it keeps the running
// total of frames granted.
func (p *Page) Clock() *Clock {
	if p.clock == nil {
		p.clock = &Clock{p: p}
	}
	return p.clock
}

// Take puts the game on the host's clock: from now on it advances stepMS of
// game time for each frame granted with Step. It resets the grant to zero.
func (c *Clock) Take(stepMS float64) error {
	if stepMS <= 0 {
		return fmt.Errorf("capture clock: the step must be above zero milliseconds, got %v", stepMS)
	}
	c.granted = 0
	if err := c.write("data-gosx-capture-grant", "0"); err != nil {
		return err
	}
	return c.write("data-gosx-capture-step", strconv.FormatFloat(stepMS, 'f', -1, 64))
}

// Release gives the clock back to the page, which runs on its own time again.
func (c *Clock) Release() error {
	return c.write("data-gosx-capture-step", "0")
}

// Granted is how many frames the clock has granted since Take.
func (c *Clock) Granted() uint64 { return c.granted }

// Step grants n frames and returns when the page has stepped and drawn them.
// It fails when the page does not reach the grant within Bound, and says what
// was granted, what the page last reported and how long it waited.
func (c *Clock) Step(n int) error {
	if n < 0 {
		return fmt.Errorf("capture clock: cannot grant %d frames", n)
	}
	c.granted += uint64(n)
	want := strconv.FormatUint(c.granted, 10)
	if err := c.write("data-gosx-capture-grant", want); err != nil {
		return err
	}
	bound := c.Bound
	if bound == 0 {
		bound = ClockBound
	}
	start := time.Now()
	last := "none"
	for {
		var done string
		if err := chromedp.Run(c.p.Ctx, chromedp.Evaluate(`document.documentElement.getAttribute("data-gosx-capture-done") ?? ""`, &done)); err == nil && done != "" {
			last = done
		}
		if done == want || (done == "" && c.granted == 0) {
			return nil
		}
		if err := c.p.Ctx.Err(); err != nil {
			return fmt.Errorf("capture clock: granted %d frames, the page reported %s done, and the page ended after %v: %w", c.granted, last, time.Since(start).Round(time.Millisecond), err)
		}
		if time.Since(start) >= bound {
			return fmt.Errorf("capture clock: granted %d frames, the page reported %s done after %v (is the page built with the gosx_capture tag, and has Take run?)", c.granted, last, time.Since(start).Round(time.Millisecond))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// StepUntil grants one frame at a time until the JavaScript condition cond is
// true, and returns how many frames it granted. It checks cond before each
// frame, so a condition that already holds costs none. It fails when cond is
// still false after max frames.
func (c *Clock) StepUntil(cond string, max int) (int, error) {
	for n := 0; ; n++ {
		if c.p.Eval(cond) {
			return n, nil
		}
		if n >= max {
			return n, fmt.Errorf("capture clock: the condition is still false after max %d frames: %s", max, cond)
		}
		if err := c.Step(1); err != nil {
			return n, err
		}
	}
}

// StepWhile runs change, such as a pad press that the page answers on a frame,
// and grants frames one at a time until it returns, so the page reads the
// change on the first frame granted after it was sent. It fails after 20
// frames without an answer. After a failure change may still be running.
func (c *Clock) StepWhile(change func()) error {
	done := make(chan struct{})
	go func() { defer close(done); change() }()
	for n := 0; n < whileFrames; n++ {
		if err := c.Step(1); err != nil {
			return err
		}
		select {
		case <-done:
			return nil
		case <-time.After(whileLate):
			// The answer is late, not missing: the next grant is the second
			// frame the change may wait for.
		}
	}
	return fmt.Errorf("capture clock: the change was not answered in %d granted frames", whileFrames)
}

func (c *Clock) write(attr, value string) error {
	js := fmt.Sprintf(`document.documentElement.setAttribute(%q, %q)`, attr, value)
	if err := chromedp.Run(c.p.Ctx, chromedp.Evaluate(js, nil)); err != nil {
		return fmt.Errorf("capture clock: write %s=%s: %w", attr, value, err)
	}
	return nil
}

// drawnJS reads how many frames the page has drawn. It uses FrameCounter's
// count when the page has one, and else installs a counter of its own at the
// first call that counts from then on.
const drawnJS = `(() => {
  if (typeof window.__frames === "number") return window.__frames;
  if (typeof window.__gosxDrawn !== "number") {
    window.__gosxDrawn = 0;
    const tick = () => { window.__gosxDrawn++; requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
  }
  return window.__gosxDrawn;
})()`

// WaitFramesUntil is the wait for a page that runs on its own time, paced by
// the frames the page draws and not by seconds. It polls the JavaScript
// condition cond and returns when it is true. It fails when the page has drawn
// frames frames since the call and cond is still false; what names the wait in
// the error. If the page draws no frame, it does not fail before wall has
// passed since the last frame, and then says the page stalled, with the frame
// count. It counts FrameCounter's frames when the page has them (the page must
// have called window.__countFrames()), and else installs a counter of its own.
func (p *Page) WaitFramesUntil(what, cond string, frames int, wall time.Duration) error {
	count := func() (int, bool) {
		var n int
		if err := chromedp.Run(p.Ctx, chromedp.Evaluate(drawnJS, &n)); err != nil {
			return 0, false
		}
		return n, true
	}
	first, _ := count()
	last, moved := first, time.Now()
	for {
		n, ok := count()
		if ok && n != last {
			last, moved = n, time.Now()
		}
		if p.Eval(cond) {
			return nil
		}
		if err := p.Ctx.Err(); err != nil {
			return fmt.Errorf("%s: the page ended after %d frames: %w", what, last-first, err)
		}
		if last-first >= frames {
			return fmt.Errorf("%s: the condition is still false after %d frames: %s", what, last-first, strings.TrimSpace(cond))
		}
		if time.Since(moved) >= wall {
			return fmt.Errorf("%s: the page stalled: it drew %d of %d frames and none in the last %v: %s", what, last-first, frames, wall, strings.TrimSpace(cond))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
