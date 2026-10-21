package vessel

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/det/parity"
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
