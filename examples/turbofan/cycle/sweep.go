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

type SweepPoint struct {
	Fuel, Thrust, N1, N2, OPR, TSFC float64
	Extended                        bool
}
type SweepChart struct {
	X, Y, SecondY                               string
	Key, Label, Unit, Path, SecondPath, Maximum string
	MaxY                                        float64
}

// Sweep runs the fixed model, rather than interpolating the calibration data.
func (m *MappedModel) Sweep(count int) ([]SweepPoint, error) {
	if count < 2 || count > 1000 {
		return nil, fmt.Errorf("sweep count must be between 2 and 1000")
	}
	points := make([]SweepPoint, 0, count)
	for i := 0; i < count; i++ {
		fuel := .2054 + (2.055-.2054)*float64(i)/float64(count-1)
		r, err := m.Solve(MappedInputs{FuelFlowKGS: fuel})
		if err != nil {
			return nil, fmt.Errorf("sweep at %.5f kg/s: %w", fuel, err)
		}
		q := SweepPoint{Fuel: fuel, Thrust: r.Cycle.NetThrustN, N1: r.N1RPM, N2: r.N2RPM, OPR: r.Cycle.Inputs.OverallPressureRatio, TSFC: r.Cycle.TSFC}
		for _, p := range r.MapPoints {
			q.Extended = q.Extended || p.Extended
		}
		points = append(points, q)
	}
	return points, nil
}

func (m *MappedModel) SweepCharts(points []SweepPoint) []SweepChart {
	charts := []SweepChart{{Key: "thrust", Label: "Net thrust", Unit: "kN", MaxY: 225}, {Key: "speed", Label: "Spool speeds", Unit: "% rated model reference", MaxY: 110}, {Key: "opr", Label: "Overall pressure ratio", Unit: "ratio", MaxY: 25}, {Key: "tsfc", Label: "Specific fuel consumption", Unit: "mg/(N·s)"}}
	for _, p := range points {
		charts[3].MaxY = math.Max(charts[3].MaxY, p.TSFC*1e6)
	}
	charts[3].MaxY = math.Ceil(charts[3].MaxY/2) * 2
	for i := range charts {
		chart := &charts[i]
		var path, second strings.Builder
		for j, p := range points {
			value, other := SweepValues(p, chart.Key, m)
			x, y := SweepPosition(p.Fuel, value, chart.MaxY)
			command := "L"
			if j == 0 {
				command = "M"
			}
			fmt.Fprintf(&path, "%s%.2f %.2f ", command, x, y)
			if chart.Key == "speed" {
				_, yy := SweepPosition(p.Fuel, other, chart.MaxY)
				fmt.Fprintf(&second, "%s%.2f %.2f ", command, x, yy)
			}
		}
		chart.Path, chart.SecondPath = path.String(), second.String()
		chart.Maximum = strconv.FormatFloat(chart.MaxY, 'f', 0, 64)
	}
	return charts
}

func SweepValues(p SweepPoint, key string, m *MappedModel) (float64, float64) {
	switch key {
	case "thrust":
		return p.Thrust / 1000, 0
	case "speed":
		return 100 * p.N1 / m.ReferenceN1RPM, 100 * p.N2 / m.ReferenceN2RPM
	case "opr":
		return p.OPR, 0
	default:
		return p.TSFC * 1e6, 0
	}
}

func SweepPosition(fuel, value, maximum float64) (float64, float64) {
	return 48 + 400*(fuel-.2054)/(2.055-.2054), 180 - 150*value/maximum
}

func (d *EngineeringRecord) SampleSensitivity(points []SweepPoint, fuel float64) {
	if len(points) < 2 || d.Numbers["fuel/flow.kg_s"] == 0 {
		return
	}
	i := sort.Search(len(points), func(i int) bool { return points[i].Fuel >= fuel })
	i = max(1, min(i, len(points)-1))
	a, b := points[i-1], points[i]
	d.Numbers["sensitivity/thrust.Ns_kg"] = (b.Thrust - a.Thrust) / (b.Fuel - a.Fuel)
	d.Numbers["sensitivity/n1.rpm_s_kg"] = (b.N1 - a.N1) / (b.Fuel - a.Fuel)
	d.Numbers["sensitivity/n2.rpm_s_kg"] = (b.N2 - a.N2) / (b.Fuel - a.Fuel)
	d.Text["sensitivity/bracket"] = fmt.Sprintf("Model secant over %.4f–%.4f kg/s fuel; sampled sensitivity, not a measured derivative.", a.Fuel, b.Fuel)
}

func SweepCSV(points []SweepPoint) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"model sweep", "JT9D ground model; fixed geometry; not test observations"})
	_ = w.Write([]string{"fuel kg/s", "net thrust N", "N1 rpm", "N2 rpm", "OPR", "TSFC kg/(N s)", "uses modeled map extension"})
	for _, p := range points {
		row := []string{}
		for _, x := range []float64{p.Fuel, p.Thrust, p.N1, p.N2, p.OPR, p.TSFC} {
			row = append(row, strconv.FormatFloat(x, 'g', 16, 64))
		}
		_ = w.Write(append(row, strconv.FormatBool(p.Extended)))
	}
	w.Flush()
	return b.String()
}
