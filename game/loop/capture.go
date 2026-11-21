package loop

import (
	"strings"
	"time"
)

// The capture clock lets a test host film a game frame by frame: in capture
// mode the host grants frames one at a time, the Driver advances the game by
// exactly one fixed interval per granted frame, whatever the wall time between
// frames, and it holds the game (no Step, no render callback) while no grant
// is waiting. The renderer keeps drawing the held state, so a screenshot taken
// at any time shows the last granted frame. Two runs that grant the same
// frames and send the same input produce the same game frames, on a loaded
// machine or at a resolution the machine draws slowly.
//
// The browser side is in capture_js.go and exists only in builds with the
// gosx_capture tag; other builds carry none of it.
//
// A tagged page runs on the frame source's time until the host sets the
// clock, so a page opened, reloaded or navigated by a test that never films
// plays as in a player's window. A page whose URL query carries
// CaptureQueryField asks for the clock from the start instead: it holds at
// its first frame until the host sets the clock, so a film starts from the
// same frame however long the page took to load.

// CaptureQueryField is the URL query parameter (any value, or none) with
// which a page asks the capture clock to hold it at its first frame until
// the host sets the clock. Like the pad adapter's PadTestQueryField in
// game/host, it is read once, when the clock is created.
const CaptureQueryField = "gosx-capture"

// captureRequested reports whether a page's URL query (location.search, with
// or without its leading "?") carries CaptureQueryField.
func captureRequested(search string) bool {
	search = strings.TrimPrefix(search, "?")
	for _, pair := range strings.Split(search, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if key == CaptureQueryField {
			return true
		}
	}
	return false
}

// captureHost is where the capture clock reads its grants and reports its
// progress.
type captureHost interface {
	// read returns whether the game may run, the interval per frame in
	// milliseconds (zero or less: the clock is off and the game runs on the
	// frame source's time) and the number of frames granted since the page
	// started. set is false only on a page that asked for the clock from the
	// start (CaptureQueryField) while the host has not yet set it: the game
	// holds at its first frame.
	read() (set bool, stepMS float64, granted uint64)
	// announce reports that done frames have been stepped and that the last of
	// them has been drawn.
	announce(done uint64)
}

type captureClock struct {
	host      captureHost
	done      uint64
	announced uint64
}

// next decides the frame under way. run is false when the game holds; when it
// runs, interval is the time to step, or zero to keep the frame source's own.
func (c *captureClock) next() (interval time.Duration, run bool) {
	// The previous granted frame was stepped in an earlier callback, and the
	// renderer drew it before this callback, so it is announced now.
	if c.announced != c.done {
		c.host.announce(c.done)
		c.announced = c.done
	}
	set, stepMS, granted := c.host.read()
	if !set {
		return 0, false
	}
	if !(stepMS > 0) {
		return 0, true
	}
	if granted <= c.done {
		return 0, false
	}
	c.done++
	return time.Duration(stepMS * float64(time.Millisecond)), true
}
