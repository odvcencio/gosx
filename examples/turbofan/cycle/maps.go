package cycle

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

//go:embed data/maps.json
var mapData []byte

//go:embed data/jt9d_icao.json
var engineData []byte

//go:embed data/profile.json
var profileData []byte

type Profile struct {
	LowSpeedLoss float64 `json:"lowSpeedLoss"`
	IdleRecovery float64 `json:"idleRecovery"`
	Protocol     string  `json:"protocol"`
}

var profile Profile

type MapScale struct{ Speed, Flow, Pressure, Efficiency float64 }
type rawMap struct {
	Name       string             `json:"name"`
	Speed      []float64          `json:"speed"`
	Coordinate []float64          `json:"coordinate"`
	Flow       [][]float64        `json:"flow"`
	Efficiency [][]float64        `json:"efficiency"`
	Pressure   [][]float64        `json:"pressure"`
	Scale      map[string]float64 `json:"scale"`
}

type Observation struct {
	Mode           string  `json:"mode"`
	ThrustFraction float64 `json:"thrustFraction"`
	FuelFlowKGS    float64 `json:"fuelFlowKGS"`
	Role           string  `json:"role"`
}

type EngineReference struct {
	Engine        string        `json:"engine"`
	UID           string        `json:"uid"`
	RatedThrustN  float64       `json:"ratedThrustN"`
	PressureRatio float64       `json:"pressureRatio"`
	BypassRatio   float64       `json:"bypassRatio"`
	Points        []Observation `json:"points"`
}

var reference EngineReference
var rawMaps map[string]rawMap

func init() {
	if err := json.Unmarshal(mapData, &rawMaps); err != nil {
		panic(err)
	}
	if err := json.Unmarshal(engineData, &reference); err != nil {
		panic(err)
	}
	if err := json.Unmarshal(profileData, &profile); err != nil {
		panic(err)
	}
	for _, m := range rawMaps {
		if err := m.validate(); err != nil {
			panic(err)
		}
	}
}

func Reference() EngineReference {
	r := reference
	r.Points = append([]Observation(nil), r.Points...)
	return r
}

func (m rawMap) validate() error {
	for _, a := range [][]float64{m.Speed, m.Coordinate} {
		if len(a) < 2 {
			return fmt.Errorf("%s: short axis", m.Name)
		}
		for i, x := range a {
			if !finite(x) || (i > 0 && x <= a[i-1]) {
				return fmt.Errorf("%s: unsorted axis", m.Name)
			}
		}
	}
	for _, a := range [][][]float64{m.Flow, m.Efficiency, m.Pressure} {
		if a == nil {
			continue
		}
		if len(a) != len(m.Speed) {
			return fmt.Errorf("%s: map height", m.Name)
		}
		for _, row := range a {
			if len(row) != len(m.Coordinate) {
				return fmt.Errorf("%s: map width", m.Name)
			}
			for _, v := range row {
				if !finite(v) {
					return fmt.Errorf("%s: nonfinite map value", m.Name)
				}
			}
		}
	}
	return nil
}

func bracket(axis []float64, x float64) (int, float64, error) {
	if !finite(x) || x < axis[0]-1e-10 || x > axis[len(axis)-1]+1e-10 {
		return 0, 0, fmt.Errorf("%g outside [%g, %g]", x, axis[0], axis[len(axis)-1])
	}
	x = math.Max(axis[0], math.Min(x, axis[len(axis)-1]))
	i := sort.SearchFloat64s(axis, x) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(axis)-1 {
		i = len(axis) - 2
	}
	return i, (x - axis[i]) / (axis[i+1] - axis[i]), nil
}

func (m rawMap) lookup(n, c float64) (flow, eff, pr float64, err error) {
	i, fy, err := bracket(m.Speed, n)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%s corrected speed: %w", m.Name, err)
	}
	j, fx, err := bracket(m.Coordinate, c)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%s map coordinate: %w", m.Name, err)
	}
	interp := func(a [][]float64) float64 {
		return (1-fy)*((1-fx)*a[i][j]+fx*a[i][j+1]) + fy*((1-fx)*a[i+1][j]+fx*a[i+1][j+1])
	}
	flow, eff = interp(m.Flow), interp(m.Efficiency)
	pr = c
	if m.Pressure != nil {
		pr = interp(m.Pressure)
	}
	return flow, eff, pr, nil
}

type MapPoint struct {
	Name             string  `json:"name"`
	CorrectedSpeed   float64 `json:"correctedSpeed"`
	Coordinate       float64 `json:"coordinate"`
	CorrectedFlowKGS float64 `json:"correctedFlowKGS"`
	PressureRatio    float64 `json:"pressureRatio"`
	Efficiency       float64 `json:"efficiency"`
	Extended         bool    `json:"extended"`
}

func (m *MappedModel) point(name string, n, t, c float64) (MapPoint, error) {
	s := m.Scales[name]
	nc := n / math.Sqrt(t/TRef) / s.Speed
	w, e, p, extended, err := m.lookup(name, nc, c)
	if err != nil {
		return MapPoint{}, err
	}
	p = 1 + (p-1)*s.Pressure
	e *= s.Efficiency
	// A single aggregate correction is fitted to the approach observation.
	// It is not a measurement of an individual manufacturer's component loss.
	if name == "Fan" || name == "LPC" {
		deficit := math.Max(0, 1-nc)
		e *= 1 - m.LowSpeedLoss*deficit*deficit
		// The approach-only correction does not identify efficiency below the
		// fan map's lowest speed. One additional shared coefficient is fitted
		// to idle; it joins the prior correction with zero slope at Nc=0.5.
		idle := math.Max(0, 1-nc/.5)
		e *= 1 + m.IdleRecovery*idle*idle
	}
	if e <= 0 || e > 1 || p < 1 {
		return MapPoint{}, fmt.Errorf("%s map returns nonphysical efficiency or pressure ratio", name)
	}
	return MapPoint{Name: name, CorrectedSpeed: nc, Coordinate: c, CorrectedFlowKGS: w * s.Flow, PressureRatio: p, Efficiency: e, Extended: extended}, nil
}
