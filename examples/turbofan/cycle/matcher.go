package cycle

import (
	"fmt"
	"math"
)

func cost(r [7]float64) float64 {
	v := 0.0
	for _, e := range r {
		v += e * e
	}
	return v
}

func linearSolve(a [7][7]float64, b [7]float64) ([7]float64, bool) {
	for i := range 7 {
		pivot := i
		for j := i + 1; j < 7; j++ {
			if math.Abs(a[j][i]) > math.Abs(a[pivot][i]) {
				pivot = j
			}
		}
		if math.Abs(a[pivot][i]) < 1e-18 {
			return [7]float64{}, false
		}
		a[i], a[pivot] = a[pivot], a[i]
		b[i], b[pivot] = b[pivot], b[i]
		v := a[i][i]
		for j := i; j < 7; j++ {
			a[i][j] /= v
		}
		b[i] /= v
		for row := range 7 {
			if row == i {
				continue
			}
			v = a[row][i]
			for j := i; j < 7; j++ {
				a[row][j] -= v * a[i][j]
			}
			b[row] -= v * b[i]
		}
	}
	return b, true
}

func (m *MappedModel) match(x [7]float64, in MappedInputs) ([7]float64, MappedResult, int, error) {
	lambda := 1e-5
	r, state, err := m.evaluate(x, in)
	if err != nil {
		return x, state, 0, err
	}
	for iteration := 0; iteration < 70; iteration++ {
		if state.MatchResidual < 1e-9 {
			return x, state, iteration, nil
		}
		var jac [7][7]float64
		for j := range 7 {
			step := 1e-5 * math.Max(1, math.Abs(x[j]))
			plus, minus := x, x
			plus[j] += step
			minus[j] -= step
			rp, _, ep := m.evaluate(plus, in)
			rm, _, em := m.evaluate(minus, in)
			if ep != nil && em != nil {
				return x, state, iteration, fmt.Errorf("cannot differentiate inside map domain: %w", ep)
			}
			for i := range 7 {
				if ep == nil && em == nil {
					jac[i][j] = (rp[i] - rm[i]) / (2 * step)
				} else if ep == nil {
					jac[i][j] = (rp[i] - r[i]) / step
				} else {
					jac[i][j] = (r[i] - rm[i]) / step
				}
			}
		}
		improved := false
		for range 15 {
			var a [7][7]float64
			var b [7]float64
			for j := range 7 {
				for i := range 7 {
					b[j] -= jac[i][j] * r[i]
					for k := range 7 {
						a[j][k] += jac[i][j] * jac[i][k]
					}
				}
				a[j][j] += lambda * math.Max(a[j][j], 1e-6)
			}
			delta, ok := linearSolve(a, b)
			if !ok {
				lambda *= 10
				continue
			}
			candidate := x
			for j := range 7 {
				candidate[j] += delta[j]
			}
			rn, sn, e := m.evaluate(candidate, in)
			if e == nil && cost(rn) < cost(r) {
				x, r, state = candidate, rn, sn
				lambda = math.Max(1e-12, lambda*.2)
				improved = true
				break
			}
			lambda *= 10
		}
		if !improved {
			return x, state, iteration, fmt.Errorf("matching stalled at relative residual %.3g", state.MatchResidual)
		}
	}
	return x, state, 70, fmt.Errorf("matching iteration limit, relative residual %.3g", state.MatchResidual)
}
