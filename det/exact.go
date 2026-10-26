// Portions Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license, see LICENSE-GO.

package det

import "math"

// Constants re-exported so a deterministic package needs only det.
const (
	E       = math.E
	Pi      = math.Pi
	Phi     = math.Phi
	Sqrt2   = math.Sqrt2
	SqrtE   = math.SqrtE
	SqrtPi  = math.SqrtPi
	SqrtPhi = math.SqrtPhi
	Ln2     = math.Ln2
	Log2E   = math.Log2E
	Ln10    = math.Ln10
	Log10E  = math.Log10E

	MaxFloat32             = math.MaxFloat32
	SmallestNonzeroFloat32 = math.SmallestNonzeroFloat32
	MaxFloat64             = math.MaxFloat64
	SmallestNonzeroFloat64 = math.SmallestNonzeroFloat64
)

// The functions below are exact on every platform: IEEE 754 requires them to
// be correctly rounded or they only move bits. They are thin wrappers so the
// linter can treat det as the single source of allowed math calls.

// Sqrt returns the square root of x. IEEE 754 requires it to be correctly
// rounded, and every Go platform uses a hardware instruction or an exact
// software routine.
func Sqrt(x float64) float64 { return math.Sqrt(x) }

// Abs returns the absolute value of x.
func Abs(x float64) float64 { return math.Abs(x) }

// Floor returns the greatest integer value less than or equal to x.
func Floor(x float64) float64 { return math.Floor(x) }

// Ceil returns the least integer value greater than or equal to x.
func Ceil(x float64) float64 { return math.Ceil(x) }

// Trunc returns the integer value of x.
func Trunc(x float64) float64 { return math.Trunc(x) }

// Round returns the nearest integer, rounding half away from zero.
func Round(x float64) float64 { return math.Round(x) }

// RoundToEven returns the nearest integer, rounding ties to even.
func RoundToEven(x float64) float64 { return math.RoundToEven(x) }

// Copysign returns a value with the magnitude of f and the sign of sign.
func Copysign(f, sign float64) float64 { return math.Copysign(f, sign) }

// Signbit reports whether x is negative or negative zero.
func Signbit(x float64) bool { return math.Signbit(x) }

// Max returns the larger of x or y, with the special cases of [math.Max].
func Max(x, y float64) float64 { return math.Max(x, y) }

// Min returns the smaller of x or y, with the special cases of [math.Min].
func Min(x, y float64) float64 { return math.Min(x, y) }

// Mod returns the floating-point remainder of x/y. The result is exact.
func Mod(x, y float64) float64 { return math.Mod(x, y) }

// Modf returns integer and fractional floating-point numbers that sum to f.
func Modf(f float64) (float64, float64) { return math.Modf(f) }

// Frexp breaks f into a normalized fraction and a power of two.
func Frexp(f float64) (float64, int) { return math.Frexp(f) }

// Ldexp is the inverse of [Frexp]. It returns frac × 2**exp.
func Ldexp(frac float64, exp int) float64 { return math.Ldexp(frac, exp) }

// IsNaN reports whether f is a NaN.
func IsNaN(f float64) bool { return f != f }

// IsInf reports whether f is an infinity, according to sign.
func IsInf(f float64, sign int) bool { return math.IsInf(f, sign) }

// Inf returns positive infinity if sign >= 0, negative infinity otherwise.
func Inf(sign int) float64 { return math.Inf(sign) }

// NaN returns an IEEE 754 "not-a-number" value.
func NaN() float64 { return math.NaN() }

// Float64bits returns the IEEE 754 binary representation of f.
func Float64bits(f float64) uint64 { return math.Float64bits(f) }

// Float64frombits returns the float64 with the given IEEE 754 representation.
func Float64frombits(b uint64) float64 { return math.Float64frombits(b) }

// MulAdd returns x*y + z with the product rounded before the addition. It is
// the explicit non-fused form: the result is the same on every platform.
//
// To get a single rounding instead, call [math.FMA]. That result is also the
// same on every platform, but it is slower where no hardware instruction
// exists.
func MulAdd(x, y, z float64) float64 {
	return float64(x*y) + z
}

// MulSub returns x*y - z with the product rounded before the subtraction.
func MulSub(x, y, z float64) float64 {
	return float64(x*y) - z
}
