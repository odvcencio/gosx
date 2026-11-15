//go:build !js || !wasm

package bundle

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu/headless"
)

const customPostTestWGSL = `@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0, 0.0, 1.0, 1.0); }`

func customPostTestBundle(effects ...engine.RenderPostEffect) engine.RenderBundle {
	return engine.RenderBundle{
		Camera:     engine.RenderCamera{Z: 3, FOV: math.Pi / 3, Near: 0.1, Far: 50},
		Background: "#101820",
		Materials:  []engine.RenderMaterial{{Kind: "standard", Color: "#44aaee"}},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: -0.3, DirectionY: -1, DirectionZ: -0.4,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "cube", Kind: "cube", Size: 1, MaterialIndex: 0,
			InstanceCount: 1, Transforms: scaledTransform(1, 0, 0, 0),
		}},
		PostEffects: effects,
	}
}

// renderHeadlessFrame renders one frame on the headless device and returns a
// copy of the pixels with the diagnostics the renderer published for that frame.
func renderHeadlessFrame(t *testing.T, b engine.RenderBundle) ([]byte, []engine.RenderDiagnostic) {
	t.Helper()
	device, surface := headless.New(64, 64)
	r, err := New(Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if err := r.Frame(b, 64, 64, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	pixels := append([]byte(nil), device.Framebuffer().Pix...)
	return pixels, append([]engine.RenderDiagnostic(nil), r.Stats().MaterialFallbacks...)
}

func distinctPixels(pix []byte) int {
	seen := map[[4]byte]bool{}
	for i := 0; i+4 <= len(pix); i += 4 {
		seen[[4]byte{pix[i], pix[i+1], pix[i+2], pix[i+3]}] = true
	}
	return len(seen)
}

func diagnosticCodes(diagnostics []engine.RenderDiagnostic) []string {
	codes := make([]string, len(diagnostics))
	for i, d := range diagnostics {
		codes[i] = d.Code + "/" + d.Target
	}
	return codes
}

// TestHeadlessCustomPostIsReportedPassthrough proves that the headless device
// draws a custom post pass as an identity passthrough and that the renderer
// says so. The frame with the pass equals the frame without it, byte for byte,
// and the frame with the pass carries one diagnostic that names the pass.
func TestHeadlessCustomPostIsReportedPassthrough(t *testing.T) {
	custom := engine.RenderPostEffect{Kind: "customPost", Name: "GravLens", FragmentWGSL: customPostTestWGSL}
	without, withoutDiagnostics := renderHeadlessFrame(t, customPostTestBundle())
	with, diagnostics := renderHeadlessFrame(t, customPostTestBundle(custom))

	if len(withoutDiagnostics) != 0 {
		t.Fatalf("a frame with no post effect reported %v", diagnosticCodes(withoutDiagnostics))
	}
	if distinctPixels(without) < 2 {
		t.Fatal("the test scene drew nothing, so equal frames prove nothing")
	}
	if !bytes.Equal(with, without) {
		t.Fatal("the custom pass changed pixels on the headless device; it must be an identity passthrough")
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one passthrough", diagnosticCodes(diagnostics))
	}
	d := diagnostics[0]
	if d.Code != "scene.native.custom_post_passthrough" || d.Target != "GravLens" || d.Severity != "warning" || d.Backend != "native" {
		t.Fatalf("diagnostic = %+v", d)
	}
	if !strings.Contains(d.Message, `"GravLens"`) {
		t.Fatalf("message does not name the pass: %s", d.Message)
	}
}

func TestHeadlessCustomPostWithoutWGSLIsReportedSkipped(t *testing.T) {
	skipped := engine.RenderPostEffect{Kind: "customPost"}
	without, _ := renderHeadlessFrame(t, customPostTestBundle())
	with, diagnostics := renderHeadlessFrame(t, customPostTestBundle(skipped))
	if !bytes.Equal(with, without) {
		t.Fatal("a pass with no WGSL changed pixels")
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "scene.native.custom_post_skipped" || diagnostics[0].Target != "custom" {
		t.Fatalf("diagnostics = %v, want one skipped pass named custom", diagnosticCodes(diagnostics))
	}
}

func TestHeadlessCustomPostDiagnosticsKeepDeclarationOrder(t *testing.T) {
	b := customPostTestBundle(
		engine.RenderPostEffect{Kind: "customPost", Name: "first", FragmentWGSL: customPostTestWGSL},
		engine.RenderPostEffect{Kind: "bloom", Source: "specular", Intensity: .3},
		engine.RenderPostEffect{Kind: "custompost", Name: "second"},
	)
	want := []string{
		"scene.native.custom_post_passthrough/first",
		"scene.native.specular_bloom_unsupported/",
		"scene.native.custom_post_skipped/second",
	}
	for name, got := range map[string][]engine.RenderDiagnostic{
		"PostEffectDiagnostics": PostEffectDiagnostics(b),
		"MaterialDiagnostics":   MaterialDiagnostics(b),
	} {
		codes := diagnosticCodes(got)
		if len(codes) != len(want) {
			t.Fatalf("%s = %v, want %v", name, codes, want)
		}
		for i := range want {
			if codes[i] != want[i] {
				t.Fatalf("%s = %v, want %v", name, codes, want)
			}
		}
	}
	// Two custom passes plus specular bloom, rendered: all three reach the frame.
	_, diagnostics := renderHeadlessFrame(t, b)
	if len(diagnostics) != 3 {
		t.Fatalf("rendered diagnostics = %v, want 3", diagnosticCodes(diagnostics))
	}
}
