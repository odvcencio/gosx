package bundle

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func specOf(v float64) *float64 { return &v }

func colorOf(r, g, b float64) *[3]float64 {
	c := [3]float64{r, g, b}
	return &c
}

func TestMaterialSpecularDefaultsAndFallback(t *testing.T) {
	def := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})

	// nil and an explicit white colour at intensity 1 fingerprint identically.
	explicit := materialFromRender(engine.RenderMaterial{Color: "#ffffff",
		SpecularIntensity: specOf(1), SpecularColor: colorOf(1, 1, 1)})
	if explicit != def {
		t.Fatal("nil specular must fingerprint identically to explicit white / intensity 1")
	}

	// Invalid renderer inputs fall back to the same defaults.
	for _, tc := range []engine.RenderMaterial{
		{Color: "#ffffff", SpecularIntensity: specOf(math.NaN())},
		{Color: "#ffffff", SpecularIntensity: specOf(math.Inf(1))},
		{Color: "#ffffff", SpecularIntensity: specOf(math.Inf(-1))},
		{Color: "#ffffff", SpecularIntensity: specOf(-0.1)},
		{Color: "#ffffff", SpecularIntensity: specOf(1.5)},
		{Color: "#ffffff", SpecularColor: colorOf(math.NaN(), 1, 1)},
		{Color: "#ffffff", SpecularColor: colorOf(1, math.Inf(-1), 1)},
		{Color: "#ffffff", SpecularColor: colorOf(1, 1, -0.5)},
	} {
		if got := materialFromRender(tc); got != def {
			t.Fatalf("invalid specular input %+v must fall back to the default fingerprint, got %+v", tc, got)
		}
	}
}

func TestMaterialSpecularZeroVersusBlackVersusDefault(t *testing.T) {
	def := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	zeroIntensity := materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularIntensity: specOf(0)})
	black := materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularColor: colorOf(0, 0, 0)})
	if zeroIntensity == def || black == def || zeroIntensity == black {
		t.Fatal("explicit zero intensity, a black specular colour and the default must be three distinct fingerprints")
	}
	// Explicit zero intensity: F0 = 0 everywhere, F90 = 0.
	if zeroIntensity.specF0R != 0 || zeroIntensity.specF0G != 0 || zeroIntensity.specF0B != 0 || zeroIntensity.specF90 != 0 {
		t.Fatalf("zero intensity lanes = %v %v %v f90=%v, want all zero",
			zeroIntensity.specF0R, zeroIntensity.specF0G, zeroIntensity.specF0B, zeroIntensity.specF90)
	}
	// Black colour with default intensity: F0 = 0 but F90 stays 1.
	if black.specF0R != 0 || black.specF90 != 1 {
		t.Fatalf("black colour lanes = f0=%v f90=%v, want f0=0 f90=1", black.specF0R, black.specF90)
	}
}

func TestMaterialSpecularHDRClampOrderAndIntensity(t *testing.T) {
	// Default IOR 1.5 -> f0 = 0.04. min(0.04*100, 1) * 0.5 = 0.5,
	// 0.04*1*0.5 = 0.02, 0.04*0*0.5 = 0. The clamp must run before intensity.
	fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff",
		SpecularIntensity: specOf(0.5), SpecularColor: colorOf(100, 1, 0)})
	got := [3]float32{fp.specF0R, fp.specF0G, fp.specF0B}
	want := [3]float32{0.5, 0.02, 0}
	if got != want {
		t.Fatalf("HDR F0 = %v, want %v", got, want)
	}
	if fp.specF90 != 0.5 {
		t.Fatalf("F90 = %v, want 0.5", fp.specF90)
	}
}

func TestMaterialSpecularIOROneWithHugeColourYieldsExactZero(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1),
		SpecularColor: colorOf(math.MaxFloat64, math.MaxFloat64, math.MaxFloat64)})
	if fp.specF0R != 0 || fp.specF0G != 0 || fp.specF0B != 0 {
		t.Fatalf("IOR 1 with MaxFloat64 colour = %v %v %v, want exact zeros", fp.specF0R, fp.specF0G, fp.specF0B)
	}
	if math.IsNaN(float64(fp.specF0R)) || math.IsNaN(float64(fp.specF0G)) || math.IsNaN(float64(fp.specF0B)) {
		t.Fatal("huge finite colour must not produce NaN")
	}
}

func TestMaterialSpecularPositiveIORHugeColourSaturatesWithoutInfOrNaN(t *testing.T) {
	// At IOR 1.2, f0 = ((1.2-1)/(1.2+1))^2 ~= 0.00826446. The float64 product
	// f0 * MaxFloat64 is finite, because f0 < 1. Only casting that product to
	// float32 before the clamp would overflow to +Inf; the actual preparation
	// computes in float64 and clamps first, which is correct.
	fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1.2),
		SpecularColor: colorOf(math.MaxFloat64, math.MaxFloat64, math.MaxFloat64)})
	for name, lane := range map[string]float32{"R": fp.specF0R, "G": fp.specF0G, "B": fp.specF0B} {
		if lane != 1 {
			t.Fatalf("IOR 1.2 with MaxFloat64 colour: F0%s = %v, want saturated 1", name, lane)
		}
	}
	for name, lane := range map[string]float32{"R": fp.specF0R, "G": fp.specF0G, "B": fp.specF0B} {
		if math.IsInf(float64(lane), 0) || math.IsNaN(float64(lane)) {
			t.Fatalf("huge finite colour must saturate without Inf/NaN, F0%s = %v", name, lane)
		}
	}
	if fp.specF90 != 1 {
		t.Fatalf("F90 with MaxFloat64 colour = %v, want 1", fp.specF90)
	}
}

func TestMaterialSpecularIORZeroWithHDRColour(t *testing.T) {
	// IOR 0 derives F0 = 1, so min(1*c, 1) * 0.5 gives [.1, .2, .5].
	fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(0),
		SpecularIntensity: specOf(0.5), SpecularColor: colorOf(0.2, 0.4, 2)})
	got := [3]float32{fp.specF0R, fp.specF0G, fp.specF0B}
	want := [3]float32{0.1, 0.2, 0.5}
	if got != want {
		t.Fatalf("IOR 0 F0 = %v, want %v", got, want)
	}
	if fp.specF90 != 0.5 {
		t.Fatalf("F90 = %v, want 0.5", fp.specF90)
	}
}

func TestMaterialSpecularFingerprintDistinctness(t *testing.T) {
	base := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	if materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularIntensity: specOf(0.5)}) == base {
		t.Fatal("a different intensity must change the fingerprint")
	}
	if materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularColor: colorOf(100, 1, 0)}) == base {
		t.Fatal("an HDR colour must change the fingerprint")
	}
	// Both 100 and 101 clamp the red F0 lane to 1 before intensity is applied,
	// so the resulting effective materials are identical and MUST share a
	// fingerprint.
	if materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularColor: colorOf(100, 1, 0)}) !=
		materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularColor: colorOf(101, 1, 0)}) {
		t.Fatal("clamped-to-the-same-effective-value materials must share a fingerprint")
	}
	// Distinct unsaturated inputs must remain distinguishable.
	if materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularColor: colorOf(0.25, 0.5, 0.75)}) ==
		materialFromRender(engine.RenderMaterial{Color: "#ffffff", SpecularColor: colorOf(0.25, 0.5, 0.8)}) {
		t.Fatal("distinct unsaturated colours must not share a fingerprint")
	}
	if materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1.2),
		SpecularColor: colorOf(math.MaxFloat64, math.MaxFloat64, math.MaxFloat64)}) ==
		materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(2.42),
			SpecularColor: colorOf(math.MaxFloat64, math.MaxFloat64, math.MaxFloat64)}) {
		t.Fatal("saturated colours at distinct IORs must not share a fingerprint")
	}
}

func TestMaterialUniformSpecularLane(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff",
		SpecularIntensity: specOf(0.5), SpecularColor: colorOf(100, 1, 0)})
	b := materialUniformBytes(fp)
	if len(b) != 128 {
		t.Fatalf("material uniform size = %d, want 128", len(b))
	}
	var got [4]float32
	for i := range got {
		got[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[112+4*i : 116+4*i]))
	}
	want := [4]float32{0.5, 0.02, 0, 0.5}
	if got != want {
		t.Fatalf("specular lane bytes 112..128 = %v, want %v", got, want)
	}

	// The legacy IOR lane at bytes 96..112 is untouched.
	diamond := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(2.42)})
	db := materialUniformBytes(diamond)
	if len(db) != 128 {
		t.Fatalf("material uniform size = %d, want 128", len(db))
	}
	if lane := math.Float32frombits(binary.LittleEndian.Uint32(db[100:104])); lane != wantF0(2.42) {
		t.Fatalf("IOR F0 lane at byte 100 = %v, want %v", lane, wantF0(2.42))
	}
	if tail := db[104:112]; string(tail) != string(float32sToBytes([]float32{0, 0})) {
		t.Fatalf("physicalParams2 tail = %v, want zeros", tail)
	}

	// The default material carries the default F0 and F90 = 1.
	defLane := materialUniformBytes(materialFromRender(engine.RenderMaterial{Color: "#ffffff"}))
	var defGot [4]float32
	for i := range defGot {
		defGot[i] = math.Float32frombits(binary.LittleEndian.Uint32(defLane[112+4*i : 116+4*i]))
	}
	defWant := [4]float32{0.04, 0.04, 0.04, 1}
	if defGot != defWant {
		t.Fatalf("default specular lane = %v, want %v", defGot, defWant)
	}
}

func TestDefaultVertexColorMaterialInitializesSpecular(t *testing.T) {
	def := defaultVertexColorMaterial()
	if def.specF0R != defaultDielectricF0 || def.specF0G != defaultDielectricF0 || def.specF0B != defaultDielectricF0 {
		t.Fatalf("default vertex-color F0 = %v %v %v, want %v",
			def.specF0R, def.specF0G, def.specF0B, defaultDielectricF0)
	}
	if def.specF90 != 1 {
		t.Fatalf("default vertex-color F90 = %v, want 1", def.specF90)
	}
}
