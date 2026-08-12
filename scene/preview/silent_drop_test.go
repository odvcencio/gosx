package preview_test

import (
	"image"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
	"m31labs.dev/gosx/scene/preview"
)

// This file names the record channels and the material fields that a native
// frame carries and never draws.
//
// A drop with a diagnostic is a feature gap. A drop without one is a lie: the
// author receives a PNG and a success exit code for a scene that never
// rendered. Each test below demands the diagnostic and demands that the frame
// really is empty, so neither half can rot alone.

// TestPointCloudsDrawNothingAndSayWhy pins a channel that dropped in silence.
// scene/preview lowers ir.Points into RenderBundle.Points, and render/bundle
// reads that field nowhere, so a star field or a scatter plot produced no pixels
// and no warning.
func TestPointCloudsDrawNothingAndSayWhy(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","points":[{"id":"stars","count":8,"size":0.4,"color":"#ff66aa",
		"positions":[0,0,0, 1,0,0, -1,0,0, 0,1,0, 0,-1,0, 1,1,0, -1,-1,0, 1,-1,0]}]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{
		Width: matrixWidth, Height: matrixHeight, Background: "#000000",
		Camera: cameraAt(0, 0, 5), DisableShadows: true, DisablePostFX: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	coverage, unique, variance := frameMetrics(result)
	diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.unsupported_points")
	if !reported {
		t.Fatalf("a point cloud drew coverage %.4f with %d colours and variance %.6f and reported nothing",
			coverage, unique, variance)
	}
	if !strings.Contains(diagnostic.Message, "reads no RenderBundle.Points field") {
		t.Fatalf("the point-cloud diagnostic does not name the blocker: %s", diagnostic.Message)
	}
	if coverage > 0.001 {
		t.Fatalf("point clouds now draw %.2f%% of the frame; drop the warning and add a coverage gate instead",
			coverage*100)
	}
}

// TestComputeParticlesNeedMoreThanOneFrameAndSayWhy pins a gap that is not a
// dropped draw. rasterizeParticles works, and TestBundleFrameRendersComputeParticles
// proves it. A particle spawns with age zero and fades in over the first fifteen
// percent of its life, and a preview always builds a fresh renderer and draws one
// frame, so every particle is still fully transparent when the frame is captured.
func TestComputeParticlesNeedMoreThanOneFrameAndSayWhy(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","computeParticles":[{"id":"spark","count":64,
		"emitter":{"kind":"point","radius":0.001,"lifetime":0.1,"scatter":0.01},
		"forces":[{"kind":"gravity","strength":0}],
		"material":{"color":"#00ff88","colorEnd":"#00ff88","size":3,"sizeEnd":3,"opacity":1,"opacityEnd":1}}]}`
	for _, time := range []float64{0, 1.0 / 60.0, 0.5, 2} {
		result, err := preview.RenderJSON([]byte(doc), preview.Options{
			Width: 64, Height: 48, Background: "#000020", Time: time,
			Camera: cameraAt(0, 0, 5), DisableShadows: true, DisablePostFX: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		coverage, unique, variance := frameMetrics(result)
		if coverage > 0.001 {
			t.Fatalf("a one-frame preview at time %.4f now draws particles (%.2f%%, %d colours, variance %.6f); "+
				"drop the warning and pin the pixels instead", time, coverage*100, unique, variance)
		}
		diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.unsupported_compute-particles")
		if !reported || !strings.Contains(diagnostic.Message, "fades in over the first") {
			t.Fatalf("time %.4f drew no particle and did not explain why: %+v", time, result.Bundle.Diagnostics)
		}
	}
}

// TestEnvironmentFieldsThatChangeNothingAreReported covers the environment terms
// the CPU path never reads, and the terms it reads and once did not.
//
// Fog is the only one left in the ignored list. No copy of the material carries
// a fog term on the native side at all, so it is a material gap and not a pass
// gap. Exposure, tone mapping and the environment map all reach a pixel now, so
// each has a case that proves it and states the direction of the change.
func TestEnvironmentFieldsThatChangeNothingAreReported(t *testing.T) {
	base := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"sphere","radius":1.3,"color":"#66ccff"}],
		"lights":[{"id":"sun","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]`
	render := func(t *testing.T, environment string) *preview.Result {
		t.Helper()
		document := base + `}`
		if environment != "" {
			document = base + `,"environment":` + environment + `}`
		}
		result, err := preview.RenderJSON([]byte(document), preview.Options{
			Width: matrixWidth, Height: matrixHeight, Background: "#000000",
			Camera: cameraAt(0, 1, 5), DisableShadows: true, DisablePostFX: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	baseline := render(t, "")
	baseHash := hashPixels(baseline.Image)
	if coverage, unique, variance := frameMetrics(baseline); coverage < 0.05 || unique < 80 || variance < 0.007 {
		t.Fatalf("the baseline sphere is not shaded: coverage %.4f, unique colours %d, variance %.6f",
			coverage, unique, variance)
	}

	// envMap left this list. The rasterizer now samples an environment cubemap
	// for image-based lighting, so it reaches a pixel; its case moved below.
	//
	// Exposure and tone mapping left it on 2026-07-26, when the headless device
	// started running the present pass instead of copying it. Their cases moved
	// below too.
	//
	// Fog is the one that remains, and it is not a post-pass problem. Neither
	// litWGSL in render/bundle nor the CPU rasterizer carries a fog term at all.
	// The browser applies fog inside its material shader, so closing this needs
	// a material change on both sides, not a pass.
	for _, tc := range []struct{ name, environment, field string }{
		{"fog", `{"fogColor":"#ffffff","fogDensity":0.6}`, "fogDensity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := render(t, tc.environment)
			if hashPixels(result.Image) != baseHash {
				t.Fatalf("%s now changes the frame; move it out of the ignored list and pin the new pixels", tc.name)
			}
			diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.environment_fields_ignored")
			if !reported || !strings.Contains(diagnostic.Message, tc.field) {
				t.Fatalf("%s changed nothing and was not named: %+v", tc.name, result.Bundle.Diagnostics)
			}
		})
	}

	// The present pass runs on the CPU path, so these two now shape the frame.
	// A case states the direction as well as the fact, because a field that
	// changed the frame the wrong way still changes it.
	t.Run("exposure-reaches-the-frame", func(t *testing.T) {
		dim := render(t, `{"exposure":0.4}`)
		bright := render(t, `{"exposure":2.5}`)
		if hashPixels(dim.Image) == baseHash {
			t.Fatal("a lowered exposure changed no pixel; the present pass is not reading the exposure lane")
		}
		if hashPixels(bright.Image) == baseHash {
			t.Fatal("a raised exposure changed no pixel; the present pass is not reading the exposure lane")
		}
		if meanChannel(bright.Image) <= meanChannel(dim.Image) {
			t.Fatalf("exposure 2.5 gave mean %.3f and exposure 0.4 gave %.3f; a raised exposure must brighten the frame",
				meanChannel(bright.Image), meanChannel(dim.Image))
		}
	})
	t.Run("toneMapping-reaches-the-frame", func(t *testing.T) {
		// The empty tone-map string already means ACES, so the baseline carries
		// that curve. These two ask for different operators, so each must move
		// away from the baseline and away from the other.
		clamped := render(t, `{"toneMapping":"none"}`)
		reinhard := render(t, `{"toneMapping":"reinhard"}`)
		if hashPixels(clamped.Image) == baseHash {
			t.Fatal(`tone mapping "none" changed no pixel; the present pass still applies one fixed curve`)
		}
		if hashPixels(reinhard.Image) == baseHash {
			t.Fatal(`tone mapping "reinhard" changed no pixel; the present pass still applies one fixed curve`)
		}
		if hashPixels(clamped.Image) == hashPixels(reinhard.Image) {
			t.Fatal(`"none" and "reinhard" produced the same frame; the mode lane is not selecting an operator`)
		}
	})

	// envMap reaches a pixel through the cubemap image-based lighting term.
	t.Run("envMap-reaches-the-frame", func(t *testing.T) {
		result := render(t, `{"envMap":"/env/studio.hdr","envIntensity":2}`)
		if hashPixels(result.Image) == baseHash {
			t.Fatal("envMap changed no CPU pixel; the image-based lighting term should sample the cube")
		}
		if diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.environment_fields_ignored"); reported &&
			strings.Contains(diagnostic.Message, "envMap") {
			t.Fatalf("envMap shades and must not be reported as ignored: %s", diagnostic.Message)
		}
	})

	// The WebGPU environment-map cell went true (16a-scene-webgpu.js binds
	// envMapTex/envMapSampler and taps envEquirectUV). The gap warning this
	// test used to require is gone: capability.Supports gates
	// environmentMapBackendDiagnostic, and it now returns false unconditionally
	// for this scene, so scene.preview.environment_map_backend_gap never fires.
	t.Run("envMap-raises-no-WebGPU-gap-warning", func(t *testing.T) {
		if !capability.Supports(capability.BackendWebGPU, capability.FeatureEnvironmentMap) {
			t.Fatal("the WebGPU environment-map cell went false again. Restore the gap-warning case and pin " +
				"the browser regression instead.")
		}
		result := render(t, `{"envMap":"/env/studio.hdr","envIntensity":2}`)
		if _, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.environment_map_backend_gap"); reported {
			t.Fatalf("a scene with an authored envMap still raised the WebGPU gap warning, but both GPU "+
				"backends implement the feature now: %+v", result.Bundle.Diagnostics)
		}
	})

	// The terms that do reach a pixel must not be reported. A blanket warning
	// would train the reader to ignore the whole diagnostic.
	for _, tc := range []struct{ name, environment string }{
		{"ambient", `{"ambientColor":"#ff0000","ambientIntensity":0.8}`},
		{"hemisphere", `{"skyColor":"#ff0000","groundColor":"#0000ff","skyIntensity":1,"groundIntensity":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := render(t, tc.environment)
			if hashPixels(result.Image) == baseHash {
				t.Fatalf("%s is documented as read by the rasterizer but changed no pixel", tc.name)
			}
			if _, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.environment_fields_ignored"); reported {
				t.Fatalf("%s changes the frame and must not be reported ignored", tc.name)
			}
		})
	}
}

// TestMaterialKindsShadeIdentically pins the largest material gap in one place.
//
// render/bundle builds its material fingerprint from colour, opacity, the
// physical scalars and the map slots. It never reads RenderMaterial.Kind. So a
// flat, ghost, glass, glow, matte or custom material produces pixels that are
// byte-identical to a standard material with the same style, and the frame now
// says so through the ignored-fields note.
func TestMaterialKindsShadeIdentically(t *testing.T) {
	render := func(t *testing.T, kind string) *preview.Result {
		t.Helper()
		doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"sphere","radius":1.3,` +
			`"materialKind":"` + kind + `","color":"#4488ff"}],` +
			`"lights":[{"id":"sun","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]}`
		result, err := preview.RenderJSON([]byte(doc), preview.Options{
			Width: matrixWidth, Height: matrixHeight, Background: "#000000",
			Camera: cameraAt(0, 1, 5), DisableShadows: true, DisablePostFX: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	standard := render(t, "standard")
	standardHash := hashPixels(standard.Image)
	coverage, unique, variance := frameMetrics(standard)
	if coverage < 0.05 || unique < 80 || variance < 0.007 {
		t.Fatalf("the standard material draws nothing useful: coverage %.4f, unique colours %d, variance %.6f",
			coverage, unique, variance)
	}
	if _, reported := findDiagnostic(standard.Bundle.Diagnostics, "scene.preview.material_fields_ignored"); reported {
		t.Fatal("the standard kind is what the rasterizer does, so it must raise no ignored-fields note")
	}

	for _, kind := range []string{"flat", "ghost", "glass", "glow", "matte", "custom", "line-basic", "line-dashed"} {
		t.Run(kind, func(t *testing.T) {
			result := render(t, kind)
			if hashPixels(result.Image) != standardHash {
				t.Fatalf("material kind %q now changes the frame; the CPU path gained a kind-aware shader, "+
					"so remove materialKind from the ignored list", kind)
			}
			diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.material_fields_ignored")
			if !reported || !strings.Contains(diagnostic.Message, "materialKind") {
				t.Fatalf("material kind %q shades like standard and was not named: %+v",
					kind, result.Bundle.Diagnostics)
			}
		})
	}
}

// TestPhysicallyBasedFieldsChangeNoPixel pins the browser-only material surface.
// The CPU rasterizer shades from base colour, opacity, emissive and one base
// colour texture. Every physically-based scalar and every map slot other than
// base colour reaches the material uniform and dies there.
func TestPhysicallyBasedFieldsChangeNoPixel(t *testing.T) {
	render := func(t *testing.T, extra string) *preview.Result {
		t.Helper()
		fields := ""
		if extra != "" {
			fields = "," + extra
		}
		doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"sphere","radius":1.3,` +
			`"color":"#4488ff"` + fields + `}],` +
			`"lights":[{"id":"sun","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]}`
		result, err := preview.RenderJSON([]byte(doc), preview.Options{
			Width: matrixWidth, Height: matrixHeight, Background: "#000000",
			Camera: cameraAt(0, 1, 5), DisableShadows: true, DisablePostFX: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	baseHash := hashPixels(render(t, "").Image)

	// INVERTED. This loop used to assert each field changed NO pixel, which was
	// true while the CPU rasterizer shaded a Lambert term. render/gpu/headless
	// now runs the whole litWGSL fragment stage, so each of these reaches a
	// pixel and the assertion is the other way round.
	//
	// Keep the loop rather than deleting it. It is the record of which authored
	// material fields the CPU path can express, and it must stay accurate in
	// both directions: a field that stops shading has to fail here.
	for _, tc := range []struct{ name, json string }{
		{"roughness", `"roughness":0.95`},
		{"metalness", `"metalness":1`},
		{"clearcoat", `"clearcoat":1`},
		{"sheen", `"sheen":1`},
		{"transmission", `"transmission":1`},
		{"iridescence", `"iridescence":1`},
		{"anisotropy", `"anisotropy":1`},
		{"normalMap", `"normalMap":"/textures/normal.png"`},
		{"roughnessMap", `"roughnessMap":"/textures/rough.png"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if hashPixels(render(t, tc.json).Image) == baseHash {
				t.Fatalf("%s changed no CPU pixel; the fragment stage should shade it", tc.name)
			}
		})
	}

	// Two maps MODULATE a factor, so they move nothing while that factor is
	// zero. They are sampled, not ignored, and this records the distinction so
	// an author who sees no change checks the scalar first.
	for _, tc := range []struct{ name, json string }{
		{"metalnessMap", `"metalnessMap":"/textures/metal.png","metalness":1`},
		{"emissiveMap", `"emissiveMap":"/textures/emit.png","emissive":1`},
	} {
		t.Run("modulating-"+tc.name, func(t *testing.T) {
			if hashPixels(render(t, tc.json).Image) == baseHash {
				t.Fatalf("%s changed no CPU pixel even with its factor set", tc.name)
			}
		})
	}

	// wireframe is the one material field left that truly cannot reach a pixel.
	// It has no lane in materialFingerprint, so it never reaches the material
	// uniform; it needs a line-topology pipeline in render/bundle. Invert this
	// case when that exists.
	t.Run("wireframe", func(t *testing.T) {
		result := render(t, `"wireframe":true`)
		if hashPixels(result.Image) != baseHash {
			t.Fatal("wireframe now changes a CPU pixel; remove it from IgnoredMaterialFields and invert this case")
		}
		diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.material_fields_ignored")
		if !reported || !strings.Contains(diagnostic.Message, "wireframe") {
			t.Fatalf("wireframe changed nothing and was not named: %+v", result.Bundle.Diagnostics)
		}
	})

	// The style fields that do reach a pixel must change it. Without this half,
	// a rasterizer that ignored every material field would pass the checks above.
	for _, tc := range []struct{ name, json string }{
		{"color", `"color":"#ff2200"`},
		{"opacity", `"opacity":0.35,"blendMode":"alpha"`},
		{"emissive", `"emissive":1`},
	} {
		t.Run("reaches-"+tc.name, func(t *testing.T) {
			doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"sphere","radius":1.3,` +
				tc.json + `}],` +
				`"lights":[{"id":"sun","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]}`
			result, err := preview.RenderJSON([]byte(doc), preview.Options{
				Width: matrixWidth, Height: matrixHeight, Background: "#000000",
				Camera: cameraAt(0, 1, 5), DisableShadows: true, DisablePostFX: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if hashPixels(result.Image) == baseHash {
				t.Fatalf("%s is documented as read by the rasterizer but changed no pixel", tc.name)
			}
		})
	}
}

// meanChannel returns the average of the red, green and blue channels of an
// image, on a zero to one scale. Exposure scales every channel, so one mean is
// enough to state the direction of the change.
func meanChannel(img *image.RGBA) float64 {
	bounds := img.Bounds()
	count := float64(bounds.Dx() * bounds.Dy() * 3)
	if count == 0 {
		return 0
	}
	sum := 0.0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			sum += float64(c.R) + float64(c.G) + float64(c.B)
		}
	}
	return sum / count / 255
}

// TestEveryAuthoredLightKindChangesThePreviewFrame proves the preview shades the
// whole light array, which is what makes the lightDiagnostics note in coverage.go
// wrong today.
//
// The scene lights one sphere and pushes every ambient term to almost nothing, so
// a kind the rasterizer drops leaves the frame at the background. A sphere carries
// every facing ratio, so one camera serves a light from any direction. A
// rect-area light is the reference for "dropped": engine.RenderLight carries no
// width and no height, so no copy of the renderer can integrate over a rectangle.
//
// A PASS PROVES: ambient, directional, point, spot and hemisphere each light the
// frame on their own, and a rect-area light does not.
//
// A PASS DOES NOT PROVE: that the preview and the browser agree numerically.
// render/gpu/headless/lit_parity_test.go pins the CPU copy against litWGSL, and
// render/bundle/lit_drift_test.go pins litWGSL against the browser.
func TestEveryAuthoredLightKindChangesThePreviewFrame(t *testing.T) {
	document := func(light string) string {
		return `{"schema":"gosx.scene3d.ir.v1",` +
			`"objects":[{"id":"ball","kind":"sphere","radius":1.6,"segments":24,"color":"#ffffff","roughness":0.9}],` +
			`"environment":{"ambientColor":"#000000","ambientIntensity":0.0001,` +
			`"skyColor":"#000000","skyIntensity":0.0001,` +
			`"groundColor":"#000000","groundIntensity":0.0001,"toneMapping":"none"},` +
			`"lights":[` + light + `]}`
	}
	brightest := func(light string) int {
		result, err := preview.RenderJSON([]byte(document(light)), preview.Options{
			Width: matrixWidth, Height: matrixHeight, Background: "#000000",
			Camera: cameraAt(0, 0, 5), DisableShadows: true, DisablePostFX: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		best := 0
		bounds := result.Image.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := result.Image.RGBAAt(x, y)
				if sum := int(c.R) + int(c.G) + int(c.B); sum > best {
					best = sum
				}
			}
		}
		return best
	}

	const rectArea = `{"id":"panel","kind":"rect-area","color":"#ffffff","intensity":4,"z":3,"width":4,"height":4}`
	if dark := brightest(rectArea); dark != 0 {
		t.Fatalf("the rect-area reference frame is not black (channel sum %d), so this test cannot tell a shaded kind from a dropped one", dark)
	}

	for _, tc := range []struct{ name, light string }{
		{"ambient", `{"id":"fill","kind":"ambient","color":"#ffffff","intensity":0.5}`},
		{"directional", `{"id":"key","kind":"directional","color":"#ffffff","intensity":1,"directionY":-1,"directionZ":-0.2}`},
		{"point", `{"id":"lamp","kind":"point","color":"#ffffff","intensity":4,"z":3}`},
		{"spot", `{"id":"spot","kind":"spot","color":"#ffffff","intensity":8,"z":3,"directionZ":-1,"angle":0.7,"penumbra":0.3}`},
		{"hemisphere", `{"id":"dome","kind":"hemisphere","color":"#ffffff","groundColor":"#000000","intensity":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if brightest(tc.light) == 0 {
				t.Fatalf("a %s light lit nothing; the preview drops that kind", tc.name)
			}
		})
	}
}
