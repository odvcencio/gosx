package headless

import (
	"testing"

	"m31labs.dev/gosx/engine"
)

// killDirectLights blacks out the scene's directional lights and points them
// away from the camera (direction +Z, opposite the camera), so direct
// illumination is eliminated while VdotH stays well away from 1 and the
// primary-angle cube Fresnel is exercised.
func killDirectLights(s *engine.RenderBundle) {
	for i := range s.Lights {
		s.Lights[i].Color = "#000000"
		s.Lights[i].DirectionX = 0
		s.Lights[i].DirectionY = 0
		s.Lights[i].DirectionZ = 1
	}
}

func cubeOnlyScene(m engine.RenderMaterial) engine.RenderBundle {
	s := litSphereScene(m)
	s.Environment.EnvMap = "studio"
	s.Environment.EnvIntensity = 1
	killDirectLights(&s)
	return s
}

// TestHeadlessCubemapSpecularRespondsToF90 pins the environment cube term
// through the existing litSphereScene fixture. These are CPU-rendered
// pixels from the headless rasterizer, not a GPU frame. Intensity 0 gives
// F0 = 0 and F90 = 0; a black specular colour at intensity 1 gives F0 = 0
// and F90 = 1, so the two frames can only differ through the cube Fresnel.
func TestHeadlessCubemapSpecularRespondsToF90(t *testing.T) {
	f0zero := cubeOnlyScene(dielectricSpecMaterial(specIntensity(0), nil))
	f90one := cubeOnlyScene(dielectricSpecMaterial(specIntensity(1), specColor(0, 0, 0)))

	base90 := renderMaterialFrame(t, f90one)
	if c := base90.RGBAAt(materialProbeSize/2, materialProbeSize/2); c.R == 0 && c.G == 0 && c.B == 0 {
		t.Fatal("the cubemap-only scene rendered black at the probe; the environment cube is not reaching the pixels")
	}
	if maxDelta, changed := frameDelta(renderMaterialFrame(t, f0zero), base90); maxDelta < 3 || changed == 0 {
		t.Logf("positive cube result: maxDelta %d, changed %d", maxDelta, changed)
		t.Fatalf("F90 = 1 produced no cubemap response over F90 = 0 (maxDelta %d, changed %d)", maxDelta, changed)
	} else {
		t.Logf("positive cube result: maxDelta %d, changed %d", maxDelta, changed)
	}

	// Control: with the cube binding removed there is no light left, so the
	// two frames must be identical.
	c0 := f0zero
	c0.Environment.EnvMap = ""
	c1 := f90one
	c1.Environment.EnvMap = ""
	if maxDelta, changed := frameDelta(renderMaterialFrame(t, c0), renderMaterialFrame(t, c1)); maxDelta != 0 || changed != 0 {
		t.Fatalf("the response survived removing the cubemap (maxDelta %d, changed %d); direct or ambient light is leaking in", maxDelta, changed)
	}

	// The default vec4 must stay byte-identical to an authored white F0 at
	// intensity 1 under cube-only lighting.
	def := cubeOnlyScene(dielectricSpecMaterial(nil, nil))
	white := cubeOnlyScene(dielectricSpecMaterial(specIntensity(1), specColor(1, 1, 1)))
	if maxDelta, changed := frameDelta(renderMaterialFrame(t, def), renderMaterialFrame(t, white)); maxDelta != 0 || changed != 0 {
		t.Fatalf("default vec4 differs from authored white under the cube (maxDelta %d, changed %d)", maxDelta, changed)
	}

	// A fully metallic surface takes its cube F0 from the base colour, so the
	// dielectric inputs must not move the cubemap frame.
	metalA := cubeOnlyScene(metalSpecMaterial(nil, nil, nil))
	metalB := cubeOnlyScene(metalSpecMaterial(specIntensity(1), specColor(1, 0, 0), testIOR(2.42)))
	if maxDelta, changed := frameDelta(renderMaterialFrame(t, metalA), renderMaterialFrame(t, metalB)); maxDelta != 0 || changed != 0 {
		t.Fatalf("dielectric inputs moved a fully metallic cubemap frame (maxDelta %d, changed %d)", maxDelta, changed)
	}
}
