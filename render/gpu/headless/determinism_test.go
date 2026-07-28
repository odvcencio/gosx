package headless

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
)

// This file bounds the determinism claim for the CPU rasterizer. Read the bound
// before you quote the claim.
//
// What the tests below prove:
//
//   - Repeated renders in one process produce byte-identical pixels, on a fresh
//     device each time and on one reused renderer.
//   - A render in a fresh operating-system process produces the same pixels. This
//     is the check that catches a dependence on Go map iteration order, on a
//     pointer value, or on any per-process seed. A single-process repeat cannot
//     see those, because the order or the seed holds still inside one process.
//   - Concurrent renders of the same scene agree with the serial result. Under
//     the race detector this also proves no shared mutable state leaks between
//     renderers.
//
// What remains unproven, and cannot be proven from inside this package:
//
//   - Identical pixels on a second processor architecture. The rasterizer runs
//     float32 and float64 arithmetic. Go pins IEEE-754 semantics for a single
//     operation, but a compiler may still contract a multiply and an add into one
//     fused instruction on a target that offers one, and a fused result differs
//     from the two-step result in the last bit. One differing bit in a depth
//     comparison flips a pixel.
//   - Identical pixels on a different Go release. A change to the compiler's
//     instruction selection or to a math routine can move a last bit.
//
// Closing the first gap needs a committed golden frame checked on a second
// architecture in continuous integration, for example amd64 and arm64 running the
// same pinned hash. Nothing inside one architecture can substitute for that. Do
// not describe the CPU rasterizer as bit-reproducible across machines until that
// job exists and is green.

// determinismProbeEnv makes the test binary render one frame and print its hash
// instead of running the suite. The fresh-process check re-executes this binary
// with the variable set.
const determinismProbeEnv = "GOSX_HEADLESS_DETERMINISM_PROBE"

const (
	determinismWidth  = 96
	determinismHeight = 64
)

// determinismScene carries map-valued fields on purpose. Go randomizes map
// iteration order per process, so a lowering step that walked a map would produce
// different pixels in a fresh process and identical pixels in a warm one.
func determinismScene() engine.RenderBundle {
	transforms := []float64{
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -1.4, 0.4, 0, 1,
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1.4, 0.4, 0, 1,
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, -1.1, 0.8, 1,
	}
	return engine.RenderBundle{
		Background: "#050608",
		Camera:     engine.RenderCamera{Y: 1.2, Z: 6, FOV: 1, Near: 0.1, Far: 60},
		// Every environment term is authored, including the two hemisphere
		// intensities. resolveHemisphereAmbient substitutes an intensity of one
		// when either is unset, and that substitution is under dispute, so a
		// fixture must not depend on it.
		Environment: engine.RenderEnvironment{
			AmbientColor: "#404858", AmbientIntensity: 0.35,
			SkyColor: "#ccddff", SkyIntensity: 0.6,
			GroundColor: "#483c38", GroundIntensity: 0.4,
		},
		Materials: []engine.RenderMaterial{
			{Kind: "standard", Color: "#ff8060", Roughness: 0.5,
				CustomUniforms: map[string]any{"zeta": 1, "alpha": 2, "mu": 3, "kappa": 4, "omega": 5},
				ShaderLayout:   map[string]any{"material": "probe", "slot": 7}},
			{Kind: "standard", Color: "#60ff80", Roughness: 0.9},
		},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1.2,
			DirectionX: -0.4, DirectionY: -1, DirectionZ: -0.3, CastShadow: true,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{
			{
				ID: "ground", Kind: "plane", Width: 30, Height: 30,
				MaterialIndex: 1, InstanceCount: 1, ReceiveShadow: true,
				Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, -1.6, -4, 1},
			},
			{
				ID: "spheres", Kind: "sphere", Radius: 0.7, Segments: 20,
				MaterialIndex: 0, InstanceCount: 3, Transforms: transforms, CastShadow: true,
				Attributes: map[string][]float64{"gamma": {1, 2, 3}, "beta": {4, 5, 6}, "delta": {7, 8, 9}},
			},
		},
	}
}

// renderDeterminismFrame draws one frame on a fresh device and returns the
// framebuffer.
func renderDeterminismFrame() (*image.RGBA, error) {
	device, surface := New(determinismWidth, determinismHeight)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		return nil, err
	}
	defer renderer.Destroy()
	if err := renderer.Frame(determinismScene(), determinismWidth, determinismHeight, 0); err != nil {
		return nil, err
	}
	out := image.NewRGBA(device.Framebuffer().Bounds())
	copy(out.Pix, device.Framebuffer().Pix)
	return out, nil
}

// determinismHash hashes the dimensions and the raw RGBA bytes. It skips the PNG
// container, so a re-encode never looks like a visual change.
func determinismHash(img *image.RGBA) string {
	h := sha256.New()
	bounds := img.Bounds()
	fmt.Fprintf(h, "rgba/%d/%d\n", bounds.Dx(), bounds.Dy())
	stride := bounds.Dx() * 4
	for y := 0; y < bounds.Dy(); y++ {
		offset := img.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		h.Write(img.Pix[offset : offset+stride])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestMain serves the fresh-process probe. With the probe variable set, the
// binary renders one frame, prints its hash, and exits without running any test.
func TestMain(m *testing.M) {
	if os.Getenv(determinismProbeEnv) == "" {
		os.Exit(m.Run())
	}
	img, err := renderDeterminismFrame()
	if err != nil {
		fmt.Fprintln(os.Stderr, "determinism probe:", err)
		os.Exit(2)
	}
	colors, variance := frameEvidence(img)
	// Print the content evidence beside the hash. A child that rendered nothing
	// would otherwise hand back a stable hash of a blank frame.
	fmt.Printf("%s %d %.9f\n", determinismHash(img), colors, variance)
	os.Exit(0)
}

// TestFrameRepeatsInOneProcess proves the frame is reproducible inside one
// process, both on a fresh device and on one reused renderer. A reused renderer
// carries caches, so it is the case where leftover state would show.
func TestFrameRepeatsInOneProcess(t *testing.T) {
	first, err := renderDeterminismFrame()
	if err != nil {
		t.Fatal(err)
	}
	reference := determinismHash(first)
	colors, variance := frameEvidence(first)
	if colors < 20 || variance < 0.005 {
		t.Fatalf("the determinism scene drew nothing worth pinning: %d colours, variance %.6f",
			colors, variance)
	}
	t.Logf("reference %s: %d colours, luminance variance %.6f", reference[:16], colors, variance)

	for run := 2; run <= 8; run++ {
		img, err := renderDeterminismFrame()
		if err != nil {
			t.Fatal(err)
		}
		if got := determinismHash(img); got != reference {
			t.Fatalf("run %d on a fresh device produced %s, want %s", run, got, reference)
		}
	}

	device, surface := New(determinismWidth, determinismHeight)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Destroy()
	for run := 1; run <= 8; run++ {
		if err := renderer.Frame(determinismScene(), determinismWidth, determinismHeight, 0); err != nil {
			t.Fatal(err)
		}
		if got := determinismHash(device.Framebuffer()); got != reference {
			t.Fatalf("run %d on the reused renderer produced %s, want %s; "+
				"a cache or a target keeps state between frames", run, got, reference)
		}
	}
}

// TestFrameRepeatsInAFreshProcess is the check a single-process repeat cannot
// make. Go randomizes map iteration order per process, so a lowering step that
// depended on it would look reproducible inside one process and change here.
func TestFrameRepeatsInAFreshProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("the fresh-process check re-executes the test binary")
	}
	local, err := renderDeterminismFrame()
	if err != nil {
		t.Fatal(err)
	}
	reference := determinismHash(local)
	localColors, localVariance := frameEvidence(local)

	binary, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}
	for run := 1; run <= 3; run++ {
		command := exec.Command(binary)
		command.Env = append(os.Environ(), determinismProbeEnv+"=1")
		output, err := command.Output()
		if err != nil {
			t.Fatalf("child run %d: %v", run, err)
		}
		fields := strings.Fields(strings.TrimSpace(string(output)))
		if len(fields) != 3 {
			t.Fatalf("child run %d printed %q, want a hash, a colour count and a variance", run, output)
		}
		if fields[0] != reference {
			t.Fatalf("child run %d produced %s, this process produced %s; "+
				"something in the render path depends on per-process state such as map order",
				run, fields[0], reference)
		}
		// The child must also report real content. A child that failed to draw
		// would hand back a stable hash of a blank frame and look reproducible.
		if fields[1] == "1" {
			t.Fatalf("child run %d rendered a single-colour frame: %q", run, output)
		}
		t.Logf("child run %d agrees: %s (%s colours against %d local, variance %s against %.9f)",
			run, fields[0][:16], fields[1], localColors, fields[2], localVariance)
	}
}

// TestConcurrentRendersAgree renders the same scene from several goroutines at
// once. Every result must match the serial reference. Run the package with -race
// to make this also prove that two renderers share no mutable state.
func TestConcurrentRendersAgree(t *testing.T) {
	serial, err := renderDeterminismFrame()
	if err != nil {
		t.Fatal(err)
	}
	reference := determinismHash(serial)

	const workers = 8
	hashes := make([]string, workers)
	errs := make([]error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(index int) {
			defer group.Done()
			img, err := renderDeterminismFrame()
			if err != nil {
				errs[index] = err
				return
			}
			hashes[index] = determinismHash(img)
		}(worker)
	}
	group.Wait()
	for index := 0; index < workers; index++ {
		if errs[index] != nil {
			t.Fatalf("worker %d: %v", index, errs[index])
		}
		if hashes[index] != reference {
			t.Fatalf("worker %d produced %s, the serial render produced %s; "+
				"two renderers share mutable state", index, hashes[index], reference)
		}
	}
}

// TestDeterminismDetectsAPerturbedScene proves the hash is sensitive. A
// determinism check that compared a constant would pass forever, so move the
// light, change the material and shift a transform, and require a new hash each
// time.
func TestDeterminismDetectsAPerturbedScene(t *testing.T) {
	base, err := renderDeterminismFrame()
	if err != nil {
		t.Fatal(err)
	}
	reference := determinismHash(base)

	render := func(t *testing.T, mutate func(*engine.RenderBundle)) string {
		t.Helper()
		frame := determinismScene()
		mutate(&frame)
		device, surface := New(determinismWidth, determinismHeight)
		renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
		if err != nil {
			t.Fatal(err)
		}
		defer renderer.Destroy()
		if err := renderer.Frame(frame, determinismWidth, determinismHeight, 0); err != nil {
			t.Fatal(err)
		}
		return determinismHash(device.Framebuffer())
	}

	for _, tc := range []struct {
		name   string
		mutate func(*engine.RenderBundle)
	}{
		{"move the light", func(f *engine.RenderBundle) {
			f.Lights[0].DirectionX = 0.9
		}},
		{"dim the light", func(f *engine.RenderBundle) {
			f.Lights[0].Intensity = 0.4
		}},
		{"change the base colour", func(f *engine.RenderBundle) {
			f.Materials[0].Color = "#6080ff"
		}},
		{"shift one instance", func(f *engine.RenderBundle) {
			f.InstancedMeshes[1].Transforms[12] += 0.25
		}},
		{"scale one instance", func(f *engine.RenderBundle) {
			f.InstancedMeshes[1].Transforms[0] = 1.3
		}},
		{"rotate one instance", func(f *engine.RenderBundle) {
			// A quarter turn about X, in column-major order. The silhouette of a
			// sphere holds still and the surface normals turn, so only the shading
			// moves. That makes this the mutation that proves the hash reads a
			// normal and not just a silhouette.
			//
			// Do not turn about Y here. A UV sphere is a surface of revolution
			// about Y, and a quarter turn on a twenty-segment grid maps the mesh
			// onto itself exactly, so the frame stays byte-identical and the
			// mutation proves nothing.
			f.InstancedMeshes[1].Transforms[4] = 0
			f.InstancedMeshes[1].Transforms[6] = 1
			f.InstancedMeshes[1].Transforms[9] = -1
			f.InstancedMeshes[1].Transforms[10] = 0
		}},
		{"stop casting shadows", func(f *engine.RenderBundle) {
			f.InstancedMeshes[1].CastShadow = false
		}},
		{"move the camera", func(f *engine.RenderBundle) {
			f.Camera.Z = 6.25
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(t, tc.mutate); got == reference {
				t.Fatalf("%s left the frame byte-identical (%s); the pinned hash is inert on this axis",
					tc.name, got)
			}
		})
	}
}
