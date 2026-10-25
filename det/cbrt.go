//gosx:deterministic

// Portions Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license, see LICENSE-GO.
//
// Cbrt is based on FreeBSD's /usr/src/lib/msun/src/s_cbrt.c, as ported by the
// Go authors. This file is the portable Go algorithm of package math with an
// explicit float64 conversion on every product that feeds an addition or
// subtraction.

package det

// Cbrt returns the cube root of x.
//
// Special cases are:
//
//	Cbrt(±0) = ±0
//	Cbrt(±Inf) = ±Inf
//	Cbrt(NaN) = NaN
func Cbrt(x float64) float64 {
	const (
		B1             = 715094163                   // (682-0.03306235651)*2**20
		B2             = 696219795                   // (664-0.03306235651)*2**20
		C              = 5.42857142857142815906e-01  // 19/35     = 0x3FE15F15F15F15F1
		D              = -7.05306122448979611050e-01 // -864/1225 = 0xBFE691DE2532C834
		E              = 1.41428571428571436819e+00  // 99/70     = 0x3FF6A0EA0EA0EA0F
		F              = 1.60714285714285720630e+00  // 45/28     = 0x3FF9B6DB6DB6DB6E
		G              = 3.57142857142857150787e-01  // 5/14      = 0x3FD6DB6DB6DB6DB7
		SmallestNormal = 2.22507385850720138309e-308 // 2**-1022  = 0x0010000000000000
	)
	if x == 0 || IsNaN(x) || IsInf(x, 0) {
		return x
	}

	sign := false
	if x < 0 {
		x = -x
		sign = true
	}

	// rough cbrt to 5 bits
	t := Float64frombits(Float64bits(x)/3 + B1<<32)
	if x < SmallestNormal {
		// subnormal number
		t = float64(1 << 54) // set t= 2**54
		t = float64(t * x)
		t = Float64frombits(Float64bits(t)/3 + B2<<32)
	}

	// new cbrt to 23 bits
	r := float64(t*t) / x
	s := C + float64(r*t)
	t = float64(t * (G + F/(s+E+D/s)))

	// chop to 22 bits, make larger than cbrt(x)
	t = Float64frombits(Float64bits(t)&(0xFFFFFFFFC<<28) + 1<<30)

	// one step newton iteration to 53 bits with error less than 0.667ulps
	s = float64(t * t) // t*t is exact
	r = x / s
	w := t + t
	r = (r - t) / (w + r) // r-s is exact
	t = t + float64(t*r)

	// restore the sign bit
	if sign {
		t = -t
	}
	return t
}
