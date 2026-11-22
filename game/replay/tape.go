package replay

import "math"

// DefaultTapeSpeed is the speed of a Tape whose Speed is not set: half speed,
// the usual pace of a sports replay.
const DefaultTapeSpeed = 0.5

// TapeMark is a landmark on a Tape: a frame and the name a transport shows
// for it ("Snap", "Tackle", "Whistle").
type TapeMark struct {
	Index int    `json:"index"`
	Label string `json:"label"`
}

// Tape is a replay transport for a game with a fixed step. It is a cursor
// over the frames [0, Count) of one stretch of a recording: the game opens it
// over the live view, plays it at Speed recorded frames per tick of its own
// loop, holds it on a frame, steps it between marks and closes it to go back
// to the live game.
//
// A Tape holds no simulation and no frames. The game shows frame Index, for
// example by seeking its Player to the stretch's first tick plus Index. Unlike
// Player.Advance, which runs on wall time, Tick moves the cursor once per
// fixed tick, so a frame-stepped page plays the same frames at every frame
// rate, and a replay held on a frame shows exactly that recorded frame.
//
// The zero Tape is closed and empty. Load it with each new stretch.
type Tape struct {
	// Speed is how many recorded frames one Tick plays: 0.5 is half speed.
	// Zero or less plays at DefaultTapeSpeed.
	Speed float64

	count         int
	marks         []TapeMark
	cursor        float64
	open, playing bool
}

// Load points the tape at a stretch of count frames with its marks, in frame
// order, and closes it. Speed is kept.
func (t *Tape) Load(count int, marks []TapeMark) {
	*t = Tape{Speed: t.Speed, count: max(count, 0), marks: marks}
}

// Clear empties and closes the tape. Speed is kept.
func (t *Tape) Clear() { *t = Tape{Speed: t.Speed} }

// Count is the number of frames; zero means there is nothing to show.
func (t *Tape) Count() int { return t.count }

// Marks are the stretch's landmarks, as loaded.
func (t *Tape) Marks() []TapeMark { return t.marks }

// Active reports whether the tape is open: the replay is on screen.
func (t *Tape) Active() bool { return t.open }

// Playing reports whether the open tape is running, as opposed to held on a
// frame.
func (t *Tape) Playing() bool { return t.open && t.playing }

// Index is the frame on screen, or -1 when the tape is closed.
func (t *Tape) Index() int {
	if !t.open {
		return -1
	}
	return int(math.Floor(t.cursor))
}

// Open starts playing from the first frame. An empty tape stays closed.
func (t *Tape) Open() {
	if t.count == 0 {
		return
	}
	t.open, t.playing, t.cursor = true, true, 0
}

// Close goes back to the live game.
func (t *Tape) Close() { t.open, t.playing = false, false }

// Seek holds the open tape on frame index, clamped to the stretch.
func (t *Tape) Seek(index int) {
	if !t.open {
		return
	}
	t.cursor = float64(max(0, min(t.count-1, index)))
	t.playing = false
}

// Toggle plays or holds the open tape. Playing from the last frame starts
// over from the first.
func (t *Tape) Toggle() {
	if !t.open {
		return
	}
	if !t.playing && t.Index() == t.count-1 {
		t.cursor = 0
	}
	t.playing = !t.playing
}

// Tick advances a playing tape by one tick of the game's step, Speed frames,
// and holds it on the last frame when it gets there.
func (t *Tape) Tick() {
	if !t.open || !t.playing {
		return
	}
	speed := t.Speed
	if speed <= 0 {
		speed = DefaultTapeSpeed
	}
	t.cursor = math.Min(float64(t.count-1), t.cursor+speed)
	if t.cursor >= float64(t.count-1) {
		t.playing = false
	}
}

// Jump holds the tape on the first mark after the frame on screen, or with a
// negative direction the last mark before it. It reports whether there was a
// mark that way.
func (t *Tape) Jump(direction int) bool {
	if !t.open {
		return false
	}
	at := t.Index()
	if direction > 0 {
		for _, m := range t.marks {
			if m.Index > at {
				t.Seek(m.Index)
				return true
			}
		}
		return false
	}
	for i := len(t.marks) - 1; i >= 0; i-- {
		if t.marks[i].Index < at {
			t.Seek(t.marks[i].Index)
			return true
		}
	}
	return false
}
