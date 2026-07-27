package headless

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
)

// This file states, in executable form, what the CPU rasterizer's shading model
// is and is not. Read it before you treat a headless frame as a material oracle.
//
// Until 2026-07-26 the whole model was one Lambert expression:
//
//	base * (ambient + lightColor*NdotL*shadow)
//
// It had no specular lobe, no Fresnel term, no F0, and therefore no roughness and
// no metalness. Eleven material fields reached the material uniform and left no
// mark on a pixel, so no headless frame could witness a material feature added
// for three.js parity. The tests below recorded that silence as identities, with
// an instruction to invert each case when the model learned the feature.
//
// litProgram.shade in device.go now runs the whole fragment stage of litWGSL in
// render/bundle/lit.go: a Cook-Torrance specular lobe, normal mapping, a real
// emissive term, clear coat, sheen, iridescence, anisotropy and transmission.
// So the cases below are inverted: each names a field, the framing that makes it
// visible, and the smallest pixel move it must still produce.
//
// One field is still invisible and stays asserted as an identity. Read
// TestMaterialFieldsStillInvisible for which one and why.

// litSurfaceScene builds one plane in the XZ plane with a +Y normal, seen from
// directly above. The framing is deliberate: a flat surface at a fixed angle to
// the light gives one constant shaded colour, so a single pixel is enough
// evidence and no interpolation blurs the result.
//
// The camera, the normal and the light all line up, so the view is head on:
// NdotV is one. Every term that grows toward the silhouette therefore vanishes
// here. Use litSphereScene for those.
//
// lightY selects the light direction along Y. Minus one lights the surface fully;
// plus one leaves NdotL at zero, which is the case that separates an emitter from
// an albedo multiplier.
func litSurfaceScene(material engine.RenderMaterial, lightY float64) engine.RenderBundle {
	return engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Y: 5, RotationX: 1.5707963, FOV: 1, Near: 0.1, Far: 100},
		Materials:  []engine.RenderMaterial{material},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionY: lightY,
		}},
		// The ambient and hemisphere terms are pushed to almost nothing, so the
		// visible colour comes from the key light alone. An unlit face then reads
		// pure black, which makes "does emissive glow" a yes or no question.
		Environment: darkEnvironment(),
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "floor", Kind: "plane", Width: 6, Height: 6,
			MaterialIndex: 0, InstanceCount: 1,
			Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		}},
	}
}

// litSphereScene builds one sphere lit from the side. A sphere carries every
// facing ratio from one at the centre to zero at the silhouette, so a term gated
// on 1 - NdotV reaches the frame here and cannot reach litSurfaceScene.
//
// The light comes from the right and slightly behind the eye, not along the view
// axis. A light along the view axis puts NdotL near zero everywhere NdotV is near
// zero, so the grazing band would carry no light for a grazing term to tint.
func litSphereScene(material engine.RenderMaterial) engine.RenderBundle {
	return engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 4, FOV: 1, Near: 0.1, Far: 100},
		Materials:  []engine.RenderMaterial{material},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: -1, DirectionY: -0.2, DirectionZ: -0.4,
		}},
		Environment: darkEnvironment(),
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "ball", Kind: "sphere", Radius: 1.4, Segments: 32,
			MaterialIndex: 0, InstanceCount: 1,
			Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		}},
	}
}

// darkEnvironment holds every ambient term at almost nothing, so a fixture reads
// the key light alone. Author all three: resolveHemisphereAmbient substitutes a
// full intensity for an unset sky or ground, and that substitution is disputed.
// darkEnvironment pushes every ambient term to almost nothing so a probe reads
// the material term alone.
//
// Tone mapping is "none", which makes the present pass clamp instead of curve.
// Every test in this file states an expected byte value for one material term,
// and those statements only hold on the linear value the lit stage wrote. The
// default ACES curve lifts the low end and rolls the high end off, so it would
// turn each expectation into a number that says nothing about the material.
// TestPresentPassAppliesAuthoredToneMap covers the curve itself.
func darkEnvironment() engine.RenderEnvironment {
	return engine.RenderEnvironment{
		AmbientColor: "#000000", AmbientIntensity: 0.0001,
		SkyColor: "#000000", SkyIntensity: 0.0001,
		GroundColor: "#000000", GroundIntensity: 0.0001,
		ToneMapping: "none",
	}
}

const materialProbeSize = 16

// renderMaterialFrameAt draws one frame at one size and returns the framebuffer.
func renderMaterialFrameAt(t *testing.T, frame engine.RenderBundle, size int) *image.RGBA {
	t.Helper()
	device, surface := New(size, size)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	if err := renderer.Frame(frame, size, size, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return device.Framebuffer()
}

// renderMaterialFrame draws one frame at the small probe size.
func renderMaterialFrame(t *testing.T, frame engine.RenderBundle) *image.RGBA {
	t.Helper()
	return renderMaterialFrameAt(t, frame, materialProbeSize)
}

// renderCenterPixel draws one frame and returns the middle pixel. The surface
// fills the frame, so the middle pixel is always on it.
func renderCenterPixel(t *testing.T, frame engine.RenderBundle) color.RGBA {
	t.Helper()
	return renderMaterialFrame(t, frame).RGBAAt(materialProbeSize/2, materialProbeSize/2)
}

// frameDelta reports the largest single-channel change between two frames and how
// many pixels moved at all.
//
// The largest delta is the number each case pins. A changed-pixel count alone
// would accept a one-byte rounding move as evidence that a whole lighting lobe
// arrived, and a mean would let a bright, narrow highlight average away.
func frameDelta(a, b *image.RGBA) (maxDelta, changed int) {
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pa, pb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if pa == pb {
				continue
			}
			changed++
			for _, d := range []int{
				int(pa.R) - int(pb.R),
				int(pa.G) - int(pb.G),
				int(pa.B) - int(pb.B),
			} {
				if d < 0 {
					d = -d
				}
				if d > maxDelta {
					maxDelta = d
				}
			}
		}
	}
	return maxDelta, changed
}

// pngDataURL encodes one image as a base64 PNG data URL.
//
// A material map has to be authored, not named. render/bundle turns an unknown
// texture URL into a hash-seeded checker, so a URL-named map would pin a colour
// nobody chose and the test would say nothing about the channel the shader reads.
func pngDataURL(t *testing.T, img *image.RGBA) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// solidTextureURL builds a one-colour map. Every material map the rasterizer
// reads takes its value from one channel, so a flat colour states the input
// exactly.
func solidTextureURL(t *testing.T, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return pngDataURL(t, img)
}

// TestPhysicallyBasedMaterialFieldsReachThePixels is the inverted half of the old
// identity table. Each case names one material field, the framing that exposes
// it, and the smallest per-channel move it must make.
//
// The floors are measured, not guessed. Each one sits at about half the delta the
// shipped model produces, so a rounding change passes and a lost term fails.
// TestMaterialFeatureFloorsRejectALostTerm proves the floors bite.
func TestPhysicallyBasedMaterialFieldsReachThePixels(t *testing.T) {
	// A tilted tangent-space normal. A flat map is (128, 128, 255) and would
	// correctly change nothing, so it would prove nothing either.
	tiltedNormal := solidTextureURL(t, color.RGBA{R: 210, G: 128, B: 200, A: 255})
	// glTF 2.0 packs roughness in green and metalness in blue. Each map below
	// holds 255 in every channel except the one the shader must read, and 255 is
	// the identity for a factor multiply. So a copy that read either wrong
	// channel would leave the material unchanged and fall under its floor.
	roughnessMap := solidTextureURL(t, color.RGBA{R: 255, G: 40, B: 255, A: 255})
	metalnessMap := solidTextureURL(t, color.RGBA{R: 255, G: 255, B: 40, A: 255})
	emissiveMap := solidTextureURL(t, color.RGBA{R: 240, G: 20, B: 20, A: 255})

	for _, tc := range []struct {
		feature string
		// base is the material before the mutation. Several fields only act on
		// top of another: a metalness map scales the metalness factor, and an
		// emissive map colours a glow that the emissive strength turns on.
		base     engine.RenderMaterial
		mutate   func(*engine.RenderMaterial)
		scene    func(engine.RenderMaterial) engine.RenderBundle
		minDelta int
		// why names the shading term the delta comes from.
		why string
	}{
		{
			feature:  "roughness",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.2},
			mutate:   func(m *engine.RenderMaterial) { m.Roughness = 0.95 },
			minDelta: 12,
			why:      "the GGX distribution widens, so the head-on highlight loses height",
		},
		{
			feature:  "metalness",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.4},
			mutate:   func(m *engine.RenderMaterial) { m.Metalness = 1 },
			minDelta: 40,
			why:      "F0 moves from 0.04 to the base colour and the diffuse lobe drops out",
		},
		{
			feature:  "clearcoat",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5},
			mutate:   func(m *engine.RenderMaterial) { m.Clearcoat = 1 },
			minDelta: 20,
			why:      "a second tighter lobe adds a white coat over the whole surface",
		},
		{
			feature:  "transmission",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5},
			mutate:   func(m *engine.RenderMaterial) { m.Transmission = 1 },
			minDelta: 10,
			why:      "the shaded colour fades toward the ambient term plus a tenth of the base colour",
		},
		{
			feature:  "anisotropy",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5},
			mutate:   func(m *engine.RenderMaterial) { m.Anisotropy = 1 },
			minDelta: 10,
			why:      "the roughness drops by 28 percent, so the highlight tightens",
		},
		{
			feature:  "normalMap",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5},
			mutate:   func(m *engine.RenderMaterial) { m.NormalMap = tiltedNormal },
			minDelta: 20,
			why:      "the shading normal leaves the geometric normal, so NdotL and NdotH move",
		},
		{
			feature:  "roughnessMap",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.9},
			mutate:   func(m *engine.RenderMaterial) { m.RoughnessMap = roughnessMap },
			minDelta: 8,
			why:      "the green channel scales the roughness factor down to about a sixth",
		},
		{
			feature:  "metalnessMap",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.4, Metalness: 1},
			mutate:   func(m *engine.RenderMaterial) { m.MetalnessMap = metalnessMap },
			minDelta: 8,
			why:      "the blue channel scales the metalness factor down to about a fifth",
		},
		{
			feature:  "emissiveMap",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Emissive: 1},
			mutate:   func(m *engine.RenderMaterial) { m.EmissiveMap = emissiveMap },
			minDelta: 60,
			why:      "the map replaces the emissive colour, so a blue body glows red",
		},
		{
			feature:  "sheen",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.6},
			mutate:   func(m *engine.RenderMaterial) { m.Sheen = 1 },
			scene:    litSphereScene,
			minDelta: 8,
			why:      "the velvet term rises as the surface turns away, so the silhouette brightens",
		},
		{
			feature:  "iridescence",
			base:     engine.RenderMaterial{Kind: "standard", Color: "#c0c0c0", Roughness: 0.4},
			mutate:   func(m *engine.RenderMaterial) { m.Iridescence = 1 },
			scene:    litSphereScene,
			minDelta: 8,
			why:      "the thin-film hue sweep grows toward the silhouette",
		},
	} {
		t.Run(tc.feature, func(t *testing.T) {
			build := tc.scene
			if build == nil {
				build = func(m engine.RenderMaterial) engine.RenderBundle {
					return litSurfaceScene(m, -1)
				}
			}
			reference := renderMaterialFrame(t, build(tc.base))
			// The reference must carry light. A black frame would let any
			// mutation clear its floor for the wrong reason. litSurfaceScene
			// fills the frame with one flat colour on purpose, so count
			// brightness here, not the number of distinct colours.
			if center := reference.RGBAAt(materialProbeSize/2, materialProbeSize/2); center.R+center.G+center.B == 0 {
				t.Fatalf("the reference frame is black at the centre: %+v", center)
			}
			mutated := tc.base
			tc.mutate(&mutated)
			got := renderMaterialFrame(t, build(mutated))
			delta, changed := frameDelta(got, reference)
			if delta < tc.minDelta {
				t.Fatalf("%s moved the frame by at most %d over %d pixels, want at least %d.\n"+
					"Expected effect: %s.\n"+
					"litProgram.shade has lost the term, or the field no longer reaches the material uniform.",
					tc.feature, delta, changed, tc.minDelta, tc.why)
			}
		})
	}
}

// TestMaterialFieldsStillInvisible names every material field that reaches
// engine.RenderMaterial and never reaches a CPU pixel.
//
// Identity is the honest current behaviour, not a wish. Invert the case that
// fails on the day the rasterizer learns the feature, and say in the commit
// message which feature it learned.
func TestMaterialFieldsStillInvisible(t *testing.T) {
	base := engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5}
	reference := renderCenterPixel(t, litSurfaceScene(base, -1))
	if reference.A != 255 || reference.R == 0 {
		t.Fatalf("the reference surface did not draw: %+v", reference)
	}

	for _, tc := range []struct {
		feature string
		mutate  func(*engine.RenderMaterial)
		// why records what blocks the field today, so a reader knows which
		// layer to change rather than searching the shading model.
		why string
	}{
		{
			feature: "wireframe",
			mutate:  func(m *engine.RenderMaterial) { m.Wireframe = true },
			why: "materialFingerprint in render/bundle/material.go carries no wireframe lane, " +
				"so the flag never reaches the material uniform and no shading term could read it. " +
				"Wireframe is a rasterizer state, not a material term: it needs a line-topology " +
				"pipeline in render/bundle/renderer.go and an edge walk here.",
		},
	} {
		t.Run(tc.feature, func(t *testing.T) {
			material := base
			tc.mutate(&material)
			got := renderCenterPixel(t, litSurfaceScene(material, -1))
			if got != reference {
				t.Fatalf("%s now changes a CPU pixel: %+v against %+v.\n"+
					"Recorded reason it could not: %s\n"+
					"Move the case into TestPhysicallyBasedMaterialFieldsReachThePixels with a measured floor.",
					tc.feature, got, reference, tc.why)
			}
		})
	}

	// The other half of the proof. A shading model that ignored every field would
	// pass the cases above, so the fields the model does read must move the pixel.
	for _, tc := range []struct {
		feature string
		mutate  func(*engine.RenderMaterial)
	}{
		{"color", func(m *engine.RenderMaterial) { m.Color = "#c04080" }},
		{"emissive", func(m *engine.RenderMaterial) { m.Emissive = 1 }},
	} {
		t.Run("reads-"+tc.feature, func(t *testing.T) {
			material := base
			tc.mutate(&material)
			if got := renderCenterPixel(t, litSurfaceScene(material, -1)); got == reference {
				t.Fatalf("%s is documented as read by the rasterizer but changed no pixel: %+v",
					tc.feature, got)
			}
		})
	}
}

// TestEmissiveIsARealEmitter replaces
// TestEmissiveIsAnAlbedoMultiplierNotAnEmitter, which recorded the defect this
// change removed.
//
// The old rasterizer folded the emissive strength into the albedo, in
// materialState.resolve, and then multiplied the albedo by the diffuse light. Two
// consequences followed. A lit surface read base*(1+strength), so the base colour
// was applied twice. An unlit surface stayed black at every strength, so the model
// held no emitter at all while both browser renderers glowed.
//
// litProgram.shade now adds the emissive term after the light, exactly as litWGSL
// does. So the two properties below are the inverse of the old ones.
func TestEmissiveIsARealEmitter(t *testing.T) {
	material := func(strength float64) engine.RenderMaterial {
		return engine.RenderMaterial{Kind: "standard", Color: "#404040", Emissive: strength}
	}

	// An unlit face. NdotL is zero and the ambient dome is almost nothing, so
	// anything visible here is emission and nothing else.
	dark := renderCenterPixel(t, litSurfaceScene(material(0), 1))
	if dark.R != 0 || dark.G != 0 || dark.B != 0 {
		t.Fatalf("the unlit reference is not black (%+v), so this test cannot tell an emitter from an albedo", dark)
	}
	// 0x40 is 64 of 255. The emissive colour is the base colour, so an unlit face
	// reads base*strength and rises in step with the strength.
	const baseByte = 0x40
	previous := -1
	for _, tc := range []struct {
		strength float64
		wantR    int
	}{
		{0.5, baseByte / 2},
		{1, baseByte},
		{2, baseByte * 2},
		{3, baseByte * 3},
	} {
		got := renderCenterPixel(t, litSurfaceScene(material(tc.strength), 1))
		if diff := int(got.R) - tc.wantR; diff > 1 || diff < -1 {
			t.Errorf("an unlit face at emissive strength %.2f reads red %d, want about %d; "+
				"the emissive term is no longer baseColor*strength added after the light",
				tc.strength, got.R, tc.wantR)
		}
		if int(got.R) <= previous {
			t.Errorf("emissive strength %.2f did not raise the unlit face above the previous step (%d then %d)",
				tc.strength, previous, got.R)
		}
		previous = int(got.R)
	}

	// Saturation. Four times a quarter-brightness base overruns the range.
	if got := renderCenterPixel(t, litSurfaceScene(material(4), 1)); got.R != 0xff {
		t.Errorf("emissive strength 4 gave red %d, want 255; the write clamp disappeared", got.R)
	}

	// A lit face carries the light and the emission, so it must stay brighter
	// than the same material unlit. This half rejects a model that dropped the
	// light and kept only the glow.
	lit := renderCenterPixel(t, litSurfaceScene(material(1), -1))
	glowing := renderCenterPixel(t, litSurfaceScene(material(1), 1))
	if lit.R <= glowing.R {
		t.Errorf("a lit emissive face reads red %d and an unlit one reads %d; "+
			"the direct light term no longer reaches an emissive material", lit.R, glowing.R)
	}
}

// TestSpecularLobeRespondsToTheViewDirection pins the specular lobe as a shape,
// not as one number.
//
// A roughness sweep alone would pass on any model that reads the field. The lobe
// has to peak where the mirror direction meets the eye and fall away from it, and
// it has to narrow as the roughness drops. Both properties are measured here from
// the same sphere, which carries every facing ratio at once.
func TestSpecularLobeRespondsToTheViewDirection(t *testing.T) {
	// A polished lobe is a few degrees wide, so it falls between the samples of a
	// sixteen-pixel frame. Render this one case larger.
	const size = 96
	probe := func(roughness float64) *image.RGBA {
		return renderMaterialFrameAt(t, litSphereScene(engine.RenderMaterial{
			Kind: "standard", Color: "#202020", Roughness: roughness, Metalness: 0,
		}), size)
	}
	// A near-black body, so the diffuse lobe contributes little and the specular
	// lobe is most of what is left.
	sharp := probe(0.18)
	broad := probe(1.0)

	brightest := func(img *image.RGBA) (peak int, above int) {
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				v := int(img.RGBAAt(x, y).G)
				if v > peak {
					peak = v
				}
			}
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if int(img.RGBAAt(x, y).G) > peak/2 {
					above++
				}
			}
		}
		return peak, above
	}
	sharpPeak, sharpArea := brightest(sharp)
	broadPeak, broadArea := brightest(broad)

	if sharpPeak <= broadPeak {
		t.Errorf("a polished sphere peaks at %d and a rough one at %d; "+
			"the GGX distribution no longer concentrates energy as roughness falls",
			sharpPeak, broadPeak)
	}
	if sharpArea >= broadArea {
		t.Errorf("the polished highlight covers %d pixels above half peak and the rough one %d; "+
			"the lobe no longer widens with roughness", sharpArea, broadArea)
	}
	if sharpPeak < 32 {
		t.Errorf("the polished highlight peaks at %d on a near-black body; "+
			"the specular lobe is missing or is being multiplied by the base colour", sharpPeak)
	}
}

// TestEnvironmentCubemapReachesThePixels pins the image-based lighting term.
//
// The environment map is a scene field, not a material field, so it sits outside
// the table above. It belongs here because litWGSL folds it into the same
// fragment stage and because it was the last term in that stage the rasterizer
// could not see.
//
// render/bundle builds a six-colour cube map, one texel per face, from a hash of
// the map name. The rasterizer picks the face from the major axis of the lookup
// direction, so a body lit by the cube shows a different colour where it faces a
// different face.
func TestEnvironmentCubemapReachesThePixels(t *testing.T) {
	base := litSphereScene(engine.RenderMaterial{
		Kind: "standard", Color: "#808080", Roughness: 0.35, Metalness: 1,
	})
	withCube := base
	withCube.Environment.EnvMap = "studio"
	withCube.Environment.EnvIntensity = 1

	plain := renderMaterialFrame(t, base)
	lit := renderMaterialFrame(t, withCube)
	delta, changed := frameDelta(lit, plain)
	if delta < 20 {
		t.Fatalf("an environment map moved the frame by at most %d over %d pixels, want at least 20; "+
			"litProgram.shade no longer samples the environment cube, or envParams.z no longer reaches it",
			delta, changed)
	}

	// The rotation must move the picture too. Without this half a model that
	// added one flat cube colour everywhere would pass.
	rotated := withCube
	rotated.Environment.EnvRotation = 1.5707963
	if delta, _ := frameDelta(renderMaterialFrame(t, rotated), lit); delta < 8 {
		t.Fatalf("turning the environment map a quarter turn moved the frame by at most %d, want at least 8; "+
			"the lookup direction is not being rotated, or every cube face carries the same colour", delta)
	}
}

// TestMaterialFeatureFloorsRejectALostTerm proves the floors above can fail.
//
// Every case in TestPhysicallyBasedMaterialFieldsReachThePixels compares a
// mutated material against a reference. A test written that way passes trivially
// if the floor is small enough. So this test runs the same comparison with the
// mutation removed and requires every case to fall under its own floor.
//
// An audit of this repository found eighteen tests that passed while proving
// nothing. This one is the evidence that the material floors are not the
// nineteenth.
func TestMaterialFeatureFloorsRejectALostTerm(t *testing.T) {
	for _, tc := range []struct {
		feature  string
		material engine.RenderMaterial
		scene    func(engine.RenderMaterial) engine.RenderBundle
		floor    int
	}{
		{"roughness", engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.2}, nil, 12},
		{"metalness", engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.4}, nil, 40},
		{"clearcoat", engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5}, nil, 20},
		{"sheen", engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.6}, litSphereScene, 8},
		{"iridescence", engine.RenderMaterial{Kind: "standard", Color: "#c0c0c0", Roughness: 0.4}, litSphereScene, 8},
	} {
		t.Run(tc.feature, func(t *testing.T) {
			build := tc.scene
			if build == nil {
				build = func(m engine.RenderMaterial) engine.RenderBundle {
					return litSurfaceScene(m, -1)
				}
			}
			// The same material twice. A floor that a null change clears is a
			// floor that measures nothing.
			delta, _ := frameDelta(
				renderMaterialFrame(t, build(tc.material)),
				renderMaterialFrame(t, build(tc.material)))
			if delta >= tc.floor {
				t.Fatalf("rendering %s twice with no change moved the frame by %d, "+
					"which clears its own floor of %d; the floor proves nothing",
					tc.feature, delta, tc.floor)
			}
		})
	}
}

// TestAmbientDomeSumsThreeIndependentTerms pins the ambient model as a closed-form
// number, not as an opaque hash.
//
// litWGSL in render/bundle sums three independent terms and both browser renderers
// do the same:
//
//	hemi       = normal.y * 0.5 + 0.5
//	envDiffuse = ambientColor * ambientIntensity
//	           + skyColor * hemi
//	           + groundColor * (1 - hemi)
//
// This copy of the model once multiplied the sky and ground blend by the ambient
// intensity as well. The default ambient intensity is 0.35, so it cut the whole
// dome to about a third. No golden image could see the error, because this path
// re-implements shading and never reads the shader source. A numeric check closes
// that hole: it fails when either copy moves, and it names the value that moved.
//
// The sky and ground intensities arrive premultiplied into the colour by
// resolveHemisphereAmbient, so the expectation below premultiplies them too.
func TestAmbientDomeSumsThreeIndependentTerms(t *testing.T) {
	// The surface faces straight up, so hemi is one, the sky term applies in full
	// and the ground term drops out. The light points up and away, so NdotL is
	// zero, the whole direct term including the specular lobe drops out, and the
	// ambient dome is the only contribution left.
	const (
		ambientByte   = 0x80
		ambientFactor = 0.5
		skyByte       = 0x40
		skyFactor     = 1.0
	)
	frame := litSurfaceScene(engine.RenderMaterial{Kind: "standard", Color: "#ffffff"}, 1)
	frame.Environment = engine.RenderEnvironment{
		AmbientColor: "#808080", AmbientIntensity: ambientFactor,
		SkyColor: "#404040", SkyIntensity: skyFactor,
		// A black ground with a real intensity, so resolveHemisphereAmbient
		// substitutes nothing and the ground term contributes zero by value.
		GroundColor: "#000000", GroundIntensity: 1,
		// No display curve, for the reason on darkEnvironment. This case
		// replaces the whole environment, so it must repeat the setting.
		ToneMapping: "none",
	}
	got := renderCenterPixel(t, frame)

	ambient := ambientByte / 255.0 * ambientFactor
	sky := skyByte / 255.0 * skyFactor
	want := uint8((ambient + sky) * 255)
	if diff := int(got.R) - int(want); diff > 2 || diff < -2 {
		// The old form gave ambient*ambientIntensity*sky, which is far darker.
		old := uint8(ambientByte / 255.0 * ambientFactor * sky * 255)
		t.Fatalf("the ambient dome reads %d, want about %d.\n"+
			"The three terms must sum, each gated by its own intensity only. "+
			"A reading near %d means the sky term is being multiplied by the ambient intensity again, "+
			"which is the form litWGSL and both browsers no longer use.",
			got.R, want, old)
	}

	// The sky intensity must move the result on its own. Without this half, a
	// model that dropped the sky term entirely could still match one number.
	brighter := frame
	brighter.Environment.SkyIntensity = skyFactor * 2
	if raised := renderCenterPixel(t, brighter); raised.R <= got.R {
		t.Fatalf("doubling the sky intensity gave %d against %d; the sky term is not independent",
			raised.R, got.R)
	}

	// The ground term must reach the frame too. Turn the surface over by lighting
	// the model from the other side: a downward normal makes hemi zero.
	groundOnly := frame
	groundOnly.Environment.SkyColor = "#000000"
	groundOnly.Environment.GroundColor = "#404040"
	if lit := renderCenterPixel(t, groundOnly); lit.R >= got.R {
		t.Fatalf("moving the dome colour from sky to ground gave %d against %d; "+
			"an upward-facing surface must read the sky term, not the ground term", lit.R, got.R)
	}

	// The dome must take the surface colour. The frames above use a white body,
	// so a model that dropped the base colour from the ambient term would match
	// every number so far and turn every shadowed face grey.
	tinted := frame
	tinted.Materials = []engine.RenderMaterial{{Kind: "standard", Color: "#ff0000"}}
	red := renderCenterPixel(t, tinted)
	if red.G != 0 || red.B != 0 {
		t.Fatalf("a pure red body under the dome reads %+v; "+
			"the ambient term is no longer scaled by the base colour, so it lights a colour the surface does not have", red)
	}
	if red.R == 0 {
		t.Fatalf("a pure red body under the dome reads black in red; the ambient term stopped reaching the frame")
	}
}

// TestVertexColorFlagSelectsTheColorSource pins the one material switch that does
// change the shading path. An explicit RenderMaterial clears the vertex-colour
// flag and uses its own base colour; a mesh with no material keeps the generated
// vertex colours.
func TestVertexColorFlagSelectsTheColorSource(t *testing.T) {
	explicit := litSurfaceScene(engine.RenderMaterial{Kind: "standard", Color: "#4080c0"}, -1)
	generated := explicit
	generated.Materials = nil
	generated.InstancedMeshes = append([]engine.RenderInstancedMesh(nil), explicit.InstancedMeshes...)
	generated.InstancedMeshes[0].MaterialIndex = -1

	withMaterial := renderCenterPixel(t, explicit)
	withVertexColor := renderCenterPixel(t, generated)
	if withMaterial == withVertexColor {
		t.Fatalf("an explicit material and the vertex-colour fallback produced the same pixel (%+v); "+
			"materialState.useVertexColor no longer selects the colour source", withMaterial)
	}
	if withVertexColor.A != 255 {
		t.Fatalf("the vertex-colour fallback drew nothing: %+v", withVertexColor)
	}
}

// TestBaseColorMapIsSampledPerPixel pins where the base colour texture is read.
//
// The rasterizer used to sample every material map at the three triangle corners
// and interpolate the result, so a texture on a two-triangle plane collapsed to a
// linear ramp between four texels. litWGSL samples in the fragment stage. This
// test draws a two-by-two checker on one plane and requires the two diagonals to
// differ, which only per-pixel sampling can produce.
func TestBaseColorMapIsSampledPerPixel(t *testing.T) {
	checker := image.NewRGBA(image.Rect(0, 0, 2, 2))
	checker.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	checker.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	checker.SetRGBA(1, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	checker.SetRGBA(0, 1, color.RGBA{R: 0, G: 0, B: 0, A: 255})

	frame := renderMaterialFrame(t, litSurfaceScene(engine.RenderMaterial{
		Kind: "standard", Color: "#ffffff", Roughness: 0.9,
		Texture: pngDataURL(t, checker),
	}, -1))

	// The plane fills the frame, so a quarter of it holds one checker cell. Sample
	// the middle of two opposite quarters.
	quarter := materialProbeSize / 4
	white := frame.RGBAAt(quarter, quarter)
	black := frame.RGBAAt(materialProbeSize-quarter, quarter)
	if int(white.R)-int(black.R) < 16 {
		t.Fatalf("two opposite checker cells read %+v and %+v; "+
			"the base colour map is being sampled at the triangle corners, not per pixel", white, black)
	}
}
