package browsertest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// FrameCounter is an init script that counts the frames the page draws once
// window.__countFrames() is called; window.__frames holds the count. Open
// installs it with OpenOptions.CountFrames, and Page.Frames reads it.
const FrameCounter = `(() => {
  window.__frames = 0;
  window.__countFrames = () => { const tick = () => { window.__frames++; requestAnimationFrame(tick); }; requestAnimationFrame(tick); };
})();`

// OpenOptions describe how Open loads a page.
type OpenOptions struct {
	// Width and Height are the viewport in CSS pixels; zero keeps the
	// browser's own.
	Width, Height int64
	// Mobile emulates a phone's viewport, Landscape turns it.
	Mobile, Landscape bool
	// Touch emulates a touch screen with this many touch points.
	Touch int64
	// Focus makes the page believe its window has the focus, so it applies
	// focus styles and gets focus events without a real window.
	Focus bool
	// Offline blocks every http and https request, so a page from a file
	// must run on what it carries.
	Offline bool
	// CountFrames installs FrameCounter before any page script runs.
	CountFrames bool
	// Scripts run in every new document before any page script.
	Scripts []string
}

// Open loads target, a URL or an absolute file path, with the options.
func Open(target string, o OpenOptions) chromedp.Tasks {
	if u, err := url.Parse(target); err != nil || u.Scheme == "" {
		target = (&url.URL{Scheme: "file", Path: target}).String()
	}
	var tasks chromedp.Tasks
	if o.Offline {
		tasks = append(tasks, network.Enable(), network.SetBlockedURLs().WithURLPatterns([]*network.BlockPattern{
			{URLPattern: "http://*:*/*", Block: true},
			{URLPattern: "https://*:*/*", Block: true},
		}))
	}
	if o.Width > 0 && o.Height > 0 {
		var emulate []chromedp.EmulateViewportOption
		if o.Mobile {
			emulate = append(emulate, chromedp.EmulateMobile)
		}
		if o.Landscape {
			emulate = append(emulate, chromedp.EmulateLandscape)
		}
		tasks = append(tasks, chromedp.EmulateViewport(o.Width, o.Height, emulate...))
	}
	if o.Touch > 0 {
		tasks = append(tasks, emulation.SetTouchEmulationEnabled(true).WithMaxTouchPoints(o.Touch))
	}
	if o.Focus {
		tasks = append(tasks, emulation.SetFocusEmulationEnabled(true))
	}
	scripts := o.Scripts
	if o.CountFrames {
		scripts = append([]string{FrameCounter}, scripts...)
	}
	for _, script := range scripts {
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
			return err
		}))
	}
	return append(tasks, chromedp.Navigate(target))
}

// Page drives one tab for a test: actions that fail the test with a picture
// of the page, and waits that stretch when the page draws slowly.
type Page struct {
	T   testing.TB
	Ctx context.Context
	// Slow is how many times slower than wanted the page draws; every wait
	// and the deadline stretch by it. Zero and one mean no stretch.
	Slow float64
	// Snapshot, when set, describes the page in failure messages.
	Snapshot func() string

	started  time.Time
	budget   time.Duration
	deadline *time.Timer
}

// NewPage drives tab with a deadline of budget, after which every action
// fails. The deadline is a timer that cancels a child context, not a
// WithTimeout, so Stretch can move it; the tab itself stays open.
func NewPage(t testing.TB, tab context.Context, budget time.Duration) *Page {
	ctx, cancel := context.WithCancel(tab)
	p := &Page{T: t, Ctx: ctx, Slow: 1, started: time.Now(), budget: budget}
	p.deadline = time.AfterFunc(budget, cancel)
	t.Cleanup(func() { p.deadline.Stop(); cancel() })
	return p
}

// Stretch sets Slow and moves the deadline to budget times slow after the
// page was made.
func (p *Page) Stretch(slow float64) {
	p.Slow = slow
	if p.deadline != nil {
		p.deadline.Reset(time.Duration(float64(p.budget)*max(1, slow)) - time.Since(p.started))
	}
}

// MeasureFrameRate counts the frames the page draws in one second (the page
// must have FrameCounter running) and, below want frames a second, stretches
// waits by want over the rate, at most ten times. It returns the rate.
func (p *Page) MeasureFrameRate(want int) int {
	f0 := p.Frames()
	time.Sleep(time.Second)
	fps := p.Frames() - f0
	if fps < want {
		slow := 10.0
		if fps > want/10 {
			slow = float64(want) / float64(fps)
		}
		p.Stretch(slow)
	}
	return fps
}

// Run runs actions, and fails the test naming stage when one fails.
func (p *Page) Run(stage string, actions ...chromedp.Action) {
	p.T.Helper()
	if err := chromedp.Run(p.Ctx, actions...); err != nil {
		p.T.Fatalf("%s: %v%s", stage, err, p.describe())
	}
}

func (p *Page) describe() string {
	if p.Snapshot == nil {
		return ""
	}
	return "\npage: " + p.Snapshot()
}

// Eval reports whether expr is true. An expression that fails is false.
func (p *Page) Eval(expr string) bool {
	var ok bool
	if err := chromedp.Run(p.Ctx, chromedp.Evaluate(expr, &ok)); err != nil {
		return false
	}
	return ok
}

// Text is the visible text of the first element matching selector, with runs
// of white space made one space, or "" when there is none.
func (p *Page) Text(selector string) string {
	var s string
	expr := fmt.Sprintf(`(() => { const e = document.querySelector(%q); return e ? (e.innerText || '').replace(/\s+/g, ' ').trim() : ''; })()`, selector)
	_ = chromedp.Run(p.Ctx, chromedp.Evaluate(expr, &s))
	return s
}

// WaitFor polls expr until it is true, and fails the test naming stage when
// it is not true within d (stretched by Slow).
func (p *Page) WaitFor(stage, expr string, d time.Duration) {
	p.T.Helper()
	if !p.TryWait(expr, d) {
		p.T.Fatalf("%s: condition never held within %v: %s%s", stage, d, expr, p.describe())
	}
}

// TryWait polls expr every 50 ms until it is true, for at most d stretched by
// Slow, and reports whether it held.
func (p *Page) TryWait(expr string, d time.Duration) bool {
	deadline := time.Now().Add(time.Duration(float64(d) * max(1, p.Slow)))
	for {
		if p.Eval(expr) {
			return true
		}
		if time.Now().After(deadline) || p.Ctx.Err() != nil {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Frames is how many frames the page has drawn since FrameCounter began.
func (p *Page) Frames() int {
	var n int
	_ = chromedp.Run(p.Ctx, chromedp.Evaluate(`window.__frames`, &n))
	return n
}

// WaitFrames waits until the page has drawn n more frames, for at most limit.
func (p *Page) WaitFrames(n int, limit time.Duration) {
	start, deadline := p.Frames(), time.Now().Add(limit)
	for p.Frames() < start+n && time.Now().Before(deadline) && p.Ctx.Err() == nil {
		time.Sleep(20 * time.Millisecond)
	}
}

// Screenshot saves a picture of the viewport as a PNG at path. A failure is
// logged, not fatal: pictures are evidence, not checks.
func (p *Page) Screenshot(path string) {
	var png []byte
	if err := chromedp.Run(p.Ctx, chromedp.CaptureScreenshot(&png)); err != nil {
		p.T.Logf("screenshot %s failed: %v", path, err)
		return
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		p.T.Logf("screenshot %s failed: %v", path, err)
	}
}
