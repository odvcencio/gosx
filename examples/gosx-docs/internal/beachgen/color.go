package beachgen

import "math"

func srgbLinear(value float64) float64 {
	if value <= .04045 {
		return value / 12.92
	}
	return math.Pow((value+.055)/1.055, 2.4)
}
