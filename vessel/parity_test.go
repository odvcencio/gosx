package vessel

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/det/parity"
	"m31labs.dev/gosx/oceanspec"
)

var updateParity = flag.Bool("update-parity", false, "rewrite testdata/parity-<class>.json from this platform's run")

// voyage drives one hull through a scripted trip: power changes, course
// changes, a hard-over rudder segment, a crash stop and stabilizers switching
// on and off, in a rough sea. It records a state hash every 100 steps.
func voyage(c HullClass) parity.Trace {
	sea := NewSea(SeaConfig{WaveHeight: 3, WaveLength: 70, WindDirection: 25, Speed: 1})
	ship := NewShip(c, 120, -340, 0.3, sea)
	rec := parity.NewRecorder(c.Name, ship.AppendState)
	const dt = 1.0 / 30
	for i := 0; i < 30*1800; i++ {
		sec := float64(i) * dt
		in := Input{Power: 0.85, Heading: 0.3, Stabilizers: int(sec/240)%2 == 1}
		switch {
		case sec > 120 && sec < 700:
			in.Heading = 2.2
		case sec >= 700 && sec < 900:
			in.Power, in.Heading = 0.3, -1.4
		case sec >= 900 && sec < 1000:
			in.RudderOverride, in.Rudder = true, 1
		case sec >= 1200 && sec < 1300:
			in.Power = -1
		case sec >= 1300:
			in.Power, in.Heading = 1, 3.0
		}
		ship.Step(in, dt)
		if i%100 == 0 {
			rec.Step()
		}
	}
	return rec.Trace()
}

func TestParityVoyages(t *testing.T) {
	if *updateParity {
		for _, c := range Presets() {
			data, err := voyage(c).Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join("testdata", "parity-"+c.Name+".json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	for _, c := range Presets() {
		data, err := os.ReadFile(filepath.Join("testdata", "parity-"+c.Name+".json"))
		if err != nil {
			t.Fatalf("%s: %v (run with -update-parity to record)", c.Name, err)
		}
		want, err := parity.Unmarshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if d := parity.Compare(want, voyage(c)); d != nil {
			t.Errorf("%s: %v", c.Name, d)
		}
	}
}

// layoutTrace hashes, per class, the generated deck polygons and a set of
// routes and world-space deck reads, so decks and pathing are checked for
// bit-identical results across platforms.
func layoutTrace() parity.Trace {
	var l Layout
	rec := parity.NewRecorder("layout", func(dst []byte) []byte {
		for _, d := range l.Decks {
			dst = parity.AppendFloat64(dst, d.Y)
			for _, poly := range append(append([]Polygon{}, d.Walk...), d.Holes...) {
				for _, p := range poly {
					dst = parity.AppendFloat64(dst, p.X)
					dst = parity.AppendFloat64(dst, p.Z)
				}
			}
		}
		for _, p := range l.Portals {
			dst = parity.AppendFloat64(dst, p.X)
			dst = parity.AppendFloat64(dst, p.Z)
		}
		flow := l.NewFlow()
		for i := 0; i < 40; i++ {
			from, to := (i*7)%len(l.Decks), (i*11+3)%len(l.Decks)
			r, ok := l.Route(from, float64(i)-20, float64(i*5)-100, to, float64(20-i), float64(90-i*4), RouteOptions{Flow: flow, Now: float64(i * 30)})
			dst = parity.AppendFloat64(dst, r.Time)
			dst = parity.AppendInt64(dst, int64(len(r.Hops)))
			if ok && len(r.Hops) > 0 {
				h := r.Hops[0]
				_, arrive, _ := flow.Admit(&l, h.Portal, h.From, h.To, 5+i, float64(i*30))
				dst = parity.AppendFloat64(dst, arrive)
			}
			pose := Pose{X: float64(i * 100), Y: 1, Z: -float64(i * 50), Heading: float64(i) * 0.37, Pitch: 0.03, Roll: 0.2}
			w := pose.ToWorld(Vec3{X: -12, Y: l.Decks[from].Y, Z: 40})
			dst = parity.AppendInt64(dst, int64(l.DeckAtWorld(pose, w)))
			dst = parity.AppendFloat64(dst, w.X)
			dst = parity.AppendFloat64(dst, w.Y)
			dst = parity.AppendFloat64(dst, w.Z)
		}
		return dst
	})
	for _, c := range Presets() {
		l = StandardLayout(c)
		rec.Step()
	}
	return rec.Trace()
}

func TestParityLayout(t *testing.T) {
	path := filepath.Join("testdata", "parity-layout.json")
	if *updateParity {
		data, err := layoutTrace().Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update-parity to record)", err)
	}
	want, err := parity.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if d := parity.Compare(want, layoutTrace()); d != nil {
		t.Error(d)
	}
}

// swellTrace hashes the swell waves of the four sea states, as the spectrum
// function selects them, and a liner under way in the rough one. The waves'
// bits cover the hash, Box-Muller, JONSWAP and the ranking; the ship covers
// Sea.Height with Swell set.
func swellTrace() parity.Trace {
	states := []oceanspec.Params{
		{WindSpeed: 3, FetchKm: 30, Gamma: 3.3, Spread: 16, Swell: 0.1, Seed: 1},
		{WindSpeed: 7, FetchKm: 100, Gamma: 3.3, Spread: 8, Swell: 0.15, Seed: 1},
		{WindSpeed: 11, FetchKm: 200, Gamma: 3.3, Spread: 6, Swell: 0.25, Seed: 1},
		{WindSpeed: 20, FetchKm: 250, Gamma: 2, Spread: 4, Swell: 0.35, Seed: 1},
	}
	cascades := []oceanspec.Cascade{{Size: 1600, KMin: 2 * pi / 1600, KMax: 0.12}, {Size: 250, KMin: 0.12, KMax: 0.80}}
	var swell [][]SwellWave
	for i, p := range states {
		p.WindDir, p.SwellDir = oceanspec.Direction(float64(40*i)), oceanspec.Direction(0)
		var waves []SwellWave
		for _, w := range oceanspec.Strongest(p, cascades, 24) {
			waves = append(waves, SwellWave{DirX: w.DirX, DirZ: w.DirZ, K: w.K, Omega: w.Omega, Amplitude: w.Amplitude, Phase: w.Phase})
		}
		swell = append(swell, waves)
	}
	step := 0
	ship := NewShip(Presets()[0], 120, -340, 0.3, NewSea(SeaConfig{Swell: swell[2]}))
	out := parity.NewRecorder("swell", func(dst []byte) []byte {
		if step < len(swell) {
			for _, w := range swell[step] {
				for _, v := range []float64{w.DirX, w.DirZ, w.K, w.Omega, w.Amplitude, w.Phase} {
					dst = parity.AppendFloat64(dst, v)
				}
			}
			return dst
		}
		return ship.AppendState(dst)
	})
	for ; step < len(swell); step++ {
		out.Step()
	}
	const dt = 1.0 / 30
	for i := 0; i < 30*600; i++ {
		ship.Step(Input{Power: 0.8, Heading: 0.5}, dt)
		if i%100 == 0 {
			out.Step()
		}
	}
	return out.Trace()
}

func TestParitySwellSea(t *testing.T) {
	path := filepath.Join("testdata", "parity-swell.json")
	if *updateParity {
		data, err := swellTrace().Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update-parity to record)", err)
	}
	want, err := parity.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if d := parity.Compare(want, swellTrace()); d != nil {
		t.Error(d)
	}
}
