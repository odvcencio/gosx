package ibl

import "math"

// PrefilterOptions controls the GGX specular convolution.
type PrefilterOptions struct {
	// Samples is the number of importance-sampled directions per texel. The
	// cost of a level grows linearly with this number.
	Samples int
	// MipSelect enables the filtered-importance-sampling mip bias of Karis.
	// It trades a small amount of sharpness for a large drop in noise.
	MipSelect bool
}

// DefaultPrefilterSamples balances noise against build time. A 256 pixel cube
// with 128 samples per texel converges well below visible noise on the mip
// levels that matter.
const DefaultPrefilterSamples = 128

// Prefilter convolves a source cubemap against the GGX normal distribution,
// one roughness value per mip level. Level 0 copies the source, because at
// roughness 0 the GGX lobe is a mirror direction and the convolution is the
// identity. The returned chain runs from the source size down to 1x1, so a
// level index maps to roughness level/(levels-1).
func Prefilter(source *Cube, opts PrefilterOptions) Chain {
	if source == nil {
		return nil
	}
	samples := opts.Samples
	if samples < 1 {
		samples = DefaultPrefilterSamples
	}
	sourceChain := BuildChain(source)
	levels := len(sourceChain)
	out := make(Chain, levels)
	out[0] = source.Clone()
	if levels == 1 {
		return out
	}
	// saTexel is the solid angle of one source texel. The mip selection below
	// compares it against the solid angle a single sample represents.
	saTexel := 4 * math.Pi / (6 * float64(source.Size) * float64(source.Size))

	for level := 1; level < levels; level++ {
		size := sourceChain[level].Size
		roughness := float64(level) / float64(levels-1)
		alpha := roughness * roughness
		dst := NewCube(size)
		parallelRows(6*size, func(row int) {
			face := row / size
			y := row % size
			for x := 0; x < size; x++ {
				normal := FaceDirection(face, x, y, size)
				dst.Set(face, x, y, prefilterTexel(sourceChain, normal, alpha, samples, saTexel, opts.MipSelect))
			}
		})
		out[level] = dst
	}
	return out
}

// prefilterTexel evaluates the split-sum prefiltered radiance for one normal.
// The routine assumes the view direction equals the normal, which is the
// standard split-sum approximation.
func prefilterTexel(chain Chain, normal Vec3, alpha float64, samples int, saTexel float64, mipSelect bool) Vec3 {
	if alpha == 0 {
		return chain[0].Sample(normal)
	}
	tangentX, tangentY := basisFromNormal(normal)
	var sum Vec3
	var weight float64
	for i := 0; i < samples; i++ {
		u1, u2 := hammersley(i, samples)
		half := importanceSampleGGX(u1, u2, alpha, normal, tangentX, tangentY)
		nDotH := normal.dot(half)
		light := half.scale(2 * nDotH).sub(normal)
		nDotL := normal.dot(light)
		if nDotL <= 0 {
			continue
		}
		level := 0.0
		if mipSelect {
			// The sample density is D*NdotH/(4*VdotH). The view direction
			// equals the normal here, so VdotH equals NdotH and the density
			// reduces to D/4.
			pdf := distributionGGX(math.Max(nDotH, 0), alpha)/4 + 1e-4
			saSample := 1 / (float64(samples) * pdf)
			level = 0.5 * math.Log2(saSample/saTexel)
			if level < 0 {
				level = 0
			}
		}
		sum = sum.add(chain.SampleLevel(light, level).scale(nDotL))
		weight += nDotL
	}
	if weight == 0 {
		return chain[0].Sample(normal)
	}
	return sum.scale(1 / weight)
}

// basisFromNormal builds an orthonormal tangent frame around a normal.
func basisFromNormal(normal Vec3) (Vec3, Vec3) {
	up := Vec3{0, 0, 1}
	if math.Abs(normal.Z) > 0.999 {
		up = Vec3{1, 0, 0}
	}
	tangentX := cross(up, normal).normalize()
	tangentY := cross(normal, tangentX)
	return tangentX, tangentY
}

func cross(a, b Vec3) Vec3 {
	return Vec3{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}

// hammersley returns the i-th point of the Hammersley sequence of length n.
func hammersley(i, n int) (float64, float64) {
	return float64(i) / float64(n), radicalInverse(uint32(i))
}

// radicalInverse is the base-2 Van der Corput sequence.
func radicalInverse(bits uint32) float64 {
	bits = bits<<16 | bits>>16
	bits = (bits&0x55555555)<<1 | (bits&0xAAAAAAAA)>>1
	bits = (bits&0x33333333)<<2 | (bits&0xCCCCCCCC)>>2
	bits = (bits&0x0F0F0F0F)<<4 | (bits&0xF0F0F0F0)>>4
	bits = (bits&0x00FF00FF)<<8 | (bits&0xFF00FF00)>>8
	return float64(bits) * 2.3283064365386963e-10
}

// importanceSampleGGX returns a half vector drawn from the GGX distribution
// with the given alpha, expressed in world space.
func importanceSampleGGX(u1, u2, alpha float64, normal, tangentX, tangentY Vec3) Vec3 {
	phi := 2 * math.Pi * u1
	denominator := 1 + (alpha*alpha-1)*u2
	if denominator <= 0 {
		denominator = 1e-7
	}
	cosTheta := math.Sqrt((1 - u2) / denominator)
	if cosTheta > 1 {
		cosTheta = 1
	}
	sinTheta := math.Sqrt(math.Max(0, 1-cosTheta*cosTheta))
	local := Vec3{sinTheta * math.Cos(phi), sinTheta * math.Sin(phi), cosTheta}
	return tangentX.scale(local.X).
		add(tangentY.scale(local.Y)).
		add(normal.scale(local.Z)).
		normalize()
}

// distributionGGX is the Trowbridge-Reitz normal distribution term.
func distributionGGX(nDotH, alpha float64) float64 {
	a2 := alpha * alpha
	d := nDotH*nDotH*(a2-1) + 1
	return a2 / (math.Pi * d * d)
}

// RoughnessForLevel returns the roughness a prefiltered mip level carries.
func RoughnessForLevel(level, levels int) float64 {
	if levels <= 1 {
		return 0
	}
	return float64(level) / float64(levels-1)
}
