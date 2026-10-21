package netplay

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ReplayCheck is a checksum of the state at the start of Frame.
type ReplayCheck struct {
	Frame uint32
	Sum   uint64
}

// Replay is a recorded match: the confirmed inputs of both players, the
// simulation identity they were played under, and checksums to verify a
// re-simulation. Marshal and Unmarshal give the versioned binary format.
type Replay struct {
	Identity  [32]byte
	InputSize int // bytes per encoded input
	Interval  int // checksum interval K
	Frames    int // number of frames recorded
	// Inputs holds Frames entries of 2*InputSize bytes: player 0's input,
	// then player 1's, for each frame in order.
	Inputs []byte
	Checks []ReplayCheck
}

// Replay file format, version 1 (little endian):
//
//	8  bytes  magic "GXNPRPL\x00"
//	2         version (1)
//	2         flags (0)
//	32        simulation identity
//	2         input size
//	2         checksum interval K
//	4         frames
//	4         number of checks
//	frames*2*inputSize   inputs, player 0 then player 1 for each frame
//	checks*12            frame (4), checksum (8)
//	8         FNV-1a 64 of every byte above
const (
	replayMagic   = "GXNPRPL\x00"
	replayVersion = 1
	replayHdr     = 8 + 2 + 2 + 32 + 2 + 2 + 4 + 4
)

// Replay errors. Test with errors.Is.
var (
	ErrReplayFormat  = errors.New("netplay: malformed replay")
	ErrReplayVersion = errors.New("netplay: unsupported replay version")
	ErrReplayDiverge = errors.New("netplay: replay checksum mismatch")
)

// ReplayDivergeError says where a re-simulation stopped matching.
type ReplayDivergeError struct {
	Frame     uint32
	Want, Got uint64
}

func (e *ReplayDivergeError) Error() string {
	return fmt.Sprintf("netplay: replay diverges at frame %d (recorded %016x, simulated %016x)", e.Frame, e.Want, e.Got)
}

// Is makes errors.Is(err, ErrReplayDiverge) true.
func (e *ReplayDivergeError) Is(target error) bool { return target == ErrReplayDiverge }

// Marshal encodes the replay in the versioned binary format.
func (r *Replay) Marshal() []byte {
	b := make([]byte, 0, replayHdr+len(r.Inputs)+12*len(r.Checks)+8)
	le := binary.LittleEndian
	b = append(b, replayMagic...)
	b = le.AppendUint16(b, replayVersion)
	b = le.AppendUint16(b, 0)
	b = append(b, r.Identity[:]...)
	b = le.AppendUint16(b, uint16(r.InputSize))
	b = le.AppendUint16(b, uint16(r.Interval))
	b = le.AppendUint32(b, uint32(r.Frames))
	b = le.AppendUint32(b, uint32(len(r.Checks)))
	b = append(b, r.Inputs...)
	for _, c := range r.Checks {
		b = le.AppendUint32(b, c.Frame)
		b = le.AppendUint64(b, c.Sum)
	}
	return le.AppendUint64(b, Checksum(b))
}

// UnmarshalReplay decodes and validates a replay.
func UnmarshalReplay(b []byte) (*Replay, error) {
	le := binary.LittleEndian
	if len(b) < replayHdr+8 || string(b[:8]) != replayMagic {
		return nil, ErrReplayFormat
	}
	if v := le.Uint16(b[8:]); v != replayVersion {
		return nil, fmt.Errorf("%w: %d", ErrReplayVersion, v)
	}
	if le.Uint64(b[len(b)-8:]) != Checksum(b[:len(b)-8]) {
		return nil, fmt.Errorf("%w: trailer checksum", ErrReplayFormat)
	}
	r := &Replay{
		InputSize: int(le.Uint16(b[44:])),
		Interval:  int(le.Uint16(b[46:])),
		Frames:    int(le.Uint32(b[48:])),
	}
	copy(r.Identity[:], b[12:44])
	nchecks := int(le.Uint32(b[52:]))
	if r.InputSize == 0 || r.Interval == 0 {
		return nil, ErrReplayFormat
	}
	inLen := uint64(r.Frames) * 2 * uint64(r.InputSize)
	if uint64(len(b)) != uint64(replayHdr)+inLen+12*uint64(nchecks)+8 {
		return nil, fmt.Errorf("%w: length", ErrReplayFormat)
	}
	r.Inputs = append([]byte(nil), b[replayHdr:replayHdr+int(inLen)]...)
	o := replayHdr + int(inLen)
	for i := 0; i < nchecks; i++ {
		r.Checks = append(r.Checks, ReplayCheck{Frame: le.Uint32(b[o:]), Sum: le.Uint64(b[o+4:])})
		o += 12
	}
	return r, nil
}

// VerifyReplay re-simulates r with a fresh game and compares every recorded
// checksum. It refuses a replay recorded under a different simulation
// identity (ErrIdentityMismatch) before it runs a single frame.
func VerifyReplay[I any](r *Replay, identity [32]byte, newGame func() Game[I], codec Codec[I]) error {
	if r.Identity != identity {
		return ErrIdentityMismatch
	}
	isz := codec.Size()
	if r.InputSize != isz || len(r.Inputs) != r.Frames*2*isz {
		return ErrReplayFormat
	}
	g := newGame()
	in := make([]I, 2)
	ci := 0
	var buf []byte
	for f := 0; f < r.Frames; f++ {
		o := f * 2 * isz
		in[0] = codec.Get(r.Inputs[o:])
		in[1] = codec.Get(r.Inputs[o+isz:])
		g.Step(in)
		for ci < len(r.Checks) && int(r.Checks[ci].Frame) == f+1 {
			buf = g.Save(buf[:0])
			if got := Checksum(buf); got != r.Checks[ci].Sum {
				return &ReplayDivergeError{Frame: r.Checks[ci].Frame, Want: r.Checks[ci].Sum, Got: got}
			}
			ci++
		}
	}
	if ci != len(r.Checks) {
		for _, c := range r.Checks[ci:] {
			if int(c.Frame) > r.Frames {
				continue // recorded beyond the inputs we hold
			}
			return ErrReplayFormat
		}
	}
	return nil
}
