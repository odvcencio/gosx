package scene

import "m31labs.dev/gosx/oceanspec"

// oceanspecBins returns the amplitudes of every bin of one cascade.
func oceanspecBins(p oceanspec.Params, c oceanspec.Cascade, cascade int) []float64 {
	var out []float64
	for _, w := range oceanspec.Bins(p, c, cascade) {
		out = append(out, w.Amplitude)
	}
	return out
}
