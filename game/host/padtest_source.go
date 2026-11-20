package host

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
)

// Limits the validator enforces.
const (
	PadTestMaxSlots   = 4   // browsers expose at most four pads: indexes 0 to 3
	PadTestMaxButtons = 32  // matches the input poller's limit
	PadTestMaxAxes    = 16  //
	PadTestMaxID      = 256 // bytes
)

// DefaultPadTestID is the neutral stub ID for checks that are not about
// branding.
const DefaultPadTestID = "Stub Standard Pad (STANDARD GAMEPAD)"

// NeutralStandardPad returns a connected standard-layout pad at rest: 17
// buttons and four axes, all zero.
func NeutralStandardPad(index int, id, mapping string) GamepadState {
	return GamepadState{Index: index, ID: id, Mapping: mapping, Buttons: make([]float64, 17), Axes: make([]float64, 4)}
}

// ValidatePadStates checks a complete snapshot of connected pads: indexes in
// range and unique, a non-empty ID, a known mapping, bounded counts, finite
// values with buttons in [0,1] and axes in [-1,1].
func ValidatePadStates(pads []GamepadState) error {
	seen := map[int]bool{}
	for _, p := range pads {
		if p.Index < 0 || p.Index >= PadTestMaxSlots {
			return fmt.Errorf("pad index %d outside 0..%d", p.Index, PadTestMaxSlots-1)
		}
		if seen[p.Index] {
			return fmt.Errorf("pad index %d appears twice", p.Index)
		}
		seen[p.Index] = true
		if p.ID == "" || len(p.ID) > PadTestMaxID {
			return fmt.Errorf("pad %d: ID must be 1 to %d bytes", p.Index, PadTestMaxID)
		}
		switch p.Mapping {
		case "", "standard", "xr-standard":
		default:
			return fmt.Errorf("pad %d: unknown mapping %q", p.Index, p.Mapping)
		}
		if len(p.Buttons) > PadTestMaxButtons {
			return fmt.Errorf("pad %d: %d buttons, limit %d", p.Index, len(p.Buttons), PadTestMaxButtons)
		}
		if len(p.Axes) > PadTestMaxAxes {
			return fmt.Errorf("pad %d: %d axes, limit %d", p.Index, len(p.Axes), PadTestMaxAxes)
		}
		for i, v := range p.Buttons {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
				return fmt.Errorf("pad %d: button %d value %v outside [0,1]", p.Index, i, v)
			}
		}
		for i, v := range p.Axes {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < -1 || v > 1 {
				return fmt.Errorf("pad %d: axis %d value %v outside [-1,1]", p.Index, i, v)
			}
		}
	}
	return nil
}

// Errors from PadTestSource.Enqueue.
var (
	// ErrPadSequence means a sequence number did not increase.
	ErrPadSequence = errors.New("host: pad change sequence did not increase")
	// ErrPadSourceLost means the source was shut down with Lose.
	ErrPadSourceLost = errors.New("host: pad test source lost its page")
)

// PadLateError is returned for an exact-frame change whose frame has begun.
type PadLateError struct {
	Seq       uint64
	Requested uint64
	Current   uint64 // the frame number of the latest begun frame
}

func (e *PadLateError) Error() string {
	return fmt.Sprintf("host: pad change %d for frame %d arrived at frame %d", e.Seq, e.Requested, e.Current)
}

// PadSet is a request to replace the complete set of connected pads.
type PadSet struct {
	// Seq is the sequence number. It must exceed every earlier accepted Seq.
	Seq uint64
	// Next applies the change at the first frame that begins after Enqueue.
	// Otherwise the change applies exactly at Frame.
	Next  bool
	Frame uint64
	// Pads is the whole connected set. Slots not listed are absent. The source
	// copies the slices.
	Pads []GamepadState
}

// PadFrameReceipt is what EndFrame reports.
type PadFrameReceipt struct {
	// Frame is the frame that just ended. Drawn and SampledFrames are the
	// totals of completed frames, and of completed frames that Gamepads was
	// called in, including this one.
	Frame         uint64
	Drawn         uint64
	SampledFrames uint64
	// Sampled reports that Gamepads was called during this frame.
	Sampled bool
	// Seq is the newest latched sequence that a sampled frame has now read, or
	// zero when this frame acknowledges none. LatchedFrame is the frame that
	// latched it. A change latched on a frame with no read is acknowledged by
	// the first later frame that reads, so LatchedFrame can be below Frame.
	Seq          uint64
	LatchedFrame uint64
	// Missed lists exact-frame sequences dropped because their frame began
	// while an earlier change was still queued ahead of them.
	Missed []uint64
}

// PadTestSource is a host.GamepadSource over a frame-latched snapshot. It is
// plain Go with no clock: a frame loop calls BeginFrame, the game reads
// Gamepads, and the loop calls EndFrame. Changes queued with Enqueue between
// frames take effect at a BeginFrame, so every read inside one frame sees the
// same state.
//
// The source latches at most one change per frame, in sequence order, so a
// press and a release queued together land on two frames and a poller sees
// both edges. An exact-frame change whose frame has begun is rejected at
// Enqueue. It is safe for concurrent use: a socket goroutine may call Enqueue
// while the frame loop runs.
type PadTestSource struct {
	mu            sync.Mutex
	frame         uint64
	inFrame       bool
	sampled       bool
	drawn         uint64
	sampledFrames uint64
	lastSeq       uint64
	queue         []PadSet
	latched       []GamepadState
	unacked       uint64
	unackedFrame  uint64
	missed        []uint64
	lost          bool
}

// NewPadTestSource creates a source with no pads and no frames begun.
func NewPadTestSource() *PadTestSource { return &PadTestSource{} }

// Enqueue validates and queues a change. It never blocks on frames.
func (s *PadTestSource) Enqueue(req PadSet) error {
	pads := make([]GamepadState, len(req.Pads))
	for i, p := range req.Pads {
		pads[i] = GamepadState{
			Index: p.Index, ID: p.ID, Mapping: p.Mapping,
			Buttons: append([]float64(nil), p.Buttons...),
			Axes:    append([]float64(nil), p.Axes...),
		}
	}
	if err := ValidatePadStates(pads); err != nil {
		return err
	}
	sort.Slice(pads, func(i, j int) bool { return pads[i].Index < pads[j].Index })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lost {
		return ErrPadSourceLost
	}
	if req.Seq == 0 || req.Seq <= s.lastSeq {
		return ErrPadSequence
	}
	if !req.Next && req.Frame <= s.frame {
		return &PadLateError{Seq: req.Seq, Requested: req.Frame, Current: s.frame}
	}
	s.lastSeq = req.Seq
	req.Pads = pads
	s.queue = append(s.queue, req)
	return nil
}

// BeginFrame starts the next frame, latches the change that is due, and
// returns the frame number (the first frame is 1). Calling it again before
// EndFrame abandons the open frame without counting it.
func (s *PadTestSource) BeginFrame() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frame++
	s.inFrame = true
	s.sampled = false
	for len(s.queue) > 0 {
		if s.unacked != 0 {
			// A change latched earlier is not acknowledged yet (no read has
			// happened since). Latching another now would drop its receipt, so
			// the queue waits for the first reading frame.
			break
		}
		q := s.queue[0]
		if !q.Next {
			if q.Frame > s.frame {
				break // the head waits for its frame and holds the rest behind it
			}
			if q.Frame < s.frame {
				s.missed = append(s.missed, q.Seq)
				s.queue = s.queue[1:]
				continue
			}
		}
		s.queue = s.queue[1:]
		s.latched = q.Pads
		s.unacked, s.unackedFrame = q.Seq, s.frame
		break
	}
	return s.frame
}

// Gamepads returns the latched pads in ascending Index, without compacting
// indexes: with slot 0 unplugged, slot 1 is still Index 1. Inside a frame it
// marks the frame as sampled. The slice and its inner slices are shared and
// immutable until the next change: callers must not modify them.
func (s *PadTestSource) Gamepads() []GamepadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFrame {
		s.sampled = true
	}
	return s.latched
}

// EndFrame closes the open frame and returns its receipt. It returns the zero
// receipt when no frame is open.
func (s *PadTestSource) EndFrame() PadFrameReceipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inFrame {
		return PadFrameReceipt{}
	}
	s.inFrame = false
	s.drawn++
	r := PadFrameReceipt{Frame: s.frame, Sampled: s.sampled, Missed: s.missed}
	s.missed = nil
	if s.sampled {
		s.sampledFrames++
		if s.unacked != 0 {
			r.Seq, r.LatchedFrame = s.unacked, s.unackedFrame
			s.unacked = 0
		}
	}
	r.Drawn, r.SampledFrames = s.drawn, s.sampledFrames
	return r
}

// Counts returns the completed frames and the completed frames with a read.
func (s *PadTestSource) Counts() (drawn, sampled uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawn, s.sampledFrames
}

// Frame returns the number of the latest begun frame.
func (s *PadTestSource) Frame() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frame
}

// Lose drops every pad and queued change and refuses later changes, for a lost
// connection. The next read returns no pads; it never falls back to real ones.
func (s *PadTestSource) Lose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lost = true
	s.queue = nil
	s.latched = nil
	s.unacked = 0
}
