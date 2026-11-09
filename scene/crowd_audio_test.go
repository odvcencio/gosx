package scene

import (
	"math"
	"testing"
)

func TestCrowdSyncBeatsLandOnTheTempo(t *testing.T) {
	d := mustDirector(t)
	if err := d.SetExcitement(0, 1, 0.01); err != nil {
		t.Fatal(err)
	}
	// 120 beats per minute: one clap every half second, hands meeting at
	// the clap cycle's beat phase.
	if err := d.Sync(10, CrowdClap, 120, "lower-0"); err != nil {
		t.Fatal(err)
	}
	f := d.Frame(10)
	s := f.Sections[0]
	if !s.Synced || s.Epoch != 10 || math.Abs(s.Rate-1) > 1e-12 {
		t.Fatalf("sync frame: %+v", s)
	}
	beats := d.Beats(10, 12)
	if len(beats) != 4 {
		t.Fatalf("beats in 2 s at 120 bpm: %d, want 4: %+v", len(beats), beats)
	}
	for i, b := range beats {
		if want := 10 + 0.25 + 0.5*float64(i); math.Abs(b.Time-want) > 1e-9 || b.Section != "lower-0" || b.Action != CrowdClap {
			t.Fatalf("beat %d: %+v, want time %v", i, b, want)
		}
		if b.Strength <= 0 || b.Strength > 0.05 {
			t.Fatalf("one section of 44 has strength %v", b.Strength)
		}
	}
	// Doubling the tempo doubles the beats; releasing stops them.
	if err := d.Sync(20, CrowdJump, 140, "lower-0"); err != nil {
		t.Fatal(err)
	}
	if n := len(d.Beats(20, 26)); n != 14 {
		t.Fatalf("jumps in 6 s at 140 bpm: %d, want 14", n)
	}
	if err := d.Release(30, "lower-0"); err != nil {
		t.Fatal(err)
	}
	if n := len(d.Beats(30, 40)); n != 0 {
		t.Fatalf("a released section still beats: %d", n)
	}
}

func TestCrowdAudioLevelsAndWaveFronts(t *testing.T) {
	d := mustDirector(t)
	d.StartWave(100, CrowdWave{Origin: 0.25, Speed: 0.1, Laps: 2})
	f := d.Frame(100)
	if len(f.Waves) != 1 || f.Waves[0].Width != 0.035 || f.Waves[0].Laps != 2 {
		t.Fatalf("wave frame: %+v", f.Waves)
	}
	a := d.Audio(105)
	if len(a.Waves) != 1 || math.Abs(a.Waves[0].Front-0.75) > 1e-9 {
		t.Fatalf("wave front after 5 s at 0.1 turns/s from 0.25: %+v", a.Waves)
	}
	if a := d.Audio(130); len(a.Waves) != 0 {
		t.Fatalf("the wave is still running after its two laps: %+v", a.Waves)
	}
	for i := 0; i < 6; i++ {
		d.StartWave(200, CrowdWave{})
	}
	if n := len(d.Frame(200).Waves); n != 4 {
		t.Fatalf("a frame carries %d waves, want the 4 newest", n)
	}

	quiet := d.Audio(300).Loudness
	if err := d.SetExcitement(300, 1, 0.01); err != nil {
		t.Fatal(err)
	}
	if err := d.SetAction(300, CrowdCheer); err != nil {
		t.Fatal(err)
	}
	loud := d.Audio(310)
	if quiet > 0.01 || loud.Loudness < 0.95 {
		t.Fatalf("loudness quiet %v, everyone cheering %v", quiet, loud.Loudness)
	}
	if len(loud.Sections) != 44 || loud.Sections[0].Fans == 0 || loud.Sections[0].Action != CrowdCheer {
		t.Fatalf("section audio: %+v", loud.Sections[0])
	}
}

type fakeCrowdBus map[string]float64

func (b fakeCrowdBus) SetVolume(bus string, v float64) bool { b[bus] = v; return true }

func TestCrowdNoiseFollowsLoudness(t *testing.T) {
	bus := fakeCrowdBus{}
	n := CrowdNoise{Bus: "crowd", Quiet: 0.2, Loud: 1}
	if v := n.Follow(bus, CrowdAudio{Loudness: 0}, 0); v != 0.2 || bus["crowd"] != 0.2 {
		t.Fatalf("quiet volume %v", v)
	}
	// Rising is fast: most of the way in a quarter second.
	for i := 0; i < 15; i++ {
		n.Follow(bus, CrowdAudio{Loudness: 1}, 1.0/60)
	}
	if n.Level() < 0.6 || bus["crowd"] < 0.68 {
		t.Fatalf("after 0.25 s of a roar the level is %v", n.Level())
	}
	// Falling is slow.
	for i := 0; i < 15; i++ {
		n.Follow(bus, CrowdAudio{Loudness: 0}, 1.0/60)
	}
	if n.Level() < 0.4 {
		t.Fatalf("the level fell to %v in 0.25 s", n.Level())
	}
	var _ CrowdBus = bus
}
