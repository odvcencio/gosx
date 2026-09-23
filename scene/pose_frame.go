package scene

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// PoseFrame carries pose updates for retained InstancedGLBMeshIR
// batches, including their animation clip, time, and loop state. Membership,
// appearance, and assets still use scene commands. IDs must match an already
// mounted batch in the same instance order. ParentMatrix is unsupported; use
// a JSON scene command when it changes. Send EncodePoseFrame's bytes to
// window.__gosx.scene3d.dispatchPoseFrame(target, bytes, options); supply
// options.fallbackCommands for the existing JSON command route when retained
// application is unavailable.
type PoseFrame struct {
	Batches []PoseBatch
}

type PoseBatch struct {
	ID        string
	Instances []MeshInstanceIR
}

// PoseFrameEncoder reuses its buffers and validation tables across frames.
// It is not safe for concurrent use. The returned bytes remain valid only
// until the next Encode call; dispatch copies them into a JS-owned array first.
type PoseFrameEncoder struct {
	out           []byte
	clips         []string
	clipIndex     map[string]uint16
	seenBatches   map[string]bool
	seenInstances map[string]bool
}

// EncodePoseFrame writes the GSP2 little-endian wire format. A frame starts
// with a per-frame animation clip dictionary (index zero is the empty clip).
// Each instance stores an ID, nine float32 transform values, animation time,
// one animation clip index, and a loop byte.
func EncodePoseFrame(frame PoseFrame) ([]byte, error) {
	return new(PoseFrameEncoder).Encode(frame)
}

// Encode writes one pose frame, reusing storage held by the encoder.
func (e *PoseFrameEncoder) Encode(frame PoseFrame) ([]byte, error) {
	if len(frame.Batches) > math.MaxUint16 {
		return nil, errors.New("scene pose frame has too many batches")
	}
	e.clips = e.clips[:0]
	e.clips = append(e.clips, "")
	if e.clipIndex == nil {
		e.clipIndex = make(map[string]uint16)
	} else {
		clear(e.clipIndex)
	}
	e.clipIndex[""] = 0
	if e.seenBatches == nil {
		e.seenBatches = make(map[string]bool, len(frame.Batches))
	} else {
		clear(e.seenBatches)
	}
	if e.seenInstances == nil {
		maxInstances := 0
		for _, batch := range frame.Batches {
			if len(batch.Instances) > maxInstances {
				maxInstances = len(batch.Instances)
			}
		}
		e.seenInstances = make(map[string]bool, maxInstances)
	}
	var out []byte
	appendID := func(id string) error {
		if id == "" || len(id) > math.MaxUint16 || !utf8.ValidString(id) {
			return errors.New("scene pose frame requires a bounded nonempty ID")
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(id)))
		out = append(out, id...)
		return nil
	}
	for _, batch := range frame.Batches {
		for _, instance := range batch.Instances {
			if instance.Animation == "" {
				continue
			}
			if _, exists := e.clipIndex[instance.Animation]; exists {
				continue
			}
			if len(e.clips) > math.MaxUint16 {
				return nil, errors.New("scene pose frame has too many clips")
			}
			e.clipIndex[instance.Animation] = uint16(len(e.clips))
			e.clips = append(e.clips, instance.Animation)
		}
	}
	// The wire size is known after the clip dictionary is collected. Reserve it
	// once so dense crowds do not repeatedly grow/copy the output buffer.
	capacity := uint64(8)
	for _, clip := range e.clips[1:] {
		capacity += uint64(2 + len(clip))
	}
	for _, batch := range frame.Batches {
		capacity += uint64(4 + len(batch.ID))
		for _, instance := range batch.Instances {
			capacity += uint64(45 + len(instance.ID))
		}
	}
	if capacity > uint64(^uint(0)>>1) {
		return nil, errors.New("scene pose frame exceeds addressable size")
	}
	if cap(e.out) < int(capacity) {
		e.out = make([]byte, 0, int(capacity))
	}
	out = e.out[:6]
	copy(out, "GSP2")
	binary.LittleEndian.PutUint16(out[4:], uint16(len(e.clips)-1))
	for _, clip := range e.clips[1:] {
		if err := appendID(clip); err != nil {
			return nil, err
		}
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(frame.Batches)))
	for _, batch := range frame.Batches {
		if e.seenBatches[batch.ID] {
			return nil, errors.New("scene pose frame has duplicate batch ID")
		}
		e.seenBatches[batch.ID] = true
		if err := appendID(batch.ID); err != nil {
			return nil, err
		}
		if len(batch.Instances) > math.MaxUint16 {
			return nil, errors.New("scene pose frame has too many instances")
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(batch.Instances)))
		clear(e.seenInstances)
		for _, instance := range batch.Instances {
			if len(instance.ParentMatrix) != 0 {
				return nil, errors.New("scene pose frame cannot encode parentMatrix; use JSON scene commands")
			}
			if e.seenInstances[instance.ID] {
				return nil, errors.New("scene pose frame has duplicate instance ID")
			}
			e.seenInstances[instance.ID] = true
			if err := appendID(instance.ID); err != nil {
				return nil, err
			}
			for _, value := range [...]float64{instance.X, instance.Y, instance.Z, instance.RotationX, instance.RotationY, instance.RotationZ, instance.ScaleX, instance.ScaleY, instance.ScaleZ} {
				converted := float32(value)
				if math.IsNaN(value) || math.IsInf(value, 0) || math.IsInf(float64(converted), 0) {
					return nil, errors.New("scene pose frame requires finite float32 transforms")
				}
				out = binary.LittleEndian.AppendUint32(out, math.Float32bits(converted))
			}
			animationTime := float32(instance.AnimationTime)
			if math.IsNaN(instance.AnimationTime) || math.IsInf(instance.AnimationTime, 0) || math.IsInf(float64(animationTime), 0) || animationTime < 0 {
				return nil, errors.New("scene pose frame requires finite nonnegative animation time")
			}
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(animationTime))
			out = binary.LittleEndian.AppendUint16(out, e.clipIndex[instance.Animation])
			if instance.AnimationLoop {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	e.out = out
	return out, nil
}
