package audio

import (
	"errors"
	"math"
	"testing"
)

type regionSource struct {
	*fakeSource
	region [3]float64
	loopAt [2]float64
}

func (s *regionSource) StartRegion(at, offset, duration float64) error {
	s.region = [3]float64{at, offset, duration}
	return nil
}
func (s *regionSource) SetLoopRegion(start, end float64) error {
	s.loopAt = [2]float64{start, end}
	return nil
}

type targetGain struct {
	*fakeGain
	target [3]float64
}

func (g *targetGain) SetGainTarget(value, at, constant float64) error {
	g.target = [3]float64{value, at, constant}
	return nil
}

func TestOptionalAudioControlsPreserveScheduledValues(t *testing.T) {
	source := &regionSource{fakeSource: &fakeSource{fakeNode: &fakeNode{}}}
	if err := StartRegion(source, 12.5, 3.2, .085); err != nil || source.region != [3]float64{12.5, 3.2, .085} {
		t.Fatalf("sprite changed: %v %+v", err, source.region)
	}
	if err := SetLoopRegion(source, 1.25, 7.75); err != nil || source.loopAt != [2]float64{1.25, 7.75} || source.loop {
		t.Fatalf("loop bounds changed or enabled playback: %v %+v", err, source)
	}
	gain := &targetGain{fakeGain: &fakeGain{fakeNode: &fakeNode{}}}
	if err := SetGainTarget(gain, 0, 12.5, .05); err != nil || gain.target != [3]float64{0, 12.5, .05} {
		t.Fatalf("mute automation changed: %v %+v", err, gain.target)
	}
	if err := StartRegion(source.fakeSource, 0, 0, 1); !errors.Is(err, ErrUnsupported) || len(source.started) != 0 {
		t.Fatal("unsupported host silently played wrong sprite", err)
	}
	if err := SetLoopRegion(source.fakeSource, 0, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if err := SetGainTarget(gain.fakeGain, 1, 0, .05); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestAudioControlsRejectInvalidTimesBeforeTouchingNodes(t *testing.T) {
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := StartRegion(nil, bad, 0, 1); err == nil || errors.Is(err, ErrUnsupported) {
			t.Fatalf("invalid scheduling time accepted: %g", bad)
		}
		if err := StartRegion(nil, 0, bad, 1); err == nil || errors.Is(err, ErrUnsupported) {
			t.Fatalf("invalid offset accepted: %g", bad)
		}
		if err := SetLoopRegion(nil, 0, bad); err == nil || errors.Is(err, ErrUnsupported) {
			t.Fatalf("invalid loop accepted: %g", bad)
		}
		if err := SetGainTarget(nil, 1, 0, bad); err == nil || errors.Is(err, ErrUnsupported) {
			t.Fatalf("invalid gain time constant accepted: %g", bad)
		}
	}
	if err := SetLoopRegion(nil, 2, 1); err == nil || errors.Is(err, ErrUnsupported) {
		t.Fatal("reversed loop accepted")
	}
}
