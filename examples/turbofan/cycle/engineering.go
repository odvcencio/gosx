package cycle

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// EngineeringRecord contains SI quantities (shaft speed in rpm) derived from solved states.
// Keys end in their unit. plot.* keys are presentation coordinates, excluded
// from engineering CSV. Missing quantities have no entry, rather than zero.
type EngineeringRecord struct {
	Numbers map[string]float64
	Text    map[string]string
}

type engineeringStation struct {
	id string
	s  Station
}

// Analyze does no component matching and does not change the operating point.
// Gas properties follow the selected solver, including its air approximation.
func Analyze(r Result, mapped *MappedResult, model *MappedModel, solveErr error) EngineeringRecord {
	d := EngineeringRecord{Numbers: map[string]float64{}, Text: map[string]string{}}
	if solveErr != nil {
		d.Text["state"] = "No solved point · engineering outputs cleared"
		return d
	}
	h := func(t float64) float64 { return Cp * (t - TRef) }
	s := func(t float64) float64 { return Cp * math.Log(t/TRef) }
	cp := func(float64) float64 { return Cp }
	shaftEta, burnerEta, lhv := .99, .99, 43e6
	d.Text["state"] = "Generic design point · constant gas properties"
	if mapped != nil {
		h, cp = gasH, gasCP
		s = func(t float64) float64 { return gasS(t) - gasS(TRef) }
		d.Text["state"] = "JT9D model estimate · frozen-air gas properties"
	}
	put := func(k string, x float64) {
		if finite(x) {
			d.Numbers[k] = x
		}
	}
	stations := []engineeringStation{{"2", r.Inlet}, {"13", r.Fan}}
	if mapped != nil {
		stations = append(stations, engineeringStation{"25", mapped.LowCompressor})
	}
	stations = append(stations, engineeringStation{"3", r.Compressor}, engineeringStation{"4", r.Burner})
	if mapped != nil {
		stations = append(stations, engineeringStation{"41", mapped.TurbineInlet}, engineeringStation{"45a", mapped.HPTOutlet})
	}
	stations = append(stations, engineeringStation{"45", r.HighTurbine}, engineeringStation{"5", r.LowTurbine}, engineeringStation{"9", r.CoreNozzle.Exit})
	all := append(append([]engineeringStation(nil), stations...), engineeringStation{"19", r.BypassNozzle.Exit})
	for _, q := range all {
		p := "station/" + q.id + "/"
		put(p+"temperature.K", q.s.TemperatureK)
		put(p+"pressure.Pa", q.s.PressurePa)
		put(p+"flow.kg_s", q.s.MassFlowKGS)
		put(p+"enthalpy.J_kg", h(q.s.TemperatureK))
		put(p+"entropy.J_kgK", s(q.s.TemperatureK)-R*math.Log(q.s.PressurePa/PRef))
		put(p+"cp.J_kgK", cp(q.s.TemperatureK))
		put(p+"gamma.1", cp(q.s.TemperatureK)/(cp(q.s.TemperatureK)-R))
		if q.id != "9" && q.id != "19" {
			put(p+"corrected-flow.kg_s", q.s.MassFlowKGS*math.Sqrt(q.s.TemperatureK/TRef)/(q.s.PressurePa/PRef))
		}
	}
	put("air/total.kg_s", r.Inlet.MassFlowKGS)
	put("air/core.kg_s", r.CoreFlowKGS)
	put("air/bypass.kg_s", r.BypassFlowKGS)
	put("fuel/flow.kg_s", r.FuelFlowKGS)
	put("fuel/air-ratio.1", r.FuelAirRatio)
	put("fuel/chemical-power.W", r.FuelFlowKGS*lhv)
	put("fuel/heat-to-gas.W", r.FuelFlowKGS*lhv*burnerEta)
	put("fuel/burner-loss.W", r.FuelFlowKGS*lhv*(1-burnerEta))
	put("fuel/hpt-inlet-cooling.kg_s", 0)
	put("fuel/hpt-exit-cooling.kg_s", 0)
	put("power/hp-load.W", r.CompressorPowerW)
	put("power/lp-load.W", r.FanPowerW)
	put("power/hp-delivered.W", r.CompressorPowerW+r.HighShaftResidualW)
	put("power/lp-delivered.W", r.FanPowerW+r.LowShaftResidualW)
	put("power/shaft-loss.W", (r.CompressorPowerW+r.FanPowerW)*(1/shaftEta-1))
	put("power/thrust.W", r.NetThrustN*r.FlightSpeedMPS)
	put("power/inlet-kinetic.W", .5*r.Inlet.MassFlowKGS*r.FlightSpeedMPS*r.FlightSpeedMPS)
	put("power/inlet-total-enthalpy.W", r.Inlet.MassFlowKGS*h(r.Inlet.TemperatureK))
	put("power/exhaust-total-enthalpy.W", r.LowTurbine.MassFlowKGS*h(r.LowTurbine.TemperatureK)+r.BypassFlowKGS*h(r.Fan.TemperatureK))
	put("power/exhaust-static-enthalpy.W", r.CoreNozzle.Exit.MassFlowKGS*h(r.CoreNozzle.Exit.TemperatureK)+r.BypassNozzle.Exit.MassFlowKGS*h(r.BypassNozzle.Exit.TemperatureK))
	put("power/exhaust-kinetic.W", .5*(r.CoreNozzle.Exit.MassFlowKGS*r.CoreNozzle.VelocityMPS*r.CoreNozzle.VelocityMPS+r.BypassNozzle.Exit.MassFlowKGS*r.BypassNozzle.VelocityMPS*r.BypassNozzle.VelocityMPS))
	put("thrust/specific.Ns_kg", r.NetThrustN/r.Inlet.MassFlowKGS)
	put("flow/bypass-ratio.1", r.BypassFlowKGS/r.CoreFlowKGS)
	put("flight/velocity.m_s", r.FlightSpeedMPS)
	put("closure/burner.W", r.BurnerResidualW)
	put("closure/hp.W", r.HighShaftResidualW)
	put("closure/lp.W", r.LowShaftResidualW)
	put("closure/energy.W", r.EnergyResidualW)
	put("closure/mass.kg_s", r.MassResidualKGS)
	put("closure/relative.1", r.MaxRelativeResidual)
	for _, q := range []struct {
		name string
		n    Nozzle
		in   Station
		air  float64
	}{{"core", r.CoreNozzle, r.LowTurbine, r.CoreFlowKGS}, {"bypass", r.BypassNozzle, r.Fan, r.BypassFlowKGS}} {
		p := "nozzle/" + q.name + "/"
		put(p+"npr.1", q.in.PressurePa/r.Ambient.PressurePa)
		put(p+"critical-npr.1", q.n.CriticalNPR)
		put(p+"choking-ratio.1", q.in.PressurePa/r.Ambient.PressurePa/q.n.CriticalNPR)
		put(p+"mach.1", q.n.Mach)
		put(p+"velocity.m_s", q.n.VelocityMPS)
		put(p+"area.m2", q.n.AreaM2)
		put(p+"density.kg_m3", q.n.Exit.PressurePa/(R*q.n.Exit.TemperatureK))
		put(p+"mass-flux.kg_m2s", q.n.Exit.MassFlowKGS/q.n.AreaM2)
		put(p+"sound-speed.m_s", math.Sqrt(cp(q.n.Exit.TemperatureK)/(cp(q.n.Exit.TemperatureK)-R)*R*q.n.Exit.TemperatureK))
		put(p+"momentum-thrust.N", q.n.Exit.MassFlowKGS*q.n.VelocityMPS)
		put(p+"ram-drag.N", q.air*r.FlightSpeedMPS)
		put(p+"pressure-thrust.N", q.n.PressureThrustN)
		net := q.n.Exit.MassFlowKGS*q.n.VelocityMPS - q.air*r.FlightSpeedMPS + q.n.PressureThrustN
		put(p+"net-thrust.N", net)
		put(p+"thrust-share.1", net/r.NetThrustN)
		d.Text[p+"state"] = "unchoked"
		if q.n.Choked {
			d.Text[p+"state"] = "choked"
		}
	}
	type component struct {
		name    string
		in, out Station
	}
	components := []component{{"Fan", r.Inlet, r.Fan}}
	compressorIn, turbineIn, hpOut := r.Fan, r.Burner, r.HighTurbine
	if mapped != nil {
		components = append(components, component{"LPC", Station{r.Fan.TemperatureK, r.Fan.PressurePa, r.CoreFlowKGS}, mapped.LowCompressor})
		compressorIn, turbineIn, hpOut = mapped.LowCompressor, mapped.TurbineInlet, mapped.HPTOutlet
		put("fuel/hpt-inlet-cooling.kg_s", .055*r.CoreFlowKGS)
		put("fuel/hpt-exit-cooling.kg_s", .035*r.CoreFlowKGS)
		put("power/lp-torque.Nm", r.FanPowerW/(mapped.N1RPM*2*math.Pi/60))
		put("power/hp-torque.Nm", r.CompressorPowerW/(mapped.N2RPM*2*math.Pi/60))
		put("solver/iterations.1", float64(mapped.Iterations))
		put("solver/max-residual.1", mapped.MatchResidual)
		put("solver/tolerance.1", 1e-9)
		if model != nil {
			scales := []float64{model.Rated.CoreFlowKGS, model.Rated.CoreFlowKGS*.965 + reference.Points[0].FuelFlowKGS, model.hpPower, model.Rated.LowTurbine.MassFlowKGS, model.lpPower, model.Rated.LowTurbine.MassFlowKGS, model.Rated.BypassFlowKGS}
			for i, value := range mapped.Residuals {
				unit := "kg_s"
				if i == 2 || i == 4 {
					unit = "W"
				}
				put(fmt.Sprintf("residual/%d/normalized.1", i), value)
				put(fmt.Sprintf("residual/%d/absolute.%s", i, unit), value*scales[i])
			}
		}
	}
	compressorIn.MassFlowKGS = r.CoreFlowKGS
	components = append(components, component{"HPC", compressorIn, r.Compressor}, component{"HPT", turbineIn, hpOut}, component{"LPT", r.HighTurbine, r.LowTurbine})
	for _, q := range components {
		p := "component/" + q.name + "/"
		put(p+"tin.K", q.in.TemperatureK)
		put(p+"tout.K", q.out.TemperatureK)
		put(p+"pressure-ratio.1", math.Max(q.in.PressurePa, q.out.PressurePa)/math.Min(q.in.PressurePa, q.out.PressurePa))
		put(p+"gas-power.W", q.in.MassFlowKGS*(h(q.out.TemperatureK)-h(q.in.TemperatureK)))
		put(p+"corrected-flow.kg_s", q.in.MassFlowKGS*math.Sqrt(q.in.TemperatureK/TRef)/(q.in.PressurePa/PRef))
		if mapped == nil {
			eta := DefaultModel().TurbineEfficiency
			if q.name == "Fan" {
				eta = DefaultModel().FanEfficiency
			} else if q.name == "HPC" {
				eta = DefaultModel().CompressorEfficiency
			}
			put(p+"efficiency.1", eta)
		}
	}
	if mapped != nil && model != nil {
		for _, point := range mapped.MapPoints {
			p := "component/" + point.Name + "/"
			put(p+"efficiency.1", point.Efficiency)
			put(p+"corrected-flow.kg_s", point.CorrectedFlowKGS)
			put(p+"corrected-speed.rpm", point.CorrectedSpeed*model.Scales[point.Name].Speed)
			put(p+"map-speed.1", point.CorrectedSpeed)
			put(p+"map-coordinate.1", point.Coordinate)
			d.Text[p+"region"] = "published table"
			if point.Extended {
				d.Text[p+"region"] = "modeled extension"
			}
		}
	}
	if mapped != nil {
		d.recordPublicLimits(*mapped)
	}
	engineeringPaths(&d, stations, r.Ambient.PressurePa)
	return d
}

func engineeringPaths(d *EngineeringRecord, stations []engineeringStation, ambient float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	pmax := 1.0
	for _, q := range stations {
		x := d.Numbers["station/"+q.id+"/entropy.J_kgK"]
		lo, hi = math.Min(lo, x), math.Max(hi, x)
		pmax = math.Max(pmax, q.s.PressurePa/ambient)
	}
	lo = math.Floor((lo-40)/100) * 100
	hi = math.Ceil((hi+40)/100) * 100
	top := math.Ceil(math.Log10(pmax))
	if top < 1 {
		top = 1
	}
	d.Numbers["plot/s-min.J_kgK"], d.Numbers["plot/s-max.J_kgK"] = lo, hi
	d.Numbers["plot/p-max.1"] = math.Pow(10, top)
	var ts, pressure strings.Builder
	positions := map[string]int{"2": 0, "13": 1, "25": 2, "3": 3, "4": 4, "41": 5, "45a": 6, "45": 7, "5": 8, "9": 9}
	for i, q := range stations {
		x := 48 + 400*(d.Numbers["station/"+q.id+"/entropy.J_kgK"]-lo)/(hi-lo)
		y := 180 - 150*(q.s.TemperatureK-200)/1800
		command := "L"
		if i == 0 {
			command = "M"
		}
		fmt.Fprintf(&ts, "%s%.2f %.2f ", command, x, y)
		px := 48 + 400*float64(positions[q.id])/9
		py := 180 - 150*(math.Log10(q.s.PressurePa/ambient)+.1)/(top+.1)
		fmt.Fprintf(&pressure, "%s%.2f %.2f ", command, px, py)
		d.Numbers["plot/"+q.id+"/ts-x.px"], d.Numbers["plot/"+q.id+"/ts-y.px"] = x, y
		if q.id == "2" || q.id == "3" || q.id == "4" || q.id == "5" {
			d.Text["plot/"+q.id+"/ts-transform"] = fmt.Sprintf("translate(%.2f %.2f)", x, y)
		}
	}
	d.Text["plot/ts-path"], d.Text["plot/pressure-path"] = ts.String(), pressure.String()
}

func (d EngineeringRecord) CSV() string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"engineering diagnostics", "derived model quantities; SI and shaft rpm; not independent measurements", ""})
	_ = w.Write([]string{"quantity", "value", "unit"})
	keys := make([]string, 0, len(d.Numbers))
	for k := range d.Numbers {
		if !strings.HasPrefix(k, "plot/") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		i := strings.LastIndex(k, ".")
		unit := k[i+1:]
		if expanded, ok := map[string]string{"kg_s": "kg/s", "J_kg": "J/kg", "J_kgK": "J/(kg K)", "kg_m3": "kg/m3", "kg_m2s": "kg/(m2 s)", "Ns_kg": "N s/kg", "m_s": "m/s", "rpm_s_kg": "rpm s/kg"}[unit]; ok {
			unit = expanded
		}
		_ = w.Write([]string{k[:i], strconv.FormatFloat(d.Numbers[k], 'g', 16, 64), unit})
	}
	keys = keys[:0]
	for k := range d.Text {
		if !strings.HasPrefix(k, "plot/") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		_ = w.Write([]string{k, d.Text[k], ""})
	}
	w.Flush()
	return b.String()
}
