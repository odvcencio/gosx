package quantize

import "math"

// UnitCodec stores values in a normalized integer. A signed codec covers the
// range minus one to one, and an unsigned codec covers zero to one.
type UnitCodec struct {
	// Bits is the component width, 8 or 16.
	Bits int
	// Signed selects the range minus one to one.
	Signed bool
}

// Steps returns the largest magnitude the codec can store.
func (c UnitCodec) Steps() float64 {
	bits := c.Bits
	if bits != 8 {
		bits = 16
	}
	if c.Signed {
		return float64(int(1)<<(bits-1) - 1)
	}
	return float64(int(1)<<bits - 1)
}

// Encode returns the stored integer for one value.
func (c UnitCodec) Encode(value float64) int32 {
	if math.IsNaN(value) {
		value = 0
	}
	steps := c.Steps()
	low := 0.0
	if c.Signed {
		low = -1
	}
	value = math.Max(low, math.Min(1, value))
	return int32(math.Round(value * steps))
}

// Decode returns the value a stored integer stands for. It matches the
// dequantization the glTF specification defines for a normalized accessor.
func (c UnitCodec) Decode(stored int32) float64 {
	steps := c.Steps()
	if c.Signed {
		return math.Max(float64(stored)/steps, -1)
	}
	return float64(stored) / steps
}

// NormalError reports the angle a quantized unit vector turns through.
type NormalError struct {
	// MaxDegrees is the largest angle between a source normal and its decoded
	// normal.
	MaxDegrees float64
	// RMSDegrees is the root mean square angle.
	RMSDegrees float64
	// Bound is the largest angle the codec can produce for a unit vector. A
	// measured MaxDegrees above Bound means the encoder is wrong.
	Bound float64
	// ZeroLength counts source vectors with no direction. The codec cannot
	// preserve a direction that does not exist.
	ZeroLength int
}

// EncodeUnitVectors quantizes a stream of unit vectors and reports the angular
// error. components is 3 for a normal and 4 for a tangent, where the fourth
// component holds the handedness sign.
//
// The encoder renormalizes each source vector first. A source vector that is
// not unit length would otherwise clamp against the codec range and lose more
// direction than the lattice needs.
func EncodeUnitVectors(values []float64, components int, codec UnitCodec) ([]int32, NormalError) {
	out := make([]int32, 0, len(values))
	report := NormalError{}
	if components < 3 {
		return out, report
	}
	// The worst case sits at the middle of a lattice cell. Half a step on each
	// of three axes is the largest offset, and the angle it produces against a
	// unit vector is its arc sine.
	half := 0.5 / codec.Steps()
	report.Bound = math.Asin(math.Min(1, math.Sqrt(3)*half)) * 180 / math.Pi
	totalSquared := 0.0
	count := 0
	for i := 0; i+components <= len(values); i += components {
		length := 0.0
		for axis := 0; axis < 3; axis++ {
			length += values[i+axis] * values[i+axis]
		}
		length = math.Sqrt(length)
		var unit [3]float64
		if length > 0 {
			for axis := 0; axis < 3; axis++ {
				unit[axis] = values[i+axis] / length
			}
		} else {
			report.ZeroLength++
			unit = [3]float64{0, 1, 0}
		}
		var stored [3]int32
		var decoded [3]float64
		decodedLength := 0.0
		for axis := 0; axis < 3; axis++ {
			stored[axis] = codec.Encode(unit[axis])
			decoded[axis] = codec.Decode(stored[axis])
			decodedLength += decoded[axis] * decoded[axis]
		}
		decodedLength = math.Sqrt(decodedLength)
		out = append(out, stored[0], stored[1], stored[2])
		for extra := 3; extra < components; extra++ {
			// The handedness sign of a tangent is exactly plus or minus one, so
			// a normalized integer stores it without loss.
			sign := 1.0
			if values[i+extra] < 0 {
				sign = -1
			}
			out = append(out, codec.Encode(sign))
		}
		if length <= 0 || decodedLength <= 0 {
			count++
			continue
		}
		cosine := 0.0
		for axis := 0; axis < 3; axis++ {
			cosine += unit[axis] * decoded[axis] / decodedLength
		}
		angle := math.Acos(math.Max(-1, math.Min(1, cosine))) * 180 / math.Pi
		report.MaxDegrees = math.Max(report.MaxDegrees, angle)
		totalSquared += angle * angle
		count++
	}
	if count > 0 {
		report.RMSDegrees = math.Sqrt(totalSquared / float64(count))
	}
	return out, report
}

// UnitRangeError reports the error a normalized integer introduces on values
// that already sit inside the codec range, such as texture coordinates inside
// the unit square.
type UnitRangeError struct {
	// Max is the largest absolute error of one component.
	Max float64
	// Bound is half a lattice step, the largest error rounding can produce.
	Bound float64
	// Clamped counts components the codec had to clamp, so those values sat
	// outside the codec range.
	Clamped int
}

// EncodeUnitRange quantizes values that sit inside the codec range and reports
// the error. It refuses nothing: the caller decides whether a clamped count
// above zero is acceptable.
func EncodeUnitRange(values []float64, codec UnitCodec, tolerance float64) ([]int32, UnitRangeError) {
	out := make([]int32, 0, len(values))
	report := UnitRangeError{Bound: 0.5 / codec.Steps()}
	low := 0.0
	if codec.Signed {
		low = -1
	}
	for _, value := range values {
		if value < low-tolerance || value > 1+tolerance {
			report.Clamped++
		}
		stored := codec.Encode(value)
		out = append(out, stored)
		delta := math.Abs(codec.Decode(stored) - value)
		report.Max = math.Max(report.Max, delta)
	}
	return out, report
}

// Range returns the smallest and largest value of a stream.
func Range(values []float64) (float64, float64) {
	low := math.Inf(1)
	high := math.Inf(-1)
	for _, value := range values {
		if math.IsNaN(value) {
			continue
		}
		low = math.Min(low, value)
		high = math.Max(high, value)
	}
	return low, high
}
