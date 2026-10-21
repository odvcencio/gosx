//gosx:deterministic

// Portions Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license, see LICENSE-GO.
//
// The original C code and the constants below are from FreeBSD's
// /usr/src/lib/msun/src/e_log.c and came with this notice:
//
// ====================================================
// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.
//
// Developed at SunPro, a Sun Microsystems, Inc. business.
// Permission to use, copy, modify, and distribute this
// software is freely granted, provided that this notice
// is preserved.
// ====================================================
//
// This file is the portable Go algorithm of package math with an explicit
// float64 conversion on every product that feeds an addition or subtraction.

package det

// Log returns the natural logarithm of x.
//
// Special cases are:
//
//	Log(+Inf) = +Inf
//	Log(0) = -Inf
//	Log(x < 0) = NaN
//	Log(NaN) = NaN
func Log(x float64) float64 {
	const (
		Ln2Hi = 6.93147180369123816490e-01 /* 3fe62e42 fee00000 */
		Ln2Lo = 1.90821492927058770002e-10 /* 3dea39ef 35793c76 */
		L1    = 6.666666666666735130e-01   /* 3FE55555 55555593 */
		L2    = 3.999999999940941908e-01   /* 3FD99999 9997FA04 */
		L3    = 2.857142874366239149e-01   /* 3FD24924 94229359 */
		L4    = 2.222219843214978396e-01   /* 3FCC71C5 1D8E78AF */
		L5    = 1.818357216161805012e-01   /* 3FC74664 96CB03DE */
		L6    = 1.531383769920937332e-01   /* 3FC39A09 D078C69F */
		L7    = 1.479819860511658591e-01   /* 3FC2F112 DF3E5244 */
	)

	switch {
	case IsNaN(x) || IsInf(x, 1):
		return x
	case x < 0:
		return NaN()
	case x == 0:
		return Inf(-1)
	}

	// Reduce.
	f1, ki := Frexp(x)
	if f1 < Sqrt2/2 {
		f1 *= 2
		ki--
	}
	f := f1 - 1
	k := float64(ki)

	// Compute.
	s := f / (2 + f)
	s2 := float64(s * s)
	s4 := float64(s2 * s2)
	a1 := float64(s4*L7) + L5
	a1 = float64(s4*a1) + L3
	a1 = float64(s4*a1) + L1
	t1 := float64(s2 * a1)
	a2 := float64(s4*L6) + L4
	a2 = float64(s4*a2) + L2
	t2 := float64(s4 * a2)
	R := t1 + t2
	hfsq := float64(float64(0.5*f) * f)
	return float64(k*Ln2Hi) - ((hfsq - (float64(s*(hfsq+R)) + float64(k*Ln2Lo))) - f)
}

// Log10 returns the decimal logarithm of x.
// The special cases are the same as for [Log].
func Log10(x float64) float64 {
	return float64(Log(x) * (1 / Ln10))
}

// Log2 returns the binary logarithm of x.
// The special cases are the same as for [Log].
func Log2(x float64) float64 {
	frac, exp := Frexp(x)
	// Make sure exact powers of two give an exact answer.
	if frac == 0.5 {
		return float64(exp - 1)
	}
	return float64(Log(frac)*(1/Ln2)) + float64(exp)
}
