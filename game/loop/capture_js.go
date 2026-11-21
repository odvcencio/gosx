//go:build js && wasm && gosx_capture

package loop

import (
	"strconv"
	"syscall/js"
)

// In a build with the gosx_capture tag the capture clock (capture.go) reads
// and writes three attributes on the document element:
//
//	data-gosx-capture-step   the host writes the interval per frame in
//	                         milliseconds (16.666666666666668 films at 60 per
//	                         second of game time); 0: the game runs on the
//	                         frame source's own time; absent: the game holds
//	                         at its first frame (a page may replace the
//	                         document while it loads, so a host sets the
//	                         attributes once the page is up)
//	data-gosx-capture-grant  the host writes how many frames it has granted in
//	                         total since the page started
//	data-gosx-capture-done   the Driver writes how many granted frames it has
//	                         stepped once the renderer has drawn the last one
//
// A capture script sets the step once the page is up, raises
// the grant by one, waits until done equals the grant, takes its picture, and
// repeats. Only the first Driver created on a page uses the clock.
type documentCaptureHost struct{ dataset js.Value }

func (h documentCaptureHost) read() (bool, float64, uint64) {
	raw := h.dataset.Get("gosxCaptureStep")
	if raw.Type() != js.TypeString {
		return false, 0, 0
	}
	step, err := strconv.ParseFloat(raw.String(), 64)
	if err != nil {
		return true, 0, 0
	}
	granted, err := strconv.ParseUint(h.dataset.Get("gosxCaptureGrant").String(), 10, 64)
	if err != nil {
		granted = 0
	}
	return true, step, granted
}

func (h documentCaptureHost) announce(done uint64) {
	h.dataset.Set("gosxCaptureDone", strconv.FormatUint(done, 10))
}

var captureClaimed bool

func newCaptureClock() *captureClock {
	if captureClaimed {
		return nil
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	root := doc.Get("documentElement")
	if !root.Truthy() {
		return nil
	}
	captureClaimed = true
	return &captureClock{host: documentCaptureHost{dataset: root.Get("dataset")}}
}
