package ouroboros

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/perf"
	"m31labs.dev/gosx/visual"
)

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func testRuntimeJSONStaticIdentity() *RuntimeJSONStaticIdentity {
	counts := RuntimeJSONStaticCounts{
		SerializationSiteCount:             3,
		GosxReadCount:                      5,
		GosxWriteCount:                     2,
		GosxCallCount:                      1,
		UniqueGosxGlobals:                  4,
		ExactCount:                         6,
		AmbiguousCount:                     1,
		UnknownCount:                       2,
		SerializationHotPathPossibleCount:  2,
		SerializationHotPathConfirmedCount: 1,
	}
	return &RuntimeJSONStaticIdentity{
		Ref:                "perf/runtime-json-static.jsonl",
		SchemaVersion:      RuntimeJSONProbeSchemaVersion,
		ScannerVersion:     runtimeJSONStaticScannerVersion,
		QueryID:            "gosx.ouroboros.o02.runtime-json-static.ast.v2",
		PhaseClassifier:    runtimeJSONPhaseClassifierVersion,
		SourceIdentityHash: "sha256:source",
		SemanticHash:       "sha256:semantic",
		CountsHash:         RuntimeJSONStaticCountsHash(counts),
		GlobalNameHash:     "sha256:globals",
		Validated:          true,
		Counts:             counts,
	}
}

func testCompatibilityAuditIdentity(status string, available bool, static *RuntimeJSONStaticIdentity) *CompatibilityAuditIdentity {
	if static == nil {
		static = testRuntimeJSONStaticIdentity()
	}
	addedCount := 0
	if !available {
		addedCount = 1
	}
	return &CompatibilityAuditIdentity{
		SchemaVersion:      compatibilityAuditSchemaVersion,
		Status:             status,
		ScanStatus:         compatibilityScanStatusComplete,
		CanonicalAvailable: available,
		Receipt: CompatibilityNameSetSummary{
			Count:       canonicalGosx,
			NameSetHash: compatibilityReceiptHash,
		},
		Anchor: CompatibilityNameSetSummary{
			Count:       canonicalGosx,
			NameSetHash: "sha256:anchor",
		},
		Current: CompatibilityNameSetSummary{
			Count:       canonicalGosx,
			NameSetHash: "sha256:current",
		},
		Reconciliation: CompatibilityReconciliationRef{
			RecoveredPreexistingCount: 8,
			RecoveredPreexistingHash:  "sha256:recovered",
			MissingFromAnchorCount:    8,
			MissingFromAnchorHash:     "sha256:missing",
			AddedSinceAnchorCount:     addedCount,
			AddedSinceAnchorHash:      nameSetHash(nil),
			RemovedSinceAnchorHash:    nameSetHash(nil),
		},
		RuntimeJSONSourceIdentityHash: static.SourceIdentityHash,
		RuntimeJSONSemanticHash:       static.SemanticHash,
		RuntimeJSONCountsHash:         static.CountsHash,
		RuntimeJSONGlobalNameHash:     static.GlobalNameHash,
	}
}

func testBrowserSourceIdentityWithAudit(audit *CompatibilityAuditIdentity) SourceIdentity {
	return SourceIdentity{
		BaseRevision:                "abc1234",
		OverlayHash:                 "sha256:clean",
		TrackedDiffHash:             "sha256:tracked",
		UntrackedIncludedSourceHash: "sha256:untracked",
		InventorySHA256:             "sha256:inventory",
		StrictInventory:             true,
		CurrentOverlayVerified:      true,
		ReconstructionProof:         true,
		Reconstruction:              &ReconstructionEvidence{BaseRevision: "abc1234", ObservedOverlayHash: "sha256:clean", Isolated: true, Applied: true, Verified: true},
		RuntimeJSONStatic:           testRuntimeJSONStaticIdentity(),
		CompatibilityAudit:          audit,
	}
}

func testValidBrowserSample(source SourceIdentity) BrowserRawSample {
	return BrowserRawSample{
		SchemaVersion: BrowserBaselineSchemaVersion,
		RouteID:       "R00",
		SampleLane:    SampleLaneProduct,
		CacheMode:     "cold",
		Source:        source,
		Proofs:        ProofBundle{FirstUsable: ProofPayload{Name: "ready", OK: true}},
		Page:          &perf.PageReport{},
		Network:       []NetworkRecord{{URL: "http://127.0.0.1/static", Role: "Document", Status: 200}},
		ProbeEvents:   []ProbeEvent{{Kind: "navigation", Phase: "route-load"}},
		Trace:         TraceSampleSummary{Captured: true},
		Coverage:      CoverageSampleSummary{Captured: true},
		Artifacts:     SampleArtifacts{HeapSnapshotRef: "heaps/R00.heapsnapshot"},
	}
}

func writeValidPixelManifestForD(t *testing.T, root, routeID, backend string) visual.PixelEvidenceManifest {
	t.Helper()
	forceWebGL := backend == string(visual.RequireBackendWebGL)
	hook := "gosx-o02-clear-force-webgl-new-document"
	if forceWebGL {
		hook = "gosx-o02-force-webgl-new-document"
	}
	manifest := visual.PixelEvidenceManifest{
		SchemaVersion:      visual.OuroborosPixelSchemaVersion,
		RouteID:            routeID,
		Mode:               string(visual.PixelModeRecordBaseline),
		ArtifactRoot:       root,
		BackendRequirement: backend,
		BackendSelection: visual.PixelBackendSelection{
			RequestedBackend:       backend,
			RuntimeObservedBackend: backend,
			ForceWebGL:             forceWebGL,
			PreNavigationHook:      hook,
		},
		Certified:              true,
		HardwareClassification: "hardware-" + backend,
		Selected:               visual.SelectedSceneEvidence{MountID: "mount", MountSelector: "#mount", CanvasSelector: "canvas", CanvasCount: 1, MountCount: 1},
		Threshold:              visual.PixelThresholdEvidence{EffectivePct: 0.5},
		SettlePolicy:           validPixelSettlePolicyForTest(),
	}
	for _, stateName := range []string{"initial", "settled"} {
		renderLoop := validPixelRenderLoopForTest()
		frame := 10
		if stateName == "settled" {
			frame = 40
		}
		state := visual.PixelStateEvidence{
			State:  stateName,
			Settle: visual.PixelSettleResult{RequiredFrame: frame, ObservedFrame: frame, AdvanceRequired: stateName == "settled", RenderLoop: renderLoop},
			Batch:  validPixelBatchForPerfTest(routeID+"-"+stateName+"-batch", stateName, backend, frame, renderLoop),
		}
		for i := 0; i < 3; i++ {
			body := []byte{byte(i + 1), byte(len(stateName)), 3, 4}
			path := filepath.Join(root, routeID+"-"+stateName+"-"+string(rune('0'+i))+".png")
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(body)
			state.Captures = append(state.Captures, visual.PixelCaptureEvidence{
				Index:              i,
				Path:               path,
				SHA256:             hex.EncodeToString(sum[:]),
				Backend:            backend,
				Renderer:           backend,
				RuntimeTruthParsed: true,
				RuntimeGPU:         true,
				Implementation:     "test-" + backend,
				HardwareClass:      "hardware-" + backend,
				FrameSeq:           frame,
				BatchID:            state.Batch.ID,
				RenderLoop:         renderLoop,
				WebGPU:             testWebGPUEvidenceForBackend(backend),
				WebGL:              testWebGLEvidenceForBackend(backend),
				Comparison:         &visual.PixelComparison{Passed: true},
				Selected:           manifest.Selected,
			})
		}
		manifest.States = append(manifest.States, state)
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeStrictCanonicalPixelManifestRef(t *testing.T, evidenceRoot, routeID, backend string, source visual.PixelSourceIdentity) string {
	t.Helper()
	dir := filepath.Join(evidenceRoot, strings.ToLower(routeID+"-"+backend))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := writeValidPixelManifestForD(t, dir, routeID, backend)
	manifest.Source = source
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pixel-evidence.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Join(strings.ToLower(routeID+"-"+backend), "pixel-evidence.json"))
}

func writeValidCanonicalPixelManifestForSelector(t *testing.T, root, routeID, backend, selector string, source visual.PixelSourceIdentity) visual.PixelEvidenceManifest {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	forceWebGL := backend == string(visual.RequireBackendWebGL)
	hook := "gosx-o02-clear-force-webgl-new-document"
	if forceWebGL {
		hook = "gosx-o02-force-webgl-new-document"
	}
	manifest := visual.PixelEvidenceManifest{
		SchemaVersion:      visual.OuroborosPixelSchemaVersion,
		RouteID:            routeID,
		Mode:               string(visual.PixelModeRecordBaseline),
		ArtifactRoot:       root,
		Source:             source,
		BackendRequirement: backend,
		BackendSelection: visual.PixelBackendSelection{
			RequestedBackend:       backend,
			RuntimeObservedBackend: backend,
			ForceWebGL:             forceWebGL,
			PreNavigationHook:      hook,
		},
		Certified:              true,
		HardwareClassification: "hardware-" + backend,
		Viewport:               visual.ViewportEvidence{Width: 1440, Height: 900, DPR: 1},
		Selected:               visual.SelectedSceneEvidence{MountID: "mount", MountSelector: "#mount", CanvasSelector: selector, CanvasCount: 1, MountCount: 1},
		Threshold:              visual.PixelThresholdEvidence{EffectivePct: 0.5},
		SettlePolicy:           validPixelSettlePolicyForTest(),
	}
	renderLoop := validPixelRenderLoopForTest()
	for _, stateName := range []string{"initial", "settled"} {
		settle := visual.PixelSettleResult{RequiredFrame: 10, ObservedFrame: 10, RenderLoop: renderLoop}
		if stateName == "settled" {
			settle.RequiredFrame = 40
			settle.ObservedFrame = 40
			settle.AdvanceRequired = true
		}
		state := visual.PixelStateEvidence{State: stateName, Settle: settle, Batch: validPixelBatchForPerfTest(routeID+"-"+stateName+"-batch", stateName, backend, settle.ObservedFrame, renderLoop)}
		for i := 0; i < 3; i++ {
			data := canonicalPixelPNGForTest(t)
			path := filepath.Join(root, fmt.Sprintf("%s-%s-%02d.png", routeID, stateName, i))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			state.Captures = append(state.Captures, visual.PixelCaptureEvidence{
				Index:              i,
				Path:               path,
				SHA256:             hex.EncodeToString(sum[:]),
				Bytes:              len(data),
				Width:              8,
				Height:             8,
				Backend:            backend,
				Renderer:           backend,
				RuntimeTruthParsed: true,
				RuntimeGPU:         true,
				Implementation:     "test-" + backend,
				HardwareClass:      "hardware-" + backend,
				FrameSeq:           settle.ObservedFrame,
				BatchID:            state.Batch.ID,
				RenderLoop:         renderLoop,
				WebGPU:             testWebGPUEvidenceForBackend(backend),
				WebGL:              testWebGLEvidenceForBackend(backend),
				Selected:           manifest.Selected,
				Comparison:         &visual.PixelComparison{BaselineThresholdPct: 0.5, EffectiveThresholdPct: 0.5, Passed: true},
			})
		}
		manifest.States = append(manifest.States, state)
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel-evidence.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func testWebGPUEvidenceForBackend(backend string) visual.WebGPUEvidence {
	if backend != string(visual.RequireBackendWebGPU) {
		return visual.WebGPUEvidence{}
	}
	return visual.WebGPUEvidence{
		Available:    true,
		AdapterName:  "NVIDIA RTX test adapter",
		Vendor:       "nvidia",
		Architecture: "discrete-gpu",
		Device:       "test-device",
		Description:  "Dawn NVIDIA RTX test adapter",
		AdapterInfo: map[string]interface{}{
			"vendor":       "nvidia",
			"architecture": "discrete-gpu",
			"device":       "test-device",
			"description":  "Dawn NVIDIA RTX test adapter",
		},
	}
}

func testWebGLEvidenceForBackend(backend string) visual.WebGLEvidence {
	if backend != string(visual.RequireBackendWebGL) {
		return visual.WebGLEvidence{}
	}
	return visual.WebGLEvidence{
		Vendor:   "NVIDIA Corporation",
		Renderer: "ANGLE (NVIDIA GeForce RTX 4090 Direct3D11 vs_5_0 ps_5_0)",
		Version:  "WebGL 2.0",
	}
}

func validPixelSettlePolicyForTest() visual.PixelSettlePolicy {
	return visual.PixelSettlePolicy{
		WarmupFrames:                 30,
		WarmupAnchor:                 "initial-observed-frame",
		RuntimeRenderLoopRequired:    true,
		StaticStoppedAllowsNoAdvance: true,
		RAFGate: visual.PixelRAFGatePolicy{
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
	}
}

func validPixelRenderLoopForTest() visual.RenderLoopEvidence {
	return visual.RenderLoopEvidence{
		State:                "active",
		Reason:               "runtime-program",
		Active:               true,
		WantsAnimation:       true,
		StateParsed:          true,
		WantsAnimationParsed: true,
		Valid:                true,
	}
}

func validPixelBatchForPerfTest(id, state, backend string, frame int, loop visual.RenderLoopEvidence) visual.PixelBatchEvidence {
	clip := visual.PixelCanvasClipEvidence{Width: 8, Height: 8, Scale: 1, Stable: true}
	snapshot := visual.PixelBatchSnapshot{
		Visible:            true,
		Focused:            true,
		Backend:            backend,
		Renderer:           backend,
		FrameSeq:           frame,
		RuntimeTruthParsed: true,
		RenderLoopState:    loop.State,
		RenderLoopActive:   loop.Active,
		WantsAnimation:     loop.WantsAnimation,
		Clip:               clip,
	}
	return visual.PixelBatchEvidence{
		ID:                 id,
		State:              state,
		Acquired:           true,
		Released:           true,
		ReleaseProved:      true,
		NonceHash:          "sha256:" + strings.Repeat("1", 64),
		GlobalKeyHash:      "sha256:" + strings.Repeat("2", 64),
		DrainTicks:         2,
		NativeTickCount:    2,
		QueueAfterDrain:    1,
		QueueBeforeRelease: 1,
		Delivered:          1,
		Restored:           true,
		Cleaned:            true,
		Clip:               clip,
		BeforeAcquire:      snapshot,
		Before:             snapshot,
		After:              snapshot,
	}
}

func canonicalPixelPNGForTest(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 31), G: uint8(y * 29), B: uint8(40 + x*y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLoadFixtureCorpusReadsOuroborosFixtureRoutes(t *testing.T) {
	corpus, err := LoadFixtureCorpus(filepath.Join("..", "..", "examples", "ouroboros-corpus", "fixtures.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "gosx.ouroboros.fixtures.v1" {
		t.Fatalf("schemaVersion = %q", corpus.SchemaVersion)
	}
	if len(corpus.Routes) < 11 {
		t.Fatalf("routes = %d, want at least 11", len(corpus.Routes))
	}
	if corpus.Routes[0].ID != "R00" || corpus.Routes[0].Route != "/static" {
		t.Fatalf("first route = %#v", corpus.Routes[0])
	}
}

func TestLoadFixtureCorpusAcceptsManifestFixtureRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.json")
	if err := os.WriteFile(path, []byte(`{
		"schemaVersion":"gosx.ouroboros.baseline.v1",
		"contractVersion":"O0.2",
		"corpusID":"gosx-ouroboros-o0.2-v1",
		"fixtureRoutes":[{"id":"R00","route":"/static","fixtureApp":"examples/ouroboros-corpus"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus, err := LoadFixtureCorpus(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Routes) != 1 || corpus.Routes[0].ID != "R00" {
		t.Fatalf("routes = %#v", corpus.Routes)
	}
}

func TestSamplingPlanSmokeCannotUpdateBaseline(t *testing.T) {
	plan, err := samplingPlan("smoke")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Canonical || plan.CanUpdateBaseline {
		t.Fatalf("smoke plan can update baseline: %#v", plan)
	}
	if plan.ColdSamples != 1 || plan.WarmSamples != 1 || plan.PilotsDiscarded != 0 {
		t.Fatalf("smoke counts = %#v", plan)
	}
}

func TestSamplingPlanBaselineMatchesContractCounts(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Canonical || !plan.CanUpdateBaseline {
		t.Fatalf("baseline plan is not canonical: %#v", plan)
	}
	if plan.PilotsDiscarded != 2 || plan.ColdSamples != 11 || plan.WarmSamples != 21 || plan.SceneColdSamples != 7 || plan.SceneWarmSamples != 15 {
		t.Fatalf("baseline counts = %#v", plan)
	}
}

func TestCanonicalRouteSelectionRejectsSubsetAndMissingRoutes(t *testing.T) {
	corpus, err := LoadFixtureCorpus(filepath.Join("..", "..", "examples", "ouroboros-corpus", "fixtures.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalRouteSelection(corpus.Routes, []string{"R00"}); err == nil || !strings.Contains(err.Error(), "--routes") {
		t.Fatalf("subset error = %v", err)
	}
	if err := validateCanonicalRouteSelection(corpus.Routes[:len(corpus.Routes)-1], nil); err == nil || !strings.Contains(err.Error(), "R10") {
		t.Fatalf("missing route error = %v", err)
	}
	duplicate := append([]FixtureSpec{}, corpus.Routes...)
	duplicate = append(duplicate, corpus.Routes[0])
	if err := validateCanonicalRouteSelection(duplicate, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate route error = %v", err)
	}
	extra := append([]FixtureSpec{}, corpus.Routes...)
	extra = append(extra, FixtureSpec{ID: "R99", Route: "/extra"})
	if err := validateCanonicalRouteSelection(extra, nil); err == nil || !strings.Contains(err.Error(), "extra routes") {
		t.Fatalf("extra route error = %v", err)
	}
	if err := validateCanonicalRouteSelection(corpus.Routes, nil); err != nil {
		t.Fatalf("complete corpus rejected: %v", err)
	}
}

func TestValidateCanonicalSampleMatrixRejectsMissingBucketAndUndercount(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	samples := canonicalBrowserMatrixSamples(plan)
	if err := validateCanonicalSampleMatrix(plan, samples); err != nil {
		t.Fatalf("complete matrix rejected: %v", err)
	}
	undercount := append([]BrowserRawSample{}, samples...)
	for i, sample := range undercount {
		if sample.RouteID == "R00" && sample.CacheMode == "cold" && !sample.Pilot {
			undercount[i].Discarded = true
			break
		}
	}
	err = validateCanonicalSampleMatrix(plan, undercount)
	if err == nil || !strings.Contains(err.Error(), "R00/cold") {
		t.Fatalf("undercount error = %v", err)
	}
	var missingBucket []BrowserRawSample
	for _, sample := range samples {
		if sample.RouteID == "R10" && sample.CacheMode == "warm" {
			continue
		}
		missingBucket = append(missingBucket, sample)
	}
	err = validateCanonicalSampleMatrix(plan, missingBucket)
	if err == nil || !strings.Contains(err.Error(), "R10/warm") {
		t.Fatalf("missing bucket error = %v", err)
	}
	missingPilot := append([]BrowserRawSample{}, samples...)
	for i, sample := range missingPilot {
		if sample.RouteID == "R01" && sample.CacheMode == "warm" && sample.Pilot {
			missingPilot = append(missingPilot[:i], missingPilot[i+1:]...)
			break
		}
	}
	err = validateCanonicalSampleMatrix(plan, missingPilot)
	if err == nil || !strings.Contains(err.Error(), "R01/warm pilots=1") {
		t.Fatalf("missing pilot error = %v", err)
	}
	mislabeled := append([]BrowserRawSample{}, samples...)
	for i, sample := range mislabeled {
		if sample.RouteID == "R02" && sample.CacheMode == "cold" && sample.Pilot {
			mislabeled[i].Discarded = false
			break
		}
	}
	err = validateCanonicalSampleMatrix(plan, mislabeled)
	if err == nil || !strings.Contains(err.Error(), "mislabeled") {
		t.Fatalf("mislabeled pilot error = %v", err)
	}
	extraProbe := append([]BrowserRawSample{}, samples...)
	extraProbe = append(extraProbe, BrowserRawSample{SchemaVersion: BrowserBaselineSchemaVersion, RouteID: "R03", SampleLane: SampleLaneProbe, CacheMode: "cold"})
	err = validateCanonicalSampleMatrix(plan, extraProbe)
	if err == nil || !strings.Contains(err.Error(), "R03/cold probe=2 want 1") {
		t.Fatalf("extra probe error = %v", err)
	}
	badLane := append([]BrowserRawSample{}, samples...)
	badLane[0].SampleLane = SampleLane("extra")
	err = validateCanonicalSampleMatrix(plan, badLane)
	if err == nil || !strings.Contains(err.Error(), "unknown sample lane") {
		t.Fatalf("bad lane error = %v", err)
	}
}

func TestCanonicalNoiseRerunProofKeepsRawSamplesSeparate(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	source := compareSource("")
	baseline := canonicalBrowserMatrixSamples(plan)
	rerun := canonicalBrowserMatrixSamples(plan)
	for i := range baseline {
		baseline[i].Source = source
		rerun[i].Source = source
		if baseline[i].SampleLane == SampleLaneProduct && baseline[i].RouteID == "R00" && baseline[i].CacheMode == "cold" && !baseline[i].Discarded {
			if baseline[i].SampleIndex%2 == 0 {
				baseline[i].Metrics = map[string]float64{"dclMs": 100}
			} else {
				baseline[i].Metrics = map[string]float64{"dclMs": 200}
			}
			rerun[i].Metrics = map[string]float64{"dclMs": 100}
		}
	}
	if len(baseline) != 484 {
		t.Fatalf("canonical raw sample count = %d, want 484", len(baseline))
	}
	root := t.TempDir()
	baselineRaw := filepath.Join(root, "perf", "raw-samples.jsonl")
	rerunRaw := filepath.Join(root, "noise-rerun", "perf", "raw-samples.jsonl")
	writeRawSamples(t, baselineRaw, baseline)
	writeRawSamples(t, rerunRaw, rerun)
	summary := SummarizeBrowserSamples(baseline, "baseline", source)
	proof, err := BuildNoiseRerunProof(summary, baselineRaw, rerunRaw, "perf/raw-samples.jsonl", "noise-rerun/perf/raw-samples.jsonl", baseline, rerun, []string{"startup.dclMs"})
	if err != nil {
		t.Fatal(err)
	}
	if proof.BaselineRawSamplesRef != "perf/raw-samples.jsonl" || proof.RerunRawSamplesRef != "noise-rerun/perf/raw-samples.jsonl" {
		t.Fatalf("proof refs = %#v", proof)
	}
	gotBaseline, err := ReadBrowserRawSamplesJSONLStrict(baselineRaw)
	if err != nil {
		t.Fatal(err)
	}
	gotRerun, err := ReadBrowserRawSamplesJSONLStrict(rerunRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotBaseline) != 484 || len(gotRerun) != 484 {
		t.Fatalf("raw counts baseline=%d rerun=%d", len(gotBaseline), len(gotRerun))
	}
	if len(proof.Metrics) != 1 || proof.Metrics[0].Group != "R00/cold" || proof.Metrics[0].Metric != "startup.dclMs" {
		t.Fatalf("proof metrics = %#v", proof.Metrics)
	}
}

func TestCanonicalNoiseRerunOptionsPreserveExternalEvidencePreflight(t *testing.T) {
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "artifact")
	evidenceRoot := filepath.Join(root, "evidence")
	source := SourceIdentity{
		BaseRevision:    "0123456789abcdef0123456789abcdef01234567",
		OverlayHash:     "sha256:" + strings.Repeat("1", 64),
		InventoryRef:    "source/source-inventory.json",
		InventorySHA256: "sha256:" + strings.Repeat("2", 64),
	}
	pixelSource := visual.PixelSourceIdentity{BaseRevision: source.BaseRevision, OverlayHash: source.OverlayHash, InventorySHA256: source.InventorySHA256}
	var pixelRefs []string
	for _, routeID := range []string{"R08", "R10"} {
		for _, backend := range []string{"webgpu", "webgl"} {
			pixelRefs = append(pixelRefs, writeStrictCanonicalPixelManifestRef(t, evidenceRoot, routeID, backend, pixelSource))
		}
	}
	opts := BrowserBaselineOptions{
		ArtifactRoot:  artifactRoot,
		EvidenceRoot:  evidenceRoot,
		InventoryPath: filepath.Join(root, "inventory.json"),
		PixelManifest: strings.Join(pixelRefs, ","),
	}
	rerunOpts := canonicalNoiseRerunOptions(opts, opts.InventoryPath)
	if rerunOpts.ArtifactRoot != filepath.Join(artifactRoot, "noise-rerun") {
		t.Fatalf("rerun artifact root = %s", rerunOpts.ArtifactRoot)
	}
	if rerunOpts.EvidenceRoot != evidenceRoot {
		t.Fatalf("rerun evidence root = %s, want %s", rerunOpts.EvidenceRoot, evidenceRoot)
	}
	if rerunOpts.PixelManifest != opts.PixelManifest {
		t.Fatalf("rerun pixel refs changed: %q", rerunOpts.PixelManifest)
	}
	if !rerunOpts.DisableNoiseRerun {
		t.Fatal("nested rerun was not disabled")
	}
	if err := validateCanonicalEvidenceRoot(rerunOpts); err != nil {
		t.Fatalf("rerun evidence root preflight failed: %v", err)
	}
	manifests, err := readCanonicalPixelManifestRefs(rerunOpts)
	if err != nil {
		t.Fatalf("rerun pixel refs did not resolve from external evidence root: %v", err)
	}
	if len(manifests) != 4 {
		t.Fatalf("rerun pixel manifest count = %d", len(manifests))
	}
	if samePath(rerunOpts.ArtifactRoot, rerunOpts.EvidenceRoot) {
		t.Fatal("rerun output root aliases external evidence root")
	}
}

func canonicalBrowserMatrixSamples(plan SamplingPlan) []BrowserRawSample {
	var samples []BrowserRawSample
	for _, routeID := range canonicalRouteIDs() {
		cold, warm := canonicalRequiredCounts(routeID)
		for _, cacheMode := range []string{"cold", "warm"} {
			for i := 0; i < plan.PilotsDiscarded; i++ {
				samples = append(samples, BrowserRawSample{SchemaVersion: BrowserBaselineSchemaVersion, RouteID: routeID, SampleLane: SampleLaneProduct, CacheMode: cacheMode, SampleIndex: i, Pilot: true, Discarded: true})
				samples = append(samples, BrowserRawSample{SchemaVersion: BrowserBaselineSchemaVersion, RouteID: routeID, SampleLane: SampleLaneProbeOverhead, CacheMode: cacheMode, SampleIndex: i, Pilot: true, Discarded: true})
			}
			samples = append(samples, BrowserRawSample{SchemaVersion: BrowserBaselineSchemaVersion, RouteID: routeID, SampleLane: SampleLaneProbe, CacheMode: cacheMode})
		}
		for i := 0; i < cold; i++ {
			samples = append(samples, BrowserRawSample{SchemaVersion: BrowserBaselineSchemaVersion, RouteID: routeID, SampleLane: SampleLaneProduct, CacheMode: "cold", SampleIndex: plan.PilotsDiscarded + i})
		}
		for i := 0; i < warm; i++ {
			samples = append(samples, BrowserRawSample{SchemaVersion: BrowserBaselineSchemaVersion, RouteID: routeID, SampleLane: SampleLaneProduct, CacheMode: "warm", SampleIndex: plan.PilotsDiscarded + i})
		}
	}
	return samples
}

func TestComputeStatsIncludesOuroborosRequiredFields(t *testing.T) {
	stats := ComputeStats([]float64{1, 2, 3, 4, 100})
	if stats.N != 5 {
		t.Fatalf("N = %d", stats.N)
	}
	if stats.Median != 3 || stats.P75 != 4 || stats.Max != 100 {
		t.Fatalf("stats = %#v", stats)
	}
	if stats.MAD == 0 || stats.IQR == 0 {
		t.Fatalf("MAD/IQR missing: %#v", stats)
	}
}

func TestSummarizeBrowserSamplesDropsPilotRuns(t *testing.T) {
	source := SourceIdentity{BaseRevision: "abc1234", OverlayHash: "sha256:clean", RuntimeJSONStatic: testRuntimeJSONStaticIdentity()}
	summary := SummarizeBrowserSamples([]BrowserRawSample{
		{RouteID: "R00", SampleLane: SampleLaneProduct, CacheMode: "cold", Discarded: true, Metrics: map[string]float64{"ttfbMs": 100}},
		{RouteID: "R00", SampleLane: SampleLaneProduct, CacheMode: "cold", Metrics: map[string]float64{"ttfbMs": 10}},
		{RouteID: "R00", SampleLane: SampleLaneProduct, CacheMode: "cold", Metrics: map[string]float64{"ttfbMs": 20}},
		{RouteID: "R00", SampleLane: SampleLaneProbe, CacheMode: "cold", Metrics: map[string]float64{"ttfbMs": 1000}},
	}, "smoke", source)
	if summary.Discarded != 1 {
		t.Fatalf("discarded = %d", summary.Discarded)
	}
	got := summary.Groups["R00/cold"]["metrics"]["ttfbMs"]
	if got.N != 2 || got.Median != 15 {
		t.Fatalf("summary stats = %#v", got)
	}
	if summary.SampleCount != 2 {
		t.Fatalf("sampleCount = %d", summary.SampleCount)
	}
}

func TestProbeSchemaReservesRuntimeAndJSONKinds(t *testing.T) {
	probe := DefaultProbeSchemaIdentity()
	want := map[string]bool{"runtime-call": false, "json-call": false}
	for _, kind := range probe.EventKinds {
		if _, ok := want[kind]; ok {
			want[kind] = true
		}
	}
	for kind, found := range want {
		if !found {
			t.Fatalf("probe event kind %q missing from %#v", kind, probe.EventKinds)
		}
	}
	if !probe.InjectedByCDP || probe.ProductAsset {
		t.Fatalf("probe must be CDP-only: %#v", probe)
	}
}

func TestSampleMetricsPreservesRawTransferAndMemoryValues(t *testing.T) {
	page := &perf.PageReport{
		TTFBMs:                12,
		DCLMs:                 34,
		FullyLoadedMs:         56,
		TotalBytesTransferred: 789,
		LongTaskCount:         2,
		LongTaskTotalMs:       80,
		TotalBlockingTimeMs:   30,
	}
	mem := perf.MemoryStats{JSHeapUsedMB: 1.5, DOMNodeCount: 42, WASMPages: 3, WASMBytes: 196608}
	metrics := sampleMetrics(page, mem)
	if metrics["transferBytes"] != 789 || metrics["jsHeapUsedMb"] != 1.5 || metrics["domNodeCount"] != 42 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if metrics["wasmPages"] != 3 || metrics["wasmBytes"] != 196608 {
		t.Fatalf("wasm metrics = %#v", metrics)
	}
	mem.WASMPages = 0
	mem.WASMBytes = 0
	metrics = sampleMetrics(page, mem)
	if _, ok := metrics["wasmPages"]; ok {
		t.Fatalf("wasmPages present without observed WASM memory: %#v", metrics)
	}
}

func TestProductLaneInstallsWASMMemoryObserverOnly(t *testing.T) {
	if !installWASMMemoryObserver(SampleLaneProduct) {
		t.Fatal("product lane does not install WASM memory observer")
	}
	script := wasmMemoryObserverScript()
	if !strings.Contains(script, "WebAssembly.instantiate") || !strings.Contains(script, "__gosxOuroborosWASMMemory") {
		t.Fatalf("WASM memory observer missing expected hooks: %s", script)
	}
	if strings.Contains(script, "__gosxOuroborosProbe") || strings.Contains(script, "JSON.stringify") || strings.Contains(script, "JSON.parse") {
		t.Fatalf("WASM memory observer includes probe or JSON hot-path logic: %s", script)
	}
}

func TestWASMMemoryObserverRefreshesGrownMemoryOnSnapshot(t *testing.T) {
	script := wasmMemoryObserverScript()
	if !strings.Contains(script, "refreshMemories();") || !strings.Contains(script, "state.memories") {
		t.Fatalf("WASM memory observer does not refresh tracked memories: %s", script)
	}
	if strings.Contains(script, "__gosxOuroborosProbe") || strings.Contains(script, "JSON.stringify") || strings.Contains(script, "JSON.parse") {
		t.Fatalf("WASM memory observer includes probe or JSON hot-path logic: %s", script)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable for WASM observer script execution")
	}
	js := `
global.window = global;
let bytes = 65536;
const memory = {};
Object.defineProperty(memory, "buffer", { get() { return { byteLength: bytes }; } });
global.WebAssembly = {
  instantiate() { return Promise.resolve({ instance: { exports: { memory } } }); }
};
` + script + `
WebAssembly.instantiate().then(() => {
  bytes = 196608;
  const snap = window.__gosxOuroborosWASMMemory.snapshot();
  if (snap.pages !== 3 || snap.bytes !== 196608) {
    console.error("snapshot", snap);
    process.exit(1);
  }
}).catch((err) => {
  console.error(err && err.stack || err);
  process.exit(1);
});
`
	cmd := exec.Command("node", "-e", js)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("observer script did not report grown memory: %v\n%s", err, out)
	}
}

func TestBrowserBaselineSmokeR00(t *testing.T) {
	if os.Getenv("GOSX_OUROBOROS_BROWSER_SMOKE") != "1" {
		t.Skip("set GOSX_OUROBOROS_BROWSER_SMOKE=1 to run Chrome fixture smoke")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "build", "ouroboros", "o0.2", "browser-smoke-test")
	_ = os.RemoveAll(out)
	result, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      root,
		ArtifactRoot:  out,
		FixtureApp:    filepath.Join(root, "examples", "ouroboros-corpus"),
		CorpusPath:    filepath.Join(root, "examples", "ouroboros-corpus", "fixtures.v1.json"),
		Serve:         true,
		Samples:       "smoke",
		Routes:        []string{"R00"},
		Headless:      true,
		Timeout:       45 * time.Second,
		Trace:         false,
		Coverage:      false,
		HeapSnapshots: false,
		Environment:   "headless-logic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Canonical {
		t.Fatal("smoke run reported canonical")
	}
	if result.SampleCount != 2 || result.DiscardedCount != 0 {
		t.Fatalf("samples = %d discarded = %d", result.SampleCount, result.DiscardedCount)
	}
	for _, path := range []string{result.ManifestPath, result.EnvironmentPath, result.RawSamplesPath, result.SummaryPath, result.CommandLogPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing artifact %s: %v", path, err)
		}
	}
}

func TestBrowserBaselineSmokeRoutes(t *testing.T) {
	rawRoutes := os.Getenv("GOSX_OUROBOROS_BROWSER_SMOKE_ROUTES")
	if rawRoutes == "" {
		t.Skip("set GOSX_OUROBOROS_BROWSER_SMOKE_ROUTES to run selected Chrome fixture routes")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	routes := strings.Split(rawRoutes, ",")
	for i := range routes {
		routes[i] = strings.TrimSpace(routes[i])
	}
	out := filepath.Join(root, "build", "ouroboros", "o0.2", "browser-smoke-routes-test")
	_ = os.RemoveAll(out)
	result, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      root,
		ArtifactRoot:  out,
		FixtureApp:    filepath.Join(root, "examples", "ouroboros-corpus"),
		CorpusPath:    filepath.Join(root, "examples", "ouroboros-corpus", "fixtures.v1.json"),
		Serve:         true,
		Samples:       "smoke",
		Routes:        routes,
		Headless:      true,
		Timeout:       45 * time.Second,
		Trace:         false,
		Coverage:      false,
		HeapSnapshots: false,
		Environment:   "headless-logic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Canonical {
		t.Fatal("smoke run reported canonical")
	}
}

func TestBrowserBaselineTraceCoverageWindowR06(t *testing.T) {
	if os.Getenv("GOSX_OUROBOROS_TRACE_WINDOW_SMOKE") != "1" {
		t.Skip("set GOSX_OUROBOROS_TRACE_WINDOW_SMOKE=1 to run Chrome trace/coverage window smoke")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "build", "ouroboros", "o0.2", "browser-trace-window-test")
	_ = os.RemoveAll(out)
	result, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      root,
		ArtifactRoot:  out,
		FixtureApp:    filepath.Join(root, "examples", "ouroboros-corpus"),
		CorpusPath:    filepath.Join(root, "examples", "ouroboros-corpus", "fixtures.v1.json"),
		Serve:         true,
		Samples:       "smoke",
		Routes:        []string{"R06"},
		Headless:      true,
		Timeout:       60 * time.Second,
		Trace:         true,
		Coverage:      true,
		HeapSnapshots: false,
		Environment:   "headless-logic",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(result.RawSamplesPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("raw sample lines = %d", len(lines))
	}
	for _, line := range lines {
		var sample BrowserRawSample
		if err := json.Unmarshal([]byte(line), &sample); err != nil {
			t.Fatal(err)
		}
		if !sample.Trace.Captured {
			t.Fatalf("%s/%s missing trace", sample.RouteID, sample.CacheMode)
		}
		if !sample.Coverage.Captured {
			t.Fatalf("%s/%s missing coverage", sample.RouteID, sample.CacheMode)
		}
		if sample.RuntimeJSONDrain != nil {
			t.Fatalf("%s/%s product sample leaked runtime JSON drain", sample.RouteID, sample.CacheMode)
		}
		if len(sample.ProbeEvents) != 0 {
			t.Fatalf("%s/%s product sample leaked probe events: %#v", sample.RouteID, sample.CacheMode, sample.ProbeEvents)
		}
	}
}

func TestValidateBrowserBaselineFailsCanonicalOnBadSample(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{{
		SchemaVersion: BrowserBaselineSchemaVersion,
		RouteID:       "R00",
		CacheMode:     "cold",
		Source:        SourceIdentity{BaseRevision: "abc1234", OverlayHash: "sha256:clean"},
		Proofs:        ProofBundle{FailClosed: true, FirstUsable: ProofPayload{Name: "first", OK: false}},
		Page:          &perf.PageReport{},
		Errors:        []string{"synthetic failure"},
	}}, SourceIdentity{BaseRevision: "abc1234", OverlayHash: "sha256:clean"}, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{})
	if validation.Status != "fail" {
		t.Fatalf("validation status = %q", validation.Status)
	}
	if len(validation.Errors) == 0 {
		t.Fatal("expected canonical errors")
	}
}

func TestCollectBrowserEnvironmentRecordsRemoteMetadataWithoutLocalChrome(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "ws://user:secret@127.0.0.1:1/devtools/browser/abc?token=secret")

	env, err := CollectBrowserEnvironment(t.Context(), BrowserBaselineOptions{
		Timeout:        time.Millisecond,
		Headless:       true,
		Environment:    "hardware-webgpu",
		ViewportWidth:  1440,
		ViewportHeight: 900,
		DPR:            1,
	})
	if err != nil {
		t.Fatalf("CollectBrowserEnvironment: %v", err)
	}
	if got := env.Browser["connectionMode"]; got != "remote-cdp" {
		t.Fatalf("connectionMode = %#v, want remote-cdp", got)
	}
	if got := env.Browser["executable"]; got != "remote-cdp" {
		t.Fatalf("executable = %#v, want remote-cdp", got)
	}
	if got := env.Browser["headless"]; got != "remote-not-controlled" {
		t.Fatalf("headless = %#v, want remote-not-controlled", got)
	}
	if got := env.Browser["flags"]; got != "remote-not-controlled" {
		t.Fatalf("flags = %#v, want remote-not-controlled", got)
	}
	if got := env.Browser["closeSemantics"]; got != "session-only" {
		t.Fatalf("closeSemantics = %#v, want session-only", got)
	}
	if _, ok := env.Browser["remoteEndpoint"]; ok {
		t.Fatalf("remoteEndpoint display value must not be recorded: %#v", env.Browser["remoteEndpoint"])
	}
	hash, _ := env.Browser["remoteEndpointSHA256"].(string)
	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("remoteEndpointSHA256 = %q, want sha256", hash)
	}
	if env.HardwareClassification == "hardware-webgpu" {
		t.Fatalf("classification used requested label without observed GPU truth")
	}
}

func TestRedactRemoteEndpointErrorRemovesSecretsPathAndBrowserID(t *testing.T) {
	msg := redactRemoteEndpointError(errors.New(`dial ws://user:secret@127.0.0.1:9222/devtools/browser/abc123?token=secret#frag failed; retry https://admin:pw@example.test/json/version?x=1`))
	for _, forbidden := range []string{"user", "secret", "token", "devtools", "abc123", "json/version", "admin", "pw", "x=1"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("redacted error leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "remote-cdp-endpoint") {
		t.Fatalf("redacted error missing placeholder: %q", msg)
	}
}

func TestRemoteBoundaryRedactsReturnedErrorFailureAndCommandLog(t *testing.T) {
	raw := "ws://user:secret@127.0.0.1:1/devtools/browser/abc123?token=secret#frag"
	opts := BrowserBaselineOptions{ArtifactRoot: t.TempDir(), ChromeWebSocketURL: raw}
	err := redactRemoteEndpointErrorForOptions(opts, errors.New("sample failed against "+raw+" and /devtools/browser/abc123?token=secret plus devtools/browser/abc123 and frag"))
	assertNoRemoteEndpointLeak(t, "returned error", err.Error())

	writeRemoteBoundaryFailure(opts, err)
	failure, readErr := os.ReadFile(filepath.Join(opts.ArtifactRoot, "failure.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	assertNoRemoteEndpointLeak(t, "failure.json", string(failure))

	var log bytes.Buffer
	logBrowserCommand(&log, opts, "gosx perf ouroboros", []string{"--chrome-ws-url=" + raw, "--note=/devtools/browser/abc123?token=secret", "--f=frag", "--relative=devtools/browser/abc123"})
	assertNoRemoteEndpointLeak(t, "commands.log", log.String())
}

func TestRunBrowserBaselineRemoteDialErrorRedactedInArtifacts(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw := "ws://user:secret@127.0.0.1:1/devtools/browser/abc123?token=secret#frag"
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", raw)
	oldArgs := os.Args
	os.Args = []string{"gosx", "perf", "ouroboros", "--chrome-ws-url=" + raw}
	defer func() { os.Args = oldArgs }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>R00</title></head><body data-route-id="R00"></body></html>`)
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "remote-redaction")
	_, err = RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      root,
		ArtifactRoot:  out,
		CorpusPath:    filepath.Join(root, "examples", "ouroboros-corpus", "fixtures.v1.json"),
		BaseURL:       srv.URL,
		Samples:       "smoke",
		Routes:        []string{"R00"},
		Headless:      true,
		Timeout:       20 * time.Millisecond,
		Trace:         false,
		Coverage:      false,
		HeapSnapshots: false,
		Environment:   "headless-logic",
	})
	if err == nil {
		t.Fatal("expected remote dial failure")
	}
	assertNoRemoteEndpointLeak(t, "CLI-visible returned error", err.Error())
	for _, path := range []string{
		filepath.Join(out, "failure.json"),
		filepath.Join(out, "manifest.json"),
		filepath.Join(out, "commands.log"),
	} {
		body, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		assertNoRemoteEndpointLeak(t, filepath.Base(path), string(body))
	}
}

func assertNoRemoteEndpointLeak(t *testing.T, label, text string) {
	t.Helper()
	for _, forbidden := range []string{
		"user",
		"secret",
		"token",
		"abc123",
		"devtools/browser",
		"/devtools",
		"frag",
		"token=secret",
		"chrome-ws-url=ws://",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("%s leaked %q in %q", label, forbidden, text)
		}
	}
}

func TestRecordBrowserVersionUpdatesEnvironmentMetadata(t *testing.T) {
	env := EnvironmentReport{
		Tools:   map[string]string{"browserVersion": "unknown"},
		Browser: map[string]any{},
	}
	recordBrowserVersion(&env, "1.3", "Chrome/140.0.1", "rev-1", "UA", "14.0")

	if env.Tools["browserVersion"] != "Chrome/140.0.1" {
		t.Fatalf("tools browserVersion = %q", env.Tools["browserVersion"])
	}
	for key, want := range map[string]any{
		"protocolVersion": "1.3",
		"product":         "Chrome/140.0.1",
		"revision":        "rev-1",
		"userAgent":       "UA",
		"jsVersion":       "14.0",
	} {
		if got := env.Browser[key]; got != want {
			t.Fatalf("browser[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestClassifyHardwareRequiresObservedGPUTruth(t *testing.T) {
	if got := classifyHardware("hardware-webgpu", map[string]any{"webgpuAvailable": true}); got != "headless-logic" {
		t.Fatalf("navigator.gpu-only class = %q, want headless-logic", got)
	}
	if got := classifyHardware("hardware-webgpu", map[string]any{
		"webgpuAdapter": map[string]any{"isFallbackAdapter": true},
	}); got != "headless-logic" {
		t.Fatalf("fallback adapter class = %q, want headless-logic", got)
	}
	if got := classifyHardware("hardware-webgpu", map[string]any{
		"webgpuAdapter": map[string]any{"isFallbackAdapter": "false", "info": map[string]any{}},
	}); got != "headless-logic" {
		t.Fatalf("empty/coerced adapter class = %q, want headless-logic", got)
	}
	if got := classifyHardware("hardware-webgpu", map[string]any{
		"webgl": map[string]any{"renderer": "Google SwiftShader"},
	}); got != "software-raster" {
		t.Fatalf("software class = %q, want software-raster", got)
	}
	if got := classifyHardware("hardware-webgpu", map[string]any{
		"webgpuAdapter": map[string]any{"isFallbackAdapter": false, "info": map[string]any{"description": "llvmpipe LLVM 18"}},
	}); got != "software-raster" {
		t.Fatalf("llvmpipe class = %q, want software-raster", got)
	}
	if got := classifyHardware("hardware-webgpu", map[string]any{
		"webgl": map[string]any{"renderer": "ANGLE (Microsoft Basic Render Driver WARP)"},
	}); got != "software-raster" {
		t.Fatalf("warp class = %q, want software-raster", got)
	}
	if got := classifyHardware("headless-logic", map[string]any{
		"webgpuAvailable": true,
		"webgpuAdapter":   map[string]any{"isFallbackAdapter": false, "info": map[string]any{"vendor": "NVIDIA"}},
		"webgl":           map[string]any{"renderer": "ANGLE (NVIDIA GeForce RTX Direct3D11)"},
	}); got != "hardware-webgpu" {
		t.Fatalf("webgpu hardware class = %q, want hardware-webgpu", got)
	}
	if got := classifyHardware("headless-logic", map[string]any{
		"webgl": map[string]any{"renderer": "ANGLE (Intel UHD Graphics Direct3D11)"},
	}); got != "hardware-webgl" {
		t.Fatalf("webgl hardware class = %q, want hardware-webgl", got)
	}
}

func TestValidateBrowserBaselineFailsCanonicalForNonHardwareClass(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	source := testBrowserSourceIdentityWithAudit(testCompatibilityAuditIdentity("pass", true, nil))
	for _, class := range []string{"", "unknown", "headless-logic", "software-raster"} {
		validation := ValidateBrowserBaseline(plan, []BrowserRawSample{testValidBrowserSample(source)}, source, EnvironmentReport{HardwareClassification: class}, BrowserBaselineOptions{
			Trace:         true,
			Coverage:      true,
			HeapSnapshots: true,
			PixelManifest: "pixels.json",
		})
		if !containsString(validation.Errors, "canonical run requires observed hardware-webgpu or hardware-webgl classification") {
			t.Fatalf("class %q errors = %+v", class, validation.Errors)
		}
	}
	for _, class := range []string{"hardware-webgpu", "hardware-webgl"} {
		validation := ValidateBrowserBaseline(plan, []BrowserRawSample{testValidBrowserSample(source)}, source, EnvironmentReport{HardwareClassification: class}, BrowserBaselineOptions{
			Trace:         true,
			Coverage:      true,
			HeapSnapshots: true,
			PixelManifest: "pixels.json",
		})
		if containsString(validation.Errors, "canonical run requires observed hardware-webgpu or hardware-webgl classification") {
			t.Fatalf("class %q unexpectedly rejected: %+v", class, validation.Errors)
		}
	}
}

func TestValidateBrowserBaselineAllowsR00WithoutRuntimeCalls(t *testing.T) {
	plan, err := samplingPlan("smoke")
	if err != nil {
		t.Fatal(err)
	}
	source := SourceIdentity{BaseRevision: "abc1234", OverlayHash: "sha256:clean", RuntimeJSONStatic: testRuntimeJSONStaticIdentity()}
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{{
		SchemaVersion: BrowserBaselineSchemaVersion,
		RouteID:       "R00",
		SampleLane:    SampleLaneProduct,
		CacheMode:     "cold",
		Source:        source,
		Proofs:        ProofBundle{FirstUsable: ProofPayload{Name: "ssr-no-runtime", OK: true}},
		Page:          &perf.PageReport{},
		Network:       []NetworkRecord{{URL: "http://127.0.0.1/ssr-only", Role: "Document", Status: 200}},
	}}, source, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{})
	if validation.Status != "pass" {
		t.Fatalf("validation status = %q errors = %v", validation.Status, validation.Errors)
	}
}

func TestValidateBrowserBaselineWarnsForUnavailableCompatibilityAuditInSmoke(t *testing.T) {
	plan, err := samplingPlan("smoke")
	if err != nil {
		t.Fatal(err)
	}
	source := testBrowserSourceIdentityWithAudit(testCompatibilityAuditIdentity("fail-closed", false, nil))
	sample := testValidBrowserSample(source)
	sample.ProbeEvents = nil
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{sample}, source, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{})
	if validation.Status != "pass" {
		t.Fatalf("validation status = %q errors = %v", validation.Status, validation.Errors)
	}
	if !containsString(validation.Warnings, "compatibility audit unavailable; run cannot become canonical") {
		t.Fatalf("warnings = %+v", validation.Warnings)
	}
}

func TestValidateBrowserBaselineFailsCanonicalForUnavailableCompatibilityAudit(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	source := testBrowserSourceIdentityWithAudit(testCompatibilityAuditIdentity("fail-closed", false, nil))
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{testValidBrowserSample(source)}, source, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{
		Trace:         true,
		Coverage:      true,
		HeapSnapshots: true,
	})
	if validation.Status != "fail" {
		t.Fatalf("validation status = %q warnings = %v", validation.Status, validation.Warnings)
	}
	for _, want := range []string{
		"canonical run requires passing compatibility audit",
		"canonical run requires compatibility audit canonical availability",
	} {
		if !containsString(validation.Errors, want) {
			t.Fatalf("errors = %+v, missing %q", validation.Errors, want)
		}
	}
}

func TestValidateBrowserBaselineFailsCanonicalForCompatibilityDrift(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	audit := testCompatibilityAuditIdentity("pass", true, nil)
	audit.Reconciliation.AddedSinceAnchorCount = 1
	audit.Reconciliation.AddedSinceAnchorHash = "sha256:added"
	source := testBrowserSourceIdentityWithAudit(audit)
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{testValidBrowserSample(source)}, source, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{
		Trace:         true,
		Coverage:      true,
		HeapSnapshots: true,
	})
	if !containsString(validation.Errors, "canonical run requires zero compatibility anchor/current drift") {
		t.Fatalf("errors = %+v", validation.Errors)
	}
}

func TestCanonicalEvidenceRefsRequireSeparateEvidenceRoot(t *testing.T) {
	root := t.TempDir()
	err := validateCanonicalEvidenceRefs(BrowserBaselineOptions{
		ArtifactRoot:  root,
		EvidenceRoot:  root,
		PixelManifest: "pixel.json",
	}, testBrowserSourceIdentityWithAudit(nil))
	if err == nil || !strings.Contains(err.Error(), "evidence-root") {
		t.Fatalf("validateCanonicalEvidenceRefs error = %v", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err = validateCanonicalEvidenceRefs(BrowserBaselineOptions{
		ArtifactRoot:  alias,
		EvidenceRoot:  root,
		PixelManifest: "pixel.json",
	}, testBrowserSourceIdentityWithAudit(nil))
	if err == nil || !strings.Contains(err.Error(), "evidence-root") {
		t.Fatalf("validateCanonicalEvidenceRefs symlink alias error = %v", err)
	}
}

func TestCanonicalEvidenceRefsRequireContainedInventoryRef(t *testing.T) {
	root := t.TempDir()
	source := testBrowserSourceIdentityWithAudit(nil)
	source.InventoryRef = "/tmp/external/source-inventory.json"
	err := validateCanonicalEvidenceRefs(BrowserBaselineOptions{
		ArtifactRoot:  filepath.Join(root, "artifact"),
		EvidenceRoot:  filepath.Join(root, "evidence"),
		PixelManifest: "pixel.json",
	}, source)
	if err == nil || !strings.Contains(err.Error(), "inventory ref") {
		t.Fatalf("validateCanonicalEvidenceRefs absolute ref error = %v", err)
	}
	source.InventoryRef = "../external/source-inventory.json"
	err = validateCanonicalEvidenceRefs(BrowserBaselineOptions{
		ArtifactRoot:  filepath.Join(root, "artifact"),
		EvidenceRoot:  filepath.Join(root, "evidence"),
		PixelManifest: "pixel.json",
	}, source)
	if err == nil || !strings.Contains(err.Error(), "inventory ref") {
		t.Fatalf("validateCanonicalEvidenceRefs parent ref error = %v", err)
	}
	source.InventoryRef = "source/source-inventory.json"
	err = validateCanonicalEvidenceRefs(BrowserBaselineOptions{
		ArtifactRoot:  filepath.Join(root, "artifact"),
		EvidenceRoot:  filepath.Join(root, "evidence"),
		PixelManifest: "pixel.json",
	}, source)
	if err == nil || strings.Contains(err.Error(), "inventory ref") {
		t.Fatalf("validateCanonicalEvidenceRefs contained inventory ref error = %v", err)
	}
}

func TestResolveEvidenceRefRejectsMissingAndEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := resolveEvidenceRef(root, "../escape.json"); err == nil {
		t.Fatal("resolveEvidenceRef accepted escape")
	}
	if _, err := resolveEvidenceRef(root, "missing.json"); err == nil {
		t.Fatal("resolveEvidenceRef accepted missing ref")
	}
	path := filepath.Join(root, "ok.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveEvidenceRef(root, "ok.json"); err != nil || got != path {
		t.Fatalf("resolveEvidenceRef = %q, %v", got, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolveEvidenceRef(root, "link.json"); err == nil {
		t.Fatal("resolveEvidenceRef accepted symlink escape")
	}
}

func TestProductPathPrefixesForSampleUsesLoadedRuntimeAssetsOnly(t *testing.T) {
	prefixes := productPathPrefixesForSample(BrowserRawSample{Network: []NetworkRecord{
		{URL: "http://127.0.0.1:8080/gosx/runtime.js", RuntimeAssetRole: "runtime"},
		{URL: "http://127.0.0.1:8080/gosx/bootstrap.js", RuntimeAssetRole: "bootstrap"},
		{URL: "http://127.0.0.1:8080/assets/app.js", RuntimeAssetRole: ""},
		{URL: "http://127.0.0.1:8080/media/video.mp4", RuntimeAssetRole: "runtime", UnresolvedAssetRole: true},
	}})
	want := []string{"/gosx/bootstrap.js", "/gosx/runtime.js"}
	if !sameStringSlice(prefixes, want) {
		t.Fatalf("productPathPrefixesForSample = %#v, want %#v", prefixes, want)
	}
}

func TestBrowserDynamicLaneMappingKeepsProbeOverheadDistinct(t *testing.T) {
	tests := []struct {
		sample BrowserRawSample
		want   string
		ok     bool
	}{
		{sample: BrowserRawSample{SampleLane: SampleLaneProduct}, want: RuntimeJSONDynamicLaneProduct, ok: true},
		{sample: BrowserRawSample{SampleLane: SampleLaneProbe}, want: RuntimeJSONDynamicLaneProbe, ok: true},
		{sample: BrowserRawSample{SampleLane: SampleLaneProbeOverhead}, want: RuntimeJSONDynamicLaneProbeOverhead, ok: true},
		{sample: BrowserRawSample{SampleLane: SampleLane("extra")}, ok: false},
	}
	for _, tt := range tests {
		got, ok := runtimeJSONDynamicLaneForBrowserSample(tt.sample)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("runtimeJSONDynamicLaneForBrowserSample(%q) = %q, %v; want %q, %v", tt.sample.SampleLane, got, ok, tt.want, tt.ok)
		}
	}
}

func TestOuroborosDriverOptionsPropagateBrowserTimeout(t *testing.T) {
	const remote = "ws://127.0.0.1:9222/devtools/browser/test"
	opts := BrowserBaselineOptions{
		Headless:           false,
		Timeout:            120 * time.Second,
		ChromeWebSocketURL: remote,
	}
	d := &perf.Driver{}
	for _, option := range ouroborosDriverOptions(opts) {
		option(d)
	}

	driverValue := reflect.ValueOf(d).Elem()
	if got := time.Duration(driverValue.FieldByName("timeout").Int()); got != opts.Timeout {
		t.Fatalf("driver timeout = %s, want %s", got, opts.Timeout)
	}
	if got := driverValue.FieldByName("headless").Bool(); got != opts.Headless {
		t.Fatalf("driver headless = %v, want %v", got, opts.Headless)
	}
	if got := driverValue.FieldByName("remoteWSURL").String(); got != remote {
		t.Fatalf("driver remote websocket URL = %q, want %q", got, remote)
	}
}

func TestProductDriverInstallsNoPageProbe(t *testing.T) {
	d, err := newOuroborosDriver(t.Context(), BrowserBaselineOptions{Headless: true, Timeout: 20 * time.Second}, SampleLaneProduct)
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("newOuroborosDriver: %v", err)
		}
		t.Skipf("skipping product preload test: %v", err)
	}
	defer d.Close()
	if err := d.Navigate("data:text/html,<html><body>product</body></html>"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var state struct {
		GenericProbe bool `json:"genericProbe"`
		RuntimeProbe bool `json:"runtimeProbe"`
		PerfProbe    bool `json:"perfProbe"`
	}
	if err := d.Evaluate(`({
		genericProbe: !!window.__gosxOuroborosProbe,
		runtimeProbe: !!(window.__gosxOuroborosProbe && window.__gosxOuroborosProbe.runtimeJSONProbe),
		perfProbe: !!window.__gosx_perf
	})`, &state); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if state.GenericProbe || state.RuntimeProbe || state.PerfProbe {
		t.Fatalf("product lane installed preload probes: %+v", state)
	}
}

func TestValidateBrowserBaselineRejectsRuntimeJSONProbeLeakInProduct(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	source := testBrowserSourceIdentityWithAudit(testCompatibilityAuditIdentity("pass", true, nil))
	sample := testValidBrowserSample(source)
	sample.RuntimeJSONDrain = &RuntimeJSONRawDrain{SchemaVersion: RuntimeJSONProbeSchemaVersion}
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{sample}, source, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{
		Trace:         true,
		Coverage:      true,
		HeapSnapshots: true,
		PixelManifest: "pixel-evidence.json",
	})
	if !containsString(validation.Errors, "R00/cold: product sample leaked full runtime JSON probe") {
		t.Fatalf("errors = %+v", validation.Errors)
	}
}

func TestValidateBrowserBaselineRejectsGenericProbeLeakInProduct(t *testing.T) {
	plan, err := samplingPlan("baseline")
	if err != nil {
		t.Fatal(err)
	}
	source := testBrowserSourceIdentityWithAudit(testCompatibilityAuditIdentity("pass", true, nil))
	sample := testValidBrowserSample(source)
	sample.ProbeEvents = []ProbeEvent{{Kind: "probe", Phase: "route-load", Name: "install"}}
	validation := ValidateBrowserBaseline(plan, []BrowserRawSample{sample}, source, EnvironmentReport{HardwareClassification: "hardware-webgl"}, BrowserBaselineOptions{
		Trace:         true,
		Coverage:      true,
		HeapSnapshots: true,
		PixelManifest: "pixel-evidence.json",
	})
	if !containsString(validation.Errors, "R00/cold: product sample leaked probe events") {
		t.Fatalf("errors = %+v", validation.Errors)
	}
}

func TestRuntimeJSONDynamicEvidenceRejectsMixedGenericProbeKinds(t *testing.T) {
	source, samples := browserDynamicEvidenceFixtureSamples()
	for i := range samples {
		if samples[i].SampleLane == SampleLaneProbe && samples[i].RouteID == "R00" && samples[i].CacheMode == "cold" {
			samples[i].RuntimeJSONDrain.Events = append(samples[i].RuntimeJSONDrain.Events, ProbeEvent{
				Kind:   "interaction",
				Name:   "route marker",
				Phase:  "input",
				Detail: map[string]any{"routeID": "R00"},
			})
			break
		}
	}
	_, err := buildRuntimeJSONDynamicEvidenceFromSamples(source, samples)
	if err == nil || !strings.Contains(err.Error(), "has invalid kind: interaction") {
		t.Fatalf("buildRuntimeJSONDynamicEvidenceFromSamples error = %v", err)
	}
}

func TestRuntimeJSONDynamicEvidencePairsProductPilotsWithRuntimeProbeOverhead(t *testing.T) {
	source, samples := browserDynamicEvidenceFixtureSamples()
	manifest, err := buildRuntimeJSONDynamicEvidenceFromSamples(source, samples)
	if err != nil {
		t.Fatalf("buildRuntimeJSONDynamicEvidenceFromSamples: %v", err)
	}
	wantPairs := len(canonicalRouteIDs()) * 2 * 2
	if len(manifest.OverheadPairs) != wantPairs {
		t.Fatalf("overhead pair count = %d, want %d", len(manifest.OverheadPairs), wantPairs)
	}
	samplesByID := map[string]RuntimeJSONDynamicSample{}
	for _, sample := range manifest.Samples {
		samplesByID[sample.ID] = sample
	}
	for _, pair := range manifest.OverheadPairs {
		product := samplesByID[pair.ProductSampleID]
		probe := samplesByID[pair.ProbeSampleID]
		if product.Drain != nil {
			t.Fatalf("product pilot carried drain in pair %+v", pair)
		}
		if probe.Drain == nil || probe.Drain.SchemaVersion != RuntimeJSONProbeSchemaVersion {
			t.Fatalf("probe-overhead pilot did not carry runtime JSON drain in pair %+v", pair)
		}
		if probe.Lane != RuntimeJSONDynamicLaneProbeOverhead || product.Lane != RuntimeJSONDynamicLaneProduct {
			t.Fatalf("bad overhead lanes: product=%s probe=%s", product.Lane, probe.Lane)
		}
	}
}

func browserDynamicEvidenceFixtureSamples() (SourceIdentity, []BrowserRawSample) {
	knownGlobals := []string{"__gosx_action", "__gosx_canvas_event"}
	source := SourceIdentity{
		BaseRevision:                "abc1234",
		OverlayHash:                 "sha256:overlay",
		TrackedDiffHash:             "sha256:tracked",
		UntrackedIncludedSourceHash: "sha256:untracked",
		InventorySHA256:             "sha256:inventory",
		RuntimeProbeNames:           knownGlobals,
		RuntimeJSONStatic: &RuntimeJSONStaticIdentity{
			SourceIdentityHash: runtimeJSONDynamicSourceBindingHash(RuntimeJSONDynamicSourceBinding{
				BaseRevision:                "abc1234",
				OverlayHash:                 "sha256:overlay",
				TrackedDiffHash:             "sha256:tracked",
				UntrackedIncludedSourceHash: "sha256:untracked",
				InventorySHA256:             "sha256:inventory",
			}),
			SemanticHash:    "sha256:semantic",
			CountsHash:      "sha256:counts",
			GlobalNameHash:  RuntimeJSONStaticGlobalNameHash(knownGlobals),
			ScannerVersion:  runtimeJSONStaticScannerVersion,
			PhaseClassifier: runtimeJSONPhaseClassifierVersion,
		},
	}
	var samples []BrowserRawSample
	requiredProduct := map[string]bool{"R02": true, "R03": true, "R05": true, "R06": true, "R08": true, "R09A": true, "R09B": true, "R10": true}
	for _, routeID := range canonicalRouteIDs() {
		for _, cacheMode := range []string{"cold", "warm"} {
			for i := 0; i < 2; i++ {
				samples = append(samples,
					browserDynamicEvidenceSample(source, SampleLaneProduct, routeID, cacheMode, i, true, nil),
					browserDynamicEvidenceSample(source, SampleLaneProbeOverhead, routeID, cacheMode, i, true, []ProbeEvent{browserProbeInstallEvent(routeID)}),
				)
			}
			events := []ProbeEvent{browserProbeInstallEvent(routeID), browserHarnessJSONProbeEvent(routeID)}
			if requiredProduct[routeID] {
				if routeID == "R05" {
					events = append(events, browserProductRuntimeProbeEvent(routeID, "__gosx_canvas_event", 3, 3))
				} else {
					events = append(events, browserProductRuntimeProbeEvent(routeID, "__gosx_action", 0, 1))
				}
			}
			samples = append(samples, browserDynamicEvidenceSample(source, SampleLaneProbe, routeID, cacheMode, 0, false, events))
		}
	}
	return source, samples
}

func browserDynamicEvidenceSample(source SourceIdentity, lane SampleLane, routeID, cacheMode string, index int, pilot bool, events []ProbeEvent) BrowserRawSample {
	var drain *RuntimeJSONRawDrain
	if lane == SampleLaneProbe || lane == SampleLaneProbeOverhead {
		drain = &RuntimeJSONRawDrain{
			SchemaVersion:       RuntimeJSONProbeSchemaVersion,
			FacadeSchemaVersion: 1,
			Version:             "1",
			Phase:               "input",
			RouteID:             routeID,
			Events:              events,
			WrappedGlobals:      browserWrappedGlobalsForProbeEvents(events),
			KnownGlobals:        []string{"__gosx_action", "__gosx_canvas_event"},
			Limits:              RuntimeJSONRawDrainLimits{EventLimit: 8192},
		}
	}
	return BrowserRawSample{
		SchemaVersion:    BrowserBaselineSchemaVersion,
		Source:           source,
		RouteID:          routeID,
		CacheMode:        cacheMode,
		SampleLane:       lane,
		SampleIndex:      index,
		DurationMs:       float64(100 + index),
		Pilot:            pilot,
		Discarded:        pilot,
		RuntimeJSONDrain: drain,
		Network:          []NetworkRecord{{URL: "http://127.0.0.1:8080/assets/app.js", RuntimeAssetRole: "runtime"}},
	}
}

func browserProbeInstallEvent(routeID string) ProbeEvent {
	return ProbeEvent{Kind: "probe", Name: "install", Phase: "route-load", Detail: map[string]any{"routeID": routeID}}
}

func browserHarnessJSONProbeEvent(routeID string) ProbeEvent {
	return ProbeEvent{Kind: "json-call", Name: "JSON.parse", Phase: "input", Detail: map[string]any{
		"routeID":      routeID,
		"operation":    "JSON.parse",
		"payloadBytes": float64(12),
		"resultBytes":  float64(4),
		"exception":    "",
		"stackHash":    "harness-stack",
		"source":       map[string]any{"path": "/__gosx_ouroboros_harness/runner.js", "line": float64(1), "column": float64(1), "urlHash": "h1"},
	}}
}

func browserProductRuntimeProbeEvent(routeID, name string, eventKind, argCount int) ProbeEvent {
	detail := map[string]any{
		"routeID":     routeID,
		"argCount":    float64(argCount),
		"argTypes":    []any{"object", "number", "string"},
		"argBytes":    []any{float64(1), float64(2), float64(3)},
		"resultType":  "undefined",
		"resultBytes": float64(1),
		"exception":   "",
		"async":       false,
		"stackHash":   "product-stack",
		"source":      map[string]any{"path": "/assets/app.js", "line": float64(2), "column": float64(3), "urlHash": "p1"},
	}
	if eventKind > 0 {
		detail["eventKind"] = float64(eventKind)
	}
	return ProbeEvent{Kind: "runtime-call", Name: name, Phase: "input", Detail: detail}
}

func browserWrappedGlobalsForProbeEvents(events []ProbeEvent) []string {
	seen := map[string]bool{}
	var wrapped []string
	for _, event := range events {
		if event.Kind != "runtime-call" || event.Name == "" || seen[event.Name] {
			continue
		}
		seen[event.Name] = true
		wrapped = append(wrapped, event.Name)
	}
	return wrapped
}

func TestValidateCanonicalPixelManifestRejectsUnboundSourceIdentity(t *testing.T) {
	root := t.TempDir()
	manifest := writeValidPixelManifestForD(t, root, "R08", "webgl")
	source := testBrowserSourceIdentityWithAudit(nil)
	path := filepath.Join(root, "pixel-evidence.json")
	err := validateCanonicalPixelManifest(path, manifest, BrowserBaselineOptions{ViewportWidth: 1440, ViewportHeight: 900, DPR: 1}, source)
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("validateCanonicalPixelManifest error = %v", err)
	}
}

func TestValidateCanonicalPixelManifestAcceptsDefaultCanvasSelector(t *testing.T) {
	root := t.TempDir()
	source := SourceIdentity{
		BaseRevision:    "abc1234",
		OverlayHash:     "sha256:clean",
		InventorySHA256: "sha256:" + strings.Repeat("a", 64),
	}
	pixelSource := visual.PixelSourceIdentity{
		BaseRevision:    source.BaseRevision,
		OverlayHash:     source.OverlayHash,
		InventorySHA256: source.InventorySHA256,
	}
	manifest := writeValidCanonicalPixelManifestForSelector(t, root, "R08", "webgl", visual.DefaultPixelCanvasSelector, pixelSource)
	path := filepath.Join(root, "pixel-evidence.json")

	err := validateCanonicalPixelManifest(path, manifest, BrowserBaselineOptions{ViewportWidth: 1440, ViewportHeight: 900, DPR: 1}, source)
	if err != nil {
		t.Fatalf("validateCanonicalPixelManifest rejected default selector manifest: %v", err)
	}
}

func TestValidateCanonicalPixelBaselineStillAllowsExplicitCanvasSelector(t *testing.T) {
	root := t.TempDir()
	source := visual.PixelSourceIdentity{
		BaseRevision:    "abc1234",
		OverlayHash:     "sha256:clean",
		InventorySHA256: "sha256:" + strings.Repeat("a", 64),
	}
	writeValidCanonicalPixelManifestForSelector(t, root, "R08", "webgl", "canvas", source)

	_, err := visual.ValidateCanonicalPixelBaselineManifest(filepath.Join(root, "pixel-evidence.json"), source, visual.PixelEvidenceOptions{
		Mode:           visual.PixelModeRecordBaseline,
		RouteID:        "R08",
		Backend:        visual.RequireBackendWebGL,
		ForceWebGL:     true,
		CanvasSelector: "canvas",
		WarmupFrames:   30,
		Viewport:       visual.Viewport{Width: 1440, Height: 900, Scale: 1},
	})
	if err != nil {
		t.Fatalf("ValidateCanonicalPixelBaselineManifest rejected explicit selector manifest: %v", err)
	}
}

func TestValidateSceneSamplePixelManifestRefsRequiresExactRouteBackends(t *testing.T) {
	root := t.TempDir()
	webgpuDir := filepath.Join(root, "r08-webgpu")
	webglDir := filepath.Join(root, "r08-webgl")
	wrongRouteDir := filepath.Join(root, "r10-webgpu")
	duplicateDir := filepath.Join(root, "r08-webgpu-2")
	for _, dir := range []string{webgpuDir, webglDir, wrongRouteDir, duplicateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeValidPixelManifestForD(t, webgpuDir, "R08", "webgpu")
	writeValidPixelManifestForD(t, webglDir, "R08", "webgl")
	writeValidPixelManifestForD(t, wrongRouteDir, "R10", "webgpu")
	writeValidPixelManifestForD(t, duplicateDir, "R08", "webgpu")
	sample := BrowserRawSample{
		RouteID: "R08",
		Artifacts: SampleArtifacts{PixelManifestRefs: []string{
			"r08-webgpu/pixel-evidence.json",
			"r08-webgl/pixel-evidence.json",
		}},
	}
	opts := BrowserBaselineOptions{EvidenceRoot: root}
	if err := validateSceneSamplePixelManifestRefs(sample, opts); err != nil {
		t.Fatalf("valid refs rejected: %v", err)
	}
	tests := []struct {
		name string
		refs []string
		want string
	}{
		{name: "missing", refs: []string{"r08-webgpu/pixel-evidence.json"}, want: "refs=1 want 2"},
		{name: "duplicate_ref", refs: []string{"r08-webgpu/pixel-evidence.json", "r08-webgpu/pixel-evidence.json"}, want: "duplicate pixel manifest ref"},
		{name: "duplicate_backend", refs: []string{"r08-webgpu/pixel-evidence.json", "r08-webgpu-2/pixel-evidence.json"}, want: "duplicate pixel manifest backend webgpu"},
		{name: "wrong_route", refs: []string{"r10-webgpu/pixel-evidence.json", "r08-webgl/pixel-evidence.json"}, want: "routeID = R10, want R08"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := sample
			bad.Artifacts.PixelManifestRefs = tt.refs
			err := validateSceneSamplePixelManifestRefs(bad, opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateSceneSamplePixelManifestRefs error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRouteStateMachineAssertionsDoNotUseConstantTruth(t *testing.T) {
	js := routeStateMachineJS(FixtureSpec{ID: "R07", RoutePlanAssertions: []string{
		"same-origin sync socket",
		"one island",
		"patch runtime",
		"valid action endpoint",
		"five islands",
		"shared signal program",
		"shared signal manifest entry",
		"declarative action marker",
		"validation response",
		"redirect response",
		"follow sync props",
		"external reference only",
	}}, SampleLaneProduct)
	if strings.Contains(js, "assertions[a] = true") {
		t.Fatal("routeStateMachineJS still contains constant-truth assertion")
	}
	for _, want := range []string{
		"msg.data",
		"media-sync-ready",
		"containsSelectionSignal(program)",
		".counter button:last-of-type",
		"counterAfter !== counterBefore",
		"validationAfter !== validationBefore",
		"invalidResult.status === 422 &&",
		"structured-validation-response",
		"submitFormBrowserRedirectResult",
		"text/html,application/xhtml+xml,*/*;q=0.8",
		`msg.event === "echo"`,
		`{event:"echo", data:{}}`,
		"ignored.push",
		`name === "__welcome"`,
		"unexpected:true",
		`waitFor(function(){`,
		`sharedSignalValue("$ouroboros.echo")`,
		"waitSceneMountCanvas",
		"proveSceneOrbitInput",
		"scene-orbit-state-changed",
		"water-orbit-state-changed",
		"window.__gosxOuroborosSceneBackendWaitMS || 10000",
		"scene-backend-committed",
		"data-gosx-scene3d-render-backend-truth",
		"data-gosx-scene3d-render-gpu",
		"pixelManifestRefs:\"canonical sample artifacts\"",
		"http://gosx.invalid/__gosx_ouroboros_harness/route-state-machine.js",
		"pointerId:1",
		"externalR10Route",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("routeStateMachineJS missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Submit action|Counter",
		"required|error|idle",
		`{type:"echo"}`,
		"msg.payload",
		"scene-backend-nonblank-frame",
		"water-scene-nonblank-frame",
		"dispatchSceneOrbit",
		"scene-orbit-event",
		"water-orbit-event",
	} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("routeStateMachineJS still contains weak proof %q", forbidden)
		}
	}
}

func TestRouteStateMachineR04SplitsStructuredValidationAndBrowserRedirect(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	srv, requests := newR04ProofMockServer(t)
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/action/form"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	bundle, err := executeRouteStateMachine(d, r04ProofRoute(), ExternalRouteEvidence{}, SampleLaneProduct)
	if err != nil {
		t.Fatalf("R04 route state machine: %v bundle=%+v", err, bundle)
	}
	if bundle.FirstUsable.Name != "visible-validation" || !bundle.FirstUsable.OK {
		t.Fatalf("first usable = %+v", bundle.FirstUsable)
	}
	validation := requireProofPayload(t, bundle, "structured-validation-response")
	if got := intFromAny(validation["status"]); got != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d payload=%+v", got, validation)
	}
	redirect := requireProofPayload(t, bundle, "valid-result")
	if redirected, _ := redirect["redirected"].(bool); !redirected {
		t.Fatalf("valid result did not follow a browser redirect: %+v", redirect)
	}
	if accept := stringFromAny(redirect["accept"]); strings.Contains(accept, "application/json") {
		t.Fatalf("redirect proof used JSON accept: %+v", redirect)
	}
	if url := stringFromAny(redirect["url"]); !strings.Contains(url, "/action/form?ok=1") {
		t.Fatalf("redirect final URL = %q payload=%+v", url, redirect)
	}
	requests.mu.Lock()
	defer requests.mu.Unlock()
	if requests.invalidJSON != 1 {
		t.Fatalf("invalid JSON request count = %d", requests.invalidJSON)
	}
	if requests.validBrowser != 1 {
		t.Fatalf("valid browser request count = %d", requests.validBrowser)
	}
	if strings.Contains(requests.validAccept, "application/json") {
		t.Fatalf("valid redirect accept = %q", requests.validAccept)
	}
}

func TestRouteStateMachineR06IgnoresControlFramesBeforeEcho(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	srv := newR06ControlFrameMockServer(t)
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/hub/echo"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var ready bool
	if err := d.Evaluate(`(async function(){
		var deadline = performance.now() + 2000;
		while (performance.now() <= deadline) {
			if (window.__r06Ready) return true;
			await new Promise(function(resolve){ setTimeout(resolve, 25); });
		}
		return false;
	})()`, &ready); err != nil {
		t.Fatalf("wait for page websocket: %v", err)
	}
	if !ready {
		t.Fatal("page websocket did not open")
	}
	bundle, err := executeRouteStateMachine(d, r06ProofRoute(), ExternalRouteEvidence{}, SampleLaneProduct)
	if err != nil {
		t.Fatalf("R06 route state machine: %v bundle=%+v", err, bundle)
	}
	payload := requireProofPayload(t, bundle, "socket-echo-applied")
	echoed, ok := payload["echoed"].(map[string]any)
	if !ok {
		t.Fatalf("missing echoed payload: %+v", payload)
	}
	if event := stringFromAny(echoed["event"]); event != "echo" {
		t.Fatalf("echoed event = %q payload=%+v", event, echoed)
	}
	ignored, ok := echoed["ignored"].([]any)
	if !ok || len(ignored) != 1 {
		t.Fatalf("welcome frame was not recorded as the only ignored frame: %+v", echoed)
	}
	welcome, ok := ignored[0].(map[string]any)
	if !ok || stringFromAny(welcome["event"]) != "__welcome" {
		t.Fatalf("ignored frame = %+v", ignored)
	}
	if signal := stringFromAny(payload["signal"]); signal != "echo" {
		t.Fatalf("echo signal = %q payload=%+v", signal, payload)
	}
}

func TestRouteStateMachineR06RejectsUnexpectedPreEchoEvent(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	srv := newR06MockServer(t, []r06PreEchoFrame{{Event: "presence", Data: map[string]int{"count": 1}}})
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/hub/echo"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var ready bool
	if err := d.Evaluate(`(async function(){
		var deadline = performance.now() + 2000;
		while (performance.now() <= deadline) {
			if (window.__r06Ready) return true;
			await new Promise(function(resolve){ setTimeout(resolve, 25); });
		}
		return false;
	})()`, &ready); err != nil {
		t.Fatalf("wait for page websocket: %v", err)
	}
	if !ready {
		t.Fatal("page websocket did not open")
	}
	bundle, err := executeRouteStateMachine(d, r06ProofRoute(), ExternalRouteEvidence{}, SampleLaneProduct)
	if err == nil {
		t.Fatalf("R06 proof passed with unexpected pre-echo event: %+v", bundle)
	}
	if bundle.FirstUsable.OK {
		t.Fatalf("first usable passed with unexpected event: %+v", bundle.FirstUsable)
	}
	if !containsString(bundle.MissingRequired, "socket-echo-applied") {
		t.Fatalf("missing required = %+v", bundle.MissingRequired)
	}
	payload := requireFailedProofPayload(t, bundle, "socket-echo-applied")
	echoed, ok := payload["echoed"].(map[string]any)
	if !ok {
		t.Fatalf("missing echoed payload: %+v", payload)
	}
	if event := stringFromAny(echoed["event"]); event != "presence" {
		t.Fatalf("unexpected event = %q payload=%+v", event, echoed)
	}
	if unexpected, _ := echoed["unexpected"].(bool); !unexpected {
		t.Fatalf("unexpected flag missing: %+v", echoed)
	}
}

func TestRouteStateMachineR08ValidatesBackendTruth(t *testing.T) {
	tests := []struct {
		name      string
		setup     string
		wantPass  bool
		wantError string
	}{
		{
			name: "late_backend_commit",
			setup: `setTimeout(function(){
				var m = document.querySelector("[data-gosx-scene3d]");
				m.setAttribute("data-gosx-scene3d-backend", "webgl");
				m.setAttribute("data-gosx-scene3d-render-gpu", "true");
				m.setAttribute("data-gosx-scene3d-render-backend-truth", JSON.stringify({backend:"webgl", gpu:true, deviceLost:false, initError:"", lastError:""}));
			}, 120);`,
			wantPass: true,
		},
		{
			name: "late_mount_canvas_and_commit_after_old_bound",
			setup: `window.__gosxOuroborosSceneBackendWaitMS = 2600;
			document.getElementById("scene").remove();
			setTimeout(function(){
				var m = document.createElement("div");
				m.id = "scene";
				m.setAttribute("data-gosx-scene3d", "true");
				var c = document.createElement("canvas");
				c.width = 320;
				c.height = 180;
				m.appendChild(c);
				document.body.appendChild(m);
			}, 100);
			setTimeout(function(){
				commitSceneBackend("webgl", JSON.stringify({backend:"webgl", gpu:true, deviceLost:false, initError:"", lastError:""}));
			}, 2300);`,
			wantPass: true,
		},
		{
			name:      "malformed_truth",
			setup:     `commitSceneBackend("webgl", "not-json");`,
			wantError: "scene-backend-committed",
		},
		{
			name:      "backend_mismatch",
			setup:     `commitSceneBackend("webgl", JSON.stringify({backend:"webgpu", gpu:true, deviceLost:false, initError:"", lastError:""}));`,
			wantError: "scene-backend-committed",
		},
		{
			name:      "device_lost",
			setup:     `commitSceneBackend("webgl", JSON.stringify({backend:"webgl", gpu:true, deviceLost:true, initError:"", lastError:""}));`,
			wantError: "scene-backend-committed",
		},
		{
			name:      "init_error",
			setup:     `commitSceneBackend("webgl", JSON.stringify({backend:"webgl", gpu:true, deviceLost:false, initError:"boom", lastError:""}));`,
			wantError: "scene-backend-committed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := requireOuroborosBrowserDriver(t, 45*time.Second)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = io.WriteString(w, r08BackendMockPage(tt.setup))
			}))
			defer srv.Close()
			if err := d.Navigate(srv.URL + "/scene/basic"); err != nil {
				t.Fatalf("Navigate: %v", err)
			}
			if err := d.WaitReady(); err != nil {
				t.Fatalf("WaitReady: %v", err)
			}
			bundle, err := executeRouteStateMachine(d, r08ProofRoute(), ExternalRouteEvidence{}, SampleLaneProduct)
			if tt.wantPass {
				if err != nil {
					t.Fatalf("R08 proof failed: %v bundle=%+v", err, bundle)
				}
				if bundle.FirstUsable.Name != "scene-backend-committed" || !bundle.FirstUsable.OK {
					t.Fatalf("first usable = %+v", bundle.FirstUsable)
				}
				payload := requireProofPayload(t, bundle, "scene-backend-committed")
				scene, ok := payload["scene3d"].(map[string]any)
				if !ok {
					t.Fatalf("missing scene3d payload: %+v", payload)
				}
				commit, ok := scene["backendCommit"].(map[string]any)
				if !ok || stringFromAny(commit["reason"]) != "backend committed" {
					t.Fatalf("backendCommit = %+v", scene["backendCommit"])
				}
				return
			}
			if err == nil {
				t.Fatalf("R08 proof passed, bundle=%+v", bundle)
			}
			if !containsString(bundle.MissingRequired, tt.wantError) {
				t.Fatalf("missing required = %+v, want %q", bundle.MissingRequired, tt.wantError)
			}
		})
	}
}

func r08ProofRoute() FixtureSpec {
	return FixtureSpec{
		ID:                  "R08",
		Route:               "/scene/basic",
		RoutePlanAssertions: []string{"Scene3D engine", "WebGPU remains selectable"},
	}
}

func r08BackendMockPage(setup string) string {
	return `<!doctype html><html><head><title>R08 mock</title></head><body>
<section data-route-id="R08" data-route-path="/scene/basic" data-marker="scene-basic" data-expected-capability="scene3d"></section>
<script id="gosx-manifest" type="application/json">{"engines":[{"id":"scene","kind":"surface","component":"GoSXScene3D","capabilities":["webgpu","webgl"],"props":{}}]}</script>
<div id="scene" data-gosx-scene3d="true" style="width:320px;height:180px"><canvas width="320" height="180"></canvas></div>
<script>
var orbit = {yaw:1, pitch:0.5, radius:8};
var camera = {position:{x:0,y:2,z:8}, rotation:{x:0.1,y:0.2,z:0.3}};
window.__gosx_scene3d_telemetry = function(){ return {orbit:orbit, camera:camera}; };
var dragging = false;
document.addEventListener("pointerdown", function(event){ if (event.target && event.target.tagName === "CANVAS") dragging = true; });
document.addEventListener("pointermove", function(){
	if (!dragging) return;
	orbit = {yaw:orbit.yaw+0.15, pitch:orbit.pitch+0.07, radius:orbit.radius};
	camera = {position:{x:camera.position.x+0.2,y:camera.position.y,z:camera.position.z-0.2}, rotation:{x:camera.rotation.x,y:camera.rotation.y+0.2,z:camera.rotation.z}};
});
document.addEventListener("pointerup", function(){ dragging = false; });
function commitSceneBackend(backend, truth) {
	var m = document.querySelector("[data-gosx-scene3d]");
	m.setAttribute("data-gosx-scene3d-backend", backend);
	m.setAttribute("data-gosx-scene3d-render-gpu", "true");
	m.setAttribute("data-gosx-scene3d-render-backend-truth", truth);
}
` + setup + `
</script>
</body></html>`
}

func TestRuntimeJSONStaticCompatibilityMatchRejectsMismatchedAuditHashes(t *testing.T) {
	static := testRuntimeJSONStaticIdentity()
	for _, tc := range []struct {
		name   string
		mutate func(*CompatibilityAuditIdentity)
		want   string
	}{
		{
			name: "semantic",
			mutate: func(a *CompatibilityAuditIdentity) {
				a.RuntimeJSONSemanticHash = "sha256:other"
			},
			want: "runtimeJSONSemanticHash",
		},
		{
			name: "counts",
			mutate: func(a *CompatibilityAuditIdentity) {
				a.RuntimeJSONCountsHash = "sha256:other"
			},
			want: "runtimeJSONCountsHash",
		},
		{
			name: "global",
			mutate: func(a *CompatibilityAuditIdentity) {
				a.RuntimeJSONGlobalNameHash = "sha256:other"
			},
			want: "runtimeJSONGlobalNameHash",
		},
		{
			name: "source",
			mutate: func(a *CompatibilityAuditIdentity) {
				a.RuntimeJSONSourceIdentityHash = "sha256:other"
			},
			want: "runtimeJSONSourceIdentityHash",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			audit := testCompatibilityAuditIdentity("pass", true, static)
			tc.mutate(audit)
			err := requireRuntimeJSONStaticCompatibilityMatch(*static, audit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("requireRuntimeJSONStaticCompatibilityMatch error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRouteStateMachineR05AcceptsOnlyDOMPickAndPixelsWithRuntimeProbe(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	injectR05RuntimeProbe(t, d)
	srv := newR05ProofMockServer(t, r05ProofMockPage(true, "draw", "listener"))
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/canvas-board"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	bundle, err := executeRouteStateMachine(d, r05ProofRoute(), ExternalRouteEvidence{}, SampleLaneProbe)
	if err != nil {
		t.Fatalf("R05 route state machine: %v bundle=%+v", err, bundle)
	}
	if bundle.FirstUsable.Name != "canvasboard-ready" || !bundle.FirstUsable.OK {
		t.Fatalf("first usable = %+v", bundle.FirstUsable)
	}
	pick := requireProofPayload(t, bundle, "canvasboard-dom-pick-accepted")
	if got := intFromAny(pick["callCount"]); got != 1 {
		t.Fatalf("callCount = %d payload=%+v", got, pick)
	}
	if got := intFromAny(pick["acceptedCallCount"]); got != 1 {
		t.Fatalf("acceptedCallCount = %d payload=%+v", got, pick)
	}
	if got := stringFromAny(pick["selectedID"]); got != "alpha" {
		t.Fatalf("selectedID = %q payload=%+v", got, pick)
	}
	call, ok := pick["observedCall"].(map[string]any)
	if !ok {
		t.Fatalf("missing observedCall: %+v", pick)
	}
	if stringFromAny(call["name"]) != "__gosx_canvas_event" {
		t.Fatalf("observed call name mismatch: %+v", call)
	}
	if kindEvidence, _ := call["kindEvidenceAvailable"].(bool); !kindEvidence {
		t.Fatalf("missing kind evidence: %+v", call)
	}
	if got := intFromAny(call["runtimeKind"]); got != 3 {
		t.Fatalf("observed runtime kind = %d call=%+v", got, call)
	}
}

func TestRouteStateMachineR05RejectsMissingDOMListenerEvenWhenDirectABIWorks(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	injectR05RuntimeProbe(t, d)
	srv := newR05ProofMockServer(t, r05ProofMockPage(true, "draw", ""))
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/canvas-board"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	bundle, err := executeRouteStateMachine(d, r05ProofRoute(), ExternalRouteEvidence{}, SampleLaneProbe)
	if err == nil {
		t.Fatalf("R05 proof passed without DOM listener: %+v", bundle)
	}
	if bundle.FirstUsable.OK {
		t.Fatalf("first usable passed without DOM listener: %+v", bundle.FirstUsable)
	}
	if !containsString(bundle.MissingRequired, "canvasboard-dom-pick-accepted") {
		t.Fatalf("missing required = %+v", bundle.MissingRequired)
	}
	if containsString(bundle.MissingRequired, "canvasboard-nonblank") {
		t.Fatalf("pixel proof unexpectedly failed: %+v", bundle.MissingRequired)
	}
	var directWorks bool
	if err := d.Evaluate(`(function(){
		var c = document.querySelector("#ouroboros-board");
		var id = c && c.getAttribute("data-gosx-surface-id");
		window.__gosx_canvas_event(id, 3, new Float64Array([284, 184, 640, 360]), "");
		return window.__gosx_get_shared_signal("$surface.event.selectedID") === "\"alpha\"";
	})()`, &directWorks); err != nil {
		t.Fatalf("direct ABI diagnostic: %v", err)
	}
	if !directWorks {
		t.Fatal("direct ABI diagnostic did not prove the mocked runtime can select alpha")
	}
}

func TestRouteStateMachineR05RejectsMissingRuntimeProbeEvenWhenDOMPickWorks(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	srv := newR05ProofMockServer(t, r05ProofMockPage(true, "draw", "listener"))
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/canvas-board"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	bundle, err := executeRouteStateMachine(d, r05ProofRoute(), ExternalRouteEvidence{}, SampleLaneProbe)
	if err == nil {
		t.Fatalf("R05 proof passed without runtime probe: %+v", bundle)
	}
	if !containsString(bundle.MissingRequired, "canvasboard-dom-pick-accepted") {
		t.Fatalf("missing required = %+v", bundle.MissingRequired)
	}
	pick := requireFailedProofPayload(t, bundle, "canvasboard-dom-pick-accepted")
	probe, ok := pick["probe"].(map[string]any)
	if !ok || probe["available"] == true {
		t.Fatalf("expected missing probe diagnostics: %+v", pick)
	}
}

func TestRouteStateMachineR05RejectsRuntimeCallWithoutKindEvidence(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	srv := newR05ProofMockServer(t, r05ProofMockPage(true, "draw", "manual-probe-missing-kind"))
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/canvas-board"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	bundle, err := executeRouteStateMachine(d, r05ProofRoute(), ExternalRouteEvidence{}, SampleLaneProbe)
	if err == nil {
		t.Fatalf("R05 proof passed without kind evidence: %+v", bundle)
	}
	pick := requireFailedProofPayload(t, bundle, "canvasboard-dom-pick-accepted")
	if got := intFromAny(pick["callCount"]); got != 1 {
		t.Fatalf("callCount = %d payload=%+v", got, pick)
	}
	if got := intFromAny(pick["acceptedCallCount"]); got != 0 {
		t.Fatalf("acceptedCallCount = %d payload=%+v", got, pick)
	}
}

func TestRouteStateMachineR05RejectsWrongRuntimeEventKind(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	srv := newR05ProofMockServer(t, r05ProofMockPage(true, "draw", "manual-probe-wrong-kind"))
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/canvas-board"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	bundle, err := executeRouteStateMachine(d, r05ProofRoute(), ExternalRouteEvidence{}, SampleLaneProbe)
	if err == nil {
		t.Fatalf("R05 proof passed with wrong runtime event kind: %+v", bundle)
	}
	pick := requireFailedProofPayload(t, bundle, "canvasboard-dom-pick-accepted")
	if got := intFromAny(pick["callCount"]); got != 1 {
		t.Fatalf("callCount = %d payload=%+v", got, pick)
	}
	if got := intFromAny(pick["acceptedCallCount"]); got != 0 {
		t.Fatalf("acceptedCallCount = %d payload=%+v", got, pick)
	}
	calls, ok := pick["calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("calls = %+v", pick["calls"])
	}
	call, ok := calls[0].(map[string]any)
	if !ok || intFromAny(call["runtimeKind"]) != 2 {
		t.Fatalf("wrong-kind call diagnostics = %+v", calls)
	}
}

func TestRouteStateMachineR05RejectsBadCanvasPixelProof(t *testing.T) {
	tests := []struct {
		name   string
		canvas bool
		pixel  string
	}{
		{name: "missing", canvas: false, pixel: "draw"},
		{name: "missing_2d_context", canvas: true, pixel: "missing-context"},
		{name: "unreadable", canvas: true, pixel: "throw"},
		{name: "blank", canvas: true, pixel: "blank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := requireOuroborosBrowserDriver(t, 45*time.Second)
			injectR05RuntimeProbe(t, d)
			srv := newR05ProofMockServer(t, r05ProofMockPage(tt.canvas, tt.pixel, "listener"))
			defer srv.Close()
			if err := d.Navigate(srv.URL + "/canvas-board"); err != nil {
				t.Fatalf("Navigate: %v", err)
			}
			if err := d.WaitReady(); err != nil {
				t.Fatalf("WaitReady: %v", err)
			}
			bundle, err := executeRouteStateMachine(d, r05ProofRoute(), ExternalRouteEvidence{}, SampleLaneProbe)
			if err == nil {
				t.Fatalf("R05 proof passed with bad canvas %q: %+v", tt.name, bundle)
			}
			if bundle.FirstUsable.OK {
				t.Fatalf("first usable passed with bad canvas %q: %+v", tt.name, bundle.FirstUsable)
			}
			if !containsString(bundle.MissingRequired, "canvasboard-nonblank") {
				t.Fatalf("missing required = %+v", bundle.MissingRequired)
			}
			nonblank := requireProofByName(t, bundle, "canvasboard-nonblank")
			if nonblank.OK {
				t.Fatalf("nonblank proof passed with bad canvas %q: %+v", tt.name, nonblank)
			}
			if len(nonblank.Payload) == 0 {
				t.Fatalf("nonblank proof lacks diagnostics: %+v", nonblank)
			}
		})
	}
}

func TestRuntimeJSONStaticIdentityRejectsTamperedCountsHash(t *testing.T) {
	identity := testRuntimeJSONStaticIdentity()
	identity.Counts.GosxReadCount--
	err := validateRuntimeJSONStaticIdentity(identity)
	if err == nil || !strings.Contains(err.Error(), "countsHash") {
		t.Fatalf("validateRuntimeJSONStaticIdentity error = %v", err)
	}
}

func TestRuntimeJSONStaticIdentityAllowsChangedCountsWithMatchingHash(t *testing.T) {
	identity := testRuntimeJSONStaticIdentity()
	identity.Counts.GosxReadCount++
	identity.Counts.UniqueGosxGlobals++
	identity.Counts.SerializationSiteCount++
	identity.CountsHash = RuntimeJSONStaticCountsHash(identity.Counts)
	if err := validateRuntimeJSONStaticIdentity(identity); err != nil {
		t.Fatalf("validateRuntimeJSONStaticIdentity rejected matching changed counts: %v", err)
	}
}

func TestRuntimeJSONStaticCorpusTamperChecksRemainFailClosed(t *testing.T) {
	site := RuntimeJSONStaticSite{
		Path:             "client/js/bootstrap-src/00-runtime.js",
		Line:             1,
		Column:           1,
		SourceFamily:     "browser-js",
		SourceKind:       "javascript",
		Operation:        "gosx-read",
		GlobalName:       "__gosx_hydrate",
		Phase:            "route-load",
		PhaseStatus:      "exact",
		PossiblePhases:   []string{"route-load"},
		HotPathPossible:  false,
		HotPathConfirmed: false,
		PhaseRule:        "R40",
		PhaseReason:      "test",
		TextHash:         "sha256:text",
		ContextHash:      "sha256:context",
	}
	base := runtimeJSONCorpusForSitesForTest(t, []RuntimeJSONStaticSite{site})
	if err := ValidateRuntimeJSONStaticCorpus(base); err != nil {
		t.Fatalf("base corpus invalid: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*RuntimeJSONStaticCorpus)
		want   string
	}{
		{
			name: "counts",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.Counts.GosxReadCount++
			},
			want: "gosxReadCount",
		},
		{
			name: "counts hash",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.CountsHash = "sha256:tampered"
			},
			want: "countsHash",
		},
		{
			name: "semantic hash",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.SemanticHash = "sha256:tampered"
			},
			want: "semanticHash",
		},
		{
			name: "source identity",
			mutate: func(c *RuntimeJSONStaticCorpus) {
				c.CurrentSourceIdentityHash = "sha256:tampered"
			},
			want: "current source identity hash",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			corpus := cloneRuntimeJSONCorpusForTest(t, base)
			tc.mutate(corpus)
			err := ValidateRuntimeJSONStaticCorpus(corpus)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateRuntimeJSONStaticCorpus error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSmokeRefusesExistingCanonicalManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"canonical":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      filepath.Join("..", ".."),
		ArtifactRoot:  root,
		Samples:       "smoke",
		Routes:        []string{"R00"},
		BaseURL:       "http://127.0.0.1:1",
		Trace:         false,
		Coverage:      false,
		HeapSnapshots: false,
	})
	if err == nil || !strings.Contains(err.Error(), "refuses to overwrite canonical") {
		t.Fatalf("RunBrowserBaseline error = %v", err)
	}
}

func TestCanonicalRequiresInventoryBeforeWriting(t *testing.T) {
	root := t.TempDir()
	_, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      filepath.Join("..", ".."),
		ArtifactRoot:  filepath.Join(root, "canonical"),
		Samples:       "baseline",
		Routes:        []string{"R00"},
		BaseURL:       "http://127.0.0.1:1",
		Trace:         false,
		Coverage:      false,
		HeapSnapshots: false,
	})
	if err == nil || !strings.Contains(err.Error(), "requires --inventory") {
		t.Fatalf("RunBrowserBaseline error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "canonical")); !os.IsNotExist(statErr) {
		t.Fatalf("canonical root was created: %v", statErr)
	}
}

func TestCanonicalRemotePreflightFailuresDoNotMutateArtifactRoot(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw := "ws://user:secret@127.0.0.1:1/devtools/browser/abc123?token=secret#frag"
	t.Setenv("CHROME_WS_URL", raw)
	oldArgs := os.Args
	os.Args = []string{"gosx", "perf", "ouroboros", "--chrome-ws-url=" + raw}
	defer func() { os.Args = oldArgs }()

	t.Run("missing_inventory", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "canonical")
		_, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
			RepoRoot:     repoRoot,
			CorpusPath:   filepath.Join(repoRoot, "examples", "ouroboros-corpus", "fixtures.v1.json"),
			ArtifactRoot: out,
			EvidenceRoot: filepath.Join(root, "evidence"),
			Samples:      "baseline",
		})
		if err == nil || !strings.Contains(err.Error(), "requires --inventory") {
			t.Fatalf("RunBrowserBaseline error = %v, want missing inventory", err)
		}
		assertNoRemoteEndpointLeak(t, "returned error", err.Error())
		assertArtifactRootAbsent(t, out)
	})

	t.Run("existing_root", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "canonical")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(out, "marker.txt")
		if err := os.WriteFile(marker, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
			RepoRoot:      repoRoot,
			CorpusPath:    filepath.Join(repoRoot, "examples", "ouroboros-corpus", "fixtures.v1.json"),
			ArtifactRoot:  out,
			EvidenceRoot:  filepath.Join(root, "evidence"),
			InventoryPath: filepath.Join(root, "missing-inventory.json"),
			Samples:       "baseline",
		})
		if err == nil || !strings.Contains(err.Error(), "no such file") {
			t.Fatalf("RunBrowserBaseline error = %v, want missing inventory", err)
		}
		assertNoRemoteEndpointLeak(t, "returned error", err.Error())
		body, readErr := os.ReadFile(marker)
		if readErr != nil || string(body) != "old" {
			t.Fatalf("existing root marker mutated: body=%q err=%v", body, readErr)
		}
		if _, statErr := os.Stat(filepath.Join(out, "failure.json")); !os.IsNotExist(statErr) {
			t.Fatalf("preflight wrote failure.json: %v", statErr)
		}
	})

	for _, tc := range []struct {
		name          string
		pixelManifest string
		writeEvidence func(t *testing.T, evidenceRoot string)
		want          string
	}{
		{
			name:          "same_evidence_root",
			pixelManifest: "",
			want:          "evidence-root",
		},
		{
			name:          "missing_pixel_manifest",
			pixelManifest: "a.json,b.json,c.json,d.json",
			want:          "no such file",
		},
		{
			name:          "tampered_pixel_manifest",
			pixelManifest: "a.json,b.json,c.json,d.json",
			writeEvidence: func(t *testing.T, evidenceRoot string) {
				for _, name := range []string{"a.json", "b.json", "c.json", "d.json"} {
					writeTestFile(t, filepath.Join(evidenceRoot, name), `{"schemaVersion":"bad","unknown":true}`)
				}
			},
			want: "no such file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			out := filepath.Join(root, "canonical")
			evidenceRoot := filepath.Join(root, "evidence")
			if tc.name == "same_evidence_root" {
				evidenceRoot = out
			}
			if tc.writeEvidence != nil {
				tc.writeEvidence(t, evidenceRoot)
			}
			_, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
				RepoRoot:      repoRoot,
				CorpusPath:    filepath.Join(repoRoot, "examples", "ouroboros-corpus", "fixtures.v1.json"),
				ArtifactRoot:  out,
				EvidenceRoot:  evidenceRoot,
				InventoryPath: filepath.Join(root, "missing-inventory.json"),
				PixelManifest: tc.pixelManifest,
				Samples:       "baseline",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("RunBrowserBaseline error = %v, want %q", err, tc.want)
			}
			assertNoRemoteEndpointLeak(t, "returned error", err.Error())
			assertArtifactRootAbsent(t, out)
		})
	}
}

func assertArtifactRootAbsent(t *testing.T, path string) {
	t.Helper()
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("artifact root was created or mutated before preflight finished: %v", statErr)
	}
}

func TestCanonicalBrowserPreflightAcceptsCleanSourceIdentityBeforePixelValidation(t *testing.T) {
	repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
	artifactRoot := filepath.Join(t.TempDir(), "browser-preflight-clean")
	plan, err := PredictCanonicalInventoryMaterialization(t.Context(), repoRoot, inventoryPath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := BuildSourceIdentityHandoff(t.Context(), repoRoot, inventoryPath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Source.TrackedDiffHash != OverlayClean || handoff.Source.UntrackedIncludedSourceHash != OverlayClean {
		t.Fatalf("clean handoff hashes = %#v", handoff.Source)
	}
	handoffPath := filepath.Join(t.TempDir(), "source-identity.json")
	if err := WriteNewJSONFile(handoffPath, handoff); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSourceIdentityHandoffStrict(handoffPath); err != nil {
		t.Fatalf("strict read rejected clean handoff: %v", err)
	}
	evidenceRoot := filepath.Join(t.TempDir(), "evidence")
	source := sourceIdentityFromMaterialization(plan)
	if _, err := MaterializeCanonicalInventory(t.Context(), repoRoot, inventoryPath, artifactRoot); err != nil {
		t.Fatal(err)
	}
	addCanonicalBrowserPreseedGitExcludes(t, repoRoot)
	routes := canonicalBrowserRoutesForTest(t)
	writeCanonicalBrowserRuntimePreseedForTest(t, repoRoot, artifactRoot, source)
	writeCanonicalBrowserSizePreseedForTest(t, repoRoot, artifactRoot, source, routes)
	pixelSource := visual.PixelSourceIdentity{BaseRevision: source.BaseRevision, OverlayHash: source.OverlayHash, InventorySHA256: source.InventorySHA256}
	var pixelRefs []string
	for _, routeID := range []string{"R08", "R10"} {
		for _, backend := range []string{"webgpu", "webgl"} {
			pixelRefs = append(pixelRefs, writeStrictCanonicalPixelManifestRef(t, evidenceRoot, routeID, backend, pixelSource))
		}
	}
	_, err = preflightCanonicalBrowserInputs(t.Context(), BrowserBaselineOptions{
		RepoRoot:           repoRoot,
		ArtifactRoot:       artifactRoot,
		EvidenceRoot:       evidenceRoot,
		InventoryPath:      inventoryPath,
		SourceIdentityPath: handoffPath,
		PixelManifest:      strings.Join(pixelRefs, ","),
		ViewportWidth:      1440,
		ViewportHeight:     900,
		DPR:                1,
	}, routes)
	if err == nil || !strings.Contains(err.Error(), "invalid O0.2 pixel baseline") {
		t.Fatalf("preflight error = %v, want pixel validation after clean handoff acceptance", err)
	}
	if strings.Contains(err.Error(), "source identity handoff") || strings.Contains(err.Error(), "source mismatch") {
		t.Fatalf("preflight rejected clean source identity handoff before pixel validation: %v", err)
	}
	for _, rel := range []string{"manifest.json", "commands.log", "environment.json", "failure.json"} {
		if _, statErr := os.Stat(filepath.Join(artifactRoot, rel)); !os.IsNotExist(statErr) {
			t.Fatalf("preflight wrote browser artifact %s: %v", rel, statErr)
		}
	}
}

func TestCanonicalBrowserSourceIdentityHandoffMismatchBeforeRootMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*SourceIdentityHandoff)
		want   string
	}{
		{
			name: "artifact_root",
			mutate: func(h *SourceIdentityHandoff) {
				h.ArtifactRoot = filepath.Join(filepath.Dir(h.ArtifactRoot), "wrong-root")
			},
			want: "artifactRoot mismatch",
		},
		{
			name: "inventory_hash",
			mutate: func(h *SourceIdentityHandoff) {
				h.Source.InventorySHA256 = "sha256:" + strings.Repeat("f", 64)
			},
			want: "source mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
			artifactRoot := filepath.Join(repoRoot, "build", "browser-preflight-mismatch")
			plan, err := PredictCanonicalInventoryMaterialization(t.Context(), repoRoot, inventoryPath, artifactRoot)
			if err != nil {
				t.Fatal(err)
			}
			handoff, err := BuildSourceIdentityHandoff(t.Context(), repoRoot, inventoryPath, artifactRoot)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(handoff)
			handoffPath := filepath.Join(t.TempDir(), "source-identity.json")
			if err := WriteNewJSONFile(handoffPath, handoff); err != nil {
				t.Fatal(err)
			}
			evidenceRoot := filepath.Join(t.TempDir(), "evidence")
			source := sourceIdentityFromMaterialization(plan)
			pixelSource := visual.PixelSourceIdentity{BaseRevision: source.BaseRevision, OverlayHash: source.OverlayHash, InventorySHA256: source.InventorySHA256}
			var pixelRefs []string
			for _, routeID := range []string{"R08", "R10"} {
				for _, backend := range []string{"webgpu", "webgl"} {
					pixelRefs = append(pixelRefs, writeStrictCanonicalPixelManifestRef(t, evidenceRoot, routeID, backend, pixelSource))
				}
			}
			_, err = RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
				RepoRoot:           repoRoot,
				CorpusPath:         filepath.Join("..", "..", "examples", "ouroboros-corpus", "fixtures.v1.json"),
				ArtifactRoot:       artifactRoot,
				EvidenceRoot:       evidenceRoot,
				InventoryPath:      inventoryPath,
				SourceIdentityPath: handoffPath,
				PixelManifest:      strings.Join(pixelRefs, ","),
				Samples:            "baseline",
				ViewportWidth:      1440,
				ViewportHeight:     900,
				DPR:                1,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("RunBrowserBaseline error = %v, want %q", err, tc.want)
			}
			assertArtifactRootAbsent(t, artifactRoot)
		})
	}
}

func TestCanonicalBrowserPreseedRootValidation(t *testing.T) {
	valid := func(t *testing.T) (BrowserBaselineOptions, CanonicalInventoryMaterialization, SourceIdentity, []FixtureSpec) {
		t.Helper()
		repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
		artifactRoot := filepath.Join(t.TempDir(), "browser-preseed")
		routes := canonicalBrowserRoutesForTest(t)
		plan, err := PredictCanonicalInventoryMaterialization(t.Context(), repoRoot, inventoryPath, artifactRoot)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := MaterializeCanonicalInventory(t.Context(), repoRoot, inventoryPath, artifactRoot); err != nil {
			t.Fatal(err)
		}
		addCanonicalBrowserPreseedGitExcludes(t, repoRoot)
		source := sourceIdentityFromMaterialization(plan)
		writeCanonicalBrowserRuntimePreseedForTest(t, repoRoot, artifactRoot, source)
		writeCanonicalBrowserSizePreseedForTest(t, repoRoot, artifactRoot, source, routes)
		opts := BrowserBaselineOptions{
			RepoRoot:      repoRoot,
			ArtifactRoot:  artifactRoot,
			InventoryPath: inventoryPath,
			EvidenceRoot:  filepath.Join(t.TempDir(), "evidence"),
		}
		return opts, plan, source, routes
	}
	validWithRuntimeOutputPreseed := func(t *testing.T) (BrowserBaselineOptions, CanonicalInventoryMaterialization, SourceIdentity, []FixtureSpec) {
		t.Helper()
		repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
		artifactRoot := filepath.Join(repoRoot, "build", "browser-preseed")
		routes := canonicalBrowserRoutesForTest(t)
		plan, err := PredictCanonicalInventoryMaterialization(t.Context(), repoRoot, inventoryPath, artifactRoot)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := MaterializeCanonicalInventory(t.Context(), repoRoot, inventoryPath, artifactRoot); err != nil {
			t.Fatal(err)
		}
		addCanonicalBrowserPreseedGitExcludes(t, repoRoot)
		source := sourceIdentityFromMaterialization(plan)
		writeCanonicalBrowserRuntimePreseedForTestWithOutputDir(t, repoRoot, artifactRoot, source, filepath.Join(artifactRoot, "wasm", "runtime-output"))
		writeCanonicalBrowserSizePreseedForTest(t, repoRoot, artifactRoot, source, routes)
		opts := BrowserBaselineOptions{
			RepoRoot:      repoRoot,
			ArtifactRoot:  artifactRoot,
			InventoryPath: inventoryPath,
			EvidenceRoot:  filepath.Join(t.TempDir(), "evidence"),
		}
		return opts, plan, source, routes
	}
	bindRuntimeJSONStaticPreseed := func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) SourceIdentity {
		t.Helper()
		ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
		if err != nil {
			t.Fatal(err)
		}
		site := RuntimeJSONStaticSite{
			Path:           "client/js/bootstrap-src/00-runtime.js",
			Line:           1,
			Column:         1,
			SourceFamily:   "browser-js",
			SourceKind:     "javascript",
			Operation:      "gosx-read",
			GlobalName:     "__gosx_hydrate",
			Phase:          "route-load",
			PhaseStatus:    "exact",
			PossiblePhases: []string{"route-load"},
			PhaseRule:      "R40",
			PhaseReason:    "test",
			TextHash:       "sha256:text",
			ContextHash:    "sha256:context",
		}
		corpus := runtimeJSONCorpusForSitesForTest(t, []RuntimeJSONStaticSite{site})
		corpus.Source = source
		corpus.CurrentSourceIdentity = source
		corpus.CurrentSourceIdentityHash = RuntimeJSONStaticCanonicalSourceIdentityHash(source)
		corpus.Query.ID = "gosx.ouroboros.o02.runtime-json-static.ast.v2"
		corpus.Query.PhaseClassifier = runtimeJSONPhaseClassifierVersion
		corpus.SemanticHash = RuntimeJSONStaticCorpusSemanticHash(corpus)
		if err := ValidateRuntimeJSONStaticCorpus(corpus); err != nil {
			t.Fatal(err)
		}
		static := &RuntimeJSONStaticIdentity{
			Ref:                "perf/runtime-json-static.jsonl",
			SchemaVersion:      corpus.SchemaVersion,
			ScannerVersion:     corpus.ScannerVersion,
			QueryID:            corpus.Query.ID,
			PhaseClassifier:    corpus.Query.PhaseClassifier,
			SourceIdentityHash: corpus.CurrentSourceIdentityHash,
			SemanticHash:       corpus.SemanticHash,
			CountsHash:         corpus.CountsHash,
			GlobalNameHash:     corpus.GlobalNames.Hash,
			Validated:          true,
			Counts:             corpus.Counts,
		}
		ev.Source.RuntimeJSONStatic = static
		writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
		runtimeEv, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
		if err != nil {
			t.Fatal(err)
		}
		runtimeEv.Source.RuntimeJSONStatic = static
		writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), runtimeEv)
		if err := WriteRuntimeJSONStaticCorpusJSONL(filepath.Join(opts.ArtifactRoot, "perf", "runtime-json-static.jsonl"), corpus); err != nil {
			t.Fatal(err)
		}
		source.RuntimeJSONStatic = static
		return source
	}
	writeRuntimeVariantSidecars := func(t *testing.T, opts BrowserBaselineOptions) RuntimeBuildEvidence {
		t.Helper()
		ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, variant := range ev.Variants {
			if variant.Generation != "current" || variant.Status != "measured" {
				continue
			}
			raw, err := os.ReadFile(variant.SourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if gzipBody := GzipBytes(raw); len(gzipBody) < len(raw) {
				if err := os.WriteFile(variant.SourcePath+".gz", gzipBody, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(variant.SourcePath + ".gz"); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if brotliBody := BrotliBytes(raw); len(brotliBody) < len(raw) {
				if err := os.WriteFile(variant.SourcePath+".br", brotliBody, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(variant.SourcePath + ".br"); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		return *ev
	}

	t.Run("valid_exact_preseed", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		sizeEvidence, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(filepath.ToSlash(sizeEvidence.ExportPath), "canonical-export/dist/export.json") {
			t.Fatalf("size exportPath = %s, want canonical-export/dist sibling", sizeEvidence.ExportPath)
		}
		tempBefore := gosxOuroborosTempDirsForTest(t)
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err != nil || !ok {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want valid preseed", ok, err)
		}
		tempAfter := gosxOuroborosTempDirsForTest(t)
		if !sameStringSlice(tempBefore, tempAfter) {
			t.Fatalf("preseed replay leaked temp dirs: before=%v after=%v", tempBefore, tempAfter)
		}
		for _, rel := range []string{"manifest.json", "commands.log", "environment.json", "failure.json", "perf/raw-samples.jsonl", "summaries/browser-summary.json"} {
			if _, statErr := os.Stat(filepath.Join(opts.ArtifactRoot, rel)); !os.IsNotExist(statErr) {
				t.Fatalf("preseed validation wrote browser artifact %s: %v", rel, statErr)
			}
		}
	})

	t.Run("valid_exact_runtime_json_static_preseed", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		allowedExact, allowedDirs, err := canonicalBrowserAllowedPreseedPaths(opts.ArtifactRoot, plan)
		if err != nil {
			t.Fatal(err)
		}
		if !allowedExact["perf/runtime-json-static.jsonl"] || !allowedDirs["perf"] {
			t.Fatalf("runtime JSON static preseed missing from allowlist: exact=%v dirs=%v", allowedExact, allowedDirs)
		}
		if allowedExact["perf/raw-samples.jsonl"] || allowedExact["perf/unknown.jsonl"] {
			t.Fatalf("canonical preseed allowlist broadly allowed perf files: %v", allowedExact)
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err != nil || !ok {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want static JSONL accepted", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_arbitrary_perf_file", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		writeTestFile(t, filepath.Join(opts.ArtifactRoot, "perf", "unknown.jsonl"), "bad")
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "browser-owned") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want arbitrary perf rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_raw_samples", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		writeTestFile(t, filepath.Join(opts.ArtifactRoot, "perf", "raw-samples.jsonl"), "bad")
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "browser-owned") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want raw samples rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_missing_file", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		if err := os.Remove(filepath.Join(opts.ArtifactRoot, "perf", "runtime-json-static.jsonl")); err != nil {
			t.Fatal(err)
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "perf/runtime-json-static.jsonl") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want missing static JSONL rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_malformed_file", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		writeTestFile(t, filepath.Join(opts.ArtifactRoot, "perf", "runtime-json-static.jsonl"), "{}\n")
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "missing value") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want malformed static JSONL rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_source_hash_mismatch", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
		if err != nil {
			t.Fatal(err)
		}
		ev.Source.RuntimeJSONStatic.SourceIdentityHash = "sha256:" + strings.Repeat("f", 64)
		writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "sourceIdentityHash") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want source hash mismatch rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_runtime_size_identity_divergence", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
		if err != nil {
			t.Fatal(err)
		}
		diverged := *source.RuntimeJSONStatic
		diverged.SemanticHash = "sha256:" + strings.Repeat("e", 64)
		ev.Source.RuntimeJSONStatic = &diverged
		writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "differs from size evidence") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want runtime/size static identity divergence rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_missing_runtime_identity", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
		if err != nil {
			t.Fatal(err)
		}
		ev.Source.RuntimeJSONStatic = nil
		writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "missing static identity") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want missing runtime static identity rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_file_symlink", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		target := filepath.Join(opts.ArtifactRoot, "perf", "runtime-json-static.jsonl")
		outside := filepath.Join(t.TempDir(), "runtime-json-static.jsonl")
		body, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outside, body, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want file symlink rejection", ok, err)
		}
	})

	t.Run("runtime_json_static_preseed_rejects_parent_symlink", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		source = bindRuntimeJSONStaticPreseed(t, opts, source)
		perfDir := filepath.Join(opts.ArtifactRoot, "perf")
		outside := filepath.Join(t.TempDir(), "perf")
		if err := os.Rename(perfDir, outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, perfDir); err != nil {
			t.Fatal(err)
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "parent directory must be real") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want parent symlink rejection", ok, err)
		}
	})

	t.Run("browser_owned_artifacts_still_rejected", func(t *testing.T) {
		for _, rel := range []string{
			"manifest.json",
			"commands.log",
			"environment.json",
			"failure.json",
			"summaries/browser-summary.json",
			"traces/R00.trace.zip",
			"coverage/R00.json",
			"heaps/R00.heapsnapshot",
			"dynamic/runtime-json-dynamic.json",
		} {
			t.Run(strings.ReplaceAll(rel, "/", "_"), func(t *testing.T) {
				opts, plan, source, routes := valid(t)
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, filepath.FromSlash(rel)), "bad")
				ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
				if err == nil || !strings.Contains(err.Error(), "browser-owned") {
					t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want browser-owned rejection for %s", ok, err, rel)
				}
			})
		}
	})

	t.Run("valid_runtime_output_published_shim", func(t *testing.T) {
		opts, plan, source, routes := validWithRuntimeOutputPreseed(t)
		allowedExact, _, err := canonicalBrowserAllowedPreseedPaths(opts.ArtifactRoot, plan)
		if err != nil {
			t.Fatal(err)
		}
		if !allowedExact["wasm/runtime-output/wasm_exec.js"] {
			t.Fatal("published TinyGo shim missing from exact canonical preseed allowlist")
		}
		if allowedExact["wasm/runtime-output/unknown.js"] {
			t.Fatal("canonical preseed allowlist broadly allowed runtime-output files")
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err != nil || !ok {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want published shim accepted", ok, err)
		}
	})

	t.Run("valid_runtime_output_compressed_sidecars", func(t *testing.T) {
		opts, plan, source, routes := validWithRuntimeOutputPreseed(t)
		ev := writeRuntimeVariantSidecars(t, opts)
		allowedExact, _, err := canonicalBrowserAllowedPreseedPaths(opts.ArtifactRoot, plan)
		if err != nil {
			t.Fatal(err)
		}
		for _, variant := range ev.Variants {
			if variant.Generation != "current" || variant.Status != "measured" {
				continue
			}
			baseRel, err := canonicalBrowserPreseedFileRel(opts.ArtifactRoot, variant.SourcePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, ext := range []string{".gz", ".br"} {
				raw, err := os.ReadFile(variant.SourcePath)
				if err != nil {
					t.Fatal(err)
				}
				wantAllowed := (ext == ".gz" && len(GzipBytes(raw)) < len(raw)) || (ext == ".br" && len(BrotliBytes(raw)) < len(raw))
				if wantAllowed && !allowedExact[baseRel+ext] {
					t.Fatalf("runtime sidecar %s missing from exact allowlist", baseRel+ext)
				}
				if !wantAllowed && allowedExact[baseRel+ext] {
					t.Fatalf("runtime sidecar %s allowed even though compression is not smaller", baseRel+ext)
				}
			}
		}
		if allowedExact["wasm/runtime-output/unknown.wasm.br"] {
			t.Fatal("canonical preseed allowlist broadly allowed runtime sidecars")
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err != nil || !ok {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want runtime sidecars accepted", ok, err)
		}
	})

	t.Run("runtime_output_parent_symlink_rejected_before_sidecar_allowlist", func(t *testing.T) {
		opts, plan, _, _ := validWithRuntimeOutputPreseed(t)
		writeRuntimeVariantSidecars(t, opts)
		outputDir := filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output")
		outside := filepath.Join(t.TempDir(), "runtime-output")
		if err := os.Rename(outputDir, outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, outputDir); err != nil {
			t.Fatal(err)
		}
		_, _, err := canonicalBrowserAllowedPreseedPaths(opts.ArtifactRoot, plan)
		if err == nil || !strings.Contains(err.Error(), "parent directory must be real") {
			t.Fatalf("canonicalBrowserAllowedPreseedPaths error = %v, want parent symlink rejection", err)
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence)
		want   string
	}{
		{
			name: "runtime_output_sidecar_unknown_file",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "unknown.wasm.br"), "bad")
			},
			want: "unknown file",
		},
		{
			name: "runtime_output_sidecar_unknown_directory",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "nested", "gosx-runtime.wasm.br"), "bad")
			},
			want: "unknown directory",
		},
		{
			name: "runtime_output_sidecar_missing_required_brotli",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				if err := os.Remove(ev.Variants[0].SourcePath + ".br"); err != nil {
					t.Fatal(err)
				}
			},
			want: "is required",
		},
		{
			name: "runtime_output_sidecar_tampered_brotli",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				writeTestFile(t, ev.Variants[0].SourcePath+".br", "bad")
			},
			want: "compression mismatch",
		},
		{
			name: "runtime_output_sidecar_present_when_not_smaller",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				variant := ev.Variants[0]
				raw := []byte{1}
				if err := os.WriteFile(variant.SourcePath, raw, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(variant.SourcePath + ".gz"); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err := os.Remove(variant.SourcePath + ".br"); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				writeTestFile(t, variant.SourcePath+".br", "bad")
				runtimeEv, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				metrics, err := MetricsForFile(variant.SourcePath)
				if err != nil {
					t.Fatal(err)
				}
				for i := range runtimeEv.Variants {
					if runtimeEv.Variants[i].ID != variant.ID {
						continue
					}
					sizeBytes := metrics.Bytes
					runtimeEv.Variants[i].SizeBytes = &sizeBytes
					runtimeEv.Variants[i].Bytes = metrics.Bytes
					runtimeEv.Variants[i].GzipBytes = metrics.GzipBytes
					runtimeEv.Variants[i].BrotliBytes = metrics.BrotliBytes
					runtimeEv.Variants[i].SHA256 = metrics.SHA256
				}
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), runtimeEv)
			},
			want: "must be absent",
		},
		{
			name: "runtime_output_raw_wasm_symlink_before_sidecar_read",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				target := ev.Variants[0].SourcePath
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), filepath.Base(target))
				writeTestFile(t, outside, "raw")
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "regular file",
		},
		{
			name: "runtime_output_sidecar_symlink_gzip",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, ev RuntimeBuildEvidence) {
				target := ev.Variants[0].SourcePath + ".gz"
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), filepath.Base(target))
				writeTestFile(t, outside, "bad")
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "regular file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, plan, source, routes := validWithRuntimeOutputPreseed(t)
			ev := writeRuntimeVariantSidecars(t, opts)
			tc.mutate(t, opts, ev)
			ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
			if err == nil || !strings.Contains(err.Error(), tc.want) || ok {
				t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want %q", ok, err, tc.want)
			}
		})
	}

	t.Run("copied_runtime_output_shim_rejected_when_evidence_output_dir_is_outside_preseed", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		writeTestFile(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "wasm_exec.js"), "shim")
		allowedExact, _, err := canonicalBrowserAllowedPreseedPaths(opts.ArtifactRoot, plan)
		if err != nil {
			t.Fatal(err)
		}
		if allowedExact["wasm/runtime-output/wasm_exec.js"] {
			t.Fatal("canonical preseed allowlist admitted copied shim without matching runtime OutputDir")
		}
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err == nil || !strings.Contains(err.Error(), "unknown directory") {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want copied shim rejection", ok, err)
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, opts BrowserBaselineOptions)
		want   string
	}{
		{
			name: "runtime_output_unknown_file",
			mutate: func(t *testing.T, opts BrowserBaselineOptions) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "unknown.js"), "bad")
			},
			want: "unknown file",
		},
		{
			name: "runtime_output_unknown_directory",
			mutate: func(t *testing.T, opts BrowserBaselineOptions) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "nested", "unknown.js"), "bad")
			},
			want: "unknown directory",
		},
		{
			name: "runtime_output_missing_published_shim",
			mutate: func(t *testing.T, opts BrowserBaselineOptions) {
				if err := os.Remove(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "wasm_exec.js")); err != nil {
					t.Fatal(err)
				}
			},
			want: "published shim",
		},
		{
			name: "runtime_output_tampered_published_shim",
			mutate: func(t *testing.T, opts BrowserBaselineOptions) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "wasm_exec.js"), "changed")
			},
			want: "published shim metrics mismatch",
		},
		{
			name: "runtime_output_symlink_published_shim",
			mutate: func(t *testing.T, opts BrowserBaselineOptions) {
				target := filepath.Join(opts.ArtifactRoot, "wasm", "runtime-output", "wasm_exec.js")
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "wasm_exec.js")
				writeTestFile(t, outside, "shim")
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, plan, source, routes := validWithRuntimeOutputPreseed(t)
			tc.mutate(t, opts)
			ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want %q", ok, err, tc.want)
			}
		})
	}

	t.Run("recorded_symlink_alias_to_live_tool", func(t *testing.T) {
		opts, plan, source, routes := valid(t)
		ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
		if err != nil {
			t.Fatal(err)
		}
		aliasDir := t.TempDir()
		alias := filepath.Join(aliasDir, "go")
		liveGo, err := exec.LookPath("go")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(liveGo, alias); err != nil {
			t.Fatal(err)
		}
		ev.GoVersion.Path = alias
		writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
		ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
		if err != nil || !ok {
			t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want symlink alias accepted", ok, err)
		}
	})

	t.Run("final_revalidation_rejects_symlink_swap", func(t *testing.T) {
		for _, rel := range []string{"source/source-inventory.json", "wasm/runtime-artifacts.json", "size/route-assets.json"} {
			t.Run(strings.ReplaceAll(rel, "/", "_"), func(t *testing.T) {
				opts, plan, source, routes := valid(t)
				if ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes); err != nil || !ok {
					t.Fatalf("initial validateCanonicalBrowserRootPreseed = %v/%v", ok, err)
				}
				target := filepath.Join(opts.ArtifactRoot, filepath.FromSlash(rel))
				outside := filepath.Join(t.TempDir(), filepath.Base(rel))
				body, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
				err = revalidateCanonicalBrowserPreseedArtifacts(t.Context(), opts, opts.InventoryPath, routes, source)
				if err == nil || !strings.Contains(err.Error(), "regular file") {
					t.Fatalf("revalidateCanonicalBrowserPreseedArtifacts error = %v, want regular file rejection", err)
				}
			})
		}
	})

	t.Run("final_revalidation_rejects_runtime_json_static_post_preflight_swap", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(t *testing.T, path string)
			want   string
		}{
			{
				name: "tamper",
				mutate: func(t *testing.T, path string) {
					writeTestFile(t, path, "{}\n")
				},
				want: "missing value",
			},
			{
				name: "symlink",
				mutate: func(t *testing.T, path string) {
					outside := filepath.Join(t.TempDir(), "runtime-json-static.jsonl")
					body, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(outside, body, 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal(err)
					}
				},
				want: "regular file",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				opts, plan, source, routes := valid(t)
				source = bindRuntimeJSONStaticPreseed(t, opts, source)
				if ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes); err != nil || !ok {
					t.Fatalf("initial validateCanonicalBrowserRootPreseed = %v/%v", ok, err)
				}
				tc.mutate(t, filepath.Join(opts.ArtifactRoot, "perf", "runtime-json-static.jsonl"))
				err := revalidateCanonicalBrowserPreseedArtifacts(t.Context(), opts, opts.InventoryPath, routes, source)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("revalidateCanonicalBrowserPreseedArtifacts error = %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("final_revalidation_rejects_runtime_sidecar_post_preflight_tamper", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(t *testing.T, path string)
			want   string
		}{
			{
				name: "missing",
				mutate: func(t *testing.T, path string) {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				},
				want: "is required",
			},
			{
				name: "tamper",
				mutate: func(t *testing.T, path string) {
					writeTestFile(t, path, "bad")
				},
				want: "compression mismatch",
			},
			{
				name: "symlink",
				mutate: func(t *testing.T, path string) {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					outside := filepath.Join(t.TempDir(), filepath.Base(path))
					writeTestFile(t, outside, "bad")
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal(err)
					}
				},
				want: "regular file",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				opts, plan, source, routes := validWithRuntimeOutputPreseed(t)
				ev := writeRuntimeVariantSidecars(t, opts)
				if ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes); err != nil || !ok {
					t.Fatalf("initial validateCanonicalBrowserRootPreseed = %v/%v", ok, err)
				}
				tc.mutate(t, ev.Variants[0].SourcePath+".br")
				err := revalidateCanonicalBrowserPreseedArtifacts(t.Context(), opts, opts.InventoryPath, routes, source)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("revalidateCanonicalBrowserPreseedArtifacts error = %v, want %q", err, tc.want)
				}
			})
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity)
		want   string
	}{
		{
			name: "unknown_file",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "source", "unexpected.txt"), "bad")
			},
			want: "unknown file",
		},
		{
			name: "browser_owned_file",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				if err := os.MkdirAll(filepath.Join(opts.ArtifactRoot, "dynamic"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "browser-owned",
		},
		{
			name: "symlink_escape",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(opts.ArtifactRoot, "source", "escape")); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
		{
			name: "runtime_evidence_symlink_rejected_before_allowlist_decode",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				target := filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json")
				outside := filepath.Join(t.TempDir(), "runtime-artifacts.json")
				body, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
		{
			name: "size_evidence_symlink_rejected_before_allowlist_decode",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				target := filepath.Join(opts.ArtifactRoot, "size", "route-assets.json")
				outside := filepath.Join(t.TempDir(), "route-assets.json")
				body, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
		{
			name: "tampered_source",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				writeTestFile(t, filepath.Join(opts.ArtifactRoot, "source", "source-inventory.json"), "{}")
			},
			want: "source inventory",
		},
		{
			name: "pretty_source_inventory_drift",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				path := filepath.Join(opts.ArtifactRoot, "source", "source-inventory.json")
				f, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				inv, err := DecodeInventoryStrict(f)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
				writeFixtureJSON(t, path, inv)
			},
			want: "differs from predicted materialization",
		},
		{
			name: "invalid_runtime_rows",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants = ev.Variants[:1]
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "runtime evidence invalid",
		},
		{
			name: "invalid_size_routes",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Routes = ev.Routes[:len(ev.Routes)-1]
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "size evidence invalid",
		},
		{
			name: "runtime_schema_contract",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.SchemaVersion = "bad"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "runtime schemaVersion",
		},
		{
			name: "size_schema_contract",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Contract = "bad"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "size contractVersion",
		},
		{
			name: "size_route_path",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Routes[0].Route = "/wrong"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "routes mismatch",
		},
		{
			name: "runtime_source_mismatch",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Source.InventorySHA256 = "sha256:" + strings.Repeat("f", 64)
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "runtime source mismatch",
		},
		{
			name: "runtime_shim_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].Shim.Bytes++
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "shim metrics mismatch",
		},
		{
			name: "runtime_build_input_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.BuildInput.GoSXModuleDir = filepath.Join(opts.RepoRoot, "wrong")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "runtime build input mismatch",
		},
		{
			name: "runtime_compressed_metric_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].GzipBytes++
				ev.Variants[1].BrotliBytes++
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "metrics mismatch",
		},
		{
			name: "runtime_tool_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].TinyGoVersion = "wrong"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "tool version mismatch",
		},
		{
			name: "runtime_tool_unavailable",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.TinyGo.Available = false
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "tinygo tool is unavailable",
		},
		{
			name: "runtime_tool_path_forged",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.GoVersion.Path = filepath.Join(opts.RepoRoot, "build", "tool-bin", "missing-go")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "go tool path",
		},
		{
			name: "runtime_malicious_recorded_executable",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				malicious := filepath.Join(t.TempDir(), "go")
				writeExecutableTestFile(t, malicious, "#!/bin/sh\nif [ \"$1\" = version ]; then echo 'go test'; exit 0; fi\nif [ \"$1\" = env ] && [ \"$2\" = GOROOT ]; then echo '"+filepath.ToSlash(filepath.Join(opts.RepoRoot, "build", "goroot"))+"'; exit 0; fi\nif [ \"$1\" = env ] && [ \"$2\" = GOWORK ]; then echo off; exit 0; fi\nexit 1\n")
				ev.GoVersion.Path = malicious
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "recorded tool path does not match live tool",
		},
		{
			name: "runtime_artifact_root_recorded_tool",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.GoVersion.Path = filepath.Join(opts.ArtifactRoot, "source", "source-inventory.json")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "artifact root",
		},
		{
			name: "runtime_evidence_root_recorded_tool",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				tool := filepath.Join(opts.EvidenceRoot, "go")
				writeExecutableTestFile(t, tool, "#!/bin/sh\necho 'go test'\n")
				ev.GoVersion.Path = tool
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "evidence root",
		},
		{
			name: "runtime_tool_version_forged",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.GoVersion.Version = "wrong"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "go tool status mismatch",
		},
		{
			name: "runtime_tool_root_forged",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.TinyGo.TinyGoRoot = filepath.Join(opts.RepoRoot, "wrong")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "tinygo tool status mismatch",
		},
		{
			name: "runtime_tool_env_forged",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.GoVersion.Environment["GOWORK"] = "forged"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "go tool status mismatch",
		},
		{
			name: "runtime_build_tags_reorder",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[1].BuildTags = []string{"gosx_tiny_islands_only", "tinygo", "gosx_tiny_runtime"}
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "build tags mismatch",
		},
		{
			name: "runtime_build_tags_extra",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].BuildTags = append(ev.Variants[0].BuildTags, "extra")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "build tags mismatch",
		},
		{
			name: "runtime_build_tags_missing",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].BuildTags = []string{"tinygo"}
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "build tags mismatch",
		},
		{
			name: "runtime_build_args_module",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].BuildArgs[len(ev.Variants[0].BuildArgs)-1] = "example.invalid/module"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "build args mismatch",
		},
		{
			name: "runtime_build_args_flag",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].BuildArgs[1] = "-bad"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "build args mismatch",
		},
		{
			name: "runtime_published_shim_removed",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				if err := os.Remove(filepath.Join(opts.RepoRoot, "build", "run", "tinygo", "current", "wasm_exec.js")); err != nil {
					t.Fatal(err)
				}
			},
			want: "published shim",
		},
		{
			name: "runtime_published_shim_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				writeTestFile(t, filepath.Join(opts.RepoRoot, "build", "run", "tinygo", "current", "wasm_exec.js"), "changed")
			},
			want: "published shim metrics mismatch",
		},
		{
			name: "runtime_published_shim_parent_symlink",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				outputDir := filepath.Join(opts.RepoRoot, "build", "run", "tinygo", "current")
				if err := os.Remove(filepath.Join(outputDir, "wasm_exec.js")); err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				writeTestFile(t, filepath.Join(outside, "wasm_exec.js"), "shim")
				link := filepath.Join(outputDir, "shim-link")
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join("shim-link", "wasm_exec.js"), filepath.Join(outputDir, "wasm_exec.js")); err != nil {
					t.Fatal(err)
				}
			},
			want: "published shim",
		},
		{
			name: "runtime_parent_symlink_escape",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				outside := t.TempDir()
				writeTestFile(t, filepath.Join(outside, "gosx-runtime.wasm"), string(bytes.Repeat([]byte{1}, 1024)))
				link := filepath.Join(opts.RepoRoot, "build", "runtime-link")
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.OutputDir = link
				ev.Variants[0].SourcePath = filepath.Join(link, "gosx-runtime.wasm")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "runtime outputDir",
		},
		{
			name: "runtime_selected_routes_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[0].PlannedSelectedBy = []string{"R05"}
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "selected routes mismatch",
		},
		{
			name: "runtime_future_routes_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[2].PlannedSelectedBy = []string{"R01"}
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "future runtime variant core selected routes mismatch",
		},
		{
			name: "runtime_future_measured_metadata",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadRuntimeBuildEvidenceStrict(filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Variants[2].File = "forged.wasm"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "wasm", "runtime-artifacts.json"), ev)
			},
			want: "must not pretend to have measured binaries",
		},
		{
			name: "size_build_input_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.BuildInput.ManifestSHA256 = "sha256:" + strings.Repeat("f", 64)
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "build input mismatch",
		},
		{
			name: "size_resource_manifest_hash_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.BuildInput.ResourceManifestSHA256 = "sha256:" + strings.Repeat("f", 64)
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "resource manifest hash mismatch",
		},
		{
			name: "size_resource_manifest_path_missing",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.ResourceManifestPath = ""
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "requires resourceManifestPath",
		},
		{
			name: "size_resource_manifest_removed",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(ev.DistDir, filepath.FromSlash(CanonicalResourceManifestRef))); err != nil {
					t.Fatal(err)
				}
			},
			want: "resourceManifestPath",
		},
		{
			name: "size_resource_file_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(ev.DistDir, "_ouroboros", "framework.js"), "tampered")
			},
			want: "resource framework-js metrics mismatch",
		},
		{
			name: "size_resource_extra_file",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(ev.DistDir, "_ouroboros", "extra.js"), "extra")
			},
			want: "extra resource file",
		},
		{
			name: "size_resource_manifest_symlink",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(ev.DistDir, filepath.FromSlash(CanonicalResourceManifestRef))
				outside := filepath.Join(t.TempDir(), "resources.v1.json")
				body, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
		{
			name: "size_resource_file_symlink",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(ev.DistDir, "_ouroboros", "framework.js")
				outside := filepath.Join(t.TempDir(), "framework.js")
				writeTestFile(t, outside, "framework")
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
		{
			name: "size_asset_sha_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Assets[0].SHA256 = strings.Repeat("f", 64)
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "assets mismatch",
		},
		{
			name: "size_asset_compressed_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Assets[0].GzipBytes++
				ev.Assets[0].BrotliBytes++
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "assets mismatch",
		},
		{
			name: "size_route_bytes_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Routes[0].RawBytes++
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "routes mismatch",
		},
		{
			name: "size_totals_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Totals.RawBytes++
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "totals mismatch",
		},
		{
			name: "size_source_path_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Assets[0].SourcePath = filepath.Join(opts.RepoRoot, "build", "canonical-export", "dist", "assets", "runtime", "wrong.wasm")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "assets mismatch",
		},
		{
			name: "size_manifest_hash_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Assets[0].ManifestHash = "wrong"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "assets mismatch",
		},
		{
			name: "size_asset_bucket_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Assets[0].Bucket = "wrong"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "assets mismatch",
		},
		{
			name: "size_route_file_tamper",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.Routes[0].File = "wrong.html"
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "routes mismatch",
		},
		{
			name: "size_missing_html_ref",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				var htmlFile string
				for _, route := range ev.Routes {
					if route.ID != "R00" {
						htmlFile = route.File
						break
					}
				}
				if htmlFile == "" {
					t.Fatal("missing non-static route")
				}
				if err := os.WriteFile(filepath.Join(ev.DistDir, filepath.FromSlash(htmlFile)), []byte("<html></html>"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "HTML attribution replay",
		},
		{
			name: "size_parent_symlink_escape",
			mutate: func(t *testing.T, opts BrowserBaselineOptions, source SourceIdentity) {
				outside := t.TempDir()
				writeTestFile(t, filepath.Join(outside, "build-manifest.json"), `{"manifest":true}`)
				link := filepath.Join(opts.RepoRoot, "build", "manifest-link")
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				ev, err := ReadSizeEvidenceStrict(filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"))
				if err != nil {
					t.Fatal(err)
				}
				ev.ManifestPath = filepath.Join(link, "build-manifest.json")
				writeFixtureJSON(t, filepath.Join(opts.ArtifactRoot, "size", "route-assets.json"), ev)
			},
			want: "path escapes repo root",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, plan, source, routes := valid(t)
			tc.mutate(t, opts, source)
			ok, err := validateCanonicalBrowserRootPreseed(t.Context(), opts, plan, source, routes)
			if err == nil || !strings.Contains(err.Error(), tc.want) || ok {
				t.Fatalf("validateCanonicalBrowserRootPreseed = %v/%v, want %q", ok, err, tc.want)
			}
		})
	}
}

func TestCanonicalBrowserRuntimeBuildArgsMatchCurrentRuntimeVariantShape(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "build", "run", "tinygo", "current", "gosx-runtime-islands.wasm")
	variant := RuntimeArtifactVariant{
		ID:         "islands",
		File:       "gosx-runtime-islands.wasm",
		SourcePath: sourcePath,
		BuildTags:  []string{"tinygo", "gosx_tiny_runtime", "gosx_tiny_islands_only"},
		BuildArgs: []string{
			"build",
			"-target", "wasm",
			"-tags=tinygo gosx_tiny_runtime gosx_tiny_islands_only",
			"-no-debug",
			"-panic=trap",
			"-o", sourcePath,
			"m31labs.dev/gosx/client/wasm",
		},
	}
	if err := validateCanonicalBrowserRuntimeBuildContract(variant); err != nil {
		t.Fatalf("currentRuntimeVariant-shaped args rejected: %v", err)
	}
}

func TestCanonicalBrowserPreseedFailureDoesNotWriteBrowserArtifacts(t *testing.T) {
	repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
	artifactRoot := filepath.Join(t.TempDir(), "browser-preseed-fail")
	plan, err := PredictCanonicalInventoryMaterialization(t.Context(), repoRoot, inventoryPath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeCanonicalInventory(t.Context(), repoRoot, inventoryPath, artifactRoot); err != nil {
		t.Fatal(err)
	}
	source := sourceIdentityFromMaterialization(plan)
	addCanonicalBrowserPreseedGitExcludes(t, repoRoot)
	writeCanonicalBrowserRuntimePreseedForTest(t, repoRoot, artifactRoot, source)
	writeCanonicalBrowserSizePreseedForTest(t, repoRoot, artifactRoot, source, canonicalBrowserRoutesForTest(t))
	writeTestFile(t, filepath.Join(artifactRoot, "dynamic", "old.json"), "{}")
	evidenceRoot := filepath.Join(t.TempDir(), "evidence")
	pixelSource := visual.PixelSourceIdentity{BaseRevision: source.BaseRevision, OverlayHash: source.OverlayHash, InventorySHA256: source.InventorySHA256}
	var pixelRefs []string
	for _, routeID := range []string{"R08", "R10"} {
		for _, backend := range []string{"webgpu", "webgl"} {
			pixelRefs = append(pixelRefs, writeStrictCanonicalPixelManifestRef(t, evidenceRoot, routeID, backend, pixelSource))
		}
	}
	_, err = RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      repoRoot,
		CorpusPath:    filepath.Join("..", "..", "examples", "ouroboros-corpus", "fixtures.v1.json"),
		ArtifactRoot:  artifactRoot,
		EvidenceRoot:  evidenceRoot,
		InventoryPath: inventoryPath,
		PixelManifest: strings.Join(pixelRefs, ","),
		Samples:       "baseline",
	})
	if err == nil || !strings.Contains(err.Error(), "browser-owned") {
		t.Fatalf("RunBrowserBaseline error = %v, want browser-owned preflight failure", err)
	}
	for _, rel := range []string{"manifest.json", "commands.log", "environment.json", "failure.json", "perf/raw-samples.jsonl", "summaries/browser-summary.json"} {
		if _, statErr := os.Stat(filepath.Join(artifactRoot, rel)); !os.IsNotExist(statErr) {
			t.Fatalf("preflight failure wrote %s: %v", rel, statErr)
		}
	}
}

func writeCanonicalBrowserRuntimePreseedForTest(t *testing.T, repoRoot, root string, source SourceIdentity) {
	t.Helper()
	outputDir := filepath.Join(repoRoot, "build", "run", "tinygo", "current")
	writeCanonicalBrowserRuntimePreseedForTestWithOutputDir(t, repoRoot, root, source, outputDir)
}

func writeCanonicalBrowserRuntimePreseedForTestWithOutputDir(t *testing.T, repoRoot, root string, source SourceIdentity, outputDir string) {
	t.Helper()
	runtimeBody := bytes.Repeat([]byte{1}, 1024)
	islandsBody := bytes.Repeat([]byte{2}, 1024)
	writeTestFile(t, filepath.Join(outputDir, "gosx-runtime.wasm"), string(runtimeBody))
	writeTestFile(t, filepath.Join(outputDir, "gosx-runtime-islands.wasm"), string(islandsBody))
	for _, name := range []string{"gosx-runtime.wasm", "gosx-runtime-islands.wasm"} {
		path := filepath.Join(outputDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(GzipBytes(raw)) < len(raw) {
			if err := os.WriteFile(path+".gz", GzipBytes(raw), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(BrotliBytes(raw)) < len(raw) {
			if err := os.WriteFile(path+".br", BrotliBytes(raw), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	shimPath := filepath.Join(repoRoot, "build", "tinygo", "targets", "wasm_exec.js")
	writeTestFile(t, shimPath, "shim")
	writeTestFile(t, filepath.Join(outputDir, "wasm_exec.js"), "shim")
	buildInput, err := BuildInputEvidenceForRepo(repoRoot, "", "")
	if err != nil {
		t.Fatal(err)
	}
	tools := writeCanonicalBrowserRuntimeToolFixtures(t, repoRoot, root, filepath.Join(filepath.Dir(filepath.Dir(shimPath))))
	writeFixtureJSON(t, filepath.Join(root, "wasm", "runtime-artifacts.json"), canonicalBrowserRuntimePreseedForTest(source, outputDir, shimPath, buildInput, tools))
}

func canonicalBrowserRuntimePreseedForTest(source SourceIdentity, outputDir, shimPath string, buildInput BuildInputEvidence, tools map[string]ToolStatus) RuntimeBuildEvidence {
	runtimeMetrics, _ := MetricsForFile(filepath.Join(outputDir, "gosx-runtime.wasm"))
	islandsMetrics, _ := MetricsForFile(filepath.Join(outputDir, "gosx-runtime-islands.wasm"))
	runtimeSize := runtimeMetrics.Bytes
	islandsSize := islandsMetrics.Bytes
	budget := int64(2048)
	shimMetrics, _ := MetricsForFile(shimPath)
	shim := &shimMetrics
	variants := []RuntimeArtifactVariant{
		{ID: "runtime", Generation: "current", Status: "measured", SizeBytes: &runtimeSize, BudgetBytes: &budget, File: "gosx-runtime.wasm", SourcePath: filepath.Join(outputDir, "gosx-runtime.wasm"), SHA256: runtimeMetrics.SHA256, Bytes: runtimeMetrics.Bytes, GzipBytes: runtimeMetrics.GzipBytes, BrotliBytes: runtimeMetrics.BrotliBytes, BuildArgs: []string{"build", "-target", "wasm", "-tags=tinygo gosx_tiny_runtime", "-no-debug", "-panic=trap", "-o", filepath.Join(outputDir, "gosx-runtime.wasm"), "m31labs.dev/gosx/client/wasm"}, BuildTags: []string{"tinygo", "gosx_tiny_runtime"}, TinyGoVersion: "tinygo test", GoVersion: "go test", WasmOptVersion: "wasm-opt test", WasmOptAvailable: true, Shim: shim, PlannedSelectedBy: []string{"R05", "R06", "R07", "R08", "R10"}},
		{ID: "islands", Generation: "current", Status: "measured", SizeBytes: &islandsSize, BudgetBytes: &budget, File: "gosx-runtime-islands.wasm", SourcePath: filepath.Join(outputDir, "gosx-runtime-islands.wasm"), SHA256: islandsMetrics.SHA256, Bytes: islandsMetrics.Bytes, GzipBytes: islandsMetrics.GzipBytes, BrotliBytes: islandsMetrics.BrotliBytes, BuildArgs: []string{"build", "-target", "wasm", "-tags=tinygo gosx_tiny_runtime gosx_tiny_islands_only", "-no-debug", "-panic=trap", "-o", filepath.Join(outputDir, "gosx-runtime-islands.wasm"), "m31labs.dev/gosx/client/wasm"}, BuildTags: []string{"tinygo", "gosx_tiny_runtime", "gosx_tiny_islands_only"}, TinyGoVersion: "tinygo test", GoVersion: "go test", WasmOptVersion: "wasm-opt test", WasmOptAvailable: true, Shim: shim, PlannedSelectedBy: []string{"R02", "R03", "R09A", "R09B"}},
	}
	for _, id := range []string{"core", "engine", "collab", "full"} {
		variants = append(variants, RuntimeArtifactVariant{
			ID:                id,
			Generation:        "future",
			Status:            "planned",
			PlannedSelectedBy: canonicalBrowserFutureRuntimeSelectedRoutes(id),
			MissingReason:     "planned O1-O6 bounded TinyGo variant; no artifact exists in O0.2",
		})
	}
	return RuntimeBuildEvidence{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Source:        source,
		BuildInput:    buildInput,
		OutputDir:     outputDir,
		GoVersion:     tools["go"],
		TinyGo:        tools["tinygo"],
		WasmOpt:       tools["wasm-opt"],
		Variants:      variants,
	}
}

func writeCanonicalBrowserRuntimeToolFixtures(t *testing.T, repoRoot, artifactRoot, tinyRoot string) map[string]ToolStatus {
	t.Helper()
	t.Setenv("CANONICAL_BROWSER_SENTINEL", "present")
	bin := filepath.Join(t.TempDir(), "tool-bin")
	goPath := filepath.Join(bin, "go")
	tinygoReal := filepath.Join(bin, "tinygo-real")
	tinygoPath := filepath.Join(bin, "tinygo")
	wasmOptPath := filepath.Join(bin, "wasm-opt")
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeExecutableTestFile(t, goPath, "#!/bin/sh\nif [ \"$1\" = version ]; then if [ -n \"$CANONICAL_BROWSER_SENTINEL\" ]; then echo 'ambient leak'; else echo 'go test'; fi; exit 0; fi\nexec '"+filepath.ToSlash(realGo)+"' \"$@\"\n")
	writeExecutableTestFile(t, tinygoReal, "#!/bin/sh\nif [ -n \"$CANONICAL_BROWSER_SENTINEL\" ]; then echo 'ambient leak'; exit 0; fi\nif [ \"$1\" = version ]; then echo 'tinygo test'; exit 0; fi\nif [ \"$1\" = env ] && [ \"$2\" = TINYGOROOT ]; then echo '"+filepath.ToSlash(tinyRoot)+"'; exit 0; fi\nif [ \"$1\" = env ] && [ \"$2\" = GOROOT ]; then echo '"+filepath.ToSlash(filepath.Join(repoRoot, "build", "tinygo-goroot"))+"'; exit 0; fi\nexit 1\n")
	if err := os.Symlink("tinygo-real", tinygoPath); err != nil {
		t.Fatal(err)
	}
	writeExecutableTestFile(t, wasmOptPath, "#!/bin/sh\nif [ -n \"$CANONICAL_BROWSER_SENTINEL\" ]; then echo 'ambient leak'; exit 0; fi\nif [ \"$1\" = --version ]; then echo 'wasm-opt test'; exit 0; fi\nexit 1\n")
	ev := &RuntimeBuildEvidence{
		GoVersion: ToolStatus{Name: "go", Path: goPath, Available: true},
		TinyGo:    ToolStatus{Name: "tinygo", Path: tinygoPath, Available: true},
		WasmOpt:   ToolStatus{Name: "wasm-opt", Path: wasmOptPath, Available: true},
	}
	verifier, err := newCanonicalBrowserToolVerifier(BrowserBaselineOptions{
		RepoRoot:     repoRoot,
		ArtifactRoot: artifactRoot,
		EvidenceRoot: filepath.Join(t.TempDir(), "evidence"),
	}, ev)
	if err != nil {
		t.Fatal(err)
	}
	goStatus, err := verifier.status("go", ev.GoVersion, "version")
	if err != nil {
		t.Fatal(err)
	}
	tinyStatus, err := verifier.status("tinygo", ev.TinyGo, "version")
	if err != nil {
		t.Fatal(err)
	}
	wasmStatus, err := verifier.status("wasm-opt", ev.WasmOpt, "--version")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]ToolStatus{"go": goStatus, "tinygo": tinyStatus, "wasm-opt": wasmStatus}
}

func writeExecutableTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeCanonicalBrowserSizePreseedForTest(t *testing.T, repoRoot, root string, source SourceIdentity, routes []FixtureSpec) {
	t.Helper()
	distDir := filepath.Join(repoRoot, "build", "canonical-export", "dist")
	manifestPath := filepath.Join(distDir, "build.json")
	writeCanonicalBrowserSizeDistForTest(t, distDir, routes)
	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: manifestPath,
		DistDir:      distDir,
		RepoRoot:     repoRoot,
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	report.Source = source
	report.Canonical = true
	for i := range report.Routes {
		report.Routes[i].ID = canonicalOuroborosRouteID(report.Routes[i].Route)
	}
	writeFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), report)
}

func writeCanonicalBrowserSizeDistForTest(t *testing.T, distDir string, specs []FixtureSpec) {
	t.Helper()
	runtimeBody := "runtime"
	writeTestFile(t, filepath.Join(distDir, "assets", "runtime", "gosx-runtime.test.wasm"), runtimeBody)
	writeTestFile(t, filepath.Join(distDir, "build.json"), `{
  "runtime": {
    "wasm": {"file": "gosx-runtime.test.wasm", "hash": "runtimehash", "size": 7}
  },
  "islands": [],
  "css": []
}`)
	var exportRoutes []string
	for _, spec := range specs {
		file := canonicalOuroborosRouteFile(spec.Route)
		body := []byte(`<script src="/gosx/runtime.wasm"></script>`)
		writeTestFile(t, filepath.Join(distDir, filepath.FromSlash(file)), string(body))
		writeTestFile(t, filepath.Join(distDir, "static", filepath.FromSlash(file)), string(body))
		sum := sha256.Sum256(body)
		exportRoutes = append(exportRoutes, fmt.Sprintf(
			`{"path":%q,"file":%q,"sha256":%q,"bytes":%d}`,
			spec.Route,
			file,
			"sha256:"+hex.EncodeToString(sum[:]),
			len(body),
		))
	}
	writeTestFile(t, filepath.Join(distDir, "export.json"), `{"routes":[`+strings.Join(exportRoutes, ",")+`],"resourceManifest":`+fmt.Sprintf("%q", CanonicalResourceManifestRef)+`}`)
	resource := testResource(t, distDir, "framework-js", "/_ouroboros/framework.js", "_ouroboros/framework.js", "framework")
	resource.UsedByRoutes = canonicalRouteIDs()
	writeResourceManifestFixture(t, distDir, []ResourceManifestResource{resource}, nil)
}

func canonicalBrowserBuildInputForTest() BuildInputEvidence {
	return BuildInputEvidence{
		ManifestSHA256: "sha256:" + strings.Repeat("a", 64),
		ExportSHA256:   "sha256:" + strings.Repeat("b", 64),
	}
}

func addCanonicalBrowserPreseedGitExcludes(t *testing.T, repoRoot string) {
	t.Helper()
	excludePath := filepath.Join(repoRoot, ".git", "info", "exclude")
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\nbuild/\n"); err != nil {
		t.Fatal(err)
	}
}

func gosxOuroborosTempDirsForTest(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "gosx-ouroboros-source-") {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

func canonicalBrowserRoutesForTest(t *testing.T) []FixtureSpec {
	t.Helper()
	corpus, err := LoadFixtureCorpus(filepath.Join("..", "..", "examples", "ouroboros-corpus", "fixtures.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalRouteSelection(corpus.Routes, nil); err != nil {
		t.Fatal(err)
	}
	return corpus.Routes
}

func TestCanonicalRejectsTamperedInventoryBeforeRootMutation(t *testing.T) {
	root := t.TempDir()
	inventoryPath := filepath.Join(root, "external-source-inventory.json")
	inventoryBody := []byte(`{"schemaVersion":"gosx.ouroboros.baseline.v1","unknown":true}`)
	if err := os.WriteFile(inventoryPath, inventoryBody, 0o644); err != nil {
		t.Fatal(err)
	}
	inventorySum := sha256.Sum256(inventoryBody)
	evidenceRoot := filepath.Join(root, "evidence")
	pixelSource := visual.PixelSourceIdentity{
		BaseRevision:    "abc1234",
		OverlayHash:     "sha256:clean",
		InventorySHA256: "sha256:" + hex.EncodeToString(inventorySum[:]),
	}
	var pixelRefs []string
	for _, routeID := range []string{"R08", "R10"} {
		for _, backend := range []string{"webgpu", "webgl"} {
			pixelRefs = append(pixelRefs, writeStrictCanonicalPixelManifestRef(t, evidenceRoot, routeID, backend, pixelSource))
		}
	}
	artifactRoot := filepath.Join(root, "canonical")
	_, err := RunBrowserBaseline(t.Context(), BrowserBaselineOptions{
		RepoRoot:      filepath.Join("..", ".."),
		ArtifactRoot:  artifactRoot,
		InventoryPath: inventoryPath,
		PixelManifest: strings.Join(pixelRefs, ","),
		Samples:       "baseline",
		Routes:        nil,
		BaseURL:       "http://127.0.0.1:1",
		EvidenceRoot:  evidenceRoot,
		Trace:         false,
		Coverage:      false,
		HeapSnapshots: false,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("RunBrowserBaseline error = %v, want strict inventory failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(artifactRoot, "source", "source-inventory.json")); !os.IsNotExist(statErr) {
		t.Fatalf("materialized inventory survived tamper failure: %v", statErr)
	}
}

func TestExternalR10RequiresDocsBaseURL(t *testing.T) {
	route := FixtureSpec{ID: "R10", Route: "examples/gosx-docs:/demos/water", External: true}
	if _, err := routeURL(BrowserBaselineOptions{BaseURL: "http://127.0.0.1:3000"}, route); err == nil {
		t.Fatal("routeURL accepted R10 without docs base URL")
	}
	got, err := routeURL(BrowserBaselineOptions{DocsBaseURL: "http://127.0.0.1:4000"}, route)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:4000/demos/water" {
		t.Fatalf("routeURL = %q", got)
	}
}

func TestResolveNodeExecutableHonorsTrimmedNODE(t *testing.T) {
	t.Setenv("NODE", "  /tmp/custom-node  ")
	if got := resolveNodeExecutable(); got != "/tmp/custom-node" {
		t.Fatalf("resolveNodeExecutable = %q", got)
	}
	t.Setenv("NODE", "   ")
	if got := resolveNodeExecutable(); got != "node" {
		t.Fatalf("resolveNodeExecutable default = %q", got)
	}
}

func TestWaterProfileUsesResolvedNodeSafeLogAndR10TimeoutFloor(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fakeNode := filepath.Join(dir, "fake-node")
	script := `#!/bin/sh
out=""
timeout=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --out-dir) shift; out="$1" ;;
    --timeout-ms) shift; timeout="$1" ;;
  esac
  shift
done
mkdir -p "$out"
printf '{"validation":{"passed":true,"failures":[]},"timeoutMS":%s}\n' "$timeout" > "$out/water-profile-evidence.json"
`
	if err := os.WriteFile(fakeNode, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NODE", "  "+fakeNode+"  ")
	opts := BrowserBaselineOptions{
		RepoRoot:       root,
		ArtifactRoot:   filepath.Join(dir, "out"),
		Timeout:        25 * time.Second,
		ViewportWidth:  320,
		ViewportHeight: 180,
	}
	var log bytes.Buffer
	ref, err := runWaterProfileEvidence(t.Context(), opts, FixtureSpec{ID: "R10"}, "http://127.0.0.1:3000/demos/water", &log)
	if err != nil {
		t.Fatalf("runWaterProfileEvidence: %v", err)
	}
	if ref != "external/R10/water-profile/water-profile-evidence.json" {
		t.Fatalf("water profile ref = %q", ref)
	}
	if strings.Contains(log.String(), fakeNode) {
		t.Fatalf("command log leaked NODE path: %q", log.String())
	}
	if !strings.Contains(log.String(), "fake-node") {
		t.Fatalf("command log missing safe node label: %q", log.String())
	}
	body, err := os.ReadFile(filepath.Join(opts.ArtifactRoot, ref))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"timeoutMS":60000`) {
		t.Fatalf("water timeout was not floored: %s", body)
	}
}

func TestEffectiveRouteTimeoutFloorsOnlyR10(t *testing.T) {
	opts := BrowserBaselineOptions{Timeout: 25 * time.Second}
	if got := effectiveRouteTimeout(opts, FixtureSpec{ID: "R10"}); got != 60*time.Second {
		t.Fatalf("R10 timeout = %s", got)
	}
	if got := effectiveRouteTimeout(opts, FixtureSpec{ID: "R08"}); got != 25*time.Second {
		t.Fatalf("R08 timeout = %s", got)
	}
	if got := effectiveRouteTimeout(BrowserBaselineOptions{Timeout: 75 * time.Second}, FixtureSpec{ID: "R10"}); got != 75*time.Second {
		t.Fatalf("large R10 timeout = %s", got)
	}
}

func TestFixtureServerReadyRequiresEveryPath2xxOr3xx(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler.HandleFunc("/demos/water", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/demos/water/", http.StatusTemporaryRedirect)
	})
	handler.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	if !fixtureServerReady(t.Context(), client, server.URL, []string{"/readyz", "/demos/water"}) {
		t.Fatal("ready paths with 2xx and 3xx did not pass")
	}
	if fixtureServerReady(t.Context(), client, server.URL, []string{"/readyz", "/missing"}) {
		t.Fatal("readiness accepted a missing docs path")
	}
}

func TestExternalR10ServerPlanDoesNotRequireCorpusBaseURL(t *testing.T) {
	routes := []FixtureSpec{{ID: "R10", Route: "examples/gosx-docs:/demos/water", FixtureApp: "examples/gosx-docs", External: true}}
	server, err := maybeStartFixtureServer(t.Context(), BrowserBaselineOptions{}, routes, io.Discard)
	if err != nil {
		t.Fatalf("maybeStartFixtureServer external-only = %v", err)
	}
	if server != nil {
		t.Fatal("external-only route started corpus fixture server")
	}
	_, err = maybeStartExternalDocsServer(t.Context(), BrowserBaselineOptions{}, routes, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires --docs-base-url") {
		t.Fatalf("maybeStartExternalDocsServer error = %v", err)
	}
	server, err = maybeStartExternalDocsServer(t.Context(), BrowserBaselineOptions{DocsBaseURL: "http://127.0.0.1:4000"}, routes, io.Discard)
	if err != nil {
		t.Fatalf("provided docs base URL errored: %v", err)
	}
	if server != nil {
		t.Fatal("provided docs base URL started managed docs server")
	}
}

func TestBoundaryFailureWritesLocalSanitizedFailureOnce(t *testing.T) {
	dir := t.TempDir()
	node := filepath.Join(dir, "private-node")
	t.Setenv("NODE", node)
	opts := BrowserBaselineOptions{ArtifactRoot: filepath.Join(dir, "out")}
	writeBoundaryFailure(opts, fmt.Errorf("exec %s: no such file or directory", node))
	path := filepath.Join(opts.ArtifactRoot, "failure.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), node) || strings.Contains(string(body), dir) {
		t.Fatalf("failure leaked local executable path: %s", body)
	}
	if !strings.Contains(string(body), "private-node") {
		t.Fatalf("failure omitted safe command label: %s", body)
	}
	writeBoundaryFailure(opts, errors.New("second failure"))
	body2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body2) != string(body) {
		t.Fatalf("failure was overwritten:\nfirst=%s\nsecond=%s", body, body2)
	}
}

func TestR10ExternalEvidenceSanitizesMissingNodePath(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	node := filepath.Join(dir, "missing-node")
	t.Setenv("NODE", node)
	opts := BrowserBaselineOptions{
		RepoRoot:       root,
		ArtifactRoot:   filepath.Join(dir, "out"),
		DocsBaseURL:    "http://127.0.0.1:3000",
		Samples:        "smoke",
		Timeout:        time.Millisecond,
		ViewportWidth:  320,
		ViewportHeight: 180,
	}
	route := FixtureSpec{ID: "R10", Route: "examples/gosx-docs:/demos/water", FixtureApp: "examples/gosx-docs", External: true}
	ev := collectR10ExternalEvidence(t.Context(), opts, route, nil, io.Discard)
	if len(ev.Errors) == 0 {
		t.Fatal("expected missing NODE error")
	}
	body, err := os.ReadFile(filepath.Join(opts.ArtifactRoot, "external", "R10", "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range append(ev.Errors, string(body)) {
		if strings.Contains(text, node) || strings.Contains(text, dir) {
			t.Fatalf("external evidence leaked NODE path: %s", text)
		}
		if !strings.Contains(text, "missing-node") {
			t.Fatalf("external evidence omitted safe label: %s", text)
		}
	}
}

func TestR10ExternalEvidenceFailsClosed(t *testing.T) {
	missing := r10ExternalMissing(ExternalRouteEvidence{})
	for _, want := range []string{"external docs fixture", "water profile evidence"} {
		if !containsString(missing, want) {
			t.Fatalf("missing %q in %v", want, missing)
		}
	}
	if containsString(missing, "webgpu/webgl pixel evidence") {
		t.Fatalf("smoke R10 evidence required canonical pixels: %v", missing)
	}
	missing = r10ExternalMissing(ExternalRouteEvidence{Canonical: true})
	if !containsString(missing, "webgpu/webgl pixel evidence") {
		t.Fatalf("canonical R10 evidence did not require pixels: %v", missing)
	}
	missing = r10ExternalMissing(ExternalRouteEvidence{
		Canonical:         true,
		URL:               "http://127.0.0.1:4000/demos/water",
		BaseURL:           "http://127.0.0.1:4000",
		WaterProfileRef:   "external/R10/water-profile/water-profile-evidence.json",
		PixelManifestRefs: []string{"external/R10/pixels/webgpu/pixel-evidence.json", "external/R10/pixels/webgl/pixel-evidence.json"},
	})
	if len(missing) != 0 {
		t.Fatalf("complete R10 evidence failed: %v", missing)
	}
}

func TestRouteStateMachineR10ExternalRouteMarker(t *testing.T) {
	d := requireOuroborosBrowserDriver(t, 45*time.Second)
	completeExternal := ExternalRouteEvidence{
		RouteID:         "R10",
		URL:             "http://127.0.0.1:4000/demos/water",
		BaseURL:         "http://127.0.0.1:4000",
		WaterProfileRef: "external/R10/water-profile/water-profile-evidence.json",
	}
	tests := []struct {
		name          string
		path          string
		body          string
		wantMissing   []string
		wantNoMissing []string
		wantProof     string
		wantPass      bool
	}{
		{
			name:          "external_water_path_without_fixture_root",
			path:          "/demos/water",
			body:          r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{Telemetry: true, Handler: true}),
			wantNoMissing: []string{"route marker", "routePlanAssertion:external reference only", "water-orbit-state-changed"},
			wantProof:     "water-orbit-state-changed",
			wantPass:      true,
		},
		{
			name:        "wrong_path_does_not_fabricate_marker",
			path:        "/demos/not-water",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{Telemetry: true, Handler: true}),
			wantMissing: []string{"route marker", "routePlanAssertion:external reference only"},
		},
		{
			name:        "water_copy_prefix_rejected",
			path:        "/demos/water-copy",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{Telemetry: true, Handler: true}),
			wantMissing: []string{"route marker", "routePlanAssertion:external reference only"},
		},
		{
			name:        "waterfall_prefix_rejected",
			path:        "/demos/waterfall",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{Telemetry: true, Handler: true}),
			wantMissing: []string{"route marker", "routePlanAssertion:external reference only"},
		},
		{
			name:        "fixture_local_copy_does_not_fabricate_marker",
			path:        "/demos/water",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{LocalCopy: true, Telemetry: true, Handler: true}),
			wantMissing: []string{"route marker", "routePlanAssertion:external reference only"},
		},
		{
			name:        "no_telemetry_fails_orbit",
			path:        "/demos/water",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{}),
			wantMissing: []string{"water-orbit-state-changed"},
			wantProof:   "water-orbit-state-changed",
		},
		{
			name:        "finite_telemetry_without_handler_fails_orbit",
			path:        "/demos/water",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{Telemetry: true}),
			wantMissing: []string{"water-orbit-state-changed"},
			wantProof:   "water-orbit-state-changed",
		},
		{
			name:        "backend_only_canvas_fails_orbit",
			path:        "/demos/water",
			body:        r10ExternalMarkerMockPage(r10ExternalMarkerMockOptions{BackendOnly: true}),
			wantMissing: []string{"water-orbit-state-changed"},
			wantProof:   "water-orbit-state-changed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			if err := d.Navigate(srv.URL + tt.path); err != nil {
				t.Fatalf("Navigate: %v", err)
			}
			if err := d.WaitReady(); err != nil {
				t.Fatalf("WaitReady: %v", err)
			}
			bundle, _ := executeRouteStateMachine(d, r10ExternalMarkerRoute(), completeExternal, SampleLaneProduct)
			for _, want := range tt.wantMissing {
				if !containsString(bundle.MissingRequired, want) {
					t.Fatalf("missing required = %v, want %q", bundle.MissingRequired, want)
				}
			}
			for _, forbidden := range tt.wantNoMissing {
				if containsString(bundle.MissingRequired, forbidden) {
					t.Fatalf("missing required contains %q: %v", forbidden, bundle.MissingRequired)
				}
			}
			if tt.wantProof != "" {
				proof := requireProofByName(t, bundle, tt.wantProof)
				if proof.OK != tt.wantPass {
					t.Fatalf("%s proof OK = %v, want %v payload=%+v", tt.wantProof, proof.OK, tt.wantPass, proof.Payload)
				}
				if tt.wantPass {
					deltas, ok := proof.Payload["deltas"].(map[string]any)
					if !ok || numberFromPayload(deltas, "orbitMax") < 0.01 || numberFromPayload(deltas, "cameraMax") < 0.01 {
						t.Fatalf("orbit proof lacks nonzero deltas: %+v", proof.Payload)
					}
				}
			}
		})
	}
}

func r10ExternalMarkerRoute() FixtureSpec {
	return FixtureSpec{
		ID:                  "R10",
		Route:               "examples/gosx-docs:/demos/water",
		External:            true,
		RoutePlanAssertions: []string{"external reference only"},
	}
}

type r10ExternalMarkerMockOptions struct {
	LocalCopy   bool
	Telemetry   bool
	Handler     bool
	BackendOnly bool
}

func r10ExternalMarkerMockPage(opts r10ExternalMarkerMockOptions) string {
	local := ""
	if opts.LocalCopy {
		local = `<div data-fixture-local-copy="true"></div>`
	}
	telemetry := ""
	if opts.Telemetry {
		telemetry = `
<script>
var orbit = {yaw:1, pitch:0.5, radius:8};
var camera = {x:0,y:2,z:8,rotationX:0.1,rotationY:0.2,rotationZ:0.3};
window.__gosx_scene3d_telemetry = function(){ return {orbit:orbit, camera:camera}; };
</script>`
	}
	handler := ""
	if opts.Handler {
		handler = `
<script>
var dragging = false;
var canvas = document.querySelector("canvas");
canvas.addEventListener("pointerdown", function(){ dragging = true; });
canvas.addEventListener("pointermove", function(){
	if (!dragging) return;
	orbit = {yaw:orbit.yaw+0.15, pitch:orbit.pitch+0.07, radius:orbit.radius};
	camera = {x:camera.x+0.2,y:camera.y,z:camera.z-0.2,rotationX:camera.rotationX,rotationY:camera.rotationY+0.2,rotationZ:camera.rotationZ};
});
canvas.addEventListener("pointerup", function(){ dragging = false; });
</script>`
	}
	if opts.BackendOnly {
		telemetry = ""
		handler = ""
	}
	return `<!doctype html><html><head><title>Water | GoSX</title></head><body>` + local + `
<div data-gosx-scene3d-mounted="true" data-gosx-scene3d-backend="webgpu" data-gosx-scene3d-render-gpu="true" data-gosx-scene3d-render-backend-truth='{"backend":"webgpu","gpu":true,"deviceLost":false,"initError":"","lastError":""}'>
<canvas width="320" height="180" style="width:320px;height:180px"></canvas>
</div>
` + telemetry + handler + `
</body></html>`
}

func numberFromPayload(values map[string]any, key string) float64 {
	switch value := values[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case json.Number:
		out, _ := value.Float64()
		return out
	default:
		return 0
	}
}

func TestProbeCaptureWindowRequiresRouteLoadOrInteraction(t *testing.T) {
	if probeCoversInteractionWindow([]ProbeEvent{{Kind: "mark", Phase: "cold-load"}}) {
		t.Fatal("cold-load only should not satisfy capture window")
	}
	if !probeCoversInteractionWindow([]ProbeEvent{{Kind: "navigation", Phase: "route-load"}}) {
		t.Fatal("route-load should satisfy capture window")
	}
	if !probeCoversInteractionWindow([]ProbeEvent{{Kind: "interaction", Phase: "dispatch"}}) {
		t.Fatal("interaction should satisfy capture window")
	}
	missing := RuntimeJSONProbeCoverage([]ProbeEvent{
		{Kind: "navigation", Phase: "route-load", Detail: map[string]any{"routeID": "R03"}},
	}, []string{"R04"}, []string{"route-load"})
	if len(missing) == 0 {
		t.Fatal("probe coverage accepted a mismatched route")
	}
	missing = runtimeProbeCoverageMissing([]ProbeEvent{
		{Kind: "probe", Name: "install", Phase: "cold-load", Detail: map[string]any{"wrappedCount": 2}},
		{Kind: "navigation", Phase: "route-load", Detail: map[string]any{"routeID": "R03"}},
	}, []string{"R03"}, []string{"route-load"})
	if len(missing) != 0 {
		t.Fatalf("wrapped probe surface should satisfy zero-call coverage, missing %v", missing)
	}
}

func r05ProofRoute() FixtureSpec {
	return FixtureSpec{
		ID:                  "R05",
		Route:               "/canvas-board",
		RoutePlanAssertions: []string{"CanvasBoard marker", "canvas2d surface"},
	}
}

func r04ProofRoute() FixtureSpec {
	return FixtureSpec{
		ID:    "R04",
		Route: "/action/form",
		RoutePlanAssertions: []string{
			"declarative action marker",
			"bootstrap mode lite",
			"validation response",
			"redirect response",
		},
	}
}

func r06ProofRoute() FixtureSpec {
	return FixtureSpec{
		ID:                  "R06",
		Route:               "/hub/echo",
		RoutePlanAssertions: []string{"hub manifest", "echo binding", "no wasm runtime path"},
	}
}

type r04ProofRequests struct {
	mu           sync.Mutex
	invalidJSON  int
	validBrowser int
	validAccept  string
}

func newR04ProofMockServer(t *testing.T) (*httptest.Server, *r04ProofRequests) {
	t.Helper()
	requests := &r04ProofRequests{}
	handler := http.NewServeMux()
	handler.HandleFunc("/action/form", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, r04ProofMockPage())
	})
	handler.HandleFunc("/action/form/__actions/validate-name", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.Form.Get("name"))
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "application/json") {
			requests.mu.Lock()
			requests.invalidJSON++
			requests.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"ok":false,"message":"name required","data":{"html":"<p data-action-state=\"error\">name required</p>"},"fieldErrors":{"name":"name required"},"values":{"name":""}}`)
			return
		}
		if name == "baseline" {
			requests.mu.Lock()
			requests.validBrowser++
			requests.validAccept = accept
			requests.mu.Unlock()
			http.Redirect(w, r, "/action/form?ok=1", http.StatusSeeOther)
			return
		}
		http.Error(w, "unexpected action request", http.StatusBadRequest)
	})
	return httptest.NewServer(handler), requests
}

func r04ProofMockPage() string {
	return `<!doctype html><html><head><title>R04 mock</title></head><body>
<main>
<section data-route-id="R04" data-marker="action-form" data-expected-capability="action-bridge">
<form method="post" action="/action/form/__actions/validate-name" data-action-name="validate-name" data-gosx-action="POST /action/form/__actions/validate-name" data-gosx-action-target="#action-state" data-gosx-action-signal="$ouroboros.action.name">
<label>Name <input name="name" value=""></label>
<button type="submit">Submit</button>
<output id="action-state" data-action-state="idle">idle</output>
</form>
</section>
</main>
<script>window.__gosx = {ready:true};</script>
</body></html>`
}

type r06WSClient struct {
	conn *websocket.Conn
	send chan []byte
}

type r06PreEchoFrame struct {
	Event string
	Data  any
}

func newR06ControlFrameMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newR06MockServer(t, nil)
}

func newR06MockServer(t *testing.T, preEcho []r06PreEchoFrame) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var mu sync.Mutex
	clients := map[*r06WSClient]bool{}
	encode := func(event string, data any) []byte {
		body, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal websocket data: %v", err)
		}
		msg, err := json.Marshal(map[string]any{"event": event, "data": string(body)})
		if err != nil {
			t.Fatalf("marshal websocket message: %v", err)
		}
		return msg
	}
	broadcast := func(payload []byte) {
		mu.Lock()
		defer mu.Unlock()
		for client := range clients {
			select {
			case client.send <- payload:
			default:
			}
		}
	}
	handler := http.NewServeMux()
	handler.HandleFunc("/hub/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, r06ControlFrameMockPage())
	})
	handler.HandleFunc("/_ouroboros/hub/echo", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &r06WSClient{conn: conn, send: make(chan []byte, 8)}
		mu.Lock()
		clients[client] = true
		mu.Unlock()
		go func() {
			defer conn.Close()
			for payload := range client.send {
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					return
				}
			}
		}()
		client.send <- encode("__welcome", map[string]string{"clientId": "r06-test"})
		for _, frame := range preEcho {
			client.send <- encode(frame.Event, frame.Data)
		}
		go func() {
			defer func() {
				mu.Lock()
				delete(clients, client)
				mu.Unlock()
				close(client.send)
				_ = conn.Close()
			}()
			for {
				_, payload, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg struct {
					Event string `json:"event"`
				}
				if err := json.Unmarshal(payload, &msg); err != nil {
					continue
				}
				if msg.Event == "echo" {
					broadcast(encode("echo", map[string]string{"status": "echo"}))
				}
			}
		}()
	})
	return httptest.NewServer(handler)
}

func r06ControlFrameMockPage() string {
	return `<!doctype html><html><head><title>R06 mock</title></head><body>
<main>
<section data-route-id="R06" data-marker="hub-echo" data-expected-capability="collab">
<div data-hub="ouroboros-echo"><p data-signal="$ouroboros.echo">echo signal</p></div>
</section>
</main>
<script>
(function(){
  window.__gosx = {hubs:new Map(), sharedSignals:{values:new Map(), subscribers:new Map()}};
  var ws = new WebSocket(location.origin.replace(/^http/, "ws") + "/_ouroboros/hub/echo");
  ws.onopen = function(){ window.__r06Ready = true; };
  ws.onmessage = function(event) {
    try {
      var msg = JSON.parse(event.data || "{}");
      var data = typeof msg.data === "string" ? JSON.parse(msg.data) : (msg.data || {});
      if (msg.event === "echo" && data.status === "echo") window.__gosx.sharedSignals.values.set("$ouroboros.echo", data);
    } catch (_) {}
  };
})();
</script></body></html>`
}

func newR05ProofMockServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, body)
	}))
}

func requireOuroborosBrowserDriver(t *testing.T, timeout time.Duration) *perf.Driver {
	t.Helper()
	d, err := perf.New(perf.WithHeadless(true), perf.WithTimeout(timeout))
	if err == nil {
		t.Cleanup(func() { _ = d.Close() })
		return d
	}
	if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
		t.Fatalf("GOSX_REQUIRE_CHROME is set, but Chrome could not start: %v", err)
	}
	t.Skipf("skipping browser route proof test: %v", err)
	return nil
}

func injectR05RuntimeProbe(t *testing.T, d *perf.Driver) {
	t.Helper()
	script, err := RuntimeJSONProbeScript([]string{"__gosx_canvas_event"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectPreloadScript(d, script); err != nil {
		t.Fatalf("inject runtime probe: %v", err)
	}
}

func r05ProofMockPage(includeCanvas bool, pixelMode, eventMode string) string {
	canvas := ""
	if includeCanvas {
		canvas = `<canvas id="ouroboros-board" width="640" height="360" data-gosx-surface-id="surface-alpha" style="width:640px;height:360px"></canvas>`
	}
	return `<!doctype html><html><head><title>R05 mock</title></head><body>
<section data-route-id="R05" data-marker="canvas-board" data-expected-capability="engine" data-canvas-board-runtime="true">` + canvas + `</section>
<script>
(function(){
  var selected = "stale";
  var eventMode = "` + eventMode + `";
  window.__gosx = {ready:true, sharedSignals:{values:new Map([["$surface.event.selectedID", selected]])}};
  if (eventMode === "manual-probe-missing-kind" || eventMode === "manual-probe-wrong-kind") {
    window.__gosxOuroborosProbe = {
      events: [],
      refresh: function(){},
      record: function(kind, name, detail) {
        this.events.push({kind:kind, name:name, detail:detail || {}, phase:"interaction"});
      }
    };
  }
  function setSelected(value) {
    selected = value;
    window.__gosx.sharedSignals.values.set("$surface.event.selectedID", value);
  }
  window.__gosx_get_shared_signal = function(name) {
    if (name === "$surface.event.selectedID") return JSON.stringify(selected);
    return "";
  };
  window.__gosx_set_shared_signal = function(name, valueJSON) {
    if (name !== "$surface.event.selectedID") return null;
    try { setSelected(JSON.parse(valueJSON)); } catch (_) { setSelected(String(valueJSON || "")); }
    return null;
  };
  window.__gosx_canvas_event = function(id, kind, floats) {
    if (id === "surface-alpha" && Number(kind) === 3 && floats && floats.length >= 4) {
      setSelected("alpha");
    }
    return null;
  };
  var canvas = document.querySelector("#ouroboros-board");
  if (!canvas) return;
  var pixelMode = "` + pixelMode + `";
  if (pixelMode === "draw") {
    var ctx = canvas.getContext("2d");
    ctx.fillStyle = "rgba(17,24,39,1)";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
  } else if (pixelMode === "missing-context") {
    canvas.getContext = function(){ return null; };
  } else if (pixelMode === "throw") {
    canvas.getContext = function(){ return {getImageData:function(){ throw new Error("tainted canvas"); }}; };
  }
  if (eventMode === "listener" || eventMode === "manual-probe-missing-kind" || eventMode === "manual-probe-wrong-kind") {
    canvas.addEventListener("pointerup", function(e) {
      var r = canvas.getBoundingClientRect();
      window.__gosx_canvas_event(
        canvas.getAttribute("data-gosx-surface-id"),
        3,
        new Float64Array([e.clientX - r.left, e.clientY - r.top, r.width, r.height]),
        ""
      );
      if (eventMode === "manual-probe-missing-kind") {
        window.__gosxOuroborosProbe.record("runtime-call", "__gosx_canvas_event", {argCount:4, argTypes:["string","number","object","string"], exception:""});
      } else if (eventMode === "manual-probe-wrong-kind") {
        window.__gosxOuroborosProbe.record("runtime-call", "__gosx_canvas_event", {argCount:4, argTypes:["string","number","object","string"], eventKind:2, exception:""});
      }
    });
  }
})();
</script></body></html>`
}

func requireProofByName(t *testing.T, bundle ProofBundle, name string) ProofPayload {
	t.Helper()
	for _, proof := range bundle.Interaction {
		if proof.Name == name {
			return proof
		}
	}
	t.Fatalf("missing proof %q in %+v", name, bundle.Interaction)
	return ProofPayload{}
}

func requireProofPayload(t *testing.T, bundle ProofBundle, name string) map[string]any {
	t.Helper()
	proof := requireProofByName(t, bundle, name)
	if !proof.OK {
		t.Fatalf("proof %q failed: %+v", name, proof)
	}
	if proof.Payload == nil {
		t.Fatalf("proof %q has nil payload", name)
	}
	return proof.Payload
}

func requireFailedProofPayload(t *testing.T, bundle ProofBundle, name string) map[string]any {
	t.Helper()
	proof := requireProofByName(t, bundle, name)
	if proof.OK {
		t.Fatalf("proof %q unexpectedly passed: %+v", name, proof)
	}
	if proof.Payload == nil {
		t.Fatalf("proof %q has nil payload", name)
	}
	return proof.Payload
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
