package store

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const (
	opPut     byte = 1 // key, value
	opDel     byte = 2 // key
	opAppend  byte = 3 // log name, record
	opLogHead byte = 4 // log name, uvarint first sequence (snapshots)
	opEnd     byte = 5 // snapshot trailer: uvarint op count
	opDropLog byte = 6 // log name

	frameHeader = 8
	// maxFrame bounds the payload a decoder will believe, so a corrupt length
	// cannot ask for gigabytes. Commit refuses a batch whose frame would be
	// larger (ErrBatchTooLarge): recovery reads such a frame as a torn tail.
	maxFrame = MaxBatchBytes
)

// MaxBatchBytes is the largest encoded batch one commit accepts: one WAL
// frame. A batch encodes as its keys and values plus up to 21 bytes per op.
const MaxBatchBytes = 64 << 20

var crcTable = crc32.MakeTable(crc32.Castagnoli)

var (
	errShort = errors.New("store: short frame")
	errCRC   = errors.New("store: frame checksum mismatch")
	errBig   = errors.New("store: frame too large")
	errOps   = errors.New("store: malformed ops")
)

type op struct {
	kind byte
	key  string
	val  []byte
}

// appendFrame appends one frame holding ops to dst.
func appendFrame(dst []byte, ops []op) []byte {
	start := len(dst)
	dst = append(dst, 0, 0, 0, 0, 0, 0, 0, 0)
	for _, o := range ops {
		dst = append(dst, o.kind)
		dst = binary.AppendUvarint(dst, uint64(len(o.key)))
		dst = append(dst, o.key...)
		dst = binary.AppendUvarint(dst, uint64(len(o.val)))
		dst = append(dst, o.val...)
	}
	payload := dst[start+frameHeader:]
	binary.LittleEndian.PutUint32(dst[start:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(dst[start+4:], crc32.Checksum(payload, crcTable))
	return dst
}

// opBytes is the encoded size of o inside a frame payload, as appendFrame
// writes it.
func opBytes(o op) int {
	return 1 + uvarintLen(uint64(len(o.key))) + len(o.key) + uvarintLen(uint64(len(o.val))) + len(o.val)
}

func uvarintLen(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

// nextFrame returns the payload of the frame at the start of b and the number
// of bytes it occupies.
func nextFrame(b []byte) (payload []byte, n int, err error) {
	if len(b) < frameHeader {
		return nil, 0, errShort
	}
	size := binary.LittleEndian.Uint32(b)
	if size > maxFrame {
		return nil, 0, errBig
	}
	if uint64(len(b)-frameHeader) < uint64(size) {
		return nil, 0, errShort
	}
	payload = b[frameHeader : frameHeader+int(size)]
	if crc32.Checksum(payload, crcTable) != binary.LittleEndian.Uint32(b[4:]) {
		return nil, 0, errCRC
	}
	return payload, frameHeader + int(size), nil
}

// parseOps decodes a frame payload. Values alias payload.
func parseOps(payload []byte) ([]op, error) {
	var ops []op
	for len(payload) > 0 {
		kind := payload[0]
		if kind < opPut || kind > opDropLog {
			return nil, errOps
		}
		payload = payload[1:]
		key, rest, ok := lenPrefixed(payload)
		if !ok {
			return nil, errOps
		}
		val, rest, ok := lenPrefixed(rest)
		if !ok {
			return nil, errOps
		}
		payload = rest
		ops = append(ops, op{kind: kind, key: string(key), val: val})
	}
	if len(ops) == 0 {
		return nil, errOps
	}
	return ops, nil
}

func lenPrefixed(b []byte) (field, rest []byte, ok bool) {
	n, w := binary.Uvarint(b)
	if w <= 0 || n > uint64(len(b)-w) {
		return nil, nil, false
	}
	return b[w : w+int(n)], b[w+int(n):], true
}

// scanLog decodes consecutive frames from data and calls apply for each. It
// stops at the first frame that is short, corrupt, or malformed and returns
// the length of the valid prefix and the number of frames read.
func scanLog(data []byte, apply func([]op) error) (valid int, frames int, err error) {
	for valid < len(data) {
		payload, n, ferr := nextFrame(data[valid:])
		if ferr != nil {
			return valid, frames, nil
		}
		ops, perr := parseOps(payload)
		if perr != nil {
			return valid, frames, nil
		}
		if apply != nil {
			if aerr := apply(ops); aerr != nil {
				return valid, frames, aerr
			}
		}
		valid += n
		frames++
	}
	return valid, frames, nil
}
