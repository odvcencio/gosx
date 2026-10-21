//gosx:deterministic

// Portions Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license, see LICENSE-GO.
//
// The sine and cosine code is based on the Cephes library by Stephen L.
// Moshier, as ported by the Go authors. This file is the portable Go
// algorithm of package math with an explicit float64 conversion on every
// product that feeds an addition or subtraction.

package det

import "math/bits"

// sin and cos coefficients: Cephes sin.c, domain |x| <= pi/4.
var _sin = [...]float64{
	1.58962301576546568060e-10, // 0x3de5d8fd1fd19ccd
	-2.50507477628578072866e-8, // 0xbe5ae5e5a9291f5d
	2.75573136213857245213e-6,  // 0x3ec71de3567d48a1
	-1.98412698295895385996e-4, // 0xbf2a01a019bfdf03
	8.33333333332211858878e-3,  // 0x3f8111111110f7d0
	-1.66666666666666307295e-1, // 0xbfc5555555555548
}

var _cos = [...]float64{
	-1.13585365213876817300e-11, // 0xbda8fa49a0861a9b
	2.08757008419747316778e-9,   // 0x3e21ee9d7b4e3f05
	-2.75573141792967388112e-7,  // 0xbe927e4f7eac4bc6
	2.48015872888517045348e-5,   // 0x3efa01a019c844f5
	-1.38888888888730564116e-3,  // 0xbf56c16c16c14f91
	4.16666666666665929218e-2,   // 0x3fa555555555554b
}

const (
	pi4A = 7.85398125648498535156e-1  // 0x3fe921fb40000000, Pi/4 split into three parts
	pi4B = 3.77489470793079817668e-8  // 0x3e64442d00000000,
	pi4C = 2.69515142907905952645e-15 // 0x3ce8469898cc5170,
)

// sinPoly evaluates the sine polynomial z + z*zz*P(zz).
func sinPoly(z, zz float64) float64 {
	p := float64(_sin[0]*zz) + _sin[1]
	p = float64(p*zz) + _sin[2]
	p = float64(p*zz) + _sin[3]
	p = float64(p*zz) + _sin[4]
	p = float64(p*zz) + _sin[5]
	return z + float64(float64(z*zz)*p)
}

// cosPoly evaluates the cosine polynomial 1 - zz/2 + zz*zz*P(zz).
func cosPoly(zz float64) float64 {
	p := float64(_cos[0]*zz) + _cos[1]
	p = float64(p*zz) + _cos[2]
	p = float64(p*zz) + _cos[3]
	p = float64(p*zz) + _cos[4]
	p = float64(p*zz) + _cos[5]
	return (1.0 - float64(0.5*zz)) + float64(float64(zz*zz)*p)
}

// reduceOctant reduces x >= 0 to an octant count j and a remainder z, where
// x = j*(pi/4) + z. j is made even.
func reduceOctant(x float64) (j uint64, z float64) {
	if x >= reduceThreshold {
		return trigReduce(x)
	}
	j = uint64(x * (4 / Pi)) // integer part of x/(Pi/4), for tests on the phase angle
	y := float64(j)          // integer part of x/(Pi/4), as float

	if j&1 == 1 {
		j++
		y++
	}
	j &= 7 // octant modulo 2Pi radians (360 degrees)
	// Extended precision modular arithmetic.
	z = ((x - float64(y*pi4A)) - float64(y*pi4B)) - float64(y*pi4C)
	return j, z
}

// Cos returns the cosine of the radian argument x.
//
// Special cases are:
//
//	Cos(±Inf) = NaN
//	Cos(NaN) = NaN
func Cos(x float64) float64 {
	switch {
	case IsNaN(x) || IsInf(x, 0):
		return NaN()
	}

	sign := false
	x = Abs(x)

	j, z := reduceOctant(x)

	if j > 3 {
		j -= 4
		sign = !sign
	}
	if j > 1 {
		sign = !sign
	}

	zz := float64(z * z)
	var y float64
	if j == 1 || j == 2 {
		y = sinPoly(z, zz)
	} else {
		y = cosPoly(zz)
	}
	if sign {
		y = -y
	}
	return y
}

// Sin returns the sine of the radian argument x.
//
// Special cases are:
//
//	Sin(±0) = ±0
//	Sin(±Inf) = NaN
//	Sin(NaN) = NaN
func Sin(x float64) float64 {
	switch {
	case x == 0 || IsNaN(x):
		return x // return ±0 || NaN()
	case IsInf(x, 0):
		return NaN()
	}

	sign := false
	if x < 0 {
		x = -x
		sign = true
	}

	j, z := reduceOctant(x)
	if j > 3 {
		sign = !sign
		j -= 4
	}
	zz := float64(z * z)
	var y float64
	if j == 1 || j == 2 {
		y = cosPoly(zz)
	} else {
		y = sinPoly(z, zz)
	}
	if sign {
		y = -y
	}
	return y
}

// Tan coefficients: Cephes tan.c.
var _tanP = [...]float64{
	-1.30936939181383777646e4, // 0xc0c992d8d24f3f38
	1.15351664838587416140e6,  // 0x413199eca5fc9ddd
	-1.79565251976484877988e7, // 0xc1711fead3299176
}
var _tanQ = [...]float64{
	1.00000000000000000000e0,
	1.36812963470692954678e4,  // 0x40cab8a5eeb36572
	-1.32089234440210967447e6, // 0xc13427bc582abc96
	2.50083801823357915839e7,  // 0x4177d98fc2ead8ef
	-5.38695755929454629881e7, // 0xc189afe03cbe5a31
}

// Tan returns the tangent of the radian argument x.
//
// Special cases are:
//
//	Tan(±0) = ±0
//	Tan(±Inf) = NaN
//	Tan(NaN) = NaN
func Tan(x float64) float64 {
	switch {
	case x == 0 || IsNaN(x):
		return x // return ±0 || NaN()
	case IsInf(x, 0):
		return NaN()
	}

	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	var j uint64
	var y, z float64
	if x >= reduceThreshold {
		j, z = trigReduce(x)
	} else {
		j = uint64(x * (4 / Pi)) // integer part of x/(Pi/4), for tests on the phase angle
		y = float64(j)           // integer part of x/(Pi/4), as float

		/* map zeros and singularities to origin */
		if j&1 == 1 {
			j++
			y++
		}

		z = ((x - float64(y*pi4A)) - float64(y*pi4B)) - float64(y*pi4C)
	}
	zz := float64(z * z)

	if zz > 1e-14 {
		num := float64(_tanP[0]*zz) + _tanP[1]
		num = float64(num*zz) + _tanP[2]
		num = float64(zz * num)
		den := (zz + _tanQ[1])
		den = float64(den*zz) + _tanQ[2]
		den = float64(den*zz) + _tanQ[3]
		den = float64(den*zz) + _tanQ[4]
		y = z + float64(z*(num/den))
	} else {
		y = z
	}
	if j&2 == 2 {
		y = -1 / y
	}
	if sign {
		y = -y
	}
	return y
}

// trigReduce implements Payne-Hanek range reduction by Pi/4 for x that is
// outside the range [0, reduceThreshold). It returns the octant j and the
// remainder z with x = j*(Pi/4) + z. It uses only integer arithmetic until the
// last step.
const reduceThreshold = 1 << 29

const (
	trigShift = 52
	trigMask  = 0x7FF
	trigBias  = 1023
)

func trigReduce(x float64) (j uint64, z float64) {
	const PI4 = Pi / 4
	if x < PI4 {
		return 0, x
	}
	// Extract out the integer and exponent such that, x = ix * 2 ** exp.
	ix := Float64bits(x)
	exp := int(ix>>trigShift&trigMask) - trigBias - trigShift
	ix &^= trigMask << trigShift
	ix |= 1 << trigShift
	// Use the exponent to extract the 3 appropriate uint64 digits from mPi4,
	// B ~ (z0, z1, z2), such that the product leading digit has the exponent
	// -61. Note, exp >= -53 since x >= PI4 and exp < 971 for maximum float64.
	digit, bitshift := uint(exp+61)/64, uint(exp+61)%64
	z0 := (mPi4[digit] << bitshift) | (mPi4[digit+1] >> (64 - bitshift))
	z1 := (mPi4[digit+1] << bitshift) | (mPi4[digit+2] >> (64 - bitshift))
	z2 := (mPi4[digit+2] << bitshift) | (mPi4[digit+3] >> (64 - bitshift))
	// Multiply mantissa by the digits and extract the upper two digits (hi, lo).
	z2hi, _ := bits.Mul64(z2, ix)
	z1hi, z1lo := bits.Mul64(z1, ix)
	z0lo := z0 * ix
	lo, c := bits.Add64(z1lo, z2hi, 0)
	hi, _ := bits.Add64(z0lo, z1hi, c)
	// The top 3 bits of hi are j.
	j = hi >> 61
	// Extract the fraction and find its magnitude.
	hi = hi<<3 | lo>>61
	lz := uint(bits.LeadingZeros64(hi))
	e := uint64(trigBias - (lz + 1))
	// Clear implicit mantissa bit and shift into place.
	hi = (hi << (lz + 1)) | (lo >> (64 - (lz + 1)))
	hi >>= 64 - trigShift
	// Include the exponent and convert to a float.
	hi |= e << trigShift
	z = Float64frombits(hi)
	// Map zeros to origin.
	if j&1 == 1 {
		j++
		j &= 7
		z--
	}
	// Multiply the fractional part by pi/4.
	return j, z * PI4
}

// mPi4 is the binary digits of 4/pi as a uint64 array, that is, 4/pi =
// Sum mPi4[i]*2^(-64*i). 19 64-bit digits and the leading one bit give 1217
// bits of precision to handle the largest possible float64 exponent.
var mPi4 = [...]uint64{
	0x0000000000000001,
	0x45f306dc9c882a53,
	0xf84eafa3ea69bb81,
	0xb6c52b3278872083,
	0xfca2c757bd778ac3,
	0x6e48dc74849ba5c0,
	0x0c925dd413a32439,
	0xfc3bd63962534e7d,
	0xd1046bea5d768909,
	0xd338e04d68befc82,
	0x7323ac7306a673e9,
	0x3908bf177bf25076,
	0x3ff12fffbc0b301f,
	0xde5e2316b414da3e,
	0xda6cfd9e4f96136e,
	0x9e8c7ecd3cbfd45a,
	0xea4f758fd7cbe2f6,
	0x7a0e73ef14a525d4,
	0xd7f6bf623f1aba10,
	0xac06608df8f6d757,
}
