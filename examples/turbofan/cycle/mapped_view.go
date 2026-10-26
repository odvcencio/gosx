package cycle

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type CalibrationCheck struct {
	Mode, Role, Fuel, Target, Prediction, Error, Status string
}

func (m *MappedModel) Checks() []CalibrationCheck {
	var rows []CalibrationCheck
	for _, p := range reference.Points {
		q := CalibrationCheck{Mode: p.Mode, Role: p.Role, Fuel: fmt.Sprintf("%.4f", p.FuelFlowKGS), Target: fmt.Sprintf("%.2f", p.ThrustFraction*reference.RatedThrustN/1000), Prediction: "—", Error: "—", Status: "outside coverage"}
		if r, err := m.Solve(MappedInputs{FuelFlowKGS: p.FuelFlowKGS}); err == nil {
			q.Prediction = fmt.Sprintf("%.2f", r.Cycle.NetThrustN/1000)
			q.Error = fmt.Sprintf("%+.2f%%", 100*(r.Cycle.NetThrustN/(p.ThrustFraction*reference.RatedThrustN)-1))
			q.Status = "converged"
		}
		rows = append(rows, q)
	}
	return rows
}

type MapPlot struct {
	Name, Label, MaxFlow, MaxPressure string
	Paths                             []string
	X, Y                              string
}

func (m *MappedModel) plotBounds(name string) (float64, float64) {
	r := rawMaps[name]
	maxw, maxp := 0.0, 1.0
	s := m.Scales[name]
	for i := range r.Speed {
		for j := range r.Coordinate {
			maxw = math.Max(maxw, r.Flow[i][j]*s.Flow)
			p := r.Coordinate[j]
			if r.Pressure != nil {
				p = r.Pressure[i][j]
			}
			maxp = math.Max(maxp, 1+(p-1)*s.Pressure)
		}
	}
	return math.Ceil(maxw/25) * 25, math.Ceil(maxp*10) / 10
}

func (m *MappedModel) PlotPoint(p MapPoint) (string, string) {
	w, pr := m.plotBounds(p.Name)
	return fmt.Sprintf("%.2f", 35+290*p.CorrectedFlowKGS/w), fmt.Sprintf("%.2f", 175-145*(p.PressureRatio-1)/(pr-1))
}

func (m *MappedModel) Plots() []MapPlot {
	var plots []MapPlot
	for _, name := range []string{"Fan", "HPC"} {
		w, pr := m.plotBounds(name)
		r := rawMaps[name]
		scale := m.Scales[name]
		p := MapPlot{Name: name, Label: map[string]string{"Fan": "Fan map", "HPC": "High-pressure compressor map"}[name], MaxFlow: fmt.Sprintf("%.0f", w), MaxPressure: fmt.Sprintf("%.1f", pr)}
		for i := range r.Speed {
			var path strings.Builder
			for j := range r.Coordinate {
				command := "L"
				if j == 0 {
					command = "M"
				}
				x := 35 + 290*r.Flow[i][j]*scale.Flow/w
				y := 175 - 145*((r.Pressure[i][j]-1)*scale.Pressure)/(pr-1)
				fmt.Fprintf(&path, "%s%.2f %.2f ", command, x, y)
			}
			p.Paths = append(p.Paths, strings.TrimSpace(path.String()))
		}
		plots = append(plots, p)
	}
	return plots
}

func FormatMapped(r MappedResult, err error) Display {
	v := Format(r.Cycle, err)
	for _, key := range []string{"n1", "n2", "mapped-opr", "mapped-bypass", "booster-temperature", "hpt-inlet-temperature", "match-residual"} {
		v.Values[key] = "—"
	}
	if err != nil {
		v.Values["status"] = "No matched point: " + err.Error()
		return v
	}
	v.Values["n1"] = fmt.Sprintf("%.0f", r.N1RPM)
	v.Values["n2"] = fmt.Sprintf("%.0f", r.N2RPM)
	v.Values["mapped-opr"] = fmt.Sprintf("%.2f", r.Cycle.Inputs.OverallPressureRatio)
	v.Values["mapped-bypass"] = fmt.Sprintf("%.2f", r.Cycle.Inputs.BypassRatio)
	v.Values["booster-temperature"] = fmt.Sprintf("%.0f", r.LowCompressor.TemperatureK)
	v.Values["hpt-inlet-temperature"] = fmt.Sprintf("%.0f", r.TurbineInlet.TemperatureK)
	v.Values["match-residual"] = fmt.Sprintf("%.2e", r.MatchResidual)
	v.Values["status"] = "JT9D ground profile · five maps matched; fixed nozzle areas; both shaft balances close."
	return v
}

func (m *MappedModel) CSV(r MappedResult) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	n := func(x float64) string { return strconv.FormatFloat(x, 'g', 16, 64) }
	put := func(name string, x float64, unit string) { _ = w.Write([]string{name, n(x), unit}) }
	_ = w.Write([]string{"profile", reference.Engine, "ICAO " + reference.UID})
	_ = w.Write([]string{"scope", "ground calibration to takeoff and approach; climbout unfitted; idle unsupported", ""})
	_ = w.Write([]string{"gas", "temperature-dependent frozen air NASA7; air-equivalent combustion products", ""})
	_ = w.Write([]string{"map source", "NASA T-MATS ad6e4d5d0d76c229db4eb72ca40ef58d5ddc4014", "scaled shapes, not proprietary manufacturer maps"})
	_ = w.Write([]string{"quantity", "value", "unit"})
	for _, q := range []struct {
		name, unit string
		value      float64
	}{
		{"altitude", "m", r.Command.AltitudeM}, {"Mach", "1", r.Command.Mach}, {"fuel command", "kg/s", r.Command.FuelFlowKGS},
		{"net thrust", "N", r.Cycle.NetThrustN}, {"core thrust", "N", r.Cycle.CoreThrustN}, {"bypass thrust", "N", r.Cycle.BypassThrustN},
		{"model N1", "rpm", r.N1RPM}, {"model N2", "rpm", r.N2RPM}, {"overall pressure ratio", "1", r.Cycle.Inputs.OverallPressureRatio}, {"bypass ratio", "1", r.Cycle.Inputs.BypassRatio},
		{"fixed core area", "m2", m.CoreAreaM2}, {"fixed bypass area", "m2", m.BypassAreaM2}, {"low-speed loss correction", "1", m.LowSpeedLoss},
		{"gas constant", "J/(kg K)", R}, {"fuel LHV", "J/kg", 43e6}, {"burner efficiency", "1", .99}, {"shaft efficiency", "1", .99}, {"burner pressure recovery", "1", .945},
		{"HPT inlet cooling fraction", "1", .055}, {"HPT outlet cooling fraction", "1", .035}, {"HP shaft power", "W", r.Cycle.CompressorPowerW}, {"LP shaft power (fan plus booster)", "W", r.Cycle.FanPowerW},
		{"matching residual", "1", r.MatchResidual}, {"mass residual", "kg/s", r.Cycle.MassResidualKGS}, {"energy residual", "W", r.Cycle.EnergyResidualW},
	} {
		put(q.name, q.value, q.unit)
	}
	if r.Cycle.NetThrustN > 0 {
		put("TSFC", r.Cycle.TSFC, "kg/(N s)")
	}
	_ = w.Write([]string{"station", "temperature K", "pressure Pa", "flow kg/s"})
	for _, q := range stationRows(r.Cycle) {
		_ = w.Write([]string{q.name, n(q.state.TemperatureK), n(q.state.PressurePa), n(q.state.MassFlowKGS)})
	}
	for _, q := range []struct {
		name string
		s    Station
	}{{"25 Booster exit", r.LowCompressor}, {"41 Cooled HPT inlet", r.TurbineInlet}} {
		_ = w.Write([]string{q.name, n(q.s.TemperatureK), n(q.s.PressurePa), n(q.s.MassFlowKGS)})
	}
	_ = w.Write([]string{"component", "map corrected speed", "map coordinate", "corrected flow kg/s", "pressure ratio", "efficiency"})
	for _, p := range r.MapPoints {
		_ = w.Write([]string{p.Name, n(p.CorrectedSpeed), n(p.Coordinate), n(p.CorrectedFlowKGS), n(p.PressureRatio), n(p.Efficiency)})
	}
	w.Flush()
	return b.String()
}
