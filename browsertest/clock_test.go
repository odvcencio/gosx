package browsertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// clockFixture is the page side of the capture clock in a few lines: on each
// frame, while the step is above zero and the grant is ahead, it steps one
// frame (window.ticks) and reports it in data-gosx-capture-done. The hash
// picks what the page does:
//
//	#stall    never reports a frame done
//	#drawing  starts FrameCounter
//
// Without #drawing the page's frame counter stays at zero, as a page whose
// frames do not run.
const clockFixture = `<!doctype html><body><script>
const root = document.documentElement;
window.ticks = 0;
let done = 0;
function tick() {
  const step = parseFloat(root.getAttribute("data-gosx-capture-step") || "0");
  const grant = parseInt(root.getAttribute("data-gosx-capture-grant") || "0", 10);
  if (step > 0 && location.hash !== "#stall") {
    if (done > grant) done = grant;
    if (done < grant) { done++; window.ticks++; root.setAttribute("data-gosx-capture-done", String(done)); }
  }
  requestAnimationFrame(tick);
}
requestAnimationFrame(tick);
if (location.hash === "#drawing") window.__countFrames();
</script></body>`

// clockPage opens clockFixture at hash in a real Chrome (GOSX_CHROME_BIN), or
// in the GPU host's browser (GPU_CHROME_ENDPOINT).
func clockPage(t *testing.T, hash string) *Page {
	t.Helper()
	var opts Options
	switch chrome, endpoint := strings.TrimSpace(os.Getenv("GOSX_CHROME_BIN")), strings.TrimSpace(os.Getenv("GPU_CHROME_ENDPOINT")); {
	case chrome != "":
		opts = Options{Chrome: chrome, Flags: []string{"--no-sandbox", "--disable-gpu"}}
	case endpoint != "":
		opts = Options{Endpoint: endpoint}
	default:
		t.Skip("set GOSX_CHROME_BIN or GPU_CHROME_ENDPOINT to run the clock checks in a real Chrome")
	}
	html := filepath.Join(t.TempDir(), "clock.html")
	if err := os.WriteFile(html, []byte(clockFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	tab := Launch(t, opts)
	p := NewPage(t, tab, 2*time.Minute)
	p.Shots = t.TempDir()
	if err := chromedp.Run(p.Ctx, Open("file://"+html+hash, OpenOptions{Width: 320, Height: 240, CountFrames: true})...); err != nil {
		t.Fatalf("open the page: %v", err)
	}
	return p
}

func attr(p *Page, name string) string {
	var s string
	_ = chromedp.Run(p.Ctx, chromedp.Evaluate(`document.documentElement.getAttribute(`+"`"+name+"`"+`) ?? "absent"`, &s))
	return s
}

func TestClockGrantsFramesAndWaitsForDone(t *testing.T) {
	p := clockPage(t, "")
	c := p.Clock()
	if c != p.Clock() {
		t.Fatal("a page has more than one clock")
	}
	if err := c.Take(1000.0 / 30); err != nil {
		t.Fatal(err)
	}
	if got := attr(p, "data-gosx-capture-step"); got != "33.333333333333336" {
		t.Errorf("step attribute = %q", got)
	}
	if got := attr(p, "data-gosx-capture-grant"); got != "0" {
		t.Errorf("grant attribute after Take = %q, want 0", got)
	}
	if err := c.Step(3); err != nil {
		t.Fatal(err)
	}
	if got := attr(p, "data-gosx-capture-done"); got != "3" {
		t.Errorf("done after Step(3) = %q, want 3", got)
	}
	if err := c.Step(2); err != nil {
		t.Fatal(err)
	}
	if got := attr(p, "data-gosx-capture-done"); got != "5" {
		t.Errorf("done after Step(2) = %q, want 5", got)
	}
	var ticks int
	if err := chromedp.Run(p.Ctx, chromedp.Evaluate(`window.ticks`, &ticks)); err != nil || ticks != 5 {
		t.Errorf("the page stepped %d frames (%v), want 5", ticks, err)
	}
	if c.Granted() != 5 {
		t.Errorf("Granted = %d, want 5", c.Granted())
	}
}

func TestClockStepFailsWhenThePageNeverSteps(t *testing.T) {
	p := clockPage(t, "#stall")
	c := p.Clock()
	c.Bound = 700 * time.Millisecond
	if err := c.Take(16); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := c.Step(1)
	if err == nil {
		t.Fatal("Step succeeded on a page that never steps")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("Step failed after %v, want about its bound of 700ms", took)
	}
	for _, want := range []string{"granted 1 frames", "reported none done"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q: %v", want, err)
		}
	}
}

func TestClockStepUntilStopsAtMax(t *testing.T) {
	p := clockPage(t, "")
	c := p.Clock()
	if err := c.Take(16); err != nil {
		t.Fatal(err)
	}
	n, err := c.StepUntil(`window.ticks >= 4`, 10)
	if err != nil || n != 4 {
		t.Fatalf("StepUntil = %d, %v; want 4, nil", n, err)
	}
	if n, err := c.StepUntil(`window.ticks >= 4`, 10); err != nil || n != 0 {
		t.Errorf("a condition that already holds granted %d frames (%v), want 0", n, err)
	}
	n, err = c.StepUntil(`false`, 3)
	if err == nil || n != 3 || !strings.Contains(err.Error(), "max 3 frames") {
		t.Fatalf("StepUntil(false, 3) = %d, %v; want 3 and an error naming max 3 frames", n, err)
	}
	if c.Granted() != 7 {
		t.Errorf("Granted = %d, want 7 (4 + 3)", c.Granted())
	}
}

func TestClockReleaseGivesTheClockBack(t *testing.T) {
	p := clockPage(t, "")
	c := p.Clock()
	if err := c.Take(16); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(); err != nil {
		t.Fatal(err)
	}
	if got := attr(p, "data-gosx-capture-step"); got != "0" {
		t.Errorf("step attribute after Release = %q, want 0", got)
	}
	if err := c.Take(0); err == nil {
		t.Error("Take(0) was accepted; 0 gives the clock back")
	}
}

func TestClockStepWhileGrantsFramesUntilTheChangeReturns(t *testing.T) {
	defer func(old time.Duration) { whileLate = old }(whileLate)
	whileLate = 150 * time.Millisecond
	p := clockPage(t, "")
	c := p.Clock()
	if err := c.Take(16); err != nil {
		t.Fatal(err)
	}
	// A change answered on the second frame granted after it starts.
	answered := make(chan struct{})
	go func() {
		for p.Eval(`window.ticks < 2`) {
			time.Sleep(5 * time.Millisecond)
		}
		close(answered)
	}()
	if err := c.StepWhile(func() { <-answered }); err != nil {
		t.Fatal(err)
	}
	if c.Granted() != 2 {
		t.Errorf("Granted = %d, want 2", c.Granted())
	}
	if err := c.StepWhile(func() { select {} }); err == nil || !strings.Contains(err.Error(), "20 granted frames") {
		t.Errorf("a change that never returns: %v", err)
	}
}

func TestWaitFramesSparesAStalledPage(t *testing.T) {
	stalled := clockPage(t, "")
	start := time.Now()
	err := stalled.WaitFramesUntil("a stalled page", "false", 10, 2*time.Second)
	took := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "the page stalled") || !strings.Contains(err.Error(), "drew 0 of 10 frames") {
		t.Fatalf("a stalled page: %v", err)
	}
	if took < 2*time.Second {
		t.Errorf("failed after %v, before the wall bound of 2s", took)
	}

	drawing := clockPage(t, "#drawing")
	start = time.Now()
	err = drawing.WaitFramesUntil("a drawing page", "false", 10, 20*time.Second)
	if err == nil || !strings.Contains(err.Error(), "still false after") || strings.Contains(err.Error(), "stalled") {
		t.Fatalf("a drawing page: %v", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("failed after %v, want about 10 frames", took)
	}

	// With no FrameCounter the wait counts frames of its own.
	own := clockPage(t, "")
	own.Run("remove the counter", chromedp.Evaluate(`delete window.__frames`, nil))
	if err := own.WaitFramesUntil("a page with no counter", "false", 5, 20*time.Second); err == nil || !strings.Contains(err.Error(), "still false after") {
		t.Errorf("a page with no counter: %v", err)
	}
}

func TestWaitFramesReturnsWhenTheConditionHolds(t *testing.T) {
	p := clockPage(t, "#drawing")
	start := time.Now()
	if err := p.WaitFramesUntil("a condition that holds", "true", 100000, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %v for a condition that already held", took)
	}
	if err := p.WaitFramesUntil("a condition that turns true", "window.__frames > 3", 100000, 20*time.Second); err != nil {
		t.Fatal(err)
	}
}
