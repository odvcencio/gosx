package netplay

import "errors"

// Game is a deterministic simulation the netplay engine can roll back.
//
// Inputs are absolute held state for one frame. Press and release edges are
// derived inside Step from the previous frame's held state, so repeating the
// last input as a prediction never re-fires a press. Save must contain only
// simulation state, never session, connection or UI state. In steady state
// Step, Save, Load and Events must not allocate.
type Game[I any] interface {
	// Step advances exactly one frame; inputs is indexed by player.
	Step(inputs []I)
	// Save appends the full state to dst using a deterministic encoding.
	Save(dst []byte) []byte
	// Load replaces the state with one produced by Save.
	Load(src []byte) error
	// Events appends every event emitted by the last Step, in emission order.
	// A game that keeps only one "last event" per frame loses same-frame
	// trades and does not meet this contract.
	Events(dst []Event) []Event
}

// Event is one presentation-relevant happening in one frame.
type Event struct {
	Frame  int64
	Kind   uint16
	Source uint32   // app-defined id, for example attacker slot plus move id
	Data   [16]byte // app-defined payload
}

// Predictor guesses a remote player's input for a frame not yet received.
// last is the last confirmed input from that player.
type Predictor[I any] func(player int, last I, frame int64) I

// RepeatLast is the default Predictor: it repeats the last confirmed input.
func RepeatLast[I any](player int, last I, frame int64) I { return last }

// Codec encodes one input to a fixed number of bytes for the wire.
type Codec[I any] interface {
	// Size is the encoded size in bytes; it must not change.
	Size() int
	// Put writes in to dst[:Size()].
	Put(dst []byte, in I)
	// Get reads an input from src[:Size()].
	Get(src []byte) I
}

var errBadState = errors.New("netplay: bad state blob")

// Checksum hashes Save bytes (FNV-1a, 64 bit). It does not allocate.
func Checksum(state []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, b := range state {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}
