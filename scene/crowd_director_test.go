package scene

import (
	"math"
	"testing"
)

func mustDirector(t *testing.T) *CrowdDirector {
	t.Helper()
	d, err := NewCrowdDirector(testCrowdBowl(21))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCrowdFrameRoundTrip(t *testing.T) {
	in := CrowdFrame{Crowd: "bowl", Now: 3600.25, ReplaceWaves: true,
		Sections: []CrowdSectionFrame{{Section: 7, From: CrowdIdle, To: CrowdClap, BlendStart: 3600.5, Blend: 0.4, ExcitementFrom: 0.2, ExcitementTo: 0.8, Rate: 1.5, Synced: true, Epoch: 3599, Jitter: 0.04, Props: true}},
		Waves:    []CrowdWaveFrame{{Start: 3601, Origin: 0.25, Speed: -0.08, Width: 0.035, Laps: 2}},
		Palettes: []CrowdPaletteFrame{{Section: 3, Colors: []string{"#c8102e", "#fff"}}},
	}
	data, err := EncodeCrowdFrame(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := 4 + 2 + 4 + 8 + 2 + 42 + 2 + 20 + 2 + 2 + 1 + 6 + 1; len(data) != want {
		t.Fatalf("frame is %d bytes, want %d", len(data), want)
	}
	out, err := DecodeCrowdFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	s := out.Sections[0]
	// Times travel as float32 offsets from Now, so an hour into a game they
	// still come back to the microsecond.
	if out.Crowd != "bowl" || out.Now != in.Now || !out.ReplaceWaves || s.Section != 7 || s.From != CrowdIdle || s.To != CrowdClap ||
		math.Abs(s.BlendStart-3600.5) > 1e-6 || math.Abs(s.Epoch-3599) > 1e-6 || !s.Synced || !s.Props || math.Abs(s.Rate-1.5) > 1e-6 {
		t.Fatalf("section round trip: %+v", s)
	}
	if p := out.Palettes; len(p) != 1 || p[0].Section != 3 || len(p[0].Colors) != 2 || p[0].Colors[0] != "#c8102e" || p[0].Colors[1] != "#ffffff" || p[0].Accents != nil {
		t.Fatalf("palette round trip: %+v", p)
	}
	if _, err := EncodeCrowdFrame(CrowdFrame{Crowd: "x", Palettes: []CrowdPaletteFrame{{Colors: []string{"red"}}}}); err == nil {
		t.Fatal("encoded a named colour")
	}
	w := out.Waves[0]
	if math.Abs(w.Start-3601) > 1e-6 || math.Abs(w.Speed+0.08) > 1e-6 || w.Laps != 2 {
		t.Fatalf("wave round trip: %+v", w)
	}
	for _, bad := range [][]byte{data[:len(data)-1], append(append([]byte(nil), data...), 0), []byte("GSP3")} {
		if _, err := DecodeCrowdFrame(bad); err == nil {
			t.Fatalf("decoded a malformed frame of %d bytes", len(bad))
		}
	}
	if _, err := EncodeCrowdFrame(CrowdFrame{Crowd: "x", Sections: []CrowdSectionFrame{{To: "moonwalk"}}}); err == nil {
		t.Fatal("encoded an unknown action")
	}
	if _, err := EncodeCrowdFrame(CrowdFrame{Crowd: "x", Waves: make([]CrowdWaveFrame, 5)}); err == nil {
		t.Fatal("encoded five waves")
	}
	if _, err := EncodeCrowdFrame(CrowdFrame{Crowd: "x", Now: math.NaN()}); err == nil {
		t.Fatal("encoded a NaN clock")
	}
}

func TestCrowdDirectorFramesCarryOnlyChanges(t *testing.T) {
	d := mustDirector(t)
	if d.Changed() {
		t.Fatal("a new director has changes to send")
	}
	if err := d.SetAction(10, CrowdCheer, "lower-3", "lower-4"); err != nil {
		t.Fatal(err)
	}
	f := d.Frame(10)
	if len(f.Sections) != 2 || f.Sections[0].Section != 3 || f.Sections[1].Section != 4 {
		t.Fatalf("frame sections: %+v", f.Sections)
	}
	s := f.Sections[0]
	if s.From != CrowdIdle || s.To != CrowdCheer || s.BlendStart != 10 || s.Blend != 0.4 || !s.Props {
		t.Fatalf("action change: %+v", s)
	}
	if d.Changed() || len(d.Frame(11).Sections) != 0 {
		t.Fatal("Frame did not clear the changes")
	}
	// A stand ID selects all its sections; "*" selects everything.
	if err := d.SetExcitement(12, 0.9, 2, "upper"); err != nil {
		t.Fatal(err)
	}
	if n := len(d.Frame(12).Sections); n != 20 {
		t.Fatalf("stand selector changed %d sections, want 20", n)
	}
	if err := d.SetAction(13, CrowdStandUp); err != nil {
		t.Fatal(err)
	}
	if n := len(d.Frame(13).Sections); n != 44 {
		t.Fatalf("no selector changed %d sections, want all 44", n)
	}
	if err := d.SetAction(13, CrowdCheer, "nope"); err == nil {
		t.Fatal("an unknown selector was accepted")
	}
	if err := d.SetAction(13, CrowdRideWave); err == nil {
		t.Fatal("the wave motion was accepted as a section action")
	}
	if full := d.FullFrame(14); len(full.Sections) != 44 || !full.ReplaceWaves {
		t.Fatalf("full frame: %d sections replace=%v", len(full.Sections), full.ReplaceWaves)
	}
}

func TestCrowdDirectorExcitementRamps(t *testing.T) {
	d := mustDirector(t)
	if err := d.Group("home", "lower-0", "lower-1", "lower-2"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetExcitement(5, 1, 2, "home"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ t, want float64 }{{4, 0}, {5, 0}, {6, 0.5}, {7, 1}, {30, 1}} {
		if got, _ := d.Excitement(c.t, "lower-1"); math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("excitement at %v = %v, want %v", c.t, got, c.want)
		}
	}
	// A change mid-ramp starts from where the ramp is.
	if err := d.SetExcitement(6, 0, 1, "home"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Excitement(6, "lower-1"); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("mid-ramp change jumped to %v", got)
	}
	f := d.Frame(6)
	if f.Sections[0].ExcitementFrom != 0.5 || f.Sections[0].ExcitementTo != 0 || f.Sections[0].ExcitementStart != 6 || f.Sections[0].ExcitementRamp != 1 {
		t.Fatalf("frame ramp: %+v", f.Sections[0])
	}
}

func TestCrowdSyncSharesOneClock(t *testing.T) {
	d := mustDirector(t)
	if err := d.Sync(10, CrowdClap, 120, "lower-0"); err != nil {
		t.Fatal(err)
	}
	s := d.Frame(10).Sections[0]
	if !s.Synced || s.Epoch != 10 || s.To != CrowdClap || math.Abs(s.Rate-1) > 1e-12 {
		t.Fatalf("sync frame: %+v", s)
	}
	if err := d.Sync(10, CrowdClap, 0); err == nil {
		t.Fatal("a zero tempo was accepted")
	}
	if err := d.Release(12, "lower-0"); err != nil {
		t.Fatal(err)
	}
	if s := d.Frame(12).Sections[0]; s.Synced || s.Rate != 1 {
		t.Fatalf("release frame: %+v", s)
	}
}

func TestCrowdWavesAndProps(t *testing.T) {
	d := mustDirector(t)
	d.StartWave(100, CrowdWave{Origin: 0.25, Speed: 0.1, Laps: 2})
	f := d.Frame(100)
	if len(f.Waves) != 1 || f.Waves[0].Width != 0.035 || f.Waves[0].Laps != 2 {
		t.Fatalf("wave frame: %+v", f.Waves)
	}
	for i := 0; i < 6; i++ {
		d.StartWave(200, CrowdWave{})
	}
	if n := len(d.Frame(200).Waves); n != 4 {
		t.Fatalf("a frame carries %d waves, want the 4 newest", n)
	}
	d.StopWaves(201)
	if f := d.Frame(201); !f.ReplaceWaves || len(f.Waves) != 0 {
		t.Fatalf("stop frame: %+v", f)
	}
	if err := d.RaiseProps(202, false, "lower-3"); err != nil {
		t.Fatal(err)
	}
	if s := d.Frame(202).Sections[0]; s.Section != 3 || s.Props {
		t.Fatalf("props frame: %+v", s)
	}
}

func TestCrowdPaletteRecolorsSections(t *testing.T) {
	d := mustDirector(t)
	if err := d.SetPalette(5, []string{"#002147", "#a3c1ad"}, nil, "lower"); err != nil {
		t.Fatal(err)
	}
	f := d.Frame(5)
	if len(f.Palettes) != 24 || f.Palettes[0].Colors[0] != "#002147" || f.Palettes[0].Accents[0] != "#f2c14e" {
		t.Fatalf("palette frame: %d palettes, first %+v", len(f.Palettes), f.Palettes[0])
	}
	if len(d.Frame(6).Palettes) != 0 {
		t.Fatal("a palette was sent twice")
	}
	full := d.FullFrame(7)
	if len(full.Palettes) != 44 || full.Palettes[0].Colors[0] != "#002147" || full.Palettes[30].Colors[0] != "#c8102e" {
		t.Fatalf("full frame palettes: %+v / %+v", full.Palettes[0], full.Palettes[30])
	}
	if err := d.SetPalette(8, []string{"#1", "#2", "#3", "#4", "#5"}, nil); err == nil {
		t.Fatal("five colours were accepted")
	}
	if err := d.SetPalette(8, []string{"blue"}, nil); err == nil {
		t.Fatal("a named colour was accepted")
	}
}
