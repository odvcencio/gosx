package cycle

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
)

type StationRow struct{ ID, Name, T, P, Flow string }

// Display is shared by server rendering and the browser engine. Its values
// are rounded for reading; CSV retains the solver's numerical precision.
type Display struct {
	Values          map[string]string
	Stations        []StationRow
	TemperaturePath string
}

func stationRows(r Result) []struct {
	id, name string
	state    Station
} {
	return []struct {
		id, name string
		state    Station
	}{
		{"2", "2 · Inlet", r.Inlet}, {"13", "13 · Fan exit", r.Fan},
		{"3", "3 · Compressor exit", r.Compressor}, {"4", "4 · Burner exit", r.Burner},
		{"45", "45 · HP turbine exit", r.HighTurbine}, {"5", "5 · LP turbine exit", r.LowTurbine},
		{"19", "19 · Bypass nozzle (static)", r.BypassNozzle.Exit}, {"9", "9 · Core nozzle (static)", r.CoreNozzle.Exit},
	}
}

func Format(r Result, err error) Display {
	v := Display{Values: map[string]string{
		"net-thrust": "—", "fuel-flow": "—", "tsfc": "—", "bypass-share": "—",
		"fan-power": "—", "compressor-power": "—", "core-nozzle": "—", "bypass-nozzle": "—",
		"core-area": "—", "bypass-area": "—", "residual": "—", "status": "",
	}}
	for _, s := range stationRows(r) {
		row := StationRow{ID: s.id, Name: s.name, T: "—", P: "—", Flow: "—"}
		if err == nil {
			row.T = fmt.Sprintf("%.0f", s.state.TemperatureK)
			row.P = fmt.Sprintf("%.1f", s.state.PressurePa/1000)
			row.Flow = fmt.Sprintf("%.2f", s.state.MassFlowKGS)
		}
		v.Stations = append(v.Stations, row)
	}
	if err != nil {
		v.Values["status"] = "Infeasible point: " + err.Error()
		return v
	}
	v.Values["net-thrust"] = fmt.Sprintf("%.1f", r.NetThrustN/1000)
	v.Values["fuel-flow"] = fmt.Sprintf("%.3f", r.FuelFlowKGS)
	if r.NetThrustN > 0 {
		v.Values["tsfc"] = fmt.Sprintf("%.1f", r.TSFC*1e6)
		v.Values["bypass-share"] = fmt.Sprintf("%.0f", r.BypassThrustN/r.NetThrustN*100)
	}
	v.Values["fan-power"] = fmt.Sprintf("%.2f", r.FanPowerW/1e6)
	v.Values["compressor-power"] = fmt.Sprintf("%.2f", r.CompressorPowerW/1e6)
	for name, nozzle := range map[string]Nozzle{"core": r.CoreNozzle, "bypass": r.BypassNozzle} {
		state := "Unchoked"
		if nozzle.Choked {
			state = "Choked"
		}
		v.Values[name+"-nozzle"] = fmt.Sprintf("%s · M %.2f", state, nozzle.Mach)
		v.Values[name+"-area"] = fmt.Sprintf("%.3f", nozzle.AreaM2)
	}
	v.Values["residual"] = fmt.Sprintf("%.2e", r.MaxRelativeResidual)
	v.Values["status"] = "Steady point · mass, energy and both shaft balances close."
	if r.NetThrustN <= 0 {
		v.Values["status"] = "Negative net thrust at this design point; specific fuel consumption is undefined."
	}
	var path strings.Builder
	for i, s := range stationRows(r)[:6] {
		command := "L"
		if i == 0 {
			command = "M"
		}
		fmt.Fprintf(&path, "%s%.1f %.1f ", command, 40+float64(i)*128, 200-s.state.TemperatureK/1800*172)
	}
	v.TemperaturePath = strings.TrimSpace(path.String())
	return v
}

// CSV exports one design point, all assumptions and unrounded station results.
func (m Model) CSV(r Result) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	write := func(a, value, unit string) { _ = w.Write([]string{a, value, unit}) }
	n := func(x float64) string { return strconv.FormatFloat(x, 'g', 16, 64) }
	write("Turbofan steady design point", "calorically perfect gas; uncalibrated; variable nozzle areas", "")
	write("quantity", "value", "unit")
	for _, s := range []struct {
		name, unit string
		value      float64
	}{
		{"altitude", "m", r.Inputs.AltitudeM}, {"Mach", "1", r.Inputs.Mach},
		{"turbine inlet temperature", "K", r.Inputs.TurbineInletK}, {"overall pressure ratio", "1", r.Inputs.OverallPressureRatio},
		{"fan pressure ratio", "1", r.Inputs.FanPressureRatio}, {"bypass ratio", "1", r.Inputs.BypassRatio},
		{"gamma", "1", Gamma}, {"gas constant", "J/(kg K)", R}, {"specific heat", "J/(kg K)", Cp},
		{"corrected core flow", "kg/s", m.CorrectedCoreFlow}, {"reference temperature", "K", TRef}, {"reference pressure", "Pa", PRef},
		{"fan efficiency", "1", m.FanEfficiency}, {"compressor efficiency", "1", m.CompressorEfficiency},
		{"turbine efficiency", "1", m.TurbineEfficiency}, {"shaft efficiency", "1", m.ShaftEfficiency},
		{"burner efficiency", "1", m.BurnerEfficiency}, {"inlet pressure recovery", "1", m.InletRecovery},
		{"burner pressure recovery", "1", m.BurnerRecovery}, {"fuel LHV", "J/kg", m.FuelHeatingValue},
		{"ambient temperature", "K", r.Ambient.TemperatureK}, {"ambient pressure", "Pa", r.Ambient.PressurePa},
		{"flight speed", "m/s", r.FlightSpeedMPS}, {"net thrust", "N", r.NetThrustN},
		{"core thrust", "N", r.CoreThrustN}, {"bypass thrust", "N", r.BypassThrustN},
		{"fuel flow", "kg/s", r.FuelFlowKGS}, {"fan shaft power", "W", r.FanPowerW}, {"compressor shaft power", "W", r.CompressorPowerW},
		{"core nozzle area", "m2", r.CoreNozzle.AreaM2}, {"bypass nozzle area", "m2", r.BypassNozzle.AreaM2},
		{"core nozzle Mach", "1", r.CoreNozzle.Mach}, {"bypass nozzle Mach", "1", r.BypassNozzle.Mach},
		{"core pressure thrust", "N", r.CoreNozzle.PressureThrustN}, {"bypass pressure thrust", "N", r.BypassNozzle.PressureThrustN},
		{"burner energy residual", "W", r.BurnerResidualW}, {"HP shaft residual", "W", r.HighShaftResidualW}, {"LP shaft residual", "W", r.LowShaftResidualW},
		{"whole-cycle energy residual", "W", r.EnergyResidualW}, {"mass residual", "kg/s", r.MassResidualKGS},
	} {
		write(s.name, n(s.value), s.unit)
	}
	if r.NetThrustN > 0 {
		write("TSFC", n(r.TSFC), "kg/(N s)")
	} else {
		write("TSFC", "undefined (nonpositive net thrust)", "kg/(N s)")
	}
	_ = w.Write([]string{"station", "temperature K", "pressure Pa", "flow kg/s"})
	for _, s := range stationRows(r) {
		_ = w.Write([]string{s.name, n(s.state.TemperatureK), n(s.state.PressurePa), n(s.state.MassFlowKGS)})
	}
	w.Flush()
	return b.String()
}
