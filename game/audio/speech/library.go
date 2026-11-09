package speech

import "sort"

// FragID names one recorded fragment (a word or phrase). Zero is "none".
type FragID uint32

// Pos is where a fragment sits in a sentence. Speakers pitch the start of a
// sentence, its middle and its end differently, so each fragment can have one
// recording per position.
type Pos uint8

const (
	PosStart Pos = iota
	PosMid
	PosEnd
	numPos
)

// String returns "start", "mid" or "end".
func (p Pos) String() string {
	switch p {
	case PosStart:
		return "start"
	case PosMid:
		return "mid"
	case PosEnd:
		return "end"
	}
	return "unknown"
}

// Clip is one recording of a fragment.
type Clip struct {
	// Frames is the clip length in sample frames at the library's rate.
	Frames int32
	// Handle is whatever the playback backend needs: a decoded host buffer
	// in the browser, a []float32 for Mix, or nil when only timing matters.
	Handle any
}

// Library maps fragment keys to recordings. The grammar interns the keys it
// needs when it compiles; the rendered clip manifest then attaches recordings.
// A Library is not safe for concurrent writes; fill it at load time.
type Library struct {
	rate  int
	keys  []string // index = FragID; keys[0] is unused
	index map[string]FragID
	clips [][numPos]Clip
	has   [][numPos]bool
}

// NewLibrary returns an empty library whose clips are at rate Hz.
func NewLibrary(rate int) *Library {
	return &Library{rate: rate, keys: []string{""}, index: map[string]FragID{}, clips: make([][numPos]Clip, 1), has: make([][numPos]bool, 1)}
}

// Rate is the sample rate of every clip, in Hz.
func (l *Library) Rate() int { return l.rate }

// Intern returns the ID for key, adding it when new.
func (l *Library) Intern(key string) FragID {
	if id, ok := l.index[key]; ok {
		return id
	}
	id := FragID(len(l.keys))
	l.keys = append(l.keys, key)
	l.index[key] = id
	l.clips = append(l.clips, [numPos]Clip{})
	l.has = append(l.has, [numPos]bool{})
	return id
}

// Lookup returns the ID for key without adding it.
func (l *Library) Lookup(key string) (FragID, bool) {
	id, ok := l.index[key]
	return id, ok
}

// Key returns the key behind id.
func (l *Library) Key(id FragID) string {
	if int(id) >= len(l.keys) {
		return ""
	}
	return l.keys[id]
}

// Len is the number of interned keys.
func (l *Library) Len() int { return len(l.keys) - 1 }

// Set attaches a recording of id for position pos.
func (l *Library) Set(id FragID, pos Pos, c Clip) {
	if id == 0 || int(id) >= len(l.keys) || pos >= numPos {
		return
	}
	l.clips[id][pos] = c
	l.has[id][pos] = true
}

// Clip returns the recording for id at pos. When that position was not
// recorded it falls back to the middle, then end, then start recording, so a
// library with a single recording per fragment still plays.
func (l *Library) Clip(id FragID, pos Pos) (Clip, bool) {
	if int(id) >= len(l.keys) || id == 0 {
		return Clip{}, false
	}
	h := &l.has[id]
	if pos < numPos && h[pos] {
		return l.clips[id][pos], true
	}
	for _, p := range [...]Pos{PosMid, PosEnd, PosStart} {
		if h[p] {
			return l.clips[id][p], true
		}
	}
	return Clip{}, false
}

// Missing lists the keys that have no recording, sorted. Run it after loading
// the manifest to find fragments the grammar needs but the render lacks.
func (l *Library) Missing() []string {
	var out []string
	for id := 1; id < len(l.keys); id++ {
		if !l.has[id][0] && !l.has[id][1] && !l.has[id][2] {
			out = append(out, l.keys[id])
		}
	}
	sort.Strings(out)
	return out
}
