package capability

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// lobeEvidence ties one fidelity label to renderer source. Like cellEvidence, it
// reads first and decides second, so a label that is raised or lowered without
// the renderer changing fails whichever way it moved.
//
// Three kinds of symbol feed the decision:
//
//   - core:   the lobe exists at all. Any core symbol missing means unsupported.
//   - standIn: a symbol that marks the cheap substitute. Any one present means
//     approximate, even when every full symbol is also present.
//   - full:   a symbol that exists only when the published model is implemented
//     and takes every input the scene API carries. Exact needs a full reading,
//     every full symbol present and no stand-in present.
type lobeEvidence struct {
	t       evidenceT
	lobe    Lobe
	backend Backend

	coreRead, fullRead int
	coreMissing        []string
	standInFound       []string
	fullMissing        []string
}

func lobeEvidenceFor(t evidenceT, lobe Lobe, backend Backend) *lobeEvidence {
	t.Helper()
	return &lobeEvidence{t: t, lobe: lobe, backend: backend}
}

func (e *lobeEvidence) core(path, source string, symbols ...string) *lobeEvidence {
	e.coreRead += len(symbols)
	e.coreMissing = append(e.coreMissing, missingSymbols(path, source, symbols...)...)
	return e
}

func (e *lobeEvidence) standIn(path, source string, symbols ...string) *lobeEvidence {
	for _, symbol := range symbols {
		if strings.Contains(source, symbol) {
			e.standInFound = append(e.standInFound, fmt.Sprintf("%s: %q", path, symbol))
		}
	}
	return e
}

func (e *lobeEvidence) full(path, source string, symbols ...string) *lobeEvidence {
	e.fullRead += len(symbols)
	e.fullMissing = append(e.fullMissing, missingSymbols(path, source, symbols...)...)
	return e
}

// want is the level the renderer source demands.
func (e *lobeEvidence) want() Level {
	switch {
	case len(e.coreMissing) > 0:
		return Unsupported
	case len(e.standInFound) > 0, len(e.fullMissing) > 0, e.fullRead == 0:
		return Approximate
	}
	return Exact
}

func (e *lobeEvidence) assertAgrees(why string) {
	e.t.Helper()
	e.assertLevelIs(FidelityOf(e.lobe, e.backend).Level, why)
}

func (e *lobeEvidence) assertLevelIs(got Level, why string) {
	e.t.Helper()
	if e.coreRead == 0 {
		e.t.Fatalf("no core evidence was read for Fidelity[%s][%s]; a lobe with no reading proves nothing", e.lobe, e.backend)
		return
	}
	want := e.want()
	if got == want {
		return
	}
	e.t.Fatalf("Fidelity[%s][%s] is %q but the renderer source demands %q.\n"+
		"Core symbols missing: %s\n"+
		"Stand-in symbols present: %s\n"+
		"Full-model symbols missing: %s (read %d)\n"+
		"Reasoning: %s\n"+
		"Change the label only together with the renderer, and rewrite this reading to match.",
		e.lobe, e.backend, got, want,
		strings.Join(sortedCopy(e.coreMissing), ", "),
		strings.Join(sortedCopy(e.standInFound), ", "),
		strings.Join(sortedCopy(e.fullMissing), ", "), e.fullRead, why)
}

// TestFidelityEvidenceFailsEveryWrongLabel proves the reader catches a wrong
// label in every direction, on synthetic source and without touching the table.
func TestFidelityEvidenceFailsEveryWrongLabel(t *testing.T) {
	const path = "synthetic.glsl"
	read := map[string]func(e *lobeEvidence){
		"absent": func(e *lobeEvidence) { e.core(path, "nothing here", "lobeCore").full(path, "nothing here", "lobeFull") },
		"stand-in": func(e *lobeEvidence) {
			e.core(path, "lobeCore cheapRim", "lobeCore").standIn(path, "lobeCore cheapRim", "cheapRim").full(path, "lobeCore cheapRim", "lobeFull")
		},
		"stand-in and full": func(e *lobeEvidence) {
			src := "lobeCore lobeFull cheapRim"
			e.core(path, src, "lobeCore").standIn(path, src, "cheapRim").full(path, src, "lobeFull")
		},
		"partial full": func(e *lobeEvidence) {
			src := "lobeCore lobeFull"
			e.core(path, src, "lobeCore").full(path, src, "lobeFull", "lobeSecondInput")
		},
		"full": func(e *lobeEvidence) {
			src := "lobeCore lobeFull lobeSecondInput"
			e.core(path, src, "lobeCore").full(path, src, "lobeFull", "lobeSecondInput")
		},
		"no full reading": func(e *lobeEvidence) { e.core(path, "lobeCore", "lobeCore") },
	}
	want := map[string]Level{
		"absent": Unsupported, "stand-in": Approximate, "stand-in and full": Approximate,
		"partial full": Approximate, "full": Exact, "no full reading": Approximate,
	}
	levels := []Level{Exact, Approximate, Unsupported}
	for name, fn := range read {
		for _, label := range levels {
			rec := &recordingT{}
			e := lobeEvidenceFor(rec, LobeSheen, BackendWebGPU)
			fn(e)
			e.assertLevelIs(label, "synthetic reading")
			if wantFail := label != want[name]; rec.failed != wantFail {
				t.Errorf("%s read as %q: label %q failed=%v want %v (%s)", name, want[name], label, rec.failed, wantFail, rec.message)
			}
		}
	}
	rec := &recordingT{}
	e := lobeEvidenceFor(rec, LobeSheen, BackendWebGPU)
	e.assertLevelIs(Exact, "empty")
	if !rec.failed {
		t.Error("a reading with no core evidence must fail")
	}
}

// TestFidelityTableIsComplete requires a label and a reason for every lobe on
// both GPU backends, and no entry for an unknown lobe or backend.
func TestFidelityTableIsComplete(t *testing.T) {
	known := map[Lobe]bool{}
	for _, lobe := range AllLobes {
		known[lobe] = true
		for _, backend := range []Backend{BackendWebGPU, BackendWebGL} {
			note, ok := Fidelity[lobe][backend]
			if !ok {
				t.Errorf("Fidelity[%s][%s] has no label", lobe, backend)
				continue
			}
			switch note.Level {
			case Exact, Approximate, Unsupported:
			default:
				t.Errorf("Fidelity[%s][%s] has level %q; want exact, approximate or unsupported", lobe, backend, note.Level)
			}
			if len(strings.Fields(note.Why)) < 4 {
				t.Errorf("Fidelity[%s][%s] needs a reason an author can act on; got %q", lobe, backend, note.Why)
			}
		}
	}
	for lobe, row := range Fidelity {
		if !known[lobe] {
			t.Errorf("Fidelity has a row for %q, which AllLobes does not list", lobe)
		}
		for backend := range row {
			if backend != BackendWebGPU && backend != BackendWebGL {
				t.Errorf("Fidelity[%s] has a label for %q; Canvas2D is covered by FidelityOf", lobe, backend)
			}
		}
	}
	for _, lobe := range AllLobes {
		if got := FidelityOf(lobe, BackendCanvas2D).Level; got != Unsupported {
			t.Errorf("FidelityOf(%s, canvas2d) = %q, want unsupported", lobe, got)
		}
	}
}

const (
	motionBlurWebGPUPath = "../../client/runtime/scene3d/motion-blur-wgpu.ts"
	motionBlurWebGLPath  = "../../client/runtime/scene3d/motion-blur-gl.ts"
	taaWebGPUPath        = "../../client/runtime/scene3d/taa-wgpu.ts"
)

// TestFidelityLabelsMatchRendererSource reads every renderer shader and checks
// each label against it. Symbols marked "future" below name what a landing must
// add to raise a label; they are absent today, and that absence is the evidence.
func TestFidelityLabelsMatchRendererSource(t *testing.T) {
	gpu := readRenderer(t, webgpuRendererPath)
	gl := readRenderer(t, webglRendererPath)
	mbGPU := readRenderer(t, motionBlurWebGPUPath)
	mbGL := readRenderer(t, motionBlurWebGLPath)
	taa := readRenderer(t, taaWebGPUPath)

	type src struct{ path, text string }
	gpuSrc, glSrc := src{webgpuRendererPath, gpu}, src{webglRendererPath, gl}

	// Clear coat: a GGX coat lobe with its own Fresnel and energy loss is
	// present, but the roughness is the constant 0.04 and the scene API carries
	// only a weight. A coat roughness input reaches the shader as
	// clearcoatRoughness (future).
	lobeEvidenceFor(t, LobeClearcoat, BackendWebGPU).
		core(gpuSrc.path, gpu, "fn gosxLayerSetup", "fn gosxCoatOver").
		standIn(gpuSrc.path, gpu, "surfaceClearcoatRoughness = 0.04").
		full(gpuSrc.path, gpu, "material.clearcoatRoughness").
		assertAgrees("The coat lobe is real, but surfaceClearcoatRoughness is pinned at 0.04 and no coat normal exists.")
	lobeEvidenceFor(t, LobeClearcoat, BackendWebGL).
		core(glSrc.path, gl, "vec3 gosxCoatOver(", "GosxLayer gosxLayerSetup(").
		standIn(glSrc.path, gl, "surfaceClearcoatRoughness = 0.04").
		full(glSrc.path, gl, "u_clearcoatRoughness").
		assertAgrees("The coat lobe is real, but surfaceClearcoatRoughness is pinned at 0.04 and no coat normal exists.")

	// Sheen: a view-angle rim, not the Charlie cloth lobe. gosxSheenLobe and
	// gosxSheenAlbedo are the functions the sheen landing adds (future).
	lobeEvidenceFor(t, LobeSheen, BackendWebGPU).
		core(gpuSrc.path, gpu, "surfaceSheen").
		standIn(gpuSrc.path, gpu, "pow(1.0 - NoV, 3.0) * sheen").
		full(gpuSrc.path, gpu, "gosxSheenLobe", "gosxSheenAlbedo").
		assertAgrees("The shader adds albedo * (1 - NoV)^3 * sheen * 0.55 to the colour; no sheen colour, roughness or Charlie distribution exists.")
	lobeEvidenceFor(t, LobeSheen, BackendWebGL).
		core(glSrc.path, gl, "surfaceSheen").
		standIn(glSrc.path, gl, "pow(1.0 - NoV, 3.0) * sheen").
		full(glSrc.path, gl, "gosxSheenLobe", "gosxSheenAlbedo").
		assertAgrees("The shader adds albedo * (1 - NoV)^3 * sheen * 0.55 to the colour; no sheen colour, roughness or Charlie distribution exists.")

	// KHR specular: the factor, colour and both textures reach the dielectric
	// F0 and F90 that direct light and image light share.
	lobeEvidenceFor(t, LobeSpecular, BackendWebGPU).
		core(gpuSrc.path, gpu, "material.specularF0", "material.specularF90").
		full(gpuSrc.path, gpu, "hasSpecularIntensityMap", "hasSpecularColorMap", "material.specularColorLog").
		assertAgrees("Factor, colour, intensity texture and colour texture all feed F0 and F90 before the metallic mix.")
	lobeEvidenceFor(t, LobeSpecular, BackendWebGL).
		core(glSrc.path, gl, "u_specularF0", "u_specularF90").
		full(glSrc.path, gl, "u_hasSpecularIntensityMap", "u_hasSpecularColorMap", "u_specularColorLog").
		assertAgrees("Factor, colour, intensity texture and colour texture all feed F0 and F90 before the metallic mix.")

	// Iridescence: a cosine palette on NoV, not a thin film. gosxThinFilm is the
	// function a thin-film landing adds (future).
	lobeEvidenceFor(t, LobeIridescence, BackendWebGPU).
		core(gpuSrc.path, gpu, "material.iridescence").
		standIn(gpuSrc.path, gpu, "cos(vec3f(0.0, 2.1, 4.2)").
		full(gpuSrc.path, gpu, "gosxThinFilm").
		assertAgrees("The shader tints the colour with a fixed cosine palette on NoV; film IOR, thickness and interference do not exist.")
	lobeEvidenceFor(t, LobeIridescence, BackendWebGL).
		core(glSrc.path, gl, "u_iridescence").
		standIn(glSrc.path, gl, "cos(vec3(0.0, 2.1, 4.2)").
		full(glSrc.path, gl, "gosxThinFilm").
		assertAgrees("The shader tints the colour with a fixed cosine palette on NoV; film IOR, thickness and interference do not exist.")

	// Anisotropy: roughness is lowered by up to 28 percent; there is no
	// direction. gosxAnisotropicGGX is the function a real lobe adds (future).
	lobeEvidenceFor(t, LobeAnisotropy, BackendWebGPU).
		core(gpuSrc.path, gpu, "material.anisotropy").
		standIn(gpuSrc.path, gpu, "abs(material.anisotropy) * 0.28").
		full(gpuSrc.path, gpu, "gosxAnisotropicGGX").
		assertAgrees("Anisotropy only scales the isotropic roughness by up to 28 percent; no tangent-aligned highlight stretch exists.")
	lobeEvidenceFor(t, LobeAnisotropy, BackendWebGL).
		core(glSrc.path, gl, "u_anisotropy").
		standIn(glSrc.path, gl, "abs(u_anisotropy) * 0.28").
		full(glSrc.path, gl, "gosxAnisotropicGGX").
		assertAgrees("Anisotropy only scales the isotropic roughness by up to 28 percent; no tangent-aligned highlight stretch exists.")

	// Transmission with thickness and attenuation: a refracted screen-space
	// read of the opaque scene plus Beer-Lambert attenuation. Thickness is the
	// authored number, with no object scale (volumeThicknessScale, future), and
	// glass behind glass is not captured.
	lobeEvidenceFor(t, LobeTransmission, BackendWebGPU).
		core(gpuSrc.path, gpu, "fn volumeTransmission", "refract(-V, N, 1.0 / ior)", "pow(material.attenuationColor").
		standIn(gpuSrc.path, gpu, "textureSampleLevel(transmissionScene").
		full(gpuSrc.path, gpu, "volumeThicknessScale").
		assertAgrees("Refraction reads a screen-space copy of the opaque scene at one exit point; thickness has no object-scale term and glass does not refract glass.")
	lobeEvidenceFor(t, LobeTransmission, BackendWebGL).
		core(glSrc.path, gl, "vec3 volumeTransmission(", "refract(-V, N, 1.0 / u_volume.y)", "pow(u_attenuationColor").
		standIn(glSrc.path, gl, "textureLod(u_transmissionScene").
		full(glSrc.path, gl, "volumeThicknessScale").
		assertAgrees("Refraction reads a screen-space copy of the opaque scene at one exit point; thickness has no object-scale term and glass does not refract glass.")

	// Dispersion, diffuse transmission and subsurface: no scene input and no
	// shader symbol on either backend. Each lowercase stem would appear in a
	// uniform, a struct field or a function name once a path existed.
	for _, c := range []struct {
		lobe  Lobe
		stems []string
	}{
		{LobeDispersion, []string{"dispersion", "Dispersion"}},
		{LobeDiffuseTransmit, []string{"diffuseTransmission", "DiffuseTransmission"}},
		{LobeSubsurface, []string{"subsurface", "Subsurface"}},
	} {
		for _, b := range []struct {
			backend Backend
			src     src
		}{{BackendWebGPU, gpuSrc}, {BackendWebGL, glSrc}} {
			// core is the stem that a working path must carry; a missing stem
			// reads as unsupported, a present stem raises the reading.
			present := ""
			for _, stem := range c.stems {
				if strings.Contains(b.src.text, stem) {
					present = stem
					break
				}
			}
			symbol := c.stems[0]
			if present != "" {
				symbol = present
			}
			lobeEvidenceFor(t, c.lobe, b.backend).
				core(b.src.path, b.src.text, symbol).
				assertAgrees(fmt.Sprintf("The renderer carries no %q symbol, so no path draws the lobe.", c.stems[0]))
		}
	}

	// Object motion blur. WebGPU adds a velocity for each retained or skinned
	// mesh that moved, but not for instanced batches or crowds. WebGL2 has no
	// per-object velocity input at all, so only the camera blurs.
	lobeEvidenceFor(t, LobeObjectMotionBlur, BackendWebGPU).
		core(motionBlurWebGPUPath, mbGPU, "motion.velocityView").
		core(taaWebGPUPath, taa, "function taaDrawMovers(").
		standIn(taaWebGPUPath, taa, "(!obj.retainedGeometry && !skin)").
		full(taaWebGPUPath, taa, "taaDrawInstanceMovers").
		assertAgrees("Moving retained and skinned meshes add their own velocity; instanced batches and crowds blur with the camera only.")
	lobeEvidenceFor(t, LobeObjectMotionBlur, BackendWebGL).
		core(motionBlurWebGLPath, mbGL, "velocityView").
		assertAgrees("The WebGL2 blur reprojects depth through the previous camera and reads no per-object velocity, so a moving object on a still camera stays sharp.")

	// Every label in the table carries a reading above; a lobe added to the table
	// without one is caught by the completeness test, and the readings above
	// cover AllLobes by construction. Keep the lists in step.
	covered := []Lobe{LobeClearcoat, LobeSheen, LobeSpecular, LobeIridescence, LobeAnisotropy,
		LobeTransmission, LobeDispersion, LobeDiffuseTransmit, LobeSubsurface, LobeObjectMotionBlur}
	sort.Slice(covered, func(i, j int) bool { return covered[i] < covered[j] })
	listed := append([]Lobe(nil), AllLobes...)
	sort.Slice(listed, func(i, j int) bool { return listed[i] < listed[j] })
	if fmt.Sprint(covered) != fmt.Sprint(listed) {
		t.Fatalf("this test reads %v but AllLobes lists %v; read the renderer for every lobe", covered, listed)
	}
}
