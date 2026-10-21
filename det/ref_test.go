package det

import (
	"math/big"
)

// High-precision reference functions, used to measure det's error where the
// standard library is itself platform-dependent (math.Exp uses assembly on
// amd64 and arm64).

const refPrec = 256

func bf(x float64) *big.Float { return new(big.Float).SetPrec(refPrec).SetFloat64(x) }

// refExp returns e**x at refPrec bits, by argument halving and a Taylor series.
func refExp(x *big.Float) *big.Float {
	// Halve until |x| < 2^-8, then square back.
	k := 0
	r := new(big.Float).SetPrec(refPrec).Set(x)
	half := bf(0.5)
	lim := bf(1.0 / 256)
	for new(big.Float).Abs(r).Cmp(lim) > 0 {
		r.Mul(r, half)
		k++
	}
	sum := bf(1)
	term := bf(1)
	for i := 1; i < 60; i++ {
		term.Mul(term, r)
		term.Quo(term, bf(float64(i)))
		sum.Add(sum, term)
	}
	for i := 0; i < k; i++ {
		sum.Mul(sum, sum)
	}
	return sum
}

// refLog returns ln(x) for x > 0 by Newton's method on refExp.
func refLog(x float64) *big.Float {
	bx := bf(x)
	y := bf(Log(x)) // any close seed works; the iteration converges to the true value
	for i := 0; i < 6; i++ {
		e := refExp(y)
		// y = y + 2*(x-e)/(x+e)
		num := new(big.Float).SetPrec(refPrec).Sub(bx, e)
		den := new(big.Float).SetPrec(refPrec).Add(bx, e)
		num.Quo(num, den)
		num.Mul(num, bf(2))
		y.Add(y, num)
	}
	return y
}

func refToFloat(v *big.Float) float64 {
	f, _ := v.Float64()
	return f
}

func refPow(x, y float64) float64 {
	return refToFloat(refExp(new(big.Float).SetPrec(refPrec).Mul(bf(y), refLog(x))))
}

func refSinh(x float64) float64 {
	e := refExp(bf(x))
	inv := new(big.Float).SetPrec(refPrec).Quo(bf(1), e)
	d := new(big.Float).SetPrec(refPrec).Sub(e, inv)
	return refToFloat(d.Quo(d, bf(2)))
}

func refCosh(x float64) float64 {
	e := refExp(bf(x))
	inv := new(big.Float).SetPrec(refPrec).Quo(bf(1), e)
	d := new(big.Float).SetPrec(refPrec).Add(e, inv)
	return refToFloat(d.Quo(d, bf(2)))
}

func refTanh(x float64) float64 {
	e := refExp(new(big.Float).SetPrec(refPrec).Mul(bf(x), bf(2)))
	n := new(big.Float).SetPrec(refPrec).Sub(e, bf(1))
	d := new(big.Float).SetPrec(refPrec).Add(e, bf(1))
	return refToFloat(n.Quo(n, d))
}

func refExpF(x float64) float64 { return refToFloat(refExp(bf(x))) }
