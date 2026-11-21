//go:build js && wasm

package browser

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/browserdom"
)

func TestLabelWritesOnlyWhatChanged(t *testing.T) {
	raw := js.Global().Call("eval", `({hidden: false, textContent: "", writes: 0,
	  style: {transform: "", vars: {}, setProperty(n, v) { this.vars[n] = v; this.owner.writes++; }}})`)
	raw.Get("style").Set("owner", raw)
	l := NewLabel(browserdom.FromJS(raw))
	if !raw.Get("hidden").Bool() || !l.Hidden() {
		t.Fatal("a new label must start hidden")
	}
	l.Show(10.7, 20.2, "WR 4")
	l.SetVar("--chip", "rgb(1 2 3)")
	if raw.Get("hidden").Bool() || raw.Get("textContent").String() != "WR 4" ||
		raw.Get("style").Get("transform").String() != "translate(10px,20px) translate(-50%,-50%)" ||
		raw.Get("style").Get("vars").Get("--chip").String() != "rgb(1 2 3)" {
		t.Fatalf("label after Show: hidden %v text %q transform %q", raw.Get("hidden"), raw.Get("textContent"), raw.Get("style").Get("transform"))
	}
	raw.Get("style").Set("transform", "untouched")
	l.Show(10.2, 20.9, "WR 4")
	l.SetVar("--chip", "rgb(1 2 3)")
	if raw.Get("style").Get("transform").String() != "untouched" || raw.Get("writes").Int() != 1 {
		t.Fatal("a label that did not move or change rewrote the page")
	}
	l.Hide()
	if !raw.Get("hidden").Bool() {
		t.Fatal("Hide left the label shown")
	}
}
