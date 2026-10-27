package cycle

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestEngineeringLedgersAndCoolingStates(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	for _, obs := range Reference().Points {
		r, err := m.Solve(MappedInputs{FuelFlowKGS: obs.FuelFlowKGS})
		if err != nil {
			t.Fatal(err)
		}
		d := Analyze(r.Cycle, &r, m, nil)
		n := d.Numbers
		near(t, "stream thrust sums", n["nozzle/core/net-thrust.N"]+n["nozzle/bypass/net-thrust.N"], r.Cycle.NetThrustN, .001)
		near(t, "fuel heat split", n["fuel/heat-to-gas.W"]+n["fuel/burner-loss.W"], n["fuel/chemical-power.W"], 1e-7)
		near(t, "whole engine enthalpy ledger", n["power/exhaust-total-enthalpy.W"]+n["power/shaft-loss.W"]-n["power/inlet-total-enthalpy.W"]-n["fuel/heat-to-gas.W"], r.Cycle.EnergyResidualW, 1e-7)
		near(t, "nozzle total-to-static energy", n["power/exhaust-static-enthalpy.W"]+n["power/exhaust-kinetic.W"], n["power/exhaust-total-enthalpy.W"], .05)
		near(t, "LP torque", n["power/lp-torque.Nm"]*r.N1RPM*2*math.Pi/60, r.Cycle.FanPowerW, 1e-6)
		near(t, "HP torque", n["power/hp-torque.Nm"]*r.N2RPM*2*math.Pi/60, r.Cycle.CompressorPowerW, 1e-6)
		near(t, "HP turbine gas work", -n["component/HPT/gas-power.W"]*.99, n["power/hp-delivered.W"], 1e-6)
		near(t, "LP turbine gas work", -n["component/LPT/gas-power.W"]*.99, n["power/lp-delivered.W"], 1e-6)
		near(t, "LP load components", n["component/Fan/gas-power.W"]+n["component/LPC/gas-power.W"], n["power/lp-load.W"], 1e-6)
		mixedEnergy := r.HPTOutlet.MassFlowKGS*gasH(r.HPTOutlet.TemperatureK) + n["fuel/hpt-exit-cooling.kg_s"]*gasH(r.Cycle.Compressor.TemperatureK)
		near(t, "cooling mix enthalpy", mixedEnergy, r.Cycle.HighTurbine.MassFlowKGS*gasH(r.Cycle.HighTurbine.TemperatureK), .001)
		near(t, "HP absolute residual", n["residual/2/absolute.W"], r.Cycle.HighShaftResidualW, 1e-10)
		near(t, "core absolute flow residual", n["residual/5/absolute.kg_s"], r.Cycle.CoreNozzle.Exit.MassFlowKGS-r.Cycle.LowTurbine.MassFlowKGS, 1e-12)
		maxResidual := 0.0
		for _, v := range r.Residuals {
			maxResidual = math.Max(maxResidual, math.Abs(v))
		}
		near(t, "max matching audit", maxResidual, r.MatchResidual, 0)
		for _, name := range []string{"core", "bypass"} {
			p := "nozzle/" + name + "/"
			near(t, "exit continuity", n[p+"density.kg_m3"]*n[p+"velocity.m_s"], n[p+"mass-flux.kg_m2s"], 1e-6)
			near(t, "exit Mach", n[p+"velocity.m_s"]/n[p+"sound-speed.m_s"], n[p+"mach.1"], 1e-12)
			if (n[p+"choking-ratio.1"] >= 1) != (d.Text[p+"state"] == "choked") {
				t.Fatal("choking diagnosis contradicts the nozzle")
			}
		}
		near(t, "nozzle entropy", n["station/9/entropy.J_kgK"], n["station/5/entropy.J_kgK"], 1e-5)
		if _, ok := n["station/9/corrected-flow.kg_s"]; ok {
			t.Fatal("static exit presented as a corrected total flow")
		}
		t.Logf("%s: net %.3f N, fuel heat %.6f MW, LP torque %.6f kN m, 7 residuals max %.3g", obs.Mode, r.Cycle.NetThrustN, n["fuel/heat-to-gas.W"]/1e6, n["power/lp-torque.Nm"]/1000, maxResidual)
	}
}

func TestEngineeringGenericAndInvalidPoint(t *testing.T) {
	in := DefaultInputs()
	in.TurbineInletK = 1800
	r, err := DefaultModel().Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	d := Analyze(r, nil, nil, nil)
	if d.Text["nozzle/core/state"] != "choked" || d.Numbers["nozzle/core/choking-ratio.1"] <= 1 {
		t.Fatal("generic sonic threshold missing")
	}
	near(t, "perfect-gas entropy", d.Numbers["station/9/entropy.J_kgK"], d.Numbers["station/5/entropy.J_kgK"], 1e-8)
	if _, ok := d.Numbers["power/lp-torque.Nm"]; ok {
		t.Fatal("generic model invented shaft RPM/torque")
	}
	for _, key := range []string{"plot/ts-path", "plot/pressure-path"} {
		if strings.Contains(d.Text[key], "NaN") || !strings.HasPrefix(d.Text[key], "M") {
			t.Fatal("invalid engineering diagram")
		}
	}
	bad := Analyze(r, nil, nil, errors.New("invalid point"))
	if len(bad.Numbers) != 0 || len(bad.Text) != 1 {
		t.Fatal("invalid point retained engineering results")
	}
	if strings.Contains(d.CSV(), "plot/") || !strings.Contains(d.CSV(), "fuel/chemical-power") {
		t.Fatal("engineering export lost raw engineering quantities")
	}
}

func TestEngineeringSweepAndSampledSensitivity(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	points, err := m.Sweep(81)
	if err != nil || len(points) != 81 {
		t.Fatal(err)
	}
	near(t, "idle sweep", points[0].Thrust, 14945, .01)
	near(t, "rated sweep", points[80].Thrust, 213500, .01)
	if !points[0].Extended || points[80].Extended {
		t.Fatal("sweep lost modeled-region flags")
	}
	for i := 1; i < len(points); i++ {
		if points[i].Thrust <= points[i-1].Thrust {
			t.Fatal("nonmonotonic sweep")
		}
	}
	for _, chart := range m.SweepCharts(points) {
		if strings.Contains(chart.Path, "NaN") || !strings.HasPrefix(chart.Path, "M") {
			t.Fatal("invalid operating line")
		}
	}
	d := EngineeringRecord{Numbers: map[string]float64{"fuel/flow.kg_s": 2}, Text: map[string]string{}}
	d.SampleSensitivity([]SweepPoint{{Fuel: 1, Thrust: 2, N1: 10, N2: 20}, {Fuel: 3, Thrust: 18, N1: 30, N2: 40}}, 2)
	near(t, "quadratic secant slope", d.Numbers["sensitivity/thrust.Ns_kg"], 8, 0)
	if !strings.Contains(SweepCSV(points), "not test observations") {
		t.Fatal("sweep export lost model provenance")
	}
}
