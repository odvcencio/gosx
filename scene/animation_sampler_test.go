package scene

import (
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestAnimationSamplerMatchesProceduralComposition(t *testing.T) {
	objects := map[string]ObjectIR{"a": {ID: "a", X: 2, RotationY: 1}, "z": {ID: "z", Z: 3}, "alias": {ID: "a", X: 7}}
	clips := []AnimationClipIR{{Channels: []AnimationChannelIR{
		{TargetID: "z", Property: "scale", Times: []float64{0, 1, 2}, Values: []float64{1, 1, 1, 2, 3, 4, 1, 1, 1}, Interpolation: "STEP"},
		{TargetID: "a", Property: "translation", Times: []float64{0, 2}, Values: []float64{0, 4, 0, 2, 0, 6}},
		{TargetID: "a", Property: "rotation", Times: []float64{0, 2}, Values: []float64{0, 0, 0, 0, 2, 0}},
		{TargetID: "missing", Property: "translation", Times: []float64{0}, Values: []float64{1, 2, 3}},
		{TargetID: "a", Property: "scale", Times: []float64{0, 1}, Values: []float64{1}},
		{TargetID: "a", Property: "rotation", Times: []float64{0, 0}, Values: []float64{0, 0, 0, 1, 1, 1}},
	}}, {Channels: []AnimationChannelIR{
		{TargetID: "alias", Property: "translation", Times: []float64{0, 2}, Values: []float64{0, 0, 0, 4, 0, 0}},
	}}}
	sampler := NewAnimationSampler(clips, objects)
	for _, at := range []float64{-1, 0, .5, 1, 1.5, 2, 3, math.NaN(), math.Inf(1)} {
		for _, final := range []bool{false, true} {
			want := SampleAnimationCommands(clips, objects, at, final)
			if got := sampler.Sample(at, final); !reflect.DeepEqual(got, want) {
				t.Fatalf("time=%v final=%v: got %+v want %+v", at, final, got, want)
			}
		}
	}
}

func TestAnimationSamplerOwnsTracksAndPoses(t *testing.T) {
	objects := map[string]ObjectIR{"a": {ID: "a", X: 2}}
	clips := []AnimationClipIR{{Channels: []AnimationChannelIR{{TargetID: "a", Property: "translation", Times: []float64{0, 2}, Values: []float64{0, 0, 0, 4, 0, 0}}}}}
	sampler := NewAnimationSampler(clips, objects)
	want := sampler.Sample(1, false)
	clips[0].Channels[0].Times[1] = 10
	clips[0].Channels[0].Values[3] = 100
	objects["a"] = ObjectIR{ID: "a", X: 100}
	if got := sampler.Sample(1, false); !reflect.DeepEqual(got, want) {
		t.Fatal("authoring changes altered prepared playback", got, want)
	}
	want[0].Data = AnimationPose{}
	if got := sampler.Sample(1, false)[0].Data.(AnimationPose).X; got != 4 {
		t.Fatal("returned commands alias sampler storage", got)
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if sampler.Sample(1, false)[0].Data.(AnimationPose).X != 4 {
				t.Error("concurrent sampling changed the pose")
			}
		})
	}
	group.Wait()
}
