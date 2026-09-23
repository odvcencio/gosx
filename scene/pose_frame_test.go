package scene

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestEncodePoseFrameCarriesAnimationAndTransforms(t *testing.T) {
	frame := PoseFrame{Batches: []PoseBatch{{ID: "heroes", Instances: []MeshInstanceIR{
		{ID: "hero", X: 1.5, ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: "run", AnimationTime: 0.25, AnimationLoop: true},
		{ID: "enemy", X: -2, ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: "run", AnimationTime: 0.5},
	}}}}
	data, err := EncodePoseFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "GSP2" {
		t.Fatalf("magic: %q", data[:4])
	}
	if got := binary.LittleEndian.Uint16(data[4:6]); got != 1 {
		t.Fatalf("clip dictionary count: %d", got)
	}
	// One repeated clip is stored once; the full frame remains much smaller
	// than two JSON instance records for this representative shape.
	if len(data) >= 250 {
		t.Fatalf("unexpected pose frame size: %d", len(data))
	}
}

func TestEncodePoseFrameRejectsAmbiguousOrUnrepresentableInput(t *testing.T) {
	for name, frame := range map[string]PoseFrame{
		"duplicate batch":         {Batches: []PoseBatch{{ID: "a"}, {ID: "a"}}},
		"duplicate instance":      {Batches: []PoseBatch{{ID: "a", Instances: []MeshInstanceIR{{ID: "x"}, {ID: "x"}}}}},
		"nonfinite":               {Batches: []PoseBatch{{ID: "a", Instances: []MeshInstanceIR{{ID: "x", X: math.NaN()}}}}},
		"overflow":                {Batches: []PoseBatch{{ID: "a", Instances: []MeshInstanceIR{{ID: "x", X: math.MaxFloat64}}}}},
		"negative animation time": {Batches: []PoseBatch{{ID: "a", Instances: []MeshInstanceIR{{ID: "x", AnimationTime: -1}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodePoseFrame(frame); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
