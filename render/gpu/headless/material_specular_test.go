package headless

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

// This file pins the native specular rendering path end to end: the prepared
// material contract (effective dielectric F0 rgb, then F90, at byte 112..128
// of the material uniform), the F90-aware Schlick term, and the scalar diffuse
// weight (1 - maxRGB(dielectric Fresnel)) * (1 - metalness).
//
// The browser copy has no F90 lane and still weights the diffuse by the
// componentwise mixed metallic Fresnel, so nothing here claims browser
// agreement. render/bundle/lit_drift_test.go records both gaps in its
// divergence ledger.
//
// The cubemap-only specular response is pinned below against the existing
// litSphereScene fixture, rendered on the CPU rasterizer; no GPU frame and no
// new fixture API are claimed.

func specIntensity(v float64) *float64 { return &v }

func specColor(r, g, b float64) *[3]float64 {
	c := [3]float64{r, g, b}
	return &c
}

func dielectricSpecMaterial(intensity *float64, color *[3]float64) engine.RenderMaterial {
	return engine.RenderMaterial{
		Kind: "standard", Color: "#ffffff", Roughness: 0.4,
		SpecularIntensity: intensity, SpecularColor: color,
	}
}

func metalSpecMaterial(intensity *float64, color *[3]float64, ior *float64) engine.RenderMaterial {
	m := dielectricSpecMaterial(intensity, color)
	// A coloured base at high roughness keeps the highlights from saturating,
	// so the exact dielectric-input independence below stays meaningful; the
	// default white fixture could clip the lobe to white at roughness 0.4.
	m.Color = "#4080c0"
	m.Roughness = 0.7
	m.Metalness = 1
	m.IOR = ior
	return m
}

// shadeHeadOn shades one fragment head on through an addressable program
// value: shade has a pointer receiver, so newLitProgram(...).shade(...) does
// not compile.
func shadeHeadOn(m materialState, base [3]float32) [3]float32 {
	p := newLitProgram(headOnLighting(), m)
	return p.shade(headOnFragment(base))
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestHeadlessDefaultSpecularMatchesAuthoredWhiteAtFullIntensity proves the
// default vec4 [.04,.04,.04,1] is byte-identical to an authored white F0 at
// intensity 1, on a real framebuffer.
func TestHeadlessDefaultSpecularMatchesAuthoredWhiteAtFullIntensity(t *testing.T) {
	base := renderMaterialFrame(t, litSurfaceScene(dielectricSpecMaterial(nil, nil), -1))
	authored := renderMaterialFrame(t, litSurfaceScene(dielectricSpecMaterial(specIntensity(1), specColor(1, 1, 1)), -1))
	if maxDelta, changed := frameDelta(base, authored); maxDelta != 0 || changed != 0 {
		t.Fatalf("authored white F0 at intensity 1 changed the frame (maxDelta %d, changed %d); the default vec4 must stay exact", maxDelta, changed)
	}
	if c := authored.RGBAAt(materialProbeSize/2, materialProbeSize/2); c.R == 0 && c.G == 0 && c.B == 0 {
		t.Fatal("the authored frame rendered black; the specular path is not reaching the rasterizer")
	}
}

// TestHeadlessSpecularInputsChangeTheDielectricFrame proves intensity zero,
// a coloured F0, a saturating F0 and a non-default IOR all move a dielectric
// frame. Intensity alone at F90 is not probed here because litSurfaceScene is
// head on (VdotH = 1, so k = 0 and F90 cannot show); the numerical tests below
// cover F90 at grazing.
func TestHeadlessSpecularInputsChangeTheDielectricFrame(t *testing.T) {
	base := renderMaterialFrame(t, litSurfaceScene(dielectricSpecMaterial(nil, nil), -1))
	withIOR := dielectricSpecMaterial(specIntensity(1), nil)
	withIOR.IOR = testIOR(2.42)
	for _, tc := range []struct {
		name string
		m    engine.RenderMaterial
	}{
		{"intensity-zero", dielectricSpecMaterial(specIntensity(0), nil)},
		{"red-f0", dielectricSpecMaterial(specIntensity(1), specColor(1, 0, 0))},
		{"saturating-f0", dielectricSpecMaterial(specIntensity(1), specColor(64, 64, 64))},
		{"nondefault-ior", withIOR},
	} {
		frame := renderMaterialFrame(t, litSurfaceScene(tc.m, -1))
		maxDelta, changed := frameDelta(base, frame)
		t.Logf("%s: measured maxDelta %d, changed %d", tc.name, maxDelta, changed)
		if maxDelta < 3 || changed == 0 {
			t.Fatalf("%s produced no CPU framebuffer response (maxDelta %d, changed %d)", tc.name, maxDelta, changed)
		}
	}
}

// TestHeadlessFullyMetallicFrameInvariantToSpecularInputs proves a fully
// metallic surface takes its F0 from the base colour exactly: zero intensity,
// a black F0, a saturating HDR F0 and authored IORs must all render the same
// frame, with no lerp rounding leak.
func TestHeadlessFullyMetallicFrameInvariantToSpecularInputs(t *testing.T) {
	base := renderMaterialFrame(t, litSurfaceScene(metalSpecMaterial(nil, nil, nil), -1))
	for _, tc := range []struct {
		name string
		m    engine.RenderMaterial
	}{
		{"zero-intensity", metalSpecMaterial(specIntensity(0), nil, nil)},
		{"black-f0", metalSpecMaterial(specIntensity(1), specColor(0, 0, 0), nil)},
		{"hdr-f0", metalSpecMaterial(specIntensity(1), specColor(1e300, 1e300, 1e300), nil)},
		{"ior", metalSpecMaterial(nil, nil, testIOR(2.42))},
		{"hdr-and-ior", metalSpecMaterial(specIntensity(1), specColor(1e300, 1e300, 1e300), testIOR(42))},
	} {
		frame := renderMaterialFrame(t, litSurfaceScene(tc.m, -1))
		if maxDelta, changed := frameDelta(base, frame); maxDelta != 0 || changed != 0 {
			t.Fatalf("%s: a fully metallic frame must ignore the dielectric inputs (maxDelta %d, changed %d)", tc.name, maxDelta, changed)
		}
	}
}

// TestHeadlessHugeSpecularColorClampsWithoutNaN proves the HDR clamp: an F0
// colour so large that min(f0 * colour, 1) saturates must render identically
// to the smallest colour that saturates, with no NaN surviving. The float64
// product stays finite because f0 < 1; only an early float32 cast would
// overflow, and the preparation does not do one.
func TestHeadlessHugeSpecularColorClampsWithoutNaN(t *testing.T) {
	saturated := renderMaterialFrame(t, litSurfaceScene(dielectricSpecMaterial(specIntensity(1), specColor(25, 25, 25)), -1))
	huge := renderMaterialFrame(t, litSurfaceScene(dielectricSpecMaterial(specIntensity(1), specColor(math.MaxFloat64, math.MaxFloat64, math.MaxFloat64)), -1))
	if maxDelta, changed := frameDelta(saturated, huge); maxDelta != 0 || changed != 0 {
		t.Fatalf("MaxFloat64 F0 rendered differently from the saturating colour 25 (maxDelta %d, changed %d)", maxDelta, changed)
	}
	def := renderMaterialFrame(t, litSurfaceScene(dielectricSpecMaterial(nil, nil), -1))
	if maxDelta, changed := frameDelta(def, saturated); maxDelta < 3 || changed == 0 {
		t.Fatalf("a saturated F0 must change the dielectric frame (maxDelta %d, changed %d)", maxDelta, changed)
	}
}

// TestDefaultMaterialStateKeepsExactDefaultSpecular pins the default vec4:
// exact float32 0.04 replicated across RGB, F90 = 1.
func TestDefaultMaterialStateKeepsExactDefaultSpecular(t *testing.T) {
	s := defaultMaterialState()
	for i := range s.specularF0 {
		if math.Float32bits(s.specularF0[i]) != math.Float32bits(0.04) {
			t.Fatalf("default specular F0 lane %d = %v, want exact float32 0.04", i, s.specularF0[i])
		}
	}
	if s.specularF90 != 1 {
		t.Fatalf("default specular F90 = %v, want 1", s.specularF90)
	}
}

// TestMaterialSpecularParamsBindingDecoding pins the vec4 decode at byte
// 112..128: default fallback, short buffers, legacy 112-byte bindings bounded
// inside bigger buffers, non-zero offsets, and a respected all-zero vec4.
func TestMaterialSpecularParamsBindingDecoding(t *testing.T) {
	legacy := float32(0.04)
	wantLegacy := [3]float32{legacy, legacy, legacy}
	authored := [3]float32{0.1, 0.2, 0.3}

	putVec4 := func(buf []byte, off int, f0 [3]float32, f90 float32) {
		for i := 0; i < 3; i++ {
			binary.LittleEndian.PutUint32(buf[off+4*i:off+4*i+4], math.Float32bits(f0[i]))
		}
		binary.LittleEndian.PutUint32(buf[off+12:off+16], math.Float32bits(f90))
	}

	// A full binding with an all-zero vec4: intensity zero is a legitimate
	// authored value and must be returned, never re-defaulted.
	zero := make([]byte, 128)
	if f0, f90, ok := materialSpecularParams(zero, 0, 0, legacy); !ok || f0 != ([3]float32{}) || f90 != 0 {
		t.Fatalf("zero vec4 read = (%v, %v, %v), want (zeros, 0, true)", f0, f90, ok)
	}

	// A full binding with authored values is read verbatim.
	full := make([]byte, 128)
	putVec4(full, 112, authored, 0.5)
	if f0, f90, ok := materialSpecularParams(full, 0, 0, legacy); !ok || f0 != authored || f90 != 0.5 {
		t.Fatalf("authored vec4 read = (%v, %v, %v), want (%v, 0.5, true)", f0, f90, ok, authored)
	}

	// A legacy 112-byte binding inside a bigger buffer must not read the
	// unrelated tail, even when the tail holds a plausible vec4.
	big := make([]byte, 256)
	putVec4(big, 112, authored, 0.5)
	if f0, f90, ok := materialSpecularParams(big, 0, 112, legacy); ok || f0 != wantLegacy || f90 != 1 {
		t.Fatalf("bounded legacy binding read = (%v, %v, %v), want legacy replicated with ok=false", f0, f90, ok)
	}
	// The same buffer with no declared size does read the vec4: the entry's
	// explicit bound, not the buffer length, is what protects the tail.
	if f0, f90, ok := materialSpecularParams(big, 0, 0, legacy); !ok || f0 != authored || f90 != 0.5 {
		t.Fatalf("unbounded read = (%v, %v, %v), want the vec4", f0, f90, ok)
	}

	// A binding at a non-zero offset reads relative to that offset.
	shifted := make([]byte, 160)
	putVec4(shifted, 128, authored, 0.5)
	if f0, f90, ok := materialSpecularParams(shifted, 16, 0, legacy); !ok || f0 != authored || f90 != 0.5 {
		t.Fatalf("offset read = (%v, %v, %v), want the vec4 at offset+112", f0, f90, ok)
	}
	// The same offset with a declared 112-byte size stops short of the vec4.
	if _, _, ok := materialSpecularParams(shifted, 16, 112, legacy); ok {
		t.Fatal("a 112-byte binding at offset 16 must not read a vec4 that starts past its end")
	}

	// Short buffers keep the legacy values with ok=false: a 100-byte buffer
	// has not even the legacy lane, a 112-byte buffer has no vec4.
	for _, n := range []int{100, 112} {
		if f0, f90, ok := materialSpecularParams(make([]byte, n), 0, 0, legacy); ok || f0 != wantLegacy || f90 != 1 {
			t.Fatalf("short buffer of %d bytes read = (%v, %v, %v), want legacy replicated with ok=false", n, f0, f90, ok)
		}
	}
}

// headOnLighting builds one head-on lighting state: N = L = V = +Y, so
// NdotV = NdotL = NdotH = VdotH = 1 and every term has one closed form.
func headOnLighting() sceneLighting {
	l := defaultSceneLighting()
	l.ambientColor = [4]float32{}
	l.skyColor = [4]float32{}
	l.groundColor = [4]float32{}
	l.envParams = [4]float32{}
	l.lightDir = [3]float32{0, -1, 0}
	l.cameraPos = [3]float32{0, 5, 0}
	l.lights = []sceneLight{{
		kind: lightKindDirectional, color: [3]float32{1, 1, 1}, intensity: 1,
		direction: [3]float32{0, -1, 0},
	}}
	l.lightParams = [4]float32{1, -1, 0, 0}
	return l
}

func headOnFragment(base [3]float32) fragment {
	return fragment{base: base, normal: [3]float32{0, 1, 0}}
}

func specState(f0 [3]float32, f90, metalness float32) materialState {
	m := defaultMaterialState()
	m.roughness = 0.5
	m.metalness = metalness
	m.specularF0 = f0
	m.specularF90 = f90
	return m
}

// headOnTerms returns the closed-form GGX, Hammon visibility and diffuse
// factors the production helpers must reproduce at NdotV = NdotL = NdotH =
// VdotH = 1 with roughness 0.5, computed independently here.
func headOnTerms() (d, g, kD float32) {
	// D = a2 / (pi * d * d + 1e-7) with a2 = 0.25^2 and d = a2 at NdotH = 1.
	d = 0.0625 / (float32(math.Pi)*0.00390625 + 1e-7)
	// Hammon: ggxV = ggxL = 1*(1*(1-0.25)+0.25) = 1, so G = 0.5/2.
	g = 0.25
	// Scalar diffuse weight from the dielectric Fresnel at head on: F = F0
	// exactly (k = 0), so kD = (1 - 0.04) * (1 - metalness).
	kD = 0.96
	return d, g, kD
}

// TestFresnelSchlickCarriesF90Numerically pins the F90-aware Schlick term,
// including at grazing where the term is exactly F90.
func TestFresnelSchlickCarriesF90Numerically(t *testing.T) {
	f0 := [3]float32{0.04, 0.04, 0.04}
	if got := fresnelSchlick(f0, 1, 1); got != f0 {
		t.Fatalf("head-on Fresnel = %v, want exactly F0", got)
	}
	k := float32(math.Pow(0.5, 5))
	if got := fresnelSchlick(f0, 1, 0.5)[0]; abs32(got-(0.04+(1-0.04)*k)) > 1e-7 {
		t.Fatalf("F90 = 1 Fresnel at VdotH 0.5 = %v, want %v", got, 0.04+(1-0.04)*k)
	}
	if got := fresnelSchlick(f0, 0.5, 0.5)[0]; abs32(got-(0.04+(0.5-0.04)*k)) > 1e-7 {
		t.Fatalf("F90 = 0.5 Fresnel at VdotH 0.5 = %v, want %v", got, 0.04+(0.5-0.04)*k)
	}
	if got := fresnelSchlick(f0, 0.37, 0)[0]; abs32(got-0.37) > 1e-6 {
		t.Fatalf("grazing Fresnel = %v, want the F90 0.37", got)
	}
}

// TestHeadOnDielectricShadeMatchesIndependentNumbers pins the whole head-on
// dielectric response against numbers computed here, not in the production
// helper. The default F0 0.04 with F90 1 shades head on as
// 0.96/pi + D*G*0.04, about 0.3565.
func TestHeadOnDielectricShadeMatchesIndependentNumbers(t *testing.T) {
	d, g, kD := headOnTerms()
	m := specState([3]float32{0.04, 0.04, 0.04}, 1, 0)
	got := shadeHeadOn(m, [3]float32{1, 1, 1})[0]
	want := kD/float32(math.Pi) + d*g*0.04
	if abs32(got-want) > 1e-5 {
		t.Fatalf("head-on dielectric shade = %v, want %v", got, want)
	}
	if got < 0.356 || got > 0.357 {
		t.Fatalf("head-on dielectric shade = %v left the independently computed band [0.356, 0.357]", got)
	}
}

// TestScalarDiffuseWeightIsIndependentOfBaseColour proves the diffuse weight
// is a scalar built from the dielectric Fresnel alone. The shaded value is
// specular + kD/pi * base, so halving the base does not halve the shaded
// colour: the constant specular contribution stays put. Each expectation is
// computed here from the independent specular and diffuse terms.
func TestScalarDiffuseWeightIsIndependentOfBaseColour(t *testing.T) {
	m := specState([3]float32{0.04, 0.04, 0.04}, 1, 0)
	white := shadeHeadOn(m, [3]float32{1, 1, 1})
	half := shadeHeadOn(m, [3]float32{0.5, 0.5, 0.5})
	colored := shadeHeadOn(m, [3]float32{1, 0.5, 0.25})
	_, _, kD := headOnTerms()
	for i := 0; i < 3; i++ {
		specular := white[i] - kD/float32(math.Pi)
		wantHalf := specular + kD/float32(math.Pi)*0.5
		if abs32(half[i]-wantHalf) > 1e-6 {
			t.Fatalf("channel %d: half base shaded %v, want %v; the diffuse weight is not scalar", i, half[i], wantHalf)
		}
		want := specular + kD/float32(math.Pi)*[3]float32{1, 0.5, 0.25}[i]
		if abs32(colored[i]-want) > 1e-6 {
			t.Fatalf("channel %d: coloured base shaded %v, want %v; the diffuse weight gained a per-channel tint", i, colored[i], want)
		}
	}
}

// TestColouredDielectricF0LeavesDiffuseUntinted proves the scalar weight uses
// the maximum dielectric Fresnel channel: with F0 = [.2, .04, .01] the weight
// is (1 - 0.2) * (1 - 0) = 0.8 for every channel, so after subtracting the
// per-channel specular the remaining diffuse is 0.8/pi * base on each channel
// equally, not a componentwise inverse tint (which would weight R by 0.8, G
// by 0.96 and B by 0.99).
func TestColouredDielectricF0LeavesDiffuseUntinted(t *testing.T) {
	d, g, _ := headOnTerms()
	f0 := [3]float32{0.2, 0.04, 0.01}
	base := [3]float32{1, 0.5, 0.25}
	got := shadeHeadOn(specState(f0, 1, 0), base)
	for i := 0; i < 3; i++ {
		want := d*g*f0[i] + 0.8/float32(math.Pi)*base[i]
		if abs32(got[i]-want) > 1e-5 {
			t.Fatalf("channel %d: shaded %v, want the scalar-weight value %v (kD = 0.8 on every channel)", i, got[i], want)
		}
	}
}

// TestMixedMetalDiffuseUsesScalarDielectricWeight pins the intentional
// correction: at metalness 0.5 the diffuse weight is
// (1 - maxRGB(dielectric Fresnel)) * (1 - metalness) = 0.48, not the old
// componentwise (1 - F_mixed) * (1 - metalness) = 0.365. The two forms differ
// by about 0.018 in the shaded channel, and the test requires the new number.
func TestMixedMetalDiffuseUsesScalarDielectricWeight(t *testing.T) {
	d, g, _ := headOnTerms()
	m := specState([3]float32{0.04, 0.04, 0.04}, 1, 0.5)
	got := shadeHeadOn(m, [3]float32{0.5, 0.5, 0.5})[0]
	// F0_mixed = mix(0.04, 0.5, 0.5) = 0.27 head on; the scalar diffuse
	// weight comes from the dielectric Fresnel 0.04, not from 0.27.
	want := 0.48*0.5/float32(math.Pi) + d*g*0.27
	if abs32(got-want) > 1e-5 {
		t.Fatalf("mixed-metal shade = %v, want the scalar-diffuse value %v", got, want)
	}
	oldForm := (1-0.27)*0.5*0.5/float32(math.Pi) + d*g*0.27
	if math.Abs(float64(got-oldForm)) < 0.005 {
		t.Fatalf("shaded %v is indistinguishable from the old componentwise form %v; the correction is not under test", got, oldForm)
	}
}

// TestFullyMetallicShadeInvariantToDielectricInputs proves at the shading
// level that a fully metallic surface ignores the dielectric inputs exactly:
// F0 is the base colour, F90 is 1, and the dielectric Fresnel is multiplied
// by (1 - metalness) = 0, so every dielectric variant shades identically.
func TestFullyMetallicShadeInvariantToDielectricInputs(t *testing.T) {
	base := [3]float32{1, 0.5, 0.25}
	ref := shadeHeadOn(specState([3]float32{0.04, 0.04, 0.04}, 1, 1), base)
	for _, spec := range [][3]float32{{0, 0, 0}, {1, 1, 1}, {0.9, 0.1, 0.5}} {
		for _, f90 := range []float32{0, 1, 0.37} {
			got := shadeHeadOn(specState(spec, f90, 1), base)
			if got != ref {
				t.Fatalf("metallic shade with dielectric F0 %v F90 %v = %v, want exactly %v", spec, f90, got, ref)
			}
		}
	}
}
