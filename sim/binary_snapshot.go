package sim

import (
	"encoding/binary"
	"errors"
)

// Binary snapshot frame layout (all integers big endian). Every frame starts
// with the 0x00 application prefix that hub.BroadcastBinarySnapshot requires.
//
//	keyframe: 00 'K' epoch(8) frame(8) state...
//	delta:    00 'D' epoch(8) frame(8) prefixLen(4) suffixLen(4) middle...
//
// A delta rebuilds the state as keyframeState[:prefixLen] + middle +
// keyframeState[len-suffixLen:]. It depends only on its keyframe, never on an
// earlier delta, as the hub snapshot lane requires.
const (
	binaryKeyframe  = 'K'
	binaryDelta     = 'D'
	binaryHeaderLen = 2 + 8 + 8
)

func binaryHeader(kind byte, epoch, frame uint64, extra int) []byte {
	buf := make([]byte, binaryHeaderLen, binaryHeaderLen+extra)
	buf[1] = kind
	binary.BigEndian.PutUint64(buf[2:], epoch)
	binary.BigEndian.PutUint64(buf[10:], frame)
	return buf
}

func encodeBinaryKeyframe(epoch, frame uint64, state []byte) []byte {
	return append(binaryHeader(binaryKeyframe, epoch, frame, len(state)), state...)
}

// encodeBinaryDelta returns nil when a delta would not be smaller than the
// full state, so the caller starts a new keyframe instead.
func encodeBinaryDelta(epoch, frame uint64, base, state []byte) []byte {
	p := 0
	for p < len(base) && p < len(state) && base[p] == state[p] {
		p++
	}
	s := 0
	for s < len(base)-p && s < len(state)-p && base[len(base)-1-s] == state[len(state)-1-s] {
		s++
	}
	middle := state[p : len(state)-s]
	if 8+len(middle) >= len(state) {
		return nil
	}
	buf := binaryHeader(binaryDelta, epoch, frame, 8+len(middle))
	buf = binary.BigEndian.AppendUint32(buf, uint32(p))
	buf = binary.BigEndian.AppendUint32(buf, uint32(s))
	return append(buf, middle...)
}

// ErrBinarySnapshot reports a malformed or mismatched binary snapshot frame.
var ErrBinarySnapshot = errors.New("sim: invalid binary snapshot frame")

// DecodeBinarySnapshot decodes a frame produced by NewBinarySnapshotBroadcaster.
// For a keyframe, pass baseline as nil. For a delta, pass the keyframe frame it
// was encoded against. It returns the epoch, the simulation frame and the state.
func DecodeBinarySnapshot(frame, baseline []byte) (epoch, simFrame uint64, state []byte, err error) {
	if len(frame) < binaryHeaderLen || frame[0] != 0 {
		return 0, 0, nil, ErrBinarySnapshot
	}
	epoch = binary.BigEndian.Uint64(frame[2:])
	simFrame = binary.BigEndian.Uint64(frame[10:])
	switch frame[1] {
	case binaryKeyframe:
		return epoch, simFrame, append([]byte(nil), frame[binaryHeaderLen:]...), nil
	case binaryDelta:
		if len(frame) < binaryHeaderLen+8 || len(baseline) < binaryHeaderLen || baseline[1] != binaryKeyframe ||
			binary.BigEndian.Uint64(baseline[2:]) != epoch {
			return 0, 0, nil, ErrBinarySnapshot
		}
		base := baseline[binaryHeaderLen:]
		p := int(binary.BigEndian.Uint32(frame[binaryHeaderLen:]))
		s := int(binary.BigEndian.Uint32(frame[binaryHeaderLen+4:]))
		if p+s > len(base) {
			return 0, 0, nil, ErrBinarySnapshot
		}
		out := append([]byte(nil), base[:p]...)
		out = append(out, frame[binaryHeaderLen+8:]...)
		out = append(out, base[len(base)-s:]...)
		return epoch, simFrame, out, nil
	}
	return 0, 0, nil, ErrBinarySnapshot
}
