package scene

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// MotionFrame carries GPU-evaluated motion and animation state for retained
// InstancedGLBMeshIR crowd batches: a previous and a next transform with
// scene-clock timestamps, plus an animation clip start time, loop flag, and
// playback rate. Unlike PoseFrame (GSP2), the renderer interpolates the
// transform and derives the animation pose EVERY RENDERED FRAME from a
// single "now" uniform, so the caller uploads a MotionFrame only when an
// instance's motion or animation state actually changes -- typically once
// per network snapshot (10-20Hz), not once per rendered frame (60Hz).
//
// MotionFrame is a new, backward-compatible channel. It does not replace
// PoseFrame: a mount that has not opted into the GPU-motion crowd path (or a
// backend that has not implemented it) keeps applying PoseFrame exactly as
// before. IDs must match an already-mounted InstancedGLBMesh batch in the
// same instance order, exactly like PoseFrame. ParentMatrix is unsupported;
// use a JSON scene command when it changes. Send EncodeMotionFrame's bytes to
// window.__gosx.scene3d.dispatchMotionFrame(target, bytes, options); supply
// options.fallbackCommands or options.fallbackPoseFrame for a route that
// still works when GPU-motion application is unavailable.
type MotionFrame struct {
	Batches []MotionBatch
}

type MotionBatch struct {
	ID        string
	Instances []MotionInstanceIR
}

// MotionInstanceIR holds one instance's motion key (a previous and a next
// transform, each with a scene-clock timestamp) and its GPU-evaluated
// animation state. The transform fields mirror MeshInstanceIR's X/Y/Z,
// Rotation*, and Scale* fields exactly, once for the previous sample and once
// for the next.
//
// The renderer interpolates Prev->Next linearly across [TPrev, TNext] (with
// rotation lerp taking the shortest angular path per axis, matching
// sceneCrowdMotionTransform), clamps to Prev before TPrev, and extrapolates
// briefly past TNext before holding -- see
// SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS in animation.ts for the
// documented extrapolation window.
//
// Animation carries the clip name; empty means no animation (bind pose).
// ClipStartTime is the scene-clock time the clip began playing, so elapsed
// clip time is always derived as (now-ClipStartTime)*PlaybackRate -- the
// renderer never receives a continuously-updated "animation time" the way
// PoseFrame's AnimationTime is. AnimationLoop and PlaybackRate behave like
// their PoseFrame counterparts (PlaybackRate defaults to 1 when unset by a
// caller that only fills AnimationTime-style call sites; this type always
// encodes the exact value given).
type MotionInstanceIR struct {
	ID string

	PrevX, PrevY, PrevZ                         float64
	PrevRotationX, PrevRotationY, PrevRotationZ float64
	PrevScaleX, PrevScaleY, PrevScaleZ          float64
	TPrev                                       float64

	NextX, NextY, NextZ                         float64
	NextRotationX, NextRotationY, NextRotationZ float64
	NextScaleX, NextScaleY, NextScaleZ          float64
	TNext                                       float64

	Animation     string
	ClipStartTime float64
	AnimationLoop bool
	PlaybackRate  float64

	ParentMatrix []float64
}

// MotionFrameEncoder reuses its buffers and validation tables across frames.
// It is not safe for concurrent use. The returned bytes remain valid only
// until the next Encode call; dispatch copies them into a JS-owned array
// first.
type MotionFrameEncoder struct {
	out           []byte
	clips         []string
	clipIndex     map[string]uint16
	seenBatches   map[string]bool
	seenInstances map[string]bool
}

// EncodeMotionFrame writes the GSP3 little-endian wire format. A frame starts
// with a per-frame animation clip dictionary (index zero is the empty clip),
// exactly like GSP2. Each instance stores an ID, eighteen float32 transform
// values (nine for the previous sample, nine for the next), two float32
// timestamps, an animation clip index, a clip start time, a loop byte, and a
// playback rate.
func EncodeMotionFrame(frame MotionFrame) ([]byte, error) {
	return new(MotionFrameEncoder).Encode(frame)
}

// Encode writes one motion frame, reusing storage held by the encoder.
func (e *MotionFrameEncoder) Encode(frame MotionFrame) ([]byte, error) {
	if len(frame.Batches) > math.MaxUint16 {
		return nil, errors.New("scene motion frame has too many batches")
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
			return errors.New("scene motion frame requires a bounded nonempty ID")
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
				return nil, errors.New("scene motion frame has too many clips")
			}
			e.clipIndex[instance.Animation] = uint16(len(e.clips))
			e.clips = append(e.clips, instance.Animation)
		}
	}
	// The wire size is known after the clip dictionary is collected. Reserve
	// it once so dense crowds do not repeatedly grow/copy the output buffer.
	capacity := uint64(8)
	for _, clip := range e.clips[1:] {
		capacity += uint64(2 + len(clip))
	}
	for _, batch := range frame.Batches {
		capacity += uint64(4 + len(batch.ID))
		for _, instance := range batch.Instances {
			capacity += uint64(93 + len(instance.ID))
		}
	}
	if capacity > uint64(^uint(0)>>1) {
		return nil, errors.New("scene motion frame exceeds addressable size")
	}
	if cap(e.out) < int(capacity) {
		e.out = make([]byte, 0, int(capacity))
	}
	out = e.out[:6]
	copy(out, "GSP3")
	binary.LittleEndian.PutUint16(out[4:], uint16(len(e.clips)-1))
	for _, clip := range e.clips[1:] {
		if err := appendID(clip); err != nil {
			return nil, err
		}
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(frame.Batches)))
	for _, batch := range frame.Batches {
		if e.seenBatches[batch.ID] {
			return nil, errors.New("scene motion frame has duplicate batch ID")
		}
		e.seenBatches[batch.ID] = true
		if err := appendID(batch.ID); err != nil {
			return nil, err
		}
		if len(batch.Instances) > math.MaxUint16 {
			return nil, errors.New("scene motion frame has too many instances")
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(batch.Instances)))
		clear(e.seenInstances)
		for _, instance := range batch.Instances {
			if len(instance.ParentMatrix) != 0 {
				return nil, errors.New("scene motion frame cannot encode parentMatrix; use JSON scene commands")
			}
			if e.seenInstances[instance.ID] {
				return nil, errors.New("scene motion frame has duplicate instance ID")
			}
			e.seenInstances[instance.ID] = true
			if err := appendID(instance.ID); err != nil {
				return nil, err
			}
			if math.IsNaN(instance.TPrev) || math.IsInf(instance.TPrev, 0) ||
				math.IsNaN(instance.TNext) || math.IsInf(instance.TNext, 0) {
				return nil, errors.New("scene motion frame requires finite float32 values")
			}
			if !(float32(instance.TNext) > float32(instance.TPrev)) {
				return nil, errors.New("scene motion frame requires TNext after TPrev")
			}
			transforms := [...]float64{
				instance.PrevX, instance.PrevY, instance.PrevZ,
				instance.PrevRotationX, instance.PrevRotationY, instance.PrevRotationZ,
				instance.PrevScaleX, instance.PrevScaleY, instance.PrevScaleZ,
			}
			for _, value := range transforms {
				if err := appendFinite32(&out, value); err != nil {
					return nil, err
				}
			}
			if err := appendFinite32(&out, instance.TPrev); err != nil {
				return nil, err
			}
			nextTransforms := [...]float64{
				instance.NextX, instance.NextY, instance.NextZ,
				instance.NextRotationX, instance.NextRotationY, instance.NextRotationZ,
				instance.NextScaleX, instance.NextScaleY, instance.NextScaleZ,
			}
			for _, value := range nextTransforms {
				if err := appendFinite32(&out, value); err != nil {
					return nil, err
				}
			}
			if err := appendFinite32(&out, instance.TNext); err != nil {
				return nil, err
			}
			out = binary.LittleEndian.AppendUint16(out, e.clipIndex[instance.Animation])
			if err := appendFinite32(&out, instance.ClipStartTime); err != nil {
				return nil, err
			}
			if instance.AnimationLoop {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
			if err := appendFinite32(&out, instance.PlaybackRate); err != nil {
				return nil, err
			}
		}
	}
	e.out = out
	return out, nil
}

// appendFinite32 converts value to float32, rejects NaN/Inf (in either the
// float64 input or the narrowed float32), and appends its little-endian wire
// bytes to *out.
func appendFinite32(out *[]byte, value float64) error {
	converted := float32(value)
	if math.IsNaN(value) || math.IsInf(value, 0) || math.IsInf(float64(converted), 0) {
		return errors.New("scene motion frame requires finite float32 values")
	}
	*out = binary.LittleEndian.AppendUint32(*out, math.Float32bits(converted))
	return nil
}
