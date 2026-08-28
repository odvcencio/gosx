package bundle

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

// TestDefaultVertexColorMaterialKeepsExactDefaultF0 is the regression for the
// missing initializer: a material with no authored IOR must carry the exact
// float32 0.04 default in both the fingerprint and the uniform lane, while an
// authored IOR 1 (explicit F0 zero) stays distinct and keeps a zero uniform lane.
func TestDefaultVertexColorMaterialKeepsExactDefaultF0(t *testing.T) {
	def := defaultVertexColorMaterial()
	if math.Float32bits(def.dielectricF0) != math.Float32bits(0.04) {
		t.Fatalf("defaultVertexColorMaterial F0 = %v, want exact float32 0.04", def.dielectricF0)
	}
	defBytes := materialUniformBytes(def)
	defLane := math.Float32frombits(binary.LittleEndian.Uint32(defBytes[100:104]))
	if math.Float32bits(defLane) != math.Float32bits(0.04) {
		t.Fatalf("default uniform F0 lane = %v, want exact float32 0.04", defLane)
	}

	// Explicit IOR 1 (F0 exactly zero) must still fingerprint differently.
	unity := materialFromRender(engine.RenderMaterial{Color: "#ffffff", IOR: iorOf(1)})
	if unity == def {
		t.Fatal("authored F0 0 must not fingerprint like the default")
	}
	if math.Float32bits(unity.dielectricF0) != math.Float32bits(0) {
		t.Fatalf("authored IOR 1 F0 = %v, want exact zero", unity.dielectricF0)
	}
	unityBytes := materialUniformBytes(unity)
	unityLane := math.Float32frombits(binary.LittleEndian.Uint32(unityBytes[100:104]))
	if math.Float32bits(unityLane) != math.Float32bits(0) {
		t.Fatalf("authored IOR 1 uniform F0 lane = %v, want exact zero", unityLane)
	}
}

// TestResolveMaterialFingerprintFallsBackToDefault covers absent, negative,
// and out-of-range material indices: each must resolve to the default
// vertex-colour material, now including the default dielectric F0 in both the
// fingerprint and the uniform lane.
func TestResolveMaterialFingerprintFallsBackToDefault(t *testing.T) {
	materials := []engine.RenderMaterial{{Color: "#ffffff", IOR: iorOf(2.42)}}
	want := defaultVertexColorMaterial()
	wantLane := math.Float32frombits(binary.LittleEndian.Uint32(materialUniformBytes(want)[100:104]))
	for _, tc := range []struct {
		name  string
		mats  []engine.RenderMaterial
		index int
	}{
		{"missing", nil, 0},
		{"negative", materials, -1},
		{"out-of-range", materials, 1},
		{"far-out-of-range", materials, 99},
	} {
		got := resolveMaterialFingerprint(tc.mats, tc.index)
		if got != want {
			t.Fatalf("%s index %d: fingerprint %+v, want default %+v", tc.name, tc.index, got, want)
		}
		if math.Float32bits(got.dielectricF0) != math.Float32bits(0.04) {
			t.Fatalf("%s index %d: F0 = %v, want default 0.04", tc.name, tc.index, got.dielectricF0)
		}
		lane := math.Float32frombits(binary.LittleEndian.Uint32(materialUniformBytes(got)[100:104]))
		if math.Float32bits(lane) != math.Float32bits(wantLane) {
			t.Fatalf("%s index %d: uniform F0 lane = %v, want %v", tc.name, tc.index, lane, wantLane)
		}
	}
}
