package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestSpatialThicknessAndSpecularAARoundTrip(t *testing.T) {
	aa := &GeometricSpecularAA{Variance: .15, Threshold: .2}
	material := StandardMaterial{Transmission: .7, Thickness: 2, ThicknessMap: "/thickness.png", SpecularAA: aa}
	ir := NewGraph(Mesh{ID: "solid", Geometry: BoxGeometry{}, Material: material}, Model{ID: "model", Src: "/shape.glb", Material: material}).SceneIR()
	for _, object := range []ObjectIR{ir.Objects[0], ir.Models[0].ObjectIR} {
		if object.ThicknessMap != "/thickness.png" || object.SpecularAA == nil || *object.SpecularAA != *aa {
			t.Fatalf("material lost: %+v", object)
		}
		d := object.TextureDescriptors.Thickness
		if d.URI != object.ThicknessMap || d.ColorSpace != TextureColorSpaceLinear || d.Channels != "g" || d.Role != TextureRoleData {
			t.Fatalf("nonlinear thickness descriptor: %+v", d)
		}
		profile := materialFromObjectIR(object)
		if profile.ThicknessMap != object.ThicknessMap || profile.SpecularAA == object.SpecularAA || *profile.SpecularAA != *aa {
			t.Fatal("canonical material lost/aliased controls")
		}
	}
	wire, err := ir.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded SceneIR
	if err = json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Objects[0].ThicknessMap != "/thickness.png" || decoded.Objects[0].SpecularAA.Variance != .15 {
		t.Fatal("wire lost controls")
	}
	aa.Variance = 1
	if ir.Objects[0].SpecularAA.Variance != .15 {
		t.Fatal("lowering aliases author controls")
	}
	empty := NewGraph(Mesh{ID: "solid", Geometry: BoxGeometry{}, Material: StandardMaterial{}}).SceneIR()
	raw, err := empty.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "specularAA") || strings.Contains(string(raw), "thicknessMap") {
		t.Fatal("zero-value material changed wire contract")
	}
}

func TestSpecularAAFiniteBoundsAndReplacementReset(t *testing.T) {
	for _, tc := range []struct{ in, want GeometricSpecularAA }{
		{GeometricSpecularAA{math.NaN(), math.Inf(1)}, GeometricSpecularAA{}},
		{GeometricSpecularAA{-1, 2}, GeometricSpecularAA{0, 1}},
		{GeometricSpecularAA{.15, .2}, GeometricSpecularAA{.15, .2}},
	} {
		if got := copySpecularAA(&tc.in); *got != tc.want {
			t.Fatalf("got %+v want %+v", got, tc.want)
		}
	}
	raw, err := json.Marshal(SetInstancedMeshesCommand([]InstancedMeshIR{{ID: "batch"}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"thicknessMap":""`, `"specularAA":null`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("replacement cannot clear %s: %s", field, raw)
		}
	}
}

func TestSelenaSpecularAALibraryCompiles(t *testing.T) {
	source := SelenaSpecularAASource + `
material RoughnessProbe {
 surface(geo) -> color {
  let r = gsxSpecularRoughness(normalize(geo.worldNormal), 0.22, 0.15, 0.2)
  return vec3f(r,r,r)
 }
}`
	material, _, err := CompileSelenaMaterial([]byte(source), SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ body, derivative string }{{material.FragmentWGSL, "dpdx("}, {material.FragmentGLSL, "dFdx("}} {
		if !strings.Contains(pair.body, pair.derivative) || !strings.Contains(pair.body, "sqrt(sqrt(") {
			t.Fatal("authored AA lost geometric derivatives or perceptual roughness mapping")
		}
	}
	// Selena supplies a shared WGSL module to both entry points; helper functions
	// are present in the module but invoked only from fragmentMain.
}
