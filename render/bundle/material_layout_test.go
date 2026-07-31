package bundle

import (
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// Layout drift guards for Scene3D parity cluster C, PR1 (uniform layout
// groundwork). Section 6 of the cluster spec requires the native and browser
// Material uniform layouts to carry explicit, tested offsets. These tests pin
// the growth from 112 to 176 bytes (native) and 160 to 240 bytes (browser),
// and prove every new lane still reads zero for a material that authors none
// of the new fields, per the section 5.2 backward-compatibility gate.

// TestMaterialUniformSizeGrewToOneHundredSeventySixBytes pins the new native
// buffer size. A silent shrink or an unaligned grow both break the offset
// table in materialUniformBytes.
func TestMaterialUniformSizeGrewToOneHundredSeventySixBytes(t *testing.T) {
	if materialUniformSize != 176 {
		t.Fatalf("materialUniformSize = %d, want 176 (11 vec4 lanes)", materialUniformSize)
	}
}

// TestMaterialUniformBytesNewLanesStayZero proves the four physical-lobe
// extension lanes (offsets 112-175) stay zero-filled for a material that
// authors every legacy lobe field, so growing the buffer changes no existing
// frame. This is the section 5.2 backward-compatibility gate for PR1.
func TestMaterialUniformBytesNewLanesStayZero(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Color:        "#a1b2c3",
		Roughness:    0.4,
		Metalness:    0.6,
		Clearcoat:    0.35,
		Sheen:        0.2,
		Transmission: 0.12,
		Iridescence:  0.18,
		Anisotropy:   -0.25,
		Emissive:     0.5,
	})
	got := materialUniformBytes(fp)
	if len(got) != 176 {
		t.Fatalf("materialUniformBytes length = %d, want 176", len(got))
	}
	tail := got[112:176]
	for i, b := range tail {
		if b != 0 {
			t.Fatalf("byte %d (offset %d) = %#02x, want 0x00 — the new lanes must stay zero until an authoring PR fills them", i, 112+i, b)
		}
	}
}

// TestMaterialUniformBytesExistingOffsetsUnmoved proves offsets 0-111 keep
// their pre-cluster-C values after the buffer grew. This is the "existing
// offsets never move" rule the master parity spec pins for cluster C.
func TestMaterialUniformBytesExistingOffsetsUnmoved(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Color:        "#ffffff",
		Clearcoat:    0.35,
		Sheen:        0.2,
		Transmission: 0.12,
		Iridescence:  0.18,
		Anisotropy:   -0.25,
	})
	got := materialUniformBytes(fp)[80:112]
	want := float32sToBytes([]float32{
		dequantize(quantize(0.35)),
		dequantize(quantize(0.2)),
		dequantize(quantize(0.12)),
		dequantize(quantize(0.18)),
		dequantizeSignedUnit(quantizeSignedUnit(-0.25)), 0, 0, 0,
	})
	if string(got) != string(want) {
		t.Fatalf("physicalParams+physicalParams2 bytes = %v, want %v — offset 80-111 must not move", got, want)
	}
}

// TestLitWGSLMaterialStructGrewByFourVec4Lanes pins the native WGSL struct
// text: the four new lanes exist, in order, right after physicalParams2, and
// no fragment-stage read of them exists yet (PR1 ships no shader reads).
func TestLitWGSLMaterialStructGrewByFourVec4Lanes(t *testing.T) {
	structText := normalizeWGSLSyntax(litWGSL)
	order := regexp.MustCompile(
		`(?s)physicalParams2\s*:\s*vec4f,.*?` +
			`sheenParams\s*:\s*vec4f,.*?` +
			`attenuationParams\s*:\s*vec4f,.*?` +
			`iridescenceParams\s*:\s*vec4f,.*?` +
			`specularParams\s*:\s*vec4f,`,
	)
	if !order.MatchString(structText) {
		t.Fatalf("litWGSL Material struct did not carry sheenParams, attenuationParams, iridescenceParams, specularParams in order after physicalParams2")
	}
	for _, name := range []string{"sheenParams", "attenuationParams", "iridescenceParams", "specularParams"} {
		read := regexp.MustCompile(`material\.` + name)
		if read.MatchString(structText) {
			t.Fatalf("fs_main already reads material.%s; PR1 (uniform layout groundwork) must ship no shader reads of the new lanes", name)
		}
	}
}

// TestJSMaterialUniformBufferGrewToTwoHundredFortyBytes pins the browser
// scratch buffer size and the WGSL_MATERIAL_STRUCT append order, mirroring
// the native guard above. lit_drift_test.go's litSharedTerms/litDivergentTerms
// tables pin the shaded terms; this test pins the layout the packer and the
// struct agree on.
func TestJSMaterialUniformBufferGrewToTwoHundredFortyBytes(t *testing.T) {
	jsFile := readJSWebGPURenderer(t)

	if !strings.Contains(jsFile, "_materialUniformBuf = new ArrayBuffer(240)") {
		t.Fatalf("_materialUniformBuf did not grow to 240 bytes (60 floats: the original 40 plus five new vec4f lanes)")
	}

	structText := jsShaderSource(t, jsFile, "WGSL_MATERIAL_STRUCT")
	order := regexp.MustCompile(
		`modelScaleSigns\s*:\s*vec4f,\s*` +
			`ccVolumeParams\s*:\s*vec4f,\s*` +
			`sheenParams\s*:\s*vec4f,\s*` +
			`attenuationParams\s*:\s*vec4f,\s*` +
			`iridescenceParams\s*:\s*vec4f,\s*` +
			`specularFlagParams\s*:\s*vec4f,`,
	)
	if !order.MatchString(structText) {
		t.Fatalf("WGSL_MATERIAL_STRUCT did not append ccVolumeParams, sheenParams, attenuationParams, iridescenceParams, specularFlagParams in order after modelScaleSigns:\n%s", structText)
	}

	// The packer must zero-fill f[40..59] every call. The scratch buffer
	// (_materialUniformF) is reused across materials within a frame, so a
	// missing zero-fill would leak a previous material's new-lane values
	// into a material that authors none of them.
	if !regexp.MustCompile(`for \(var pi = 40; pi < 60; pi\+\+\) \{\s*f\[pi\] = 0;`).MatchString(jsFile) {
		t.Fatalf("materialUniformData does not zero-fill f[40..59] on every call")
	}

	for _, name := range []string{"ccVolumeParams", "sheenParams", "attenuationParams", "iridescenceParams", "specularFlagParams"} {
		read := regexp.MustCompile(`material\.` + name)
		fragSrc := jsShaderSource(t, jsFile, litJSFragmentName)
		if read.MatchString(fragSrc) {
			t.Fatalf("fragmentMain already reads material.%s; PR1 (uniform layout groundwork) must ship no shader reads of the new lanes", name)
		}
	}
}
