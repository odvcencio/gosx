package cycle

import (
	"fmt"
	"math"
)

// lookup preserves every published table value. Outside those tables, only
// explicitly bounded low-speed compressor and LPT extensions are available.
// These are engineering assumptions anchored to NASA data, not measured maps.
// See MODEL.md and NASA/CP-2018-219785 p.56 (PDF p.62).
func (m *MappedModel) lookup(name string, nc, coordinate float64) (w, eta, pr float64, extended bool, err error) {
	r := rawMaps[name]
	loN, loC := r.Speed[0], r.Coordinate[0]
	if nc >= loN && coordinate >= loC {
		w, eta, pr, err = r.lookup(nc, coordinate)
		return
	}
	if !finite(nc) || !finite(coordinate) {
		err = fmt.Errorf("%s: nonfinite map point", name)
		return
	}
	if name == "Fan" || name == "LPC" {
		if nc < .2 || nc >= loN || coordinate < loC {
			err = fmt.Errorf("%s: outside bounded compressor extension", name)
			return
		}
		w, eta, pr, err = r.lookup(loN, coordinate)
		q := nc / loN
		w *= q
		pr = 1 + (pr-1)*q*q
		extended = true
		return
	}
	if name != "LPT" || nc < 20 || coordinate < 1.05 || nc > r.Speed[len(r.Speed)-1] || coordinate > r.Coordinate[len(r.Coordinate)-1] {
		err = fmt.Errorf("%s: outside bounded turbine extension", name)
		return
	}
	// Below the lowest speed, retain boundary swallowing capacity and reduce
	// work efficiency using a blade-speed-ratio parabola. At lower PR, Stodola's
	// ellipse approaches zero flow at unity PR. Use scaled physical PR, not the
	// unscaled map coordinate, for both flow and ideal jet-speed ratios.
	w, eta, _, err = r.lookup(math.Max(loN, nc), math.Max(loC, coordinate))
	if err != nil {
		return
	}
	u := math.Min(1, nc/loN)
	if coordinate < loC {
		p0 := 1 + (loC-1)*m.Scales[name].Pressure
		p := 1 + (coordinate-1)*m.Scales[name].Pressure
		w *= math.Sqrt((1 - 1/(p*p)) / (1 - 1/(p0*p0)))
		u *= math.Sqrt((1 - math.Pow(p0, -.25)) / (1 - math.Pow(p, -.25)))
	}
	eta *= math.Max(.05, math.Min(1, u*(2-u)))
	return w, eta, coordinate, true, nil
}

// FitIdleRecovery consumes the idle observation explicitly. It does not turn
// that point into validation evidence or infer a manufacturer's component map.
func FitIdleRecovery(m *MappedModel) (float64, error) {
	var idle Observation
	for _, p := range reference.Points {
		if p.Mode == "idle" {
			idle = p
		}
	}
	target := idle.ThrustFraction * reference.RatedThrustN
	var previous, previousError float64
	previousOK := false
	for recovery := 0.0; recovery <= 4; recovery += .25 {
		m.IdleRecovery = recovery
		r, err := m.Solve(MappedInputs{FuelFlowKGS: idle.FuelFlowKGS})
		if err != nil {
			previousOK = false
			continue
		}
		e := r.Cycle.NetThrustN - target
		if previousOK && e*previousError <= 0 {
			lo, hi := previous, recovery
			for range 35 {
				mid := (lo + hi) / 2
				m.IdleRecovery = mid
				r, err = m.Solve(MappedInputs{FuelFlowKGS: idle.FuelFlowKGS})
				if err != nil {
					return 0, err
				}
				if (r.Cycle.NetThrustN-target)*previousError > 0 {
					lo = mid
				} else {
					hi = mid
				}
			}
			m.IdleRecovery = (lo + hi) / 2
			return m.IdleRecovery, nil
		}
		previous, previousError, previousOK = recovery, e, true
	}
	m.IdleRecovery = 0
	return 0, fmt.Errorf("idle recovery fit has no feasible bracket in [0,4]")
}
