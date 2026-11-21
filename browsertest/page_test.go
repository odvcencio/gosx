package browsertest

import (
	"os"
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
