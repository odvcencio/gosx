package browsertest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestOpenAddsOnlyTheEmulationAsked(t *testing.T) {
	if got := len(Open("/tmp/game.html", OpenOptions{})); got != 1 {
		t.Fatalf("plain Open has %d tasks, want 1 (navigate)", got)
	}
	// Offline (two), viewport, touch, focus, the frame counter and one
	// script, then navigate.
	all := Open("/tmp/game.html", OpenOptions{Width: 800, Height: 600, Mobile: true, Touch: 5, Focus: true, Offline: true, CountFrames: true, Scripts: []string{"1"}})
	if got := len(all); got != 8 {
		t.Fatalf("full Open has %d tasks, want 8", got)
	}
	if _, ok := all[len(all)-1].(chromedp.NavigateAction); !ok {
		t.Fatalf("last task is %T, want the navigation", all[len(all)-1])
	}
}

func TestFindChromeFollowsChromePath(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHROME_PATH", exe)
	if got, err := FindChrome(); err != nil || got != exe {
		t.Fatalf("FindChrome() = %q, %v; want %q", got, err, exe)
	}
	t.Setenv("CHROME_PATH", "/nonexistent/chrome")
	if _, err := FindChrome(); err == nil {
		t.Fatal("FindChrome accepted a CHROME_PATH that is not executable")
	}
}

func TestDescribeNamesAQuerysSelector(t *testing.T) {
	for _, tc := range []struct {
		a    chromedp.Action
		want string
	}{
		{chromedp.Click(`.choice[data-call="2"]`, chromedp.ByQuery, chromedp.NodeEnabled), `query .choice[data-call="2"]`},
		{chromedp.WaitVisible("#repeat-snap", chromedp.ByQuery), "query #repeat-snap"},
		{chromedp.Navigate("about:blank"), "chromedp.ActionFunc"},
		{otherAction{}, "browsertest.otherAction"},
	} {
		if got := Describe(tc.a); got != tc.want {
			t.Errorf("Describe(%T) = %q, want %q", tc.a, got, tc.want)
		}
	}
}

// otherAction is an action of a type of its own.
type otherAction struct{}

func (otherAction) Do(context.Context) error { return nil }

func TestFlattenListsNestedTasksInOrder(t *testing.T) {
	a, b, c := chromedp.Click("#a"), chromedp.Click("#b"), chromedp.Click("#c")
	got := flatten([]chromedp.Action{a, chromedp.Tasks{b, chromedp.Tasks{c}}})
	if len(got) != 3 || Describe(got[0]) != "query #a" || Describe(got[1]) != "query #b" || Describe(got[2]) != "query #c" {
		t.Fatalf("flatten gave %d actions: %v", len(got), got)
	}
}

func TestStepLimit(t *testing.T) {
	near := func(got, want time.Duration) bool { return got > want-time.Second && got <= want }
	p := NewPage(t, t.Context(), 100*time.Second)
	if got := p.stepLimit(); got != DefaultStep {
		t.Errorf("default step on a 100 s page = %v, want %v", got, DefaultStep)
	}
	p.Stretch(2)
	if got := p.stepLimit(); got != 2*DefaultStep {
		t.Errorf("step on a page twice too slow = %v, want %v", got, 2*DefaultStep)
	}
	p.Step = -1
	if got := p.stepLimit(); got != 0 {
		t.Errorf("a negative Step gives a limit of %v, want none", got)
	}
	short := NewPage(t, t.Context(), 30*time.Second)
	if got := short.stepLimit(); !near(got, 30*time.Second-StepMargin) {
		t.Errorf("step on a 30 s page = %v, want it to end %v before the page's deadline", got, StepMargin)
	}
	literal := &Page{T: t, Ctx: t.Context(), Slow: 3, Step: 5 * time.Second}
	if got := literal.stepLimit(); got != 15*time.Second {
		t.Errorf("step on a literal page 3 times too slow = %v, want 15s", got)
	}
}

// fatalCatcher stands in for the test so a check can read a failure.
type fatalCatcher struct {
	testing.TB
	msg string
}

func (f *fatalCatcher) Helper() {}
func (f *fatalCatcher) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// caught runs check as a test would and returns how it failed.
func caught(f *fatalCatcher, check func()) string {
	done := make(chan struct{})
	go func() { defer close(done); check() }()
	<-done
	return f.msg
}

// realPage opens a page with one hidden, disabled button, in the Chrome named
// by GOSX_CHROME_BIN.
func realPage(t *testing.T, budget time.Duration) (*Page, *fatalCatcher) {
	chrome := strings.TrimSpace(os.Getenv("GOSX_CHROME_BIN"))
	if chrome == "" {
		t.Skip("set GOSX_CHROME_BIN to run the failure checks in a real Chrome")
	}
	html := filepath.Join(t.TempDir(), "stuck.html")
	if err := os.WriteFile(html, []byte(`<!doctype html><body><button id="go" class="big" hidden disabled>Snap</button></body>`), 0o644); err != nil {
		t.Fatal(err)
	}
	tab := Launch(t, Options{Chrome: chrome, Flags: []string{"--no-sandbox", "--disable-gpu"}})
	f := &fatalCatcher{TB: t}
	p := NewPage(f, tab, budget)
	p.Shots = t.TempDir()
	if err := chromedp.Run(p.Ctx, Open(html, OpenOptions{Width: 640, Height: 480, CountFrames: true})...); err != nil {
		t.Fatalf("open the page: %v", err)
	}
	return p, f
}

func checkPicture(t *testing.T, msg string) {
	t.Helper()
	i := strings.Index(msg, "\npicture: ")
	if i < 0 {
		t.Fatalf("the failure names no picture:\n%s", msg)
	}
	path := strings.TrimSpace(strings.SplitN(msg[i+len("\npicture: "):], "\n", 2)[0])
	png, err := os.ReadFile(path)
	if err != nil || len(png) < 8 || string(png[1:4]) != "PNG" {
		t.Fatalf("the picture %q is not a PNG (%v)", path, err)
	}
}

func TestRunStopsAStuckStepAndNamesIt(t *testing.T) {
	p, f := realPage(t, 60*time.Second)
	p.Step = 2 * time.Second
	start := time.Now()
	msg := caught(f, func() {
		p.Run("press a button that never comes", chromedp.Click("#later", chromedp.ByQuery))
	})
	if msg == "" {
		t.Fatal("the stuck step did not fail")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the stuck step failed after %v, want about its 2 s limit", took)
	}
	for _, want := range []string{"press a button that never comes (query #later)", "not done within the step limit of 2s",
		"ready complete", "the query matches 0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure does not say %q:\n%s", want, msg)
		}
	}
	checkPicture(t, msg)
}

func TestAFailureLooksAfterThePagesDeadline(t *testing.T) {
	p, f := realPage(t, 4*time.Second)
	p.Step = -1
	msg := caught(f, func() {
		p.Run("wait for the hidden button with no step limit", chromedp.WaitVisible("#go", chromedp.ByQuery))
	})
	if msg == "" || p.Ctx.Err() == nil {
		t.Fatalf("the wait ended before the page's deadline:\n%s", msg)
	}
	for _, want := range []string{"wait for the hidden button with no step limit (query #go): the page's deadline passed",
		"look: ready complete", "the query matches 1, the first button#go.big not shown, disabled"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure does not say %q:\n%s", want, msg)
		}
	}
	checkPicture(t, msg)
}

func TestWaitForFailureLeavesAPicture(t *testing.T) {
	p, f := realPage(t, 60*time.Second)
	msg := caught(f, func() { p.WaitFor("a condition that never holds", "false", 200*time.Millisecond) })
	if !strings.Contains(msg, "a condition that never holds: condition never held within 200ms: false") || !strings.Contains(msg, "look: ready complete") {
		t.Errorf("the failure reads:\n%s", msg)
	}
	checkPicture(t, msg)
}

func TestStretchMovesTheDeadlineBySlow(t *testing.T) {
	p := NewPage(t, t.Context(), 50*time.Millisecond)
	p.Stretch(4)
	time.Sleep(100 * time.Millisecond)
	if p.Ctx.Err() != nil {
		t.Fatal("the page's deadline did not stretch to four times its budget")
	}
	time.Sleep(150 * time.Millisecond)
	if p.Ctx.Err() == nil {
		t.Fatal("the stretched deadline never came")
	}
}
