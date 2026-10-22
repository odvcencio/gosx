//gosx:deterministic

// Portions Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license, see LICENSE-GO.
//
// The hyperbolic functions are based on the Cephes library by Stephen L.
// Moshier, as ported by the Go authors. This file is the portable Go
// algorithm of package math with an explicit float64 conversion on every
// product that feeds an addition or subtraction.

package det

// Sinh returns the hyperbolic sine of x.
//
// Special cases are:
//
//	Sinh(±0) = ±0
//	Sinh(±Inf) = ±Inf
//	Sinh(NaN) = NaN
func Sinh(x float64) float64 {
	// The coefficients are #2029 from Hart & Cheney. (20.36D)
	const (
		P0 = -0.6307673640497716991184787251e+6
		P1 = -0.8991272022039509355398013511e+5
		P2 = -0.2894211355989563807284660366e+4
		P3 = -0.2630563213397497062819489e+2
		Q0 = -0.6307673640497716991212077277e+6
		Q1 = 0.1521517378790019070696485176e+5
		Q2 = -0.173678953558233699533450911e+3
	)

	sign := false
	if x < 0 {
		x = -x
		sign = true
	}

	var temp float64
	switch {
	case x > 21:
		temp = Exp(x) * 0.5

	case x > 0.5:
		ex := Exp(x)
		temp = (ex - 1/ex) * 0.5

	default:
		sq := float64(x * x)
		num := float64(P3*sq) + P2
		num = float64(num*sq) + P1
		num = float64(num*sq) + P0
		num = float64(num * x)
		den := (sq + Q2)
		den = float64(den*sq) + Q1
		den = float64(den*sq) + Q0
		temp = num / den
	}

	if sign {
		temp = -temp
	}
	return temp
}

// Cosh returns the hyperbolic cosine of x.
//
// Special cases are:
//
//	Cosh(±0) = 1
//	Cosh(±Inf) = +Inf
//	Cosh(NaN) = NaN
func Cosh(x float64) float64 {
	x = Abs(x)
	if x > 21 {
		return Exp(x) * 0.5
	}
	ex := Exp(x)
	return (ex + 1/ex) * 0.5
}

var tanhP = [...]float64{
	-9.64399179425052238628e-1,
	-9.92877231001918586564e1,
	-1.61468768441708447952e3,
}
var tanhQ = [...]float64{
	1.12811678491632931402e2,
	2.23548839060100448583e3,
	4.84406305325125486048e3,
}

// Tanh returns the hyperbolic tangent of x.
//
// Special cases are:
//
//	Tanh(±0) = ±0
//	Tanh(±Inf) = ±1
//	Tanh(NaN) = NaN
func Tanh(x float64) float64 {
	const MAXLOG = 8.8029691931113054295988e+01 // log(2**127)
	z := Abs(x)
	switch {
	case z > 0.5*MAXLOG:
		if x < 0 {
			return -1
		}
		return 1
	case z >= 0.625:
		s := Exp(2 * z)
		z = 1 - 2/(s+1)
		if x < 0 {
			z = -z
		}
	default:
		if x == 0 {
			return x
		}
		s := float64(x * x)
		num := float64(tanhP[0]*s) + tanhP[1]
		num = float64(num*s) + tanhP[2]
		num = float64(float64(x*s) * num)
		den := (s + tanhQ[0])
		den = float64(den*s) + tanhQ[1]
		den = float64(den*s) + tanhQ[2]
		z = x + num/den
	}
	return z
}
