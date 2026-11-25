//go:build !js || !wasm

package focus

import (
	"testing"

	"m31labs.dev/gosx/client/browser"
)

// Off the browser no control is ever drawn, so these check the request's
// frames, its replacement and its cancelling; when_js_test.go checks the
// focus itself.

func TestWhenGivesUpAfterItsFrames(t *testing.T) {
	p := When(browser.CurrentDocument(), "#play", 3)
	for i, want := range []bool{true, true, false, false} {
		if got := Step(); got != want {
			t.Fatalf("Step %d = %v, want %v", i+1, got, want)
		}
	}
	if p.Waiting() {
		t.Fatal("a request with no frames left is still waiting")
	}
	if When(browser.CurrentDocument(), "#play", 0).Waiting() || When(browser.CurrentDocument(), "#play", -4).Waiting() {
		t.Fatal("a request with no frames waits")
	}
}

func TestANewerRequestReplacesTheOlder(t *testing.T) {
	doc := browser.CurrentDocument()
	old := When(doc, "#featured", 10)
	newer := When(doc, "#final-again", 10)
	if old.Waiting() || !newer.Waiting() {
		t.Fatalf("waiting: old %v newer %v, want false true", old.Waiting(), newer.Waiting())
	}
	// Cancelling the replaced request leaves the newer one alone.
	old.Cancel()
	if !newer.Waiting() || !Step() {
		t.Fatal("cancelling a replaced request withdrew the newer one")
	}
	newer.Cancel()
	if newer.Waiting() || Step() {
		t.Fatal("a cancelled request still waits")
	}
	var none *Pending
	none.Cancel()
	if none.Waiting() {
		t.Fatal("a nil request waits")
	}
}

func TestReadyNeedsAControl(t *testing.T) {
	if Ready(browser.Element{}) {
		t.Fatal("no element is ready")
	}
}
