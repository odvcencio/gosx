package ibl

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

const (
	webglRendererPath  = "../../client/js/bootstrap-src/16-scene-webgl.js"
	webgpuRendererPath = "../../client/js/bootstrap-src/16a-scene-webgpu.js"
)

func readRenderer(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestWebGLEnvironmentPathIsNotPrefilteredIBL corroborates the gap against the
// shipped renderer source.
//
// capability.Matrix says Matrix[FeatureIBL][BackendWebGL] is true. The WebGL2
// path behind that cell is scenePBRLoadHDRTexture: it fetches the .hdr, calls
// scenePBRTonemapHDRPixels to squeeze the high dynamic range values into 8-bit
// low dynamic range, uploads one RGBA UNSIGNED_BYTE 2D texture, and the shader
// samples that one texture twice, once along the normal and once along the
// reflection vector. There is no prefiltering, no split-sum lookup table, and no
// cube texture.
//
// So the cell overstates what the renderer does. This test proves the
// overstatement instead of arguing about it, and it fails the moment somebody
// implements the real thing, which forces the record to be updated at the same
// time.
//
// The same class of error as the gpu-picking case: the drift guard compares the
// Matrix against a capability manifest, and a manifest is one boolean anyone can
// edit. Only a test that reads the renderer can tell the truth.
func TestWebGLEnvironmentPathIsNotPrefilteredIBL(t *testing.T) {
	source := readRenderer(t, webglRendererPath)

	// Evidence the path tone maps to 8-bit low dynamic range.
	tonemapMarkers := []string{
		"function scenePBRTonemapHDRPixels(parsed)",
		"new Uint8Array(width * height * 4)",
		"Math.pow(r / (1 + r), 1 / 2.2)",
		"gl.RGBA, gl.UNSIGNED_BYTE, ldr.pixels",
	}
	for _, marker := range tonemapMarkers {
		if !strings.Contains(source, marker) {
			t.Errorf("the WebGL environment path changed: %q is gone from %s.\n"+
				"Re-check capability.Matrix[ibl][webgl] and this test's claim.", marker, webglRendererPath)
		}
	}

	// Evidence the shader samples one equirectangular 2D texture twice.
	shaderMarkers := []string{
		"uniform sampler2D u_envMap;",
		"texture(u_envMap, envEquirectUV(Nr)).rgb",
		"texture(u_envMap, envEquirectUV(Rr)).rgb",
		// The ad hoc gloss falloff that stands in for the split-sum term.
		"envSpecular * Fenv * (1.0 - roughness * 0.65)",
	}
	for _, marker := range shaderMarkers {
		if !strings.Contains(source, marker) {
			t.Errorf("the WebGL environment shader changed: %q is gone from %s.\n"+
				"If real IBL landed, flip the claim in this test and check the Matrix cell.",
				marker, webglRendererPath)
		}
	}

	// Evidence the real-IBL pieces are absent. Each name is one of the five
	// requirements ConsumerRequirements lists.
	absent := map[string]string{
		"samplerCube":            "a prefiltered specular cube sampler",
		"u_brdfLUT":              "the split-sum lookup table sampler",
		"textureCubeLod":         "the roughness-indexed cube tap",
		"textureLod(u_envMap":    "any roughness-indexed environment tap",
		"KTX 20":                 "a KTX2 container reader",
		"supercompressionScheme": "a KTX2 supercompression reader",
	}
	for marker, what := range absent {
		if strings.Contains(source, marker) {
			t.Errorf("%s appeared in %s (%q). Real IBL may have landed: update "+
				"ConsumerRequirements and re-check capability.Matrix[ibl][webgl].",
				what, webglRendererPath, marker)
		}
	}
}

// TestNeitherRendererConsumesTheGeneratedIBLProducts states the whole gap in one
// place: this package writes four products and nothing reads any of them.
func TestNeitherRendererConsumesTheGeneratedIBLProducts(t *testing.T) {
	for _, path := range []string{webglRendererPath, webgpuRendererPath} {
		source := readRenderer(t, path)
		for _, marker := range []string{
			"GoSXiblModel",       // The container key that pins the convention.
			".ibl.json",          // The sidecar the executor writes.
			".brdf-lut.ktx2",     // The split-sum table the executor writes.
			".irradiance.ktx2",   // The diffuse cube the executor writes.
			".ibl.ktx2",          // The prefiltered specular cube.
			"roughnessPerLevel",  // The sidecar field mapping roughness to a level.
			"sphericalHarmonics", // The sidecar field holding the diffuse bands.
		} {
			if strings.Contains(source, marker) {
				t.Errorf("%s now mentions %q. A consumer may exist: update "+
					"ConsumerRequirements and the IBL Matrix cells.", path, marker)
			}
		}
	}
}

// TestTheIBLTextureSlotsExistButStayUnbound records the nearest-miss state.
//
// sceneAllocateTextureUnits already reserves three texture units per IBL scene
// and names them irradiance, radiance, and brdfLUT. Only the first is ever
// bound, and what binds into it is the tone-mapped equirectangular 2D texture,
// not a diffuse irradiance cube. The radiance and brdfLUT slots are reserved and
// never filled.
//
// The plumbing is therefore further along than the shading. That is worth
// recording precisely, because it changes the size of the remaining work: the
// unit budget is already accounted for, so a real consumer does not have to
// renegotiate texture units with the cascaded shadow allocator.
func TestTheIBLTextureSlotsExistButStayUnbound(t *testing.T) {
	const sharedPath = "../../client/js/bootstrap-src/15a-scene-postfx-shared.js"
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
	if !strings.Contains(renderer, "layout.ibl.irradiance") {
		t.Error("the WebGL renderer no longer binds the irradiance slot")
	}
	for _, unused := range []string{"layout.ibl.radiance", "layout.ibl.brdfLUT"} {
		if strings.Contains(renderer, unused) {
			t.Errorf("%s is now bound in %s. Real IBL may have landed: update "+
				"ConsumerRequirements and the IBL Matrix cells.", unused, webglRendererPath)
		}
	}
}

// TestConsumerRequirementsRecordTheGapHonestly keeps the reported list in step
// with the tests above. Every requirement is absent today, so every Present flag
// must be false. Flipping one without adding renderer code makes the report lie.
func TestConsumerRequirementsRecordTheGapHonestly(t *testing.T) {
	requirements := ConsumerRequirements()
	if len(requirements) != 5 {
		t.Fatalf("ConsumerRequirements lists %d pieces, want 5", len(requirements))
	}
	ids := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.Present {
			t.Errorf("requirement %q claims to be present; no renderer implements it", requirement.ID)
		}
		if requirement.ID == "" || requirement.Title == "" || requirement.Detail == "" || requirement.Where == "" {
			t.Errorf("requirement %+v is incomplete", requirement)
		}
		if ids[requirement.ID] {
			t.Errorf("duplicate requirement id %q", requirement.ID)
		}
		ids[requirement.ID] = true
	}
	for _, id := range []string{"ktx2-reader-js", "cube-upload", "brdf-lut-upload", "shader-combine", "ir-carrier"} {
		if !ids[id] {
			t.Errorf("requirement %q is missing", id)
		}
	}
}

// TestIREnvironmentCannotCarryTheProducts corroborates the ir-carrier
// requirement against scene/ir.go.
//
// IREnvironment has one string for the environment. It cannot name the
// prefiltered cube, the irradiance cube, the lookup table, the mip level count,
// or the harmonics. A renderer that only sees EnvMap has no way to find what the
// build wrote.
func TestIREnvironmentCannotCarryTheProducts(t *testing.T) {
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
	for _, field := range []string{"SpecularURI", "IrradianceURI", "BRDFLUTURI", "SphericalHarmonics", "SpecularMipLevels"} {
		if strings.Contains(block, field) {
			t.Errorf("IREnvironment gained %s. The ir-carrier requirement is done: "+
				"update ConsumerRequirements.", field)
		}
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
	if row[capability.BackendWebGL] && len(ConsumerRequirements()) == 5 {
		t.Logf("the webgl cell is true while all five IBL consumer pieces are missing; " +
			"see TestWebGLEnvironmentPathIsNotPrefilteredIBL for the renderer evidence")
	}
}
