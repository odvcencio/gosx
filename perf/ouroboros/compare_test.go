package ouroboros

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/perf"
	"m31labs.dev/gosx/visual"
)

type compareFixtureOptions struct {
	canonical       bool
	sourceSuffix    string
	sourceMutator   func(*SourceIdentity)
	inventoryLines  int
	metricMutator   func(map[string]float64)
	sampleMutator   func(*BrowserRawSample)
	manifestMutator func(*BrowserManifest)
	envMutator      func(*EnvironmentReport)
	dynamic         *RuntimeJSONDynamicEvidenceManifest
	dynamicBuilder  func(SourceIdentity) *RuntimeJSONDynamicEvidenceManifest
}

func TestCompareSmokeSelfComparePasses(t *testing.T) {
	root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
	report := runSmokeCompare(t, root, root)
	if report.Status != CompareStatusPass || report.ExitCode != 0 {
		t.Fatalf("status=%s exit=%d checks=%+v", report.Status, report.ExitCode, report.Checks)
	}
	if !report.SelfCompare {
		t.Fatalf("selfCompare=false")
	}
	if report.Baseline.Canonical || report.Candidate.Canonical {
		t.Fatalf("smoke fixture reported canonical endpoint")
	}
	if !reportHasMetricPass(report, "memory.listenerCount") {
		t.Fatalf("zero listenerCount did not compare as a present metric")
	}
	for _, metric := range []string{"trace.evaluateScriptMs", "scene.cpuSubmitP95Ms", "memory.wasmPages"} {
		if !reportHasMetricWarn(report, metric) {
			t.Fatalf("inapplicable smoke metric %s did not emit warn skip", metric)
		}
	}
}

func TestCompareLiveR00SmokeSelfCompareWhenPresent(t *testing.T) {
	root := filepath.Join("..", "..", "build", "ouroboros", "o0.2", "browser-smoke-ci", "20260809T081011Z-3954451")
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err != nil {
		t.Skipf("live R00 smoke artifact not present: %v", err)
	}
	report := runSmokeCompare(t, root, root)
	if report.ExitCode != 0 {
		t.Fatalf("live R00 smoke self-compare exit=%d summary=%+v checks=%+v", report.ExitCode, report.Summary, report.Checks)
	}
	if !reportHasMetricPass(report, "memory.listenerCount") {
		t.Fatalf("live R00 listenerCount=0 was not compared")
	}
}

func TestComparePixelSelfCompareUsesExternalEvidenceRoot(t *testing.T) {
	artifactRoot := writeCompareFixture(t, filepath.Join(t.TempDir(), "browser"), compareFixtureOptions{envMutator: func(env *EnvironmentReport) {
		env.HardwareClassification = "hardware-webgpu"
	}, sourceMutator: func(source *SourceIdentity) {
		source.OverlayHash = "sha256:" + strings.Repeat("a", 64)
		source.TrackedDiffHash = "sha256:" + strings.Repeat("b", 64)
		source.UntrackedIncludedSourceHash = "sha256:" + strings.Repeat("c", 64)
	}})
	pixelRoot := filepath.Join(t.TempDir(), "pixel-evidence")
	addPixelRefToFixture(t, artifactRoot, "pixels/R00/webgpu/pixel-evidence.json")
	var manifest BrowserManifest
	readFixtureJSON(t, filepath.Join(artifactRoot, "manifest.json"), &manifest)
	initial := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "R00", "webgpu", "R00-initial-00.png"), true)
	settled := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "R00", "webgpu", "R00-settled-00.png"), true)
	writeFixtureJSON(t, filepath.Join(pixelRoot, "pixels", "R00", "webgpu", "pixel-evidence.json"), compareBaselinePixelManifest(t, manifest.Source, initial, settled))

	withoutRoot := runSmokeCompare(t, artifactRoot, artifactRoot)
	if withoutRoot.ExitCode != 1 || !reportHasMessage(withoutRoot, "pixel manifest") {
		t.Fatalf("compare without pixel root did not fail: %+v", withoutRoot.Summary)
	}

	report, err := CompareOuroborosArtifacts(CompareOptions{
		BaselineManifest:  filepath.Join(artifactRoot, "manifest.json"),
		CandidateManifest: filepath.Join(artifactRoot, "manifest.json"),
		BudgetPath:        compareBudgetPath(t),
		Mode:              CompareModeSmoke,
		BaselinePixelRoot: filepath.Join(pixelRoot),
		GeneratedAt:       time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExitCode != 0 || !report.SelfCompare || !reportHasMetricPass(report, "pixel.diffPct") {
		t.Fatalf("pixel self-compare failed: exit=%d self=%v summary=%+v", report.ExitCode, report.SelfCompare, report.Summary)
	}
}

func TestCompareCandidatePixelReplayAcceptsValidEvidence(t *testing.T) {
	source := compareSource("")
	source.OverlayHash = "sha256:" + strings.Repeat("a", 64)
	source.TrackedDiffHash = "sha256:" + strings.Repeat("b", 64)
	source.UntrackedIncludedSourceHash = "sha256:" + strings.Repeat("c", 64)
	source.InventorySHA256 = "sha256:" + strings.Repeat("e", 64)
	candidateSource := source
	candidateSource.BaseRevision = "1123456789abcdef0123456789abcdef01234567"
	candidateSource.OverlayHash = "sha256:" + strings.Repeat("d", 64)
	candidateSource.InventorySHA256 = "sha256:" + strings.Repeat("f", 64)
	basePixelRoot := mustComparePathSet(t, t.TempDir())
	candidatePixelRoot := mustComparePathSet(t, t.TempDir())
	baseInitial := writeTestPNG(t, filepath.Join(basePixelRoot.root, "pixels", "R00-initial-00.png"), true)
	baseSettled := writeTestPNG(t, filepath.Join(basePixelRoot.root, "pixels", "R00-settled-00.png"), true)
	baselineManifest := compareBaselinePixelManifest(t, source, baseInitial, baseSettled)
	candidateInitial := writeTestPNG(t, filepath.Join(candidatePixelRoot.root, "pixels", "R00-initial-00.png"), true)
	candidateSettled := writeTestPNG(t, filepath.Join(candidatePixelRoot.root, "pixels", "R00-settled-00.png"), true)
	candidateManifest := comparePixelManifest(true, source, candidateSource, candidateInitial, candidateSettled)
	for stateIndex := range candidateManifest.States {
		state := &candidateManifest.States[stateIndex]
		for captureIndex := range state.Captures {
			capture := &state.Captures[captureIndex]
			baselineCapture, ok := findComparePixelCapture(baselineManifest, state.State, capture.Index)
			if !ok {
				t.Fatalf("missing baseline capture %s/%d", state.State, capture.Index)
			}
			basePath, err := resolvePixelCapturePath(basePixelRoot, baselineCapture.Path)
			if err != nil {
				t.Fatal(err)
			}
			candPath, err := resolvePixelCapturePath(candidatePixelRoot, capture.Path)
			if err != nil {
				t.Fatal(err)
			}
			baseData, err := os.ReadFile(basePath)
			if err != nil {
				t.Fatal(err)
			}
			candData, err := os.ReadFile(candPath)
			if err != nil {
				t.Fatal(err)
			}
			comparison, err := visual.ComparePixelEvidenceWithThresholds(baseData, candData, basePath, candPath, 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			capture.Comparison = &comparison
		}
	}
	report := &CompareReport{}
	baseline := &compareLoadedArtifact{manifest: BrowserManifest{Source: source}, environment: EnvironmentReport{HardwareClassification: "hardware-webgpu"}, pixels: []visual.PixelEvidenceManifest{baselineManifest}, pixelRoot: basePixelRoot}
	candidate := &compareLoadedArtifact{manifest: BrowserManifest{Source: candidateSource}, environment: EnvironmentReport{HardwareClassification: "hardware-webgpu"}}
	runCandidatePixelCheck(report, baseline, candidate, candidateManifest, candidatePixelRoot, "ratchet.pixel.valid")
	if len(report.Ratchets) != 2 {
		t.Fatalf("ratchets = %d, want two pass checks: %+v", len(report.Ratchets), report.Ratchets)
	}
	for _, check := range report.Ratchets {
		if check.Status != "pass" {
			t.Fatalf("candidate replay check failed: %+v", check)
		}
	}
}

func TestCompareCandidatePixelReplayRejectsLoosenedThreshold(t *testing.T) {
	source := compareSource("")
	source.OverlayHash = "sha256:" + strings.Repeat("a", 64)
	source.TrackedDiffHash = "sha256:" + strings.Repeat("b", 64)
	source.UntrackedIncludedSourceHash = "sha256:" + strings.Repeat("c", 64)
	source.InventorySHA256 = "sha256:" + strings.Repeat("e", 64)
	candidateSource := source
	candidateSource.BaseRevision = "1123456789abcdef0123456789abcdef01234567"
	candidateSource.OverlayHash = "sha256:" + strings.Repeat("d", 64)
	candidateSource.InventorySHA256 = "sha256:" + strings.Repeat("f", 64)
	basePixelRoot := mustComparePathSet(t, t.TempDir())
	candidatePixelRoot := mustComparePathSet(t, t.TempDir())
	baseInitial := writeTestPNG(t, filepath.Join(basePixelRoot.root, "pixels", "R00-initial-00.png"), true)
	baseSettled := writeTestPNG(t, filepath.Join(basePixelRoot.root, "pixels", "R00-settled-00.png"), true)
	baselineManifest := compareBaselinePixelManifest(t, source, baseInitial, baseSettled)
	baselineManifest.Threshold.EffectivePct = 0.1
	candidateInitial := writeOnePixelChangedTestPNG(t, filepath.Join(candidatePixelRoot.root, "pixels", "R00-initial-00.png"), true)
	candidateSettled := writeOnePixelChangedTestPNG(t, filepath.Join(candidatePixelRoot.root, "pixels", "R00-settled-00.png"), true)
	candidateManifest := comparePixelManifest(true, source, candidateSource, candidateInitial, candidateSettled)
	candidateManifest.Threshold.EffectivePct = 1
	for stateIndex := range candidateManifest.States {
		state := &candidateManifest.States[stateIndex]
		for captureIndex := range state.Captures {
			capture := &state.Captures[captureIndex]
			baselineCapture, ok := findComparePixelCapture(baselineManifest, state.State, capture.Index)
			if !ok {
				t.Fatalf("missing baseline capture %s/%d", state.State, capture.Index)
			}
			basePath, err := resolvePixelCapturePath(basePixelRoot, baselineCapture.Path)
			if err != nil {
				t.Fatal(err)
			}
			candPath, err := resolvePixelCapturePath(candidatePixelRoot, capture.Path)
			if err != nil {
				t.Fatal(err)
			}
			baseData, err := os.ReadFile(basePath)
			if err != nil {
				t.Fatal(err)
			}
			candData, err := os.ReadFile(candPath)
			if err != nil {
				t.Fatal(err)
			}
			forged, err := visual.ComparePixelEvidenceWithThresholds(baseData, candData, basePath, candPath, baselineManifest.Threshold.EffectivePct, candidateManifest.Threshold.EffectivePct)
			if err != nil {
				t.Fatal(err)
			}
			if !forged.Passed {
				t.Fatalf("forged comparison did not pass with loosened threshold: %+v", forged)
			}
			capture.Comparison = &forged
		}
	}
	report := &CompareReport{}
	baseline := &compareLoadedArtifact{manifest: BrowserManifest{Source: source}, environment: EnvironmentReport{HardwareClassification: "hardware-webgpu"}, pixels: []visual.PixelEvidenceManifest{baselineManifest}, pixelRoot: basePixelRoot}
	candidate := &compareLoadedArtifact{manifest: BrowserManifest{Source: candidateSource}, environment: EnvironmentReport{HardwareClassification: "hardware-webgpu"}}
	runCandidatePixelCheck(report, baseline, candidate, candidateManifest, candidatePixelRoot, "ratchet.pixel.loosen")
	if !reportHasMessage(report, "exceeds baseline") {
		t.Fatalf("missing threshold loosening failure: %+v", report.Ratchets)
	}
	if !reportHasMessage(report, "stored pixel comparison does not match replay") {
		t.Fatalf("missing replay mismatch failure: %+v", report.Ratchets)
	}
}

func TestCompareCandidatePixelReplayIsReadOnlyOnRegression(t *testing.T) {
	source := compareSource("")
	source.OverlayHash = "sha256:" + strings.Repeat("a", 64)
	source.TrackedDiffHash = "sha256:" + strings.Repeat("b", 64)
	source.UntrackedIncludedSourceHash = "sha256:" + strings.Repeat("c", 64)
	source.InventorySHA256 = "sha256:" + strings.Repeat("e", 64)
	candidateSource := source
	candidateSource.BaseRevision = "1123456789abcdef0123456789abcdef01234567"
	candidateSource.OverlayHash = "sha256:" + strings.Repeat("d", 64)
	candidateSource.InventorySHA256 = "sha256:" + strings.Repeat("f", 64)

	basePixelRoot := mustComparePathSet(t, t.TempDir())
	candidatePixelRoot := mustComparePathSet(t, t.TempDir())
	baseInitial := writeTestPNG(t, filepath.Join(basePixelRoot.root, "pixels", "R00-initial-00.png"), true)
	baseSettled := writeTestPNG(t, filepath.Join(basePixelRoot.root, "pixels", "R00-settled-00.png"), true)
	baselineManifest := compareBaselinePixelManifest(t, source, baseInitial, baseSettled)
	baselineManifest.Threshold.EffectivePct = 0
	candidateInitial := writeOnePixelChangedTestPNG(t, filepath.Join(candidatePixelRoot.root, "pixels", "R00-initial-00.png"), true)
	candidateSettled := writeOnePixelChangedTestPNG(t, filepath.Join(candidatePixelRoot.root, "pixels", "R00-settled-00.png"), true)
	candidateManifest := comparePixelManifest(false, source, candidateSource, candidateInitial, candidateSettled)
	candidateManifest.Threshold.EffectivePct = 0

	for stateIndex := range candidateManifest.States {
		state := &candidateManifest.States[stateIndex]
		for captureIndex := range state.Captures {
			capture := &state.Captures[captureIndex]
			baselineCapture, ok := findComparePixelCapture(baselineManifest, state.State, capture.Index)
			if !ok {
				t.Fatalf("missing baseline capture %s/%d", state.State, capture.Index)
			}
			basePath, err := resolvePixelCapturePath(basePixelRoot, baselineCapture.Path)
			if err != nil {
				t.Fatal(err)
			}
			candPath, err := resolvePixelCapturePath(candidatePixelRoot, capture.Path)
			if err != nil {
				t.Fatal(err)
			}
			baseData, err := os.ReadFile(basePath)
			if err != nil {
				t.Fatal(err)
			}
			candData, err := os.ReadFile(candPath)
			if err != nil {
				t.Fatal(err)
			}
			comparison, err := visual.ComparePixelEvidenceWithThresholdsReadOnly(baseData, candData, basePath, candPath, baselineManifest.Threshold.EffectivePct, baselineManifest.Threshold.EffectivePct)
			if err != nil {
				t.Fatal(err)
			}
			if comparison.Passed || comparison.DiffPath == "" {
				t.Fatalf("comparison = %+v, want failing comparison with diff path", comparison)
			}
			capture.Comparison = &comparison
		}
	}
	assertNoCompareDiffPNGs(t, basePixelRoot.root, candidatePixelRoot.root)
	chmodCompareTree(t, basePixelRoot.root, 0o555, 0o444)
	defer chmodCompareTree(t, basePixelRoot.root, 0o755, 0o644)
	chmodCompareTree(t, candidatePixelRoot.root, 0o555, 0o444)
	defer chmodCompareTree(t, candidatePixelRoot.root, 0o755, 0o644)

	report := &CompareReport{}
	baseline := &compareLoadedArtifact{manifest: BrowserManifest{Source: source}, environment: EnvironmentReport{HardwareClassification: "hardware-webgpu"}, pixels: []visual.PixelEvidenceManifest{baselineManifest}, pixelRoot: basePixelRoot}
	candidate := &compareLoadedArtifact{manifest: BrowserManifest{Source: candidateSource}, environment: EnvironmentReport{HardwareClassification: "hardware-webgpu"}}
	runCandidatePixelCheck(report, baseline, candidate, candidateManifest, candidatePixelRoot, "ratchet.pixel.readonly")
	if len(report.Ratchets) != 2 {
		t.Fatalf("ratchets = %d, want two checks: %+v", len(report.Ratchets), report.Ratchets)
	}
	for _, check := range report.Ratchets {
		if check.Status != "fail" || check.Message != "pixel comparison failed" || check.Candidate <= 0 || check.AllowedAbs != 0 {
			t.Fatalf("unexpected replay check: %+v", check)
		}
	}
	assertNoCompareDiffPNGs(t, basePixelRoot.root, candidatePixelRoot.root)
}

func TestCompareCanonicalPixelIngressRejectsMalformedV2Evidence(t *testing.T) {
	source := compareSource("")
	source.OverlayHash = "sha256:" + strings.Repeat("a", 64)
	source.TrackedDiffHash = "sha256:" + strings.Repeat("b", 64)
	source.UntrackedIncludedSourceHash = "sha256:" + strings.Repeat("c", 64)
	source.InventorySHA256 = "sha256:" + strings.Repeat("e", 64)
	env := compareEnvironmentFixture()
	env.Viewport = map[string]any{"width": 1280, "height": 720, "dpr": 1}
	for _, tc := range []struct {
		name string
		edit func(*visual.PixelEvidenceManifest)
	}{
		{name: "missing batch", edit: func(m *visual.PixelEvidenceManifest) { m.States[0].Batch = visual.PixelBatchEvidence{} }},
		{name: "duplicate batch ID", edit: func(m *visual.PixelEvidenceManifest) { m.States[1].Batch.ID = m.States[0].Batch.ID }},
		{name: "frame drift", edit: func(m *visual.PixelEvidenceManifest) { m.States[0].Captures[0].FrameSeq++ }},
		{name: "policy mismatch", edit: func(m *visual.PixelEvidenceManifest) { m.SettlePolicy.RAFGate.DrainTicks = 1 }},
		{name: "v1 schema", edit: func(m *visual.PixelEvidenceManifest) { m.SchemaVersion = "gosx.ouroboros.pixels.v1" }},
		{name: "malformed linkage", edit: func(m *visual.PixelEvidenceManifest) { m.States[0].Captures[0].BatchID = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := mustComparePathSet(t, t.TempDir())
			initial := writeTestPNG(t, filepath.Join(root.root, "pixels", "R00-initial-00.png"), true)
			settled := writeTestPNG(t, filepath.Join(root.root, "pixels", "R00-settled-00.png"), true)
			manifest := compareBaselinePixelManifest(t, source, initial, settled)
			tc.edit(&manifest)
			path := filepath.Join(root.root, "pixel-evidence.json")
			writeFixtureJSON(t, path, manifest)
			if err := validatePixelManifestForCompare(root, path, manifest, source, env); err == nil {
				t.Fatalf("validatePixelManifestForCompare accepted %s", tc.name)
			}
		})
	}
}

func TestCompareCanonicalPixelSampleRefsBindRouteAndBackends(t *testing.T) {
	raw := []BrowserRawSample{{
		RouteID:     "R10",
		SampleLane:  SampleLaneProduct,
		Artifacts:   SampleArtifacts{PixelManifestRefs: []string{"r08-webgpu.json", "r10-webgl.json"}},
		SampleIndex: 0,
	}}
	manifests := map[string]visual.PixelEvidenceManifest{
		"r08-webgpu.json": {RouteID: "R08", BackendRequirement: "webgpu"},
		"r10-webgl.json":  {RouteID: "R10", BackendRequirement: "webgl"},
	}
	if err := validateLoadedPixelRefsForSamples(raw, manifests, CompareModeCanonical); err == nil || !strings.Contains(err.Error(), "routeID=R08") {
		t.Fatalf("route substitution error = %v", err)
	}
	manifests["r08-webgpu.json"] = visual.PixelEvidenceManifest{RouteID: "R10", BackendRequirement: "webgpu"}
	if err := validateLoadedPixelRefsForSamples(raw, manifests, CompareModeCanonical); err != nil {
		t.Fatalf("valid route/backend pair failed: %v", err)
	}
}

func TestCompareStrictReadersRejectUnknownAndTrailingJSON(t *testing.T) {
	root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
	manifestPath := filepath.Join(root, "manifest.json")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.Replace(body, []byte(`"schemaVersion"`), []byte(`"unknownField":true,"schemaVersion"`), 1)
	if err := os.WriteFile(manifestPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBrowserManifestStrict(manifestPath); err == nil || !strings.Contains(err.Error(), "unknownField") {
		t.Fatalf("unknown field error = %v", err)
	}

	budgetPath := filepath.Join(t.TempDir(), "budget.json")
	if err := os.WriteFile(budgetPath, []byte(`{"schemaVersion":"gosx.ouroboros.compare-budget.v1","contractVersion":"O0.2","defaults":{}} {}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCompareBudgetStrict(budgetPath); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing data error = %v", err)
	}
}

func TestCompareBudgetRejectsUnsupportedDirection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.json")
	writeFixtureJSON(t, path, CompareBudget{
		SchemaVersion: "gosx.ouroboros.compare-budget.v1",
		Contract:      ContractO02,
		Defaults:      map[string]BudgetThreshold{"startup.dclMs": {Direction: "higher"}},
	})
	if _, err := ReadCompareBudgetStrict(path); err == nil || !strings.Contains(err.Error(), "unsupported direction") {
		t.Fatalf("unsupported direction error = %v", err)
	}
}

func TestCompareZeroBaselineDeltaPctIsFinite(t *testing.T) {
	if got := percentDelta(0, 5); got != 100 {
		t.Fatalf("percentDelta(0,5) = %v, want finite 100", got)
	}
	report := CompareReport{Checks: []CompareCheck{{ID: "zero", Category: "test", Status: "fail", DeltaPct: percentDelta(0, 5)}}}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("marshal report with zero-baseline delta: %v", err)
	}
}

func TestCompareSchemaAndBudgetJSONParse(t *testing.T) {
	for _, path := range []string{"compare.schema.json", "budgets.v1.json"} {
		var value any
		readFixtureJSON(t, path, &value)
	}
}

func TestCompareStrictRawSamplesRejectEmptyLineAndDuplicateTuple(t *testing.T) {
	root := t.TempDir()
	rawPath := filepath.Join(root, "raw.jsonl")
	sample := compareSample(compareSource(""), map[string]float64{"dclMs": 100}, nil)
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, append(append(data, '\n'), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBrowserRawSamplesJSONLStrict(rawPath); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty line error = %v", err)
	}
	if err := os.WriteFile(rawPath, append(append(append([]byte{}, data...), '\n'), append(data, '\n')...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBrowserRawSamplesJSONLStrict(rawPath); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate tuple error = %v", err)
	}
}

func TestCompareArtifactLoadFailures(t *testing.T) {
	t.Run("missing raw ref", func(t *testing.T) {
		root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{
			manifestMutator: func(m *BrowserManifest) { m.RawSamples = "perf/missing.jsonl" },
		})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMessage(report, "rawSamplesRef") {
			t.Fatalf("report did not fail on missing raw ref: %+v", report.Summary)
		}
	})
	t.Run("summary mismatch", func(t *testing.T) {
		root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
		var summary BrowserSummary
		readFixtureJSON(t, filepath.Join(root, "summaries", "browser-summary.json"), &summary)
		summary.SampleCount++
		writeFixtureJSON(t, filepath.Join(root, "summaries", "browser-summary.json"), summary)
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMessage(report, "summary does not equal") {
			t.Fatalf("report did not fail on summary mismatch: %+v", report.Summary)
		}
	})
	t.Run("product probe leakage", func(t *testing.T) {
		root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{
			sampleMutator: func(sample *BrowserRawSample) {
				sample.RuntimeJSONDrain = &RuntimeJSONRawDrain{SchemaVersion: RuntimeJSONProbeSchemaVersion}
			},
		})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMessage(report, "product sample leaked runtime JSON drain") {
			t.Fatalf("report did not fail on product probe leakage: %+v", report.Summary)
		}
	})
	t.Run("missing required metric", func(t *testing.T) {
		root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{
			metricMutator: func(metrics map[string]float64) {
				delete(metrics, "dclMs")
			},
		})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMetricFailure(report, "startup.dclMs") {
			t.Fatalf("report did not fail on missing metric: %+v", report.Summary)
		}
	})
	t.Run("size route set mismatch", func(t *testing.T) {
		root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
		var manifest BrowserManifest
		readFixtureJSON(t, filepath.Join(root, "manifest.json"), &manifest)
		writeFixtureSizeEvidence(t, root, manifest.Source, []RouteAssetEvidence{})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMessage(report, "selected browser routes") {
			t.Fatalf("report did not fail on size route set mismatch: %+v", report.Summary)
		}
	})
}

func TestCompareCanonicalSizeEvidenceRequiresResourceManifestBinding(t *testing.T) {
	distDir := t.TempDir()
	writeResourceManifestFixture(t, distDir, nil, nil)
	_, resourceSHA, err := LoadResourceManifestStrict(filepath.Join(distDir, filepath.FromSlash(CanonicalResourceManifestRef)))
	if err != nil {
		t.Fatal(err)
	}
	ev := &SizeEvidence{
		Canonical:            true,
		DistDir:              distDir,
		ResourceManifestPath: filepath.Join(distDir, filepath.FromSlash(CanonicalResourceManifestRef)),
		BuildInput: BuildInputEvidence{
			ManifestSHA256:         "sha256:" + strings.Repeat("a", 64),
			ExportSHA256:           "sha256:" + strings.Repeat("b", 64),
			ResourceManifestSHA256: resourceSHA,
		},
		Routes: []RouteAssetEvidence{{ID: "R00", Route: "/"}},
	}
	routes := []FixtureSpec{{ID: "R00", Route: "/"}}
	if err := validateSizeEvidenceForCompare(ev, routes); err != nil {
		t.Fatalf("valid canonical size evidence rejected: %v", err)
	}
	if err := validateCanonicalSizeEvidenceResourceManifestForCompare(ev); err != nil {
		t.Fatalf("valid canonical resource manifest rejected: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*SizeEvidence)
		want   string
	}{
		{
			name: "missing_path",
			mutate: func(candidate *SizeEvidence) {
				candidate.ResourceManifestPath = ""
			},
			want: "requires resourceManifestPath",
		},
		{
			name: "missing_hash",
			mutate: func(candidate *SizeEvidence) {
				candidate.BuildInput.ResourceManifestSHA256 = ""
			},
			want: "resourceManifestSha256",
		},
		{
			name: "wrong_path",
			mutate: func(candidate *SizeEvidence) {
				candidate.ResourceManifestPath = filepath.Join(distDir, "_ouroboros", "wrong.json")
			},
			want: "DistDir/_ouroboros/resources.v1.json",
		},
		{
			name: "wrong_hash",
			mutate: func(candidate *SizeEvidence) {
				candidate.BuildInput.ResourceManifestSHA256 = "sha256:" + strings.Repeat("f", 64)
			},
			want: "hash mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *ev
			tc.mutate(&candidate)
			err := validateSizeEvidenceForCompare(&candidate, routes)
			if err == nil {
				err = validateCanonicalSizeEvidenceResourceManifestForCompare(&candidate)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("canonical size validation error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCompareCanonicalSizeEvidenceReplaysRecordedBytes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *SizeEvidence)
		want   string
	}{
		{
			name: "lower_route_gzip",
			mutate: func(t *testing.T, ev *SizeEvidence) {
				for i := range ev.Routes {
					if ev.Routes[i].GzipBytes > 0 {
						ev.Routes[i].GzipBytes--
						return
					}
				}
			},
			want: "routes mismatch",
		},
		{
			name: "alter_totals",
			mutate: func(t *testing.T, ev *SizeEvidence) {
				ev.Totals.GzipBytes++
			},
			want: "totals mismatch",
		},
		{
			name: "drop_asset",
			mutate: func(t *testing.T, ev *SizeEvidence) {
				ev.Assets = ev.Assets[:len(ev.Assets)-1]
			},
			want: "assets mismatch",
		},
		{
			name: "tamper_route_html",
			mutate: func(t *testing.T, ev *SizeEvidence) {
				for _, route := range ev.Routes {
					if route.File == "" {
						continue
					}
					if err := os.WriteFile(filepath.Join(ev.DistDir, filepath.FromSlash(route.File)), []byte("<html>tampered</html>"), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
			},
			want: "HTML attribution replay",
		},
		{
			name: "tamper_build_input_export_hash",
			mutate: func(t *testing.T, ev *SizeEvidence) {
				ev.BuildInput.ExportSHA256 = "sha256:" + strings.Repeat("f", 64)
			},
			want: "build input mismatch",
		},
		{
			name: "tamper_export_path",
			mutate: func(t *testing.T, ev *SizeEvidence) {
				alternate := filepath.Join(ev.DistDir, "export-copy.json")
				body, err := os.ReadFile(ev.ExportPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(alternate, body, 0o644); err != nil {
					t.Fatal(err)
				}
				ev.ExportPath = alternate
			},
			want: "exportPath must be DistDir/export.json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, source, routes := writeCanonicalCompareSizeFixture(t)
			var ev SizeEvidence
			readFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), &ev)
			if len(ev.Assets) == 0 {
				t.Fatal("fixture has no assets")
			}
			tc.mutate(t, &ev)
			writeFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), ev)
			paths := comparePathSet{root: root, rootReal: root}
			_, _, err := loadOptionalSizeEvidence(paths, source, routes, CompareModeCanonical)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadOptionalSizeEvidence error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestComparePolicyCompatibilityFailures(t *testing.T) {
	t.Run("matrix gap", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			manifestMutator: func(m *BrowserManifest) { m.Corpus.Routes = nil },
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasCheck(report, "compat.matrix.routes", "fail") {
			t.Fatalf("missing matrix failure: %+v", report.Summary)
		}
	})
	t.Run("smoke rejects canonical", func(t *testing.T) {
		root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{canonical: true})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMessage(report, "smoke mode rejects canonical") {
			t.Fatalf("missing smoke canonical rejection: %+v", report.Summary)
		}
	})
	t.Run("remote endpoint hash differs", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{
			envMutator: func(e *EnvironmentReport) {
				e.Browser["connectionMode"] = "remote-cdp"
				e.Browser["remoteEndpointSHA256"] = "sha256:one"
				e.Browser["chromeWSURLHash"] = "sha256:stale"
			},
		})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			envMutator: func(e *EnvironmentReport) {
				e.Browser["connectionMode"] = "remote-cdp"
				e.Browser["remoteEndpointSHA256"] = "sha256:two"
				e.Browser["chromeWSURLHash"] = "sha256:stale"
			},
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasCheck(report, "compat.environment.remote-cdp", "fail") {
			t.Fatalf("missing remote endpoint failure: %+v", report.Summary)
		}
	})
	t.Run("remote endpoint hash missing", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{
			envMutator: func(e *EnvironmentReport) {
				e.Browser["connectionMode"] = "remote-cdp"
				delete(e.Browser, "remoteEndpointSHA256")
			},
		})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasCheck(report, "compat.environment.remote-cdp", "fail") {
			t.Fatalf("missing remote endpoint validation: %+v", report.Summary)
		}
	})
}

func TestCanonicalCompareRequiresValidCompatibilityAuditIdentity(t *testing.T) {
	canonicalSource := func() SourceIdentity {
		source := compareSource("")
		source.InventoryRef = "inventory.json"
		source.InventorySHA256 = "sha256:inventory"
		return source
	}
	valid := canonicalSource()
	if err := requireCompareSource(valid, CompareModeCanonical); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(*SourceIdentity)
		want string
	}{
		{
			name: "missing scanStatus",
			edit: func(source *SourceIdentity) {
				source.CompatibilityAudit.ScanStatus = ""
			},
			want: "scanStatus",
		},
		{
			name: "unknown scanStatus",
			edit: func(source *SourceIdentity) {
				source.CompatibilityAudit.ScanStatus = "unknown"
			},
			want: "scanStatus",
		},
		{
			name: "failed scanStatus",
			edit: func(source *SourceIdentity) {
				source.CompatibilityAudit.ScanStatus = compatibilityScanStatusFailed
				source.CompatibilityAudit.Status = "fail-closed"
				source.CompatibilityAudit.CanonicalAvailable = false
				source.CompatibilityAudit.Current = CompatibilityNameSetSummary{}
				source.CompatibilityAudit.Reconciliation.RemovedSinceAnchorCount = 1
			},
			want: "complete passing compatibility audit",
		},
		{
			name: "canonicalAvailable tamper",
			edit: func(source *SourceIdentity) {
				source.CompatibilityAudit.CanonicalAvailable = false
			},
			want: "canonical availability mismatch",
		},
		{
			name: "status tamper",
			edit: func(source *SourceIdentity) {
				source.CompatibilityAudit.Status = "fail-closed"
			},
			want: "want pass",
		},
		{
			name: "receipt diagnostic preserved",
			edit: func(source *SourceIdentity) {
				source.CompatibilityAudit.Receipt.Count = canonicalGosx - 1
			},
			want: "pinned 209-name receipt",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := canonicalSource()
			tc.edit(&source)
			err := requireCompareSource(source, CompareModeCanonical)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("requireCompareSource error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCompareThresholdFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]float64)
		wantID string
		env    func(*EnvironmentReport)
		page   func(*BrowserRawSample)
		caps   []string
	}{
		{"transfer grows", func(m map[string]float64) { m["transferBytes"]++ }, "browser.transferBytes", nil, nil, nil},
		{"startup grows", func(m map[string]float64) { m["dclMs"] = 200 }, "startup.dclMs", nil, nil, nil},
		{"hydration grows", nil, "hydration.totalMs", nil, func(s *BrowserRawSample) { s.Page.IslandHydrationMs = 50 }, nil},
		{"heap grows", func(m map[string]float64) { m["jsHeapUsedMb"] = 20 }, "memory.jsHeapUsedMb", nil, nil, nil},
		{"wasm pages grows", func(m map[string]float64) { m["wasmPages"] = 2 }, "memory.wasmPages", nil, nil, []string{"wasm-current"}},
		{"cpu p95 grows", func(m map[string]float64) { m["sceneCpuP95Ms"] = 20 }, "scene.cpuSubmitP95Ms", nil, nil, []string{"scene3d"}},
		{"raf p95 grows", func(m map[string]float64) { m["rafP95Ms"] = 20 }, "scene.rafP95Ms", hardwareEnv, nil, []string{"scene3d"}},
		{"gpu p95 grows", nil, "scene.gpuTotalP95Ms", hardwareEnv, func(s *BrowserRawSample) { s.Page.Scene.GPU.Total.Stats.P95 = 20 }, []string{"scene3d"}},
		{"console grows", nil, "console.entryCount", nil, func(s *BrowserRawSample) { s.Console = []perf.ConsoleEntry{{Level: "error", Text: "boom"}} }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseRoot := filepath.Join(t.TempDir(), "base")
			candRoot := filepath.Join(t.TempDir(), "candidate")
			capMutator := func(m *BrowserManifest) {
				if len(tt.caps) > 0 {
					caps := append([]string{}, tt.caps...)
					for _, cap := range caps {
						if cap == "wasm-current" {
							m.Corpus.Routes[0].ExpectedTinyGoCurrent = "runtime"
						}
					}
					m.Corpus.Routes[0].ExpectedCapabilities = caps
				}
			}
			base := writeCompareFixture(t, baseRoot, compareFixtureOptions{envMutator: tt.env, manifestMutator: capMutator})
			cand := writeCompareFixture(t, candRoot, compareFixtureOptions{metricMutator: tt.mutate, sampleMutator: tt.page, envMutator: tt.env, manifestMutator: capMutator})
			report := runSmokeCompare(t, base, cand)
			if report.ExitCode != 1 || !reportHasMetricFailure(report, tt.wantID) {
				t.Fatalf("missing failure for %s: exit=%d summary=%+v", tt.wantID, report.ExitCode, report.Summary)
			}
		})
	}
}

func TestCompareRatchetFailures(t *testing.T) {
	t.Run("capability set changes", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			manifestMutator: func(m *BrowserManifest) {
				m.Corpus.Routes[0].ExpectedCapabilities = append(m.Corpus.Routes[0].ExpectedCapabilities, "new-cap")
			},
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.capability.R00", "fail") {
			t.Fatalf("missing capability ratchet: %+v", report.Summary)
		}
	})
	t.Run("inventory JavaScript lines grow", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{inventoryLines: 100})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{inventoryLines: 101})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.source.includedJavaScriptLines", "fail") {
			t.Fatalf("missing inventory line ratchet: %+v", report.Summary)
		}
	})
	t.Run("global growth", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			sourceSuffix: "-global",
			sourceMutator: func(s *SourceIdentity) {
				s.CompatibilityAudit.Current.Count++
				s.CompatibilityAudit.Reconciliation.AddedSinceAnchorCount = 1
			},
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.global.added", "fail") {
			t.Fatalf("missing global growth ratchet: %+v", report.Summary)
		}
	})
	t.Run("static JSON grows", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			sourceMutator: func(s *SourceIdentity) { s.RuntimeJSONStatic.Counts.SerializationHotPathPossibleCount++ },
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.json.static.hotPossible", "fail") {
			t.Fatalf("missing static JSON ratchet: %+v", report.Summary)
		}
	})
	t.Run("dynamic JSON grows", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{dynamicBuilder: func(source SourceIdentity) *RuntimeJSONDynamicEvidenceManifest {
			return compareDynamic(t, source, false)
		}})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{dynamicBuilder: func(source SourceIdentity) *RuntimeJSONDynamicEvidenceManifest {
			return compareDynamic(t, source, true)
		}})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.json.dynamic.hotProduct", "fail") {
			t.Fatalf("missing dynamic JSON ratchets: %+v", report.Summary)
		}
	})
	t.Run("dynamic unknown fails strict validation", func(t *testing.T) {
		root := writeCompareFixture(t, filepath.Join(t.TempDir(), "root"), compareFixtureOptions{dynamicBuilder: func(source SourceIdentity) *RuntimeJSONDynamicEvidenceManifest {
			manifest := compareDynamic(t, source, false)
			manifest.Matrix[0].HotUnknownEventCount = 1
			return manifest
		}})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 1 || !reportHasMessage(report, "validate runtime JSON dynamic evidence") {
			t.Fatalf("missing dynamic validation failure: %+v", report.Summary)
		}
	})
	t.Run("pixel comparison fails", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{})
		var baseManifest, candManifest BrowserManifest
		readFixtureJSON(t, filepath.Join(base, "manifest.json"), &baseManifest)
		readFixtureJSON(t, filepath.Join(cand, "manifest.json"), &candManifest)
		pixelRoot := t.TempDir()
		initial := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "candidate-initial.png"), true)
		settled := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "candidate-settled.png"), true)
		pixelPath := filepath.Join(pixelRoot, "pixel.json")
		writeFixtureJSON(t, pixelPath, comparePixelManifest(false, baseManifest.Source, candManifest.Source, initial, settled))
		report, err := CompareOuroborosArtifacts(CompareOptions{
			BaselineManifest:       filepath.Join(base, "manifest.json"),
			CandidateManifest:      filepath.Join(cand, "manifest.json"),
			BudgetPath:             compareBudgetPath(t),
			Mode:                   CompareModeSmoke,
			CandidatePixelManifest: []string{pixelPath},
			GeneratedAt:            time.Unix(0, 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.ExitCode != 1 || !reportHasMetricFailure(report, "pixel.diffPct") {
			t.Fatalf("missing pixel failure: %+v", report.Summary)
		}
	})
	t.Run("explicit candidate pixel schema fails", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{})
		var baseManifest, candManifest BrowserManifest
		readFixtureJSON(t, filepath.Join(base, "manifest.json"), &baseManifest)
		readFixtureJSON(t, filepath.Join(cand, "manifest.json"), &candManifest)
		pixelRoot := t.TempDir()
		initial := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "candidate-initial.png"), true)
		settled := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "candidate-settled.png"), true)
		pixelPath := filepath.Join(pixelRoot, "pixel.json")
		pixel := comparePixelManifest(true, baseManifest.Source, candManifest.Source, initial, settled)
		pixel.SchemaVersion = "gosx.ouroboros.pixel.bad"
		writeFixtureJSON(t, pixelPath, pixel)
		report, err := CompareOuroborosArtifacts(CompareOptions{
			BaselineManifest:       filepath.Join(base, "manifest.json"),
			CandidateManifest:      filepath.Join(cand, "manifest.json"),
			BudgetPath:             compareBudgetPath(t),
			Mode:                   CompareModeSmoke,
			CandidatePixelManifest: []string{pixelPath},
			GeneratedAt:            time.Unix(0, 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.ExitCode != 1 || !reportHasMessage(report, "schemaVersion") {
			t.Fatalf("missing explicit pixel schema failure: %+v", report.Ratchets)
		}
	})
}

func TestCompareNoisyMetricWithoutRerunProofIsInconclusive(t *testing.T) {
	root := writeNoisyFixture(t, t.TempDir())
	report := runSmokeCompare(t, root, root)
	if report.Status != CompareStatusInconclusive || report.ExitCode != 2 {
		t.Fatalf("status=%s exit=%d summary=%+v", report.Status, report.ExitCode, report.Summary)
	}
}

func writeCanonicalCompareSizeFixture(t *testing.T) (string, SourceIdentity, []FixtureSpec) {
	t.Helper()
	repoRoot, err := resolveRepoRootForEvidence(".")
	if err != nil {
		t.Fatal(err)
	}
	tmpRoot := filepath.Join(repoRoot, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(tmpRoot, "compare-canonical-size-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	distDir := filepath.Join(root, "dist")
	routes := canonicalBrowserRoutesForTest(t)
	writeCanonicalBrowserSizeDistForTest(t, distDir, routes)
	writeCanonicalCompareExportHashesForTest(t, distDir, routes)
	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(distDir, "build.json"),
		DistDir:      distDir,
		RepoRoot:     repoRoot,
		ArtifactRoot: distDir,
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	source := compareSource("")
	report.Source = source
	report.Canonical = true
	for i := range report.Routes {
		report.Routes[i].ID = canonicalOuroborosRouteID(report.Routes[i].Route)
	}
	writeFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), report)
	return root, source, routes
}

func writeCanonicalCompareExportHashesForTest(t *testing.T, distDir string, routes []FixtureSpec) {
	t.Helper()
	type routeRow struct {
		Path   string `json:"path"`
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Bytes  *int64 `json:"bytes"`
	}
	raw := struct {
		Routes           []routeRow `json:"routes"`
		ResourceManifest string     `json:"resourceManifest"`
	}{ResourceManifest: CanonicalResourceManifestRef}
	for _, spec := range routes {
		file := canonicalOuroborosRouteFile(spec.Route)
		htmlPath := filepath.Join(distDir, "static", filepath.FromSlash(file))
		data, err := os.ReadFile(htmlPath)
		if err != nil {
			t.Fatal(err)
		}
		canonicalHTMLPath := filepath.Join(distDir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(canonicalHTMLPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(canonicalHTMLPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		size := int64(len(data))
		raw.Routes = append(raw.Routes, routeRow{
			Path:   spec.Route,
			File:   file,
			SHA256: sha256String(string(data)),
			Bytes:  &size,
		})
	}
	writeFixtureJSON(t, filepath.Join(distDir, "export.json"), raw)
}

func writeCompareFixture(t *testing.T, root string, opts compareFixtureOptions) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "perf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "summaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := compareSource(opts.sourceSuffix)
	if opts.sourceMutator != nil {
		opts.sourceMutator(&source)
	}
	inventoryLines := opts.inventoryLines
	if inventoryLines == 0 {
		inventoryLines = 100
	}
	source = writeFixtureInventory(t, root, source, inventoryLines)
	metrics := compareMetrics()
	if opts.metricMutator != nil {
		opts.metricMutator(metrics)
	}
	sample := compareSample(source, metrics, opts.sampleMutator)
	samples := []BrowserRawSample{sample}
	writeRawSamples(t, filepath.Join(root, "perf", "raw-samples.jsonl"), samples)
	summary := SummarizeBrowserSamples(samples, "smoke", source)
	summary.GeneratedAt = "2026-08-09T00:00:00Z"
	writeFixtureJSON(t, filepath.Join(root, "summaries", "browser-summary.json"), summary)
	env := compareEnvironmentFixture()
	if opts.envMutator != nil {
		opts.envMutator(&env)
	}
	writeFixtureJSON(t, filepath.Join(root, "environment.json"), env)
	manifest := BrowserManifest{
		SchemaVersion: BrowserBaselineSchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		GeneratedAt:   "2026-08-09T00:00:00Z",
		ArtifactRoot:  root,
		Source:        source,
		Corpus: FixtureCorpus{
			SchemaVersion: "gosx.ouroboros.fixtures.v1",
			Contract:      ContractO02,
			CorpusID:      CorpusID,
			FixtureApp:    "fixtures",
			Routes:        []FixtureSpec{{ID: "R00", Route: "/", FixtureApp: "fixtures", ExpectedCapabilities: []string{"server"}}},
		},
		Sampling:    SamplingPlan{Name: "smoke", Canonical: false, ColdSamples: 1, WarmSamples: 1, CanUpdateBaseline: false},
		Environment: "environment.json",
		RawSamples:  "perf/raw-samples.jsonl",
		Summary:     "summaries/browser-summary.json",
		Probe:       DefaultProbeSchemaIdentity(),
		Validation:  BaselineValidation{Status: "pass"},
		Canonical:   opts.canonical,
	}
	if opts.dynamic != nil {
		manifest.DynamicProbe = "dynamic/runtime-json-evidence.json"
		writeFixtureJSON(t, filepath.Join(root, "dynamic", "runtime-json-evidence.json"), opts.dynamic)
	}
	if opts.dynamicBuilder != nil {
		manifest.DynamicProbe = "dynamic/runtime-json-evidence.json"
		writeFixtureJSON(t, filepath.Join(root, "dynamic", "runtime-json-evidence.json"), opts.dynamicBuilder(source))
	}
	if opts.manifestMutator != nil {
		opts.manifestMutator(&manifest)
	}
	writeFixtureJSON(t, filepath.Join(root, "manifest.json"), manifest)
	return root
}

func writeNoisyFixture(t *testing.T, root string) string {
	t.Helper()
	source := compareSource("")
	source = writeFixtureInventory(t, root, source, 100)
	if err := os.MkdirAll(filepath.Join(root, "perf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "summaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	var samples []BrowserRawSample
	for i, dcl := range []float64{100, 100, 200} {
		metrics := compareMetrics()
		metrics["dclMs"] = dcl
		s := compareSample(source, metrics, nil)
		s.SampleIndex = i
		samples = append(samples, s)
	}
	writeRawSamples(t, filepath.Join(root, "perf", "raw-samples.jsonl"), samples)
	summary := SummarizeBrowserSamples(samples, "smoke", source)
	summary.GeneratedAt = "2026-08-09T00:00:00Z"
	writeFixtureJSON(t, filepath.Join(root, "summaries", "browser-summary.json"), summary)
	env := compareEnvironmentFixture()
	writeFixtureJSON(t, filepath.Join(root, "environment.json"), env)
	manifest := BrowserManifest{
		SchemaVersion: BrowserBaselineSchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		GeneratedAt:   "2026-08-09T00:00:00Z",
		ArtifactRoot:  root,
		Source:        source,
		Corpus:        FixtureCorpus{SchemaVersion: "gosx.ouroboros.fixtures.v1", Contract: ContractO02, CorpusID: CorpusID, FixtureApp: "fixtures", Routes: []FixtureSpec{{ID: "R00", Route: "/", FixtureApp: "fixtures", ExpectedCapabilities: []string{"server"}}}},
		Sampling:      SamplingPlan{Name: "smoke", Canonical: false, ColdSamples: 3, WarmSamples: 1, CanUpdateBaseline: false},
		Environment:   "environment.json",
		RawSamples:    "perf/raw-samples.jsonl",
		Summary:       "summaries/browser-summary.json",
		Probe:         DefaultProbeSchemaIdentity(),
		Validation:    BaselineValidation{Status: "pass"},
	}
	writeFixtureJSON(t, filepath.Join(root, "manifest.json"), manifest)
	return root
}

func compareSource(suffix string) SourceIdentity {
	return SourceIdentity{
		BaseRevision:                "0123456789abcdef0123456789abcdef01234567",
		OverlayHash:                 "sha256:overlay" + suffix,
		TrackedDiffHash:             "sha256:tracked" + suffix,
		UntrackedIncludedSourceHash: "sha256:untracked" + suffix,
		StrictInventory:             true,
		ReconstructionProof:         true,
		RuntimeJSONStatic: &RuntimeJSONStaticIdentity{
			SchemaVersion:   RuntimeJSONProbeSchemaVersion,
			ScannerVersion:  runtimeJSONStaticScannerVersion,
			PhaseClassifier: runtimeJSONPhaseClassifierVersion,
			Counts: RuntimeJSONStaticCounts{
				SerializationSiteCount:             1,
				SerializationHotPathPossibleCount:  1,
				SerializationHotPathConfirmedCount: 0,
			},
		},
		CompatibilityAudit: &CompatibilityAuditIdentity{
			SchemaVersion:                 compatibilityAuditSchemaVersion,
			Status:                        "pass",
			ScanStatus:                    compatibilityScanStatusComplete,
			CanonicalAvailable:            true,
			Receipt:                       CompatibilityNameSetSummary{Count: canonicalGosx, NameSetHash: compatibilityReceiptHash},
			Anchor:                        CompatibilityNameSetSummary{Count: canonicalGosx, NameSetHash: "sha256:anchor"},
			Current:                       CompatibilityNameSetSummary{Count: canonicalGosx, NameSetHash: "sha256:current"},
			RuntimeJSONSourceIdentityHash: "sha256:source",
			RuntimeJSONSemanticHash:       "sha256:semantic",
			RuntimeJSONCountsHash:         "sha256:counts",
			RuntimeJSONGlobalNameHash:     "sha256:globals",
		},
	}
}

func writeFixtureInventory(t *testing.T, root string, source SourceIdentity, includedLines int) SourceIdentity {
	t.Helper()
	receipt, err := loadCompatibilityReceipt()
	if err != nil {
		t.Fatal(err)
	}
	receiptSet := compatibilityReceiptEvidenceFromNames(receipt.Names, CompatibilitySourceIdentity{Kind: "pinned-receipt", ArtifactPath: "perf/ouroboros/compatibility_receipt.v1.json"})
	anchorSet := compatibilityEvidenceFromNamesWithEvidenceAndScope(receipt.Names, CompatibilitySourceIdentity{Kind: "clean-anchor", Revision: source.BaseRevision, OverlayHash: OverlayClean}, compatibilityFullRuntimeScope, nil)
	currentSet := compatibilityEvidenceFromNamesWithEvidenceAndScope(receipt.Names, CompatibilitySourceIdentity{Kind: "current-overlay", Revision: source.BaseRevision, OverlayHash: source.OverlayHash}, compatibilityFullRuntimeScope, nil)
	for _, set := range []*CompatibilityNameSetEvidence{&anchorSet, &currentSet} {
		set.RuntimeJSONSourceIdentityHash = "sha256:source"
		set.RuntimeJSONSemanticHash = "sha256:semantic"
		set.RuntimeJSONCountsHash = "sha256:counts"
		set.RuntimeJSONGlobalNameHash = RuntimeJSONStaticGlobalNameHash(set.Names)
		set.EvidenceHash = compatibilityEvidenceHash(*set)
	}
	included := SourceFile{Path: "client/js/bootstrap-src/fixture.js", Language: "javascript", SourceKind: "bootstrap", Lines: includedLines, Bytes: 10, GzipBytes: 10, BrotliBytes: 10, ParseOK: true}
	artifactRef := func(kind, id string) *ArtifactRef {
		return &ArtifactRef{SchemaVersion: "gosx.ouroboros.artifact-ref.v1", Path: kind + "/" + id + ".json", BaseRevision: source.BaseRevision, OverlayHash: source.OverlayHash, SHA256: "sha256:" + kind + id}
	}
	inv := Inventory{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		BaseRevision:  source.BaseRevision,
		OverlayHash:   source.OverlayHash,
		GeneratedAt:   "2026-08-09T00:00:00Z",
		ArtifactRoot:  root,
		Scope:         Scope{Included: []ScopeRule{{Pattern: "client/js/bootstrap-src/**/*.js", Reason: "fixture"}}, Excluded: []ScopeRule{}},
		Overlay:       OverlayEvidence{Status: "clean", Hash: source.OverlayHash, BaseRevision: source.BaseRevision, TrackedDiffHash: source.TrackedDiffHash, TrackedCachedDiffHash: source.TrackedDiffHash, UntrackedSources: []UntrackedSourceHash{}, Recreate: []string{}},
		Files:         FileInventory{Included: []SourceFile{included}, Sidecars: []SourceFile{}, Embedded: []SourceFile{}, Excluded: []ExcludedFile{}, Audit: []ExcludedFile{}},
		Totals:        Totals{IncludedFiles: 1, IncludedJavaScriptLines: includedLines, IncludedBytes: 10, IncludedGzipBytes: 10, IncludedBrotliBytes: 10, ByExtension: map[string]int{".js": 1}},
		Structural:    Structural{Gotreesitter: ParseSummary{Language: "javascript", Parsed: 1}, ImportsExports: []Location{}, FreeGlobalReads: []string{}, FreeGlobalWrites: []string{}},
		Surface: Surface{
			GosxNames:                     []GosxName{},
			BroaderBrowserGosxNames:       []GosxName{},
			SerializationSites:            []SerializationSite{},
			CompatibilityAudit:            CompatibilityAudit{SchemaVersion: compatibilityAuditSchemaVersion, Status: "pass", ScanStatus: compatibilityScanStatusComplete, CanonicalAvailable: true, Receipt: receiptSet, Anchor: anchorSet, Current: currentSet, Reconciliation: CompatibilityReconciliation{RecoveredPreexisting: []string{}, AddedSinceAnchor: []string{}, RemovedSinceAnchor: []string{}, MissingFromAnchor: []string{}}},
			BroaderSerializationSiteCount: 0,
		},
		Ratchets: []ScopeRatchet{{ID: "fixture", Scope: "fixture", Status: "pass", Definition: "fixture"}},
		Manifest: CorpusManifest{
			SchemaVersion: SchemaVersion,
			Contract:      ContractO02,
			Initiative:    Initiative,
			Spec:          Spec,
			CorpusID:      CorpusID,
			BaseRevision:  source.BaseRevision,
			OverlayHash:   source.OverlayHash,
			GeneratedAt:   "2026-08-09T00:00:00Z",
			ArtifactRoot:  root,
			FixtureRoutes: []FixtureRoute{{ID: "R00", Route: "/", FixtureApp: "fixtures", ExpectedTinyGoCurrent: "none", ExpectedTinyGoFuture: "core"}},
			Variants: []RuntimeVariant{
				{ID: "runtime", Generation: "current", Status: "measured", SizeArtifact: artifactRef("size", "runtime"), WASMArtifact: artifactRef("wasm", "runtime"), SelectedByRoutes: []string{}},
				{ID: "islands", Generation: "current", Status: "measured", SizeArtifact: artifactRef("size", "islands"), WASMArtifact: artifactRef("wasm", "islands"), SelectedByRoutes: []string{}},
				{ID: "core", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R00"}},
				{ID: "engine", Generation: "future", Status: "planned", SelectedByRoutes: []string{}},
				{ID: "collab", Generation: "future", Status: "planned", SelectedByRoutes: []string{}},
				{ID: "full", Generation: "future", Status: "planned", SelectedByRoutes: []string{}},
			},
		},
	}
	path := filepath.Join(root, "inventory.json")
	writeFixtureJSON(t, path, inv)
	hash, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	source.InventoryRef = "inventory.json"
	source.InventorySHA256 = hash
	if source.RuntimeJSONStatic != nil {
		binding := DynamicSourceBindingFromSourceIdentity(source)
		source.RuntimeJSONStatic.SourceIdentityHash = runtimeJSONDynamicSourceBindingHash(binding)
		source.RuntimeJSONStatic.SemanticHash = "sha256:semantic"
		source.RuntimeJSONStatic.CountsHash = "sha256:counts"
		source.RuntimeJSONStatic.GlobalNameHash = RuntimeJSONStaticGlobalNameHash([]string{"__gosx_canvas_event"})
	}
	return source
}

func writeFixtureSizeEvidence(t *testing.T, root string, source SourceIdentity, routes []RouteAssetEvidence) {
	t.Helper()
	writeFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), SizeEvidence{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Source:        source,
		BuildInput:    BuildInputEvidence{ManifestSHA256: "sha256:manifest"},
		Routes:        routes,
		Assets:        []TransferredAsset{},
	})
}

func compareMetrics() map[string]float64 {
	return map[string]float64{
		"transferBytes":       100,
		"ttfbMs":              50,
		"dclMs":               100,
		"fullyLoadedMs":       120,
		"jsHeapUsedMb":        10,
		"jsHeapTotalMb":       20,
		"domNodeCount":        20,
		"listenerCount":       0,
		"wasmPages":           1,
		"wasmBytes":           65536,
		"sceneCpuP95Ms":       10,
		"rafP95Ms":            10,
		"missedVsyncEstimate": 0,
		"longTaskTotalMs":     0,
		"totalBlockingTimeMs": 0,
	}
}

func compareSample(source SourceIdentity, metrics map[string]float64, mutator func(*BrowserRawSample)) BrowserRawSample {
	sample := BrowserRawSample{
		SchemaVersion: BrowserBaselineSchemaVersion,
		Contract:      ContractO02,
		Kind:          "browser-sample",
		RunMode:       "smoke",
		RouteID:       "R00",
		Route:         "/",
		URL:           "http://127.0.0.1/",
		SampleLane:    SampleLaneProduct,
		CacheMode:     "cold",
		SampleIndex:   0,
		Source:        source,
		Page: &perf.PageReport{
			TTFBMs:            metrics["ttfbMs"],
			DCLMs:             metrics["dclMs"],
			FullyLoadedMs:     metrics["fullyLoadedMs"],
			IslandHydrationMs: 10,
			Islands:           []perf.IslandMetric{{ID: "island", HydrationMs: 10}},
			Interactions:      []perf.InteractionMetric{{Action: "click", DispatchMs: 10, PatchCount: 1}},
			Scene: &perf.SceneMetric{
				FirstFrameMs: 10,
				FrameStats:   perf.FrameStats{P95: metrics["sceneCpuP95Ms"], Count: 1},
				Presentation: &perf.PresentationMetric{TelemetrySeries: perf.TelemetrySeries{Stats: perf.FrameStats{P95: metrics["rafP95Ms"], Count: 1}}},
				GPU:          &perf.SceneGPUTelemetry{Total: &perf.TelemetrySeries{Stats: perf.FrameStats{P95: 10, Count: 1}}},
			},
		},
		Proofs:  ProofBundle{FirstUsable: ProofPayload{Name: "first", OK: true, AtMs: 100}},
		Trace:   TraceSampleSummary{TotalsMs: map[string]float64{"EvaluateScript": 1, "CompileScript": 1, "v8.compile": 1, "v8.parseOnBackground": 1, "WebAssembly.Compile": 1, "WebAssembly.Instantiate": 1}},
		Memory:  perf.MemoryStats{JSHeapUsedMB: metrics["jsHeapUsedMb"], JSHeapTotalMB: metrics["jsHeapTotalMb"], DOMNodeCount: int(metrics["domNodeCount"]), ListenerCount: int(metrics["listenerCount"])},
		Network: []NetworkRecord{{RuntimeAssetRole: "runtime", TransferredBytes: 10}},
		Metrics: metrics,
	}
	if mutator != nil {
		mutator(&sample)
	}
	return sample
}

func compareEnvironmentFixture() EnvironmentReport {
	return EnvironmentReport{
		SchemaVersion:          BrowserBaselineSchemaVersion,
		GeneratedAt:            "2026-08-09T00:00:00Z",
		EnvironmentClass:       "headless-logic",
		HardwareClassification: "headless-logic",
		Browser:                map[string]any{"connectionMode": "local-exec", "headless": true, "flags": "--headless", "product": "Chrome/126.0.0.0", "majorVersion": 126},
		Viewport:               map[string]any{"width": 1280, "height": 720, "dpr": 1},
		GPU:                    map[string]any{"webgpu": "unknown"},
	}
}

func hardwareEnv(env *EnvironmentReport) {
	env.EnvironmentClass = "hardware-webgpu"
	env.HardwareClassification = "hardware-webgpu"
}

func compareDynamic(t *testing.T, source SourceIdentity, extraProductEvents bool) *RuntimeJSONDynamicEvidenceManifest {
	t.Helper()
	static := DynamicStaticBindingFromRuntimeJSONStaticIdentity(source.RuntimeJSONStatic, []string{"__gosx_canvas_event"})
	var inputs []RuntimeJSONDynamicSampleInput
	requiredProduct := map[string]bool{"R02": true, "R03": true, "R05": true, "R06": true, "R08": true, "R09A": true, "R09B": true, "R10": true}
	for _, routeID := range canonicalRouteIDs() {
		for _, cacheMode := range []string{"cold", "warm"} {
			productPath := "prod/" + routeID + ".js"
			for i := 0; i < 2; i++ {
				inputs = append(inputs,
					RuntimeJSONDynamicSampleInput{Lane: RuntimeJSONDynamicLaneProduct, RouteID: routeID, CacheMode: cacheMode, SampleIndex: i, Pilot: true, Discarded: true, DurationMs: 10},
					RuntimeJSONDynamicSampleInput{Lane: RuntimeJSONDynamicLaneProbeOverhead, RouteID: routeID, CacheMode: cacheMode, SampleIndex: i, Pilot: true, Discarded: true, DurationMs: 11, Drain: emptyRuntimeJSONDrain(routeID, static.KnownGlobals)},
				)
			}
			drain := emptyRuntimeJSONDrain(routeID, static.KnownGlobals)
			if routeID == "R05" || (requiredProduct[routeID] && cacheMode == "cold") {
				drain.Events = append(drain.Events, dynamicProductEvent(routeID))
				if extraProductEvents && routeID != "R05" {
					drain.Events = append(drain.Events, dynamicProductEvent(routeID))
				}
			}
			inputs = append(inputs, RuntimeJSONDynamicSampleInput{
				Lane:                RuntimeJSONDynamicLaneProbe,
				RouteID:             routeID,
				CacheMode:           cacheMode,
				SampleIndex:         0,
				DurationMs:          12,
				ProductPathPrefixes: []string{productPath},
				Drain:               drain,
			})
		}
	}
	manifest, err := BuildRuntimeJSONDynamicEvidence(RuntimeJSONDynamicEvidenceInput{
		GeneratedAt: "2026-08-09T00:00:00Z",
		Source:      DynamicSourceBindingFromSourceIdentity(source),
		Static:      static,
		Samples:     inputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func emptyRuntimeJSONDrain(routeID string, knownGlobals []string) *RuntimeJSONRawDrain {
	return &RuntimeJSONRawDrain{SchemaVersion: RuntimeJSONProbeSchemaVersion, FacadeSchemaVersion: 1, Version: "1", RouteID: routeID, Phase: "input", KnownGlobals: knownGlobals, WrappedGlobals: knownGlobals, Limits: RuntimeJSONRawDrainLimits{EventLimit: 100}}
}

func dynamicProductEvent(routeID string) ProbeEvent {
	source := map[string]any{"urlHash": "0123456789abcdef0123456789abcdef", "path": "prod/" + routeID + ".js", "line": 1, "column": 1}
	if routeID == "R05" {
		return ProbeEvent{Kind: "runtime-call", Phase: "input", Name: "__gosx_canvas_event", Detail: map[string]any{"eventKind": 3, "argCount": 3, "argTypes": []any{"object", "number", "string"}, "argBytes": []any{8, 1, 4}, "resultType": "undefined", "resultBytes": 0, "exception": "", "async": false, "stackHash": "0123456789abcdef0123456789abcdef", "source": source}}
	}
	return ProbeEvent{Kind: "json-call", Phase: "input", Name: "JSON.stringify", Detail: map[string]any{"operation": "stringify", "payloadBytes": 8, "resultBytes": 8, "exception": "", "stackHash": "0123456789abcdef0123456789abcdef", "source": source}}
}

func comparePixelManifest(passed bool, baseline, candidate SourceIdentity, initial, settled visual.PixelCaptureEvidence) visual.PixelEvidenceManifest {
	initial = completeComparePixelCapture(initial, "initial", 0, "webgpu")
	settled = completeComparePixelCapture(settled, "settled", 0, "webgpu")
	initial.Comparison = &visual.PixelComparison{DiffPct: 2, EffectiveThresholdPct: 1, DimensionsMatch: true, Passed: passed}
	settled.Comparison = &visual.PixelComparison{DiffPct: 2, EffectiveThresholdPct: 1, DimensionsMatch: true, Passed: passed}
	return visual.PixelEvidenceManifest{
		SchemaVersion:          visual.OuroborosPixelSchemaVersion,
		GeneratedAt:            "2026-08-09T00:00:00Z",
		RouteID:                "R00",
		Mode:                   string(visual.PixelModeCandidateComparison),
		Source:                 compareTestPixelSource(candidate),
		BaselineSource:         ptrCompareTestPixelSource(baseline),
		SourceRelation:         "candidate-compared-to-baseline",
		BackendRequirement:     "webgpu",
		BackendSelection:       comparePixelBackendSelection("webgpu"),
		HardwareClassification: "hardware-webgpu",
		Viewport:               visual.ViewportEvidence{Width: 1280, Height: 720, DPR: 1},
		Selected:               visual.SelectedSceneEvidence{MountID: "mount", MountSelector: "#mount", CanvasSelector: visual.DefaultPixelCanvasSelector, CanvasCount: 1, MountCount: 1},
		Threshold:              visual.PixelThresholdEvidence{EffectivePct: 1},
		SettlePolicy:           comparePixelSettlePolicy(),
		States: []visual.PixelStateEvidence{
			{State: "initial", Settle: comparePixelSettle(10, false), Batch: comparePixelBatch("R00-initial-batch", "initial", 10, false), Captures: []visual.PixelCaptureEvidence{initial}},
			{State: "settled", Settle: comparePixelSettle(40, true), Batch: comparePixelBatch("R00-settled-batch", "settled", 40, true), Captures: []visual.PixelCaptureEvidence{settled}},
		},
	}
}

func compareBaselinePixelManifest(t *testing.T, source SourceIdentity, initial, settled visual.PixelCaptureEvidence) visual.PixelEvidenceManifest {
	t.Helper()
	initial = completeComparePixelCapture(initial, "initial", 0, "webgpu")
	settled = completeComparePixelCapture(settled, "settled", 0, "webgpu")
	return visual.PixelEvidenceManifest{
		SchemaVersion:          visual.OuroborosPixelSchemaVersion,
		GeneratedAt:            "2026-08-09T00:00:00Z",
		RouteID:                "R00",
		Mode:                   string(visual.PixelModeRecordBaseline),
		Source:                 compareTestPixelSource(source),
		BackendRequirement:     "webgpu",
		BackendSelection:       comparePixelBackendSelection("webgpu"),
		Certified:              true,
		HardwareClassification: "hardware-webgpu",
		Viewport:               visual.ViewportEvidence{Width: 1280, Height: 720, DPR: 1},
		Selected:               visual.SelectedSceneEvidence{MountID: "mount", MountSelector: "#mount", CanvasSelector: visual.DefaultPixelCanvasSelector, CanvasCount: 1, MountCount: 1},
		Threshold:              visual.PixelThresholdEvidence{EffectivePct: 1},
		SettlePolicy:           comparePixelSettlePolicy(),
		States: []visual.PixelStateEvidence{
			{State: "initial", Settle: comparePixelSettle(10, false), Batch: comparePixelBatch("R00-initial-batch", "initial", 10, false), Captures: comparePixelCaptureSeries(t, initial, "initial", 3)},
			{State: "settled", Settle: comparePixelSettle(40, true), Batch: comparePixelBatch("R00-settled-batch", "settled", 40, true), Captures: comparePixelCaptureSeries(t, settled, "settled", 3)},
		},
	}
}

func comparePixelBackendSelection(backend string) visual.PixelBackendSelection {
	return visual.PixelBackendSelection{RequestedBackend: backend, RuntimeObservedBackend: backend, PreNavigationHook: "gosx-o02-clear-force-webgl-new-document"}
}

func compareTestPixelSource(source SourceIdentity) visual.PixelSourceIdentity {
	overlay := source.OverlayHash
	if overlay == "clean" {
		overlay = "sha256:clean"
	}
	return visual.PixelSourceIdentity{BaseRevision: source.BaseRevision, OverlayHash: overlay, InventorySHA256: source.InventorySHA256}
}

func ptrCompareTestPixelSource(source SourceIdentity) *visual.PixelSourceIdentity {
	out := compareTestPixelSource(source)
	return &out
}

func comparePixelSettlePolicy() visual.PixelSettlePolicy {
	return visual.PixelSettlePolicy{WarmupFrames: 30, WarmupAnchor: "initial-observed-frame", RuntimeRenderLoopRequired: true, StaticStoppedAllowsNoAdvance: true, RAFGate: visual.PixelRAFGatePolicy{SchemaVersion: "gosx.ouroboros.raf-gate.v1", Strategy: "raf-batch-gate", Enabled: true, DrainTicks: 2, TemporaryGlobal: true, NonceKeyed: true, NonEnumerable: true, NegativeSyntheticIDs: true, NativeTimestampResume: true, CapturesUseStableClip: true, FailClosedRestore: true, ResumeBeforeNextReadinessWait: true}}
}

func comparePixelSettle(frame int, settled bool) visual.PixelSettleResult {
	if settled {
		return visual.PixelSettleResult{RequiredFrame: 40, ObservedFrame: 10, AdvanceRequired: false, StaticAccepted: true, RenderLoop: visual.RenderLoopEvidence{State: "stopped", Reason: "static", Active: false, WantsAnimation: false, StateParsed: true, WantsAnimationParsed: true, Valid: true}}
	}
	return visual.PixelSettleResult{RequiredFrame: frame, ObservedFrame: frame, AdvanceRequired: false, StaticAccepted: false, RenderLoop: visual.RenderLoopEvidence{State: "active", Reason: "runtime-program", Active: true, WantsAnimation: true, StateParsed: true, WantsAnimationParsed: true, Valid: true}}
}

func comparePixelBatch(id, state string, frame int, settled bool) visual.PixelBatchEvidence {
	loop := comparePixelSettle(frame, settled).RenderLoop
	if settled {
		frame = 10
	}
	snapshot := visual.PixelBatchSnapshot{Visible: true, Focused: true, Backend: "webgpu", Renderer: "webgpu", FrameSeq: frame, RuntimeTruthParsed: true, RenderLoopState: loop.State, RenderLoopActive: loop.Active, WantsAnimation: loop.WantsAnimation, Clip: visual.PixelCanvasClipEvidence{Width: 16, Height: 16, Scale: 1, Stable: true}}
	return visual.PixelBatchEvidence{ID: id, State: state, Acquired: true, Released: true, ReleaseProved: true, NonceHash: "sha256:" + strings.Repeat("1", 64), GlobalKeyHash: "sha256:" + strings.Repeat("2", 64), DrainTicks: 2, NativeTickCount: 2, QueueAfterDrain: 1, QueueBeforeRelease: 1, Delivered: 1, Restored: true, Cleaned: true, Clip: snapshot.Clip, BeforeAcquire: snapshot, Before: snapshot, After: snapshot}
}

func completeComparePixelCapture(capture visual.PixelCaptureEvidence, state string, index int, backend string) visual.PixelCaptureEvidence {
	capture.Index = index
	capture.Backend = backend
	capture.Renderer = backend
	capture.RuntimeTruthParsed = true
	capture.RuntimeGPU = true
	capture.Implementation = backend
	capture.HardwareClass = "hardware-" + backend
	capture.FrameSeq = 10
	capture.BatchID = "R00-" + state + "-batch"
	capture.RenderLoop = comparePixelSettle(capture.FrameSeq, state == "settled").RenderLoop
	capture.Selected = visual.SelectedSceneEvidence{MountID: "mount", MountSelector: "#mount", CanvasSelector: visual.DefaultPixelCanvasSelector, CanvasCount: 1, MountCount: 1}
	capture.WebGPU = visual.WebGPUEvidence{Available: true, AdapterName: "NVIDIA RTX", Vendor: "NVIDIA Corporation", Description: "NVIDIA RTX", AdapterInfo: map[string]interface{}{"vendor": "NVIDIA Corporation", "description": "NVIDIA RTX"}}
	capture.Viewport = visual.ViewportEvidence{Width: 1280, Height: 720, DPR: 1, CanvasWidth: 16, CanvasHeight: 16, CanvasCSSWidth: 16, CanvasCSSHeight: 16, EffectiveDPR: 1}
	return capture
}

func comparePixelCaptureSeries(t *testing.T, first visual.PixelCaptureEvidence, state string, count int) []visual.PixelCaptureEvidence {
	t.Helper()
	data, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]visual.PixelCaptureEvidence, 0, count)
	for i := 0; i < count; i++ {
		capture := first
		capture.Index = i
		if i > 0 {
			path := strings.Replace(first.Path, "-00.png", fmt.Sprintf("-%02d.png", i), 1)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			hash, err := sha256File(path)
			if err != nil {
				t.Fatal(err)
			}
			capture.Path = path
			capture.SHA256 = strings.TrimPrefix(hash, "sha256:")
			capture.Bytes = len(data)
		}
		capture = completeComparePixelCapture(capture, state, i, "webgpu")
		capture.Comparison = &visual.PixelComparison{BaselinePath: capture.Path, DiffPct: 0, Mismatched: 0, Total: capture.Width * capture.Height, DimensionsMatch: true, Similarity: 1, BaselineThresholdPct: 1, EffectiveThresholdPct: 1, Passed: true}
		out = append(out, capture)
	}
	return out
}

func writeTestPNG(t *testing.T, path string, producerStyle bool) visual.PixelCaptureEvidence {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 + x*11), G: uint8(30 + y*13), B: uint8(60 + x*y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	idx := strings.Index(filepath.ToSlash(path), "pixels/")
	if idx < 0 {
		t.Fatalf("test PNG path lacks pixels/: %s", path)
	}
	hashValue := hash
	capturePath := filepath.ToSlash(path)[idx:]
	if producerStyle {
		hashValue = strings.TrimPrefix(hashValue, "sha256:")
		capturePath = path
	}
	return visual.PixelCaptureEvidence{Index: 0, Path: capturePath, SHA256: hashValue, Bytes: buf.Len(), Width: 16, Height: 16}
}

func writeOnePixelChangedTestPNG(t *testing.T, path string, producerStyle bool) visual.PixelCaptureEvidence {
	capture := writeTestPNG(t, path, producerStyle)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	rgba.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	capture.SHA256 = hash
	if producerStyle {
		capture.SHA256 = strings.TrimPrefix(hash, "sha256:")
	}
	capture.Bytes = buf.Len()
	return capture
}

func runSmokeCompare(t *testing.T, baselineRoot, candidateRoot string) *CompareReport {
	t.Helper()
	report, err := CompareOuroborosArtifacts(CompareOptions{
		BaselineManifest:  filepath.Join(baselineRoot, "manifest.json"),
		CandidateManifest: filepath.Join(candidateRoot, "manifest.json"),
		BudgetPath:        compareBudgetPath(t),
		Mode:              CompareModeSmoke,
		GeneratedAt:       time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func mustComparePathSet(t *testing.T, root string) comparePathSet {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return comparePathSet{root: abs, rootReal: real}
}

func assertNoCompareDiffPNGs(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(path, ".diff.png") {
				t.Fatalf("unexpected diff artifact under %s: %s", root, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func chmodCompareTree(t *testing.T, root string, dirMode, fileMode os.FileMode) {
	t.Helper()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := fileMode
		if info.IsDir() {
			mode = dirMode
		}
		return os.Chmod(path, mode)
	}); err != nil {
		t.Fatalf("chmod tree %s: %v", root, err)
	}
}

func compareBudgetPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("budgets.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRawSamples(t *testing.T, path string, samples []BrowserRawSample) {
	t.Helper()
	var buf bytes.Buffer
	for _, sample := range samples {
		data, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFixtureJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func addPixelRefToFixture(t *testing.T, artifactRoot, ref string) {
	t.Helper()
	rawPath := filepath.Join(artifactRoot, "perf", "raw-samples.jsonl")
	samples, err := ReadBrowserRawSamplesJSONLStrict(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := range samples {
		samples[i].Artifacts.PixelManifestRefs = []string{ref}
	}
	writeRawSamples(t, rawPath, samples)
	summary := SummarizeBrowserSamples(samples, "smoke", samples[0].Source)
	summary.GeneratedAt = "2026-08-09T00:00:00Z"
	writeFixtureJSON(t, filepath.Join(artifactRoot, "summaries", "browser-summary.json"), summary)
}

func reportHasMessage(report *CompareReport, needle string) bool {
	for _, check := range append(append([]CompareCheck{}, report.Checks...), report.Ratchets...) {
		if strings.Contains(check.Message, needle) {
			return true
		}
	}
	return false
}

func reportHasCheck(report *CompareReport, id, status string) bool {
	for _, check := range report.Checks {
		if check.ID == id && check.Status == status {
			return true
		}
	}
	return false
}

func reportHasRatchet(report *CompareReport, id, status string) bool {
	for _, check := range report.Ratchets {
		if check.ID == id && check.Status == status {
			return true
		}
	}
	return false
}

func reportHasMetricFailure(report *CompareReport, metric string) bool {
	for _, check := range append(append([]CompareCheck{}, report.Checks...), report.Ratchets...) {
		if check.Metric == metric && check.Status == "fail" {
			return true
		}
	}
	return false
}

func reportHasMetricPass(report *CompareReport, metric string) bool {
	for _, check := range append(append([]CompareCheck{}, report.Checks...), report.Ratchets...) {
		if check.Metric == metric && check.Status == "pass" {
			return true
		}
	}
	return false
}

func reportHasMetricWarn(report *CompareReport, metric string) bool {
	for _, check := range append(append([]CompareCheck{}, report.Checks...), report.Ratchets...) {
		if check.Metric == metric && check.Status == "warn" {
			return true
		}
	}
	return false
}
