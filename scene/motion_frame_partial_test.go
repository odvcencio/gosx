package scene

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

func partialSample() MotionInstanceIR {
	return MotionInstanceIR{
		ID:    "ignored",
		PrevX: 1, PrevY: 2, PrevZ: 3,
		PrevRotationX: .5, PrevRotationY: .25, PrevRotationZ: 0,
		PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1,
		TPrev: 10,
		NextX: 4, NextY: 5, NextZ: 6,
		NextRotationX: 0, NextRotationY: .75, NextRotationZ: .125,
		NextScaleX: 2, NextScaleY: 2, NextScaleZ: 2,
		TNext:         10.5,
		Animation:     "walk",
		ClipStartTime: 9.5,
		AnimationLoop: true,
		PlaybackRate:  1.5,
	}
}

// TestEncodePartialMotionFrameGoldenBytes pins the exact GSP5 byte layout. The
// TypeScript decoder test in client/js/scene3d-motion-frame.test.mjs decodes
// the same literal bytes.
func TestEncodePartialMotionFrameGoldenBytes(t *testing.T) {
	m := partialSample()
	data, err := EncodePartialMotionFrame(PartialMotionFrame{Batches: []PartialMotionBatch{{
		ID: "crowd", Size: 300, Updates: []PartialMotionUpdate{{Slot: 2, Motion: m}, {Slot: 257, Motion: m}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	u16 := func(v int) { want = binary.LittleEndian.AppendUint16(want, uint16(v)) }
	f32 := func(v float32) { want = binary.LittleEndian.AppendUint32(want, math.Float32bits(v)) }
	want = append(want, "GSP5"...)
	u16(1)
	u16(4)
	want = append(want, "walk"...)
	u16(1)
	u16(5)
	want = append(want, "crowd"...)
	u16(300)
	u16(2)
	for _, slot := range []int{2, 257} {
		u16(slot)
		for _, v := range []float32{1, 2, 3, .5, .25, 0, 1, 1, 1, 10, 4, 5, 6, 0, .75, .125, 2, 2, 2, 10.5} {
			f32(v)
		}
		u16(1)
		f32(9.5)
		want = append(want, 1)
		f32(1.5)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("GSP5 bytes differ\n got %x\nwant %x", data, want)
	}
	// A changed agent costs a fixed 93 bytes: slot, motion key, clip state.
	perAgent := (len(data) - (4 + 2 + 2 + 4 + 2 + 2 + 5 + 2 + 2)) / 2
	if perAgent != 93 {
		t.Fatalf("bytes per changed agent: %d", perAgent)
	}
}

func TestEncodePartialMotionFrameIsSmallerThanFullFrame(t *testing.T) {
	const crowd = 5000
	full := MotionBatch{ID: "passengers"}
	for i := 0; i < crowd; i++ {
		full.Instances = append(full.Instances, MotionInstanceIR{
			ID: fmt.Sprintf("agent-%d", i), NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1,
			PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1, TNext: 1, Animation: "Run",
		})
	}
	fullBytes, err := EncodeMotionFrame(MotionFrame{Batches: []MotionBatch{full}})
	if err != nil {
		t.Fatal(err)
	}
	var slots []int
	for i := 0; i < crowd; i += 20 {
		slots = append(slots, i)
	}
	partialBytes, err := EncodePartialMotionFrame(PartialMotionFrame{Batches: []PartialMotionBatch{full.Partial(slots)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("5,000 agents: full frame %d bytes; partial frame with %d changed agents %d bytes", len(fullBytes), len(slots), len(partialBytes))
	if len(partialBytes)*15 > len(fullBytes) {
		t.Fatalf("5%% update is %d bytes against a %d-byte full frame", len(partialBytes), len(fullBytes))
	}
}

func TestEncodePartialMotionFrameRejectsBadInput(t *testing.T) {
	m := partialSample()
	cases := map[string]PartialMotionFrame{
		"slots not ascending": {Batches: []PartialMotionBatch{{ID: "b", Size: 4, Updates: []PartialMotionUpdate{{Slot: 2, Motion: m}, {Slot: 2, Motion: m}}}}},
		"slot descending":     {Batches: []PartialMotionBatch{{ID: "b", Size: 4, Updates: []PartialMotionUpdate{{Slot: 3, Motion: m}, {Slot: 1, Motion: m}}}}},
		"slot outside batch":  {Batches: []PartialMotionBatch{{ID: "b", Size: 4, Updates: []PartialMotionUpdate{{Slot: 4, Motion: m}}}}},
		"negative slot":       {Batches: []PartialMotionBatch{{ID: "b", Size: 4, Updates: []PartialMotionUpdate{{Slot: -1, Motion: m}}}}},
		"size over u16":       {Batches: []PartialMotionBatch{{ID: "b", Size: 70000}}},
		"duplicate batch":     {Batches: []PartialMotionBatch{{ID: "b", Size: 1}, {ID: "b", Size: 1}}},
		"empty batch id":      {Batches: []PartialMotionBatch{{ID: "", Size: 1}}},
		"parent matrix": {Batches: []PartialMotionBatch{{ID: "b", Size: 1, Updates: []PartialMotionUpdate{
			{Slot: 0, Motion: func() MotionInstanceIR { x := m; x.ParentMatrix = make([]float64, 16); return x }()}}}}},
		"tnext before tprev": {Batches: []PartialMotionBatch{{ID: "b", Size: 1, Updates: []PartialMotionUpdate{
			{Slot: 0, Motion: func() MotionInstanceIR { x := m; x.TNext = x.TPrev; return x }()}}}}},
		"non-finite": {Batches: []PartialMotionBatch{{ID: "b", Size: 1, Updates: []PartialMotionUpdate{
			{Slot: 0, Motion: func() MotionInstanceIR { x := m; x.NextX = math.NaN(); return x }()}}}}},
	}
	for name, frame := range cases {
		if _, err := EncodePartialMotionFrame(frame); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestPartialMotionEncoderReuseDoesNotLeakClips(t *testing.T) {
	encoder := new(MotionFrameEncoder)
	m := partialSample()
	first, err := encoder.EncodePartial(PartialMotionFrame{Batches: []PartialMotionBatch{{ID: "b", Size: 2, Updates: []PartialMotionUpdate{{Slot: 0, Motion: m}}}}})
	if err != nil {
		t.Fatal(err)
	}
	firstCopy := append([]byte(nil), first...)
	m.Animation = ""
	second, err := encoder.EncodePartial(PartialMotionFrame{Batches: []PartialMotionBatch{{ID: "b", Size: 2, Updates: []PartialMotionUpdate{{Slot: 1, Motion: m}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(second[4:6]); got != 0 {
		t.Fatalf("clip dictionary leaked %d entries", got)
	}
	again, _ := encoder.EncodePartial(PartialMotionFrame{Batches: []PartialMotionBatch{{ID: "b", Size: 2, Updates: []PartialMotionUpdate{{Slot: 0, Motion: partialSample()}}}}})
	if !bytes.Equal(again, firstCopy) {
		t.Fatal("encoder reuse changed the bytes for identical input")
	}
}

func TestMotionBatchPartialSelectsSlots(t *testing.T) {
	batch := MotionBatch{ID: "b", Instances: []MotionInstanceIR{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
	partial := batch.Partial([]int{0, 2})
	if partial.ID != "b" || partial.Size != 3 || len(partial.Updates) != 2 ||
		partial.Updates[1].Slot != 2 || partial.Updates[1].Motion.ID != "c" {
		t.Fatalf("unexpected partial batch: %+v", partial)
	}
}
