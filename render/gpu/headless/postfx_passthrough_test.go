package headless

import (
	"image"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
)

// This file guards one defect class: a screen-space pass that this backend
// cannot execute must pass its input through, never drop it.
//
// The native post-effect chain clears a scratch target, draws into it, and hands
// that target to the next stage. The last stage's target becomes the presented
// image. So a dropped draw presents a target that holds only its clear colour.
// One authored vignette turned every headless frame flat black, the renderer
// returned no error, and no test in this repository could see it.

// postFXProbeWidth and postFXProbeHeight keep the probe cheap. A lit sphere on a
// coloured background fills enough of a 64x48 frame to prove the image survived.
const (
	postFXProbeWidth  = 64
	postFXProbeHeight = 48
)

// postFXProbeScene builds one lit sphere over a non-black background, plus the
// post effects the caller names. The background must not be black: a frame
// erased to the pass clear colour is opaque black, so a black background would
// hide the exact failure this file tests.
func postFXProbeScene(effects ...engine.RenderPostEffect) engine.RenderBundle {
	return engine.RenderBundle{
		Background: "#204060",
		Camera:     engine.RenderCamera{Y: 1, Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Materials:  []engine.RenderMaterial{{Kind: "standard", Color: "#ffcc66", Roughness: 0.6}},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1.2,
			DirectionX: -0.4, DirectionY: -1, DirectionZ: -0.3,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "probe", Kind: "sphere", Radius: 1.4, Segments: 16,
			MaterialIndex: 0, InstanceCount: 1,
			Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		}},
		PostEffects: effects,
	}
}

func renderPostFXProbe(t *testing.T, frame engine.RenderBundle) *image.RGBA {
	t.Helper()
	device, surface := New(postFXProbeWidth, postFXProbeHeight)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	if err := renderer.Frame(frame, postFXProbeWidth, postFXProbeHeight, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return cloneTestRGBA(device.Framebuffer())
}

// frameEvidence counts what a frame actually shows. A blank frame scores one
// unique colour and zero luminance variance, so the two numbers together reject
// the erased frame that this file guards.
func frameEvidence(img *image.RGBA) (uniqueColors int, luminanceVariance float64) {
	colors := map[uint32]struct{}{}
	var sum, sumSquares float64
	bounds := img.Bounds()
	count := float64(bounds.Dx() * bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			colors[uint32(c.R)<<24|uint32(c.G)<<16|uint32(c.B)<<8|uint32(c.A)] = struct{}{}
			lum := 0.2126*float64(c.R)/255 + 0.7152*float64(c.G)/255 + 0.0722*float64(c.B)/255
			sum += lum
			sumSquares += lum * lum
		}
	}
	mean := sum / count
	variance := sumSquares/count - mean*mean
	if variance < 0 {
		variance = 0
	}
	return len(colors), variance
}

func cloneTestRGBA(src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}

// TestNativePostFXPassesKeepTheFrame is the regression guard for the erased
// frame. Every native screen-space effect and the whole chain must leave the
// rendered image recognisable, whether the backend shades that pass or copies
// it through.
//
// The "changes" column says whether this backend now runs the effect. An effect
// that runs must move the pixels, and an effect that copies must not. Both
// halves matter: a shaded pass that changes nothing is a dead implementation,
// and a copied pass that changes something means a stray write.
func TestNativePostFXPassesKeepTheFrame(t *testing.T) {
	baseline := renderPostFXProbe(t, postFXProbeScene())
	baseColors, baseVariance := frameEvidence(baseline)
	if baseColors < 8 || baseVariance < 0.002 {
		t.Fatalf("the probe scene itself draws nothing: %d colours, variance %.6f", baseColors, baseVariance)
	}

	cases := []struct {
		name    string
		changes bool
		effects []engine.RenderPostEffect
	}{
		// Bloom runs: the bright pass, both blurs and the compose add all
		// execute on the CPU now.
		{"bloom", true, []engine.RenderPostEffect{{Kind: "bloom", Threshold: 0.4, Intensity: 1.2, Radius: 6}}},
		// A tone-map effect with no mode resolves to ACES, which the baseline
		// frame already uses, so the frame does not move.
		{"tonemap", false, []engine.RenderPostEffect{{Kind: "tonemap"}}},
		{"vignette", true, []engine.RenderPostEffect{{Kind: "vignette", Intensity: 1}}},
		// A colour grade with no parameters resolves to exposure, contrast and
		// saturation all one, which is the identity.
		{"colorgrade", false, []engine.RenderPostEffect{{Kind: "colorgrade"}}},
		{"colorgrade-desaturate", true, []engine.RenderPostEffect{
			{Kind: "colorgrade", Params: map[string]float64{"saturation": 0.1}},
		}},
		// Ambient occlusion and depth of field read the depth attachment, which
		// this backend does not bind to a fragment stage. They still copy.
		{"ssao", false, []engine.RenderPostEffect{{Kind: "ssao", Radius: 4, Intensity: 1.2}}},
		{"dof", false, []engine.RenderPostEffect{{Kind: "dof"}}},
		{"chain", true, []engine.RenderPostEffect{
			{Kind: "bloom", Intensity: 1.2}, {Kind: "ssao"}, {Kind: "dof"},
			{Kind: "vignette", Intensity: 1}, {Kind: "colorgrade"}, {Kind: "tonemap"},
		}},
		// Two effects put the chain's scratch target back on the input side, so
		// this case covers the ping-pong direction the single cases never reach.
		{"pingpong", true, []engine.RenderPostEffect{{Kind: "vignette", Intensity: 1}, {Kind: "colorgrade"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderPostFXProbe(t, postFXProbeScene(tc.effects...))
			colors, variance := frameEvidence(got)
			if colors < 8 || variance < 0.002 {
				t.Fatalf("%s erased the frame: %d colours, variance %.6f; baseline had %d and %.6f",
					tc.name, colors, variance, baseColors, baseVariance)
			}
			equal := imagesEqual(got, baseline)
			if tc.changes && equal {
				t.Fatalf("%s runs on this backend but changed no pixel; the fragment stage is dead", tc.name)
			}
			if !tc.changes && !equal {
				t.Fatalf("%s changed the pixels; this backend does not shade that pass, so it must copy", tc.name)
			}
		})
	}
}

// postLabelProbeBindGroup binds one small texture at binding zero, which is the
// colour source of every fullscreen pass. postFragmentFor needs it: a pass with
// no source has nothing to read, so the function returns nil and the label
// routing check would prove nothing.
func postLabelProbeBindGroup(t *testing.T) *BindGroup {
	t.Helper()
	device, _ := New(2, 2)
	tex, err := device.CreateTexture(gpu.TextureDesc{Width: 2, Height: 2, Format: gpu.FormatRGBA8Unorm})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	return &BindGroup{desc: gpu.BindGroupDesc{
		Entries: []gpu.BindGroupEntry{{Binding: 0, TextureView: tex.CreateView()}},
	}}
}

// TestFullscreenCopyPassLabels pins the label routing between the three ways a
// draw can be served: shade it, copy it, or rasterize it.
func TestFullscreenCopyPassLabels(t *testing.T) {
	probe := postLabelProbeBindGroup(t)
	probeTarget := postTarget{w: 2, h: 2}
	// A pass this backend shades. Draw checks postFragmentFor before it checks
	// isFullscreenCopyPass, so these labels never reach the copy.
	shadedLabels := []string{
		"bundle.present", "bundle.present.compose",
		"bundle.postfx.vignette", "bundle.postfx.colorGrade",
		"bundle.bloom.bright", "bundle.bloom.blurH", "bundle.bloom.blurV",
	}
	for _, label := range shadedLabels {
		if postFragmentFor(label, probe, probeTarget) == nil && !isFullscreenCopyPass(label) {
			t.Errorf("label %q reaches neither the fragment stage nor the copy, so its target keeps only its clear colour", label)
		}
	}
	// A pass this backend cannot shade. It must copy, or the chain presents a
	// cleared target.
	copyLabels := []string{
		"bundle.fxaa311", "bundle.postfx.ssao", "bundle.postfx.dof",
		"bundle.postfx.custom:grain",
	}
	for _, label := range copyLabels {
		if !isFullscreenCopyPass(label) {
			t.Errorf("label %q must copy its source through, or the chain presents a cleared target", label)
		}
		if postFragmentFor(label, probe, probeTarget) != nil {
			t.Errorf("label %q has a fragment stage; move it out of the copy list and pin what it computes", label)
		}
	}
	// A geometry pass must keep reaching the rasterizer. Routing one of these to
	// the copy path would replace real triangles with a texture blit.
	rasterLabels := []string{
		"bundle.lit", "bundle.unlit", "bundle.shadow", "bundle.particles.render",
		"bundle.worldLine", "",
	}
	for _, label := range rasterLabels {
		if isFullscreenCopyPass(label) {
			t.Errorf("label %q must reach the rasterizer, not the copy path", label)
		}
		if postFragmentFor(label, probe, probeTarget) != nil {
			t.Errorf("label %q must reach the rasterizer, not a fullscreen fragment stage", label)
		}
	}
}

func imagesEqual(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}
