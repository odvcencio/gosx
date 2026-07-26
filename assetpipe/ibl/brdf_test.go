package ibl

import (
	"math"
	"testing"
)

// integrateBRDFQuadrature is an independent reference estimator. It sweeps the
// hemisphere on a regular grid instead of importance sampling, so it shares no
// code path with IntegrateBRDF beyond the BRDF definition itself.
//
//	A = integral of D*G*(1-Fc)/(4*NdotV) over the hemisphere
//	B = integral of D*G*Fc/(4*NdotV) over the hemisphere
func integrateBRDFQuadrature(nDotV, roughness float64, thetaSteps, phiSteps int) (float64, float64) {
	alpha := roughness * roughness
	k := alpha / 2
	view := Vec3{math.Sqrt(1 - nDotV*nDotV), 0, nDotV}

	var scale, bias float64
	dTheta := (math.Pi / 2) / float64(thetaSteps)
	dPhi := (2 * math.Pi) / float64(phiSteps)
	for ti := 0; ti < thetaSteps; ti++ {
		theta := (float64(ti) + 0.5) * dTheta
		sinTheta := math.Sin(theta)
		cosTheta := math.Cos(theta)
		for pi := 0; pi < phiSteps; pi++ {
			phi := (float64(pi) + 0.5) * dPhi
			light := Vec3{sinTheta * math.Cos(phi), sinTheta * math.Sin(phi), cosTheta}
			half := view.add(light).normalize()
			nDotH := math.Max(half.Z, 0)
			vDotH := math.Max(view.dot(half), 0)
			nDotL := cosTheta
			d := distributionGGX(nDotH, alpha)
			g := smithSchlick(nDotV, k) * smithSchlick(nDotL, k)
			fresnel := math.Pow(1-vDotH, 5)
			common := d * g / (4 * nDotV) * sinTheta * dTheta * dPhi
			scale += (1 - fresnel) * common
			bias += fresnel * common
		}
	}
	return scale, bias
}

func TestIntegrateBRDFMatchesMirrorLimit(t *testing.T) {
	// At roughness 0 the GGX lobe collapses to the mirror direction, so the
	// split-sum pair has the closed form A = 1-(1-NdotV)^5, B = (1-NdotV)^5.
	for _, nDotV := range []float64{0.05, 0.25, 0.5, 0.75, 1.0} {
		scale, bias := IntegrateBRDF(nDotV, 0, 64)
		fresnel := math.Pow(1-nDotV, 5)
		if math.Abs(scale-(1-fresnel)) > 1e-9 || math.Abs(bias-fresnel) > 1e-9 {
			t.Fatalf("NdotV %.2f: got (%.9f, %.9f), want (%.9f, %.9f)", nDotV, scale, bias, 1-fresnel, fresnel)
		}
	}
}

func TestIntegrateBRDFMatchesQuadrature(t *testing.T) {
	cases := []struct{ nDotV, roughness float64 }{
		{0.2, 0.35},
		{0.5, 0.5},
		{0.8, 0.5},
		{0.5, 0.8},
		{0.9, 1.0},
		{0.3, 1.0},
	}
	for _, tc := range cases {
		gotScale, gotBias := IntegrateBRDF(tc.nDotV, tc.roughness, 4096)
		wantScale, wantBias := integrateBRDFQuadrature(tc.nDotV, tc.roughness, 512, 512)
		if relativeError(gotScale, wantScale) > 0.02 {
			t.Fatalf("NdotV %.2f roughness %.2f: scale %.5f, reference %.5f", tc.nDotV, tc.roughness, gotScale, wantScale)
		}
		if math.Abs(gotBias-wantBias) > 0.01 {
			t.Fatalf("NdotV %.2f roughness %.2f: bias %.5f, reference %.5f", tc.nDotV, tc.roughness, gotBias, wantBias)
		}
	}
}

func TestIntegrateBRDFConservesEnergy(t *testing.T) {
	for y := 0; y < 16; y++ {
		roughness := (float64(y) + 0.5) / 16
		for x := 0; x < 16; x++ {
			nDotV := (float64(x) + 0.5) / 16
			scale, bias := IntegrateBRDF(nDotV, roughness, 512)
			if scale < 0 || bias < 0 {
				t.Fatalf("negative term at NdotV %.3f roughness %.3f: (%.5f, %.5f)", nDotV, roughness, scale, bias)
			}
			if scale+bias > 1.0001 {
				t.Fatalf("NdotV %.3f roughness %.3f reflects %.5f, more than the incoming energy", nDotV, roughness, scale+bias)
			}
		}
	}
}

func TestGenerateBRDFLUTCorners(t *testing.T) {
	const size = 64
	lut := GenerateBRDFLUT(size, 512)
	if lut.Size != size || len(lut.Data) != size*size*2 {
		t.Fatalf("unexpected lookup table shape: %d, %d values", lut.Size, len(lut.Data))
	}

	// Bottom-left texel: nearly grazing, nearly smooth. The mirror limit puts
	// almost all of the response in the bias term. The texel centre carries a
	// small roughness, so a few samples fall below the horizon and the pair
	// lands just under the analytic limit. One percent covers that loss.
	nDotV := 0.5 / size
	wantBias := math.Pow(1-nDotV, 5)
	scale, bias := lut.At(0, 0)
	if math.Abs(float64(bias)-wantBias) > 0.01 || math.Abs(float64(scale)-(1-wantBias)) > 0.01 {
		t.Fatalf("grazing smooth texel = (%.5f, %.5f), want (%.5f, %.5f)", scale, bias, 1-wantBias, wantBias)
	}

	// Bottom-right texel: head-on and nearly smooth. Fresnel vanishes, so the
	// scale term takes the whole response.
	scale, bias = lut.At(size-1, 0)
	if math.Abs(float64(scale)-1) > 1e-3 || math.Abs(float64(bias)) > 1e-3 {
		t.Fatalf("head-on smooth texel = (%.5f, %.5f), want (1, 0)", scale, bias)
	}

	// Rough corner: check the bake against the independent quadrature.
	scale, bias = lut.At(size-1, size-1)
	wantScale, wantBiasRough := integrateBRDFQuadrature((size-0.5)/size, (size-0.5)/size, 512, 512)
	if relativeError(float64(scale), wantScale) > 0.03 {
		t.Fatalf("rough corner scale %.5f, reference %.5f", scale, wantScale)
	}
	if math.Abs(float64(bias)-wantBiasRough) > 0.01 {
		t.Fatalf("rough corner bias %.5f, reference %.5f", bias, wantBiasRough)
	}
}

func relativeError(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got)
	}
	return math.Abs(got-want) / math.Abs(want)
}
