package headless

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func testIOR(v float64) *float64 { return &v }

func iorMaterial(ior *float64, metalness float64) engine.RenderMaterial {
	return engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.4, Metalness: metalness, IOR: ior}
}

func TestHeadlessFrameUnchangedWhenOmittedIOREqualsDefault(t *testing.T) {
	base := renderMaterialFrame(t, litSurfaceScene(iorMaterial(nil, 0), -1))
	explicit := renderMaterialFrame(t, litSurfaceScene(iorMaterial(testIOR(1.5), 0), -1))
	if maxDelta, changed := frameDelta(base, explicit); maxDelta != 0 || changed != 0 {
		t.Fatalf("explicit IOR 1.5 changed the frame (maxDelta %d, changed %d); the default F0 must stay exact", maxDelta, changed)
	}
}

func TestHeadlessFrameRespondsToAuthoredIOR(t *testing.T) {
	base := renderMaterialFrame(t, litSurfaceScene(iorMaterial(nil, 0), -1))
	for _, ior := range []float64{0, 1, 1.33, 2.42, 42} {
		frame := renderMaterialFrame(t, litSurfaceScene(iorMaterial(testIOR(ior), 0), -1))
		maxDelta, changed := frameDelta(base, frame)
		t.Logf("ior %v: measured maxDelta %d, changed %d", ior, maxDelta, changed)
		if maxDelta < 3 || changed == 0 {
			t.Fatalf("ior %v produced no CPU framebuffer response (maxDelta %d, changed %d)", ior, maxDelta, changed)
		}
	}
}

func TestHeadlessFullyMetallicFrameInvariantToIOR(t *testing.T) {
	base := renderMaterialFrame(t, litSurfaceScene(iorMaterial(nil, 1), -1))
	for _, ior := range []*float64{nil, testIOR(0), testIOR(1), testIOR(2.42)} {
		frame := renderMaterialFrame(t, litSurfaceScene(iorMaterial(ior, 1), -1))
		maxDelta, changed := frameDelta(base, frame)
		t.Logf("metallic ior %v: measured maxDelta %d, changed %d", ior, maxDelta, changed)
		if maxDelta != 0 || changed != 0 {
			t.Fatalf("a fully metallic frame must ignore the dielectric F0 (maxDelta %d, changed %d)", maxDelta, changed)
		}
	}
}

func TestDefaultMaterialStateKeepsExactDefaultF0(t *testing.T) {
	state := defaultMaterialState()
	if math.Float32bits(state.dielectricF0) != math.Float32bits(0.04) {
		t.Fatalf("default material F0 = %v, want exact float32 0.04", state.dielectricF0)
	}
}

func TestMaterialDielectricF0LaneRead(t *testing.T) {
	// A full buffer with an explicit zero lane is a valid authored F0 (IOR 1).
	full := make([]byte, 112)
	if f0, ok := materialDielectricF0(full, 0); !ok || f0 != 0 {
		t.Fatalf("zero lane read = (%v, %v), want (0, true)", f0, ok)
	}
	// A short buffer must keep the default rather than read a zero.
	// The lane is absent, so ok is false and the caller keeps the default.
	if f0, ok := materialDielectricF0(make([]byte, 100), 0); ok || f0 != 0.04 {
		t.Fatalf("short buffer read = (%v, %v), want absent lane (default 0.04, false)", f0, ok)
	}
	// A non-zero authored lane is read verbatim.
	authored := make([]byte, 112)
	binary.LittleEndian.PutUint32(authored[100:104], math.Float32bits(0.11))
	if f0, ok := materialDielectricF0(authored, 0); !ok || f0 != 0.11 {
		t.Fatalf("authored lane read = (%v, %v), want (0.11, true)", f0, ok)
	}
}
