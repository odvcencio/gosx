package scene

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// PartialMotionFrame is the slot-indexed sibling of MotionFrame (wire version
// GSP5). It carries only the agents whose motion or animation state changed,
// each named by its slot: its zero-based position in the mounted
// InstancedGLBMesh batch's Instances list. Agents it omits keep their last
// motion key, so a crowd of thousands where a few percent move per network
// tick costs bytes in proportion to the movers, not to the crowd.
//
// A PartialMotionFrame never creates, removes or reorders agents. The client
// rejects a batch whose Size differs from the mounted instance count, which
// catches a frame built against a stale membership. After a membership
// change, send a full MotionFrame (or the JSON membership command followed by
// one) to restate every agent, then resume partial frames.
//
// MotionFrame stays the format for the first frame of a batch and for resync.
// Both formats go through the same dispatch call
// (window.__gosx.scene3d.dispatchMotionFrame); the client picks the decoder
// from the four-byte magic.
type PartialMotionFrame struct {
	Batches []PartialMotionBatch
}

// PartialMotionBatch lists the changed agents of one mounted batch. Size is
// the batch's total instance count at the time the frame was built.
type PartialMotionBatch struct {
	ID      string
	Size    int
	Updates []PartialMotionUpdate
}

// PartialMotionUpdate replaces the motion state of the agent in Slot. The
// Motion.ID field is ignored: the slot names the agent. Motion.ParentMatrix
// must be empty, as for MotionFrame.
type PartialMotionUpdate struct {
	Slot   int
	Motion MotionInstanceIR
}

// Partial selects the instances of a full MotionBatch at the given slots and
// returns them as a PartialMotionBatch of the same batch. Slots must be in
// range; EncodePartialMotionFrame requires them to ascend strictly.
func (b MotionBatch) Partial(slots []int) PartialMotionBatch {
	updates := make([]PartialMotionUpdate, 0, len(slots))
	for _, slot := range slots {
		if slot >= 0 && slot < len(b.Instances) {
			updates = append(updates, PartialMotionUpdate{Slot: slot, Motion: b.Instances[slot]})
		} else {
			updates = append(updates, PartialMotionUpdate{Slot: slot})
		}
	}
	return PartialMotionBatch{ID: b.ID, Size: len(b.Instances), Updates: updates}
}

// EncodePartialMotionFrame writes the GSP5 little-endian wire format.
//
//	"GSP5"
//	u16 clipCount, clipCount x (u16 length, UTF-8 name)   // index 0 is the empty clip
//	u16 batchCount
//	batchCount x {
//	    u16 length, UTF-8 batch ID
//	    u16 size             // instances in the whole batch
//	    u16 changedCount
//	    changedCount x {
//	        u16 slot         // strictly ascending, below size
//	        20 x float32     // GSP3 motion key: prev(9) tPrev next(9) tNext
//	        u16 clip index, float32 clipStartTime, u8 loop, float32 playbackRate
//	    }
//	}
func EncodePartialMotionFrame(frame PartialMotionFrame) ([]byte, error) {
	return new(MotionFrameEncoder).EncodePartial(frame)
}

// EncodePartial writes one GSP5 frame, reusing storage held by the encoder.
// Like Encode, the returned bytes stay valid until the next call.
func (e *MotionFrameEncoder) EncodePartial(frame PartialMotionFrame) ([]byte, error) {
	if len(frame.Batches) > math.MaxUint16 {
		return nil, errors.New("scene motion frame has too many batches")
	}
	e.clips = append(e.clips[:0], "")
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
	capacity := uint64(8)
	for _, batch := range frame.Batches {
		capacity += uint64(8+len(batch.ID)) + uint64(len(batch.Updates))*93
		for _, update := range batch.Updates {
			name := update.Motion.Animation
			if name == "" {
				continue
			}
			if _, exists := e.clipIndex[name]; exists {
				continue
			}
			if len(e.clips) > math.MaxUint16 {
				return nil, errors.New("scene motion frame has too many clips")
			}
			e.clipIndex[name] = uint16(len(e.clips))
			e.clips = append(e.clips, name)
			capacity += uint64(2 + len(name))
		}
	}
	if capacity > uint64(^uint(0)>>1) {
		return nil, errors.New("scene motion frame exceeds addressable size")
	}
	if cap(e.out) < int(capacity) {
		e.out = make([]byte, 0, int(capacity))
	}
	out := e.out[:6]
	copy(out, "GSP5")
	binary.LittleEndian.PutUint16(out[4:], uint16(len(e.clips)-1))
	appendID := func(id string) error {
		if id == "" || len(id) > math.MaxUint16 || !utf8.ValidString(id) {
			return errors.New("scene motion frame requires a bounded nonempty ID")
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(id)))
		out = append(out, id...)
		return nil
	}
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
		if batch.Size < 0 || batch.Size > math.MaxUint16 {
			return nil, errors.New("scene motion frame batch size is out of range")
		}
		if len(batch.Updates) > batch.Size {
			return nil, errors.New("scene motion frame has more updates than batch instances")
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(batch.Size))
		out = binary.LittleEndian.AppendUint16(out, uint16(len(batch.Updates)))
		previous := -1
		for _, update := range batch.Updates {
			if update.Slot <= previous {
				return nil, errors.New("scene motion frame slots must ascend strictly")
			}
			if update.Slot >= batch.Size {
				return nil, errors.New("scene motion frame slot is outside the batch")
			}
			previous = update.Slot
			if len(update.Motion.ParentMatrix) != 0 {
				return nil, errors.New("scene motion frame cannot encode a per-instance parentMatrix; set InstancedGLBMesh.ParentNode for a batch-level parent, or use JSON scene commands")
			}
			out = binary.LittleEndian.AppendUint16(out, uint16(update.Slot))
			var err error
			if out, err = appendMotionPayload(out, update.Motion, e.clipIndex[update.Motion.Animation]); err != nil {
				return nil, err
			}
		}
	}
	e.out = out
	return out, nil
}
