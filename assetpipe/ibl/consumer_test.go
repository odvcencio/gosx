package ibl

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

const (
	webglRendererPath  = "../../client/runtime/scene3d/webgl.ts"
	webgpuRendererPath = "../../client/runtime/scene3d/webgpu.ts"
)

func readRenderer(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestWebGLEnvironmentPathConsumesPrefilteredIBL corroborates the real,
// capable-device consumer while keeping the unconditional matrix cell false.
func TestWebGLEnvironmentPathConsumesPrefilteredIBL(t *testing.T) {
	source := readRenderer(t, webglRendererPath)

	for _, marker := range []string{
		"uniform samplerCube u_iblIrradiance;",
		"uniform samplerCube u_iblRadiance;",
		"uniform sampler2D u_iblBRDFLUT;",
		"textureLod(u_iblRadiance, Rr, roughness * u_iblRadianceMaxLod)",
		"texture(u_iblBRDFLUT, vec2(NoV, roughness)).rg",
		"prefiltered * (F0 * brdf.x + vec3(F90) * brdf.y)",
		"irradiance * albedo * kDenv",
		"scenePBRLinearHDRPixels",
		"gl.RGBA16F || 0x881A",
		"OES_texture_float_linear",
	} {
		if !strings.Contains(source, marker) {
			t.Errorf("the WebGL IBL consumer is missing %q in %s", marker, webglRendererPath)
		}
	}
	for _, marker := range []string{"maxUnits >= 20", "fragment-texture-units<20"} {
		if !strings.Contains(source, marker) {
			t.Errorf("the WebGL staged capability gate is missing %q", marker)
		}
	}
}

// TestBothRenderersConsumeTheGeneratedIBLProducts pins the shared metadata and
// the exact, backend-specific F90 split-sum convention in both runtime
// backends. The old unweighted-B form (F0 * brdf.x + brdf.y) must not appear.
func TestBothRenderersConsumeTheGeneratedIBLProducts(t *testing.T) {
	for _, tc := range []struct {
		path     string
		splitSum string
	}{
		{webglRendererPath, "prefiltered * (F0 * brdf.x + vec3(F90) * brdf.y)"},
		{webgpuRendererPath, "prefiltered * (F0 * brdf.x + vec3f(F90) * brdf.y)"},
	} {
		source := readRenderer(t, tc.path)
		for _, marker := range []string{
			"GoSXiblRole",
			"GoSXColorSpace",
			"GoSXiblModel",
			"ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel",
			tc.splitSum,
			"irradiance * albedo * kDenv",
		} {
			if !strings.Contains(source, marker) {
				t.Errorf("%s does not consume the generated IBL contract marker %q", tc.path, marker)
			}
		}
		if strings.Contains(source, "prefiltered * (F0 * brdf.x + brdf.y)") {
			t.Errorf("%s still contains the old unweighted-B split-sum form", tc.path)
		}
	}
}

// TestTheIBLTextureSlotsAreAllocatedAndBound records the WebGL resource path.
func TestTheIBLTextureSlotsAreAllocatedAndBound(t *testing.T) {
	// The three units moved out of 15a-scene-postfx-shared.ts into
	// 15a1-scene-texture-budget.ts when the base 3D chunk was split by feature.
	// 15a1 ships in the WebGL chunk now, because 16-scene-webgl.js is its only
	// caller, so a WebGPU page stops paying for a WebGL2 sampler table.
	//
	// The units are negotiated against the cascaded-shadow allocator.
	const sharedPath = "../../client/js/bootstrap-src/15a1-scene-texture-budget.ts"
	data, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("read %s: %v", sharedPath, err)
	}
	shared := string(data)
	for _, marker := range []string{"irradiance: unit,", "radiance: unit + 1,", "brdfLUT: unit + 2,"} {
		if !strings.Contains(shared, marker) {
			t.Errorf("the IBL texture unit reservation changed: %q is gone from %s", marker, sharedPath)
		}
	}
	renderer := readRenderer(t, webglRendererPath)
	for _, bound := range []string{"layout.ibl.irradiance", "layout.ibl.radiance", "layout.ibl.brdfLUT"} {
		if !strings.Contains(renderer, bound) {
			t.Errorf("%s is not bound in %s", bound, webglRendererPath)
		}
	}
}

// TestConsumerRequirementsRecordTheGapHonestly keeps the reported list in step
// with the tests above. All five consumer pieces are implemented; availability
// policy is recorded separately by the capability matrix.
func TestConsumerRequirementsRecordTheGapHonestly(t *testing.T) {
	requirements := ConsumerRequirements()
	if len(requirements) != 5 {
		t.Fatalf("ConsumerRequirements lists %d pieces, want 5", len(requirements))
	}
	ids := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.ID == "" || requirement.Title == "" || requirement.Detail == "" || requirement.Where == "" {
			t.Errorf("requirement %+v is incomplete", requirement)
		}
		if ids[requirement.ID] {
			t.Errorf("duplicate requirement id %q", requirement.ID)
		}
		ids[requirement.ID] = true
		if !requirement.Present {
			t.Errorf("implemented consumer requirement %q is still reported absent", requirement.ID)
		}
	}
	for _, id := range []string{"ktx2-reader-js", "cube-upload", "brdf-lut-upload", "shader-combine", "ir-carrier"} {
		if !ids[id] {
			t.Errorf("requirement %q is missing", id)
		}
	}
}

// TestIREnvironmentCarriesTheProducts corroborates the completed source/IR
// contract without implying that either renderer consumes it.
func TestIREnvironmentCarriesTheProducts(t *testing.T) {
	data, err := os.ReadFile("../../scene/ir.go")
	if err != nil {
		t.Fatalf("read scene/ir.go: %v", err)
	}
	source := string(data)
	start := strings.Index(source, "type IREnvironment struct {")
	if start < 0 {
		t.Fatal("IREnvironment is gone from scene/ir.go; re-check the ir-carrier requirement")
	}
	end := strings.Index(source[start:], "\n}")
	if end < 0 {
		t.Fatal("could not find the end of IREnvironment")
	}
	block := source[start : start+end]
	if !strings.Contains(block, "EnvMap") {
		t.Fatal("IREnvironment no longer carries EnvMap")
	}
	if !strings.Contains(block, "IBL") {
		t.Fatalf("IREnvironment does not carry EnvironmentIBL: %s", block)
	}
}

// TestBRDFModelPinsTheConvention ties the string to the structured record, so a
// shader author cannot read one and get the other.
func TestBRDFModelPinsTheConvention(t *testing.T) {
	convention := Convention()
	if !strings.Contains(BRDFModel, "ggx-split-sum") {
		t.Errorf("BRDFModel %q no longer names the split-sum GGX model", BRDFModel)
	}
	if !strings.Contains(BRDFModel, "k=alpha-over-2") {
		t.Errorf("BRDFModel %q no longer pins the Smith-Schlick k", BRDFModel)
	}
	if !strings.Contains(BRDFModel, "schlick-fresnel") {
		t.Errorf("BRDFModel %q no longer pins the Fresnel term", BRDFModel)
	}
	if !strings.Contains(convention.GeometryK, "alpha / 2") {
		t.Errorf("Convention().GeometryK is %q, want the alpha/2 form", convention.GeometryK)
	}
	if !strings.Contains(convention.Alpha, "roughness * roughness") {
		t.Errorf("Convention().Alpha is %q", convention.Alpha)
	}
}

// TestIBLMatrixCellsAreRecorded documents the current cells next to the evidence
// above, so a reader of this file does not have to open capability.go.
//
// The test does not assert a value; it prints the cells and fails only when the
// feature disappears from the Matrix, which would remove the record entirely.
func TestIBLMatrixCellsAreRecorded(t *testing.T) {
	row, ok := capability.Matrix[capability.FeatureIBL]
	if !ok {
		t.Fatal("capability.Matrix no longer has an ibl row")
	}
	t.Logf("capability.Matrix[ibl] = webgpu:%v webgl:%v", row[capability.BackendWebGPU], row[capability.BackendWebGL])
	if row[capability.BackendWebGL] {
		t.Log("the WebGL cell is true; verify the 18-unit staged gate has become universal before keeping it")
	}
}
