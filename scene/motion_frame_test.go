package scene

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

func TestEncodeMotionFrameCarriesClipAndTransformKeys(t *testing.T) {
	frame := MotionFrame{Batches: []MotionBatch{{ID: "heroes", Instances: []MotionInstanceIR{
		{
			ID:    "hero",
			PrevX: 1, PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1, TPrev: 0,
			NextX: 2, NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1, TNext: .1,
			Animation: "run", ClipStartTime: 0, AnimationLoop: true, PlaybackRate: 1,
		},
		{
			ID:    "enemy",
			PrevX: -2, PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1, TPrev: 0,
			NextX: -1, NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1, TNext: .1,
			Animation: "run", ClipStartTime: 0, PlaybackRate: 1,
		},
	}}}}
	data, err := EncodeMotionFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "GSP3" {
		t.Fatalf("magic: %q", data[:4])
	}
	if got := binary.LittleEndian.Uint16(data[4:6]); got != 1 {
		t.Fatalf("clip dictionary count: %d", got)
	}
	// One repeated clip is stored once, matching GSP2's dictionary design.
	if len(data) >= 300 {
		t.Fatalf("unexpected motion frame size: %d", len(data))
	}
}

// TestDecodeMotionFrameGolden manually walks the wire bytes, mirroring how
// command-runtime.ts's decodeMotionFrame will, and asserts every field lands
// at the offset the JS decoder expects.
func TestDecodeMotionFrameGolden(t *testing.T) {
	frame := MotionFrame{Batches: []MotionBatch{{ID: "crowd", Instances: []MotionInstanceIR{{
		ID:    "actor-1",
		PrevX: 1, PrevY: 2, PrevZ: 3,
		PrevRotationX: .1, PrevRotationY: .2, PrevRotationZ: .3,
		PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1,
		TPrev: 10,
		NextX: 4, NextY: 5, NextZ: 6,
		NextRotationX: .4, NextRotationY: .5, NextRotationZ: .6,
		NextScaleX: 2, NextScaleY: 2, NextScaleZ: 2,
		TNext:         10.1,
		Animation:     "walk",
		ClipStartTime: 9.5,
		AnimationLoop: true,
		PlaybackRate:  1.5,
	}}}}}
	data, err := EncodeMotionFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	readU16 := func() uint16 {
		v := binary.LittleEndian.Uint16(data[offset:])
		offset += 2
		return v
	}
	readID := func() string {
		n := int(readU16())
		s := string(data[offset : offset+n])
		offset += n
		return s
	}
	readF32 := func() float32 {
		v := math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		return v
	}
	if string(data[:4]) != "GSP3" {
		t.Fatalf("magic: %q", data[:4])
	}
	offset = 4
	clipCount := readU16()
	if clipCount != 1 {
		t.Fatalf("clip count: %d", clipCount)
	}
	if clip := readID(); clip != "walk" {
		t.Fatalf("clip name: %q", clip)
	}
	batchCount := readU16()
	if batchCount != 1 {
		t.Fatalf("batch count: %d", batchCount)
	}
	if id := readID(); id != "crowd" {
		t.Fatalf("batch id: %q", id)
	}
	instanceCount := readU16()
	if instanceCount != 1 {
		t.Fatalf("instance count: %d", instanceCount)
	}
	if id := readID(); id != "actor-1" {
		t.Fatalf("instance id: %q", id)
	}
	want := []float32{1, 2, 3, .1, .2, .3, 1, 1, 1, 10, 4, 5, 6, .4, .5, .6, 2, 2, 2, 10.1}
	for i, w := range want {
		if got := readF32(); got != w {
			t.Fatalf("transform field %d: got %v want %v", i, got, w)
		}
	}
	if clipIndex := readU16(); clipIndex != 1 {
		t.Fatalf("clip index: %d", clipIndex)
	}
	if start := readF32(); start != 9.5 {
		t.Fatalf("clip start time: %v", start)
	}
	if loop := data[offset]; loop != 1 {
		t.Fatalf("loop byte: %d", loop)
	}
	offset++
	if rate := readF32(); rate != 1.5 {
		t.Fatalf("playback rate: %v", rate)
	}
	if offset != len(data) {
		t.Fatalf("trailing bytes: consumed %d of %d", offset, len(data))
	}
}

func TestMotionFrameEncoderReuseResetsClipAndIdentityTables(t *testing.T) {
	encoder := new(MotionFrameEncoder)
	first := MotionFrame{Batches: []MotionBatch{{ID: "actors", Instances: []MotionInstanceIR{
		{ID: "one", Animation: "Run", NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1, TNext: 1},
		{ID: "two", Animation: "Idle", NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1, TNext: 1},
	}}}}
	if _, err := encoder.Encode(first); err != nil {
		t.Fatal(err)
	}
	second := MotionFrame{Batches: []MotionBatch{
		{ID: "actors", Instances: []MotionInstanceIR{{ID: "one", Animation: "Idle", NextScaleX: 2, NextScaleY: 2, NextScaleZ: 2, TNext: 1}}},
		{ID: "effects", Instances: []MotionInstanceIR{{ID: "one", Animation: "Cast", NextScaleX: 3, NextScaleY: 3, NextScaleZ: 3, TNext: 1}}},
	}}
	got, err := encoder.Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	want, err := EncodeMotionFrame(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("reused encoder retained stale clip or identity state")
	}
	if _, err := encoder.Encode(MotionFrame{Batches: []MotionBatch{{ID: "actors", Instances: []MotionInstanceIR{{ID: "one"}, {ID: "one"}}}}}); err == nil {
		t.Fatal("reused encoder missed duplicate instance")
	}
	if _, err := encoder.Encode(second); err != nil {
		t.Fatalf("encoder did not recover after rejection: %v", err)
	}
}

func TestEncodeMotionFrameRejectsAmbiguousOrUnrepresentableInput(t *testing.T) {
	// Every case below carries a valid TNext:1 baseline (TNext > TPrev==0)
	// except the ones that specifically exercise TPrev/TNext themselves, so
	// each subtest is rejected for the reason it names, not incidentally by
	// the TNext-after-TPrev ordering check.
	for name, frame := range map[string]MotionFrame{
		"duplicate batch":                    {Batches: []MotionBatch{{ID: "a"}, {ID: "a"}}},
		"duplicate instance":                 {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", TNext: 1}, {ID: "x", TNext: 1}}}}},
		"nonfinite prev":                     {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", PrevX: math.NaN(), TNext: 1}}}}},
		"nonfinite next":                     {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", NextX: math.Inf(1), TNext: 1}}}}},
		"overflow":                           {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", PrevX: math.MaxFloat64, TNext: 1}}}}},
		"nonfinite tPrev":                    {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", TPrev: math.NaN(), TNext: 1}}}}},
		"nonfinite tNext":                    {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", TNext: math.Inf(-1)}}}}},
		"tNext not after tPrev":              {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", TPrev: 1, TNext: 1}}}}},
		"timestamps collapse to one float32": {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", TPrev: 1, TNext: 1 + 1e-8}}}}},
		"nonfinite clip start":               {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", ClipStartTime: math.NaN(), TNext: 1}}}}},
		"nonfinite rate":                     {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", PlaybackRate: math.Inf(1), TNext: 1}}}}},
		"parent matrix":                      {Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{ID: "x", ParentMatrix: []float64{1}, TNext: 1}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodeMotionFrame(frame); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

// TestEncodeMotionFrameAllowsNegativePlaybackRate documents that reverse
// playback is representable on the wire; the renderer clamps elapsed time to
// zero rather than rejecting it (see animation.ts's
// sceneCrowdMotionPoseRows), so the encoder does not reject it either.
func TestEncodeMotionFrameAllowsNegativePlaybackRate(t *testing.T) {
	frame := MotionFrame{Batches: []MotionBatch{{ID: "a", Instances: []MotionInstanceIR{{
		ID: "x", Animation: "walk", PlaybackRate: -1, TNext: 1,
		NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1,
	}}}}}
	if _, err := EncodeMotionFrame(frame); err != nil {
		t.Fatalf("negative playback rate should be representable: %v", err)
	}
}

func BenchmarkEncodeCrowdMotionFrame(b *testing.B) {
	instances := make([]MotionInstanceIR, 500)
	for i := range instances {
		instances[i] = MotionInstanceIR{
			ID:    fmt.Sprintf("actor-%d", i),
			PrevX: float64(i), NextX: float64(i) + 1,
			PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1,
			NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1,
			TNext: .1, Animation: "Run", AnimationLoop: true, PlaybackRate: 1,
		}
	}
	frame := MotionFrame{Batches: []MotionBatch{{ID: "crowd", Instances: instances}}}
	b.Run("binary", func(b *testing.B) {
		b.ReportAllocs()
		var size int
		for i := 0; i < b.N; i++ {
			data, err := EncodeMotionFrame(frame)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.SetBytes(int64(size))
		b.ReportMetric(float64(size), "wire-B")
	})
	b.Run("binary-reused", func(b *testing.B) {
		encoder := new(MotionFrameEncoder)
		b.ReportAllocs()
		var size int
		for i := 0; i < b.N; i++ {
			data, err := encoder.Encode(frame)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.SetBytes(int64(size))
		b.ReportMetric(float64(size), "wire-B")
	})
}
