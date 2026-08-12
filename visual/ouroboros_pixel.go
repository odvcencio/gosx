package visual

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	cdpRuntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	OuroborosPixelSchemaVersion   = "gosx.ouroboros.pixels.v1"
	MaxCanonicalPixelThresholdPct = 1.0
	portablePixelArtifactRoot     = "."
)

type PixelEvidenceMode string

const (
	PixelModeRecordBaseline      PixelEvidenceMode = "record-baseline"
	PixelModeCandidateComparison PixelEvidenceMode = "candidate"
)

func ValidPixelEvidenceMode(value string) bool {
	switch PixelEvidenceMode(value) {
	case PixelModeRecordBaseline, PixelModeCandidateComparison:
		return true
	default:
		return false
	}
}

type PixelEvidenceOptions struct {
	Mode           PixelEvidenceMode
	RouteID        string
	ArtifactRoot   string
	BaselineRoot   string
	Source         PixelSourceIdentity
	Backend        RequireBackend
	Samples        int
	Viewport       Viewport
	InitialWait    time.Duration
	SettledWait    time.Duration
	WarmupFrames   int
	WaitSelector   string
	CanvasSelector string
	Timeout        time.Duration
	ThresholdPct   float64
	AllowOverwrite bool
	ForceWebGL     bool
}

type PixelEvidenceManifest struct {
	SchemaVersion          string                 `json:"schemaVersion"`
	GeneratedAt            string                 `json:"generatedAt"`
	URL                    string                 `json:"url"`
	RouteID                string                 `json:"routeID"`
	Mode                   string                 `json:"mode"`
	ArtifactRoot           string                 `json:"artifactRoot"`
	BaselineRoot           string                 `json:"baselineRoot,omitempty"`
	Source                 PixelSourceIdentity    `json:"source"`
	BaselineSource         *PixelSourceIdentity   `json:"baselineSource,omitempty"`
	SourceRelation         string                 `json:"sourceRelation,omitempty"`
	BackendRequirement     string                 `json:"backendRequirement"`
	BackendSelection       PixelBackendSelection  `json:"backendSelection"`
	Certified              bool                   `json:"certified"`
	HardwareClassification string                 `json:"hardwareClassification"`
	Browser                BrowserEvidence        `json:"browser"`
	Viewport               ViewportEvidence       `json:"viewport"`
	Selected               SelectedSceneEvidence  `json:"selected"`
	States                 []PixelStateEvidence   `json:"states"`
	Failures               []string               `json:"failures,omitempty"`
	Threshold              PixelThresholdEvidence `json:"threshold"`
}

type PixelSourceIdentity struct {
	BaseRevision    string `json:"baseRevision"`
	OverlayHash     string `json:"overlayHash"`
	InventorySHA256 string `json:"inventorySHA256"`
}

type PixelBackendSelection struct {
	RequestedBackend       string `json:"requestedBackend"`
	RuntimeObservedBackend string `json:"runtimeObservedBackend,omitempty"`
	ForceWebGL             bool   `json:"forceWebGL"`
	PreNavigationHook      string `json:"preNavigationHook,omitempty"`
}

type BrowserEvidence struct {
	Product   string   `json:"product"`
	UserAgent string   `json:"userAgent"`
	Flags     []string `json:"flags,omitempty"`
}

type SelectedSceneEvidence struct {
	MountID        string `json:"mountID"`
	MountSelector  string `json:"mountSelector"`
	CanvasSelector string `json:"canvasSelector"`
	CanvasCount    int    `json:"canvasCount"`
	MountCount     int    `json:"mountCount"`
}

type ViewportEvidence struct {
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	DPR             float64 `json:"dpr"`
	EffectiveDPR    float64 `json:"effectiveDPR"`
	CanvasWidth     int     `json:"canvasWidth"`
	CanvasHeight    int     `json:"canvasHeight"`
	CanvasCSSWidth  float64 `json:"canvasCSSWidth"`
	CanvasCSSHeight float64 `json:"canvasCSSHeight"`
}

type PixelStateEvidence struct {
	State    string                 `json:"state"`
	Captures []PixelCaptureEvidence `json:"captures"`
}

type PixelCaptureEvidence struct {
	Index              int                       `json:"index"`
	Path               string                    `json:"path"`
	SHA256             string                    `json:"sha256"`
	Bytes              int                       `json:"bytes"`
	Width              int                       `json:"width"`
	Height             int                       `json:"height"`
	Blank              bool                      `json:"blank"`
	Placeholder        bool                      `json:"placeholder"`
	Backend            string                    `json:"backend"`
	Renderer           string                    `json:"renderer"`
	FallbackReason     string                    `json:"fallbackReason"`
	Implementation     string                    `json:"implementation"`
	RuntimeTruthParsed bool                      `json:"runtimeTruthParsed"`
	RuntimeGPU         bool                      `json:"runtimeGPU"`
	DeviceLost         bool                      `json:"deviceLost"`
	RendererFailure    bool                      `json:"rendererFailure"`
	SoftwareRaster     bool                      `json:"softwareRaster"`
	HardwareClass      string                    `json:"hardwareClass"`
	FrameSeq           int                       `json:"frameSeq"`
	Post               PostEvidence              `json:"post"`
	ShaderDiagnostics  ShaderDiagnosticsEvidence `json:"shaderDiagnostics"`
	WebGPU             WebGPUEvidence            `json:"webgpu"`
	WebGL              WebGLEvidence             `json:"webgl"`
	Viewport           ViewportEvidence          `json:"viewport"`
	Selected           SelectedSceneEvidence     `json:"selected"`
	Comparison         *PixelComparison          `json:"comparison,omitempty"`
	UserAgent          string                    `json:"userAgent"`
	CapturedAt         string                    `json:"capturedAt"`
}

type PostEvidence struct {
	Authored   int    `json:"authored"`
	Dispatched int    `json:"dispatched"`
	Dead       int    `json:"dead"`
	Failed     int    `json:"failed"`
	Pending    int    `json:"pending"`
	Chain      string `json:"chain,omitempty"`
}

type WebGPUEvidence struct {
	Available      bool                   `json:"available"`
	AdapterName    string                 `json:"adapterName"`
	Vendor         string                 `json:"vendor"`
	Architecture   string                 `json:"architecture"`
	Device         string                 `json:"device"`
	Description    string                 `json:"description"`
	Fallback       bool                   `json:"fallback"`
	FallbackReason string                 `json:"fallbackReason"`
	AdapterInfo    map[string]interface{} `json:"adapterInfo,omitempty"`
}

type WebGLEvidence struct {
	Vendor   string `json:"vendor"`
	Renderer string `json:"renderer"`
	Version  string `json:"version"`
}

type PixelComparison struct {
	BaselinePath          string  `json:"baselinePath"`
	DiffPath              string  `json:"diffPath,omitempty"`
	DiffPct               float64 `json:"diffPct"`
	Mismatched            int     `json:"mismatched"`
	Total                 int     `json:"total"`
	DimensionsMatch       bool    `json:"dimensionsMatch"`
	Similarity            float64 `json:"similarity"`
	BaselineThresholdPct  float64 `json:"baselineThresholdPct"`
	EffectiveThresholdPct float64 `json:"effectiveThresholdPct"`
	Passed                bool    `json:"passed"`
}

type PixelThresholdEvidence struct {
	BaselinePct  float64 `json:"baselinePct"`
	RequestedPct float64 `json:"requestedPct"`
	EffectivePct float64 `json:"effectivePct"`
}

type pagePixelMetadata struct {
	DevicePixelRatio float64               `json:"devicePixelRatio"`
	EffectiveDPR     float64               `json:"effectiveDPR"`
	Canvas           ViewportEvidence      `json:"canvas"`
	Selected         SelectedSceneEvidence `json:"selected"`
	Mount            sceneMountBackend     `json:"mount"`
	Truth            sceneTruthEvidence    `json:"truth"`
	WebGPU           WebGPUEvidence        `json:"webgpu"`
	WebGL            WebGLEvidence         `json:"webgl"`
	UserAgent        string                `json:"userAgent"`
	Errors           []string              `json:"errors"`
}

type sceneTruthEvidence struct {
	Backend           string                    `json:"backend"`
	Renderer          string                    `json:"renderer"`
	FallbackReason    string                    `json:"fallbackReason"`
	Implementation    string                    `json:"implementation"`
	GPU               bool                      `json:"gpu"`
	DeviceLost        bool                      `json:"deviceLost"`
	Adapter           string                    `json:"adapter"`
	AdapterInfo       map[string]interface{}    `json:"adapterInfo"`
	InitError         string                    `json:"initError"`
	LastError         string                    `json:"lastError"`
	FrameSeq          int                       `json:"frameSeq"`
	ShaderErrors      int                       `json:"shaderErrors"`
	ShaderDiagnostics ShaderDiagnosticsEvidence `json:"shaderDiagnostics"`
	LastShaderError   string                    `json:"lastShaderError"`
	Post              PostEvidence              `json:"post"`
	Parsed            bool                      `json:"parsed"`
}

type ShaderDiagnosticsEvidence struct {
	Messages  int    `json:"messages"`
	Errors    int    `json:"errors"`
	Failed    bool   `json:"failed"`
	LastError string `json:"lastError"`
}

type baselineCaptureKey struct {
	State string
	Index int
}

type ValidatedPixelBaseline struct {
	Manifest     PixelEvidenceManifest
	Captures     map[baselineCaptureKey]PixelCaptureEvidence
	Paths        map[baselineCaptureKey]string
	ThresholdPct float64
}

func (o *PixelEvidenceOptions) applyDefaults() {
	if o.Mode == "" {
		o.Mode = PixelModeRecordBaseline
	}
	if o.RouteID == "" {
		o.RouteID = "unknown"
	}
	if o.Samples <= 0 {
		o.Samples = 3
	}
	if o.Viewport.Width == 0 || o.Viewport.Height == 0 {
		o.Viewport = ViewportDesktop
	}
	if o.Viewport.Scale == 0 {
		o.Viewport.Scale = 1
	}
	if o.SettledWait == 0 {
		o.SettledWait = 3 * time.Second
	}
	if o.WarmupFrames == 0 {
		o.WarmupFrames = 30
	}
	if o.WaitSelector == "" {
		o.WaitSelector = "body"
	}
	if o.CanvasSelector == "" {
		o.CanvasSelector = "canvas[data-gosx-scene3d-canvas], [data-gosx-scene3d-mounted] canvas"
	}
	if o.Timeout == 0 {
		o.Timeout = 60 * time.Second
	}
}

func CapturePixelEvidence(ctx context.Context, url string, opts PixelEvidenceOptions) (PixelEvidenceManifest, error) {
	opts.applyDefaults()
	manifest := newPixelManifest(url, opts)
	if err := validatePixelOptions(opts); err != nil {
		manifest.Failures = append(manifest.Failures, err.Error())
		return manifest, err
	}
	if err := validatePixelThreshold(opts.ThresholdPct, true); err != nil {
		manifest.Failures = append(manifest.Failures, err.Error())
		return manifest, err
	}
	baselineThreshold := opts.ThresholdPct
	var baseline *ValidatedPixelBaseline
	if opts.Mode == PixelModeCandidateComparison {
		loadedBaseline, err := loadAndValidateBaselineManifest(opts.BaselineRoot, opts)
		if err != nil {
			manifest.Failures = append(manifest.Failures, err.Error())
			return manifest, err
		}
		baseline = &loadedBaseline
		baselineThreshold = loadedBaseline.ThresholdPct
		manifest.BaselineSource = &loadedBaseline.Manifest.Source
		manifest.SourceRelation = pixelSourceRelation(opts.Source, loadedBaseline.Manifest.Source)
		effective, err := effectiveCandidateThreshold(baselineThreshold, opts.ThresholdPct)
		if err != nil {
			manifest.Failures = append(manifest.Failures, err.Error())
			return manifest, err
		}
		opts.ThresholdPct = effective
	}
	manifest.Threshold = PixelThresholdEvidence{
		BaselinePct:  baselineThreshold,
		RequestedPct: manifest.Threshold.RequestedPct,
		EffectivePct: opts.ThresholdPct,
	}
	if err := preparePixelArtifactRoot(opts); err != nil {
		manifest.Failures = append(manifest.Failures, err.Error())
		return manifest, err
	}

	allocCtx, allocCancel, err := newAllocator(ctx)
	if err != nil {
		return manifest, fmt.Errorf("visual: allocator: %w", err)
	}
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	runCtx, runCancel := context.WithTimeout(browserCtx, opts.Timeout)
	defer runCancel()

	if err := navigatePixelPage(runCtx, url, opts, &manifest); err != nil {
		return manifest, err
	}

	initialFrame, err := waitForSceneReady(runCtx, opts, &manifest, 1)
	if err != nil {
		return writePixelManifestWithError(manifest, opts, err)
	}
	for _, state := range []struct {
		name     string
		wait     time.Duration
		minFrame int
	}{
		{name: "initial", wait: opts.InitialWait, minFrame: initialFrame},
		{name: "settled", wait: opts.SettledWait, minFrame: initialFrame + opts.WarmupFrames},
	} {
		if state.wait > 0 {
			if err := chromedp.Run(runCtx, chromedp.Sleep(state.wait)); err != nil {
				return writePixelManifestWithError(manifest, opts, fmt.Errorf("visual: wait %s: %w", state.name, err))
			}
		}
		if _, err := waitForSceneReady(runCtx, opts, &manifest, state.minFrame); err != nil {
			return writePixelManifestWithError(manifest, opts, fmt.Errorf("visual: %s readiness: %w", state.name, err))
		}
		stateEvidence := PixelStateEvidence{State: state.name}
		var withinRunBaseline []byte
		var withinRunBaselinePath string
		for i := 0; i < opts.Samples; i++ {
			capture, pngBytes, err := capturePixelEvidenceSample(runCtx, opts, &manifest, state.name, i)
			if err != nil {
				return writePixelManifestWithError(manifest, opts, err)
			}
			manifest.Selected = captureSelectedFromMeta(capture, manifest.Selected)
			manifest.HardwareClassification = mergeHardwareClass(manifest.HardwareClassification, capture.HardwareClass)
			manifest.Viewport = capture.Viewport
			if capture.UserAgent != "" {
				manifest.Browser.UserAgent = capture.UserAgent
			}
			manifest.BackendSelection.RuntimeObservedBackend = mergeObservedBackend(manifest.BackendSelection.RuntimeObservedBackend, capture.Backend)
			manifest.Failures = append(manifest.Failures, validateCapture(opts, capture)...)

			if opts.Mode == PixelModeCandidateComparison {
				comparison, err := compareAgainstStoredBaseline(*baseline, state.name, i, pngBytes, capture.Path, opts.ThresholdPct)
				if err != nil {
					return writePixelManifestWithError(manifest, opts, err)
				}
				capture.Comparison = &comparison
				if !comparison.Passed {
					manifest.Failures = append(manifest.Failures, fmt.Sprintf("%s sample %d diff %.3f%% exceeds %.3f%%", state.name, i, comparison.DiffPct, opts.ThresholdPct))
				}
			} else if i == 0 {
				withinRunBaseline = pngBytes
				withinRunBaselinePath = capture.Path
				capture.Comparison = &PixelComparison{BaselinePath: withinRunBaselinePath, Similarity: 1, BaselineThresholdPct: opts.ThresholdPct, EffectiveThresholdPct: opts.ThresholdPct, Passed: true}
			} else {
				comparison, err := ComparePixelEvidence(withinRunBaseline, pngBytes, withinRunBaselinePath, capture.Path, opts.ThresholdPct)
				if err != nil {
					return writePixelManifestWithError(manifest, opts, err)
				}
				capture.Comparison = &comparison
				if !comparison.Passed {
					manifest.Failures = append(manifest.Failures, fmt.Sprintf("%s sample %d within-run diff %.3f%% exceeds %.3f%%", state.name, i, comparison.DiffPct, opts.ThresholdPct))
				}
			}
			stateEvidence.Captures = append(stateEvidence.Captures, capture)
		}
		manifest.States = append(manifest.States, stateEvidence)
	}
	manifest.Failures = append(manifest.Failures, validateManifestCertification(opts, manifest)...)
	if opts.Mode == PixelModeCandidateComparison && baseline != nil {
		manifest.Failures = append(manifest.Failures, validateCandidateAgainstBaseline(manifest, baseline.Manifest)...)
	}
	manifest.Certified = len(manifest.Failures) == 0 && opts.Mode == PixelModeRecordBaseline
	manifest, err = portablePixelManifest(manifest, opts)
	if err != nil {
		return manifest, err
	}
	if err := writePixelManifest(manifest, opts); err != nil {
		return manifest, err
	}
	if len(manifest.Failures) > 0 {
		return manifest, fmt.Errorf("visual: O0.2 pixel evidence failed; see %s", pixelManifestPath(opts.ArtifactRoot))
	}
	return manifest, nil
}

func newPixelManifest(url string, opts PixelEvidenceOptions) PixelEvidenceManifest {
	return PixelEvidenceManifest{
		SchemaVersion:      OuroborosPixelSchemaVersion,
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		URL:                url,
		RouteID:            opts.RouteID,
		Mode:               string(opts.Mode),
		ArtifactRoot:       opts.ArtifactRoot,
		BaselineRoot:       opts.BaselineRoot,
		Source:             opts.Source,
		BackendRequirement: string(opts.Backend),
		BackendSelection: PixelBackendSelection{
			RequestedBackend:  string(opts.Backend),
			ForceWebGL:        opts.ForceWebGL,
			PreNavigationHook: pixelBackendSelectionHookName(opts),
		},
		Threshold: PixelThresholdEvidence{
			RequestedPct: opts.ThresholdPct,
			EffectivePct: opts.ThresholdPct,
		},
		States: []PixelStateEvidence{},
		Selected: SelectedSceneEvidence{
			CanvasSelector: opts.CanvasSelector,
		},
	}
}

func validatePixelOptions(opts PixelEvidenceOptions) error {
	if !ValidPixelEvidenceMode(string(opts.Mode)) {
		return fmt.Errorf("visual: --ouroboros-mode must be record-baseline or candidate")
	}
	if strings.TrimSpace(opts.ArtifactRoot) == "" {
		return fmt.Errorf("visual: pixel evidence requires ArtifactRoot")
	}
	if opts.Backend == RequireBackendNone {
		return fmt.Errorf("visual: O0.2 pixel evidence requires --require-backend webgpu, webgl, or any-gpu")
	}
	if opts.Backend != RequireBackendNone && !ValidRequireBackend(string(opts.Backend)) {
		return fmt.Errorf("visual: invalid backend requirement %q", opts.Backend)
	}
	if opts.Samples < 3 {
		return fmt.Errorf("visual: O0.2 pixel evidence requires at least 3 samples per state")
	}
	if opts.Mode == PixelModeCandidateComparison && strings.TrimSpace(opts.BaselineRoot) == "" {
		return fmt.Errorf("visual: candidate comparison requires BaselineRoot")
	}
	if err := validatePixelSourceIdentity("source", opts.Source); err != nil {
		return err
	}
	if opts.ForceWebGL && opts.Backend != RequireBackendWebGL {
		return fmt.Errorf("visual: --ouroboros-force-webgl requires --require-backend webgl")
	}
	return nil
}

func validatePixelSourceIdentity(label string, source PixelSourceIdentity) error {
	if !validGitRevision(source.BaseRevision) {
		return fmt.Errorf("visual: %s.baseRevision must be 7-40 lowercase hex characters", label)
	}
	if !validOverlayHash(source.OverlayHash) {
		return fmt.Errorf("visual: %s.overlayHash must be sha256:clean or sha256:<64 lowercase hex>", label)
	}
	if !validSHA256Ref(source.InventorySHA256) {
		return fmt.Errorf("visual: %s.inventorySHA256 must be sha256:<64 lowercase hex>", label)
	}
	return nil
}

func validGitRevision(value string) bool {
	if len(value) < 7 || len(value) > 40 {
		return false
	}
	return isLowerHex(value)
}

func validOverlayHash(value string) bool {
	if value == "sha256:clean" {
		return true
	}
	return validSHA256Ref(value)
}

func validSHA256Ref(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	hash := strings.TrimPrefix(value, prefix)
	return len(hash) == 64 && isLowerHex(hash)
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' {
			continue
		}
		return false
	}
	return true
}

func pixelSourceRelation(captured, baseline PixelSourceIdentity) string {
	if pixelSourceIdentityEqual(captured, baseline) {
		return "same-source"
	}
	return "candidate-compared-to-baseline"
}

func pixelSourceIdentityEqual(a, b PixelSourceIdentity) bool {
	return a.BaseRevision == b.BaseRevision &&
		a.OverlayHash == b.OverlayHash &&
		a.InventorySHA256 == b.InventorySHA256
}

func preparePixelArtifactRoot(opts PixelEvidenceOptions) error {
	if opts.Mode == PixelModeRecordBaseline && !opts.AllowOverwrite {
		if entries, err := os.ReadDir(opts.ArtifactRoot); err == nil && len(entries) > 0 {
			return fmt.Errorf("visual: record-baseline refuses to overwrite non-empty artifact root %s", opts.ArtifactRoot)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("visual: inspect artifact root: %w", err)
		}
	}
	if err := os.MkdirAll(opts.ArtifactRoot, 0o755); err != nil {
		return fmt.Errorf("visual: mkdir artifact root: %w", err)
	}
	return nil
}

func navigatePixelPage(ctx context.Context, url string, opts PixelEvidenceOptions, manifest *PixelEvidenceManifest) error {
	var product string
	var browserUserAgent string
	var flags []string
	selectionScript := pixelBackendSelectionScript(opts)
	actions := []chromedp.Action{
		chromedp.EmulateViewport(int64(opts.Viewport.Width), int64(opts.Viewport.Height), chromedp.EmulateScale(opts.Viewport.Scale)),
	}
	if selectionScript != "" {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(selectionScript).Do(ctx)
			return err
		}))
	}
	actions = append(actions,
		chromedp.Navigate(url),
		chromedp.WaitReady(opts.WaitSelector, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, gotProduct, _, gotUserAgent, _, err := browser.GetVersion().Do(ctx)
			if err != nil {
				return err
			}
			product = gotProduct
			browserUserAgent = gotUserAgent
			gotFlags, err := browser.GetBrowserCommandLine().Do(ctx)
			if err == nil {
				flags = gotFlags
			}
			return nil
		}),
	)
	if err := chromedp.Run(ctx,
		actions...,
	); err != nil {
		return fmt.Errorf("visual: navigate %s: %w", url, err)
	}
	manifest.Browser.Product = product
	manifest.Browser.UserAgent = browserUserAgent
	manifest.Browser.Flags = flags
	return nil
}

func waitForSceneReady(ctx context.Context, opts PixelEvidenceOptions, manifest *PixelEvidenceManifest, minFrame int) (int, error) {
	deadline := time.Now().Add(opts.Timeout)
	var last pagePixelMetadata
	for time.Now().Before(deadline) {
		meta, err := readPixelMetadata(ctx, opts)
		if err == nil {
			last = meta
			recordRuntimeObservedBackend(manifest, meta)
			if mismatch := runtimeObservedBackendMismatch(opts, meta); mismatch != "" {
				return 0, fmt.Errorf("visual: %s", mismatch)
			}
			if len(meta.Errors) == 0 && meta.Truth.FrameSeq >= minFrame && meta.Truth.Backend != "" {
				if err := validateSelectedMeta(opts, meta); err == nil {
					return meta.Truth.FrameSeq, nil
				}
			}
		}
		if err := chromedp.Run(ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
			return 0, err
		}
	}
	return 0, fmt.Errorf("visual: scene did not reach frame %d for selector %q; last frame=%d errors=%v", minFrame, opts.CanvasSelector, last.Truth.FrameSeq, last.Errors)
}

func recordRuntimeObservedBackend(manifest *PixelEvidenceManifest, meta pagePixelMetadata) {
	if manifest == nil || !meta.Truth.Parsed || meta.Truth.Backend == "" {
		return
	}
	manifest.BackendSelection.RuntimeObservedBackend = mergeObservedBackend(manifest.BackendSelection.RuntimeObservedBackend, meta.Truth.Backend)
}

func runtimeObservedBackendMismatch(opts PixelEvidenceOptions, meta pagePixelMetadata) string {
	if !meta.Truth.Parsed || meta.Truth.Backend == "" || opts.Backend == RequireBackendAnyGPU {
		return ""
	}
	if opts.Backend != RequireBackendNone && string(opts.Backend) != meta.Truth.Backend {
		return fmt.Sprintf("runtime observed backend=%s, requested backend=%s", meta.Truth.Backend, opts.Backend)
	}
	return ""
}

func readPixelMetadata(ctx context.Context, opts PixelEvidenceOptions) (pagePixelMetadata, error) {
	var meta pagePixelMetadata
	expr := fmt.Sprintf(pixelMetadataProbeJS, jsLiteral(opts.CanvasSelector))
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &meta, func(p *cdpRuntime.EvaluateParams) *cdpRuntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		return meta, err
	}
	return meta, nil
}

func capturePixelEvidenceSample(ctx context.Context, opts PixelEvidenceOptions, manifest *PixelEvidenceManifest, state string, index int) (PixelCaptureEvidence, []byte, error) {
	meta, err := readPixelMetadata(ctx, opts)
	if err != nil {
		return PixelCaptureEvidence{}, nil, fmt.Errorf("visual: metadata %s sample %d: %w", state, index, err)
	}
	recordRuntimeObservedBackend(manifest, meta)
	if mismatch := runtimeObservedBackendMismatch(opts, meta); mismatch != "" {
		return PixelCaptureEvidence{}, nil, fmt.Errorf("visual: %s sample %d: %s", state, index, mismatch)
	}
	if err := validateSelectedMeta(opts, meta); err != nil {
		return PixelCaptureEvidence{}, nil, err
	}
	var pngBytes []byte
	if err := chromedp.Run(ctx, chromedp.Screenshot(opts.CanvasSelector, &pngBytes, chromedp.NodeVisible, chromedp.ByQuery)); err != nil {
		return PixelCaptureEvidence{}, nil, fmt.Errorf("visual: capture %s sample %d: %w", state, index, err)
	}
	if len(pngBytes) == 0 {
		return PixelCaptureEvidence{}, nil, fmt.Errorf("visual: capture %s sample %d: empty screenshot", state, index)
	}
	hash := sha256.Sum256(pngBytes)
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return PixelCaptureEvidence{}, nil, fmt.Errorf("visual: decode %s sample %d: %w", state, index, err)
	}
	rect := img.Bounds()
	placeholder := ImageBlankOrPlaceholder(img)
	backend := firstNonEmpty(meta.Truth.Backend, meta.Mount.Backend)
	renderer := firstNonEmpty(meta.Truth.Renderer, meta.Mount.Renderer)
	fallback := firstNonEmpty(meta.Truth.FallbackReason, meta.Mount.FallbackReason)
	software := isSoftwareRaster(meta.WebGL.Renderer, meta.WebGPU.AdapterName, meta.WebGPU.Vendor, meta.WebGPU.Description, meta.Truth.Adapter, renderer, meta.Truth.Implementation)
	class := classifyHardware(backend, software, meta.WebGPU.Available, meta.Truth.GPU)
	meta.Canvas.Width = opts.Viewport.Width
	meta.Canvas.Height = opts.Viewport.Height
	meta.Canvas.DPR = meta.DevicePixelRatio
	meta.Canvas.EffectiveDPR = meta.EffectiveDPR
	if meta.Canvas.EffectiveDPR == 0 && meta.Canvas.CanvasCSSWidth > 0 {
		meta.Canvas.EffectiveDPR = float64(meta.Canvas.CanvasWidth) / meta.Canvas.CanvasCSSWidth
	}
	if meta.Canvas.EffectiveDPR == 0 {
		meta.Canvas.EffectiveDPR = opts.Viewport.Scale
	}
	path := filepath.Join(opts.ArtifactRoot, fmt.Sprintf("%s-%s-%02d.png", safeName(opts.RouteID), state, index))
	if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
		return PixelCaptureEvidence{}, nil, fmt.Errorf("visual: write %s: %w", path, err)
	}
	return PixelCaptureEvidence{
		Index:              index,
		Path:               path,
		SHA256:             hex.EncodeToString(hash[:]),
		Bytes:              len(pngBytes),
		Width:              rect.Dx(),
		Height:             rect.Dy(),
		Blank:              placeholder,
		Placeholder:        placeholder,
		Backend:            backend,
		Renderer:           renderer,
		FallbackReason:     fallback,
		Implementation:     meta.Truth.Implementation,
		RuntimeTruthParsed: meta.Truth.Parsed,
		RuntimeGPU:         meta.Truth.GPU,
		DeviceLost:         meta.Truth.DeviceLost,
		RendererFailure:    rendererFailure(meta.Truth),
		SoftwareRaster:     software,
		HardwareClass:      class,
		FrameSeq:           meta.Truth.FrameSeq,
		Post:               meta.Truth.Post,
		ShaderDiagnostics:  meta.Truth.ShaderDiagnostics,
		WebGPU:             mergeWebGPU(meta.WebGPU, meta.Truth),
		WebGL:              meta.WebGL,
		Viewport:           meta.Canvas,
		Selected:           meta.Selected,
		UserAgent:          meta.UserAgent,
		CapturedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}, pngBytes, nil
}

func validateSelectedMeta(opts PixelEvidenceOptions, meta pagePixelMetadata) error {
	if len(meta.Errors) > 0 {
		return fmt.Errorf("visual: selected canvas invalid: %s", strings.Join(meta.Errors, "; "))
	}
	if meta.Selected.CanvasCount != 1 {
		return fmt.Errorf("visual: selector %q matched %d canvases, want exactly 1", opts.CanvasSelector, meta.Selected.CanvasCount)
	}
	if meta.Selected.MountCount != 1 || meta.Selected.MountID == "" {
		return fmt.Errorf("visual: selected canvas is not bound to exactly one Scene3D mount")
	}
	if !meta.Truth.Parsed {
		return fmt.Errorf("visual: selected mount has no parsed GoSX backend truth")
	}
	if !meta.Truth.GPU {
		return fmt.Errorf("visual: selected mount backend truth did not report gpu=true")
	}
	if meta.Truth.Implementation == "" {
		return fmt.Errorf("visual: selected mount backend truth has no implementation identity")
	}
	if meta.Truth.FallbackReason != "" {
		return fmt.Errorf("visual: selected mount has fallback reason %q", meta.Truth.FallbackReason)
	}
	return checkBackendRequirement("O0.2 pixel capture", opts.Backend, []sceneMountBackend{meta.Mount})
}

func validateCapture(opts PixelEvidenceOptions, capture PixelCaptureEvidence) []string {
	var failures []string
	failures = append(failures, validateCertifiedCapture(opts, capture)...)
	return failures
}

func validateCertifiedCapture(opts PixelEvidenceOptions, capture PixelCaptureEvidence) []string {
	var failures []string
	if capture.SHA256 == "" || len(capture.SHA256) != 64 {
		failures = append(failures, fmt.Sprintf("%s sample %d has invalid SHA-256", capture.Path, capture.Index))
	}
	if _, err := os.Stat(capture.Path); err != nil {
		failures = append(failures, fmt.Sprintf("%s is missing: %v", capture.Path, err))
	}
	if capture.Placeholder || capture.Blank {
		failures = append(failures, fmt.Sprintf("%s is blank or near-uniform", capture.Path))
	}
	if capture.DeviceLost {
		failures = append(failures, fmt.Sprintf("%s observed device loss", capture.Path))
	}
	if capture.RendererFailure {
		failures = append(failures, fmt.Sprintf("%s observed renderer failure", capture.Path))
	}
	if !capture.RuntimeTruthParsed {
		failures = append(failures, fmt.Sprintf("%s has no parsed GoSX backend truth", capture.Path))
	}
	if !capture.RuntimeGPU {
		failures = append(failures, fmt.Sprintf("%s runtime backend truth did not report gpu=true", capture.Path))
	}
	if capture.Implementation == "" {
		failures = append(failures, fmt.Sprintf("%s has no renderer implementation identity", capture.Path))
	}
	if capture.FallbackReason != "" {
		failures = append(failures, fmt.Sprintf("%s has runtime fallback reason %q", capture.Path, capture.FallbackReason))
	}
	if capture.ShaderDiagnostics.Errors > 0 || capture.ShaderDiagnostics.Failed || capture.ShaderDiagnostics.LastError != "" {
		failures = append(failures, fmt.Sprintf("%s has shader diagnostics errors", capture.Path))
	}
	if capture.SoftwareRaster || capture.HardwareClass == "software-raster" || capture.HardwareClass == "headless-logic" {
		failures = append(failures, fmt.Sprintf("%s is not real hardware: %s", capture.Path, capture.HardwareClass))
	}
	if capture.Backend == "webgpu" {
		if capture.WebGPU.Fallback {
			failures = append(failures, fmt.Sprintf("%s used a WebGPU fallback adapter", capture.Path))
		}
		if capture.WebGPU.FallbackReason != "" {
			failures = append(failures, fmt.Sprintf("%s WebGPU adapter fallback reason: %s", capture.Path, capture.WebGPU.FallbackReason))
		}
	}
	if opts.Backend != RequireBackendAnyGPU && string(opts.Backend) != capture.Backend {
		failures = append(failures, fmt.Sprintf("%s backend=%s, want %s", capture.Path, capture.Backend, opts.Backend))
	}
	if opts.Backend == RequireBackendAnyGPU && capture.Backend != "webgpu" && capture.Backend != "webgl" {
		failures = append(failures, fmt.Sprintf("%s backend=%s, want GPU backend", capture.Path, capture.Backend))
	}
	if capture.FrameSeq <= 0 {
		failures = append(failures, fmt.Sprintf("%s has no rendered frame sequence", capture.Path))
	}
	return failures
}

func validateStoredCaptureComparison(manifest PixelEvidenceManifest, capture PixelCaptureEvidence) []string {
	var failures []string
	if capture.Comparison == nil {
		return []string{capture.Path + " has no stored within-run comparison"}
	}
	if !capture.Comparison.Passed {
		failures = append(failures, capture.Path+" stored comparison did not pass")
	}
	if capture.Comparison.EffectiveThresholdPct != manifest.Threshold.EffectivePct {
		failures = append(failures, capture.Path+" comparison threshold does not match manifest")
	}
	if capture.Comparison.EffectiveThresholdPct < 0 || capture.Comparison.EffectiveThresholdPct > MaxCanonicalPixelThresholdPct || math.IsNaN(capture.Comparison.EffectiveThresholdPct) || math.IsInf(capture.Comparison.EffectiveThresholdPct, 0) {
		failures = append(failures, capture.Path+" comparison threshold is invalid")
	}
	return failures
}

func validateManifestCertification(opts PixelEvidenceOptions, manifest PixelEvidenceManifest) []string {
	var failures []string
	if opts.Mode != PixelModeRecordBaseline {
		return failures
	}
	if manifest.HardwareClassification != "hardware-webgpu" && manifest.HardwareClassification != "hardware-webgl" && manifest.HardwareClassification != "mixed-hardware" {
		failures = append(failures, "record-baseline did not run on certified hardware")
	}
	if opts.Backend == RequireBackendNone {
		failures = append(failures, "record-baseline requires explicit backend")
	}
	if len(manifest.States) != 2 {
		failures = append(failures, "record-baseline requires initial and settled states")
	}
	for _, want := range []string{"initial", "settled"} {
		state := findPixelState(manifest, want)
		if state == nil {
			failures = append(failures, "missing "+want+" state")
			continue
		}
		if len(state.Captures) < 3 {
			failures = append(failures, fmt.Sprintf("%s has %d captures, want at least 3", want, len(state.Captures)))
		}
		for _, capture := range state.Captures {
			if capture.Comparison != nil && !capture.Comparison.Passed {
				failures = append(failures, fmt.Sprintf("%s comparison failed for %s", want, capture.Path))
			}
		}
	}
	return failures
}

func compareAgainstStoredBaseline(baseline ValidatedPixelBaseline, state string, index int, current []byte, currentPath string, effectiveThreshold float64) (PixelComparison, error) {
	key := baselineCaptureKey{State: state, Index: index}
	baselinePath, ok := baseline.Paths[key]
	if !ok {
		return PixelComparison{}, fmt.Errorf("visual: baseline has no capture for %s/%d", state, index)
	}
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		return PixelComparison{}, fmt.Errorf("visual: read pixel baseline %s: %w", baselinePath, err)
	}
	return ComparePixelEvidenceWithThresholds(data, current, baselinePath, currentPath, baseline.ThresholdPct, effectiveThreshold)
}

func loadBaselineThreshold(root string) (float64, error) {
	manifest, err := readPixelManifest(root)
	if err != nil {
		return 0, err
	}
	if err := validatePixelThreshold(manifest.Threshold.EffectivePct, false); err != nil {
		return 0, err
	}
	return manifest.Threshold.EffectivePct, nil
}

func readPixelManifest(root string) (PixelEvidenceManifest, error) {
	path, err := containedBaselineManifestPath(root, pixelManifestPath(root))
	if err != nil {
		return PixelEvidenceManifest{}, err
	}
	return readPixelManifestPath(path)
}

func readPixelManifestPath(path string) (PixelEvidenceManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return PixelEvidenceManifest{}, fmt.Errorf("visual: read baseline pixel manifest %s: %w", path, err)
	}
	if err := requirePixelManifestThreshold(body); err != nil {
		return PixelEvidenceManifest{}, err
	}
	manifest, err := decodePixelManifest(body, false)
	if err != nil {
		return PixelEvidenceManifest{}, err
	}
	return manifest, nil
}

func readStrictPixelManifestPath(path string) (PixelEvidenceManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return PixelEvidenceManifest{}, fmt.Errorf("visual: read baseline pixel manifest %s: %w", path, err)
	}
	if err := requirePixelManifestThreshold(body); err != nil {
		return PixelEvidenceManifest{}, err
	}
	manifest, err := decodePixelManifest(body, true)
	if err != nil {
		return PixelEvidenceManifest{}, err
	}
	return manifest, nil
}

func requirePixelManifestThreshold(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("visual: parse baseline pixel manifest: %w", err)
	}
	if _, ok := raw["threshold"]; !ok {
		return fmt.Errorf("visual: baseline pixel manifest does not define a route threshold")
	}
	return nil
}

func decodePixelManifest(body []byte, strict bool) (PixelEvidenceManifest, error) {
	var manifest PixelEvidenceManifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&manifest); err != nil {
		return PixelEvidenceManifest{}, fmt.Errorf("visual: parse baseline pixel manifest: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return PixelEvidenceManifest{}, fmt.Errorf("visual: parse baseline pixel manifest: trailing JSON after manifest")
		}
		return PixelEvidenceManifest{}, fmt.Errorf("visual: parse baseline pixel manifest: %w", err)
	}
	return manifest, nil
}

// ValidateCanonicalPixelBaselineManifest validates a stored O0.2 pixel baseline
// and replays its PNG evidence checks against the expected source identity.
func ValidateCanonicalPixelBaselineManifest(manifestPath string, expectedSource PixelSourceIdentity, opts PixelEvidenceOptions) (ValidatedPixelBaseline, error) {
	if filepath.Base(manifestPath) != "pixel-evidence.json" {
		return ValidatedPixelBaseline{}, fmt.Errorf("visual: canonical pixel manifest must be named pixel-evidence.json")
	}
	if err := validatePixelSourceIdentity("expected source", expectedSource); err != nil {
		return ValidatedPixelBaseline{}, err
	}
	root := filepath.Dir(manifestPath)
	path, err := containedBaselineManifestPath(root, manifestPath)
	if err != nil {
		return ValidatedPixelBaseline{}, err
	}
	manifest, err := readStrictPixelManifestPath(path)
	if err != nil {
		return ValidatedPixelBaseline{}, err
	}
	validated, failures := validateCanonicalBaselineManifest(root, opts, manifest)
	if !pixelSourceIdentityEqual(manifest.Source, expectedSource) {
		failures = append(failures, "baseline source does not match expected source")
	}
	if len(failures) > 0 {
		return validated, fmt.Errorf("visual: invalid O0.2 pixel baseline: %s", strings.Join(failures, "; "))
	}
	return validated, nil
}

func loadAndValidateBaselineManifest(root string, opts PixelEvidenceOptions) (ValidatedPixelBaseline, error) {
	path, err := containedBaselineManifestPath(root, pixelManifestPath(root))
	if err != nil {
		return ValidatedPixelBaseline{}, err
	}
	manifest, err := readStrictPixelManifestPath(path)
	if err != nil {
		return ValidatedPixelBaseline{}, err
	}
	validated, failures := validateCanonicalBaselineManifest(root, opts, manifest)
	if len(failures) > 0 {
		return validated, fmt.Errorf("visual: invalid O0.2 pixel baseline: %s", strings.Join(failures, "; "))
	}
	return validated, nil
}

func validatePixelThreshold(value float64, allowUnset bool) error {
	if allowUnset && value == 0 {
		return nil
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("visual: pixel threshold must be finite")
	}
	if value < 0 {
		return fmt.Errorf("visual: pixel threshold must not be negative")
	}
	if value > MaxCanonicalPixelThresholdPct {
		return fmt.Errorf("visual: pixel threshold %.3f exceeds hard maximum %.3f", value, MaxCanonicalPixelThresholdPct)
	}
	return nil
}

func effectiveCandidateThreshold(baseline, requested float64) (float64, error) {
	if err := validatePixelThreshold(baseline, false); err != nil {
		return 0, fmt.Errorf("visual: invalid baseline pixel threshold: %w", err)
	}
	if err := validatePixelThreshold(requested, true); err != nil {
		return 0, fmt.Errorf("visual: invalid requested pixel threshold: %w", err)
	}
	if requested > baseline {
		return baseline, nil
	}
	return requested, nil
}

func validateCanonicalBaselineManifest(root string, opts PixelEvidenceOptions, manifest PixelEvidenceManifest) (ValidatedPixelBaseline, []string) {
	var failures []string
	validated := ValidatedPixelBaseline{
		Manifest:     manifest,
		Captures:     map[baselineCaptureKey]PixelCaptureEvidence{},
		Paths:        map[baselineCaptureKey]string{},
		ThresholdPct: manifest.Threshold.EffectivePct,
	}
	if manifest.SchemaVersion != OuroborosPixelSchemaVersion {
		failures = append(failures, "schemaVersion is not "+OuroborosPixelSchemaVersion)
	}
	if manifest.Mode != string(PixelModeRecordBaseline) {
		failures = append(failures, "mode is not record-baseline")
	}
	if !manifest.Certified {
		failures = append(failures, "baseline is not certified")
	}
	if manifest.RouteID != opts.RouteID {
		failures = append(failures, fmt.Sprintf("routeID=%q, want %q", manifest.RouteID, opts.RouteID))
	}
	if err := validatePixelSourceIdentity("baseline source", manifest.Source); err != nil {
		failures = append(failures, err.Error())
	}
	if manifest.BaselineSource != nil {
		failures = append(failures, "record-baseline must not define baselineSource")
	}
	if manifest.SourceRelation != "" {
		failures = append(failures, "record-baseline must not define sourceRelation")
	}
	if manifest.BackendRequirement != string(opts.Backend) {
		failures = append(failures, fmt.Sprintf("backendRequirement=%q, want %q", manifest.BackendRequirement, opts.Backend))
	}
	if manifest.BackendSelection.RequestedBackend == "" {
		failures = append(failures, "backendSelection requestedBackend is empty")
	} else if manifest.BackendSelection.RequestedBackend != string(opts.Backend) {
		failures = append(failures, fmt.Sprintf("backendSelection requestedBackend=%q, want %q", manifest.BackendSelection.RequestedBackend, opts.Backend))
	}
	if manifest.BackendSelection.RuntimeObservedBackend == "" {
		failures = append(failures, "backendSelection runtimeObservedBackend is empty")
	} else if manifest.BackendSelection.RuntimeObservedBackend != string(opts.Backend) {
		failures = append(failures, fmt.Sprintf("backendSelection observedBackend=%q, want %q", manifest.BackendSelection.RuntimeObservedBackend, opts.Backend))
	}
	if manifest.BackendSelection.RequestedBackend != "" && manifest.BackendSelection.RuntimeObservedBackend != "" && manifest.BackendSelection.RequestedBackend != manifest.BackendSelection.RuntimeObservedBackend {
		failures = append(failures, "backendSelection requestedBackend does not match runtimeObservedBackend")
	}
	expectedHook := pixelBackendSelectionHookName(opts)
	if manifest.BackendSelection.PreNavigationHook == "" {
		failures = append(failures, "backendSelection preNavigationHook is empty")
	} else if manifest.BackendSelection.PreNavigationHook != expectedHook {
		failures = append(failures, fmt.Sprintf("backendSelection preNavigationHook=%q, want %q", manifest.BackendSelection.PreNavigationHook, expectedHook))
	}
	if manifest.BackendSelection.ForceWebGL != opts.ForceWebGL {
		failures = append(failures, fmt.Sprintf("backendSelection forceWebGL=%v, want %v", manifest.BackendSelection.ForceWebGL, opts.ForceWebGL))
	}
	if manifest.BackendSelection.ForceWebGL && opts.Backend != RequireBackendWebGL {
		failures = append(failures, "baseline forceWebGL is incompatible with candidate backend")
	}
	if manifest.Selected.CanvasSelector != opts.CanvasSelector {
		failures = append(failures, "canvas selector does not match candidate")
	}
	if manifest.Selected.MountID == "" || manifest.Selected.MountCount != 1 || manifest.Selected.CanvasCount != 1 {
		failures = append(failures, "baseline mount identity is incomplete")
	}
	if manifest.Viewport.Width != opts.Viewport.Width || manifest.Viewport.Height != opts.Viewport.Height || manifest.Viewport.DPR != opts.Viewport.Scale {
		failures = append(failures, "viewport or DPR policy does not match candidate")
	}
	if manifest.HardwareClassification != "hardware-webgpu" && manifest.HardwareClassification != "hardware-webgl" && manifest.HardwareClassification != "mixed-hardware" {
		failures = append(failures, "baseline hardware class is not certified hardware")
	}
	if len(manifest.Failures) > 0 {
		failures = append(failures, "baseline contains failures")
	}
	if err := validatePixelThreshold(manifest.Threshold.EffectivePct, false); err != nil {
		failures = append(failures, err.Error())
	}
	stateCounts := map[string]int{}
	for _, state := range manifest.States {
		stateCounts[state.State]++
		switch state.State {
		case "initial", "settled":
		default:
			failures = append(failures, "baseline has unexpected state "+state.State)
		}
	}
	if len(manifest.States) != 2 {
		failures = append(failures, fmt.Sprintf("baseline has %d state records, want exactly 2", len(manifest.States)))
	}
	for _, want := range []string{"initial", "settled"} {
		if stateCounts[want] != 1 {
			failures = append(failures, fmt.Sprintf("baseline has %d %s state records, want exactly 1", stateCounts[want], want))
		}
	}
	for _, stateName := range []string{"initial", "settled"} {
		state := findPixelState(manifest, stateName)
		if state == nil {
			failures = append(failures, "baseline missing "+stateName+" state")
			continue
		}
		if len(state.Captures) < 3 {
			failures = append(failures, fmt.Sprintf("baseline %s has %d captures, want at least 3", stateName, len(state.Captures)))
		}
		for _, capture := range state.Captures {
			key := baselineCaptureKey{State: stateName, Index: capture.Index}
			if capture.Index < 0 || capture.Index >= len(state.Captures) {
				failures = append(failures, fmt.Sprintf("baseline %s capture has invalid index %d", stateName, capture.Index))
			}
			if _, ok := validated.Captures[key]; ok {
				failures = append(failures, fmt.Sprintf("baseline duplicate capture key %s/%d", stateName, capture.Index))
			}
			path, captureFailures := validateBaselineCapture(root, opts, manifest, stateName, capture)
			failures = append(failures, captureFailures...)
			if len(captureFailures) == 0 {
				validated.Captures[key] = capture
				validated.Paths[key] = path
			}
		}
		for i := 0; i < len(state.Captures); i++ {
			if _, ok := validated.Captures[baselineCaptureKey{State: stateName, Index: i}]; !ok {
				failures = append(failures, fmt.Sprintf("baseline missing capture key %s/%d", stateName, i))
			}
		}
		failures = append(failures, validateStoredStateDrift(stateName, manifest.Threshold.EffectivePct, validated.Paths)...)
	}
	if duplicate := duplicateBaselinePath(validated.Paths); duplicate != "" {
		failures = append(failures, "baseline duplicate PNG path "+duplicate)
	}
	return validated, failures
}

func validateCandidateAgainstBaseline(candidate PixelEvidenceManifest, baseline PixelEvidenceManifest) []string {
	var failures []string
	if candidate.RouteID != baseline.RouteID {
		failures = append(failures, "candidate route does not match baseline")
	}
	if candidate.BackendRequirement != baseline.BackendRequirement {
		failures = append(failures, "candidate backend policy does not match baseline")
	}
	if candidate.BackendSelection.RequestedBackend != baseline.BackendSelection.RequestedBackend {
		failures = append(failures, "candidate requested backend does not match baseline")
	}
	if candidate.BackendSelection.RuntimeObservedBackend != baseline.BackendSelection.RuntimeObservedBackend {
		failures = append(failures, "candidate observed backend does not match baseline")
	}
	if candidate.BackendSelection.PreNavigationHook != baseline.BackendSelection.PreNavigationHook {
		failures = append(failures, "candidate pre-navigation hook does not match baseline")
	}
	if candidate.BackendSelection.ForceWebGL != baseline.BackendSelection.ForceWebGL {
		failures = append(failures, "candidate forceWebGL does not match baseline")
	}
	if err := validatePixelSourceIdentity("candidate source", candidate.Source); err != nil {
		failures = append(failures, err.Error())
	}
	if err := validatePixelSourceIdentity("baseline source", baseline.Source); err != nil {
		failures = append(failures, err.Error())
	}
	if candidate.BaselineSource == nil {
		failures = append(failures, "candidate baselineSource is empty")
	} else if !pixelSourceIdentityEqual(*candidate.BaselineSource, baseline.Source) {
		failures = append(failures, "candidate baselineSource does not match baseline source")
	}
	if candidate.SourceRelation != pixelSourceRelation(candidate.Source, baseline.Source) {
		failures = append(failures, "candidate sourceRelation does not match captured and baseline sources")
	}
	if candidate.Selected.CanvasSelector != baseline.Selected.CanvasSelector {
		failures = append(failures, "candidate canvas selector does not match baseline")
	}
	if candidate.Selected.MountID != "" && baseline.Selected.MountID != "" && candidate.Selected.MountID != baseline.Selected.MountID {
		failures = append(failures, "candidate mount identity does not match baseline")
	}
	if candidate.Viewport.Width != baseline.Viewport.Width || candidate.Viewport.Height != baseline.Viewport.Height {
		failures = append(failures, "candidate viewport does not match baseline")
	}
	if candidate.Viewport.DPR != baseline.Viewport.DPR {
		failures = append(failures, "candidate DPR does not match baseline")
	}
	if candidate.HardwareClassification != baseline.HardwareClassification {
		failures = append(failures, "candidate hardware class does not match baseline")
	}
	return failures
}

func validateBaselineCapture(root string, opts PixelEvidenceOptions, manifest PixelEvidenceManifest, stateName string, capture PixelCaptureEvidence) (string, []string) {
	var failures []string
	failures = append(failures, validateCertifiedCapture(opts, capture)...)
	failures = append(failures, validateStoredCaptureComparison(manifest, capture)...)
	if capture.Selected.MountID != manifest.Selected.MountID {
		failures = append(failures, capture.Path+" mount identity does not match manifest")
	}
	if capture.Selected.CanvasSelector != "" && capture.Selected.CanvasSelector != manifest.Selected.CanvasSelector {
		failures = append(failures, capture.Path+" canvas selector does not match manifest")
	}
	path, err := containedBaselinePNGPath(root, capture.Path)
	if err != nil {
		failures = append(failures, err.Error())
		return "", failures
	}
	if failure := validateCaptureFilenamePolicy(manifest.RouteID, stateName, capture.Index, path); failure != "" {
		failures = append(failures, failure)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		failures = append(failures, fmt.Sprintf("read %s: %v", path, err))
		return "", failures
	}
	sum := sha256.Sum256(data)
	if capture.SHA256 != hex.EncodeToString(sum[:]) {
		failures = append(failures, path+" hash does not match manifest")
	}
	if capture.Bytes != len(data) {
		failures = append(failures, fmt.Sprintf("%s byte count does not match manifest", path))
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		failures = append(failures, path+" is not a valid PNG")
		return path, failures
	}
	rect := img.Bounds()
	if rect.Dx() != capture.Width || rect.Dy() != capture.Height {
		failures = append(failures, path+" dimensions do not match manifest")
	}
	if ImageBlankOrPlaceholder(img) {
		failures = append(failures, path+" actual pixels are blank or placeholder")
	}
	return path, failures
}

func containedBaselinePNGPath(root, recorded string) (string, error) {
	return containedBaselineFilePath(root, recorded, ".png", "baseline PNG path")
}

func containedBaselineManifestPath(root, recorded string) (string, error) {
	return containedBaselineFilePath(root, recorded, ".json", "baseline pixel manifest path")
}

func containedBaselineFilePath(root, recorded string, ext string, label string) (string, error) {
	if recorded == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve baseline root %s: %w", root, err)
	}
	var candidate string
	if filepath.IsAbs(recorded) {
		candidate = filepath.Clean(recorded)
	} else {
		cleaned := filepath.Clean(recorded)
		if strings.HasPrefix(cleaned, filepath.Clean(root)+string(os.PathSeparator)) {
			candidate, err = filepath.Abs(cleaned)
		} else {
			candidate, err = filepath.Abs(filepath.Join(root, cleaned))
		}
		if err != nil {
			return "", err
		}
	}
	if filepath.Ext(candidate) != ext {
		return "", fmt.Errorf("%s has wrong extension: %s", label, recorded)
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %s %s: %w", label, recorded, err)
	}
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s escapes baseline root: %s", label, recorded)
	}
	return realCandidate, nil
}

func duplicateBaselinePath(paths map[baselineCaptureKey]string) string {
	seen := map[string]baselineCaptureKey{}
	for key, path := range paths {
		if prior, ok := seen[path]; ok && prior != key {
			return path
		}
		seen[path] = key
	}
	return ""
}

func validateStoredStateDrift(stateName string, threshold float64, paths map[baselineCaptureKey]string) []string {
	var failures []string
	basePath, ok := paths[baselineCaptureKey{State: stateName, Index: 0}]
	if !ok {
		return failures
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		return []string{fmt.Sprintf("read baseline replay %s: %v", basePath, err)}
	}
	for i := 1; ; i++ {
		path, ok := paths[baselineCaptureKey{State: stateName, Index: i}]
		if !ok {
			break
		}
		current, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("read baseline replay %s: %v", path, err))
			continue
		}
		comparison, err := ComparePixelEvidenceWithThresholds(base, current, basePath, path, threshold, threshold)
		if err != nil {
			failures = append(failures, fmt.Sprintf("baseline replay compare %s/%d: %v", stateName, i, err))
			continue
		}
		if !comparison.Passed {
			failures = append(failures, fmt.Sprintf("baseline replay %s/%d drift %.3f%% exceeds %.3f%%", stateName, i, comparison.DiffPct, threshold))
		}
	}
	return failures
}

func validateCaptureFilenamePolicy(routeID, state string, index int, path string) string {
	name := filepath.Base(path)
	expected := fmt.Sprintf("%s-%s-%02d.png", safeName(routeID), state, index)
	if name != expected {
		return fmt.Sprintf("%s filename does not match state/index policy %s", path, expected)
	}
	return ""
}

func ComparePixelEvidence(baseline, current []byte, baselinePath, currentPath string, thresholdPct float64) (PixelComparison, error) {
	return ComparePixelEvidenceWithThresholds(baseline, current, baselinePath, currentPath, thresholdPct, thresholdPct)
}

func ComparePixelEvidenceWithThresholds(baseline, current []byte, baselinePath, currentPath string, baselineThresholdPct, effectiveThresholdPct float64) (PixelComparison, error) {
	result, err := Diff(baseline, current)
	if err != nil {
		return PixelComparison{}, err
	}
	passed := result.DiffPct <= effectiveThresholdPct
	comparison := PixelComparison{
		BaselinePath:          baselinePath,
		DiffPct:               result.DiffPct,
		Mismatched:            result.Mismatched,
		Total:                 result.Total,
		DimensionsMatch:       result.DimensionsMatch,
		Similarity:            1 - (result.DiffPct / 100),
		BaselineThresholdPct:  baselineThresholdPct,
		EffectiveThresholdPct: effectiveThresholdPct,
		Passed:                passed,
	}
	if !passed {
		comparison.DiffPath = strings.TrimSuffix(currentPath, ".png") + ".diff.png"
		if err := writePNG(comparison.DiffPath, result.Diff); err != nil {
			return PixelComparison{}, fmt.Errorf("visual: write evidence diff: %w", err)
		}
	}
	return comparison, nil
}

func ImageBlankOrPlaceholder(img image.Image) bool {
	stats := imageStats(img)
	if stats.total == 0 || stats.opaque == 0 {
		return true
	}
	if stats.unique <= 2 {
		return true
	}
	if stats.unique < 12 && (stats.varR+stats.varG+stats.varB) < 18 {
		return true
	}
	if (stats.varR + stats.varG + stats.varB) < 4 {
		return true
	}
	return false
}

type pixelStats struct {
	total  int
	opaque int
	unique int
	varR   float64
	varG   float64
	varB   float64
}

func imageStats(img image.Image) pixelStats {
	rect := img.Bounds()
	total := rect.Dx() * rect.Dy()
	if total <= 0 {
		return pixelStats{}
	}
	colors := map[uint32]struct{}{}
	var opaque int
	var sumR, sumG, sumB float64
	var sumR2, sumG2, sumB2 float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c.A > 8 {
				opaque++
			}
			key := uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
			if len(colors) <= 4096 {
				colors[key] = struct{}{}
			}
			r, g, b := float64(c.R), float64(c.G), float64(c.B)
			sumR += r
			sumG += g
			sumB += b
			sumR2 += r * r
			sumG2 += g * g
			sumB2 += b * b
		}
	}
	n := float64(total)
	return pixelStats{
		total:  total,
		opaque: opaque,
		unique: len(colors),
		varR:   math.Max(0, sumR2/n-(sumR/n)*(sumR/n)),
		varG:   math.Max(0, sumG2/n-(sumG/n)*(sumG/n)),
		varB:   math.Max(0, sumB2/n-(sumB/n)*(sumB/n)),
	}
}

func writePixelManifestWithError(manifest PixelEvidenceManifest, opts PixelEvidenceOptions, err error) (PixelEvidenceManifest, error) {
	manifest.Failures = append(manifest.Failures, err.Error())
	portable, portableErr := portablePixelManifest(manifest, opts)
	if portableErr != nil {
		manifest.Failures = append(manifest.Failures, portableErr.Error())
		manifest.ArtifactRoot = portablePixelArtifactRoot
		manifest.BaselineRoot = ""
	} else {
		manifest = portable
	}
	_ = writePixelManifest(manifest, opts)
	return manifest, err
}

func portablePixelManifest(manifest PixelEvidenceManifest, opts PixelEvidenceOptions) (PixelEvidenceManifest, error) {
	manifest.ArtifactRoot = portablePixelArtifactRoot
	manifest.BaselineRoot = ""
	for stateIndex := range manifest.States {
		for captureIndex := range manifest.States[stateIndex].Captures {
			capture := &manifest.States[stateIndex].Captures[captureIndex]
			ref, err := portablePixelFileRef(opts.ArtifactRoot, capture.Path, ".png", "pixel capture path")
			if err != nil {
				return manifest, err
			}
			capture.Path = ref
			if capture.Comparison == nil {
				continue
			}
			baselineRoot := opts.ArtifactRoot
			if opts.Mode == PixelModeCandidateComparison {
				baselineRoot = opts.BaselineRoot
			}
			if capture.Comparison.BaselinePath != "" {
				ref, err := portablePixelFileRef(baselineRoot, capture.Comparison.BaselinePath, ".png", "pixel comparison baseline path")
				if err != nil {
					return manifest, err
				}
				capture.Comparison.BaselinePath = ref
			}
			if capture.Comparison.DiffPath != "" {
				ref, err := portablePixelFileRef(opts.ArtifactRoot, capture.Comparison.DiffPath, ".png", "pixel comparison diff path")
				if err != nil {
					return manifest, err
				}
				capture.Comparison.DiffPath = ref
			}
		}
	}
	return manifest, nil
}

func portablePixelFileRef(root, recorded, ext, label string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%s root is empty", label)
	}
	path, err := containedBaselineFilePath(root, recorded, ext, label)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, path)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s escapes root: %s", label, recorded)
	}
	return filepath.ToSlash(rel), nil
}

func writePixelManifest(manifest PixelEvidenceManifest, opts PixelEvidenceOptions) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("visual: marshal evidence: %w", err)
	}
	if err := os.MkdirAll(opts.ArtifactRoot, 0o755); err != nil {
		return fmt.Errorf("visual: mkdir artifact root: %w", err)
	}
	if err := os.WriteFile(pixelManifestPath(opts.ArtifactRoot), append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("visual: write evidence manifest: %w", err)
	}
	return nil
}

func pixelManifestPath(root string) string {
	return filepath.Join(root, "pixel-evidence.json")
}

func captureSelectedFromMeta(capture PixelCaptureEvidence, prior SelectedSceneEvidence) SelectedSceneEvidence {
	if prior.MountID != "" {
		return prior
	}
	if capture.Selected.CanvasSelector == "" {
		capture.Selected.CanvasSelector = prior.CanvasSelector
	}
	return capture.Selected
}

func findPixelState(manifest PixelEvidenceManifest, name string) *PixelStateEvidence {
	for i := range manifest.States {
		if manifest.States[i].State == name {
			return &manifest.States[i]
		}
	}
	return nil
}

func rendererFailure(truth sceneTruthEvidence) bool {
	return truth.DeviceLost ||
		truth.InitError != "" ||
		truth.LastError != "" ||
		truth.ShaderErrors > 0 ||
		truth.ShaderDiagnostics.Errors > 0 ||
		truth.ShaderDiagnostics.Failed ||
		truth.ShaderDiagnostics.LastError != "" ||
		truth.LastShaderError != "" ||
		truth.Post.Dead > 0 ||
		truth.Post.Failed > 0 ||
		truth.Post.Pending > 0
}

func mergeWebGPU(supplemental WebGPUEvidence, truth sceneTruthEvidence) WebGPUEvidence {
	if supplemental.AdapterInfo == nil {
		supplemental.AdapterInfo = truth.AdapterInfo
	}
	if supplemental.AdapterName == "" {
		supplemental.AdapterName = truth.Adapter
	}
	return supplemental
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "route"
	}
	return out
}

func classifyHardware(backend string, software bool, webgpuAvailable bool, gpuTruth bool) string {
	if software {
		return "software-raster"
	}
	switch backend {
	case "webgpu":
		if gpuTruth {
			return "hardware-webgpu"
		}
	case "webgl":
		if gpuTruth {
			return "hardware-webgl"
		}
		return "hardware-webgl"
	}
	return "headless-logic"
}

func mergeHardwareClass(current, next string) string {
	if current == "" {
		return next
	}
	if current == next {
		return current
	}
	if current == "software-raster" || next == "software-raster" {
		return "software-raster"
	}
	if current == "headless-logic" {
		return next
	}
	if next == "headless-logic" {
		return current
	}
	return "mixed-hardware"
}

func isSoftwareRaster(values ...string) bool {
	joined := strings.ToLower(strings.Join(values, " "))
	for _, marker := range []string{"swiftshader", "llvmpipe", "softpipe", "software raster", "mesa offscreen", "microsoft basic render", "warp"} {
		if strings.Contains(joined, marker) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func mergeObservedBackend(current, next string) string {
	if next != "" {
		return next
	}
	return current
}

func pixelBackendSelectionHookName(opts PixelEvidenceOptions) string {
	if opts.ForceWebGL && opts.Backend == RequireBackendWebGL {
		return "gosx-o02-force-webgl-new-document"
	}
	if opts.Backend == RequireBackendWebGPU {
		return "gosx-o02-clear-force-webgl-new-document"
	}
	return ""
}

func pixelBackendSelectionScript(opts PixelEvidenceOptions) string {
	if opts.ForceWebGL && opts.Backend == RequireBackendWebGL {
		return `window.__gosx_scene3d_force_webgl = true;`
	}
	if opts.Backend == RequireBackendWebGPU {
		return `try { delete window.__gosx_scene3d_force_webgl; } catch (_err) { window.__gosx_scene3d_force_webgl = undefined; }`
	}
	return ""
}

func jsLiteral(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}

const pixelMetadataProbeJS = `(async function() {
  const selector = %s;
  const errors = [];
  const canvases = Array.from(document.querySelectorAll(selector));
  const allMounts = Array.from(document.querySelectorAll('[data-gosx-scene3d-backend], [data-gosx-scene3d-renderer], [data-gosx-scene3d-mounted]'));
  let canvas = canvases.length === 1 ? canvases[0] : null;
  if (canvases.length !== 1) errors.push('selector matched ' + canvases.length + ' canvases');
  let mount = null;
  if (canvas) {
    mount = canvas.closest('[data-gosx-scene3d-backend], [data-gosx-scene3d-renderer], [data-gosx-scene3d-mounted]');
    if (!mount) {
      mount = allMounts.find((candidate) => candidate.contains(canvas)) || null;
    }
    if (!mount) errors.push('selected canvas has no owning Scene3D mount');
  }
  const attr = (name) => mount ? String(mount.getAttribute(name) || '') : '';
  const num = (name) => Number(attr(name) || 0);
  let backendTruth = {};
  let backendTruthParsed = false;
  try {
    const raw = attr('data-gosx-scene3d-render-backend-truth');
    backendTruth = raw ? JSON.parse(raw) : {};
    backendTruthParsed = !!raw;
  } catch (_err) {
    errors.push('backend truth JSON did not parse');
  }
  const rect = canvas ? canvas.getBoundingClientRect() : { width: 0, height: 0 };
  const webglCanvas = document.createElement('canvas');
  let webgl = { vendor: '', renderer: '', version: '' };
  try {
    const gl = webglCanvas.getContext('webgl2') || webglCanvas.getContext('webgl');
    if (gl) {
      const ext = gl.getExtension('WEBGL_debug_renderer_info');
      webgl = {
        vendor: ext ? String(gl.getParameter(ext.UNMASKED_VENDOR_WEBGL) || '') : String(gl.getParameter(gl.VENDOR) || ''),
        renderer: ext ? String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '') : String(gl.getParameter(gl.RENDERER) || ''),
        version: String(gl.getParameter(gl.VERSION) || '')
      };
    }
  } catch (_) {}
  let webgpu = {
    available: !!navigator.gpu,
    adapterName: '',
    vendor: '',
    architecture: '',
    device: '',
    description: navigator.userAgent || '',
    fallback: false,
    fallbackReason: '',
    adapterInfo: {}
  };
  try {
    if (navigator.gpu && navigator.gpu.requestAdapter) {
      const adapter = await navigator.gpu.requestAdapter();
      if (!adapter) {
        webgpu.fallbackReason = 'request-adapter-null';
      } else {
        const info = adapter.info || {};
        webgpu.adapterName = String(info.description || info.device || info.vendor || '');
        webgpu.vendor = String(info.vendor || '');
        webgpu.architecture = String(info.architecture || '');
        webgpu.device = String(info.device || '');
        webgpu.description = String(info.description || navigator.userAgent || '');
        webgpu.fallback = !!adapter.isFallbackAdapter;
        webgpu.adapterInfo = info;
      }
    }
  } catch (err) {
    webgpu.fallbackReason = String(err && err.message || err || 'request-adapter-error');
  }
  const truth = {
    backend: backendTruth.backend || '',
    renderer: attr('data-gosx-scene3d-renderer') || backendTruth.renderer || '',
    fallbackReason: backendTruth.fallbackReason || '',
    implementation: backendTruth.implementation || '',
    gpu: backendTruth.gpu === true,
    deviceLost: backendTruth.deviceLost === true,
    adapter: backendTruth.adapter || '',
    adapterInfo: backendTruth.adapterInfo || {},
    initError: backendTruth.initError || '',
    lastError: backendTruth.lastError || '',
    frameSeq: num('data-gosx-scene3d-webgpu-frame-seq') || num('data-gosx-scene3d-webgl-frame-seq') || num('data-gosx-scene3d-frame-seq'),
    shaderErrors: Number((backendTruth.shaderDiagnostics && backendTruth.shaderDiagnostics.errors) || 0),
    shaderDiagnostics: {
      messages: Number((backendTruth.shaderDiagnostics && backendTruth.shaderDiagnostics.messages) || 0),
      errors: Number((backendTruth.shaderDiagnostics && backendTruth.shaderDiagnostics.errors) || 0),
      failed: backendTruth.shaderDiagnostics && backendTruth.shaderDiagnostics.failed === true,
      lastError: String((backendTruth.shaderDiagnostics && backendTruth.shaderDiagnostics.lastError) || backendTruth.lastShaderError || '')
    },
    lastShaderError: String(backendTruth.lastShaderError || ''),
    post: {
      authored: num('data-gosx-scene3d-render-post-authored'),
      dispatched: num('data-gosx-scene3d-render-post-dispatched'),
      dead: num('data-gosx-scene3d-render-post-dead'),
      failed: num('data-gosx-scene3d-render-post-failed'),
      pending: num('data-gosx-scene3d-render-post-pending'),
      chain: attr('data-gosx-scene3d-render-post-chain')
    },
    parsed: backendTruthParsed
  };
  return {
    devicePixelRatio: window.devicePixelRatio || 1,
    effectiveDPR: canvas && rect.width ? canvas.width / rect.width : 0,
    canvas: {
      canvasWidth: canvas ? canvas.width : 0,
      canvasHeight: canvas ? canvas.height : 0,
      canvasCSSWidth: rect.width || 0,
      canvasCSSHeight: rect.height || 0
    },
    selected: {
      mountID: mount ? (mount.id || '') : '',
      mountSelector: mount && mount.id ? '#' + mount.id : '',
      canvasSelector: selector,
      canvasCount: canvases.length,
      mountCount: mount ? 1 : 0
    },
    mount: {
      id: mount ? (mount.id || '') : '',
      backend: truth.backend,
      renderer: truth.renderer,
      fallbackReason: truth.fallbackReason
    },
    truth,
    webgpu,
    webgl,
    userAgent: navigator.userAgent || '',
    errors
  };
})()`
