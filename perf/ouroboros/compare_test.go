package ouroboros

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/perf"
	"m31labs.dev/gosx/visual"
)

type compareFixtureOptions struct {
	canonical            bool
	sourceSuffix         string
	sourceMutator        func(*SourceIdentity)
	inventoryLines       int
	inventoryBytes       int64
	inventoryNameMutator func([]string) []string
	metricMutator        func(map[string]float64)
	sampleMutator        func(*BrowserRawSample)
	manifestMutator      func(*BrowserManifest)
	envMutator           func(*EnvironmentReport)
	dynamic              *RuntimeJSONDynamicEvidenceManifest
	dynamicBuilder       func(SourceIdentity) *RuntimeJSONDynamicEvidenceManifest
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

func TestValidateRuntimeEvidenceRequiresMeasuredProfilesWithoutRouteClaims(t *testing.T) {
	evidence := measuredRuntimeEvidenceFixture()
	if err := validateRuntimeEvidenceForCompare(evidence); err != nil {
		t.Fatalf("valid runtime evidence rejected: %v", err)
	}

	evidence.Variants[0].PlannedSelectedBy = []string{"R02"}
	if err := validateRuntimeEvidenceForCompare(evidence); err == nil || !strings.Contains(err.Error(), "fabricates route selection") {
		t.Fatalf("fabricated route selection error = %v", err)
	}
}

func TestValidateRuntimeEvidenceRejectsNilRouteReceipt(t *testing.T) {
	evidence := measuredRuntimeEvidenceFixture()
	evidence.Variants[0].PlannedSelectedBy = nil
	if err := validateRuntimeEvidenceForCompare(evidence); err == nil || !strings.Contains(err.Error(), "allocated empty list") {
		t.Fatalf("nil selectedByRoutes error = %v", err)
	}
}

func measuredRuntimeEvidenceFixture() *RuntimeBuildEvidence {
	ids := []struct {
		id      string
		variant string
		mask    uint32
	}{
		{"core", "core", 17},
		{"engine", "engine", 27},
		{"collab", "collab", 21},
		{"full", "full", 31},
		{"islands", "islands", 17},
	}
	shim := &AssetMetrics{
		File:        "wasm_exec.js",
		SourcePath:  "wasm_exec.js",
		SHA256:      strings.Repeat("b", 64),
		Bytes:       50,
		GzipBytes:   40,
		BrotliBytes: 35,
	}
	evidence := &RuntimeBuildEvidence{Variants: make([]RuntimeArtifactVariant, 0, len(ids))}
	for _, item := range ids {
		size := int64(100)
		evidence.Variants = append(evidence.Variants, RuntimeArtifactVariant{
			ID:                item.id,
			Variant:           item.variant,
			FeatureMask:       item.mask,
			Generation:        "current",
			Status:            "measured",
			SizeBytes:         &size,
			File:              item.id + ".wasm",
			SourcePath:        item.id + ".wasm",
			SHA256:            strings.Repeat("a", 64),
			Bytes:             size,
			GzipBytes:         80,
			BrotliBytes:       70,
			Shim:              shim,
			PlannedSelectedBy: []string{},
		})
	}
	return evidence
}

func TestValidateCanonicalRuntimeEvidenceBundleRecomputesBundledBytes(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "manifest.json"), []byte(`{}`))
	paths, err := resolveCompareManifestPath(root)
	if err != nil {
		t.Fatal(err)
	}
	evidence := writeCanonicalRuntimeBundleFixture(t, root)
	if err := validateRuntimeEvidenceForCompare(evidence); err != nil {
		t.Fatalf("valid runtime receipt rejected structurally: %v", err)
	}
	if err := validateCanonicalRuntimeEvidenceBundle(paths, evidence); err != nil {
		t.Fatalf("valid runtime bundle rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RuntimeBuildEvidence)
		want   string
	}{
		{
			name: "fabricated coherent metrics",
			mutate: func(ev *RuntimeBuildEvidence) {
				body := []byte("fabricated-smaller-runtime")
				sum := sha256.Sum256(body)
				variant := &ev.Variants[0]
				variant.SHA256 = hex.EncodeToString(sum[:])
				variant.Bytes = int64(len(body))
				variant.GzipBytes = GzipLength(body)
				variant.BrotliBytes = BrotliLength(body)
				variant.SizeBytes = int64Pointer(int64(len(body)))
			},
			want: "does not match bundled bytes",
		},
		{
			name: "artifact escape",
			mutate: func(ev *RuntimeBuildEvidence) {
				ev.Variants[0].SourcePath = "../../outside.wasm"
			},
			want: "artifact path",
		},
		{
			name: "mutated shim receipt",
			mutate: func(ev *RuntimeBuildEvidence) {
				for i := range ev.Variants {
					shim := *ev.Variants[i].Shim
					shim.Bytes++
					ev.Variants[i].Shim = &shim
				}
			},
			want: "wasm_exec.js receipt does not match bundled bytes",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyEvidence := cloneRuntimeEvidence(t, evidence)
			test.mutate(copyEvidence)
			err := validateCanonicalRuntimeEvidenceBundle(paths, copyEvidence)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("mutation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCanonicalRuntimeLoaderRejectsMutableReceiptMetrics(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "manifest.json"), []byte(`{}`))
	paths, err := resolveCompareManifestPath(root)
	if err != nil {
		t.Fatal(err)
	}
	evidence := writeCanonicalRuntimeBundleFixture(t, root)
	source := compareSource("-runtime-bundle")
	evidence.SchemaVersion = SchemaVersion
	evidence.Contract = ContractO02
	evidence.Source = source
	evidence.Variants[0].Bytes++
	evidence.Variants[0].SizeBytes = int64Pointer(evidence.Variants[0].Bytes)
	writeFixtureJSON(t, filepath.Join(root, "wasm", "runtime-artifacts.json"), evidence)

	_, _, err = loadOptionalRuntimeEvidence(paths, source, CompareModeCanonical)
	if err == nil || !strings.Contains(err.Error(), "does not match bundled bytes") {
		t.Fatalf("mutable runtime receipt error = %v", err)
	}
}

func TestSmokeRuntimeLoaderRejectsMutableReceiptMetrics(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "manifest.json"), []byte(`{}`))
	paths, err := resolveCompareManifestPath(root)
	if err != nil {
		t.Fatal(err)
	}
	evidence := writeCanonicalRuntimeBundleFixture(t, root)
	source := compareSource("-smoke-runtime-bundle")
	evidence.SchemaVersion = SchemaVersion
	evidence.Contract = ContractO02
	evidence.Source = source
	evidence.Variants[0].GzipBytes++
	writeFixtureJSON(t, filepath.Join(root, "wasm", "runtime-artifacts.json"), evidence)

	_, _, err = loadOptionalRuntimeEvidence(paths, source, CompareModeSmoke)
	if err == nil || !strings.Contains(err.Error(), "does not match bundled bytes") {
		t.Fatalf("smoke mutable runtime receipt error = %v", err)
	}
}

func writeCanonicalRuntimeBundleFixture(t *testing.T, root string) *RuntimeBuildEvidence {
	t.Helper()
	evidence := measuredRuntimeEvidenceFixture()
	evidence.BundleRoot = canonicalRuntimeEvidenceBundleRoot
	evidence.OutputDir = "."
	wantFiles := map[string]string{
		"core": "gosx-runtime-core.wasm", "engine": "gosx-runtime-engine.wasm",
		"collab": "gosx-runtime-collab.wasm", "full": "gosx-runtime.wasm",
		"islands": "gosx-runtime-islands.wasm",
	}
	for i := range evidence.Variants {
		variant := &evidence.Variants[i]
		variant.File = wantFiles[variant.ID]
		variant.SourcePath = canonicalRuntimeEvidenceBundleRoot + "/" + variant.File
		body := []byte("runtime-bundle:" + variant.ID)
		writeFixtureFile(t, filepath.Join(root, filepath.FromSlash(variant.SourcePath)), body)
		metrics, err := MetricsForFile(filepath.Join(root, filepath.FromSlash(variant.SourcePath)))
		if err != nil {
			t.Fatal(err)
		}
		variant.SHA256 = metrics.SHA256
		variant.Bytes = metrics.Bytes
		variant.GzipBytes = metrics.GzipBytes
		variant.BrotliBytes = metrics.BrotliBytes
		variant.SizeBytes = int64Pointer(metrics.Bytes)
	}
	shimRef := canonicalRuntimeEvidenceBundleRoot + "/wasm_exec.js"
	writeFixtureFile(t, filepath.Join(root, filepath.FromSlash(shimRef)), []byte("runtime-shim-bundle"))
	shim, err := MetricsForFile(filepath.Join(root, filepath.FromSlash(shimRef)))
	if err != nil {
		t.Fatal(err)
	}
	shim.SourcePath = shimRef
	for i := range evidence.Variants {
		shimCopy := shim
		evidence.Variants[i].Shim = &shimCopy
	}
	return evidence
}

func cloneRuntimeEvidence(t *testing.T, evidence *RuntimeBuildEvidence) *RuntimeBuildEvidence {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var clone RuntimeBuildEvidence
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}

func TestValidateCanonicalR10SizeProvenanceBindsBothBuilds(t *testing.T) {
	input := BuildInputEvidence{
		GoSXModuleDir:              ".",
		GoSXModuleVersion:          "v0.39.0",
		GoModSHA256:                "sha256:" + strings.Repeat("a", 64),
		GoSumSHA256:                "sha256:" + strings.Repeat("b", 64),
		ManifestSHA256:             "sha256:" + strings.Repeat("c", 64),
		ExportSHA256:               "sha256:" + strings.Repeat("d", 64),
		RejectsModuleCacheMismatch: true,
	}
	r10Input := input
	r10Input.ManifestSHA256 = "sha256:" + strings.Repeat("e", 64)
	r10Input.ExportSHA256 = "sha256:" + strings.Repeat("f", 64)
	evidence := &SizeEvidence{
		Canonical:       true,
		BuildInput:      input,
		ManifestPath:    "primary/build.json",
		DistDir:         "primary",
		ExportPath:      "primary/export.json",
		R10BuildInput:   &r10Input,
		R10ManifestPath: "r10/build.json",
		R10DistDir:      "r10",
		R10ExportPath:   "r10/export.json",
		Notes:           []string{portableCombinedSizeInputLabelsNote},
	}
	if err := validateCanonicalR10SizeProvenance(evidence, true); err != nil {
		t.Fatalf("valid combined provenance rejected: %v", err)
	}
	evidence.R10BuildInput.GoModSHA256 = "sha256:" + strings.Repeat("0", 64)
	if err := validateCanonicalR10SizeProvenance(evidence, true); err == nil || !strings.Contains(err.Error(), "does not share") {
		t.Fatalf("mismatched R10 source error = %v", err)
	}
	evidence.R10BuildInput = nil
	if err := validateCanonicalR10SizeProvenance(evidence, true); err == nil || !strings.Contains(err.Error(), "r10BuildInput") {
		t.Fatalf("missing R10 build input error = %v", err)
	}
}

func TestValidateSizeEvidenceReconcilesAssetsRoutesAndTotals(t *testing.T) {
	routes := []FixtureSpec{
		{ID: "R00", Route: "/static"},
		{ID: "R01", Route: "/lite"},
		{ID: "R02", Route: "/island/counter"},
	}
	if err := validateSizeEvidenceForCompare(validReconciledSizeEvidence(), routes); err != nil {
		t.Fatalf("valid evidence, including measured-zero R00, rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*SizeEvidence)
		message string
	}{
		{
			name:    "asset id is content-bound",
			mutate:  func(ev *SizeEvidence) { ev.Assets[0].ID = "asset-tampered" },
			message: "does not bind url and sha256",
		},
		{
			name:    "asset sha is valid",
			mutate:  func(ev *SizeEvidence) { ev.Assets[0].SHA256 = "not-a-sha" },
			message: "invalid sha256",
		},
		{
			name: "asset ids are unique",
			mutate: func(ev *SizeEvidence) {
				ev.Assets[1] = ev.Assets[0]
			},
			message: "duplicate asset",
		},
		{
			name: "same content has consistent metrics",
			mutate: func(ev *SizeEvidence) {
				ev.Assets[1].SHA256 = ev.Assets[0].SHA256
				ev.Assets[1].ID = stableAssetID(ev.Assets[1].URL, ev.Assets[1].SHA256)
			},
			message: "inconsistent transfer metrics",
		},
		{
			name:    "duplicate linkage is acyclic and content-bound",
			mutate:  func(ev *SizeEvidence) { ev.Assets[1].DuplicateOf = ev.Assets[1].ID },
			message: "duplicateOf",
		},
		{
			name: "duplicate linkage names first same-content asset",
			mutate: func(ev *SizeEvidence) {
				ev.Assets[1].SHA256 = ev.Assets[0].SHA256
				ev.Assets[1].ID = stableAssetID(ev.Assets[1].URL, ev.Assets[1].SHA256)
				ev.Assets[1].Bytes = ev.Assets[0].Bytes
				ev.Assets[1].GzipBytes = ev.Assets[0].GzipBytes
				ev.Assets[1].BrotliBytes = ev.Assets[0].BrotliBytes
				ev.Assets[1].DuplicateOf = ""
			},
			message: "want first same-content asset",
		},
		{
			name:    "route asset exists",
			mutate:  func(ev *SizeEvidence) { ev.Routes[1].AssetIDs[0] = "asset-missing" },
			message: "references unknown asset",
		},
		{
			name: "route asset is unique",
			mutate: func(ev *SizeEvidence) {
				ev.Routes[1].AssetIDs = append(ev.Routes[1].AssetIDs, ev.Routes[1].AssetIDs[0])
			},
			message: "duplicate asset id",
		},
		{
			name:    "route totals cannot be zeroed",
			mutate:  func(ev *SizeEvidence) { ev.Routes[1].RawBytes = 0 },
			message: "rawBytes",
		},
		{
			name:    "shared attribution cannot be zeroed",
			mutate:  func(ev *SizeEvidence) { ev.Routes[1].SharedGzipBytes = 0 },
			message: "sharedGzipBytes",
		},
		{
			name:    "unique attribution cannot be zeroed",
			mutate:  func(ev *SizeEvidence) { ev.Routes[1].UniqueBrotliBytes = 0 },
			message: "uniqueBrotliBytes",
		},
		{
			name:    "asset route ownership is reconciled",
			mutate:  func(ev *SizeEvidence) { ev.Assets[0].UsedByRoutes = nil },
			message: "usedByRoutes",
		},
		{
			name:    "shared attribution comment is reconciled",
			mutate:  func(ev *SizeEvidence) { ev.Routes[1].AttributionComment = "mutable claim" },
			message: "attributionComment",
		},
		{
			name:    "global totals cannot be zeroed",
			mutate:  func(ev *SizeEvidence) { ev.Totals = SizeEvidenceTotals{} },
			message: "recomputed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := validReconciledSizeEvidence()
			test.mutate(evidence)
			err := validateSizeEvidenceForCompare(evidence, routes)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("validation error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestCanonicalCompareRejectsNoncanonicalZeroSizeReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "size"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := compareSource("-noncanonical-size")
	writeFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), SizeEvidence{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Source:        source,
		Canonical:     false,
		Assets:        []TransferredAsset{},
		Routes: []RouteAssetEvidence{{
			ID:       "R00",
			Route:    "/static",
			AssetIDs: nil,
		}},
		Totals: SizeEvidenceTotals{RouteCount: 1},
	})
	_, _, err := loadOptionalSizeEvidence(
		comparePathSet{root: root, rootReal: root},
		source,
		[]FixtureSpec{{ID: "R00", Route: "/static"}},
		CompareModeCanonical,
	)
	if err == nil || !strings.Contains(err.Error(), "requires canonical size evidence") {
		t.Fatalf("canonical size downgrade error = %v", err)
	}
}

func TestSmokeSizeLoaderRejectsUncontainedReceiptMetrics(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "size"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := compareSource("-smoke-size-bundle")
	evidence := validReconciledSizeEvidence()
	evidence.SchemaVersion = SchemaVersion
	evidence.Contract = ContractO02
	evidence.Source = source
	evidence.Canonical = false
	evidence.BundleRoot = ""
	writeFixtureJSON(t, filepath.Join(root, "size", "route-assets.json"), evidence)

	routes := []FixtureSpec{
		{ID: "R00", Route: "/static"},
		{ID: "R01", Route: "/lite"},
		{ID: "R02", Route: "/island/counter"},
	}
	_, _, err := loadOptionalSizeEvidence(
		comparePathSet{root: root, rootReal: root},
		source,
		routes,
		CompareModeSmoke,
	)
	if err == nil || !strings.Contains(err.Error(), "canonical size evidence bundle requires canonical receipt") {
		t.Fatalf("smoke uncontained size receipt error = %v", err)
	}
}

func TestCompareRejectsManifestSamplingDowngrade(t *testing.T) {
	root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
	var manifest BrowserManifest
	readFixtureJSON(t, filepath.Join(root, "manifest.json"), &manifest)
	manifest.Canonical = true
	manifest.Sampling.Canonical = false
	writeFixtureJSON(t, filepath.Join(root, "manifest.json"), manifest)
	_, err := loadCompareArtifact(filepath.Join(root, "manifest.json"), CompareModeCanonical, "")
	if err == nil || !strings.Contains(err.Error(), "sampling canonical") {
		t.Fatalf("manifest sampling downgrade error = %v", err)
	}
}

func TestCompareRejectsMutableSmokeSamplingPlan(t *testing.T) {
	root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
	var manifest BrowserManifest
	readFixtureJSON(t, filepath.Join(root, "manifest.json"), &manifest)
	manifest.Sampling.PilotsDiscarded = 99
	writeFixtureJSON(t, filepath.Join(root, "manifest.json"), manifest)
	_, err := loadCompareArtifact(filepath.Join(root, "manifest.json"), CompareModeSmoke, "")
	if err == nil || !strings.Contains(err.Error(), "sampling plan") {
		t.Fatalf("mutable smoke sampling plan error = %v", err)
	}
}

func TestCanonicalSizeBundleRejectsCoherentFabricatedMetrics(t *testing.T) {
	root := t.TempDir()
	inputDir := filepath.Join(root, "size", "input", "primary")
	runtimeDir := filepath.Join(inputDir, "assets", "runtime")
	writeFixtureFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.js"), []byte("real-runtime"))
	buildJSON := []byte(`{"runtime":{"bootstrapRuntime":{"file":"bootstrap-runtime.js","hash":"manifest-hash","size":12}},"islands":[],"css":[]}`)
	exportJSON := []byte(`{"pages":["/lite"],"routes":[{"path":"/lite","file":"static/lite/index.html","capabilities":{"bootstrap":true}}]}`)
	writeFixtureFile(t, filepath.Join(inputDir, "build.json"), buildJSON)
	writeFixtureFile(t, filepath.Join(inputDir, "export.json"), exportJSON)
	writeFixtureFile(t, filepath.Join(inputDir, "static", "lite", "index.html"), []byte(`<script src="/gosx/bootstrap-runtime.js"></script>`))
	fabricatedSHA := strings.Repeat("f", 64)
	fabricatedID := stableAssetID("/gosx/bootstrap-runtime.js", fabricatedSHA)
	evidence := &SizeEvidence{
		Canonical:  true,
		BundleRoot: "size/input",
		BuildInput: BuildInputEvidence{
			ManifestSHA256: hashFixtureBytes(buildJSON),
			ExportSHA256:   hashFixtureBytes(exportJSON),
		},
		Assets: []TransferredAsset{{
			ID:           fabricatedID,
			URL:          "/gosx/bootstrap-runtime.js",
			SourcePath:   "primary/assets/runtime/bootstrap-runtime.js",
			EvidencePath: "size/input/primary/assets/runtime/bootstrap-runtime.js",
			ManifestHash: "manifest-hash",
			SHA256:       fabricatedSHA,
			Bytes:        1,
			GzipBytes:    1,
			BrotliBytes:  1,
			UsedByRoutes: []string{"/lite"},
		}},
		Routes: []RouteAssetEvidence{{
			ID:                "R01",
			Route:             "/lite",
			File:              "static/lite/index.html",
			Capabilities:      &ExportCapabilities{Bootstrap: true},
			AssetIDs:          []string{fabricatedID},
			RawBytes:          1,
			GzipBytes:         1,
			BrotliBytes:       1,
			UniqueRawBytes:    1,
			UniqueGzipBytes:   1,
			UniqueBrotliBytes: 1,
		}},
	}
	fillSizeEvidenceTotals(evidence)
	paths, err := resolveCompareManifestPath(writeMinimalCompareManifest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	err = validateCanonicalSizeEvidenceBundle(paths, evidence)
	if err == nil || !(strings.Contains(err.Error(), "build manifest hash/size") || strings.Contains(err.Error(), "metrics do not match bundled bytes")) {
		t.Fatalf("coherent fabricated metrics error = %v", err)
	}
}

func TestCanonicalSizeBundleRejectsManifestHashNotBoundToBytes(t *testing.T) {
	root := t.TempDir()
	inputDir := filepath.Join(root, "size", "input", "primary")
	runtimeBody := []byte("real-runtime")
	runtimeSHA := sha256.Sum256(runtimeBody)
	runtimeHex := hex.EncodeToString(runtimeSHA[:])
	runtimeDir := filepath.Join(inputDir, "assets", "runtime")
	writeFixtureFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.js"), runtimeBody)
	buildJSON := []byte(`{"runtime":{"bootstrapRuntime":{"file":"bootstrap-runtime.js","hash":"wrong-manifest-hash","size":12}},"islands":[],"css":[]}`)
	exportJSON := []byte(`{"pages":["/lite"],"routes":[{"path":"/lite","file":"static/lite/index.html","capabilities":{"bootstrap":true}}]}`)
	writeFixtureFile(t, filepath.Join(inputDir, "build.json"), buildJSON)
	writeFixtureFile(t, filepath.Join(inputDir, "export.json"), exportJSON)
	writeFixtureFile(t, filepath.Join(inputDir, "static", "lite", "index.html"), []byte(`<script src="/gosx/bootstrap-runtime.js"></script>`))
	id := stableAssetID("/gosx/bootstrap-runtime.js", runtimeHex)
	evidence := &SizeEvidence{
		Canonical:  true,
		BundleRoot: "size/input",
		BuildInput: BuildInputEvidence{ManifestSHA256: hashFixtureBytes(buildJSON), ExportSHA256: hashFixtureBytes(exportJSON)},
		Assets: []TransferredAsset{{
			ID: id, URL: "/gosx/bootstrap-runtime.js", SourcePath: "primary/assets/runtime/bootstrap-runtime.js",
			EvidencePath: "size/input/primary/assets/runtime/bootstrap-runtime.js", ManifestHash: "wrong-manifest-hash",
			SHA256: runtimeHex, Bytes: int64(len(runtimeBody)), GzipBytes: GzipLength(runtimeBody), BrotliBytes: BrotliLength(runtimeBody), UsedByRoutes: []string{"/lite"},
		}},
		Routes: []RouteAssetEvidence{{
			ID: "R01", Route: "/lite", File: "static/lite/index.html", Capabilities: &ExportCapabilities{Bootstrap: true}, AssetIDs: []string{id},
			RawBytes: int64(len(runtimeBody)), GzipBytes: GzipLength(runtimeBody), BrotliBytes: BrotliLength(runtimeBody),
			UniqueRawBytes: int64(len(runtimeBody)), UniqueGzipBytes: GzipLength(runtimeBody), UniqueBrotliBytes: BrotliLength(runtimeBody),
		}},
	}
	fillSizeEvidenceTotals(evidence)
	paths, err := resolveCompareManifestPath(writeMinimalCompareManifest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	err = validateCanonicalSizeEvidenceBundle(paths, evidence)
	if err == nil || !strings.Contains(err.Error(), "manifest hash does not bind bundled bytes") {
		t.Fatalf("unbound manifest hash error = %v", err)
	}
}

func TestCanonicalSizeBundleAcceptsManifestBoundInputs(t *testing.T) {
	root := t.TempDir()
	inputDir := filepath.Join(root, "size", "input", "primary")
	runtimeBody := []byte("real-runtime")
	runtimeSHA := sha256.Sum256(runtimeBody)
	runtimeHex := hex.EncodeToString(runtimeSHA[:])
	runtimeDir := filepath.Join(inputDir, "assets", "runtime")
	writeFixtureFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.js"), runtimeBody)
	buildJSON := []byte(fmt.Sprintf(`{"runtime":{"bootstrapRuntime":{"file":"bootstrap-runtime.js","hash":"%s","size":12}},"islands":[],"css":[]}`, runtimeHex[:16]))
	exportJSON := []byte(`{"pages":["/lite"],"routes":[{"path":"/lite","file":"static/lite/index.html","capabilities":{"bootstrap":true}}]}`)
	writeFixtureFile(t, filepath.Join(inputDir, "build.json"), buildJSON)
	writeFixtureFile(t, filepath.Join(inputDir, "export.json"), exportJSON)
	writeFixtureFile(t, filepath.Join(inputDir, "static", "lite", "index.html"), []byte(`<script src="/gosx/bootstrap-runtime.js"></script>`))
	id := stableAssetID("/gosx/bootstrap-runtime.js", runtimeHex)
	evidence := &SizeEvidence{
		Canonical:  true,
		BundleRoot: "size/input",
		BuildInput: BuildInputEvidence{ManifestSHA256: hashFixtureBytes(buildJSON), ExportSHA256: hashFixtureBytes(exportJSON)},
		Assets: []TransferredAsset{{
			ID: id, URL: "/gosx/bootstrap-runtime.js", SourcePath: "primary/assets/runtime/bootstrap-runtime.js",
			EvidencePath: "size/input/primary/assets/runtime/bootstrap-runtime.js", ManifestHash: runtimeHex[:16],
			SHA256: runtimeHex, Bytes: int64(len(runtimeBody)), GzipBytes: GzipLength(runtimeBody), BrotliBytes: BrotliLength(runtimeBody), UsedByRoutes: []string{"/lite"},
		}},
		Routes: []RouteAssetEvidence{{
			ID: "R01", Route: "/lite", File: "static/lite/index.html", Capabilities: &ExportCapabilities{Bootstrap: true}, AssetIDs: []string{id},
			RawBytes: int64(len(runtimeBody)), GzipBytes: GzipLength(runtimeBody), BrotliBytes: BrotliLength(runtimeBody),
			UniqueRawBytes: int64(len(runtimeBody)), UniqueGzipBytes: GzipLength(runtimeBody), UniqueBrotliBytes: BrotliLength(runtimeBody),
		}},
	}
	fillSizeEvidenceTotals(evidence)
	paths, err := resolveCompareManifestPath(writeMinimalCompareManifest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalSizeEvidenceBundle(paths, evidence); err != nil {
		t.Fatalf("manifest-bound canonical size bundle rejected: %v", err)
	}
}

func hashFixtureBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeFixtureFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMinimalCompareManifest(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "manifest.json")
	writeFixtureJSON(t, path, map[string]string{"artifact": "root"})
	return path
}

func validReconciledSizeEvidence() *SizeEvidence {
	sharedSHA := strings.Repeat("a", 64)
	uniqueSHA := strings.Repeat("b", 64)
	sharedID := stableAssetID("/gosx/bootstrap-runtime.js", sharedSHA)
	uniqueID := stableAssetID("/gosx/runtime.wasm", uniqueSHA)
	evidence := &SizeEvidence{
		Canonical:  true,
		BuildInput: BuildInputEvidence{ManifestSHA256: "sha256:" + strings.Repeat("c", 64)},
		Assets: []TransferredAsset{
			{
				ID:           sharedID,
				URL:          "/gosx/bootstrap-runtime.js",
				SHA256:       sharedSHA,
				Bytes:        100,
				GzipBytes:    70,
				BrotliBytes:  60,
				UsedByRoutes: []string{"/island/counter", "/lite"},
			},
			{
				ID:           uniqueID,
				URL:          "/gosx/runtime.wasm",
				SHA256:       uniqueSHA,
				Bytes:        50,
				GzipBytes:    40,
				BrotliBytes:  30,
				UsedByRoutes: []string{"/lite"},
			},
		},
		Routes: []RouteAssetEvidence{
			{ID: "R00", Route: "/static", Capabilities: &ExportCapabilities{}, AssetIDs: nil},
			{
				ID:                 "R01",
				Route:              "/lite",
				Capabilities:       &ExportCapabilities{Bootstrap: true},
				AssetIDs:           []string{sharedID, uniqueID},
				RawBytes:           150,
				GzipBytes:          110,
				BrotliBytes:        90,
				SharedRawBytes:     100,
				SharedGzipBytes:    70,
				SharedBrotliBytes:  60,
				UniqueRawBytes:     50,
				UniqueGzipBytes:    40,
				UniqueBrotliBytes:  30,
				AttributionComment: sharedRouteAttributionComment,
			},
			{
				ID:                 "R02",
				Route:              "/island/counter",
				Capabilities:       &ExportCapabilities{Islands: 1},
				AssetIDs:           []string{sharedID},
				RawBytes:           100,
				GzipBytes:          70,
				BrotliBytes:        60,
				SharedRawBytes:     100,
				SharedGzipBytes:    70,
				SharedBrotliBytes:  60,
				AttributionComment: sharedRouteAttributionComment,
			},
		},
	}
	fillSizeEvidenceTotals(evidence)
	return evidence
}

func TestComparePortableManifestSurvivesArtifactRelocation(t *testing.T) {
	parent := t.TempDir()
	original := writeCompareFixture(t, filepath.Join(parent, "captured"), compareFixtureOptions{
		manifestMutator: func(manifest *BrowserManifest) {
			manifest.ArtifactRoot = portableArtifactRoot
		},
	})
	moved := filepath.Join(parent, "committed", "baseline")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	report := runSmokeCompare(t, moved, moved)
	if report.Status != CompareStatusPass || report.ExitCode != 0 {
		t.Fatalf("relocated baseline status=%s exit=%d checks=%+v", report.Status, report.ExitCode, report.Checks)
	}
}

func TestCompareRejectsSelfCompareWhenUsedAsGate(t *testing.T) {
	root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{})
	report, err := CompareOuroborosArtifacts(CompareOptions{
		BaselineManifest:  filepath.Join(root, "manifest.json"),
		CandidateManifest: filepath.Join(root, "manifest.json"),
		BudgetPath:        compareBudgetPath(t),
		Mode:              CompareModeSmoke,
		GeneratedAt:       time.Unix(0, 0).UTC(),
		RejectSelfCompare: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExitCode != 1 || !reportHasCheck(report, "compat.baseline.distinct", "fail") {
		t.Fatalf("self-compare gate did not fail: exit=%d checks=%+v", report.ExitCode, report.Checks)
	}
}

func TestCompareVersionedBaselinePerturbationFails(t *testing.T) {
	baseline := writeCompareFixture(t, filepath.Join(t.TempDir(), "baseline"), compareFixtureOptions{sourceSuffix: "-baseline"})
	candidate := writeCompareFixture(t, filepath.Join(t.TempDir(), "candidate"), compareFixtureOptions{
		sourceSuffix: "-candidate",
		metricMutator: func(metrics map[string]float64) {
			metrics["dclMs"] = 1000
		},
	})
	report := runSmokeCompare(t, baseline, candidate)
	if report.ExitCode != 1 || !reportHasMetricFailure(report, "startup.dclMs") {
		t.Fatalf("deliberate DCL perturbation did not fail: exit=%d summary=%+v", report.ExitCode, report.Summary)
	}
}

func TestCompareSmokeSkipsUnavailableWASMMemoryTelemetry(t *testing.T) {
	root := writeCompareFixture(t, t.TempDir(), compareFixtureOptions{
		metricMutator: func(metrics map[string]float64) {
			delete(metrics, "wasmPages")
			delete(metrics, "wasmBytes")
		},
		manifestMutator: func(manifest *BrowserManifest) {
			manifest.Corpus.Routes[0].ExpectedTinyGoCurrent = "runtime"
		},
	})
	report := runSmokeCompare(t, root, root)
	if report.Status != CompareStatusPass || report.ExitCode != 0 {
		t.Fatalf("status=%s exit=%d checks=%+v", report.Status, report.ExitCode, report.Checks)
	}
	for _, metric := range []string{"memory.wasmPages", "memory.wasmBytes"} {
		if !reportHasMetricWarn(report, metric) {
			t.Fatalf("unavailable smoke metric %s did not emit warn skip", metric)
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
	artifactRoot := writeCompareFixture(t, filepath.Join(t.TempDir(), "browser"), compareFixtureOptions{})
	pixelRoot := filepath.Join(t.TempDir(), "pixel-evidence")
	addPixelRefToFixture(t, artifactRoot, "pixels/r00.json")
	var manifest BrowserManifest
	readFixtureJSON(t, filepath.Join(artifactRoot, "manifest.json"), &manifest)
	initial := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "r00-initial.png"), true)
	settled := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "r00-settled.png"), true)
	writeFixtureJSON(t, filepath.Join(pixelRoot, "pixels", "r00.json"), compareBaselinePixelManifest(manifest.Source, initial, settled))

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

func TestComparePortablePixelBundleSurvivesRelocation(t *testing.T) {
	artifactRoot := writeCompareFixture(t, filepath.Join(t.TempDir(), "browser"), compareFixtureOptions{})
	addPixelRefToFixture(t, artifactRoot, "pixels/r00.json")
	var browserManifest BrowserManifest
	readFixtureJSON(t, filepath.Join(artifactRoot, "manifest.json"), &browserManifest)

	parent := t.TempDir()
	original := filepath.Join(parent, "hardware-capture")
	initial := writeTestPNG(t, filepath.Join(original, "pixels", "r00-initial.png"), false)
	settled := writeTestPNG(t, filepath.Join(original, "pixels", "r00-settled.png"), false)
	writeFixtureJSON(t, filepath.Join(original, "pixels", "r00.json"), compareBaselinePixelManifest(browserManifest.Source, initial, settled))
	moved := filepath.Join(parent, "committed", "pixel-baseline")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}

	report, err := CompareOuroborosArtifacts(CompareOptions{
		BaselineManifest:  artifactRoot,
		CandidateManifest: artifactRoot,
		BudgetPath:        compareBudgetPath(t),
		Mode:              CompareModeSmoke,
		BaselinePixelRoot: moved,
		GeneratedAt:       time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExitCode != 0 || !reportHasMetricPass(report, "pixel.diffPct") {
		t.Fatalf("relocated pixel bundle failed: exit=%d checks=%+v ratchets=%+v", report.ExitCode, report.Checks, report.Ratchets)
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

func TestCompareBudgetRequiresGovernanceRules(t *testing.T) {
	for _, missing := range []string{
		budgetSourceJavaScriptLines,
		budgetSourceIncludedBytes,
		budgetGlobalCurrentCount,
		budgetGlobalAddedNames,
		budgetPixelDiffPct,
	} {
		t.Run(missing, func(t *testing.T) {
			budget := compareCanonicalBudget(t)
			delete(budget.Defaults, missing)
			path := filepath.Join(t.TempDir(), "budget.json")
			writeFixtureJSON(t, path, budget)
			if _, err := ReadCompareBudgetStrict(path); err == nil || !strings.Contains(err.Error(), "required governance policy") {
				t.Fatalf("missing policy error = %v", err)
			}
		})
	}

	t.Run("source policy must be monotonic", func(t *testing.T) {
		budget := compareCanonicalBudget(t)
		rule := budget.Defaults[budgetSourceJavaScriptLines]
		rule.AllowedAbs = 1
		budget.Defaults[budgetSourceJavaScriptLines] = rule
		path := filepath.Join(t.TempDir(), "budget.json")
		writeFixtureJSON(t, path, budget)
		if _, err := ReadCompareBudgetStrict(path); err == nil || !strings.Contains(err.Error(), "monotonic no-growth") {
			t.Fatalf("non-monotonic policy error = %v", err)
		}
	})

	t.Run("pixel policy retains hard maximum", func(t *testing.T) {
		budget := compareCanonicalBudget(t)
		rule := budget.Defaults[budgetPixelDiffPct]
		rule.AllowedAbs = visual.MaxCanonicalPixelThresholdPct + 0.01
		budget.Defaults[budgetPixelDiffPct] = rule
		path := filepath.Join(t.TempDir(), "budget.json")
		writeFixtureJSON(t, path, budget)
		if _, err := ReadCompareBudgetStrict(path); err == nil || !strings.Contains(err.Error(), "hard maximum") {
			t.Fatalf("pixel hard maximum error = %v", err)
		}
	})
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
		// Route reconciliation is a structural preflight that runs before bundle
		// containment. Exercise it directly so this test does not manufacture a
		// second, independently invalid uncontained receipt.
		err := validateSizeEvidenceForCompare(
			&SizeEvidence{Assets: []TransferredAsset{}, Routes: []RouteAssetEvidence{}},
			[]FixtureSpec{{ID: "R00", Route: "/"}},
		)
		if err == nil || !strings.Contains(err.Error(), "selected browser routes") {
			t.Fatalf("size route set mismatch error = %v", err)
		}
	})
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
		if report.ExitCode != 1 || !reportHasMessage(report, "manifest canonical") {
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
	t.Run("inventory bytes grow", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{inventoryBytes: 100})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{inventoryBytes: 101})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.source.includedBytes", "fail") {
			t.Fatalf("missing inventory byte ratchet: %+v", report.Summary)
		}
	})
	t.Run("source reductions pass monotonic budgets", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{inventoryLines: 101, inventoryBytes: 101})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{inventoryLines: 100, inventoryBytes: 100})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 0 || !reportHasRatchet(report, "ratchet.source.includedJavaScriptLines", "pass") || !reportHasRatchet(report, "ratchet.source.includedBytes", "pass") {
			t.Fatalf("source reduction did not pass: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
		}
	})
	t.Run("source equality passes monotonic budgets", func(t *testing.T) {
		root := writeCompareFixture(t, filepath.Join(t.TempDir(), "equal"), compareFixtureOptions{inventoryLines: 100, inventoryBytes: 100})
		report := runSmokeCompare(t, root, root)
		if report.ExitCode != 0 || !reportHasRatchet(report, "ratchet.source.includedJavaScriptLines", "pass") || !reportHasRatchet(report, "ratchet.source.includedBytes", "pass") {
			t.Fatalf("source equality did not pass: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
		}
	})
	t.Run("global growth", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			inventoryNameMutator: func(names []string) []string {
				return append(names, "__gosx_added_after_anchor")
			},
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.global.added", "fail") {
			t.Fatalf("missing global growth ratchet: %+v", report.Summary)
		}
	})
	t.Run("ambient replacement cannot evade equal count", func(t *testing.T) {
		base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
		cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{
			inventoryNameMutator: func(names []string) []string {
				return append(names[1:], "__gosx_replacement_name")
			},
		})
		report := runSmokeCompare(t, base, cand)
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.global.current.count", "pass") || !reportHasRatchet(report, "ratchet.global.added-names", "fail") || !reportHasMessage(report, "__gosx_replacement_name") {
			t.Fatalf("ambient replacement escaped set ratchet: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
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
}

func TestComparePixelThresholdPolicy(t *testing.T) {
	base := writeCompareFixture(t, filepath.Join(t.TempDir(), "base"), compareFixtureOptions{})
	cand := writeCompareFixture(t, filepath.Join(t.TempDir(), "cand"), compareFixtureOptions{})
	var baseManifest, candManifest BrowserManifest
	readFixtureJSON(t, filepath.Join(base, "manifest.json"), &baseManifest)
	readFixtureJSON(t, filepath.Join(cand, "manifest.json"), &candManifest)

	writeCandidate := func(t *testing.T, manifest visual.PixelEvidenceManifest) *CompareReport {
		t.Helper()
		return writeCandidateWithComparisonMutation(t, base, cand, baseManifest.Source, candManifest.Source, manifest, nil)
	}

	baseCandidate := func(threshold visual.PixelThresholdEvidence) visual.PixelEvidenceManifest {
		return visual.PixelEvidenceManifest{
			SchemaVersion:          visual.OuroborosPixelSchemaVersion,
			GeneratedAt:            "2026-08-09T00:00:00Z",
			RouteID:                "R00",
			Mode:                   string(visual.PixelModeCandidateComparison),
			BackendRequirement:     "webgpu",
			HardwareClassification: "headless-logic",
			Threshold:              threshold,
		}
	}

	t.Run("candidate may tighten", func(t *testing.T) {
		report := writeCandidate(t, baseCandidate(visual.PixelThresholdEvidence{BaselinePct: 1, RequestedPct: 0.5, EffectivePct: 0.5}))
		if report.ExitCode != 0 || !reportHasRatchet(report, "ratchet.pixel.0.threshold-policy", "pass") {
			t.Fatalf("candidate tightening did not pass: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
		}
	})
	t.Run("candidate may not loosen", func(t *testing.T) {
		report := writeCandidate(t, baseCandidate(visual.PixelThresholdEvidence{BaselinePct: 1, RequestedPct: 2, EffectivePct: 2}))
		if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.pixel.0.threshold-policy", "fail") {
			t.Fatalf("candidate loosening did not fail: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
		}
	})
	t.Run("stored passed contradiction fails", func(t *testing.T) {
		manifest := baseCandidate(visual.PixelThresholdEvidence{BaselinePct: 1, RequestedPct: 0.5, EffectivePct: 0.5})
		report := writeCandidateWithComparisonMutation(t, base, cand, baseManifest.Source, candManifest.Source, manifest, func(c *visual.PixelComparison) {
			c.Passed = false
		})
		if report.ExitCode != 1 || !reportHasMessage(report, "contradicts") {
			t.Fatalf("stored result contradiction did not fail: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
		}
	})
}

func TestCompareBaselinePixelThresholdMustEqualPolicy(t *testing.T) {
	artifactRoot := writeCompareFixture(t, filepath.Join(t.TempDir(), "browser"), compareFixtureOptions{})
	pixelRoot := filepath.Join(t.TempDir(), "pixel-evidence")
	addPixelRefToFixture(t, artifactRoot, "pixels/r00.json")
	var manifest BrowserManifest
	readFixtureJSON(t, filepath.Join(artifactRoot, "manifest.json"), &manifest)
	initial := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "r00-initial.png"), true)
	settled := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "r00-settled.png"), true)
	pixelManifest := compareBaselinePixelManifest(manifest.Source, initial, settled)
	pixelManifest.Threshold = visual.PixelThresholdEvidence{BaselinePct: 0.5, RequestedPct: 0.5, EffectivePct: 0.5}
	writeFixtureJSON(t, filepath.Join(pixelRoot, "pixels", "r00.json"), pixelManifest)

	report, err := CompareOuroborosArtifacts(CompareOptions{
		BaselineManifest:  filepath.Join(artifactRoot, "manifest.json"),
		CandidateManifest: filepath.Join(artifactRoot, "manifest.json"),
		BudgetPath:        compareBudgetPath(t),
		Mode:              CompareModeSmoke,
		BaselinePixelRoot: pixelRoot,
		GeneratedAt:       time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExitCode != 1 || !reportHasRatchet(report, "ratchet.pixel.0.threshold-policy", "fail") {
		t.Fatalf("baseline self-report escaped policy: exit=%d ratchets=%+v", report.ExitCode, report.Ratchets)
	}
}

func writeCandidateWithComparisonMutation(t *testing.T, baselineRoot, candidateRoot string, baseline, candidate SourceIdentity, manifest visual.PixelEvidenceManifest, mutate func(*visual.PixelComparison)) *CompareReport {
	t.Helper()
	pixelRoot := t.TempDir()
	initial := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "candidate-initial.png"), true)
	settled := writeTestPNG(t, filepath.Join(pixelRoot, "pixels", "candidate-settled.png"), true)
	manifest.Source = visual.PixelSourceIdentity{BaseRevision: candidate.BaseRevision, OverlayHash: candidate.OverlayHash, InventorySHA256: candidate.InventorySHA256}
	manifest.BaselineSource = &visual.PixelSourceIdentity{BaseRevision: baseline.BaseRevision, OverlayHash: baseline.OverlayHash, InventorySHA256: baseline.InventorySHA256}
	manifest.States = []visual.PixelStateEvidence{
		{State: "initial", Captures: []visual.PixelCaptureEvidence{initial}},
		{State: "settled", Captures: []visual.PixelCaptureEvidence{settled}},
	}
	for i := range manifest.States {
		comparison := &visual.PixelComparison{
			DiffPct:               0.25,
			DimensionsMatch:       true,
			BaselineThresholdPct:  manifest.Threshold.BaselinePct,
			EffectiveThresholdPct: manifest.Threshold.EffectivePct,
			Passed:                true,
		}
		if mutate != nil {
			mutate(comparison)
		}
		manifest.States[i].Captures[0].Comparison = comparison
	}
	path := filepath.Join(pixelRoot, "candidate.json")
	writeFixtureJSON(t, path, manifest)
	report, err := CompareOuroborosArtifacts(CompareOptions{
		BaselineManifest:       filepath.Join(baselineRoot, "manifest.json"),
		CandidateManifest:      filepath.Join(candidateRoot, "manifest.json"),
		BudgetPath:             compareBudgetPath(t),
		Mode:                   CompareModeSmoke,
		CandidatePixelManifest: []string{path},
		GeneratedAt:            time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestCompareNoisyMetricWithoutRerunProofIsInconclusive(t *testing.T) {
	root := writeNoisyFixture(t, t.TempDir())
	report := runSmokeCompare(t, root, root)
	if report.Status != CompareStatusInconclusive || report.ExitCode != 2 {
		t.Fatalf("status=%s exit=%d summary=%+v", report.Status, report.ExitCode, report.Summary)
	}
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
	source = writeFixtureInventory(t, root, source, inventoryLines, opts.inventoryBytes, opts.inventoryNameMutator)
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
		Sampling:    mustSamplingPlan(t, "smoke"),
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

func mustSamplingPlan(t *testing.T, name string) SamplingPlan {
	t.Helper()
	plan, err := samplingPlan(name)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func writeNoisyFixture(t *testing.T, root string) string {
	t.Helper()
	source := compareSource("")
	source = writeFixtureInventory(t, root, source, 100, 0, nil)
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
		Sampling:      mustSamplingPlan(t, "smoke"),
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
			SchemaVersion: compatibilityAuditSchemaVersion,
			Status:        "pass",
			Receipt:       CompatibilityNameSetSummary{Count: canonicalGosx, NameSetHash: compatibilityReceiptHash},
			Anchor:        CompatibilityNameSetSummary{Count: canonicalGosx, NameSetHash: "sha256:anchor"},
			Current:       CompatibilityNameSetSummary{Count: canonicalGosx, NameSetHash: "sha256:current"},
		},
	}
}

func writeFixtureInventory(t *testing.T, root string, source SourceIdentity, includedLines int, includedBytes int64, nameMutator func([]string) []string) SourceIdentity {
	t.Helper()
	receipt, err := loadCompatibilityReceipt()
	if err != nil {
		t.Fatal(err)
	}
	receiptSet := compatibilityReceiptEvidenceFromNames(receipt.Names, CompatibilitySourceIdentity{Kind: "pinned-receipt", ArtifactPath: "perf/ouroboros/compatibility_receipt.v1.json"})
	anchorSet := compatibilityEvidenceFromNamesWithEvidenceAndScope(receipt.Names, CompatibilitySourceIdentity{Kind: "clean-anchor", Revision: source.BaseRevision, OverlayHash: OverlayClean}, compatibilityFullRuntimeScope, nil)
	currentNames := append([]string{}, receipt.Names...)
	if nameMutator != nil {
		currentNames = nameMutator(currentNames)
		sort.Strings(currentNames)
	}
	currentSet := compatibilityEvidenceFromNamesWithEvidenceAndScope(currentNames, CompatibilitySourceIdentity{Kind: "current-overlay", Revision: source.BaseRevision, OverlayHash: source.OverlayHash}, compatibilityFullRuntimeScope, nil)
	for _, set := range []*CompatibilityNameSetEvidence{&anchorSet, &currentSet} {
		set.RuntimeJSONSourceIdentityHash = "sha256:source"
		set.RuntimeJSONSemanticHash = "sha256:semantic"
		set.RuntimeJSONCountsHash = "sha256:counts"
		set.RuntimeJSONGlobalNameHash = RuntimeJSONStaticGlobalNameHash(set.Names)
		set.EvidenceHash = compatibilityEvidenceHash(*set)
	}
	if includedBytes == 0 {
		includedBytes = 10
	}
	included := SourceFile{Path: "client/js/bootstrap-src/fixture.js", Language: "javascript", SourceKind: "bootstrap", Lines: includedLines, Bytes: includedBytes, GzipBytes: 10, BrotliBytes: 10, ParseOK: true}
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
		Totals:        Totals{IncludedFiles: 1, IncludedJavaScriptLines: includedLines, IncludedBytes: includedBytes, IncludedGzipBytes: 10, IncludedBrotliBytes: 10, ByExtension: map[string]int{".js": 1}, RuntimeSemanticGate: "cmd/buildbootstrap + make test-runtime-types", RuntimeAmbientFacade: "client/runtime/host/compatibility.ts"},
		Structural:    Structural{Gotreesitter: ParseSummary{Language: "javascript", Parsed: 1}, ImportsExports: []Location{}, FreeGlobalReads: []string{}, FreeGlobalWrites: []string{}},
		Drift:         DriftReport{Status: "pass"},
		Surface: Surface{
			GosxNames:               []GosxName{},
			BroaderBrowserGosxNames: []GosxName{},
			SerializationSites:      []SerializationSite{},
			CompatibilityAudit: CompatibilityAudit{
				SchemaVersion:      compatibilityAuditSchemaVersion,
				Status:             passFailCompatibilitySets(anchorSet.Names, currentSet.Names),
				CanonicalAvailable: equalStrings(anchorSet.Names, currentSet.Names),
				Receipt:            receiptSet,
				Anchor:             anchorSet,
				Current:            currentSet,
				Reconciliation: CompatibilityReconciliation{
					RecoveredPreexisting: differenceStrings(anchorSet.Names, receiptSet.Names),
					AddedSinceAnchor:     differenceStrings(currentSet.Names, anchorSet.Names),
					RemovedSinceAnchor:   differenceStrings(anchorSet.Names, currentSet.Names),
					MissingFromAnchor:    differenceStrings(receiptSet.Names, anchorSet.Names),
				},
			},
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
				{ID: "runtime", Generation: "current", Status: "measured", SizeBytes: int64Pointer(1564711), BudgetBytes: int64Pointer(1564711), SizeArtifact: artifactRef("size", "runtime"), WASMArtifact: artifactRef("wasm", "runtime"), SelectedByRoutes: []string{}},
				{ID: "islands", Generation: "current", Status: "measured", SizeBytes: int64Pointer(801474), BudgetBytes: int64Pointer(801474), SizeArtifact: artifactRef("size", "islands"), WASMArtifact: artifactRef("wasm", "islands"), SelectedByRoutes: []string{}},
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
	source.CompatibilityAudit = compatibilityAuditIdentityFromInventory(inv.Surface.CompatibilityAudit)
	if source.RuntimeJSONStatic != nil {
		binding := DynamicSourceBindingFromSourceIdentity(source)
		source.RuntimeJSONStatic.SourceIdentityHash = runtimeJSONDynamicSourceBindingHash(binding)
		source.RuntimeJSONStatic.SemanticHash = "sha256:semantic"
		source.RuntimeJSONStatic.CountsHash = "sha256:counts"
		source.RuntimeJSONStatic.GlobalNameHash = RuntimeJSONStaticGlobalNameHash([]string{"__gosx_canvas_event"})
	}
	return source
}

func passFailCompatibilitySets(anchor, current []string) string {
	if equalStrings(anchor, current) {
		return "pass"
	}
	return "fail-closed"
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
	return &RuntimeJSONRawDrain{SchemaVersion: RuntimeJSONProbeSchemaVersion, FacadeSchemaVersion: 1, Version: "1", RouteID: routeID, KnownGlobals: knownGlobals, WrappedGlobals: knownGlobals, Limits: RuntimeJSONRawDrainLimits{EventLimit: 100}}
}

func dynamicProductEvent(routeID string) ProbeEvent {
	if routeID == "R05" {
		return ProbeEvent{Kind: "runtime-call", Phase: "input", Name: "__gosx_canvas_event", Detail: map[string]any{"eventKind": 3, "argCount": 3, "source": map[string]any{"path": "prod/" + routeID + ".js", "line": 1, "column": 1}}}
	}
	return ProbeEvent{Kind: "json-call", Phase: "input", Name: "JSON.stringify", Detail: map[string]any{"source": map[string]any{"path": "prod/" + routeID + ".js", "line": 1, "column": 1}}}
}

func comparePixelManifest(passed bool, baseline, candidate SourceIdentity, initial, settled visual.PixelCaptureEvidence) visual.PixelEvidenceManifest {
	initial.Comparison = &visual.PixelComparison{DiffPct: 2, BaselineThresholdPct: 1, EffectiveThresholdPct: 1, DimensionsMatch: true, Passed: passed}
	settled.Comparison = &visual.PixelComparison{DiffPct: 2, BaselineThresholdPct: 1, EffectiveThresholdPct: 1, DimensionsMatch: true, Passed: passed}
	return visual.PixelEvidenceManifest{
		SchemaVersion:          visual.OuroborosPixelSchemaVersion,
		GeneratedAt:            "2026-08-09T00:00:00Z",
		RouteID:                "R00",
		Mode:                   string(visual.PixelModeCandidateComparison),
		Source:                 visual.PixelSourceIdentity{BaseRevision: candidate.BaseRevision, OverlayHash: candidate.OverlayHash, InventorySHA256: candidate.InventorySHA256},
		BaselineSource:         &visual.PixelSourceIdentity{BaseRevision: baseline.BaseRevision, OverlayHash: baseline.OverlayHash, InventorySHA256: baseline.InventorySHA256},
		BackendRequirement:     "webgpu",
		HardwareClassification: "headless-logic",
		Threshold:              visual.PixelThresholdEvidence{BaselinePct: 1, RequestedPct: 1, EffectivePct: 1},
		States: []visual.PixelStateEvidence{
			{State: "initial", Captures: []visual.PixelCaptureEvidence{initial}},
			{State: "settled", Captures: []visual.PixelCaptureEvidence{settled}},
		},
	}
}

func compareBaselinePixelManifest(source SourceIdentity, initial, settled visual.PixelCaptureEvidence) visual.PixelEvidenceManifest {
	return visual.PixelEvidenceManifest{
		SchemaVersion:          visual.OuroborosPixelSchemaVersion,
		GeneratedAt:            "2026-08-09T00:00:00Z",
		RouteID:                "R00",
		Mode:                   string(visual.PixelModeRecordBaseline),
		Source:                 visual.PixelSourceIdentity{BaseRevision: source.BaseRevision, OverlayHash: source.OverlayHash, InventorySHA256: source.InventorySHA256},
		BackendRequirement:     "webgpu",
		Certified:              true,
		HardwareClassification: "headless-logic",
		Threshold:              visual.PixelThresholdEvidence{BaselinePct: 1, RequestedPct: 1, EffectivePct: 1},
		States: []visual.PixelStateEvidence{
			{State: "initial", Captures: []visual.PixelCaptureEvidence{initial}},
			{State: "settled", Captures: []visual.PixelCaptureEvidence{settled}},
		},
	}
}

func writeTestPNG(t *testing.T, path string, producerStyle bool) visual.PixelCaptureEvidence {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{B: 255, A: 255})
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
	return visual.PixelCaptureEvidence{Index: 0, Path: capturePath, SHA256: hashValue, Bytes: buf.Len(), Width: 2, Height: 1}
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

func compareBudgetPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("budgets.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func compareCanonicalBudget(t *testing.T) CompareBudget {
	t.Helper()
	budget, err := ReadCompareBudgetStrict(compareBudgetPath(t))
	if err != nil {
		t.Fatal(err)
	}
	return *budget
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
