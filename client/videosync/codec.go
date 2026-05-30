// Package videosync — binary sync codec, byte-identical to goetrope's wire format.
// TinyGo-clean: no time.Now/time.Since, no encoding/json, no syscall/js.
package videosync

import (
	"encoding/binary"
	"math"
)

// Frame-kind byte constants (matches goetrope/pkg/theatre/protocol.go).
const (
	kindHeartbeat   = 0x01
	kindDriftReport = 0x03
	kindPong        = 0x04
	kindPing        = 0x05
)

// Heartbeat carries a server-side sync snapshot.
// Wire layout (16 bytes, all big-endian):
//
//	[0]      = 0x01
//	[1:9]    = ServerTimeMs  uint64
//	[9:13]   = Position      float32 (math.Float32bits, big-endian)
//	[13]     = Playing       uint8  (1 = true, 0 = false)
//	[14:16]  = ViewerCount   uint16
type Heartbeat struct {
	ServerTimeMs uint64
	Position     float32
	Playing      bool
	ViewerCount  uint16
}

// EncodeHeartbeat serialises h into a 16-byte slice.
func EncodeHeartbeat(h Heartbeat) []byte {
	buf := make([]byte, 16)
	buf[0] = kindHeartbeat
	binary.BigEndian.PutUint64(buf[1:9], h.ServerTimeMs)
	binary.BigEndian.PutUint32(buf[9:13], math.Float32bits(h.Position))
	if h.Playing {
		buf[13] = 1
	}
	binary.BigEndian.PutUint16(buf[14:16], h.ViewerCount)
	return buf
}

// DecodeHeartbeat parses a Heartbeat from b.
// Returns ok=false if len(b)<16 or b[0]!=0x01.
func DecodeHeartbeat(b []byte) (Heartbeat, bool) {
	if len(b) < 16 || b[0] != kindHeartbeat {
		return Heartbeat{}, false
	}
	return Heartbeat{
		ServerTimeMs: binary.BigEndian.Uint64(b[1:9]),
		Position:     math.Float32frombits(binary.BigEndian.Uint32(b[9:13])),
		Playing:      b[13] == 1,
		ViewerCount:  binary.BigEndian.Uint16(b[14:16]),
	}, true
}

// EncodePing serialises a ping timestamp into a 9-byte slice.
// Wire layout: [0]=0x05, [1:9]=timestampMs uint64 big-endian.
func EncodePing(timestampMs uint64) []byte {
	buf := make([]byte, 9)
	buf[0] = kindPing
	binary.BigEndian.PutUint64(buf[1:9], timestampMs)
	return buf
}

// EncodePong serialises a pong (echoed timestamp) into a 9-byte slice.
// Wire layout: [0]=0x04, [1:9]=echoedMs uint64 big-endian.
func EncodePong(echoedMs uint64) []byte {
	buf := make([]byte, 9)
	buf[0] = kindPong
	binary.BigEndian.PutUint64(buf[1:9], echoedMs)
	return buf
}

// DecodePong parses a Pong frame from b.
// Returns ok=false if len(b)<9 or b[0]!=0x04.
func DecodePong(b []byte) (echoedMs uint64, ok bool) {
	if len(b) < 9 || b[0] != kindPong {
		return 0, false
	}
	return binary.BigEndian.Uint64(b[1:9]), true
}

// EncodeDriftReport serialises a drift value into a 5-byte slice.
// Wire layout: [0]=0x03, [1:5]=drift float32 (math.Float32bits, big-endian).
func EncodeDriftReport(drift float32) []byte {
	buf := make([]byte, 5)
	buf[0] = kindDriftReport
	binary.BigEndian.PutUint32(buf[1:5], math.Float32bits(drift))
	return buf
}

// FrameKind returns the frame-kind byte (b[0]).
// Returns 0 for nil or empty input — never panics.
func FrameKind(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
