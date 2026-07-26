package ibl

import "math"

// BRDFModel names the exact split-sum convention this package bakes. The
// runtime shader must use the same convention or the lookup table is wrong.
//
//	GGX (Trowbridge-Reitz) distribution with alpha = roughness*roughness.
//	Smith-Schlick geometry with k = roughness*roughness/2, the value the
//	split-sum derivation uses for image-based lighting.
//	Schlick Fresnel, factored so that F = F0*A + B.
const BRDFModel = "ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel"

// DefaultBRDFLUTSize is the usual split-sum lookup table edge length.
const DefaultBRDFLUTSize = 256

// DefaultBRDFSamples is the sample count per lookup table texel.
const DefaultBRDFSamples = 1024

// IntegrateBRDF returns the scale and bias the split-sum approximation
// applies to F0. The estimator importance-samples the GGX distribution.
//
// At roughness 0 the result has a closed form: the half vector equals the
// normal, the geometry term is 1, and the pair reduces to
// A = 1-(1-NdotV)^5 and B = (1-NdotV)^5.
func IntegrateBRDF(nDotV, roughness float64, samples int) (float64, float64) {
	if samples < 1 {
		samples = DefaultBRDFSamples
	}
	nDotV = math.Max(1e-4, math.Min(1, nDotV))
	alpha := roughness * roughness
	k := alpha / 2

	view := Vec3{math.Sqrt(1 - nDotV*nDotV), 0, nDotV}
	normal := Vec3{0, 0, 1}
	tangentX := Vec3{1, 0, 0}
	tangentY := Vec3{0, 1, 0}

	var scale, bias float64
	for i := 0; i < samples; i++ {
		u1, u2 := hammersley(i, samples)
		half := importanceSampleGGX(u1, u2, alpha, normal, tangentX, tangentY)
		vDotH := view.dot(half)
		light := half.scale(2 * vDotH).sub(view)
		nDotL := light.Z
		if nDotL <= 0 {
			continue
		}
		nDotH := math.Max(half.Z, 0)
		vDotH = math.Max(vDotH, 0)
		geometry := smithSchlick(nDotV, k) * smithSchlick(nDotL, k)
		visibility := geometry * vDotH / (nDotH*nDotV + 1e-12)
		fresnel := math.Pow(1-vDotH, 5)
		scale += (1 - fresnel) * visibility
		bias += fresnel * visibility
	}
	return scale / float64(samples), bias / float64(samples)
}

func smithSchlick(nDotX, k float64) float64 {
	return nDotX / (nDotX*(1-k) + k)
}

// BRDFLUT is a baked split-sum lookup table. X runs over NdotV and Y runs
// over roughness. Both axes sample texel centres, so texel (x, y) holds
// NdotV = (x+0.5)/Size and roughness = (y+0.5)/Size.
type BRDFLUT struct {
	Size int
	// Data holds two float32 values per texel: the F0 scale and the bias.
	Data []float32
}

// At returns the scale and bias of one texel.
func (l *BRDFLUT) At(x, y int) (float32, float32) {
	i := (y*l.Size + x) * 2
	return l.Data[i], l.Data[i+1]
}

// GenerateBRDFLUT bakes the split-sum lookup table. The table is scene
// independent, so a build can compute it once and reuse it for every project.
func GenerateBRDFLUT(size, samples int) *BRDFLUT {
	if size < 1 {
		size = DefaultBRDFLUTSize
	}
	if samples < 1 {
		samples = DefaultBRDFSamples
	}
	lut := &BRDFLUT{Size: size, Data: make([]float32, size*size*2)}
	parallelRows(size, func(y int) {
		roughness := (float64(y) + 0.5) / float64(size)
		for x := 0; x < size; x++ {
			nDotV := (float64(x) + 0.5) / float64(size)
			scale, bias := IntegrateBRDF(nDotV, roughness, samples)
			i := (y*size + x) * 2
			lut.Data[i] = float32(scale)
			lut.Data[i+1] = float32(bias)
		}
	})
	return lut
}
