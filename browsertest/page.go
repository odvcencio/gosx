package browsertest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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
	// Step is the longest one Run may take, stretched by Slow. Zero means
	// DefaultStep; a negative Step leaves Run with the page's deadline only.
	// On a page made by NewPage a step also ends StepMargin before the page's
	// deadline, so the page can still be described when it fails.
	Step time.Duration
	// Shots is the folder for the picture a failure saves. Empty means the
	// BROWSERTEST_SHOTS environment variable, else browsertest-failures in
	// the temporary folder.
	Shots string

	started  time.Time
	budget   time.Duration
	deadline *time.Timer
	ends     time.Time // when deadline fires
	clock    *Clock    // the capture clock, made by Clock
}

// DefaultStep is the longest one Run may take when Page.Step is zero, before
// the Slow stretch.
const DefaultStep = 60 * time.Second

// StepMargin is how long before the page's deadline a step ends.
const StepMargin = 10 * time.Second

// lookLimit bounds the description and the picture a failure takes.
const lookLimit = 15 * time.Second

// NewPage drives tab with a deadline of budget, after which every action
// fails. The deadline is a timer that cancels a child context, not a
// WithTimeout, so Stretch can move it; the tab itself stays open.
func NewPage(t testing.TB, tab context.Context, budget time.Duration) *Page {
	ctx, cancel := context.WithCancel(tab)
	p := &Page{T: t, Ctx: ctx, Slow: 1, started: time.Now(), budget: budget}
	p.ends = p.started.Add(budget)
	p.deadline = time.AfterFunc(budget, cancel)
	t.Cleanup(func() { p.deadline.Stop(); cancel() })
	return p
}

// Stretch sets Slow and moves the deadline to budget times slow after the
// page was made.
func (p *Page) Stretch(slow float64) {
	p.Slow = slow
	if p.deadline != nil {
		total := time.Duration(float64(p.budget) * max(1, slow))
		p.ends = p.started.Add(total)
		p.deadline.Reset(total - time.Since(p.started))
	}
}

// stepLimit is how long the next Run may take; zero means no limit of its own.
func (p *Page) stepLimit() time.Duration {
	step := p.Step
	if step == 0 {
		step = DefaultStep
	}
	if step < 0 {
		return 0
	}
	limit := time.Duration(float64(step) * max(1, p.Slow))
	if p.deadline != nil {
		if left := time.Until(p.ends) - StepMargin; left < limit {
			limit = max(left, time.Second)
		}
	}
	return limit
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

// Run runs actions in order, each one at a time, within the step limit
// (Step). When one fails or the limit passes, it fails the test naming stage
// and the action it was on (a query's selector), with a description and a
// picture of the page.
func (p *Page) Run(stage string, actions ...chromedp.Action) {
	p.T.Helper()
	ctx, cancel := p.Ctx, context.CancelFunc(func() {})
	limit := p.stepLimit()
	if limit > 0 {
		ctx, cancel = context.WithTimeout(p.Ctx, limit)
	}
	defer cancel()
	flat := flatten(actions)
	for i, a := range flat {
		err := chromedp.Run(ctx, a)
		if err == nil {
			continue
		}
		at := Describe(a)
		if len(flat) > 1 {
			at = fmt.Sprintf("action %d of %d, %s", i+1, len(flat), at)
		}
		switch {
		case p.Ctx.Err() != nil:
			err = fmt.Errorf("the page's deadline passed: %w", err)
		case ctx.Err() != nil:
			err = fmt.Errorf("not done within the step limit of %v: %w", limit.Round(time.Second), err)
		}
		p.fail(a, "%s (%s): %v", stage, at, err)
	}
}

// flatten lists the actions of nested chromedp.Tasks in the order they run.
func flatten(actions []chromedp.Action) []chromedp.Action {
	var out []chromedp.Action
	for _, a := range actions {
		if tasks, ok := a.(chromedp.Tasks); ok {
			out = append(out, flatten(tasks)...)
			continue
		}
		out = append(out, a)
	}
	return out
}

// Describe names an action for a failure message: a query (a click, a wait,
// a text) by its selector, any other action by its type.
func Describe(a chromedp.Action) string {
	if sel, ok := querySelector(a); ok {
		return "query " + sel
	}
	return fmt.Sprintf("%T", a)
}

// querySelector is the selector of a chromedp query (a click, a wait, a
// text). chromedp keeps it unexported; TestDescribeNamesAQuerysSelector
// notices when that changes.
func querySelector(a chromedp.Action) (string, bool) {
	s, ok := a.(*chromedp.Selector)
	if !ok || s == nil {
		return "", false
	}
	v := reflect.ValueOf(s).Elem().FieldByName("sel")
	if !v.IsValid() || v.Kind() != reflect.Interface || v.Elem().Kind() != reflect.String {
		return "", false
	}
	return v.Elem().String(), true
}

// fail ends the test with msg, a description of the page and a picture of
// it. Both are taken on a context of their own, which outlives the page's
// deadline: after it p.Ctx is cancelled and shows nothing.
func (p *Page) fail(at chromedp.Action, format string, args ...any) {
	p.T.Helper()
	msg := fmt.Sprintf(format, args...)
	look, cancel := context.WithTimeout(context.WithoutCancel(p.Ctx), lookLimit)
	defer cancel()
	p.T.Fatalf("%s%s%s%s", msg, p.describe(), p.lookAt(look, at), p.picture(look))
}

func (p *Page) describe() string {
	if p.Snapshot == nil {
		return ""
	}
	return "\npage: " + p.Snapshot()
}

// lookJS describes the page and, when it is a CSS selector, what the failed
// query matched. It leaves out paths and query strings.
const lookJS = `((sel) => {
  const name = (e) => !e ? "none" : e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") +
    (typeof e.className === "string" && e.className.trim() ? "." + e.className.trim().split(/\s+/).join(".") : "");
  let out = "ready " + document.readyState + ", " + document.visibilityState + ", hash " + (location.hash || "none") +
    ", focus " + name(document.activeElement) + ", frames " + (window.__frames ?? "not counted");
  if (sel) {
    let all;
    try { all = document.querySelectorAll(sel); } catch (err) { return out + "; the query is not a CSS selector"; }
    out += "; the query matches " + all.length;
    if (all.length) {
      const e = all[0], r = e.getBoundingClientRect(), st = getComputedStyle(e);
      const shown = r.width > 0 && r.height > 0 && st.visibility !== "hidden" && st.display !== "none";
      const at = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      out += ", the first " + name(e) + (shown ? " shown" : " not shown") + (e.disabled || e.getAttribute("aria-disabled") === "true" ? ", disabled" : ", enabled") +
        ", " + Math.round(r.width) + "x" + Math.round(r.height) + " at " + Math.round(r.left) + "," + Math.round(r.top) +
        (at && at !== e && !e.contains(at) ? ", covered by " + name(at) : "");
    }
  }
  return out;
})(%s)`

// lookAt describes the page on ctx: its state, the focus, and what the failed
// action's selector matches.
func (p *Page) lookAt(ctx context.Context, at chromedp.Action) string {
	sel := "null"
	if q, ok := querySelector(at); ok {
		sel = strconv.Quote(q)
	}
	var s string
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(lookJS, sel), &s)); err != nil {
		return "\nlook: none (" + err.Error() + ")"
	}
	return "\nlook: " + s
}

// picture saves a picture of the viewport on ctx and names its file.
func (p *Page) picture(ctx context.Context) string {
	dir := p.Shots
	if dir == "" {
		dir = os.Getenv("BROWSERTEST_SHOTS")
	}
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "browsertest-failures")
	}
	var png []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&png)); err != nil {
		return "\npicture: none (" + err.Error() + ")"
	}
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == ' ' || r == ':' || r == '\\' {
			return '_'
		}
		return r
	}, p.T.Name()) + "-" + time.Now().Format("150405.000") + ".png"
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "\npicture: none (" + err.Error() + ")"
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		return "\npicture: none (" + err.Error() + ")"
	}
	return "\npicture: " + path
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
// it is not true within d (stretched by Slow), with a description and a
// picture of the page.
func (p *Page) WaitFor(stage, expr string, d time.Duration) {
	p.T.Helper()
	if !p.TryWait(expr, d) {
		p.fail(nil, "%s: condition never held within %v: %s", stage, d, expr)
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
