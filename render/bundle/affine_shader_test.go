package bundle

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/render/gpu"
)

func nativeAffineShaderProblems(source string) []string {
	var problems []string
	for _, term := range []string{
		"let co0 = cross(c1, c2);",
		"let co1 = cross(c2, c0);",
		"let co2 = cross(c0, c1);",
		"abs(determinant) <= 1e-12",
		"mat3x3<f32>(co0, co1, co2) * localNormal * orientation",
		"@builtin(front_facing) frontFacing : bool",
		"frontFacing != (in.orientation > 0.0)",
	} {
		if !strings.Contains(source, term) {
			problems = append(problems, term)
		}
	}
	return problems
}

func TestNativeLitShadersUseAffineNormalsAndPerInstanceWinding(t *testing.T) {
	for name, source := range map[string]string{"rigid": litWGSL, "skinned": skinnedLitWGSL()} {
		if problems := nativeAffineShaderProblems(source); len(problems) != 0 {
			t.Errorf("%s affine shader contract missing %q", name, problems)
		}
	}
	mutated := strings.Replace(litWGSL, "let co0 = cross(c1, c2);", "let co0 = c0;", 1)
	if len(nativeAffineShaderProblems(mutated)) == 0 {
		t.Fatal("affine normal guard survived a cofactor mutation")
	}
}

func TestNativeInstancedPipelinesClassifyMixedDeterminantsInShader(t *testing.T) {
	device := newFakeDevice()
	renderer, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Destroy()
	want := map[string]bool{"bundle.lit": false, "bundle.lit.skinned": false, "bundle.shadow": false}
	for _, pipeline := range device.pipelines {
		if _, ok := want[pipeline.desc.Label]; !ok {
			continue
		}
		want[pipeline.desc.Label] = true
		if pipeline.desc.Primitive.CullMode != gpu.CullNone || pipeline.desc.Primitive.FrontFace != gpu.FrontFaceCCW {
			t.Errorf("%s fixed cull state cannot represent mixed determinant signs: %+v", pipeline.desc.Label, pipeline.desc.Primitive)
		}
	}
	for label, found := range want {
		if !found {
			t.Errorf("pipeline %s was not built", label)
		}
	}
}
