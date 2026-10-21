package scene

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// VesselPose is one authoritative ship pose from Go simulation (for example
// from package vessel). The Scene3D runtime shows a vessel with
// Vessel.ExternalPose set at this pose, smoothed between ticks.
//
// X, Y and Z are world metres of the hull origin at the waterline. Heading,
// Pitch and Roll are radians in the sailing vessel's axes: heading 0 faces -Z,
// positive pitch lifts the bow, positive roll lifts the starboard side. Speed
// is metres per second. Time is simulation seconds and is used only to order
// frames. Snap makes the renderer jump to the pose instead of blending, for a
// teleport or the first frame.
type VesselPose struct {
	ID                   string // Vessel.NodeID
	Time                 float64
	X, Y, Z              float64
	Heading, Pitch, Roll float32
	Speed                float32
	Snap                 bool
}

// VesselPoseFrame is the set of ship poses for one simulation tick.
type VesselPoseFrame struct {
	Vessels []VesselPose
}

const (
	vesselPoseMagic    = "GSV1"
	vesselPoseFixedLen = 1 + 2 + 8 + 3*8 + 4*4 // flags, id length, time, xyz, heading pitch roll speed
	// MaxVesselPoses bounds the ships in one frame.
	MaxVesselPoses = 1024
)

// VesselPoseFrameSize returns the encoded size of a frame in bytes.
func VesselPoseFrameSize(f VesselPoseFrame) int {
	n := len(vesselPoseMagic) + 2
	for _, v := range f.Vessels {
		n += vesselPoseFixedLen + len(v.ID)
	}
	return n
}

// EncodeVesselPoseFrame appends the GSV1 encoding of f to dst. The wire format
// is little-endian:
//
//	"GSV1", u16 count, then per ship:
//	  u8 flags (bit 0 = snap), u16 id length, id bytes (UTF-8),
//	  f64 time, f64 x, f64 y, f64 z, f32 heading, f32 pitch, f32 roll, f32 speed
//
// A frame with one ship whose ID has n bytes takes 57+n bytes.
func EncodeVesselPoseFrame(dst []byte, f VesselPoseFrame) ([]byte, error) {
	if len(f.Vessels) > MaxVesselPoses {
		return dst, fmt.Errorf("scene: vessel pose frame has %d ships, limit %d", len(f.Vessels), MaxVesselPoses)
	}
	dst = append(dst, vesselPoseMagic...)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(f.Vessels)))
	for _, v := range f.Vessels {
		if v.ID == "" || len(v.ID) > math.MaxUint16 || !utf8.ValidString(v.ID) {
			return dst, errors.New("scene: vessel pose needs a UTF-8 ID of 1..65535 bytes")
		}
		var flags byte
		if v.Snap {
			flags = 1
		}
		dst = append(dst, flags)
		dst = binary.LittleEndian.AppendUint16(dst, uint16(len(v.ID)))
		dst = append(dst, v.ID...)
		for _, x := range [...]float64{v.Time, v.X, v.Y, v.Z} {
			dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(x))
		}
		for _, x := range [...]float32{v.Heading, v.Pitch, v.Roll, v.Speed} {
			dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(x))
		}
	}
	return dst, nil
}

// DecodeVesselPoseFrame parses a GSV1 frame. It rejects trailing bytes.
func DecodeVesselPoseFrame(b []byte) (VesselPoseFrame, error) {
	var f VesselPoseFrame
	if len(b) < 6 || string(b[:4]) != vesselPoseMagic {
		return f, errors.New("scene: not a vessel pose frame")
	}
	count := int(binary.LittleEndian.Uint16(b[4:]))
	if count > MaxVesselPoses {
		return f, errors.New("scene: vessel pose frame has too many ships")
	}
	b = b[6:]
	for i := 0; i < count; i++ {
		if len(b) < 3 {
			return f, errors.New("scene: truncated vessel pose frame")
		}
		flags, n := b[0], int(binary.LittleEndian.Uint16(b[1:]))
		if len(b) < vesselPoseFixedLen+n {
			return f, errors.New("scene: truncated vessel pose frame")
		}
		v := VesselPose{ID: string(b[3 : 3+n]), Snap: flags&1 != 0}
		if v.ID == "" || !utf8.ValidString(v.ID) {
			return f, errors.New("scene: vessel pose has an invalid ID")
		}
		p := b[3+n:]
		f64 := func(k int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(p[8*k:])) }
		f32 := func(k int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(p[32+4*k:])) }
		v.Time, v.X, v.Y, v.Z = f64(0), f64(1), f64(2), f64(3)
		v.Heading, v.Pitch, v.Roll, v.Speed = f32(0), f32(1), f32(2), f32(3)
		f.Vessels = append(f.Vessels, v)
		b = b[vesselPoseFixedLen+n:]
	}
	if len(b) != 0 {
		return f, errors.New("scene: trailing bytes after vessel pose frame")
	}
	return f, nil
}
