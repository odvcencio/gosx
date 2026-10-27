package cycle

import (
	"encoding/csv"
	"math"
	"strings"
	"testing"
)

func near(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if !finite(got) || math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.12g, want %.12g (±%g)", name, got, want, tolerance)
	}
}

// Independent closed-form ideal-cycle benchmark (derivation in MODEL.md).
// Select ideal fan temperature ratio 11/10, overall temperature ratio 2,
// burner ratio T4/T2=4 and fuel/air=1/50. Combining both shaft balances
// yields T5/T2=134/51, without stepping through the component algorithm.
func TestIdealCycleAnalyticBenchmark(t *testing.T) {
	m := Model{FanEfficiency: 1, CompressorEfficiency: 1, TurbineEfficiency: 1,
		ShaftEfficiency: 1, BurnerEfficiency: 1, InletRecovery: 1, BurnerRecovery: 1,
		FuelHeatingValue: 104 * Cp * TRef, CorrectedCoreFlow: 40}
	in := Inputs{TurbineInletK: 4 * TRef, OverallPressureRatio: math.Pow(2, 3.5), FanPressureRatio: math.Pow(1.1, 3.5), BypassRatio: 4}
	r, err := m.Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "fan temperature", r.Fan.TemperatureK, 1.1*TRef, 1e-9)
	near(t, "compressor temperature", r.Compressor.TemperatureK, 2*TRef, 1e-9)
	near(t, "fuel/air", r.FuelAirRatio, .02, 1e-12)
	near(t, "HPT temperature", r.HighTurbine.TemperatureK, (4-15.0/17)*TRef, 1e-9)
	near(t, "LPT temperature", r.LowTurbine.TemperatureK, 134.0/51*TRef, 1e-9)
	near(t, "exit total pressure", r.LowTurbine.PressurePa, PRef*math.Pow(67.0/51, 3.5), 1e-6)
	// Unchoked fan converts exactly cp*(T13-T2) to kinetic energy.
	near(t, "bypass velocity", r.BypassNozzle.VelocityMPS, math.Sqrt(.2*Cp*TRef), 1e-8)
	if !r.CoreNozzle.Choked || r.BypassNozzle.Choked {
		t.Fatal("expected choked core and unchoked bypass")
	}
	near(t, "core exit Mach", r.CoreNozzle.Mach, 1, 1e-12)
	// The core is sonic: Te=5/6 Tt5 and pe=(5/6)^3.5 pt5.
	te := 5.0 / 6 * 134.0 / 51 * TRef
	pe := PRef * math.Pow(335.0/306, 3.5)
	ve := math.Sqrt(Gamma * R * te)
	coreArea := 40.8 * R * te / (pe * ve)
	wantThrust := 160*math.Sqrt(.2*Cp*TRef) + 40.8*ve + (pe-PRef)*coreArea
	near(t, "net thrust including pressure", r.NetThrustN, wantThrust, 1e-7)
	near(t, "fuel flow", r.FuelFlowKGS, .8, 1e-12)
}

func TestConservationAndDomainAcrossEnvelope(t *testing.T) {
	m := DefaultModel()
	valid, rejected, choked, unchoked := 0, 0, 0, 0
	for _, h := range []float64{0, 5500, 11000} {
		for _, mach := range []float64{0, .4, .85} {
			for _, temp := range []float64{1000, 1400, 1800} {
				for _, opr := range []float64{5, 20, 40} {
					for _, fan := range []float64{1.1, 1.5, 1.8} {
						for _, bypass := range []float64{1, 6, 12} {
							in := Inputs{h, mach, temp, opr, fan, bypass}
							r, err := m.Solve(in)
							if err != nil {
								rejected++
								continue
							}
							valid++
							if r.MaxRelativeResidual > 1e-12 {
								t.Fatalf("conservation at %+v: residual %g", in, r.MaxRelativeResidual)
							}
							for _, n := range []Nozzle{r.CoreNozzle, r.BypassNozzle} {
								if n.Choked {
									choked++
								} else {
									unchoked++
								}
								if n.Mach <= 0 || n.Mach > 1+1e-12 || !finite(n.AreaM2) {
									t.Fatalf("bad nozzle %+v", n)
								}
								near(t, "nozzle continuity", n.Exit.PressurePa/(R*n.Exit.TemperatureK)*n.VelocityMPS*n.AreaM2, n.Exit.MassFlowKGS, 1e-9)
							}
							near(t, "two stream momentum", r.NetThrustN,
								r.CoreNozzle.Exit.MassFlowKGS*r.CoreNozzle.VelocityMPS+r.BypassFlowKGS*r.BypassNozzle.VelocityMPS-
									r.Inlet.MassFlowKGS*r.FlightSpeedMPS+r.CoreNozzle.PressureThrustN+r.BypassNozzle.PressureThrustN, 1e-8)
						}
					}
				}
			}
		}
	}
	if valid == 0 || rejected == 0 || choked == 0 || unchoked == 0 {
		t.Fatal("envelope must exercise feasible, infeasible, choked and unchoked cases")
	}
	t.Logf("%d feasible points conserve mass/energy/shaft power; %d infeasible points rejected", valid, rejected)
}

func TestTrendsScalingAndInvalidInputs(t *testing.T) {
	m, in := DefaultModel(), DefaultInputs()
	base, err := m.Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	in.TurbineInletK = 1800
	hot, err := m.Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	if hot.FuelFlowKGS <= base.FuelFlowKGS || hot.NetThrustN <= base.NetThrustN {
		t.Fatal("adding heat must increase fuel and thrust at this fixed-flow design point")
	}
	in = DefaultInputs()
	in.AltitudeM = 11000
	high, err := m.Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	if high.NetThrustN >= base.NetThrustN || high.CoreFlowKGS >= base.CoreFlowKGS {
		t.Fatal("altitude should lower flow and thrust at fixed corrected flow")
	}
	m.CorrectedCoreFlow *= 2
	double, err := m.Solve(DefaultInputs())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "mass-scaled thrust", double.NetThrustN, 2*base.NetThrustN, 1e-8)
	near(t, "mass-scaled fuel", double.FuelFlowKGS, 2*base.FuelFlowKGS, 1e-12)
	near(t, "mass-independent TSFC", double.TSFC, base.TSFC, 1e-12)
	for _, x := range []float64{math.NaN(), math.Inf(1), -1, 11001} {
		in = DefaultInputs()
		in.AltitudeM = x
		if _, err := m.Solve(in); err == nil {
			t.Fatalf("accepted invalid altitude %g", x)
		}
	}
	in = DefaultInputs()
	in.TurbineInletK = 1000
	in.FanPressureRatio = 1.8
	in.BypassRatio = 12
	r, err := m.Solve(in)
	if err == nil {
		t.Fatal("accepted insufficient energy for large fan load")
	}
	if v := Format(r, err); v.Values["net-thrust"] != "—" || v.TemperaturePath != "" {
		t.Fatal("invalid point exposes plausible numbers")
	}
}

func TestNozzleChokingBoundary(t *testing.T) {
	critical := PRef * math.Pow((Gamma+1)/2, Gamma/(Gamma-1))
	lo, err := nozzle(Station{600, critical * (1 - 1e-9), 40}, PRef)
	if err != nil {
		t.Fatal(err)
	}
	hi, err := nozzle(Station{600, critical * (1 + 1e-9), 40}, PRef)
	if err != nil {
		t.Fatal(err)
	}
	if lo.Choked || !hi.Choked {
		t.Fatal("nozzle must switch at the isentropic critical pressure ratio")
	}
	near(t, "continuous velocity", hi.VelocityMPS, lo.VelocityMPS, 1e-5)
	near(t, "continuous area", hi.AreaM2, lo.AreaM2, 1e-8)
	if hi.PressureThrustN <= 0 || lo.PressureThrustN != 0 {
		t.Fatal("pressure thrust only when exit pressure exceeds ambient")
	}
	if _, err := nozzle(Station{600, PRef, 40}, PRef); err == nil {
		t.Fatal("accepted zero pressure head")
	}
}

func TestCSVContainsInputsAssumptionsAndUnroundedStates(t *testing.T) {
	m := DefaultModel()
	r, err := m.Solve(DefaultInputs())
	if err != nil {
		t.Fatal(err)
	}
	// The station block deliberately has four columns; the earlier records three.
	reader := csv.NewReader(strings.NewReader(m.CSV(r)))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 50 {
		t.Fatalf("incomplete export (%d rows)", len(rows))
	}
	for _, text := range []string{"corrected core flow", "fuel LHV", "core pressure thrust", "whole-cycle energy residual", "9 · Core nozzle (static)"} {
		if !strings.Contains(m.CSV(r), text) {
			t.Errorf("export lacks %q", text)
		}
	}
	t.Logf("default point: thrust=%.9f N; fuel=%.9f kg/s; TSFC=%.9f mg/(N s); residual=%g", r.NetThrustN, r.FuelFlowKGS, r.TSFC*1e6, r.MaxRelativeResidual)
}
