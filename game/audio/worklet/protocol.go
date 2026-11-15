package worklet

import (
	"errors"
	"fmt"
)

// Message types the processor posts to the page. They match the "t" field of
// the structured-clone messages documented in README.md.
const (
	MsgReady   = "r" // initialization finished
	MsgMessage = "m" // a batch of engine message records
	MsgState   = "s" // playback started (P true) or stopped (P false)
	MsgFault   = "f" // the reactor reported a fault
	MsgError   = "e" // the processor stopped on a host error
	MsgReject  = "x" // a request was refused
	MsgSwapped = "t" // an image replacement was accepted
	MsgTiming  = "q" // timing histogram, memory and underruns
)

// Event is one processor message, decoded from the structured-clone object
// that crossed the AudioWorklet port. A browser host fills the fields it
// receives; the rest stay zero.
type Event struct {
	Type string
	// Records holds the engine message records of a MsgMessage event (a copy,
	// not the transferred buffer). Frames is the engine position in sample
	// frames, since the last play, when the batch was posted.
	Records []byte
	Frames  float64
	Playing bool
	// Fault is the reactor's 16-bit fault argument for MsgFault.
	Fault uint16
	// Err is the text of a MsgError or MsgReject event; Request echoes the
	// request id a MsgReject or MsgSwapped answers.
	Err     string
	Request string
	// Underruns, MemoryBytes and Histogram come with MsgTiming. Histogram has
	// 256 bins: bins 0-253 count render quanta by 0.25 ms of CPU time, bin 254
	// counts quanta over the real-time budget, and bin 255 counts late callbacks.
	Underruns   int
	MemoryBytes int
	Histogram   []uint32
}

// State is what the page knows about one kernel node. It is a plain value
// updated by Apply, so the message protocol can be tested without a browser.
type State struct {
	Ready   bool
	Playing bool
	Closed  bool
	// Fault is nil until the reactor reports a fault.
	Fault *uint16
	// Error is the text of a fatal host error; it closes the node.
	Error string
	// Frames is the engine position in sample frames since the last play, as of
	// the most recent message batch. The processor drains messages every eighth
	// render quantum, so it lags the audio clock by up to 8*128 frames.
	Frames    float64
	Underruns int
}

// Apply updates the state for ev. It returns true when the event carried the
// transferred message buffer, which the page must hand back to the processor
// with a "b" message, or the processor stops draining messages.
func (s *State) Apply(ev Event) (returnBuffer bool) {
	switch ev.Type {
	case MsgReady:
		s.Ready = true
	case MsgMessage:
		s.Frames = ev.Frames
		return true
	case MsgState:
		s.Playing = ev.Playing
	case MsgFault:
		f := ev.Fault
		s.Fault = &f
		s.Playing = false
	case MsgError:
		s.Error = ev.Err
		s.Closed = true
		s.Playing = false
	case MsgTiming:
		s.Underruns = ev.Underruns
	}
	return false
}

// SplitRecords cuts a batch of fixed-size records. It reports an error for a
// batch whose length is not a multiple of size.
func SplitRecords(batch []byte, size int) ([][]byte, error) {
	if size < 1 || len(batch)%size != 0 {
		return nil, fmt.Errorf("audio: %d bytes is not a whole number of %d-byte records", len(batch), size)
	}
	out := make([][]byte, 0, len(batch)/size)
	for i := 0; i < len(batch); i += size {
		out = append(out, batch[i:i+size])
	}
	return out, nil
}

// CheckCommands validates a command batch against the backend layout before it
// is posted. The processor repeats the check on the audio thread.
func (b Backend) CheckCommands(batch []byte) error {
	if len(batch) == 0 {
		return errors.New("audio: empty command batch")
	}
	if len(batch)%b.CommandBytes != 0 {
		return fmt.Errorf("audio: %d bytes is not a whole number of %d-byte commands", len(batch), b.CommandBytes)
	}
	return nil
}
