package host

import (
	"errors"
	"fmt"
	"sync"
)

// PadTestSession is the page side of the pad test protocol with no transport:
// it turns incoming messages into PadTestSource calls and frame ends into the
// messages to send. A browser adapter feeds it WebSocket text; a test feeds it
// a Go WebSocket. It is safe for concurrent use.
//
// The frame loop drives the source directly (BeginFrame, the game's reads,
// then Session.EndFrame in place of Source.EndFrame).
type PadTestSession struct {
	mu       sync.Mutex
	src      *PadTestSource
	token    string
	ready    bool
	gotReady bool
	lastSeq  uint64
	watches  []padWatch
}

type padWatch struct {
	id             uint64
	drawn, sampled uint64 // absolute thresholds, 0 for none
}

// NewPadTestSession wraps src. token is the one from the page URL.
func NewPadTestSession(src *PadTestSource, token string) *PadTestSession {
	return &PadTestSession{src: src, token: token}
}

// Hello returns the first message to send after the socket opens.
func (s *PadTestSession) Hello() PadMessage {
	return PadMessage{Type: PadMsgHello, Version: PadTestProtocolVersion, Token: s.token}
}

// Ready reports whether the server answered hello.
func (s *PadTestSession) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// ErrPadProtocol is wrapped by every error Receive returns for a message the
// page must not accept.
var ErrPadProtocol = errors.New("host: pad test protocol violation")

// Handle processes one server message and returns the replies to send now. It
// drops a message Receive refuses; a browser adapter calls Receive and ends the
// session on the error.
func (s *PadTestSession) Handle(m PadMessage) []PadMessage {
	replies, _ := s.Receive(m)
	return replies
}

// Receive processes one server message. The page accepts only what a server
// sends, in order: one ready, with this protocol's version, and then set and
// watch. A set or watch before ready, a second ready, a wrong version, and a
// type the page itself sends (hello, applied, rejected, progress) return an
// error wrapping ErrPadProtocol, change nothing and send nothing.
func (s *PadTestSession) Receive(m PadMessage) ([]PadMessage, error) {
	switch m.Type {
	case PadMsgReady:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gotReady {
			return nil, fmt.Errorf("%w: second ready", ErrPadProtocol)
		}
		s.gotReady = true
		if m.Version != PadTestProtocolVersion {
			return nil, fmt.Errorf("%w: server speaks version %d, page speaks %d", ErrPadProtocol, m.Version, PadTestProtocolVersion)
		}
		s.ready = true
		return nil, nil
	case PadMsgSet, PadMsgWatch:
		if !s.Ready() {
			return nil, fmt.Errorf("%w: %s before ready", ErrPadProtocol, m.Type)
		}
	default:
		return nil, fmt.Errorf("%w: %q is not a server message", ErrPadProtocol, m.Type)
	}
	if m.Type == PadMsgSet {
		return s.handleSet(m), nil
	}
	drawn, sampled := s.src.Counts()
	w := padWatch{id: m.Watch, drawn: m.Drawn, sampled: m.Sampled}
	if m.Relative {
		if w.drawn != 0 {
			w.drawn += drawn
		}
		if w.sampled != 0 {
			w.sampled += sampled
		}
	}
	if (w.drawn == 0 || drawn >= w.drawn) && (w.sampled == 0 || sampled >= w.sampled) {
		return []PadMessage{s.progress(w.id)}, nil
	}
	s.mu.Lock()
	s.watches = append(s.watches, w)
	s.mu.Unlock()
	return nil, nil
}

func (s *PadTestSession) handleSet(m PadMessage) []PadMessage {
	req := PadSet{Seq: m.Seq, Pads: PadStatesFromWire(m.Pads)}
	switch m.At {
	case PadAtNext:
		req.Next = true
	case PadAtFrame:
		req.Frame = m.Frame
	default:
		return []PadMessage{{Type: PadMsgRejected, Seq: m.Seq, Frame: s.src.Frame(), Reason: PadRejectInvalid}}
	}
	err := s.src.Enqueue(req)
	if err == nil {
		return nil
	}
	reason := PadRejectInvalid
	var late *PadLateError
	switch {
	case errors.As(err, &late):
		reason = PadRejectLate
	case errors.Is(err, ErrPadSequence):
		reason = PadRejectSequence
	case errors.Is(err, ErrPadSourceLost):
		reason = PadRejectLost
	}
	return []PadMessage{{Type: PadMsgRejected, Seq: m.Seq, Frame: s.src.Frame(), Reason: reason}}
}

func (s *PadTestSession) progress(watch uint64) PadMessage {
	drawn, sampled := s.src.Counts()
	s.mu.Lock()
	defer s.mu.Unlock()
	return PadMessage{Type: PadMsgProgress, Watch: watch, Drawn: drawn, Sampled: sampled, Seq: s.lastSeq, Frame: s.src.Frame()}
}

// EndFrame ends the source's open frame and returns the messages to send:
// rejected for missed exact-frame changes, applied for a change a read has now
// sampled, progress for the first sampled frame (which readies the driver) and
// for each satisfied watch.
func (s *PadTestSession) EndFrame() []PadMessage {
	r := s.src.EndFrame()
	if r.Frame == 0 {
		return nil
	}
	var out []PadMessage
	for _, seq := range r.Missed {
		out = append(out, PadMessage{Type: PadMsgRejected, Seq: seq, Frame: r.Frame, Reason: PadRejectMissed})
	}
	s.mu.Lock()
	if r.Seq != 0 {
		s.lastSeq = r.Seq
	}
	s.mu.Unlock()
	if r.Seq != 0 {
		out = append(out, PadMessage{Type: PadMsgApplied, Seq: r.Seq, Latched: r.LatchedFrame, Frame: r.Frame, Drawn: r.Drawn, Sampled: r.SampledFrames})
	}
	if r.Sampled && r.SampledFrames == 1 {
		out = append(out, s.progress(0))
	}
	s.mu.Lock()
	var due []uint64
	kept := s.watches[:0]
	for _, w := range s.watches {
		if (w.drawn == 0 || r.Drawn >= w.drawn) && (w.sampled == 0 || r.SampledFrames >= w.sampled) {
			due = append(due, w.id)
		} else {
			kept = append(kept, w)
		}
	}
	s.watches = kept
	s.mu.Unlock()
	for _, id := range due {
		out = append(out, s.progress(id))
	}
	return out
}
