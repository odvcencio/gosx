package loop

import "time"

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

// captureHost is where the capture clock reads its grants and reports its
// progress.
type captureHost interface {
	// read returns whether the host has set the clock, the interval per frame
	// in milliseconds (zero or less: the clock is off and the game runs on the
	// frame source's time) and the number of frames granted since the page
	// started. Until the host sets the clock the game holds at its first frame,
	// so a capture starts from the same frame however long the page took to
	// load.
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
