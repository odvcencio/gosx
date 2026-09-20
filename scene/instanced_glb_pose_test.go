package scene

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func poseFrameFixture() []InstancedGLBMeshIR {
	pickable := false
	return []InstancedGLBMeshIR{
		{
			ID: "fragments", Src: "/fragment.glb", Pickable: &pickable, SharedAppearance: true,
			Instances: []MeshInstanceIR{
				{ID: "a", ScaleX: 1, ScaleY: 1, ScaleZ: 1},
				{ID: "b", X: 2, ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: "idle", AnimationLoop: true},
			},
		},
	}
}

func TestPackInstancedGLBPoseFrameWire(t *testing.T) {
	previous := poseFrameFixture()
	next := poseFrameFixture()
	next[0].Instances[0] = MeshInstanceIR{
		ID: "a", X: 1.25, Y: -2, Z: 3.5,
		RotationX: .1, RotationY: .2, RotationZ: .3,
		// Zero scales are omitted on the JSON wire and normalize to one.
		ScaleX: 0, ScaleY: 2, ScaleZ: 3,
	}
	next[0].Instances[1].AnimationTime = 4.5

	frame, ok := PackInstancedGLBPoseFrame(make([]byte, 0, 256), 9007199254740991, previous, next)
	if !ok {
		t.Fatal("stable pose frame was refused")
	}
	wantLength := InstancedGLBPoseFrameHeaderBytes + 2*InstancedGLBPoseFrameRowFloats*4
	if len(frame) != wantLength {
		t.Fatalf("frame length = %d, want %d", len(frame), wantLength)
	}
	if got := binary.LittleEndian.Uint32(frame[0:4]); got != InstancedGLBPoseFrameMagic {
		t.Fatalf("magic = %#x", got)
	}
	if got := binary.LittleEndian.Uint16(frame[4:6]); got != InstancedGLBPoseFrameVersion {
		t.Fatalf("version = %d", got)
	}
	if got := binary.LittleEndian.Uint16(frame[6:8]); got != InstancedGLBPoseFrameRowFloats {
		t.Fatalf("row floats = %d", got)
	}
	if got := binary.LittleEndian.Uint32(frame[12:16]); got != 2 {
		t.Fatalf("row count = %d", got)
	}
	if got := binary.LittleEndian.Uint64(frame[16:24]); got != 9007199254740991 {
		t.Fatalf("base revision = %d", got)
	}
	row := make([]float32, InstancedGLBPoseFrameRowFloats)
	for index := range row {
		row[index] = math.Float32frombits(binary.LittleEndian.Uint32(frame[24+index*4:]))
	}
	want := []float32{1.25, -2, 3.5, .1, .2, .3, 1, 2, 3, 0}
	for index := range want {
		if row[index] != want[index] {
			t.Fatalf("row[%d] = %v, want %v", index, row[index], want[index])
		}
	}
	secondTimeOffset := 24 + InstancedGLBPoseFrameRowFloats*4 + 9*4
	if got := math.Float32frombits(binary.LittleEndian.Uint32(frame[secondTimeOffset:])); got != 4.5 {
		t.Fatalf("second animation time = %v", got)
	}
}

func TestPackInstancedGLBPoseFrameReusesDestination(t *testing.T) {
	previous := poseFrameFixture()
	next := poseFrameFixture()
	next[0].Instances[0].X = 3
	dst := make([]byte, 0, 256)
	frame, ok := PackInstancedGLBPoseFrame(dst, 7, previous, next)
	if !ok {
		t.Fatal("stable pose frame was refused")
	}
	if &frame[:cap(frame)][0] != &dst[:cap(dst)][0] {
		t.Fatal("destination backing array was not reused")
	}
}

func TestPackInstancedGLBPoseFrameUsesNormalizedLayoutIDs(t *testing.T) {
	previous := []InstancedGLBMeshIR{{Src: "/a.glb", Instances: []MeshInstanceIR{{}}}}
	next := []InstancedGLBMeshIR{{ID: "  ", Src: " /a.glb ", Instances: []MeshInstanceIR{{ID: " "}}}}
	next[0].Instances[0].X = 1
	frame, ok := PackInstancedGLBPoseFrame(nil, 3, previous, next)
	if !ok {
		t.Fatal("equivalent normalized IDs and source were refused")
	}
	if got, want := binary.LittleEndian.Uint32(frame[8:12]), instancedGLBPoseLayoutHash(previous); got != want {
		t.Fatalf("layout hash = %#x, want %#x", got, want)
	}
}

func TestPackInstancedGLBPoseFrameFiltersEmptyBatchesUsingOriginalIndexes(t *testing.T) {
	previous := []InstancedGLBMeshIR{
		{ID: "empty-before", Src: "/empty-before.glb"},
		{Src: "/first.glb", Instances: []MeshInstanceIR{{}}},
		{ID: "empty-between", Src: "/empty-between.glb"},
		{ID: "missing-src", Instances: []MeshInstanceIR{{ID: "ignored"}}},
		{ID: "active", Src: "/second.glb", Instances: []MeshInstanceIR{{ID: "two"}}},
	}
	next := []InstancedGLBMeshIR{
		{ID: "empty-before", Src: "/empty-before.glb"},
		{ID: " ", Src: " /first.glb ", Instances: []MeshInstanceIR{{ID: " ", X: 11}}},
		{ID: "empty-between", Src: "/empty-between.glb"},
		{ID: "missing-src", Src: " ", Instances: []MeshInstanceIR{{ID: "ignored", X: 99}}},
		{ID: "active", Src: "/second.glb", Instances: []MeshInstanceIR{{ID: "two", X: 22}}},
	}
	frame, ok := PackInstancedGLBPoseFrame(nil, 9, previous, next)
	if !ok {
		t.Fatal("stable retained batches were refused")
	}
	if got := binary.LittleEndian.Uint32(frame[12:16]); got != 2 {
		t.Fatalf("retained row count = %d, want 2", got)
	}
	if got, want := len(frame), InstancedGLBPoseFrameHeaderBytes+2*InstancedGLBPoseFrameRowFloats*4; got != want {
		t.Fatalf("frame length = %d, want %d", got, want)
	}
	firstX := math.Float32frombits(binary.LittleEndian.Uint32(frame[24:28]))
	secondXOffset := InstancedGLBPoseFrameHeaderBytes + InstancedGLBPoseFrameRowFloats*4
	secondX := math.Float32frombits(binary.LittleEndian.Uint32(frame[secondXOffset : secondXOffset+4]))
	if firstX != 11 || secondX != 22 {
		t.Fatalf("retained rows = [%v %v], want [11 22]", firstX, secondX)
	}
	const browserFixtureLayoutHash = 0x7bbc3f25
	if got := binary.LittleEndian.Uint32(frame[8:12]); got != browserFixtureLayoutHash {
		t.Fatalf("layout hash = %#x, want browser golden %#x", got, uint32(browserFixtureLayoutHash))
	}
}

func TestPackInstancedGLBPoseFrameRejectsRetainedBatchTransitions(t *testing.T) {
	tests := []struct {
		name   string
		before InstancedGLBMeshIR
		after  InstancedGLBMeshIR
	}{
		{
			name:   "empty batch gains membership",
			before: InstancedGLBMeshIR{ID: "effects", Src: "/effects.glb"},
			after:  InstancedGLBMeshIR{ID: "effects", Src: "/effects.glb", Instances: []MeshInstanceIR{{ID: "one"}}},
		},
		{
			name:   "batch gains source",
			before: InstancedGLBMeshIR{ID: "effects", Instances: []MeshInstanceIR{{ID: "one"}}},
			after:  InstancedGLBMeshIR{ID: "effects", Src: "/effects.glb", Instances: []MeshInstanceIR{{ID: "one"}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := []InstancedGLBMeshIR{test.before, poseFrameFixture()[0]}
			next := []InstancedGLBMeshIR{test.after, poseFrameFixture()[0]}
			if _, ok := PackInstancedGLBPoseFrame(nil, 1, previous, next); ok {
				t.Fatal("retained layout transition did not force a full command")
			}
		})
	}
}

func TestPackInstancedGLBPoseFrameRejectsUnsafeChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(previous, next []InstancedGLBMeshIR)
	}{
		{"batch order or id", func(_, next []InstancedGLBMeshIR) { next[0].ID = "other" }},
		{"source", func(_, next []InstancedGLBMeshIR) { next[0].Src = "/other.glb" }},
		{"material", func(_, next []InstancedGLBMeshIR) { next[0].Color = "#fff" }},
		{"instance id", func(_, next []InstancedGLBMeshIR) { next[0].Instances[0].ID = "other" }},
		{"membership", func(_, next []InstancedGLBMeshIR) { next[0].Instances = next[0].Instances[:1] }},
		{"animation clip", func(_, next []InstancedGLBMeshIR) { next[0].Instances[1].Animation = "run" }},
		{"animation loop", func(_, next []InstancedGLBMeshIR) { next[0].Instances[1].AnimationLoop = false }},
		{"animation mode appearance", func(_, next []InstancedGLBMeshIR) { next[0].Instances[0].AnimationTime = 1 }},
		{"parent matrix", func(_, next []InstancedGLBMeshIR) { next[0].Instances[0].ParentMatrix = make([]float64, 16) }},
		{"nonfinite", func(_, next []InstancedGLBMeshIR) { next[0].Instances[0].X = math.Inf(1) }},
		{"float32 overflow", func(_, next []InstancedGLBMeshIR) { next[0].Instances[0].X = math.MaxFloat64 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := poseFrameFixture()
			next := poseFrameFixture()
			test.mutate(previous, next)
			if frame, ok := PackInstancedGLBPoseFrame(nil, 1, previous, next); ok || len(frame) != 0 {
				t.Fatalf("unsafe frame accepted: ok=%v len=%d", ok, len(frame))
			}
		})
	}
}

func TestPackInstancedGLBPoseFrameRejectsInvalidRevisionOrEmptyLayout(t *testing.T) {
	fixture := poseFrameFixture()
	for _, revision := range []uint64{0, 1 << 53} {
		if _, ok := PackInstancedGLBPoseFrame(nil, revision, fixture, fixture); ok {
			t.Fatalf("revision %d accepted", revision)
		}
	}
	if _, ok := PackInstancedGLBPoseFrame(nil, 1, nil, nil); ok {
		t.Fatal("empty layout accepted")
	}
}

func TestPackInstancedGLBPoseFrameRejectsNonfiniteAnimationTimeBeforeClamp(t *testing.T) {
	for _, animationTime := range []float64{math.Inf(-1), math.Inf(1), math.NaN()} {
		previous := poseFrameFixture()
		next := poseFrameFixture()
		next[0].Instances[1].AnimationTime = animationTime
		if frame, ok := PackInstancedGLBPoseFrame(nil, 1, previous, next); ok || len(frame) != 0 {
			t.Fatalf("nonfinite animation time %v accepted", animationTime)
		}
	}
	previous := poseFrameFixture()
	next := poseFrameFixture()
	next[0].Instances[1].AnimationTime = -3
	frame, ok := PackInstancedGLBPoseFrame(nil, 1, previous, next)
	if !ok {
		t.Fatal("finite negative animation time was refused")
	}
	offset := InstancedGLBPoseFrameHeaderBytes + InstancedGLBPoseFrameRowFloats*4 + 9*4
	if got := math.Float32frombits(binary.LittleEndian.Uint32(frame[offset:])); got != 0 {
		t.Fatalf("clamped animation time = %v, want 0", got)
	}
}

func TestPackInstancedGLBPoseFrameChecksEveryBatchDeclarationField(t *testing.T) {
	typeOfBatch := reflect.TypeOf(InstancedGLBMeshIR{})
	for fieldIndex := range typeOfBatch.NumField() {
		field := typeOfBatch.Field(fieldIndex)
		t.Run(field.Name, func(t *testing.T) {
			previous := poseFrameFixture()
			next := poseFrameFixture()
			value := reflect.ValueOf(&next[0]).Elem().Field(fieldIndex)
			switch field.Name {
			case "Instances":
				value.Set(reflect.Append(value, reflect.ValueOf(MeshInstanceIR{ID: "added"})))
			case "AlphaCutoff":
				value.Set(reflect.ValueOf(Cutoff(0.5)))
			default:
				switch value.Kind() {
				case reflect.String:
					value.SetString("changed")
				case reflect.Float64:
					value.SetFloat(0.5)
				case reflect.Bool:
					value.SetBool(!value.Bool())
				case reflect.Pointer:
					changed := reflect.New(value.Type().Elem())
					switch changed.Elem().Kind() {
					case reflect.Bool:
						changed.Elem().SetBool(true)
					case reflect.Float64:
						changed.Elem().SetFloat(0.5)
					case reflect.Array:
						changed.Elem().Index(0).SetFloat(0.5)
					default:
						t.Fatalf("unhandled pointer field type %v", value.Type())
					}
					value.Set(changed)
				case reflect.Map:
					changed := reflect.MakeMap(value.Type())
					key := reflect.ValueOf("changed")
					if value.Type().Elem().Kind() == reflect.Interface {
						changed.SetMapIndex(key, reflect.ValueOf("value"))
					} else {
						changed.SetMapIndex(key, reflect.ValueOf("value").Convert(value.Type().Elem()))
					}
					value.Set(changed)
				default:
					t.Fatalf("unhandled batch field kind %v", value.Kind())
				}
			}
			if _, ok := PackInstancedGLBPoseFrame(nil, 1, previous, next); ok {
				t.Fatalf("batch declaration field %s changed without forcing a full command", field.Name)
			}
		})
	}
}

var packedInstancedGLBPoseBenchmarkFrame []byte

func BenchmarkPackInstancedGLBPoseFrame832Rows(b *testing.B) {
	const batchCount, rowsPerBatch = 32, 26
	previous := make([]InstancedGLBMeshIR, batchCount)
	next := make([]InstancedGLBMeshIR, batchCount)
	for batchIndex := range previous {
		batchID := fmt.Sprintf("combat-effects-%d", batchIndex)
		src := fmt.Sprintf("/combat-effects-%d.glb", batchIndex)
		previous[batchIndex] = InstancedGLBMeshIR{
			ID: batchID, Src: src, SharedAppearance: true, Instances: make([]MeshInstanceIR, rowsPerBatch),
		}
		next[batchIndex] = InstancedGLBMeshIR{
			ID: batchID, Src: src, SharedAppearance: true, Instances: make([]MeshInstanceIR, rowsPerBatch),
		}
		for instanceIndex := range previous[batchIndex].Instances {
			id := fmt.Sprintf("effect-%d-%d", batchIndex, instanceIndex)
			previous[batchIndex].Instances[instanceIndex] = MeshInstanceIR{ID: id, ScaleX: 1, ScaleY: 1, ScaleZ: 1}
			next[batchIndex].Instances[instanceIndex] = MeshInstanceIR{ID: id, X: float64(instanceIndex), ScaleX: 1, ScaleY: 1, ScaleZ: 1}
		}
	}
	dst, ok := PackInstancedGLBPoseFrame(nil, 1, previous, next)
	if !ok {
		b.Fatal("warm pose frame was refused")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(dst)))
	b.ResetTimer()
	for range b.N {
		dst, ok = PackInstancedGLBPoseFrame(dst, 1, previous, next)
		if !ok {
			b.Fatal("stable pose frame was refused")
		}
	}
	packedInstancedGLBPoseBenchmarkFrame = dst
}
