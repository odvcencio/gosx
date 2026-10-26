package netplay

import (
	"encoding/binary"
	"time"
)

// Wire format, version 1. All integers are little endian. Frame numbers on
// the wire are uint32. Every packet starts with a version byte and a type
// byte. There is no pause packet type: a peer that stops sending stalls the
// other peer, and the stall timeout ends the match.
const (
	protoVersion = 1

	ptHello     = 1 // peer -> peer: identity and clock echo
	ptInput     = 2 // peer -> peer: redundant inputs, ack, checksums
	ptResync    = 3 // relay -> peer: one fragment of a Save blob
	ptResyncAck = 4 // peer -> relay: blob applied
	ptEnd       = 5 // relay -> peer: match over
	ptSpectate  = 6 // relay -> spectator: confirmed inputs
	ptSpecAck   = 7 // spectator -> relay: next frame wanted

	maxSums = 4

	// Common header of Hello and Input.
	offDelay = 10
	offFlags = 11
	offSeq   = 12
	offSend  = 16
	offEcho  = 20
	offHold  = 24
	// Hello body.
	offHelloPlayer = 28
	offHelloID     = 29
	helloLen       = 29 + 32
	// Input body.
	offFrame  = 28
	offAck    = 32
	offStart  = 36
	offCount  = 40
	offNSums  = 42
	inputHdr  = 43
	sumLen    = 13
	flagEcho  = 1
	delayAuto = 0xFF

	resyncHdr   = 33
	resyncChunk = 1000
)

// End reasons carried by a ptEnd packet.
const (
	EndDesync = 1
)

type sumEntry struct {
	frame uint32
	sum   uint64
	epoch uint8
}

// header is the decoded common part of Hello and Input packets.
type header struct {
	typ     byte
	delay   byte
	hasEcho bool
	seq     uint32
	sendUs  uint32
	echoUs  uint32
	holdUs  uint32
}

func idTag(id *[32]byte) uint64 { return binary.LittleEndian.Uint64(id[:8]) }

func putHeader(b []byte, typ byte, tag uint64, h *header) {
	b[0], b[1] = protoVersion, typ
	binary.LittleEndian.PutUint64(b[2:], tag)
	b[offDelay] = h.delay
	var fl byte
	if h.hasEcho {
		fl |= flagEcho
	}
	b[offFlags] = fl
	binary.LittleEndian.PutUint32(b[offSeq:], h.seq)
	binary.LittleEndian.PutUint32(b[offSend:], h.sendUs)
	binary.LittleEndian.PutUint32(b[offEcho:], h.echoUs)
	binary.LittleEndian.PutUint32(b[offHold:], h.holdUs)
}

func getHeader(b []byte) header {
	return header{
		typ:     b[1],
		delay:   b[offDelay],
		hasEcho: b[offFlags]&flagEcho != 0,
		seq:     binary.LittleEndian.Uint32(b[offSeq:]),
		sendUs:  binary.LittleEndian.Uint32(b[offSend:]),
		echoUs:  binary.LittleEndian.Uint32(b[offEcho:]),
		holdUs:  binary.LittleEndian.Uint32(b[offHold:]),
	}
}

// inputPkt is a decoded Input packet. entries aliases the source buffer.
type inputPkt struct {
	header
	frame   uint32
	ack     uint32
	start   uint32
	count   int
	entries []byte
	nsums   int
	sums    [maxSums]sumEntry
}

// parseInput decodes an Input packet whose entries are isz bytes each.
func parseInput(b []byte, isz int) (p inputPkt, tag uint64, ok bool) {
	if len(b) < inputHdr || b[0] != protoVersion || b[1] != ptInput {
		return p, 0, false
	}
	p.header = getHeader(b)
	tag = binary.LittleEndian.Uint64(b[2:])
	p.frame = binary.LittleEndian.Uint32(b[offFrame:])
	p.ack = binary.LittleEndian.Uint32(b[offAck:])
	p.start = binary.LittleEndian.Uint32(b[offStart:])
	p.count = int(binary.LittleEndian.Uint16(b[offCount:]))
	p.nsums = int(b[offNSums])
	if p.nsums > maxSums || len(b) != inputHdr+p.count*isz+p.nsums*sumLen {
		return p, 0, false
	}
	p.entries = b[inputHdr : inputHdr+p.count*isz]
	o := inputHdr + p.count*isz
	for i := 0; i < p.nsums; i++ {
		p.sums[i] = sumEntry{
			frame: binary.LittleEndian.Uint32(b[o:]),
			sum:   binary.LittleEndian.Uint64(b[o+4:]),
			epoch: b[o+12],
		}
		o += sumLen
	}
	return p, tag, true
}

// maxEntries is how many inputs of size isz fit in one Input packet.
func maxEntries(isz int) int {
	return (MaxPacketSize - inputHdr - maxSums*sumLen) / isz
}

func micros(d time.Duration) uint32 { return uint32(d / time.Microsecond) }

// resyncPkt is a decoded Resync fragment.
type resyncPkt struct {
	epoch  uint8
	frame  uint32
	total  uint32
	offset uint32
	sum    uint64
	data   []byte
}

func putResync(b []byte, tag uint64, p *resyncPkt) []byte {
	b = b[:resyncHdr+len(p.data)]
	b[0], b[1] = protoVersion, ptResync
	binary.LittleEndian.PutUint64(b[2:], tag)
	b[10] = p.epoch
	binary.LittleEndian.PutUint32(b[11:], p.frame)
	binary.LittleEndian.PutUint32(b[15:], p.total)
	binary.LittleEndian.PutUint32(b[19:], p.offset)
	binary.LittleEndian.PutUint64(b[23:], p.sum)
	binary.LittleEndian.PutUint16(b[31:], uint16(len(p.data)))
	copy(b[resyncHdr:], p.data)
	return b
}

func parseResync(b []byte) (p resyncPkt, tag uint64, ok bool) {
	if len(b) < resyncHdr || b[0] != protoVersion || b[1] != ptResync {
		return p, 0, false
	}
	n := int(binary.LittleEndian.Uint16(b[31:]))
	if len(b) != resyncHdr+n {
		return p, 0, false
	}
	tag = binary.LittleEndian.Uint64(b[2:])
	p.epoch = b[10]
	p.frame = binary.LittleEndian.Uint32(b[11:])
	p.total = binary.LittleEndian.Uint32(b[15:])
	p.offset = binary.LittleEndian.Uint32(b[19:])
	p.sum = binary.LittleEndian.Uint64(b[23:])
	p.data = b[resyncHdr:]
	return p, tag, true
}

// Small fixed packets: ResyncAck {epoch u8, frame u32}, End {reason u8},
// SpecAck {next u32}.
func putSmall(b []byte, typ byte, tag uint64, v1 byte, v2 uint32) []byte {
	b = b[:16]
	b[0], b[1] = protoVersion, typ
	binary.LittleEndian.PutUint64(b[2:], tag)
	b[10] = v1
	binary.LittleEndian.PutUint32(b[11:], v2)
	b[15] = 0
	return b
}

func parseSmall(b []byte, typ byte) (tag uint64, v1 byte, v2 uint32, ok bool) {
	if len(b) != 16 || b[0] != protoVersion || b[1] != typ {
		return 0, 0, 0, false
	}
	return binary.LittleEndian.Uint64(b[2:]), b[10], binary.LittleEndian.Uint32(b[11:]), true
}
