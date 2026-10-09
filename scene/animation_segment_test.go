package scene

import (
	"reflect"
	"testing"
)

func TestSegmentAnimationInterpolatesBoundariesWithoutKeyAssumptions(t *testing.T) {
	clip := AnimationClipIR{Name: "path", Duration: 4, Channels: []AnimationChannelIR{{TargetID: "tile", Property: "translation", Times: []float64{0, 1, 3, 4}, Values: []float64{0, 0, 0, 2, 0, 0, 6, 0, 0, 8, 0, 0}}}}
	left, err := SegmentAnimation(clip, .5, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	right, err := SegmentAnimation(clip, 2, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	a, b := left.Channels[0], right.Channels[0]
	if !reflect.DeepEqual(a.Times, []float64{0, 1, 3}) || !reflect.DeepEqual(a.Values, []float64{1, 0, 0, 2, 0, 0, 4, 0, 0}) {
		t.Fatal("wrong interpolated segment", a)
	}
	if !reflect.DeepEqual(a.Values[len(a.Values)-3:], b.Values[:3]) {
		t.Fatal("segment boundary jumps")
	}
	left.Channels[0].Values[0] = 100
	if clip.Channels[0].Values[0] != 0 {
		t.Fatal("segment aliased author data")
	}
	clip.Channels[0].Interpolation = "CUBICSPLINE"
	if _, err = SegmentAnimation(clip, 0, 1, 1); err == nil {
		t.Fatal("unsupported interpolation silently corrupted")
	}
}
func TestSampleAnimationComposesTracksAndFinalPose(t *testing.T) {
	objects := map[string]ObjectIR{"tile": {ID: "tile", X: 2, RotationY: 1}}
	clips := []AnimationClipIR{{Channels: []AnimationChannelIR{
		{TargetID: "tile", Property: "translation", Times: []float64{0, 2}, Values: []float64{0, 4, 0, 2, 0, 6}},
		{TargetID: "tile", Property: "rotation", Times: []float64{0, 2}, Values: []float64{0, 0, 0, 0, 2, 0}},
		{TargetID: "tile", Property: "scale", Times: []float64{0, 2}, Values: []float64{1, 1, 1, 2, 2, 2}},
	}}}
	got := SampleAnimationCommands(clips, objects, 1, false)
	want := AnimationPose{X: 3, Y: 2, Z: 3, RotationY: 2, ScaleX: 1.5, ScaleY: 1.5, ScaleZ: 1.5}
	if len(got) != 1 || got[0].Data.(AnimationPose) != want {
		t.Fatal(got)
	}
	end := SampleAnimationCommands(clips, objects, 0, true)[0].Data.(AnimationPose)
	if end.X != 4 || end.RotationY != 3 || end.ScaleX != 2 || objects["tile"].X != 2 {
		t.Fatal("final or immutable baseline lost", end)
	}
}
