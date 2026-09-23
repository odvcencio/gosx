package scene

import (
	"encoding/binary"
	"errors"
	"math"
)

// PoseFrame carries pose updates for retained InstancedGLBMeshIR
// batches, including their animation clip, time, and loop state. Membership,
// appearance, and assets still use scene commands. IDs must match an already
// mounted batch exactly. Send EncodePoseFrame's bytes to
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

// EncodePoseFrame writes the GSP2 little-endian wire format. A frame starts
// with a per-frame animation clip dictionary (index zero is the empty clip).
// Each instance stores an ID, nine float32 transform values, animation time,
// one animation clip index, and a loop byte.
func EncodePoseFrame(frame PoseFrame) ([]byte, error) {
	if len(frame.Batches) > math.MaxUint16 {
		return nil, errors.New("scene pose frame has too many batches")
	}
	out := make([]byte, 6)
	copy(out, "GSP2")
	clips := []string{""}
	clipIndex := map[string]uint16{"": 0}
	seenBatches := make(map[string]bool, len(frame.Batches))
	appendID := func(id string) error {
		if id == "" || len(id) > math.MaxUint16 {
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
			if _, exists := clipIndex[instance.Animation]; exists {
				continue
			}
			if len(clips) > math.MaxUint16 {
				return nil, errors.New("scene pose frame has too many clips")
			}
			clipIndex[instance.Animation] = uint16(len(clips))
			clips = append(clips, instance.Animation)
		}
	}
	binary.LittleEndian.PutUint16(out[4:], uint16(len(clips)-1))
	for _, clip := range clips[1:] {
		if err := appendID(clip); err != nil {
			return nil, err
		}
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(frame.Batches)))
	for _, batch := range frame.Batches {
		if seenBatches[batch.ID] {
			return nil, errors.New("scene pose frame has duplicate batch ID")
		}
		seenBatches[batch.ID] = true
		if err := appendID(batch.ID); err != nil {
			return nil, err
		}
		if len(batch.Instances) > math.MaxUint16 {
			return nil, errors.New("scene pose frame has too many instances")
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(batch.Instances)))
		seenInstances := make(map[string]bool, len(batch.Instances))
		for _, instance := range batch.Instances {
			if seenInstances[instance.ID] {
				return nil, errors.New("scene pose frame has duplicate instance ID")
			}
			seenInstances[instance.ID] = true
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
			out = binary.LittleEndian.AppendUint16(out, clipIndex[instance.Animation])
			if instance.AnimationLoop {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	return out, nil
}
