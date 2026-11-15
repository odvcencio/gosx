package animgraph

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// FrameMagic opens every animation graph frame.
const FrameMagic = "GSA2"

// FrameEncoder writes animation graph frames. It reuses its buffers, so
// steady-state encoding does not allocate. It is not safe for concurrent use,
// and the bytes Encode returns are valid only until the next call.
//
// Wire format, little-endian:
//
//	"GSA2"
//	u16 clip name count, then each name as u16 length + UTF-8 bytes
//	u16 model count, then per model:
//	    u16 length + UTF-8 model ID
//	    u32 rig hash (Program.RigHash; 0 means no rig)
//	    f32 seconds since this model's previous frame (drives smoothing)
//	    u8  flags: bits 0-1 joint detail level (LODLevel.Detail)
//	    u8  layer count, then per layer:
//	        f32 layer weight
//	        u8  sample count, then per sample (11 bytes):
//	            u16 clip name index, f32 time, f32 weight, u8 flags (bit 0 loop)
//	    u8  IK count, then per chain:
//	        u8 chain index, u8 flags (bit 0 target, 1 pole, 2 ground,
//	        3 bone of another model), f32 weight
//	        [f32 x3 target] [f32 x3 pole] [f32 height, f32 x3 normal]
//	        [u16 model index in this frame, u8 HumanoidBones index, f32 x3 offset]
//	    u8  look-at count, then per chain:
//	        u8 chain index, f32 weight, f32 x3 target
type FrameEncoder struct {
	out   []byte
	names []string
	seen  map[string]struct{}
}

// EncodeFrame encodes one frame with a fresh encoder.
func EncodeFrame(models ...*Instance) ([]byte, error) {
	return new(FrameEncoder).Encode(models...)
}

// Encode writes one frame naming every instance, in order. Instance IDs must
// be unique and nonempty.
func (e *FrameEncoder) Encode(models ...*Instance) ([]byte, error) {
	if len(models) > math.MaxUint16 {
		return nil, errors.New("animgraph: frame has too many models")
	}
	if e.seen == nil {
		e.seen = make(map[string]struct{}, len(models))
	} else {
		clear(e.seen)
	}
	e.names = e.names[:0]
	size := 8
	for _, in := range models {
		if in == nil {
			return nil, errors.New("animgraph: nil instance")
		}
		if in.id == "" || len(in.id) > math.MaxUint16 || !utf8.ValidString(in.id) {
			return nil, errors.New("animgraph: instance needs a bounded nonempty UTF-8 model ID")
		}
		if _, dup := e.seen[in.id]; dup {
			return nil, errors.New("animgraph: duplicate model ID in frame")
		}
		e.seen[in.id] = struct{}{}
		size += 2 + len(in.id) + 4 + 4 + 4
		for li := range in.layers {
			ls := &in.layers[li]
			size += 5 + ls.sampleCount*11
			for i := 0; i < ls.sampleCount; i++ {
				name := in.prog.clips[ls.samples[i].Clip].name
				if e.nameIndex(name) < 0 {
					if len(e.names) >= 0x8000 {
						return nil, errors.New("animgraph: frame has too many clip names")
					}
					e.names = append(e.names, name)
					size += 2 + len(name)
				}
			}
		}
		size += len(in.ik)*(6+12+12+16+15) + len(in.look)*17
	}
	if cap(e.out) < size {
		e.out = make([]byte, 0, size)
	}
	out := append(e.out[:0], FrameMagic...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(e.names)))
	for _, name := range e.names {
		out = binary.LittleEndian.AppendUint16(out, uint16(len(name)))
		out = append(out, name...)
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(models)))
	for _, in := range models {
		out = e.appendModel(out, in, models)
	}
	e.out = out
	return out, nil
}

// modelIndex finds an instance in the frame. The variadic slice is passed
// down rather than stored, so it stays on the caller's stack.
func modelIndex(models []*Instance, in *Instance) int {
	for i, m := range models {
		if m == in {
			return i
		}
	}
	return -1
}

func (e *FrameEncoder) appendModel(out []byte, in *Instance, models []*Instance) []byte {
	out = binary.LittleEndian.AppendUint16(out, uint16(len(in.id)))
	out = append(out, in.id...)
	out = binary.LittleEndian.AppendUint32(out, in.prog.rigHash)
	out = appendF32(out, in.sinceEncode)
	in.sinceEncode = 0
	out = append(out, in.lod.detail&3)
	out = append(out, byte(len(in.layers)))
	for li := range in.layers {
		ls := &in.layers[li]
		out = appendF32(out, ls.weight*in.lod.layerScale[li])
		out = append(out, byte(ls.sampleCount))
		for i := 0; i < ls.sampleCount; i++ {
			s := &ls.samples[i]
			out = binary.LittleEndian.AppendUint16(out, uint16(e.nameIndex(in.prog.clips[s.Clip].name)))
			out = appendF32(out, s.Time)
			out = appendF32(out, s.Weight)
			if s.Loop {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	countAt := len(out)
	out = append(out, 0)
	count := 0
	for chain := range in.ik {
		ik := &in.ik[chain]
		w := in.IKWeight(chain)
		other := -1
		if ik.BoneTarget != nil {
			other = modelIndex(models, ik.BoneTarget)
		}
		if w <= 0 || !(ik.HasTarget || ik.HasGround || other >= 0) {
			continue
		}
		var flags byte
		if other >= 0 {
			flags |= 8
		}
		if ik.HasTarget {
			flags |= 1
		}
		if ik.HasPole {
			flags |= 2
		}
		if ik.HasGround {
			flags |= 4
		}
		out = append(out, byte(chain), flags)
		out = appendF32(out, w)
		if ik.HasTarget {
			out = appendVec3(out, ik.Target)
		}
		if ik.HasPole {
			out = appendVec3(out, ik.Pole)
		}
		if ik.HasGround {
			out = appendF32(out, ik.GroundHeight)
			out = appendVec3(out, ik.GroundNormal)
		}
		if other >= 0 {
			out = binary.LittleEndian.AppendUint16(out, uint16(other))
			out = append(out, byte(ik.TargetBone))
			out = appendVec3(out, ik.TargetOffset)
		}
		count++
	}
	out[countAt] = byte(count)
	countAt = len(out)
	out = append(out, 0)
	count = 0
	for chain := range in.look {
		look := &in.look[chain]
		if look.Weight <= 0 {
			continue
		}
		out = append(out, byte(chain))
		out = appendF32(out, look.Weight)
		out = appendVec3(out, look.Target)
		count++
	}
	out[countAt] = byte(count)
	return out
}

func (e *FrameEncoder) nameIndex(name string) int {
	for i, candidate := range e.names {
		if candidate == name {
			return i
		}
	}
	return -1
}

func appendF32(out []byte, v float64) []byte {
	return binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(v)))
}

func appendVec3(out []byte, v [3]float64) []byte {
	return appendF32(appendF32(appendF32(out, v[0]), v[1]), v[2])
}

// Feedback is what the renderer measured on the pose it drew for an instance
// during the last Dispatch. Positions are in world space.
type Feedback struct {
	// Valid is true after the renderer posed the model at least once.
	Valid bool
	// RigHash is the rig the renderer applied; 0 means it did not know it.
	RigHash uint32
	// Chains and Looks count the entries below.
	Chains, Looks int
	// Animated is each IK chain's end-bone position before IK: the point to
	// raycast below for SetGround. Solved is the position after IK. Target
	// is the position the chain reached for (a bone target resolved from the
	// other model's pose), or Animated when it had none.
	Animated, Solved, Target [MaxIK][3]float64
	// LookPivot and LookForward are each look-at pivot's position and forward
	// direction after solving.
	LookPivot, LookForward [MaxLookAt][3]float64
	// Root is the world position of the model's root bone.
	Root [3]float64
	// EvalMicros is the renderer's CPU time to evaluate this model, in
	// microseconds.
	EvalMicros float64
}

// Feedback returns the renderer's last report for this instance.
func (in *Instance) Feedback() *Feedback { return &in.feedback }

// AnimatedEnd returns a chain's pre-IK end-bone position, if reported.
func (f *Feedback) AnimatedEnd(chain int) ([3]float64, bool) {
	if !f.Valid || chain < 0 || chain >= f.Chains {
		return [3]float64{}, false
	}
	return f.Animated[chain], true
}

// DecodeFeedback fills f from the renderer's feedback layout:
//
//	[rig hash, chain count N, look count M,
//	 N x (animated xyz, solved xyz, target xyz), M x (pivot xyz, forward xyz),
//	 root xyz, eval microseconds]
//
// It reports false and leaves f invalid when vals is too short.
func (f *Feedback) DecodeFeedback(vals []float64) bool {
	*f = Feedback{}
	if len(vals) < 3 {
		return false
	}
	n, m := int(vals[1]), int(vals[2])
	if n < 0 || n > MaxIK || m < 0 || m > MaxLookAt || len(vals) < 3+9*n+6*m+4 {
		return false
	}
	f.RigHash = uint32(vals[0])
	f.Chains, f.Looks = n, m
	at := 3
	for i := 0; i < n; i++ {
		copy(f.Animated[i][:], vals[at:at+3])
		copy(f.Solved[i][:], vals[at+3:at+6])
		copy(f.Target[i][:], vals[at+6:at+9])
		at += 9
	}
	for i := 0; i < m; i++ {
		copy(f.LookPivot[i][:], vals[at:at+3])
		copy(f.LookForward[i][:], vals[at+3:at+6])
		at += 6
	}
	copy(f.Root[:], vals[at:at+3])
	f.EvalMicros = vals[at+3]
	f.Valid = true
	return true
}
