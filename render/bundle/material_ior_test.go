package bundle

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func iorOf(v float64) *float64 { return &v }

// wantF0 computes the expected float32 F0 the same way the renderer does, so
// the test checks the contract instead of a second diverging formula.
func wantF0(ior float64) float32 {
	t := (ior - 1) / (ior + 1)
	return float32(t * t)
}

func TestMaterialFingerprintCarriesAuthoredDielectricF0(t *testing.T) {
	// No IOR and explicit 1.5 share the exact default float32 0.04.
	def := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	if math.Float32bits(def.dielectricF0) != math.Float32bits(0.04) {
		t.Fatalf("default dielectric F0 = %v, want exact float32 0.04", def.dielectricF0)
	}
	explicit := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1.5)})
	if explicit != def {
		t.Fatal("explicit IOR 1.5 must fingerprint identically to an omitted IOR")
	}

	for _, tc := range []struct {
		name string
		ior  float64
		want float32
	}{
		{"unity", 1, 0},
		{"explicit-zero", 0, 1},
		{"water", 1.33, wantF0(1.33)},
		{"glass", 1.5, wantF0(1.5)},
		{"diamond", 2.42, wantF0(2.42)},
		{"high", 42, wantF0(42)},
		{"max-float", math.MaxFloat64, wantF0(math.MaxFloat64)},
	} {
		fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(tc.ior)})
		if fp.dielectricF0 != tc.want {
			t.Fatalf("%s: dielectric F0 = %v, want %v", tc.name, fp.dielectricF0, tc.want)
		}
	}

	// Invalid values fall back to the default.
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, -0.5, 0.5} {
		fp := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(bad)})
		if math.Float32bits(fp.dielectricF0) != math.Float32bits(0.04) {
			t.Fatalf("ior %v: dielectric F0 = %v, want default 0.04", bad, fp.dielectricF0)
		}
	}

	// Fingerprint identity: IOR participates, and an authored F0 of exactly
	// zero (IOR 1) stays distinct from the default and from every other value.
	unity := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1)})
	if unity == def {
		t.Fatal("authored F0 0 must not fingerprint like the default")
	}
	if unity == materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(2.42)}) {
		t.Fatal("distinct IORs must not share a fingerprint")
	}
}

func TestMaterialUniformCarriesDielectricF0Lane(t *testing.T) {
	lane := func(fp materialFingerprint) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(materialUniformBytes(fp)[100:104]))
	}

	def := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	if got := lane(def); got != float32(0.04) {
		t.Fatalf("default F0 lane at byte 100 = %v, want 0.04", got)
	}
	unity := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1)})
	if got := lane(unity); got != 0 {
		t.Fatalf("IOR 1 F0 lane = %v, want exact 0", got)
	}
	diamond := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(2.42)})
	if got := lane(diamond); got != wantF0(2.42) {
		t.Fatalf("IOR 2.42 F0 lane = %v, want %v", got, wantF0(2.42))
	}

	// The rest of physicalParams2 keeps its layout: anisotropy, F0, 0, 0.
	got := materialUniformBytes(diamond)[96:112]
	want := float32sToBytes([]float32{0, diamond.dielectricF0, 0, 0})
	if string(got) != string(want) {
		t.Fatalf("physicalParams2 bytes = %v, want %v", got, want)
	}
}
