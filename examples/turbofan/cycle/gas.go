package cycle

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
)

// Frozen dry air: 78% N2, 21% O2, 1% Ar by mole. NASA7 coefficients
// from Cantera nasa_gas.yaml; provenance and the unmodified coefficients are in
// data/air.json. Combustion products are treated as air-equivalent gas.
//
//go:embed data/air.json
var gasData []byte

var airCoefficients [2][7]float64

func init() {
	var data struct {
		Coefficients [2][7]float64 `json:"coefficients"`
	}
	if err := json.Unmarshal(gasData, &data); err != nil {
		panic(err)
	}
	airCoefficients = data.Coefficients
	for t := 200.0; t <= 3500; t += 50 {
		if cp := gasCP(t); !finite(cp) || cp <= R {
			panic("invalid NASA7 gas data")
		}
	}
}

func gasCP(t float64) float64 {
	a := airCoefficients[0]
	if t >= 1000 {
		a = airCoefficients[1]
	}
	return R * (a[0] + t*(a[1]+t*(a[2]+t*(a[3]+t*a[4]))))
}

func polynomialH(t float64, a [7]float64) float64 {
	return R * (a[5] + t*(a[0]+t*(a[1]/2+t*(a[2]/3+t*(a[3]/4+t*a[4]/5)))))
}

func polynomialS(t float64, a [7]float64) float64 {
	return R * (a[0]*math.Log(t) + a[6] + t*(a[1]+t*(a[2]/2+t*(a[3]/3+t*a[4]/4))))
}

// Continuity offsets remove the tiny rounding discontinuities between the
// published polynomial ranges, so an iterative matching solver can cross 1000 K.
func gasH(t float64) float64 {
	h := polynomialH(t, airCoefficients[0])
	if t >= 1000 {
		h = polynomialH(t, airCoefficients[1]) + polynomialH(1000, airCoefficients[0]) - polynomialH(1000, airCoefficients[1])
	}
	return h - polynomialH(TRef, airCoefficients[0])
}

func gasS(t float64) float64 {
	if t < 1000 {
		return polynomialS(t, airCoefficients[0])
	}
	return polynomialS(t, airCoefficients[1]) + polynomialS(1000, airCoefficients[0]) - polynomialS(1000, airCoefficients[1])
}

func gasInvert(value float64, entropy bool) (float64, error) {
	lo, hi := 200.0, 3500.0
	f, df := gasH, gasCP
	if entropy {
		f = gasS
		df = func(t float64) float64 { return gasCP(t) / t }
	}
	if !finite(value) || value < f(lo) || value > f(hi) {
		return 0, fmt.Errorf("gas state is outside the 200–3500 K property range")
	}
	t := math.Max(lo, math.Min(hi, TRef+(value-gasH(TRef))/1100))
	if entropy {
		t = 800
	}
	for range 45 {
		d := f(t) - value
		if math.Abs(d) < 1e-7 {
			return t, nil
		}
		if d > 0 {
			hi = t
		} else {
			lo = t
		}
		next := t - d/df(t)
		if next <= lo || next >= hi {
			next = (lo + hi) / 2
		}
		t = next
	}
	return t, nil
}

func gasIsentropic(t, ratio float64) (float64, error) {
	if t < 200 || t > 3500 || ratio <= 0 {
		return 0, fmt.Errorf("invalid isentropic state")
	}
	return gasInvert(gasS(t)+R*math.Log(ratio), true)
}

func gasCompress(in Station, pr, eta float64) (Station, error) {
	ti, err := gasIsentropic(in.TemperatureK, pr)
	if err != nil {
		return Station{}, err
	}
	t, err := gasInvert(gasH(in.TemperatureK)+(gasH(ti)-gasH(in.TemperatureK))/eta, false)
	return Station{t, in.PressurePa * pr, in.MassFlowKGS}, err
}

func gasExpand(in Station, pr, eta float64) (Station, error) {
	ti, err := gasIsentropic(in.TemperatureK, 1/pr)
	if err != nil {
		return Station{}, err
	}
	t, err := gasInvert(gasH(in.TemperatureK)-eta*(gasH(in.TemperatureK)-gasH(ti)), false)
	return Station{t, in.PressurePa / pr, in.MassFlowKGS}, err
}

// gasNozzle is a fixed-area ideal convergent nozzle. It returns the capacity
// implied by the total state and ambient back pressure, independently of the
// entering flow; the component matcher must close that flow residual.
func gasNozzle(in Station, ambientPa, area float64) (Nozzle, error) {
	t := in.TemperatureK
	if in.PressurePa <= ambientPa || t <= 200 || t > 3500 || area <= 0 {
		return Nozzle{}, fmt.Errorf("nozzle has no discharge pressure head or valid gas state")
	}
	sonic := func(te float64) float64 { cp := gasCP(te); return 2*(gasH(t)-gasH(te)) - cp/(cp-R)*R*te }
	lo, hi := 200.0, t
	if sonic(lo) < 0 {
		return Nozzle{}, fmt.Errorf("sonic state is below the gas-property range")
	}
	for range 45 {
		mid := (lo + hi) / 2
		if sonic(mid) > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	te := (lo + hi) / 2
	pe := in.PressurePa * math.Exp((gasS(te)-gasS(t))/R)
	choked := pe >= ambientPa
	if !choked {
		pe = ambientPa
		var err error
		te, err = gasIsentropic(t, pe/in.PressurePa)
		if err != nil {
			return Nozzle{}, err
		}
	}
	v := math.Sqrt(2 * (gasH(t) - gasH(te)))
	flow := pe / (R * te) * v * area
	return Nozzle{Exit: Station{te, pe, flow}, Mach: v / math.Sqrt(gasCP(te)/(gasCP(te)-R)*R*te), VelocityMPS: v, AreaM2: area, PressureThrustN: (pe - ambientPa) * area, Choked: choked}, nil
}
