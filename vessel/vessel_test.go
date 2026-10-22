package vessel

import (
	"bytes"
	"math"
	"testing"
)

const testDt = 0.5

func TestCubicPowerLaw(t *testing.T) {
	for _, c := range Presets() {
		// Full power holds close to ServiceSpeed.
		full := c.SteadySpeed(1)
		if full > c.ServiceSpeed || full < 0.97*c.ServiceSpeed {
			t.Errorf("%s: full-power speed %.3f, service speed %.3f", c.Name, full, c.ServiceSpeed)
		}
		// Power is roughly proportional to speed cubed: P(v)/P(0.5v) is near 8.
		ratio := c.PowerForSpeed(full) / c.PowerForSpeed(full/2)
		if ratio < 7 || ratio > 8.1 {
			t.Errorf("%s: P(v)/P(v/2) = %.2f, want about 8", c.Name, ratio)
		}
		// Resistance grows steeply: doubling speed at least quadruples it.
		if r := c.Resistance(2*full) / c.Resistance(full); r < 3.9 {
			t.Errorf("%s: R(2v)/R(v) = %.2f, want >= 3.9", c.Name, r)
		}
		// Steady speed is close to ServiceSpeed * p^(1/3).
		for _, p := range []float64{0.125, 0.5, 0.8} {
			want := c.ServiceSpeed * math.Cbrt(p)
			if got := c.SteadySpeed(p); math.Abs(got-want) > 0.04*want {
				t.Errorf("%s p=%.3f: steady speed %.3f, cube-root law %.3f", c.Name, p, got, want)
			}
		}
	}
}

func TestShipReachesSteadySpeedAndPower(t *testing.T) {
	for _, c := range Presets() {
		s := NewShip(c, 0, 0, 0, nil)
		in := Input{Power: 0.5, Heading: 0}
		for i := 0; i < int(5*3600/testDt); i++ {
			s.Step(in, testDt)
		}
		want := c.SteadySpeed(0.5)
		if math.Abs(s.Speed()-want) > 0.01*want {
			t.Errorf("%s: speed %.3f after 5 h, want %.3f", c.Name, s.Speed(), want)
		}
		if math.Abs(s.PowerDrawn()-0.5*c.InstalledPower) > 1 {
			t.Errorf("%s: power drawn %.0f, want %.0f", c.Name, s.PowerDrawn(), 0.5*c.InstalledPower)
		}
		if s.State.Z >= 0 || math.Abs(s.State.X) > 1e-6 {
			t.Errorf("%s: heading 0 should run toward -Z, got x=%.3f z=%.3f", c.Name, s.State.X, s.State.Z)
		}
	}
}

func TestAccelerationIsGradual(t *testing.T) {
	c := Megaship()
	s := NewShip(c, 0, 0, 0, nil)
	for i := 0; i < int(60/testDt); i++ {
		s.Step(Input{Power: 1}, testDt)
	}
	if s.Speed() <= 0 || s.Speed() > 2 {
		t.Errorf("megaship speed after 60 s = %.2f m/s, want a slow start in (0, 2]", s.Speed())
	}
}

func TestTurningCircle(t *testing.T) {
	for _, c := range Presets() {
		r := TurningCircle(c, 0.6, testDt)
		want := c.Length / (c.SteerGain * c.RudderMax)
		if math.Abs(r.SteadyRadius-want) > 0.2*want {
			t.Errorf("%s: steady radius %.0f m, Length/(K*rudder) = %.0f m", c.Name, r.SteadyRadius, want)
		}
		td := r.TacticalDiameter / c.Length
		if td < 2.5 || td > 6 {
			t.Errorf("%s: tactical diameter %.2f lengths, want 2.5..6", c.Name, td)
		}
		if r.Advance <= 0 || r.Transfer <= 0 || r.Transfer >= r.TacticalDiameter {
			t.Errorf("%s: odd turn report %+v", c.Name, r)
		}
		if r.SteadySpeed >= c.SteadySpeed(0.6) {
			t.Errorf("%s: rudder drag should cost speed: %.3f vs %.3f", c.Name, r.SteadySpeed, c.SteadySpeed(0.6))
		}
		t.Logf("%s: advance %.0f m, transfer %.0f m, tactical diameter %.0f m (%.2f L), radius %.0f m, 180 turn in %.0f s, speed %.2f -> %.2f",
			c.Name, r.Advance, r.Transfer, r.TacticalDiameter, td, r.SteadyRadius, r.Time180, c.SteadySpeed(0.6), r.SteadySpeed)
	}
}

func TestStoppingDistance(t *testing.T) {
	for _, c := range Presets() {
		crash := StoppingDistance(c, 1, testDt)
		lengths := crash.Distance / c.Length
		if lengths < 2 || lengths > 14 {
			t.Errorf("%s: crash stop took %.1f lengths, want 2..14", c.Name, lengths)
		}
		slow := StoppingDistance(c, 0.2, testDt)
		if slow.Distance >= crash.Distance {
			t.Errorf("%s: stop from low power (%.0f m) should be shorter than from full (%.0f m)", c.Name, slow.Distance, crash.Distance)
		}
		t.Logf("%s: crash stop from full power %.0f m (%.1f L) in %.0f s; from 0.2 power %.0f m", c.Name, crash.Distance, lengths, crash.Time, slow.Distance)
	}
}

func TestAutopilotHoldsAndChangesHeading(t *testing.T) {
	for _, c := range Presets() {
		s := NewShip(c, 0, 0, 0, nil)
		target := 90 * math.Pi / 180
		for i := 0; i < int(3600/testDt); i++ {
			s.Step(Input{Power: 0.6, Heading: target}, testDt)
		}
		if err := math.Abs(s.State.Heading - target); err > 0.5*math.Pi/180 {
			t.Errorf("%s: heading error %.2f deg after 1 h", c.Name, err*180/math.Pi)
		}
		if math.Abs(s.State.YawRate) > 1e-4 {
			t.Errorf("%s: still yawing at %.5f rad/s", c.Name, s.State.YawRate)
		}
		// Shortest way round: from 170 deg to -170 deg is a 20 degree turn.
		s2 := NewShip(c, 0, 0, 170*math.Pi/180, nil)
		s2.State.Speed = c.SteadySpeed(0.6)
		s2.State.Power = 0.6
		peak := 0.0
		for i := 0; i < int(1800/testDt); i++ {
			s2.Step(Input{Power: 0.6, Heading: -170 * math.Pi / 180}, testDt)
			peak = math.Max(peak, math.Abs(s2.State.YawRate))
		}
		if peak > 0.05 {
			t.Errorf("%s: wrap-around turn yawed at %.3f rad/s", c.Name, peak)
		}
	}
}

func rms(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x * x
	}
	return math.Sqrt(s / float64(len(xs)))
}

func rollSeries(c HullClass, sea WaveField, stab bool) (roll, pitch []float64) {
	s := NewShip(c, 0, 0, 0, sea)
	s.State.Speed = c.SteadySpeed(0.6)
	s.State.Power = 0.6
	in := Input{Power: 0.6, Heading: 0, Stabilizers: stab}
	for i := 0; i < int(1500/0.1); i++ {
		s.Step(in, 0.1)
		if i > int(300/0.1) {
			roll = append(roll, s.State.Roll)
			pitch = append(pitch, s.State.Pitch)
		}
	}
	return
}

func TestWaveResponse(t *testing.T) {
	sea := NewSea(SeaConfig{WaveHeight: 2.5, WaveLength: 60, WindDirection: 40})
	for _, c := range Presets() {
		flatRoll, flatPitch := rollSeries(c, nil, false)
		if rms(flatRoll) != 0 || rms(flatPitch) != 0 {
			t.Errorf("%s: flat sea should give no roll or pitch", c.Name)
		}
		roll, pitch := rollSeries(c, sea, false)
		stabRoll, _ := rollSeries(c, sea, true)
		if rms(roll) < 1e-4 || rms(pitch) < 1e-4 {
			t.Errorf("%s: sea should move the ship (roll %.5f, pitch %.5f)", c.Name, rms(roll), rms(pitch))
		}
		if rms(stabRoll) >= 0.85*rms(roll) {
			t.Errorf("%s: stabilizers should cut roll: %.5f vs %.5f rad rms", c.Name, rms(stabRoll), rms(roll))
		}
		t.Logf("%s: roll rms %.3f deg, with stabilizers %.3f deg, pitch rms %.3f deg", c.Name, rms(roll)*180/math.Pi, rms(stabRoll)*180/math.Pi, rms(pitch)*180/math.Pi)
	}
}

func TestStabilizersDrawPowerAndNeedSpeed(t *testing.T) {
	c := Liner()
	s := NewShip(c, 0, 0, 0, nil)
	for i := 0; i < 400; i++ {
		s.Step(Input{Power: 0, Stabilizers: true}, 1)
	}
	if s.State.Stabilizer != 0 {
		t.Errorf("fins at rest should have no effect, got %v", s.State.Stabilizer)
	}
	s.State.Speed, s.State.Power = c.SteadySpeed(1), 1
	for i := 0; i < 400; i++ {
		s.Step(Input{Power: 1, Stabilizers: true}, 1)
	}
	if want := c.InstalledPower + c.StabilizerPower*s.State.Stabilizer; s.PowerDrawn() != want || s.State.Stabilizer < 0.9 {
		t.Errorf("power drawn %.0f, want %.0f (stabilizer %.3f)", s.PowerDrawn(), want, s.State.Stabilizer)
	}
}

func TestTurnHeelsOutward(t *testing.T) {
	c := Liner()
	s := NewShip(c, 0, 0, 0, nil)
	s.State.Speed, s.State.Power = c.SteadySpeed(0.8), 0.8
	for i := 0; i < 600; i++ {
		s.Step(Input{Power: 0.8, RudderOverride: true, Rudder: 1}, 0.5)
	}
	// Turning toward increasing heading (port) heels to starboard-down: negative roll.
	if s.State.Roll >= -0.005 {
		t.Errorf("expected outward heel (negative roll), got %.4f", s.State.Roll)
	}
}

func TestSeaMatchesOceanTable(t *testing.T) {
	sea := NewSea(SeaConfig{WaveHeight: 1, WaveLength: 40})
	var lo, hi float64
	for i := 0; i < 2000; i++ {
		h := sea.Height(float64(i)*3.7, float64(i)*1.3, float64(i)*0.9)
		lo, hi = math.Min(lo, h), math.Max(hi, h)
	}
	if hi < 0.5 || hi > 3 || lo > -0.5 || lo < -3 {
		t.Errorf("sea range [%.2f, %.2f] is implausible for height 1", lo, hi)
	}
	if NewSea(SeaConfig{}).Height(3, 4, 5) != NewSea(SeaConfig{WaveHeight: 0.8, WaveLength: 18, Speed: 1}).Height(3, 4, 5) {
		t.Error("zero SeaConfig should take the ocean defaults")
	}
}

func TestStateCodecRoundTrip(t *testing.T) {
	sea := NewSea(SeaConfig{WaveHeight: 2, WaveLength: 50})
	run := func(n int, s *Ship) {
		for i := 0; i < n; i++ {
			s.Step(Input{Power: 0.7, Heading: 1, Stabilizers: i%900 < 450}, 1.0/30)
		}
	}
	a := NewShip(Liner(), 5, -3, 0.2, sea)
	run(2000, a)
	saved := a.AppendState(nil)
	if len(saved) != StateSize {
		t.Fatalf("encoded %d bytes, want %d", len(saved), StateSize)
	}
	b := NewShip(Liner(), 0, 0, 0, sea)
	if err := b.RestoreState(saved); err != nil {
		t.Fatal(err)
	}
	run(2000, a)
	run(2000, b)
	if !bytes.Equal(a.AppendState(nil), b.AppendState(nil)) {
		t.Error("restored ship diverged from the original")
	}
	if b.RestoreState(saved[1:]) == nil {
		t.Error("short state should be rejected")
	}
}
