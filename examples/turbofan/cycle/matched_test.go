package cycle

import (
	"math"
	"strings"
	"testing"
)

func TestMapInterpolationAndCoverage(t *testing.T) {
	m := rawMap{Name: "analytic", Speed: []float64{1, 2}, Coordinate: []float64{10, 20}, Flow: [][]float64{{11, 21}, {12, 22}}, Efficiency: [][]float64{{.7, .8}, {.8, .9}}, Pressure: [][]float64{{2, 4}, {3, 5}}}
	w, e, p, err := m.lookup(1.25, 12.5)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "bilinear flow", w, 13.75, 1e-12)
	near(t, "bilinear efficiency", e, .75, 1e-12)
	near(t, "bilinear pressure", p, 2.75, 1e-12)
	for _, x := range []float64{.99, 2.01, math.NaN()} {
		if _, _, _, err := m.lookup(x, 15); err == nil {
			t.Fatal("map extrapolation accepted")
		}
	}
	if _, _, _, err := m.lookup(1.5, 21); err == nil {
		t.Fatal("coordinate extrapolation accepted")
	}
}

func TestVariableGasConservationAndNozzle(t *testing.T) {
	for _, temp := range []float64{250, 288.15, 600, 999.9, 1000.1, 1400, 2500} {
		dh := (gasH(temp+.001) - gasH(temp-.001)) / .002
		near(t, "dh/dT = cp", dh, gasCP(temp), 1e-4)
		ds := (gasS(temp+.001) - gasS(temp-.001)) / .002
		near(t, "ds/dT = cp/T", ds, gasCP(temp)/temp, 1e-6)
	}
	near(t, "enthalpy continuity", gasH(1000-1e-8), gasH(1000+1e-8), 1e-4)
	near(t, "entropy continuity", gasS(1000-1e-8), gasS(1000+1e-8), 1e-6)
	in := Station{300, PRef, 10}
	out, err := gasCompress(in, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	back, err := gasExpand(out, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "reversible compression/expansion", back.TemperatureK, in.TemperatureK, 1e-6)
	n, err := gasNozzle(Station{1000, 5 * PRef, 10}, PRef, .1)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Choked {
		t.Fatal("high pressure nozzle must choke")
	}
	near(t, "sonic nozzle", n.Mach, 1, 1e-10)
	near(t, "nozzle energy", gasH(1000)-gasH(n.Exit.TemperatureK), n.VelocityMPS*n.VelocityMPS/2, 1e-7)
	near(t, "nozzle continuity", n.Exit.PressurePa/(R*n.Exit.TemperatureK)*n.VelocityMPS*n.AreaM2, n.Exit.MassFlowKGS, 1e-9)
	if _, err := gasNozzle(in, 2*PRef, .1); err == nil {
		t.Fatal("backpressure accepted")
	}
}

func TestJT9DRatedCalibrationAndUnfittedObservations(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	near(t, "rated thrust", m.Rated.NetThrustN, 213500, 1e-3)
	near(t, "published pressure ratio", m.Rated.Inputs.OverallPressureRatio, 23.4, 1e-9)
	near(t, "published bypass ratio", m.Rated.Inputs.BypassRatio, 5, 1e-10)
	if m.CoreAreaM2 >= m.BypassAreaM2 {
		t.Fatal("wrong reconstruction branch")
	}
	for _, p := range reference.Points {
		r, err := m.Solve(MappedInputs{FuelFlowKGS: p.FuelFlowKGS})
		if err != nil {
			t.Logf("%s (%s): unavailable: %v", p.Mode, p.Role, err)
			if p.Mode == "takeoff" || p.Mode == "climbout" {
				t.Error(err)
			}
			continue
		}
		near(t, "fixed core area", r.Cycle.CoreNozzle.AreaM2, m.CoreAreaM2, 0)
		near(t, "fixed bypass area", r.Cycle.BypassNozzle.AreaM2, m.BypassAreaM2, 0)
		if r.MatchResidual > 1e-8 || r.Cycle.MaxRelativeResidual > 1e-7 {
			t.Fatalf("nonconverged point %+v", r)
		}
		target := reference.RatedThrustN * p.ThrustFraction
		t.Logf("%s (%s): target %.3f N, predicted %.3f N, error %.4f%%, N1 %.1f, N2 %.1f, match %.3g, %d iterations", p.Mode, p.Role, target, r.Cycle.NetThrustN, 100*(r.Cycle.NetThrustN/target-1), r.N1RPM, r.N2RPM, r.MatchResidual, r.Iterations)
		if p.Mode == "climbout" && math.Abs(r.Cycle.NetThrustN/target-1) > .05 {
			t.Fatal("unfitted climbout point exceeds declared 5% ground-thrust check tolerance")
		}
	}
}

func TestApproachFitReproducesStoredProfile(t *testing.T) {
	m, err := CalibrateJT9D()
	if err != nil {
		t.Fatal(err)
	}
	loss, err := FitApproachLoss(m)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "reproducible low-speed correction", loss, profile.LowSpeedLoss, 1e-7)
	r, err := m.Solve(MappedInputs{FuelFlowKGS: .6593})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "approach calibration", r.Cycle.NetThrustN, 64050, .01)
}

func TestMappedOperatingLineConservesAndStaysInMaps(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	lastThrust := 0.0
	for i := 0; i <= 20; i++ {
		fuel := .6593 + (2.055-.6593)*float64(i)/20
		r, err := m.Solve(MappedInputs{FuelFlowKGS: fuel})
		if err != nil {
			t.Fatalf("fuel %g: %v", fuel, err)
		}
		if r.Cycle.NetThrustN <= lastThrust {
			t.Fatal("nonmonotonic operating line")
		}
		lastThrust = r.Cycle.NetThrustN
		if r.Cycle.MaxRelativeResidual > 1e-7 || r.MatchResidual > 1e-8 {
			t.Fatalf("balance failed at %g", fuel)
		}
		near(t, "fixed core geometry", r.Cycle.CoreNozzle.AreaM2, m.CoreAreaM2, 0)
		near(t, "fixed bypass geometry", r.Cycle.BypassNozzle.AreaM2, m.BypassAreaM2, 0)
		for _, p := range r.MapPoints {
			if _, _, _, e := rawMaps[p.Name].lookup(p.CorrectedSpeed, p.Coordinate); e != nil {
				t.Fatal(e)
			}
		}
		if !strings.Contains(m.CSV(r), "model N1") || strings.Contains(m.CSV(r), "calorically perfect gas") {
			t.Fatal("mapped export lost model identity")
		}
	}
	if _, err := m.Solve(MappedInputs{FuelFlowKGS: .2054}); err == nil {
		t.Fatal("idle must remain unsupported rather than extrapolated")
	}
	for _, in := range []MappedInputs{{FuelFlowKGS: math.NaN()}, {FuelFlowKGS: 3}, {AltitudeM: -1, FuelFlowKGS: 1}} {
		if _, err := m.Solve(in); err == nil {
			t.Fatal("invalid command accepted")
		}
	}
}
