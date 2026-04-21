package semantic

import "math"

func cosineSimilarity(left, right []float32) float32 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for i := range left {
		l := float64(left[i])
		r := float64(right[i])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return float32(dot / math.Sqrt(leftNorm*rightNorm))
}

func cloneFloat32s(values []float32) []float32 {
	if values == nil {
		return nil
	}
	return append([]float32(nil), values...)
}
