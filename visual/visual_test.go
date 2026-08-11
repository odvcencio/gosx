package visual

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// solidPNG returns a PNG of the given size with a solid fill.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestDiffIdentical(t *testing.T) {
	red := solidPNG(t, 100, 100, color.RGBA{R: 255, A: 255})
	result, err := Diff(red, red)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !result.DimensionsMatch {
		t.Errorf("DimensionsMatch = false, want true")
	}
	if result.Mismatched != 0 {
		t.Errorf("Mismatched = %d, want 0", result.Mismatched)
	}
	if result.DiffPct != 0 {
		t.Errorf("DiffPct = %f, want 0", result.DiffPct)
	}
}

func TestDiffDifferent(t *testing.T) {
	red := solidPNG(t, 100, 100, color.RGBA{R: 255, A: 255})
	blue := solidPNG(t, 100, 100, color.RGBA{B: 255, A: 255})
	result, err := Diff(red, blue)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !result.DimensionsMatch {
		t.Errorf("DimensionsMatch = false, want true")
	}
	if result.Mismatched != 100*100 {
		t.Errorf("Mismatched = %d, want %d", result.Mismatched, 100*100)
	}
	if result.DiffPct != 100 {
		t.Errorf("DiffPct = %f, want 100", result.DiffPct)
	}
}

func TestDiffDimensionMismatch(t *testing.T) {
	small := solidPNG(t, 100, 100, color.RGBA{A: 255})
	large := solidPNG(t, 200, 100, color.RGBA{A: 255})
	result, err := Diff(small, large)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if result.DimensionsMatch {
		t.Errorf("DimensionsMatch = true, want false")
	}
	if result.DiffPct != 100 {
		t.Errorf("DiffPct = %f, want 100", result.DiffPct)
	}
}

func TestAssertUpdateCreatesBaseline(t *testing.T) {
	oldCapture := captureForAssert
	captureForAssert = func(ctx context.Context, url string, opts CaptureOptions) ([]byte, error) {
		if url != "https://example.test/update" {
			t.Fatalf("url = %q, want update URL", url)
		}
		if opts.Viewport.Width != 640 || opts.Viewport.Height != 360 {
			t.Fatalf("viewport = %#v, want 640x360", opts.Viewport)
		}
		return solidPNG(t, 8, 8, color.RGBA{G: 255, A: 255}), nil
	}
	t.Cleanup(func() { captureForAssert = oldCapture })

	baseline := filepath.Join(t.TempDir(), "baseline.png")
	err := Assert(context.Background(), "https://example.test/update", AssertOptions{
		BaselinePath: baseline,
		Update:       true,
		CaptureOptions: CaptureOptions{
			Viewport: Viewport{Width: 640, Height: 360, Scale: 1},
		},
	})
	if err != nil {
		t.Fatalf("Assert update: %v", err)
	}

	data, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	result, err := Diff(data, solidPNG(t, 8, 8, color.RGBA{G: 255, A: 255}))
	if err != nil {
		t.Fatalf("Diff written baseline: %v", err)
	}
	if result.Mismatched != 0 {
		t.Fatalf("written baseline mismatched = %d, want 0", result.Mismatched)
	}
}

func TestAssertWritesDiffAndCurrentOnMismatch(t *testing.T) {
	oldCapture := captureForAssert
	captureForAssert = func(context.Context, string, CaptureOptions) ([]byte, error) {
		return solidPNG(t, 6, 6, color.RGBA{B: 255, A: 255}), nil
	}
	t.Cleanup(func() { captureForAssert = oldCapture })

	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.png")
	diffPath := filepath.Join(dir, "baseline.diff.png")
	if err := os.WriteFile(baseline, solidPNG(t, 6, 6, color.RGBA{R: 255, A: 255}), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	err := Assert(context.Background(), "https://example.test/mismatch", AssertOptions{
		BaselinePath: baseline,
		DiffOutPath:  diffPath,
		Threshold:    0,
	})
	var mismatch *AssertMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("Assert error = %v, want *AssertMismatch", err)
	}
	if mismatch.Result.DiffPct != 100 {
		t.Fatalf("DiffPct = %f, want 100", mismatch.Result.DiffPct)
	}
	if _, err := os.Stat(diffPath); err != nil {
		t.Fatalf("stat diff: %v", err)
	}
	currentPath := filepath.Join(dir, "baseline.current.png")
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("stat current: %v", err)
	}
}

func TestValidRequireBackend(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", true},
		{"webgpu", true},
		{"webgl", true},
		{"any-gpu", true},
		{"canvas2d", false},
		{"WebGPU", false},
		{"bogus", false},
	} {
		if got := ValidRequireBackend(tc.value); got != tc.want {
			t.Errorf("ValidRequireBackend(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestCheckBackendRequirementNoneSkipsCheck(t *testing.T) {
	err := checkBackendRequirement("https://example.test/", RequireBackendNone, nil)
	if err != nil {
		t.Fatalf("checkBackendRequirement(RequireBackendNone) = %v, want nil", err)
	}
	err = checkBackendRequirement("https://example.test/", RequireBackendNone, []sceneMountBackend{{Backend: "canvas"}})
	if err != nil {
		t.Fatalf("checkBackendRequirement(RequireBackendNone) with mounts = %v, want nil", err)
	}
}

func TestCheckBackendRequirementNoMountFound(t *testing.T) {
	err := checkBackendRequirement("https://example.test/", RequireBackendWebGPU, nil)
	var reqErr *BackendRequirementError
	if !errors.As(err, &reqErr) {
		t.Fatalf("checkBackendRequirement error = %v, want *BackendRequirementError", err)
	}
	if len(reqErr.Mounts) != 0 {
		t.Fatalf("Mounts = %v, want empty", reqErr.Mounts)
	}
	if !bytes.Contains([]byte(reqErr.Error()), []byte("No Scene3D mount was found")) {
		t.Fatalf("Error() = %q, want the no-mount explanation", reqErr.Error())
	}
}

func TestCheckBackendRequirementCanvasFallbackFails(t *testing.T) {
	mounts := []sceneMountBackend{{ID: "scene-mount", Backend: "canvas", Renderer: "canvas", FallbackReason: "webgl-unavailable"}}
	err := checkBackendRequirement("https://example.test/", RequireBackendWebGPU, mounts)
	var reqErr *BackendRequirementError
	if !errors.As(err, &reqErr) {
		t.Fatalf("checkBackendRequirement error = %v, want *BackendRequirementError", err)
	}
	if len(reqErr.Mounts) != 1 || reqErr.Mounts[0].Backend != "canvas" {
		t.Fatalf("Mounts = %v, want the failing canvas mount", reqErr.Mounts)
	}
	msg := reqErr.Error()
	for _, want := range []string{"Canvas2D fallback", "use-gl=angle", "use-angle=swiftshader", "CHROME_WS_URL"} {
		if !bytes.Contains([]byte(msg), []byte(want)) {
			t.Errorf("Error() missing %q in:\n%s", want, msg)
		}
	}
}

func TestCheckBackendRequirementWrongGPUFamilyFails(t *testing.T) {
	mounts := []sceneMountBackend{{ID: "scene-mount", Backend: "webgl", Renderer: "webgl", FallbackReason: "webgpu-unavailable"}}
	err := checkBackendRequirement("https://example.test/", RequireBackendWebGPU, mounts)
	var reqErr *BackendRequirementError
	if !errors.As(err, &reqErr) {
		t.Fatalf("checkBackendRequirement error = %v, want *BackendRequirementError", err)
	}
	msg := reqErr.Error()
	if !bytes.Contains([]byte(msg), []byte("any-gpu instead")) {
		t.Errorf("Error() missing wrong-GPU-family guidance in:\n%s", msg)
	}
	if bytes.Contains([]byte(msg), []byte("Canvas2D fallback renderer, or")) {
		t.Errorf("Error() should not claim no GPU ran when the mount reached webgl:\n%s", msg)
	}
}

func TestCheckBackendRequirementAnyGPUAcceptsWebGLOrWebGPU(t *testing.T) {
	for _, backend := range []string{"webgl", "webgpu"} {
		mounts := []sceneMountBackend{{ID: "scene-mount", Backend: backend}}
		if err := checkBackendRequirement("https://example.test/", RequireBackendAnyGPU, mounts); err != nil {
			t.Errorf("checkBackendRequirement(any-gpu, %s) = %v, want nil", backend, err)
		}
	}
	mounts := []sceneMountBackend{{ID: "scene-mount", Backend: "canvas"}}
	if err := checkBackendRequirement("https://example.test/", RequireBackendAnyGPU, mounts); err == nil {
		t.Fatalf("checkBackendRequirement(any-gpu, canvas) = nil, want error")
	}
}

func TestCaptureEnforcesRequireBackend(t *testing.T) {
	oldCapture := captureForAssert
	t.Cleanup(func() { captureForAssert = oldCapture })

	// captureForAssert is a seam over Assert, not Capture itself, so this
	// test exercises checkBackendRequirement wiring through the same
	// error-propagation path Assert uses, via a fake that mimics Capture's
	// post-screenshot check.
	captureForAssert = func(ctx context.Context, url string, opts CaptureOptions) ([]byte, error) {
		mounts := []sceneMountBackend{{ID: "scene-mount", Backend: "canvas"}}
		if err := checkBackendRequirement(url, opts.RequireBackend, mounts); err != nil {
			return nil, err
		}
		return solidPNG(t, 4, 4, color.RGBA{A: 255}), nil
	}

	err := Assert(context.Background(), "https://example.test/scene", AssertOptions{
		Update: true,
		CaptureOptions: CaptureOptions{
			RequireBackend: RequireBackendWebGPU,
		},
		BaselinePath: filepath.Join(t.TempDir(), "baseline.png"),
	})
	var reqErr *BackendRequirementError
	if !errors.As(err, &reqErr) {
		t.Fatalf("Assert error = %v, want *BackendRequirementError", err)
	}
}

func TestDefaultBaselinePath(t *testing.T) {
	got := DefaultBaselinePath("https://example.com/")
	want := filepath.Join("testdata", "visual")
	if !bytes.Contains([]byte(got), []byte(want)) {
		t.Errorf("DefaultBaselinePath = %q, want prefix %q", got, want)
	}
	if !bytes.HasSuffix([]byte(got), []byte(".png")) {
		t.Errorf("DefaultBaselinePath = %q, want .png suffix", got)
	}
}

func TestOuroborosPixelRejectsBlankImages(t *testing.T) {
	black := decodePNGForTest(t, solidPNG(t, 32, 32, color.RGBA{A: 255}))
	if !ImageBlankOrPlaceholder(black) {
		t.Fatalf("black image was accepted as nonblank")
	}

	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: 80, A: 255})
		}
	}
	if ImageBlankOrPlaceholder(img) {
		t.Fatalf("varied image was rejected as blank")
	}
}

func TestOuroborosPixelDefaultsUseSceneOwnedCanvasSelector(t *testing.T) {
	opts := PixelEvidenceOptions{}
	opts.applyDefaults()

	if opts.CanvasSelector != DefaultPixelCanvasSelector {
		t.Fatalf("CanvasSelector = %q, want %q", opts.CanvasSelector, DefaultPixelCanvasSelector)
	}
	if !strings.Contains(opts.CanvasSelector, "data-gosx-scene3d-canvas") {
		t.Fatalf("default canvas selector is not Scene3D-owned: %q", opts.CanvasSelector)
	}
}

func TestOuroborosPixelComparisonWritesDiffOnFailure(t *testing.T) {
	dir := t.TempDir()
	baseline := solidPNG(t, 8, 8, color.RGBA{R: 255, A: 255})
	current := solidPNG(t, 8, 8, color.RGBA{B: 255, A: 255})
	currentPath := filepath.Join(dir, "R08-settled-01.png")
	if err := os.WriteFile(currentPath, current, 0o644); err != nil {
		t.Fatalf("write current: %v", err)
	}

	comparison, err := ComparePixelEvidence(baseline, current, filepath.Join(dir, "R08-settled-00.png"), currentPath, 0)
	if err != nil {
		t.Fatalf("ComparePixelEvidence: %v", err)
	}
	if comparison.Passed {
		t.Fatalf("comparison passed, want threshold failure")
	}
	if comparison.DiffPath == "" {
		t.Fatalf("DiffPath is empty")
	}
	if _, err := os.Stat(comparison.DiffPath); err != nil {
		t.Fatalf("stat diff: %v", err)
	}
	if comparison.Similarity >= 1 {
		t.Fatalf("Similarity = %f, want less than 1", comparison.Similarity)
	}
}

func TestOuroborosPixelComparisonReadOnlyDoesNotWriteDiff(t *testing.T) {
	dir := t.TempDir()
	baseline := solidPNG(t, 8, 8, color.RGBA{R: 255, A: 255})
	current := solidPNG(t, 8, 8, color.RGBA{B: 255, A: 255})
	currentPath := filepath.Join(dir, "R08-settled-01.png")
	if err := os.WriteFile(currentPath, current, 0o644); err != nil {
		t.Fatalf("write current: %v", err)
	}

	comparison, err := ComparePixelEvidenceWithThresholdsReadOnly(baseline, current, filepath.Join(dir, "R08-settled-00.png"), currentPath, 0, 0)
	if err != nil {
		t.Fatalf("ComparePixelEvidenceWithThresholdsReadOnly: %v", err)
	}
	if comparison.Passed {
		t.Fatalf("comparison passed, want threshold failure")
	}
	if comparison.DiffPath == "" {
		t.Fatalf("DiffPath is empty")
	}
	if _, err := os.Stat(comparison.DiffPath); !os.IsNotExist(err) {
		t.Fatalf("readonly comparison wrote diff or returned wrong stat error: %v", err)
	}
}

func TestOuroborosHardwareClassification(t *testing.T) {
	if got := classifyHardware("webgpu", false, true, true); got != "hardware-webgpu" {
		t.Fatalf("webgpu hardware class = %q", got)
	}
	if got := classifyHardware("webgl", true, false, false); got != "software-raster" {
		t.Fatalf("software class = %q", got)
	}
	if !isSoftwareRaster("ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device))") {
		t.Fatalf("SwiftShader marker was not detected")
	}
}

func TestOuroborosRecordBaselineRequiresHardwareAndSamples(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: t.TempDir(), Backend: RequireBackendWebGL, Samples: 2, Source: testPixelSource()}
	opts.applyDefaults()
	if err := validatePixelOptions(opts); err == nil {
		t.Fatalf("validatePixelOptions accepted record-baseline with fewer than 3 samples")
	}

	opts.Samples = 3
	manifest := PixelEvidenceManifest{
		HardwareClassification: "software-raster",
		States: []PixelStateEvidence{
			{State: "initial", Captures: []PixelCaptureEvidence{{Comparison: &PixelComparison{Passed: true}}}},
			{State: "settled", Captures: []PixelCaptureEvidence{{Comparison: &PixelComparison{Passed: true}}}},
		},
	}
	failures := validateManifestCertification(opts, manifest)
	if len(failures) == 0 {
		t.Fatalf("software-raster manifest was certified")
	}
}

func TestOuroborosPixelOptionsRequireBackendAndBaseline(t *testing.T) {
	record := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: t.TempDir(), Backend: RequireBackendNone, Samples: 3, Source: testPixelSource()}
	record.applyDefaults()
	if err := validatePixelOptions(record); err == nil {
		t.Fatalf("record-baseline accepted missing backend")
	}
	candidate := PixelEvidenceOptions{Mode: PixelModeCandidateComparison, ArtifactRoot: t.TempDir(), Backend: RequireBackendWebGL, Samples: 1, Source: testPixelSource()}
	candidate.applyDefaults()
	if err := validatePixelOptions(candidate); err == nil {
		t.Fatalf("candidate accepted missing baseline root")
	}
	forced := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: t.TempDir(), Backend: RequireBackendWebGPU, Samples: 3, ForceWebGL: true, Source: testPixelSource()}
	forced.applyDefaults()
	if err := validatePixelOptions(forced); err == nil {
		t.Fatalf("accepted force-webgl with WebGPU backend")
	}
	negativeWarmup := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: t.TempDir(), Backend: RequireBackendWebGL, Samples: 3, WarmupFrames: -1, Source: testPixelSource()}
	negativeWarmup.applyDefaults()
	if err := validatePixelOptions(negativeWarmup); err == nil {
		t.Fatalf("accepted negative warmup frames")
	}
}

func TestOuroborosPixelOptionsRequireSourceIdentity(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: t.TempDir(), Backend: RequireBackendWebGL, Samples: 3}
	opts.applyDefaults()
	if err := validatePixelOptions(opts); err == nil {
		t.Fatalf("validatePixelOptions accepted missing source identity")
	}
	for _, tc := range []struct {
		name   string
		source PixelSourceIdentity
	}{
		{name: "malformed base revision", source: PixelSourceIdentity{BaseRevision: "ABC1234", OverlayHash: "sha256:clean", InventorySHA256: strings.Repeat("a", 71)}},
		{name: "malformed overlay hash", source: PixelSourceIdentity{BaseRevision: "abc1234", OverlayHash: "clean", InventorySHA256: "sha256:" + strings.Repeat("a", 64)}},
		{name: "malformed inventory hash", source: PixelSourceIdentity{BaseRevision: "abc1234", OverlayHash: "sha256:clean", InventorySHA256: strings.Repeat("a", 64)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts.Source = tc.source
			if err := validatePixelOptions(opts); err == nil {
				t.Fatalf("validatePixelOptions accepted %s", tc.name)
			}
		})
	}
	opts.Source = testPixelSource()
	if err := validatePixelOptions(opts); err != nil {
		t.Fatalf("validatePixelOptions rejected valid source identity: %v", err)
	}
}

func TestOuroborosBackendSelectionScript(t *testing.T) {
	webgl := PixelEvidenceOptions{Backend: RequireBackendWebGL, ForceWebGL: true}
	if got := pixelBackendSelectionScript(webgl); got != `window.__gosx_scene3d_force_webgl = true;` {
		t.Fatalf("webgl script = %q", got)
	}
	if got := pixelBackendSelectionHookName(webgl); got == "" {
		t.Fatalf("webgl hook name is empty")
	}
	webgpu := PixelEvidenceOptions{Backend: RequireBackendWebGPU}
	got := pixelBackendSelectionScript(webgpu)
	if got == "" || strings.Contains(got, "= true") {
		t.Fatalf("webgpu clear script = %q", got)
	}
	if got := pixelBackendSelectionScript(PixelEvidenceOptions{Backend: RequireBackendWebGL}); got != "" {
		t.Fatalf("webgl without opt-in script = %q", got)
	}
}

func TestOuroborosObservedBackendMismatchFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	if err := os.WriteFile(path, variedPNG(t, 8, 8, 0), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	failures := validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGL}, PixelCaptureEvidence{
		Path:               path,
		SHA256:             strings.Repeat("a", 64),
		Backend:            "webgpu",
		RuntimeTruthParsed: true,
		RuntimeGPU:         true,
		Implementation:     "dawn",
		HardwareClass:      "hardware-webgpu",
		FrameSeq:           1,
	})
	if !containsFailure(failures, "want webgl") {
		t.Fatalf("failures = %v, want backend mismatch", failures)
	}
}

func TestActivateVisualTargetRequiresLiveTarget(t *testing.T) {
	if err := activateVisualTarget(context.Background()); err == nil || !strings.Contains(err.Error(), "missing chromedp context") {
		t.Fatalf("activateVisualTarget without chromedp context = %v", err)
	}

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	if err := activateVisualTarget(ctx); err == nil || !strings.Contains(err.Error(), "missing browser") {
		t.Fatalf("activateVisualTarget without browser = %v", err)
	}
}

func TestVisualActivationPrecedesViewportPreloadAndNavigation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		fn         string
		mustFollow []string
	}{
		{
			name: "capture",
			path: "visual.go",
			fn:   "func Capture(",
			mustFollow: []string{
				"chromedp.EmulateViewport",
				"chromedp.Navigate",
			},
		},
		{
			name: "pixel",
			path: "ouroboros_pixel.go",
			fn:   "func navigatePixelPage(",
			mustFollow: []string{
				"chromedp.EmulateViewport",
				"page.AddScriptToEvaluateOnNewDocument",
				"chromedp.Navigate",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}
			body := string(data)
			fnStart := strings.Index(body, tc.fn)
			if fnStart < 0 {
				t.Fatalf("%s missing %q", tc.path, tc.fn)
			}
			body = body[fnStart:]
			activate := strings.Index(body, "activateVisualTargetAction()")
			if activate < 0 {
				t.Fatalf("%s missing activateVisualTargetAction in %s", tc.path, tc.fn)
			}
			for _, marker := range tc.mustFollow {
				next := strings.Index(body, marker)
				if next < 0 {
					t.Fatalf("%s missing %q after %s", tc.path, marker, tc.fn)
				}
				if activate > next {
					t.Fatalf("%s activates after %s in %s", tc.path, marker, tc.fn)
				}
			}
		})
	}
}

func TestOuroborosPixelSelectedMetadataValidation(t *testing.T) {
	opts := PixelEvidenceOptions{Backend: RequireBackendWebGL, CanvasSelector: "canvas"}
	for _, tc := range []struct {
		name string
		meta pagePixelMetadata
	}{
		{name: "zero canvases", meta: pagePixelMetadata{Selected: SelectedSceneEvidence{CanvasCount: 0}}},
		{name: "multiple canvases", meta: pagePixelMetadata{Selected: SelectedSceneEvidence{CanvasCount: 2}}},
		{name: "no mount", meta: pagePixelMetadata{Selected: SelectedSceneEvidence{CanvasCount: 1, MountCount: 0}}},
		{name: "wrong backend", meta: pagePixelMetadata{Selected: SelectedSceneEvidence{CanvasCount: 1, MountCount: 1, MountID: "m"}, Mount: sceneMountBackend{Backend: "webgpu"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSelectedMeta(opts, tc.meta); err == nil {
				t.Fatalf("validateSelectedMeta accepted %s", tc.name)
			}
		})
	}
	valid := pagePixelMetadata{
		Selected:   SelectedSceneEvidence{CanvasCount: 1, MountCount: 1, MountID: "m"},
		Mount:      sceneMountBackend{Backend: "webgl"},
		Truth:      sceneTruthEvidence{Parsed: true, Backend: "webgl", GPU: true, Implementation: "webgl2", AdapterInfo: map[string]interface{}{"vendor": "NVIDIA Corporation", "description": "ANGLE RTX"}},
		RenderLoop: validRenderLoop("active", "runtime-program", true),
	}
	if err := validateSelectedMeta(opts, valid); err != nil {
		t.Fatalf("validateSelectedMeta valid = %v", err)
	}
	noWebGLIdentity := valid
	noWebGLIdentity.Truth.AdapterInfo = nil
	if err := validateSelectedMeta(opts, noWebGLIdentity); err == nil || !strings.Contains(err.Error(), "WebGL backend truth has no adapter identity") {
		t.Fatalf("validateSelectedMeta missing WebGL adapter identity error = %v", err)
	}
	malformed := valid
	malformed.RenderLoop = RenderLoopEvidence{State: "stopped", Active: true, WantsAnimation: false, StateParsed: true, WantsAnimationParsed: true, Reason: "static", Valid: true}
	if err := validateSelectedMeta(opts, malformed); err == nil {
		t.Fatalf("validateSelectedMeta accepted contradictory render-loop telemetry")
	}
}

func TestOuroborosForgedAttributesWithoutRuntimeTruthFail(t *testing.T) {
	opts := PixelEvidenceOptions{Backend: RequireBackendWebGPU, CanvasSelector: "canvas"}
	meta := pagePixelMetadata{
		Selected: SelectedSceneEvidence{CanvasCount: 1, MountCount: 1, MountID: "m"},
		Mount:    sceneMountBackend{Backend: "webgpu", Renderer: "webgpu"},
		Truth:    sceneTruthEvidence{Backend: "webgpu", GPU: true, Implementation: "dawn"},
	}
	if err := validateSelectedMeta(opts, meta); err == nil {
		t.Fatalf("validateSelectedMeta accepted forged backend attributes without parsed truth")
	}
	webglOpts := PixelEvidenceOptions{Backend: RequireBackendWebGL, CanvasSelector: "canvas"}
	webglMeta := pagePixelMetadata{
		Selected: SelectedSceneEvidence{CanvasCount: 1, MountCount: 1, MountID: "m"},
		Mount:    sceneMountBackend{Backend: "webgl", Renderer: "webgl"},
		Truth:    sceneTruthEvidence{Backend: "webgl", GPU: true, Implementation: "webgl2"},
	}
	if err := validateSelectedMeta(webglOpts, webglMeta); err == nil {
		t.Fatalf("validateSelectedMeta accepted forged WebGL attributes without parsed truth")
	}
	capture := PixelCaptureEvidence{
		Path:           filepath.Join(t.TempDir(), "capture.png"),
		SHA256:         strings.Repeat("a", 64),
		Backend:        "webgpu",
		RuntimeGPU:     true,
		Implementation: "dawn",
		HardwareClass:  "hardware-webgpu",
		FrameSeq:       1,
	}
	if err := os.WriteFile(capture.Path, solidPNG(t, 4, 4, color.RGBA{R: 1, G: 2, B: 3, A: 255}), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	failures := validateCapture(opts, capture)
	if !containsFailure(failures, "no parsed GoSX backend truth") {
		t.Fatalf("failures = %v, want parsed truth failure", failures)
	}
}

func TestOuroborosNavigatorGPUAndFakeWebGPUAttrsDoNotCertify(t *testing.T) {
	if got := classifyHardware("webgpu", false, true, false); got != "headless-logic" {
		t.Fatalf("classifyHardware with navigator GPU only = %q, want headless-logic", got)
	}
}

func TestOuroborosWebGPUCertificationRequiresAdapterIdentity(t *testing.T) {
	opts := PixelEvidenceOptions{Backend: RequireBackendWebGPU, CanvasSelector: "canvas"}
	meta := pagePixelMetadata{
		Selected:   SelectedSceneEvidence{CanvasCount: 1, MountCount: 1, MountID: "m"},
		Mount:      sceneMountBackend{Backend: "webgpu", Renderer: "webgpu"},
		Truth:      sceneTruthEvidence{Parsed: true, Backend: "webgpu", GPU: true, Implementation: "dawn"},
		RenderLoop: validRenderLoop("active", "runtime-program", true),
	}
	if err := validateSelectedMeta(opts, meta); err == nil || !strings.Contains(err.Error(), "adapter identity") {
		t.Fatalf("validateSelectedMeta missing adapter identity error = %v", err)
	}
	meta.Truth.AdapterInfo = map[string]interface{}{"vendor": "nvidia"}
	if err := validateSelectedMeta(opts, meta); err != nil {
		t.Fatalf("validateSelectedMeta valid adapter identity = %v", err)
	}

	capture := PixelCaptureEvidence{
		Path:               filepath.Join(t.TempDir(), "capture.png"),
		SHA256:             strings.Repeat("a", 64),
		Backend:            "webgpu",
		RuntimeTruthParsed: true,
		RuntimeGPU:         true,
		Implementation:     "dawn",
		HardwareClass:      "hardware-webgpu",
		FrameSeq:           1,
		RenderLoop:         validRenderLoop("active", "runtime-program", true),
	}
	if err := os.WriteFile(capture.Path, solidPNG(t, 4, 4, color.RGBA{R: 1, G: 2, B: 3, A: 255}), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	failures := validateCapture(opts, capture)
	if !containsFailure(failures, "no adapter identity") {
		t.Fatalf("failures = %v, want adapter identity failure", failures)
	}
	capture.WebGPU.Vendor = "nvidia"
	failures = validateCapture(opts, capture)
	if containsFailure(failures, "adapter identity") {
		t.Fatalf("failures = %v, want adapter identity accepted", failures)
	}
}

func TestOuroborosWebGPUFallbackAdapterFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	if err := os.WriteFile(path, solidPNG(t, 4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255}), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	failures := validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGPU}, PixelCaptureEvidence{
		Path:               path,
		SHA256:             strings.Repeat("a", 64),
		Backend:            "webgpu",
		RuntimeTruthParsed: true,
		RuntimeGPU:         true,
		Implementation:     "dawn",
		HardwareClass:      "hardware-webgpu",
		FrameSeq:           1,
		WebGPU:             WebGPUEvidence{Fallback: true},
	})
	if !containsFailure(failures, "fallback adapter") {
		t.Fatalf("failures = %v, want fallback adapter failure", failures)
	}
}

func TestOuroborosWebGLRuntimeGPUFalseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	if err := os.WriteFile(path, solidPNG(t, 4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255}), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	failures := validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGL}, PixelCaptureEvidence{
		Path:               path,
		SHA256:             strings.Repeat("a", 64),
		Backend:            "webgl",
		Renderer:           "webgl",
		RuntimeTruthParsed: true,
		RuntimeGPU:         false,
		Implementation:     "angle-webgl",
		HardwareClass:      "hardware-webgl",
		FrameSeq:           1,
	})
	if !containsFailure(failures, "gpu=true") {
		t.Fatalf("failures = %v, want gpu=false failure", failures)
	}
}

func TestOuroborosWebGLTruthSoftwareAndHardwareCertification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	png := solidPNG(t, 4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	base := PixelCaptureEvidence{
		Path:               path,
		SHA256:             strings.Repeat("a", 64),
		Backend:            "webgl",
		Bytes:              len(png),
		Width:              4,
		Height:             4,
		RuntimeTruthParsed: true,
		RuntimeGPU:         true,
		Implementation:     "webgl2",
		HardwareClass:      "hardware-webgl",
		FrameSeq:           1,
		BatchID:            "unit-batch",
		RenderLoop:         validRenderLoop("active", "runtime-program", true),
		WebGL:              WebGLEvidence{Vendor: "NVIDIA Corporation", Renderer: "ANGLE (NVIDIA GeForce RTX 4090 Direct3D11 vs_5_0 ps_5_0)", Version: "webgl2"},
	}
	failures := validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGL}, base)
	if len(failures) != 0 {
		t.Fatalf("hardware WebGL truth failures = %v", failures)
	}
	for _, renderer := range []string{
		"ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device))",
		"llvmpipe (LLVM 17.0.0, 256 bits)",
	} {
		software := base
		software.WebGL = WebGLEvidence{Vendor: "Google Inc.", Renderer: renderer, Version: "webgl2"}
		software.SoftwareRaster = isSoftwareRaster(software.WebGL.Vendor, software.WebGL.Renderer)
		software.HardwareClass = classifyHardware("webgl", software.SoftwareRaster, false, true)
		failures = validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGL}, software)
		if !containsFailure(failures, "not real hardware") {
			t.Fatalf("software WebGL truth %q failures = %v, want hardware rejection", renderer, failures)
		}
	}
	noIdentity := base
	noIdentity.WebGL = WebGLEvidence{Version: "webgl2"}
	failures = validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGL}, noIdentity)
	if !containsFailure(failures, "WebGL evidence has no adapter identity") {
		t.Fatalf("missing WebGL identity failures = %v", failures)
	}
}

func TestOuroborosShaderDiagnosticsErrorsFail(t *testing.T) {
	if !rendererFailure(sceneTruthEvidence{ShaderDiagnostics: ShaderDiagnosticsEvidence{Errors: 1}}) {
		t.Fatalf("rendererFailure ignored shader diagnostics errors")
	}
	path := filepath.Join(t.TempDir(), "capture.png")
	if err := os.WriteFile(path, solidPNG(t, 4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255}), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	failures := validateCapture(PixelEvidenceOptions{Backend: RequireBackendWebGL}, PixelCaptureEvidence{
		Path:               path,
		SHA256:             strings.Repeat("a", 64),
		Backend:            "webgl",
		RuntimeTruthParsed: true,
		RuntimeGPU:         true,
		Implementation:     "angle-webgl",
		HardwareClass:      "hardware-webgl",
		FrameSeq:           1,
		ShaderDiagnostics:  ShaderDiagnosticsEvidence{Errors: 1},
	})
	if !containsFailure(failures, "shader diagnostics") {
		t.Fatalf("failures = %v, want shader diagnostics failure", failures)
	}
}

func TestOuroborosCaptureValidationRejectsDeviceLossAndFailure(t *testing.T) {
	opts := PixelEvidenceOptions{Backend: RequireBackendWebGPU}
	capture := PixelCaptureEvidence{
		Path:            "capture.png",
		SHA256:          strings.Repeat("a", 64),
		Backend:         "webgpu",
		HardwareClass:   "hardware-webgpu",
		FrameSeq:        1,
		DeviceLost:      true,
		RendererFailure: true,
	}
	failures := validateCapture(opts, capture)
	if len(failures) < 2 {
		t.Fatalf("validateCapture failures = %v, want device loss and renderer failure", failures)
	}
}

func TestOuroborosRecordRootOverwriteRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	opts := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: dir}
	if err := preparePixelArtifactRoot(opts); err == nil {
		t.Fatalf("preparePixelArtifactRoot accepted non-empty record root")
	}
	opts.AllowOverwrite = true
	if err := preparePixelArtifactRoot(opts); err != nil {
		t.Fatalf("preparePixelArtifactRoot AllowOverwrite = %v", err)
	}
}

func TestOuroborosPixelInvalidPNG(t *testing.T) {
	if _, err := ComparePixelEvidence([]byte("not png"), []byte("also not png"), "base.png", "current.png", 0); err == nil {
		t.Fatalf("ComparePixelEvidence accepted invalid PNG data")
	}
}

func TestDiffRejectsHugePNGConfigBeforeDecode(t *testing.T) {
	huge := pngWithIHDRForTest(t, MaxPixelImageDimension+1, 1)
	small := variedPNG(t, 8, 8, 0)
	if _, err := Diff(huge, small); err == nil {
		t.Fatalf("Diff accepted huge PNG config")
	}
}

func TestDiffRejectsTruncatedPNGAfterConfig(t *testing.T) {
	truncated := pngWithIHDRForTest(t, 8, 8)
	small := variedPNG(t, 8, 8, 0)
	if _, err := Diff(truncated, small); err == nil {
		t.Fatalf("Diff accepted truncated PNG")
	}
}

func TestOuroborosCandidateLoadsBaselineAndWritesDiff(t *testing.T) {
	dir := t.TempDir()
	baselineRoot := filepath.Join(dir, "baseline")
	outRoot := filepath.Join(dir, "candidate")
	writeValidPixelBaseline(t, baselineRoot, nil)
	if got, err := loadBaselineThreshold(baselineRoot); err != nil || got != 0.5 {
		t.Fatalf("loadBaselineThreshold = %v, %v; want 0.5, nil", got, err)
	}
	opts := PixelEvidenceOptions{Mode: PixelModeCandidateComparison, RouteID: "R08", BaselineRoot: baselineRoot, ArtifactRoot: outRoot, Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", ThresholdPct: 0.05, Source: testPixelSource()}
	opts.applyDefaults()
	baseline, err := loadAndValidateBaselineManifest(baselineRoot, opts)
	if err != nil {
		t.Fatalf("loadAndValidateBaselineManifest: %v", err)
	}
	currentPath := filepath.Join(outRoot, "R08-initial-00.png")
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		t.Fatalf("mkdir out: %v", err)
	}
	current := variedPNG(t, 8, 8, 200)
	if err := os.WriteFile(currentPath, current, 0o644); err != nil {
		t.Fatalf("write current: %v", err)
	}
	comparison, err := compareAgainstStoredBaseline(baseline, "initial", 0, current, currentPath, 0.05)
	if err != nil {
		t.Fatalf("compareAgainstStoredBaseline: %v", err)
	}
	if comparison.Passed {
		t.Fatalf("candidate comparison passed, want diff failure")
	}
	if comparison.BaselineThresholdPct != 0.5 || comparison.EffectiveThresholdPct != 0.05 {
		t.Fatalf("thresholds = baseline %f effective %f", comparison.BaselineThresholdPct, comparison.EffectiveThresholdPct)
	}
	if _, err := os.Stat(comparison.DiffPath); err != nil {
		t.Fatalf("stat diff: %v", err)
	}
}

func TestOuroborosCandidateBaselineLoadRejectsNonCanonicalJSON(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeCandidateComparison, RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}
	for _, tc := range []struct {
		name   string
		tamper func(t *testing.T, root string, manifest PixelEvidenceManifest)
	}{
		{
			name: "unknown top-level field",
			tamper: func(t *testing.T, root string, manifest PixelEvidenceManifest) {
				t.Helper()
				body, err := json.Marshal(manifest)
				if err != nil {
					t.Fatalf("marshal manifest: %v", err)
				}
				var raw map[string]any
				if err := json.Unmarshal(body, &raw); err != nil {
					t.Fatalf("unmarshal manifest: %v", err)
				}
				raw["unexpected"] = true
				body, err = json.Marshal(raw)
				if err != nil {
					t.Fatalf("marshal raw manifest: %v", err)
				}
				if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
					t.Fatalf("write manifest: %v", err)
				}
			},
		},
		{
			name: "trailing JSON",
			tamper: func(t *testing.T, root string, manifest PixelEvidenceManifest) {
				t.Helper()
				body, err := json.Marshal(manifest)
				if err != nil {
					t.Fatalf("marshal manifest: %v", err)
				}
				body = append(body, []byte("\n{}")...)
				if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
					t.Fatalf("write manifest: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "baseline")
			manifest := writeValidPixelBaseline(t, root, nil)
			tc.tamper(t, root, manifest)
			if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
				t.Fatalf("loadAndValidateBaselineManifest accepted %s", tc.name)
			}
		})
	}
}

func TestOuroborosCandidateBaselineLoadRejectsSettleTampering(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeCandidateComparison, RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}
	opts.applyDefaults()
	for _, tc := range []struct {
		name   string
		tamper func(*PixelEvidenceManifest)
		want   string
	}{
		{
			name: "render loop valid true contradictory state",
			tamper: func(m *PixelEvidenceManifest) {
				m.States[0].Settle.RenderLoop = RenderLoopEvidence{State: "stopped", Active: true, WantsAnimation: false, StateParsed: true, WantsAnimationParsed: true, Reason: "static", Valid: true}
			},
			want: "state=stopped but active=true",
		},
		{
			name: "settled required frame lowered",
			tamper: func(m *PixelEvidenceManifest) {
				m.States[1].Settle.RequiredFrame = 1
			},
			want: "settled settle requiredFrame",
		},
		{
			name: "settled capture below observed frame",
			tamper: func(m *PixelEvidenceManifest) {
				m.States[1].Captures[0].FrameSeq = m.States[1].Settle.ObservedFrame - 1
			},
			want: "below settle observedFrame",
		},
		{
			name: "static accepted without static capture telemetry",
			tamper: func(m *PixelEvidenceManifest) {
				staticLoop := validRenderLoop("stopped", "static", false)
				m.States[1].Settle.StaticAccepted = true
				m.States[1].Settle.AdvanceRequired = false
				m.States[1].Settle.RenderLoop = staticLoop
				m.States[1].Captures[0].RenderLoop = validRenderLoop("active", "runtime-program", true)
			},
			want: "exact static stopped telemetry",
		},
		{
			name: "static accepted after required frame reached",
			tamper: func(m *PixelEvidenceManifest) {
				staticLoop := validRenderLoop("stopped", "static", false)
				m.States[1].Settle.StaticAccepted = true
				m.States[1].Settle.AdvanceRequired = false
				m.States[1].Settle.RenderLoop = staticLoop
				m.States[1].Settle.ObservedFrame = m.States[1].Settle.RequiredFrame
				for i := range m.States[1].Captures {
					m.States[1].Captures[i].FrameSeq = m.States[1].Settle.ObservedFrame
					m.States[1].Captures[i].RenderLoop = staticLoop
				}
			},
			want: "observedFrame is below requiredFrame",
		},
		{
			name: "settle policy warmup mismatch",
			tamper: func(m *PixelEvidenceManifest) {
				m.SettlePolicy.WarmupFrames = 1
			},
			want: "settlePolicy warmupFrames",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "baseline")
			manifest := writeValidPixelBaseline(t, root, nil)
			tc.tamper(&manifest)
			writePixelManifestForTest(t, root, manifest)
			if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
				t.Fatalf("loadAndValidateBaselineManifest accepted %s", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOuroborosInitialSettleAllowsFastFirstFrameWithoutAdvance(t *testing.T) {
	state := PixelStateEvidence{
		State: "initial",
		Settle: PixelSettleResult{
			RequiredFrame:   7,
			ObservedFrame:   7,
			AdvanceRequired: false,
			RenderLoop:      validRenderLoop("active", "runtime-program", true),
		},
	}
	if failures := validateStateSettle("initial", state); len(failures) != 0 {
		t.Fatalf("validateStateSettle initial fast frame failures = %v", failures)
	}
}

func TestOuroborosSettledTargetAnchorsToRecordedInitialState(t *testing.T) {
	preflight := PixelSettleResult{RequiredFrame: 1, ObservedFrame: 2}
	initial := PixelStateEvidence{State: "initial", Settle: PixelSettleResult{RequiredFrame: preflight.ObservedFrame, ObservedFrame: 4}}
	got, err := pixelSettledMinFrameFromInitial(&initial, 30)
	if err != nil {
		t.Fatalf("pixelSettledMinFrameFromInitial: %v", err)
	}
	if got != 34 {
		t.Fatalf("settled min frame = %d, want recorded initial observed frame 4 + warmup 30", got)
	}
	if got == preflight.ObservedFrame+30 {
		t.Fatalf("settled min frame anchored to preflight frame %d", preflight.ObservedFrame)
	}
}

func TestOuroborosCandidateBaselineLoadRejectsManifestSymlinkEscape(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeCandidateComparison, RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}
	dir := t.TempDir()
	root := filepath.Join(dir, "baseline")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	writeValidPixelBaseline(t, outside, nil)
	if err := os.Symlink(filepath.Join(outside, "pixel-evidence.json"), filepath.Join(root, "pixel-evidence.json")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
		t.Fatalf("loadAndValidateBaselineManifest accepted manifest symlink escape")
	}
}

func TestOuroborosCallerCannotLoosenThreshold(t *testing.T) {
	baseline := 0.5
	got, err := effectiveCandidateThreshold(baseline, 0.75)
	if err != nil {
		t.Fatalf("effectiveCandidateThreshold loosen: %v", err)
	}
	if got != baseline {
		t.Fatalf("loosened threshold = %f, want %f", got, baseline)
	}
	got, err = effectiveCandidateThreshold(baseline, 0.1)
	if err != nil {
		t.Fatalf("effectiveCandidateThreshold tighten: %v", err)
	}
	if got != 0.1 {
		t.Fatalf("tightened threshold = %f, want 0.1", got)
	}
}

func TestOuroborosCanonicalBaselineManifestAdversarial(t *testing.T) {
	opts := PixelEvidenceOptions{
		Mode:           PixelModeCandidateComparison,
		RouteID:        "R08",
		Backend:        RequireBackendWebGL,
		ForceWebGL:     true,
		CanvasSelector: "canvas",
		Viewport:       Viewport{Width: 1440, Height: 900, Scale: 1},
	}
	cases := []struct {
		name string
		edit func(*PixelEvidenceManifest)
	}{
		{name: "wrong schema", edit: func(m *PixelEvidenceManifest) { m.SchemaVersion = "wrong" }},
		{name: "legacy schema v1", edit: func(m *PixelEvidenceManifest) { m.SchemaVersion = ouroborosPixelLegacySchemaV1 }},
		{name: "wrong mode", edit: func(m *PixelEvidenceManifest) { m.Mode = string(PixelModeCandidateComparison) }},
		{name: "uncertified", edit: func(m *PixelEvidenceManifest) { m.Certified = false }},
		{name: "wrong route", edit: func(m *PixelEvidenceManifest) { m.RouteID = "R10" }},
		{name: "extra state", edit: func(m *PixelEvidenceManifest) { m.States = append(m.States, PixelStateEvidence{State: "extra"}) }},
		{name: "duplicate state", edit: func(m *PixelEvidenceManifest) { m.States = append(m.States, m.States[0]) }},
		{name: "missing source", edit: func(m *PixelEvidenceManifest) { m.Source = PixelSourceIdentity{} }},
		{name: "malformed source", edit: func(m *PixelEvidenceManifest) { m.Source.InventorySHA256 = "bad" }},
		{name: "candidate-only baseline source", edit: func(m *PixelEvidenceManifest) { m.BaselineSource = &m.Source }},
		{name: "candidate-only source relation", edit: func(m *PixelEvidenceManifest) { m.SourceRelation = "same-source" }},
		{name: "wrong backend", edit: func(m *PixelEvidenceManifest) { m.BackendRequirement = "webgpu" }},
		{name: "missing requested backend", edit: func(m *PixelEvidenceManifest) { m.BackendSelection.RequestedBackend = "" }},
		{name: "missing observed backend", edit: func(m *PixelEvidenceManifest) { m.BackendSelection.RuntimeObservedBackend = "" }},
		{name: "missing hook", edit: func(m *PixelEvidenceManifest) { m.BackendSelection.PreNavigationHook = "" }},
		{name: "wrong hook", edit: func(m *PixelEvidenceManifest) { m.BackendSelection.PreNavigationHook = "wrong-hook" }},
		{name: "mismatched requested observed", edit: func(m *PixelEvidenceManifest) { m.BackendSelection.RuntimeObservedBackend = "webgpu" }},
		{name: "force true baseline false option", edit: func(m *PixelEvidenceManifest) { m.BackendSelection.ForceWebGL = false }},
		{name: "wrong hardware", edit: func(m *PixelEvidenceManifest) { m.HardwareClassification = "software-raster" }},
		{name: "too few samples", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures = m.States[0].Captures[:2] }},
		{name: "device loss", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].DeviceLost = true }},
		{name: "fallback", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].FallbackReason = "webgpu-unavailable" }},
		{name: "shader error", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].ShaderDiagnostics.Errors = 1 }},
		{name: "software marker", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].SoftwareRaster = true }},
		{name: "duplicate state index", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[1].Index = 0 }},
		{name: "wrong filename state", edit: func(m *PixelEvidenceManifest) {
			data := variedPNG(t, 8, 8, 0)
			wrong := filepath.Join(filepath.Dir(m.States[0].Captures[0].Path), "R08-settled-00.png")
			if err := os.WriteFile(wrong, data, 0o644); err != nil {
				t.Fatalf("write wrong-name png: %v", err)
			}
			sum := sha256.Sum256(data)
			m.States[0].Captures[0].Path = wrong
			m.States[0].Captures[0].SHA256 = hex.EncodeToString(sum[:])
		}},
		{name: "wrong filename without route prefix", edit: func(m *PixelEvidenceManifest) {
			data := variedPNG(t, 8, 8, 0)
			wrong := filepath.Join(filepath.Dir(m.States[0].Captures[0].Path), "initial-00.png")
			if err := os.WriteFile(wrong, data, 0o644); err != nil {
				t.Fatalf("write wrong-name png: %v", err)
			}
			sum := sha256.Sum256(data)
			m.States[0].Captures[0].Path = wrong
			m.States[0].Captures[0].SHA256 = hex.EncodeToString(sum[:])
			m.States[0].Captures[0].Bytes = len(data)
		}},
		{name: "duplicate path", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[1].Path = m.States[0].Captures[0].Path }},
		{name: "missing runtime truth", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].RuntimeTruthParsed = false }},
		{name: "runtime gpu false", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].RuntimeGPU = false }},
		{name: "empty implementation", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].Implementation = "" }},
		{name: "frame zero", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].FrameSeq = 0 }},
		{name: "missing batch id", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].BatchID = "" }},
		{name: "capture batch mismatch", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].BatchID = "other" }},
		{name: "duplicate batch id", edit: func(m *PixelEvidenceManifest) { m.States[1].Batch.ID = m.States[0].Batch.ID }},
		{name: "bad nonce hash", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.NonceHash = "bad" }},
		{name: "hidden batch", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.Before.Visible = false }},
		{name: "unfocused batch", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.After.Focused = false }},
		{name: "batch backend flip", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.After.Backend = "webgpu" }},
		{name: "batch renderer flip", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.After.Renderer = "webgpu" }},
		{name: "capture frame drift from batch", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].FrameSeq++ }},
		{name: "batch restore failed", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.Restored = false }},
		{name: "batch cleanup failed", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.Cleaned = false }},
		{name: "active loop queued zero callbacks", edit: func(m *PixelEvidenceManifest) { m.States[0].Batch.QueueAfterDrain = 0 }},
		{name: "blank placeholder", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].Placeholder = true }},
		{name: "actual blank png with false flags", edit: func(m *PixelEvidenceManifest) {
			path := m.States[0].Captures[0].Path
			data := solidPNG(t, 8, 8, color.RGBA{A: 255})
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("write blank png: %v", err)
			}
			sum := sha256.Sum256(data)
			m.States[0].Captures[0].SHA256 = hex.EncodeToString(sum[:])
			m.States[0].Captures[0].Blank = false
			m.States[0].Captures[0].Placeholder = false
			m.States[0].Captures[0].Comparison.Passed = true
		}},
		{name: "renderer failure", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].RendererFailure = true }},
		{name: "missing comparison", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].Comparison = nil }},
		{name: "failed comparison", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].Comparison.Passed = false }},
		{name: "actual drift despite passed comparison", edit: func(m *PixelEvidenceManifest) {
			path := m.States[0].Captures[1].Path
			data := variedPNG(t, 8, 8, 220)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("write drifted png: %v", err)
			}
			sum := sha256.Sum256(data)
			m.States[0].Captures[1].SHA256 = hex.EncodeToString(sum[:])
			m.States[0].Captures[1].Comparison.Passed = true
		}},
		{name: "missing png", edit: func(m *PixelEvidenceManifest) {
			m.States[0].Captures[0].Path = filepath.Join(t.TempDir(), "missing.png")
		}},
		{name: "hash mismatch", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].SHA256 = strings.Repeat("b", 64) }},
		{name: "bytes mismatch", edit: func(m *PixelEvidenceManifest) { m.States[0].Captures[0].Bytes++ }},
		{name: "png symlink escape", edit: func(m *PixelEvidenceManifest) {
			path := m.States[0].Captures[0].Path
			outside := filepath.Join(t.TempDir(), "outside.png")
			data := variedPNG(t, 8, 8, 0)
			if err := os.WriteFile(outside, data, 0o644); err != nil {
				t.Fatalf("write outside png: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove capture: %v", err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Skipf("symlink not available: %v", err)
			}
		}},
		{name: "out of root", edit: func(m *PixelEvidenceManifest) {
			outside := filepath.Join(t.TempDir(), "outside.png")
			if err := os.WriteFile(outside, variedPNG(t, 8, 8, 0), 0o644); err != nil {
				t.Fatalf("write outside: %v", err)
			}
			m.States[0].Captures[0].Path = outside
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "baseline")
			writeValidPixelBaseline(t, root, tc.edit)
			if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
				t.Fatalf("loadAndValidateBaselineManifest accepted %s", tc.name)
			}
		})
	}
}

func TestOuroborosThresholdPolicyRejectsInvalidValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -0.01, 1.01} {
		if err := validatePixelThreshold(value, false); err == nil {
			t.Fatalf("validatePixelThreshold accepted %v", value)
		}
	}
	if err := validatePixelThreshold(0, false); err != nil {
		t.Fatalf("validatePixelThreshold rejected zero: %v", err)
	}
	root := t.TempDir()
	writeValidPixelBaseline(t, root, func(m *PixelEvidenceManifest) {
		m.Threshold.EffectivePct = 100
	})
	opts := PixelEvidenceOptions{RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}
	if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
		t.Fatalf("accepted fabricated 100-percent threshold manifest")
	}
}

func TestOuroborosPixelEvidenceBounds(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: t.TempDir(), Backend: RequireBackendWebGL, Samples: MaxPixelEvidenceSamples + 1, Source: testPixelSource()}
	opts.applyDefaults()
	if err := validatePixelOptions(opts); err == nil {
		t.Fatalf("validatePixelOptions accepted too many samples")
	}
	opts.Samples = 3
	opts.Backend = RequireBackendAnyGPU
	if err := validatePixelOptions(opts); err == nil {
		t.Fatalf("validatePixelOptions accepted any-gpu for O0.2")
	}
	if err := validatePixelImageBounds(MaxPixelImageDimension+1, 16); err == nil {
		t.Fatalf("validatePixelImageBounds accepted an oversized dimension")
	}
	if err := validatePixelImageBounds(MaxPixelImageDimension, (MaxPixelDecodedPixels/MaxPixelImageDimension)+1); err == nil {
		t.Fatalf("validatePixelImageBounds accepted too many decoded pixels")
	}
}

func TestOuroborosManifestAndPNGBoundsFailClosed(t *testing.T) {
	opts := PixelEvidenceOptions{Mode: PixelModeCandidateComparison, RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}

	oversizedManifestRoot := filepath.Join(t.TempDir(), "manifest")
	if err := os.MkdirAll(oversizedManifestRoot, 0o755); err != nil {
		t.Fatalf("mkdir manifest root: %v", err)
	}
	f, err := os.Create(filepath.Join(oversizedManifestRoot, "pixel-evidence.json"))
	if err != nil {
		t.Fatalf("create oversized manifest: %v", err)
	}
	if err := f.Truncate(MaxPixelManifestBytes + 1); err != nil {
		_ = f.Close()
		t.Fatalf("truncate manifest: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close manifest: %v", err)
	}
	if _, err := loadAndValidateBaselineManifest(oversizedManifestRoot, opts); err == nil {
		t.Fatalf("accepted oversized manifest")
	}

	root := filepath.Join(t.TempDir(), "baseline")
	writeValidPixelBaseline(t, root, func(m *PixelEvidenceManifest) {
		m.States[0].Captures[0].Bytes = MaxPixelPNGBytes + 1
	})
	if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
		t.Fatalf("accepted oversized declared PNG bytes")
	}
}

func TestOuroborosAtomicPreflightDoesNotCreateFinalRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pixels")
	_, err := CapturePixelEvidence(context.Background(), "https://example.test/", PixelEvidenceOptions{
		Mode:         PixelModeRecordBaseline,
		RouteID:      "R08",
		ArtifactRoot: root,
		Backend:      RequireBackendAnyGPU,
		Samples:      3,
		Source:       testPixelSource(),
	})
	if err == nil {
		t.Fatalf("CapturePixelEvidence accepted any-gpu")
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("final root exists after preflight failure: %v", statErr)
	}
}

func TestOuroborosAtomicPublishRejectsExistingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pixels")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	opts := PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: root, Backend: RequireBackendWebGL, Samples: 3, Source: testPixelSource()}
	opts.applyDefaults()
	if err := preparePixelArtifactRoot(opts); err == nil {
		t.Fatalf("preparePixelArtifactRoot accepted existing root")
	}
}

func TestOuroborosAtomicPublishSuccessAndRollback(t *testing.T) {
	dir := t.TempDir()
	finalRoot := filepath.Join(dir, "pixels")
	stagingRoot, cleanup, err := createPixelStagingRoot(finalRoot)
	if err != nil {
		t.Fatalf("create staging: %v", err)
	}
	defer cleanup()
	if err := os.WriteFile(filepath.Join(stagingRoot, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := publishPixelStagingRoot(stagingRoot, PixelEvidenceOptions{ArtifactRoot: finalRoot}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := os.Stat(filepath.Join(finalRoot, "ok.txt")); err != nil {
		t.Fatalf("published file missing: %v", err)
	}

	if err := os.WriteFile(filepath.Join(finalRoot, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write existing root: %v", err)
	}
	missingStaging := filepath.Join(dir, "missing-staging")
	err = publishPixelStagingRoot(missingStaging, PixelEvidenceOptions{ArtifactRoot: finalRoot, AllowOverwrite: true})
	if err == nil {
		t.Fatalf("publish accepted missing staging")
	}
	if _, statErr := os.Stat(filepath.Join(finalRoot, "old.txt")); statErr != nil {
		t.Fatalf("rollback did not restore existing root: %v", statErr)
	}
}

func TestOuroborosStagingCleanupRemovesPartialRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pixels")
	stagingRoot, cleanup, err := createPixelStagingRoot(root)
	if err != nil {
		t.Fatalf("create staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingRoot, "partial.png"), []byte("partial"), 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	cleanup()
	if _, err := os.Stat(stagingRoot); !os.IsNotExist(err) {
		t.Fatalf("staging root remains after cleanup: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("final root exists after staging cleanup: %v", err)
	}
}

func TestOuroborosManifestWriteFailureDoesNotCreateFinalRoot(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	finalRoot := filepath.Join(dir, "pixels")
	err := writePixelManifest(PixelEvidenceManifest{}, PixelEvidenceOptions{
		ArtifactRoot: finalRoot,
		writeRoot:    filepath.Join(blocker, "child"),
	})
	if err == nil {
		t.Fatalf("writePixelManifest accepted blocked staging root")
	}
	if _, statErr := os.Stat(finalRoot); !os.IsNotExist(statErr) {
		t.Fatalf("final root exists after manifest write failure: %v", statErr)
	}
}

func TestOuroborosCandidateBackendSelectionMustMatchBaseline(t *testing.T) {
	opts := PixelEvidenceOptions{Backend: RequireBackendWebGL}
	opts.applyDefaults()
	baseline := PixelEvidenceManifest{
		RouteID:                "R08",
		BackendRequirement:     "webgl",
		HardwareClassification: "hardware-webgl",
		Viewport:               ViewportEvidence{Width: 1440, Height: 900, DPR: 1},
		Source:                 testPixelSource(),
		Selected:               SelectedSceneEvidence{MountID: "mount", CanvasSelector: "canvas"},
		BackendSelection: PixelBackendSelection{
			RequestedBackend:       "webgl",
			RuntimeObservedBackend: "webgl",
			ForceWebGL:             true,
			PreNavigationHook:      "gosx-o02-force-webgl-new-document",
		},
	}
	candidate := baseline
	candidate.BaselineSource = &baseline.Source
	candidate.SourceRelation = "same-source"
	candidate.BackendSelection.RuntimeObservedBackend = "webgpu"
	failures := validateCandidateAgainstBaseline(opts, candidate, baseline)
	if !containsFailure(failures, "observed backend") {
		t.Fatalf("failures = %v, want observed backend mismatch", failures)
	}
	candidate = baseline
	candidate.BaselineSource = &baseline.Source
	candidate.SourceRelation = "same-source"
	candidate.BackendSelection.PreNavigationHook = "gosx-o02-clear-force-webgl-new-document"
	failures = validateCandidateAgainstBaseline(opts, candidate, baseline)
	if !containsFailure(failures, "pre-navigation hook") {
		t.Fatalf("failures = %v, want hook mismatch", failures)
	}
	candidate = baseline
	candidate.BaselineSource = &baseline.Source
	candidate.SourceRelation = "same-source"
	candidate.BackendSelection.ForceWebGL = false
	failures = validateCandidateAgainstBaseline(opts, candidate, baseline)
	if !containsFailure(failures, "forceWebGL") {
		t.Fatalf("failures = %v, want forceWebGL mismatch", failures)
	}
	baseline.BackendSelection.ForceWebGL = false
	candidate = baseline
	candidate.BaselineSource = &baseline.Source
	candidate.SourceRelation = "same-source"
	candidate.BackendSelection.ForceWebGL = true
	failures = validateCandidateAgainstBaseline(opts, candidate, baseline)
	if !containsFailure(failures, "forceWebGL") {
		t.Fatalf("failures = %v, want reverse forceWebGL mismatch", failures)
	}
}

func TestOuroborosCandidateRecordsBaselineSourceRelation(t *testing.T) {
	opts := PixelEvidenceOptions{Backend: RequireBackendWebGL}
	opts.applyDefaults()
	baseline := PixelEvidenceManifest{
		RouteID:                "R08",
		BackendRequirement:     "webgl",
		HardwareClassification: "hardware-webgl",
		Viewport:               ViewportEvidence{Width: 1440, Height: 900, DPR: 1},
		Source:                 testPixelSource(),
		Selected:               SelectedSceneEvidence{MountID: "mount", CanvasSelector: "canvas"},
		BackendSelection: PixelBackendSelection{
			RequestedBackend:       "webgl",
			RuntimeObservedBackend: "webgl",
			ForceWebGL:             true,
			PreNavigationHook:      "gosx-o02-force-webgl-new-document",
		},
	}
	candidate := baseline
	candidate.Source = PixelSourceIdentity{
		BaseRevision:    "def5678",
		OverlayHash:     "sha256:" + strings.Repeat("b", 64),
		InventorySHA256: "sha256:" + strings.Repeat("c", 64),
	}
	candidate.BaselineSource = &baseline.Source
	candidate.SourceRelation = "candidate-compared-to-baseline"
	failures := validateCandidateAgainstBaseline(opts, candidate, baseline)
	if containsFailure(failures, "source") {
		t.Fatalf("failures = %v, cross-source regression comparison must be allowed with explicit relation", failures)
	}
	candidate.SourceRelation = "same-source"
	failures = validateCandidateAgainstBaseline(opts, candidate, baseline)
	if !containsFailure(failures, "sourceRelation") {
		t.Fatalf("failures = %v, want sourceRelation mismatch", failures)
	}
	candidate.SourceRelation = "candidate-compared-to-baseline"
	other := baseline.Source
	other.InventorySHA256 = "sha256:" + strings.Repeat("d", 64)
	candidate.BaselineSource = &other
	failures = validateCandidateAgainstBaseline(opts, candidate, baseline)
	if !containsFailure(failures, "baselineSource") {
		t.Fatalf("failures = %v, want baselineSource mismatch", failures)
	}
}

func TestOuroborosCanonicalBaselineForceWebGLFalseRejectsTrueOption(t *testing.T) {
	root := filepath.Join(t.TempDir(), "baseline")
	writeValidPixelBaseline(t, root, func(m *PixelEvidenceManifest) {
		m.BackendSelection.ForceWebGL = false
		m.BackendSelection.PreNavigationHook = "gosx-o02-clear-force-webgl-new-document"
	})
	opts := PixelEvidenceOptions{RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}
	opts.applyDefaults()
	if _, err := loadAndValidateBaselineManifest(root, opts); err == nil {
		t.Fatalf("accepted false ForceWebGL baseline with true candidate option")
	}
}

func TestOuroborosCanonicalBaselineForceWebGLTrueRejectsFalseOption(t *testing.T) {
	root := filepath.Join(t.TempDir(), "baseline")
	manifest := writeValidPixelBaseline(t, root, nil)
	opts := PixelEvidenceOptions{RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: false, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}, Source: testPixelSource()}
	opts.applyDefaults()
	_, failures := validateCanonicalBaselineManifest(root, opts, manifest)
	if !containsFailure(failures, "forceWebGL=true, want false") {
		t.Fatalf("failures = %v, want forceWebGL mismatch", failures)
	}
}

func TestValidateCanonicalPixelBaselineManifestReplaysEvidenceAndSource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "baseline")
	manifest := writeValidPixelBaseline(t, root, nil)
	opts := PixelEvidenceOptions{RouteID: "R08", Backend: RequireBackendWebGL, ForceWebGL: true, CanvasSelector: "canvas", Viewport: Viewport{Width: 1440, Height: 900, Scale: 1}}
	opts.applyDefaults()
	validated, err := ValidateCanonicalPixelBaselineManifest(filepath.Join(root, "pixel-evidence.json"), manifest.Source, opts)
	if err != nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest: %v", err)
	}
	if len(validated.Captures) != 6 {
		t.Fatalf("validated captures = %d, want 6", len(validated.Captures))
	}
	wrongSource := manifest.Source
	wrongSource.InventorySHA256 = "sha256:" + strings.Repeat("e", 64)
	if _, err := ValidateCanonicalPixelBaselineManifest(filepath.Join(root, "pixel-evidence.json"), wrongSource, opts); err == nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest accepted wrong expected source")
	}
	unknownRoot := filepath.Join(t.TempDir(), "unknown")
	unknownManifest := writeValidPixelBaseline(t, unknownRoot, nil)
	unknownBody, err := json.Marshal(unknownManifest)
	if err != nil {
		t.Fatalf("marshal unknown manifest: %v", err)
	}
	var unknownRaw map[string]any
	if err := json.Unmarshal(unknownBody, &unknownRaw); err != nil {
		t.Fatalf("unmarshal unknown manifest: %v", err)
	}
	unknownRaw["unexpected"] = true
	unknownBody, err = json.Marshal(unknownRaw)
	if err != nil {
		t.Fatalf("marshal unknown raw manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unknownRoot, "pixel-evidence.json"), unknownBody, 0o644); err != nil {
		t.Fatalf("write unknown manifest: %v", err)
	}
	if _, err := ValidateCanonicalPixelBaselineManifest(filepath.Join(unknownRoot, "pixel-evidence.json"), unknownManifest.Source, opts); err == nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest accepted unknown top-level field")
	}
	trailingRoot := filepath.Join(t.TempDir(), "trailing")
	trailingManifest := writeValidPixelBaseline(t, trailingRoot, nil)
	trailingBody, err := json.Marshal(trailingManifest)
	if err != nil {
		t.Fatalf("marshal trailing manifest: %v", err)
	}
	trailingBody = append(trailingBody, []byte("\n{}")...)
	if err := os.WriteFile(filepath.Join(trailingRoot, "pixel-evidence.json"), trailingBody, 0o644); err != nil {
		t.Fatalf("write trailing manifest: %v", err)
	}
	if _, err := ValidateCanonicalPixelBaselineManifest(filepath.Join(trailingRoot, "pixel-evidence.json"), trailingManifest.Source, opts); err == nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest accepted trailing JSON")
	}
	manifest.States[0].Captures[0].SHA256 = strings.Repeat("f", 64)
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal tampered manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
		t.Fatalf("write tampered manifest: %v", err)
	}
	if _, err := ValidateCanonicalPixelBaselineManifest(filepath.Join(root, "pixel-evidence.json"), testPixelSource(), opts); err == nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest accepted tampered capture hash")
	}
	if _, err := ValidateCanonicalPixelBaselineManifest(filepath.Join(root, "manifest.json"), testPixelSource(), opts); err == nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest accepted wrong manifest name")
	}
}

func decodePNGForTest(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func variedPNG(t *testing.T, w, h int, offset int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*17 + offset) % 255),
				G: uint8((y*29 + offset) % 255),
				B: uint8((x*y + 50 + offset) % 255),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode varied png: %v", err)
	}
	return buf.Bytes()
}

func pngWithIHDRForTest(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	var ihdr [13]byte
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8
	ihdr[9] = 2
	writePNGChunkForTest(t, &buf, "IHDR", ihdr[:])
	writePNGChunkForTest(t, &buf, "IEND", nil)
	return buf.Bytes()
}

func writePNGChunkForTest(t *testing.T, buf *bytes.Buffer, kind string, data []byte) {
	t.Helper()
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	buf.WriteString(kind)
	buf.Write(data)
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(kind))
	_, _ = crc.Write(data)
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc.Sum32())
	buf.Write(sum[:])
}

func writeValidPixelBaseline(t *testing.T, root string, edit func(*PixelEvidenceManifest)) PixelEvidenceManifest {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir baseline: %v", err)
	}
	manifest := PixelEvidenceManifest{
		SchemaVersion:      OuroborosPixelSchemaVersion,
		RouteID:            "R08",
		Mode:               string(PixelModeRecordBaseline),
		ArtifactRoot:       root,
		Source:             testPixelSource(),
		BackendRequirement: "webgl",
		BackendSelection: PixelBackendSelection{
			RequestedBackend:       "webgl",
			RuntimeObservedBackend: "webgl",
			ForceWebGL:             true,
			PreNavigationHook:      "gosx-o02-force-webgl-new-document",
		},
		Certified:              true,
		HardwareClassification: "hardware-webgl",
		Viewport:               ViewportEvidence{Width: 1440, Height: 900, DPR: 1},
		Selected:               SelectedSceneEvidence{MountID: "mount", MountSelector: "#mount", CanvasSelector: "canvas", CanvasCount: 1, MountCount: 1},
		Threshold:              PixelThresholdEvidence{EffectivePct: 0.5},
		SettlePolicy: PixelSettlePolicy{
			WarmupFrames:                 30,
			WarmupAnchor:                 "initial-observed-frame",
			RuntimeRenderLoopRequired:    true,
			StaticStoppedAllowsNoAdvance: true,
			RAFGate: PixelRAFGatePolicy{
				SchemaVersion:                 "gosx.ouroboros.raf-gate.v1",
				Strategy:                      "raf-batch-gate",
				Enabled:                       true,
				DrainTicks:                    2,
				TemporaryGlobal:               true,
				NonceKeyed:                    true,
				NonEnumerable:                 true,
				NegativeSyntheticIDs:          true,
				NativeTimestampResume:         true,
				CapturesUseStableClip:         true,
				FailClosedRestore:             true,
				ResumeBeforeNextReadinessWait: true,
			},
		},
	}
	for _, stateName := range []string{"initial", "settled"} {
		settle := PixelSettleResult{
			RequiredFrame:   10,
			ObservedFrame:   10,
			AdvanceRequired: false,
			RenderLoop:      validRenderLoop("active", "runtime-program", true),
		}
		if stateName == "settled" {
			settle.RequiredFrame = 40
			settle.ObservedFrame = 40
			settle.AdvanceRequired = true
		}
		state := PixelStateEvidence{State: stateName, Settle: settle, Batch: validPixelBatchForTest("R08-"+stateName+"-batch", stateName, settle.ObservedFrame, settle.RenderLoop)}
		for i := 0; i < 3; i++ {
			data := variedPNG(t, 8, 8, 0)
			path := filepath.Join(root, fmt.Sprintf("R08-%s-%02d.png", stateName, i))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("write baseline %s: %v", path, err)
			}
			sum := sha256.Sum256(data)
			state.Captures = append(state.Captures, PixelCaptureEvidence{
				Index:              i,
				Path:               path,
				SHA256:             hex.EncodeToString(sum[:]),
				Bytes:              len(data),
				Width:              8,
				Height:             8,
				Backend:            "webgl",
				Renderer:           "webgl",
				RuntimeTruthParsed: true,
				RuntimeGPU:         true,
				Implementation:     "angle-webgl",
				HardwareClass:      "hardware-webgl",
				FrameSeq:           settle.ObservedFrame,
				BatchID:            state.Batch.ID,
				RenderLoop:         settle.RenderLoop,
				WebGL:              WebGLEvidence{Vendor: "NVIDIA Corporation", Renderer: "ANGLE RTX", Version: "webgl"},
				Selected:           manifest.Selected,
				Comparison:         &PixelComparison{Passed: true, BaselineThresholdPct: 0.5, EffectiveThresholdPct: 0.5},
			})
		}
		manifest.States = append(manifest.States, state)
	}
	if edit != nil {
		edit(&manifest)
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return manifest
}

func validRenderLoop(state, reason string, wantsAnimation bool) RenderLoopEvidence {
	return RenderLoopEvidence{
		State:                state,
		Reason:               reason,
		Active:               state == "active",
		WantsAnimation:       wantsAnimation,
		StateParsed:          true,
		WantsAnimationParsed: true,
		Valid:                true,
	}
}

func validPixelBatchForTest(id, state string, frame int, loop RenderLoopEvidence) PixelBatchEvidence {
	snapshot := PixelBatchSnapshot{
		Visible:            true,
		Focused:            true,
		Backend:            "webgl",
		Renderer:           "webgl",
		FrameSeq:           frame,
		RuntimeTruthParsed: true,
		RenderLoopState:    loop.State,
		RenderLoopActive:   loop.Active,
		WantsAnimation:     loop.WantsAnimation,
		Clip:               PixelCanvasClipEvidence{Width: 8, Height: 8, Scale: 1, Stable: true},
	}
	queued := 1
	if renderLoopIsStaticStopped(loop) {
		queued = 0
	}
	return PixelBatchEvidence{
		ID:                 id,
		State:              state,
		Acquired:           true,
		Released:           true,
		ReleaseProved:      true,
		NonceHash:          "sha256:" + strings.Repeat("1", 64),
		GlobalKeyHash:      "sha256:" + strings.Repeat("2", 64),
		DrainTicks:         2,
		NativeTickCount:    2,
		QueueAfterDrain:    queued,
		QueueBeforeRelease: queued,
		Delivered:          queued,
		Restored:           true,
		Cleaned:            true,
		Clip:               snapshot.Clip,
		BeforeAcquire:      snapshot,
		Before:             snapshot,
		After:              snapshot,
	}
}

func writePixelManifestForTest(t *testing.T, root string, manifest PixelEvidenceManifest) {
	t.Helper()
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func testPixelSource() PixelSourceIdentity {
	return PixelSourceIdentity{
		BaseRevision:    "abc1234",
		OverlayHash:     "sha256:clean",
		InventorySHA256: "sha256:" + strings.Repeat("a", 64),
	}
}

func containsFailure(failures []string, want string) bool {
	for _, failure := range failures {
		if strings.Contains(failure, want) {
			return true
		}
	}
	return false
}
