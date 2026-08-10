package ouroboros

import (
	"bufio"
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
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx/visual"
)

const CompareSchemaVersion = "gosx.ouroboros.compare.v1"

const (
	CompareModeCanonical = "canonical"
	CompareModeSmoke     = "smoke"
)

const (
	CompareStatusPass         = "pass"
	CompareStatusFail         = "fail"
	CompareStatusInconclusive = "inconclusive"
)

type CompareOptions struct {
	BaselineManifest       string
	CandidateManifest      string
	BudgetPath             string
	Mode                   string
	OutPath                string
	BaselinePixelRoot      string
	CandidatePixelRoot     string
	CandidatePixelManifest []string
	GeneratedAt            time.Time
}

type CompareReport struct {
	SchemaVersion   string                  `json:"schemaVersion"`
	ContractVersion string                  `json:"contractVersion"`
	GeneratedAt     string                  `json:"generatedAt"`
	Mode            string                  `json:"mode"`
	Status          string                  `json:"status"`
	ExitCode        int                     `json:"exitCode"`
	SelfCompare     bool                    `json:"selfCompare"`
	Baseline        CompareArtifactEndpoint `json:"baseline"`
	Candidate       CompareArtifactEndpoint `json:"candidate"`
	Compatibility   CompareCompatibility    `json:"compatibility"`
	Checks          []CompareCheck          `json:"checks"`
	Ratchets        []CompareCheck          `json:"ratchets"`
	Summary         CompareSummary          `json:"summary"`
}

type CompareArtifactEndpoint struct {
	ManifestPath               string         `json:"manifestPath"`
	ArtifactRoot               string         `json:"artifactRoot"`
	ManifestSHA256             string         `json:"manifestSHA256"`
	RawSamplesSHA256           string         `json:"rawSamplesSHA256,omitempty"`
	SummarySHA256              string         `json:"summarySHA256,omitempty"`
	EnvironmentSHA256          string         `json:"environmentSHA256,omitempty"`
	SizeEvidenceSHA256         string         `json:"sizeEvidenceSHA256,omitempty"`
	RuntimeArtifactsSHA256     string         `json:"runtimeArtifactsSHA256,omitempty"`
	DynamicEvidenceSHA256      string         `json:"dynamicEvidenceSHA256,omitempty"`
	Source                     SourceIdentity `json:"source"`
	Sampling                   SamplingPlan   `json:"sampling"`
	EnvironmentClass           string         `json:"environmentClass"`
	HardwareClassification     string         `json:"hardwareClassification"`
	Canonical                  bool           `json:"canonical"`
	BrowserConnectionMode      string         `json:"browserConnectionMode,omitempty"`
	RemoteCDPEndpointHash      string         `json:"remoteCDPEndpointHash,omitempty"`
	RuntimeJSONScannerVersion  string         `json:"runtimeJSONScannerVersion,omitempty"`
	RuntimeJSONPhaseClassifier string         `json:"runtimeJSONPhaseClassifier,omitempty"`
}

type CompareCompatibility struct {
	SchemaCompatible       bool     `json:"schemaCompatible"`
	SourceCompatible       bool     `json:"sourceCompatible"`
	EnvironmentCompatible  bool     `json:"environmentCompatible"`
	SampleMatrixCompatible bool     `json:"sampleMatrixCompatible"`
	ArtifactsCompatible    bool     `json:"artifactsCompatible"`
	Messages               []string `json:"messages,omitempty"`
}

type CompareCheck struct {
	ID                  string  `json:"id"`
	Category            string  `json:"category"`
	RouteID             string  `json:"routeID,omitempty"`
	CacheMode           string  `json:"cacheMode,omitempty"`
	Metric              string  `json:"metric,omitempty"`
	Stat                string  `json:"stat,omitempty"`
	Direction           string  `json:"direction,omitempty"`
	Baseline            float64 `json:"baseline,omitempty"`
	Candidate           float64 `json:"candidate,omitempty"`
	DeltaAbs            float64 `json:"deltaAbs,omitempty"`
	DeltaPct            float64 `json:"deltaPct,omitempty"`
	AllowedAbs          float64 `json:"allowedAbs,omitempty"`
	AllowedPct          float64 `json:"allowedPct,omitempty"`
	ThresholdFormula    string  `json:"thresholdFormula,omitempty"`
	BaselineUnstable    bool    `json:"baselineUnstable"`
	CandidateUnstable   bool    `json:"candidateUnstable"`
	Status              string  `json:"status"`
	Message             string  `json:"message,omitempty"`
	RequiresHardware    bool    `json:"requiresHardware,omitempty"`
	PrerequisiteMissing bool    `json:"prerequisiteMissing,omitempty"`
}

type CompareSummary struct {
	CheckCount        int `json:"checkCount"`
	PassCount         int `json:"passCount"`
	FailCount         int `json:"failCount"`
	WarnCount         int `json:"warnCount"`
	BlockedCount      int `json:"blockedCount"`
	UnstableCount     int `json:"unstableCount"`
	InconclusiveCount int `json:"inconclusiveCount"`
}

type CompareBudget struct {
	SchemaVersion string                     `json:"schemaVersion"`
	Contract      string                     `json:"contractVersion"`
	Defaults      map[string]BudgetThreshold `json:"defaults"`
	Routes        map[string]BudgetThreshold `json:"routes,omitempty"`
}

type BudgetThreshold struct {
	AllowedAbs       float64 `json:"allowedAbs"`
	AllowedPct       float64 `json:"allowedPct"`
	Direction        string  `json:"direction,omitempty"`
	Stat             string  `json:"stat,omitempty"`
	RequiresHardware bool    `json:"requiresHardware,omitempty"`
	Exact            bool    `json:"exact,omitempty"`
}

type compareLoadedArtifact struct {
	endpoint        CompareArtifactEndpoint
	manifest        BrowserManifest
	raw             []BrowserRawSample
	summary         BrowserSummary
	environment     EnvironmentReport
	size            *SizeEvidence
	runtime         *RuntimeBuildEvidence
	dynamic         *RuntimeJSONDynamicEvidenceManifest
	inventory       *Inventory
	pixels          []visual.PixelEvidenceManifest
	pixelRefs       map[string]string
	metricStats     map[metricBucket]map[string]Stats
	runtimeTransfer map[metricBucket]float64
	consoleCount    map[metricBucket]float64
}

type metricBucket struct {
	RouteID   string
	CacheMode string
}

type comparePathSet struct {
	manifestPath string
	root         string
	rootReal     string
}

var compareRequiredMetricIDs = []string{
	"browser.transferBytes",
	"runtime.transferBytes",
	"startup.ttfbMs",
	"startup.dclMs",
	"startup.fullyLoadedMs",
	"startup.firstUsableMs",
	"hydration.totalMs",
	"hydration.maxIslandMs",
	"interaction.dispatchMs",
	"interaction.patchCount",
	"trace.evaluateScriptMs",
	"trace.compileScriptMs",
	"trace.v8CompileMs",
	"trace.v8ParseBackgroundMs",
	"trace.wasmCompileMs",
	"trace.wasmInstantiateMs",
	"memory.jsHeapUsedMb",
	"memory.jsHeapTotalMb",
	"memory.domNodeCount",
	"memory.listenerCount",
	"memory.wasmPages",
	"memory.wasmBytes",
	"scene.firstFrameMs",
	"scene.cpuSubmitP95Ms",
	"scene.rafP95Ms",
	"scene.missedVsyncEstimate",
	"scene.gpuTotalP95Ms",
	"console.entryCount",
}

var compareSHA256Re = regexp.MustCompile(`^(sha256:)?[a-f0-9]{64}$`)

const (
	maxCompareRawSamples        = 10000
	maxCompareRawJSONLBytes     = 256 << 20
	maxCompareHashFileBytes     = 512 << 20
	maxComparePixelManifestRefs = 256
	maxComparePNGBytes          = 64 << 20
	maxComparePNGDimension      = 16384
	maxComparePNGDecodedPixels  = 64_000_000
)

func CompareOuroborosArtifacts(opts CompareOptions) (*CompareReport, error) {
	if strings.TrimSpace(opts.Mode) == "" {
		opts.Mode = CompareModeCanonical
	}
	if opts.Mode != CompareModeCanonical && opts.Mode != CompareModeSmoke {
		return nil, fmt.Errorf("unknown ouroboros compare mode %q", opts.Mode)
	}
	if strings.TrimSpace(opts.BudgetPath) == "" {
		return nil, fmt.Errorf("budget path is required")
	}
	budget, err := ReadCompareBudgetStrict(opts.BudgetPath)
	if err != nil {
		return nil, err
	}
	if budget.SchemaVersion != "gosx.ouroboros.compare-budget.v1" || budget.Contract != ContractO02 {
		return nil, fmt.Errorf("bad budget identity: schema=%q contract=%q", budget.SchemaVersion, budget.Contract)
	}
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	report := &CompareReport{
		SchemaVersion:   CompareSchemaVersion,
		ContractVersion: ContractO02,
		GeneratedAt:     generatedAt.UTC().Format(time.RFC3339),
		Mode:            opts.Mode,
		Status:          CompareStatusPass,
		ExitCode:        0,
	}
	if strings.TrimSpace(opts.OutPath) != "" {
		if _, err := os.Stat(opts.OutPath); err == nil {
			return nil, fmt.Errorf("compare refuses to overwrite existing output: %s", opts.OutPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if opts.CandidatePixelRoot == "" && opts.BaselinePixelRoot != "" && samePathInput(opts.BaselineManifest, opts.CandidateManifest) {
		opts.CandidatePixelRoot = opts.BaselinePixelRoot
	}
	baseline, err := loadCompareArtifact(opts.BaselineManifest, opts.Mode, opts.BaselinePixelRoot)
	if err != nil {
		report.addCheck("artifact.baseline.load", "artifact", "fail", err.Error())
		finalizeCompareReport(report)
		return report, nil
	}
	candidate, err := loadCompareArtifact(opts.CandidateManifest, opts.Mode, opts.CandidatePixelRoot)
	if err != nil {
		report.Baseline = baseline.endpoint
		report.addCheck("artifact.candidate.load", "artifact", "fail", err.Error())
		finalizeCompareReport(report)
		return report, nil
	}
	report.Baseline = baseline.endpoint
	report.Candidate = candidate.endpoint
	report.SelfCompare = samePath(baseline.endpoint.ManifestPath, candidate.endpoint.ManifestPath) || endpointHash(baseline.endpoint) == endpointHash(candidate.endpoint)

	runCompatibilityChecks(report, baseline, candidate, opts)
	runMetricChecks(report, baseline, candidate, budget, opts)
	runRatchetChecks(report, baseline, candidate, budget, opts)
	runPixelChecks(report, baseline, candidate, opts)
	finalizeCompareReport(report)
	if opts.OutPath != "" {
		if err := writeCompareReport(opts.OutPath, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func ReadBrowserManifestStrict(path string) (*BrowserManifest, error) {
	var out BrowserManifest
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read browser manifest: %w", err)
	}
	return &out, nil
}

func ReadBrowserRawSamplesJSONLStrict(path string) ([]BrowserRawSample, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read raw samples: %w", err)
	}
	if info.Size() > maxCompareRawJSONLBytes {
		return nil, fmt.Errorf("raw samples JSONL is too large: %d bytes > %d", info.Size(), maxCompareRawJSONLBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read raw samples: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	var samples []BrowserRawSample
	seen := map[string]bool{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("raw samples line %d is empty", lineNo)
		}
		var sample BrowserRawSample
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&sample); err != nil {
			return nil, fmt.Errorf("raw samples line %d: %w", lineNo, err)
		}
		if err := rejectTrailingJSON(dec); err != nil {
			return nil, fmt.Errorf("raw samples line %d: %w", lineNo, err)
		}
		if sample.SchemaVersion != BrowserBaselineSchemaVersion {
			return nil, fmt.Errorf("raw samples line %d schemaVersion = %q, want %q", lineNo, sample.SchemaVersion, BrowserBaselineSchemaVersion)
		}
		key := strings.Join([]string{string(sample.SampleLane), sample.RouteID, sample.CacheMode, strconv.Itoa(sample.SampleIndex)}, "\x00")
		if seen[key] {
			return nil, fmt.Errorf("duplicate raw sample tuple at line %d: lane=%s routeID=%s cacheMode=%s sampleIndex=%d", lineNo, sample.SampleLane, sample.RouteID, sample.CacheMode, sample.SampleIndex)
		}
		seen[key] = true
		samples = append(samples, sample)
		if len(samples) > maxCompareRawSamples {
			return nil, fmt.Errorf("raw sample count exceeds limit %d", maxCompareRawSamples)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return samples, nil
}

func ReadBrowserSummaryStrict(path string) (*BrowserSummary, error) {
	var out BrowserSummary
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read browser summary: %w", err)
	}
	return &out, nil
}

func ReadEnvironmentReportStrict(path string) (*EnvironmentReport, error) {
	var out EnvironmentReport
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read environment report: %w", err)
	}
	return &out, nil
}

func ReadSizeEvidenceStrict(path string) (*SizeEvidence, error) {
	var out SizeEvidence
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read size evidence: %w", err)
	}
	return &out, nil
}

func ReadRuntimeBuildEvidenceStrict(path string) (*RuntimeBuildEvidence, error) {
	var out RuntimeBuildEvidence
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read runtime build evidence: %w", err)
	}
	return &out, nil
}

func ReadRuntimeJSONDynamicEvidenceStrict(path string) (*RuntimeJSONDynamicEvidenceManifest, error) {
	var out RuntimeJSONDynamicEvidenceManifest
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read runtime JSON dynamic evidence: %w", err)
	}
	if err := ValidateRuntimeJSONDynamicEvidenceManifest(&out); err != nil {
		return nil, fmt.Errorf("validate runtime JSON dynamic evidence: %w", err)
	}
	return &out, nil
}

func ReadCompareBudgetStrict(path string) (*CompareBudget, error) {
	var out CompareBudget
	if err := readJSONStrictFile(path, &out); err != nil {
		return nil, fmt.Errorf("read compare budget: %w", err)
	}
	if err := validateCompareBudget(out); err != nil {
		return nil, err
	}
	return &out, nil
}

func validateCompareBudget(budget CompareBudget) error {
	if budget.SchemaVersion != "gosx.ouroboros.compare-budget.v1" {
		return fmt.Errorf("budget schemaVersion = %q, want gosx.ouroboros.compare-budget.v1", budget.SchemaVersion)
	}
	if budget.Contract != ContractO02 {
		return fmt.Errorf("budget contractVersion = %q, want %q", budget.Contract, ContractO02)
	}
	for scope, thresholds := range map[string]map[string]BudgetThreshold{
		"defaults": budget.Defaults,
		"routes":   budget.Routes,
	} {
		for id, threshold := range thresholds {
			direction := threshold.Direction
			if direction == "" {
				direction = "lower"
			}
			if direction != "lower" {
				return fmt.Errorf("budget %s.%s has unsupported direction %q", scope, id, threshold.Direction)
			}
			if !finiteNonNegative(threshold.AllowedAbs) || !finiteNonNegative(threshold.AllowedPct) {
				return fmt.Errorf("budget %s.%s has non-finite or negative threshold", scope, id)
			}
			stat := threshold.Stat
			if stat == "" {
				stat = "median"
			}
			if stat != "median" && stat != "value" {
				return fmt.Errorf("budget %s.%s has unsupported stat %q", scope, id, threshold.Stat)
			}
		}
	}
	return nil
}

func readJSONStrictFile(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	return rejectTrailingJSON(dec)
}

func rejectTrailingJSON(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("trailing JSON data")
}

func loadCompareArtifact(input, mode, pixelRoot string) (*compareLoadedArtifact, error) {
	paths, err := resolveCompareManifestPath(input)
	if err != nil {
		return nil, err
	}
	manifest, err := ReadBrowserManifestStrict(paths.manifestPath)
	if err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != BrowserBaselineSchemaVersion {
		return nil, fmt.Errorf("manifest schemaVersion = %q, want %q", manifest.SchemaVersion, BrowserBaselineSchemaVersion)
	}
	if manifest.Contract != ContractO02 {
		return nil, fmt.Errorf("manifest contractVersion = %q, want %q", manifest.Contract, ContractO02)
	}
	if manifestRoot, err := filepath.EvalSymlinks(manifest.ArtifactRoot); err != nil {
		return nil, fmt.Errorf("manifest artifactRoot %q cannot be resolved: %w", manifest.ArtifactRoot, err)
	} else if manifestRoot != paths.root {
		return nil, fmt.Errorf("manifest artifactRoot resolves to %s, want %s", manifestRoot, paths.root)
	}
	rawPath, err := resolveArtifactRef(paths, manifest.RawSamples)
	if err != nil {
		return nil, fmt.Errorf("rawSamplesRef: %w", err)
	}
	summaryPath, err := resolveArtifactRef(paths, manifest.Summary)
	if err != nil {
		return nil, fmt.Errorf("summaryRef: %w", err)
	}
	envPath, err := resolveArtifactRef(paths, manifest.Environment)
	if err != nil {
		return nil, fmt.Errorf("environmentRef: %w", err)
	}
	raw, err := ReadBrowserRawSamplesJSONLStrict(rawPath)
	if err != nil {
		return nil, err
	}
	for i, sample := range raw {
		if sample.Contract != ContractO02 {
			return nil, fmt.Errorf("raw sample %d contractVersion = %q, want %q", i, sample.Contract, ContractO02)
		}
		if !sourceCoreEqual(sample.Source, manifest.Source) {
			return nil, fmt.Errorf("raw sample %d source identity does not match manifest source", i)
		}
		if err := validateCompareRawSample(sample, manifest.Sampling.Canonical); err != nil {
			return nil, fmt.Errorf("raw sample %d %s/%s/%s/%d: %w", i, sample.SampleLane, sample.RouteID, sample.CacheMode, sample.SampleIndex, err)
		}
	}
	summary, err := ReadBrowserSummaryStrict(summaryPath)
	if err != nil {
		return nil, err
	}
	if summary.SchemaVersion != BrowserBaselineSchemaVersion {
		return nil, fmt.Errorf("summary schemaVersion = %q, want %q", summary.SchemaVersion, BrowserBaselineSchemaVersion)
	}
	env, err := ReadEnvironmentReportStrict(envPath)
	if err != nil {
		return nil, err
	}
	if env.SchemaVersion != BrowserBaselineSchemaVersion && env.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("environment schemaVersion = %q, want %q", env.SchemaVersion, BrowserBaselineSchemaVersion)
	}
	if err := requireSummaryParity(*summary, raw, manifest.Sampling.Name, manifest.Source); err != nil {
		return nil, err
	}
	inventory, err := loadSourceInventory(paths, manifest.Source)
	if err != nil {
		return nil, err
	}
	size, sizeHash, err := loadOptionalSizeEvidence(paths, manifest.Source, manifest.Corpus.Routes, mode)
	if err != nil {
		return nil, err
	}
	runtime, runtimeHash, err := loadOptionalRuntimeEvidence(paths, manifest.Source, mode)
	if err != nil {
		return nil, err
	}
	dynamic, dynamicHash, err := loadOptionalDynamicEvidence(paths, manifest, mode)
	if err != nil {
		return nil, err
	}
	pixels, pixelRefs, err := loadManifestPixelRefs(paths, raw, mode, pixelRoot)
	if err != nil {
		return nil, err
	}
	manifestHash, _ := sha256File(paths.manifestPath)
	rawHash, _ := sha256File(rawPath)
	summaryHash, _ := sha256File(summaryPath)
	envHash, _ := sha256File(envPath)
	endpoint := CompareArtifactEndpoint{
		ManifestPath:               paths.manifestPath,
		ArtifactRoot:               paths.root,
		ManifestSHA256:             manifestHash,
		RawSamplesSHA256:           rawHash,
		SummarySHA256:              summaryHash,
		EnvironmentSHA256:          envHash,
		SizeEvidenceSHA256:         sizeHash,
		RuntimeArtifactsSHA256:     runtimeHash,
		DynamicEvidenceSHA256:      dynamicHash,
		Source:                     manifest.Source,
		Sampling:                   manifest.Sampling,
		EnvironmentClass:           env.EnvironmentClass,
		HardwareClassification:     env.HardwareClassification,
		Canonical:                  manifest.Canonical,
		BrowserConnectionMode:      compareStringFromAny(env.Browser["connectionMode"]),
		RemoteCDPEndpointHash:      compareStringFromAny(env.Browser["remoteEndpointSHA256"]),
		RuntimeJSONScannerVersion:  runtimeScannerVersion(manifest.Source),
		RuntimeJSONPhaseClassifier: runtimePhaseClassifier(manifest.Source),
	}
	loaded := &compareLoadedArtifact{
		endpoint:        endpoint,
		manifest:        *manifest,
		raw:             raw,
		summary:         *summary,
		environment:     *env,
		size:            size,
		runtime:         runtime,
		dynamic:         dynamic,
		inventory:       inventory,
		pixels:          pixels,
		pixelRefs:       pixelRefs,
		metricStats:     buildMetricStats(raw),
		runtimeTransfer: runtimeTransferByBucket(raw),
		consoleCount:    consoleCountByBucket(raw),
	}
	return loaded, nil
}

func loadSourceInventory(paths comparePathSet, source SourceIdentity) (*Inventory, error) {
	if strings.TrimSpace(source.InventoryRef) == "" {
		return nil, fmt.Errorf("source inventoryRef is required")
	}
	path, err := resolveArtifactRef(paths, source.InventoryRef)
	if err != nil {
		return nil, fmt.Errorf("source inventoryRef: %w", err)
	}
	hash, err := sha256File(path)
	if err != nil {
		return nil, err
	}
	if source.InventorySHA256 == "" {
		return nil, fmt.Errorf("source inventorySha256 is required")
	}
	if hash != source.InventorySHA256 {
		return nil, fmt.Errorf("source inventoryRef hash = %s, want %s", hash, source.InventorySHA256)
	}
	var inv Inventory
	if err := readJSONStrictFile(path, &inv); err != nil {
		return nil, fmt.Errorf("read source inventory: %w", err)
	}
	if inv.SchemaVersion != SchemaVersion || inv.Contract != ContractO02 {
		return nil, fmt.Errorf("source inventory identity mismatch: schema=%q contract=%q", inv.SchemaVersion, inv.Contract)
	}
	if inv.BaseRevision != source.BaseRevision || inv.OverlayHash != source.OverlayHash {
		return nil, fmt.Errorf("source inventory baseRevision/overlayHash does not match source identity")
	}
	if err := ValidateInventory(&inv); err != nil {
		return nil, fmt.Errorf("source inventory validation failed: %w", err)
	}
	if got := recomputeIncludedJavaScriptLines(inv); got != inv.Totals.IncludedJavaScriptLines {
		return nil, fmt.Errorf("source inventory includedJavaScriptLines = %d, recomputed %d", inv.Totals.IncludedJavaScriptLines, got)
	}
	return &inv, nil
}

func validateCompareRawSample(sample BrowserRawSample, canonical bool) error {
	if sample.RouteID == "" || sample.CacheMode == "" {
		return fmt.Errorf("routeID and cacheMode are required")
	}
	if sample.CacheMode != "cold" && sample.CacheMode != "warm" {
		return fmt.Errorf("cacheMode = %q, want cold or warm", sample.CacheMode)
	}
	switch sample.SampleLane {
	case SampleLaneProduct:
		if sample.Pilot != sample.Discarded {
			return fmt.Errorf("product pilot/discarded labels must match")
		}
		if !sample.Discarded && sample.RuntimeJSONDrain != nil {
			return fmt.Errorf("product sample leaked runtime JSON drain")
		}
	case SampleLaneProbe:
		if sample.Pilot || sample.Discarded {
			return fmt.Errorf("probe sample must not be pilot or discarded")
		}
	case SampleLaneProbeOverhead:
		if !sample.Pilot || !sample.Discarded {
			return fmt.Errorf("probe-overhead sample must be a discarded pilot")
		}
	default:
		return fmt.Errorf("unknown sample lane %q", sample.SampleLane)
	}
	if sample.SampleIndex < 0 {
		return fmt.Errorf("sampleIndex is negative")
	}
	if sample.Page == nil {
		return fmt.Errorf("page report is required")
	}
	if !sample.Proofs.FirstUsable.OK || !finiteNonNegative(sample.Proofs.FirstUsable.AtMs) {
		return fmt.Errorf("first usable proof is required")
	}
	if sample.Proofs.FailClosed {
		return fmt.Errorf("proof bundle is fail-closed")
	}
	if len(sample.Network) == 0 {
		return fmt.Errorf("network records are required")
	}
	for i, record := range sample.Network {
		if record.TransferredBytes < 0 || !finiteNonNegative(record.EncodedDataLength) {
			return fmt.Errorf("network record %d has negative or non-finite bytes", i)
		}
	}
	for id, value := range deriveSampleMetrics(sample) {
		if !finiteNonNegative(value) {
			return fmt.Errorf("metric %s is negative or non-finite", id)
		}
	}
	if canonical && sample.SampleLane == SampleLaneProduct && !sample.Discarded {
		if sample.Trace.TotalsMs == nil {
			return fmt.Errorf("canonical product sample requires trace totals")
		}
		if sample.RouteID == "R08" || sample.RouteID == "R10" {
			if len(sample.Artifacts.PixelManifestRefs) == 0 {
				return fmt.Errorf("canonical Scene3D product sample requires pixel manifests")
			}
		}
	}
	return nil
}

func resolveCompareManifestPath(input string) (comparePathSet, error) {
	if strings.TrimSpace(input) == "" {
		return comparePathSet{}, fmt.Errorf("manifest path or artifact root is required")
	}
	path := input
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		path = filepath.Join(path, "manifest.json")
	}
	if err != nil && errors.Is(err, os.ErrNotExist) && filepath.Base(path) != "manifest.json" {
		path = filepath.Join(path, "manifest.json")
	}
	absManifest, err := filepath.Abs(path)
	if err != nil {
		return comparePathSet{}, err
	}
	realManifest, err := filepath.EvalSymlinks(absManifest)
	if err != nil {
		return comparePathSet{}, err
	}
	root := filepath.Dir(absManifest)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return comparePathSet{}, err
	}
	if !pathUnderRoot(realRoot, realManifest) {
		return comparePathSet{}, fmt.Errorf("manifest resolves outside artifact root")
	}
	return comparePathSet{manifestPath: realManifest, root: realRoot, rootReal: realRoot}, nil
}

func resolveArtifactRef(paths comparePathSet, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("empty artifact ref")
	}
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("absolute artifact ref %q is not allowed", ref)
	}
	clean := filepath.Clean(ref)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("artifact ref escapes root: %s", ref)
	}
	path := filepath.Join(paths.root, clean)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !pathUnderRoot(paths.rootReal, real) {
		return "", fmt.Errorf("artifact ref resolves outside root: %s", ref)
	}
	return real, nil
}

func pathUnderRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func loadOptionalSizeEvidence(paths comparePathSet, source SourceIdentity, routes []FixtureSpec, mode string) (*SizeEvidence, string, error) {
	path := filepath.Join(paths.root, "size", "route-assets.json")
	if _, err := os.Stat(path); err != nil {
		if mode == CompareModeCanonical {
			return nil, "", fmt.Errorf("canonical compare requires size/route-assets.json: %w", err)
		}
		return nil, "", nil
	}
	ev, err := ReadSizeEvidenceStrict(path)
	if err != nil {
		return nil, "", err
	}
	if ev.SchemaVersion != SchemaVersion || ev.Contract != ContractO02 {
		return nil, "", fmt.Errorf("bad size evidence identity")
	}
	if !sourceCoreEqual(ev.Source, source) {
		return nil, "", fmt.Errorf("size evidence source does not match browser source")
	}
	if err := validateSizeEvidenceForCompare(ev, routes); err != nil {
		return nil, "", err
	}
	if mode == CompareModeCanonical {
		if err := validateCanonicalSizeEvidenceResourceManifestForCompare(ev); err != nil {
			return nil, "", err
		}
		if err := validateCanonicalSizeEvidenceReplayForCompare(ev); err != nil {
			return nil, "", err
		}
	}
	hash, _ := sha256File(path)
	return ev, hash, nil
}

func loadOptionalRuntimeEvidence(paths comparePathSet, source SourceIdentity, mode string) (*RuntimeBuildEvidence, string, error) {
	path := filepath.Join(paths.root, "wasm", "runtime-artifacts.json")
	if _, err := os.Stat(path); err != nil {
		if mode == CompareModeCanonical {
			return nil, "", fmt.Errorf("canonical compare requires wasm/runtime-artifacts.json: %w", err)
		}
		return nil, "", nil
	}
	ev, err := ReadRuntimeBuildEvidenceStrict(path)
	if err != nil {
		return nil, "", err
	}
	if ev.SchemaVersion != SchemaVersion || ev.Contract != ContractO02 {
		return nil, "", fmt.Errorf("bad runtime build evidence identity")
	}
	if !sourceCoreEqual(ev.Source, source) {
		return nil, "", fmt.Errorf("runtime build evidence source does not match browser source")
	}
	if err := validateRuntimeEvidenceForCompare(ev); err != nil {
		return nil, "", err
	}
	hash, _ := sha256File(path)
	return ev, hash, nil
}

func loadOptionalDynamicEvidence(paths comparePathSet, manifest *BrowserManifest, mode string) (*RuntimeJSONDynamicEvidenceManifest, string, error) {
	if manifest.DynamicProbe == "" {
		if mode == CompareModeCanonical {
			return nil, "", fmt.Errorf("canonical compare requires dynamicProbeRef")
		}
		return nil, "", nil
	}
	path, err := resolveArtifactRef(paths, manifest.DynamicProbe)
	if err != nil {
		return nil, "", fmt.Errorf("dynamicProbeRef: %w", err)
	}
	ev, err := ReadRuntimeJSONDynamicEvidenceStrict(path)
	if err != nil {
		return nil, "", err
	}
	if ev.SchemaVersion != RuntimeJSONDynamicEvidenceSchemaVersion || ev.Contract != RuntimeJSONDynamicEvidenceContractVersion {
		return nil, "", fmt.Errorf("bad dynamic evidence identity")
	}
	if ev.Validation.Status != "pass" {
		return nil, "", fmt.Errorf("dynamic evidence validation status = %q", ev.Validation.Status)
	}
	if err := ValidateRuntimeJSONDynamicEvidenceManifest(ev); err != nil {
		return nil, "", fmt.Errorf("dynamic evidence validation failed: %w", err)
	}
	if !dynamicSourceMatches(ev.Source, manifest.Source) {
		return nil, "", fmt.Errorf("dynamic evidence source does not match browser source")
	}
	if manifest.Source.RuntimeJSONStatic == nil {
		return nil, "", fmt.Errorf("browser source lacks runtime JSON static identity")
	}
	if !dynamicStaticMatches(ev.Static, *manifest.Source.RuntimeJSONStatic) {
		return nil, "", fmt.Errorf("dynamic evidence static identity does not match browser source")
	}
	hash, _ := sha256File(path)
	return ev, hash, nil
}

func loadManifestPixelRefs(paths comparePathSet, raw []BrowserRawSample, mode, pixelRoot string) ([]visual.PixelEvidenceManifest, map[string]string, error) {
	refs := map[string]bool{}
	for _, sample := range raw {
		for _, ref := range sample.Artifacts.PixelManifestRefs {
			refs[ref] = true
			if len(refs) > maxComparePixelManifestRefs {
				return nil, nil, fmt.Errorf("pixel manifest ref count exceeds limit %d", maxComparePixelManifestRefs)
			}
		}
	}
	if len(refs) == 0 {
		return nil, nil, nil
	}
	refRoot := paths
	if strings.TrimSpace(pixelRoot) != "" {
		externalRoot, err := resolveExternalArtifactRoot(pixelRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("pixel evidence root: %w", err)
		}
		refRoot = externalRoot
	}
	out := make([]visual.PixelEvidenceManifest, 0, len(refs))
	pathsByRef := map[string]string{}
	for ref := range refs {
		path, err := resolveArtifactRef(refRoot, ref)
		if err != nil {
			return nil, nil, fmt.Errorf("pixel manifest %s: %w", ref, err)
		}
		var manifest visual.PixelEvidenceManifest
		if err := readJSONStrictFile(path, &manifest); err != nil {
			return nil, nil, fmt.Errorf("read pixel manifest %s: %w", ref, err)
		}
		if manifest.SchemaVersion != visual.OuroborosPixelSchemaVersion {
			return nil, nil, fmt.Errorf("pixel manifest %s schemaVersion = %q", ref, manifest.SchemaVersion)
		}
		if err := validatePixelManifestFilesForCompare(refRoot, manifest); err != nil {
			return nil, nil, fmt.Errorf("pixel manifest %s: %w", ref, err)
		}
		out = append(out, manifest)
		pathsByRef[ref] = path
	}
	return out, pathsByRef, nil
}

func validateSizeEvidenceForCompare(ev *SizeEvidence, routes []FixtureSpec) error {
	if ev == nil {
		return fmt.Errorf("size evidence is nil")
	}
	if ev.Canonical {
		if !validSourceIdentitySHA256(ev.BuildInput.ManifestSHA256) {
			return fmt.Errorf("canonical size evidence requires buildInput.manifestSha256")
		}
		if !validSourceIdentitySHA256(ev.BuildInput.ExportSHA256) {
			return fmt.Errorf("canonical size evidence requires buildInput.exportSha256")
		}
		if !validSourceIdentitySHA256(ev.BuildInput.ResourceManifestSHA256) {
			return fmt.Errorf("canonical size evidence requires buildInput.resourceManifestSha256")
		}
		if strings.TrimSpace(ev.ResourceManifestPath) == "" {
			return fmt.Errorf("canonical size evidence requires resourceManifestPath")
		}
	}
	seenRoutes := map[string]bool{}
	for _, route := range ev.Routes {
		id := route.ID
		if id == "" {
			id = route.Route
		}
		if id == "" {
			return fmt.Errorf("size evidence route has empty id")
		}
		if seenRoutes[id] {
			return fmt.Errorf("size evidence has duplicate route %s", id)
		}
		seenRoutes[id] = true
		for name, value := range map[string]int64{
			"rawBytes":          route.RawBytes,
			"gzipBytes":         route.GzipBytes,
			"brotliBytes":       route.BrotliBytes,
			"sharedRawBytes":    route.SharedRawBytes,
			"sharedGzipBytes":   route.SharedGzipBytes,
			"sharedBrotliBytes": route.SharedBrotliBytes,
			"uniqueRawBytes":    route.UniqueRawBytes,
			"uniqueGzipBytes":   route.UniqueGzipBytes,
			"uniqueBrotliBytes": route.UniqueBrotliBytes,
		} {
			if value < 0 {
				return fmt.Errorf("size route %s has negative %s", id, name)
			}
		}
	}
	wantRoutes := routeIDs(routes)
	gotRoutes := make([]string, 0, len(seenRoutes))
	for id := range seenRoutes {
		gotRoutes = append(gotRoutes, id)
	}
	sort.Strings(gotRoutes)
	if !reflect.DeepEqual(gotRoutes, wantRoutes) {
		return fmt.Errorf("size evidence route IDs = %v, want selected browser routes %v", gotRoutes, wantRoutes)
	}
	seenAssets := map[string]bool{}
	for _, asset := range ev.Assets {
		if asset.ID == "" {
			return fmt.Errorf("size evidence asset has empty id")
		}
		if seenAssets[asset.ID] {
			return fmt.Errorf("size evidence has duplicate asset %s", asset.ID)
		}
		seenAssets[asset.ID] = true
		for name, value := range map[string]int64{
			"bytes":       asset.Bytes,
			"gzipBytes":   asset.GzipBytes,
			"brotliBytes": asset.BrotliBytes,
		} {
			if value < 0 {
				return fmt.Errorf("size asset %s has negative %s", asset.ID, name)
			}
		}
		if ev.Canonical && strings.TrimSpace(asset.ManifestHash) == "" && (strings.Contains(asset.Role, "direct") || strings.Contains(asset.Bucket, "direct")) {
			return fmt.Errorf("canonical size asset %s is an unmanifested direct asset", asset.ID)
		}
	}
	return nil
}

func validateCanonicalSizeEvidenceResourceManifestForCompare(ev *SizeEvidence) error {
	distDir, err := filepath.Abs(ev.DistDir)
	if err != nil {
		return fmt.Errorf("canonical size evidence distDir: %w", err)
	}
	info, err := os.Lstat(distDir)
	if err != nil {
		return fmt.Errorf("canonical size evidence distDir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("canonical size evidence distDir must be a real directory")
	}
	path := filepath.FromSlash(ev.ResourceManifestPath)
	if !filepath.IsAbs(path) {
		path = filepath.Join(distDir, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("canonical size evidence resourceManifestPath: %w", err)
	}
	rel, err := filepath.Rel(distDir, absPath)
	if err != nil {
		return fmt.Errorf("canonical size evidence resourceManifestPath: %w", err)
	}
	if filepath.ToSlash(rel) != CanonicalResourceManifestRef {
		return fmt.Errorf("canonical size evidence resourceManifestPath must be DistDir/%s", CanonicalResourceManifestRef)
	}
	full, err := containedRegularFileNoSymlink(distDir, CanonicalResourceManifestRef)
	if err != nil {
		return fmt.Errorf("canonical size evidence resourceManifestPath: %w", err)
	}
	if !samePath(absPath, full) {
		return fmt.Errorf("canonical size evidence resourceManifestPath must be DistDir/%s", CanonicalResourceManifestRef)
	}
	loaded, err := LoadAndValidateResourceManifest(distDir, CanonicalResourceManifestRef, true)
	if err != nil {
		return fmt.Errorf("canonical size evidence resource manifest: %w", err)
	}
	if ev.BuildInput.ResourceManifestSHA256 != loaded.SHA256 {
		return fmt.Errorf("canonical size evidence resource manifest hash mismatch")
	}
	return nil
}

func validateCanonicalSizeEvidenceReplayForCompare(ev *SizeEvidence) error {
	repoRoot := strings.TrimSpace(ev.BuildInput.GoSXModuleDir)
	if repoRoot == "" {
		return fmt.Errorf("canonical size evidence requires buildInput.gosxModuleDir")
	}
	repoRootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("canonical size evidence buildInput.gosxModuleDir: %w", err)
	}
	repoRootReal, err := filepath.EvalSymlinks(repoRootAbs)
	if err != nil {
		return fmt.Errorf("canonical size evidence buildInput.gosxModuleDir: %w", err)
	}
	distDir, err := filepath.Abs(ev.DistDir)
	if err != nil {
		return fmt.Errorf("canonical size evidence distDir: %w", err)
	}
	distDirReal, err := filepath.EvalSymlinks(distDir)
	if err != nil {
		return fmt.Errorf("canonical size evidence distDir: %w", err)
	}
	if !pathUnderRoot(repoRootReal, distDirReal) {
		return fmt.Errorf("canonical size evidence distDir must be under buildInput.gosxModuleDir")
	}
	manifestPath, err := filepath.Abs(ev.ManifestPath)
	if err != nil {
		return fmt.Errorf("canonical size evidence manifestPath: %w", err)
	}
	manifestPathReal, err := filepath.EvalSymlinks(manifestPath)
	if err != nil {
		return fmt.Errorf("canonical size evidence manifestPath: %w", err)
	}
	if !pathUnderRoot(repoRootReal, manifestPathReal) {
		return fmt.Errorf("canonical size evidence manifestPath must be under buildInput.gosxModuleDir")
	}
	if !samePath(manifestPath, filepath.Join(distDir, "build.json")) {
		return fmt.Errorf("canonical size evidence manifestPath must be DistDir/build.json")
	}
	if strings.TrimSpace(ev.ExportPath) == "" {
		return fmt.Errorf("canonical size evidence requires exportPath")
	}
	exportPath, err := filepath.Abs(ev.ExportPath)
	if err != nil {
		return fmt.Errorf("canonical size evidence exportPath: %w", err)
	}
	exportPathReal, err := filepath.EvalSymlinks(exportPath)
	if err != nil {
		return fmt.Errorf("canonical size evidence exportPath: %w", err)
	}
	if !pathUnderRoot(repoRootReal, exportPathReal) {
		return fmt.Errorf("canonical size evidence exportPath must be under buildInput.gosxModuleDir")
	}
	if !samePath(exportPath, filepath.Join(distDir, "export.json")) {
		return fmt.Errorf("canonical size evidence exportPath must be DistDir/export.json")
	}
	exportManifest, err := loadExportEvidence(exportPath, true)
	if err != nil {
		return fmt.Errorf("canonical size evidence export replay: %w", err)
	}
	if err := validateExportHTMLAttribution(distDir, exportManifest); err != nil {
		return fmt.Errorf("canonical size evidence HTML attribution replay: %w", err)
	}
	replayed, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: manifestPath,
		DistDir:      distDir,
		RepoRoot:     repoRootReal,
		ArtifactRoot: distDir,
		Canonical:    false,
	})
	if err != nil {
		return fmt.Errorf("canonical size evidence replay: %w", err)
	}
	for i := range replayed.Routes {
		replayed.Routes[i].ID = canonicalOuroborosRouteID(replayed.Routes[i].Route)
	}
	if len(replayed.Unresolved) > 0 {
		return fmt.Errorf("canonical size evidence replay has unresolved refs")
	}
	if !canonicalBrowserBuildInputMatches(ev.BuildInput, replayed.BuildInput) {
		return fmt.Errorf("canonical size evidence build input mismatch")
	}
	if !sameSizeEvidenceAssetsForCompare(ev.Assets, replayed.Assets) {
		return fmt.Errorf("canonical size evidence assets mismatch")
	}
	if !sameSizeEvidenceRoutesForCompare(ev.Routes, replayed.Routes) {
		return fmt.Errorf("canonical size evidence routes mismatch")
	}
	if ev.Totals != replayed.Totals {
		return fmt.Errorf("canonical size evidence totals mismatch")
	}
	if !samePath(ev.ResourceManifestPath, replayed.ResourceManifestPath) {
		return fmt.Errorf("canonical size evidence resource manifest path mismatch")
	}
	if ev.BuildInput.ResourceManifestSHA256 != replayed.BuildInput.ResourceManifestSHA256 {
		return fmt.Errorf("canonical size evidence resource manifest hash mismatch")
	}
	if !sameStringSlice(ev.Notes, replayed.Notes) {
		return fmt.Errorf("canonical size evidence notes mismatch")
	}
	return nil
}

func sameSizeEvidenceAssetsForCompare(a, b []TransferredAsset) bool {
	bodyA, errA := json.Marshal(a)
	bodyB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(bodyA, bodyB)
}

func sameSizeEvidenceRoutesForCompare(a, b []RouteAssetEvidence) bool {
	bodyA, errA := json.Marshal(a)
	bodyB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(bodyA, bodyB)
}

func validateRuntimeEvidenceForCompare(ev *RuntimeBuildEvidence) error {
	if ev == nil {
		return fmt.Errorf("runtime evidence is nil")
	}
	if len(ev.Variants) != 6 {
		return fmt.Errorf("runtime evidence variant count = %d, want 6", len(ev.Variants))
	}
	want := map[string]struct {
		generation string
		status     string
	}{
		"runtime": {"current", "measured"},
		"islands": {"current", "measured"},
		"core":    {"future", "planned"},
		"engine":  {"future", "planned"},
		"collab":  {"future", "planned"},
		"full":    {"future", "planned"},
	}
	seen := map[string]bool{}
	for _, variant := range ev.Variants {
		if variant.ID == "" {
			return fmt.Errorf("runtime evidence variant has empty id")
		}
		if seen[variant.ID] {
			return fmt.Errorf("runtime evidence has duplicate variant %s", variant.ID)
		}
		seen[variant.ID] = true
		expected, ok := want[variant.ID]
		if !ok {
			return fmt.Errorf("runtime evidence has unexpected variant %s", variant.ID)
		}
		if variant.Generation != expected.generation || variant.Status != expected.status {
			return fmt.Errorf("runtime variant %s generation/status = %s/%s, want %s/%s", variant.ID, variant.Generation, variant.Status, expected.generation, expected.status)
		}
		if variant.Bytes < 0 || variant.GzipBytes < 0 || variant.BrotliBytes < 0 {
			return fmt.Errorf("runtime variant %s has negative bytes", variant.ID)
		}
		if variant.SizeBytes != nil && *variant.SizeBytes < 0 {
			return fmt.Errorf("runtime variant %s has negative sizeBytes", variant.ID)
		}
		if variant.Generation == "future" && (variant.Bytes != 0 || variant.GzipBytes != 0 || variant.BrotliBytes != 0 || variant.SizeBytes != nil || variant.BudgetBytes != nil || variant.File != "") {
			return fmt.Errorf("planned runtime variant %s must not pretend to have measured binaries", variant.ID)
		}
	}
	for id := range want {
		if !seen[id] {
			return fmt.Errorf("runtime evidence missing variant %s", id)
		}
	}
	return nil
}

func dynamicSourceMatches(dynamic RuntimeJSONDynamicSourceBinding, source SourceIdentity) bool {
	return dynamic.BaseRevision == source.BaseRevision &&
		dynamic.OverlayHash == source.OverlayHash &&
		dynamic.TrackedDiffHash == source.TrackedDiffHash &&
		dynamic.UntrackedIncludedSourceHash == source.UntrackedIncludedSourceHash &&
		dynamic.InventorySHA256 == source.InventorySHA256
}

func dynamicStaticMatches(dynamic RuntimeJSONDynamicStaticBinding, static RuntimeJSONStaticIdentity) bool {
	return dynamic.SourceIdentityHash == static.SourceIdentityHash &&
		dynamic.SemanticHash == static.SemanticHash &&
		dynamic.CountsHash == static.CountsHash &&
		dynamic.GlobalNameHash == static.GlobalNameHash &&
		dynamic.ScannerVersion == static.ScannerVersion &&
		dynamic.PhaseClassifier == static.PhaseClassifier
}

func validatePixelManifestFilesForCompare(root comparePathSet, manifest visual.PixelEvidenceManifest) error {
	if manifest.Mode != string(visual.PixelModeRecordBaseline) && manifest.Mode != string(visual.PixelModeCandidateComparison) {
		return fmt.Errorf("mode = %q", manifest.Mode)
	}
	if manifest.RouteID == "" {
		return fmt.Errorf("routeID is required")
	}
	if manifest.BackendRequirement == "" {
		return fmt.Errorf("backendRequirement is required")
	}
	if manifest.HardwareClassification == "" {
		return fmt.Errorf("hardwareClassification is required")
	}
	if err := validatePixelStateSet(manifest); err != nil {
		return err
	}
	seenState := map[string]bool{}
	for _, state := range manifest.States {
		if state.State != "initial" && state.State != "settled" {
			return fmt.Errorf("unsupported pixel state %q", state.State)
		}
		if seenState[state.State] {
			return fmt.Errorf("duplicate pixel state %s", state.State)
		}
		seenState[state.State] = true
		seenCapture := map[int]bool{}
		for _, capture := range state.Captures {
			if seenCapture[capture.Index] {
				return fmt.Errorf("duplicate pixel capture %s/%d", state.State, capture.Index)
			}
			seenCapture[capture.Index] = true
			if err := validatePixelCaptureFile(root, capture); err != nil {
				return fmt.Errorf("%s/%d: %w", state.State, capture.Index, err)
			}
		}
	}
	return nil
}

func validatePixelStateSet(manifest visual.PixelEvidenceManifest) error {
	counts := map[string]int{}
	for _, state := range manifest.States {
		counts[state.State] += len(state.Captures)
	}
	for _, state := range []string{"initial", "settled"} {
		if counts[state] == 0 {
			return fmt.Errorf("pixel manifest missing %s captures", state)
		}
	}
	if counts["initial"] != counts["settled"] {
		return fmt.Errorf("pixel initial/settled sample counts differ")
	}
	return nil
}

func validatePixelCaptureFile(root comparePathSet, capture visual.PixelCaptureEvidence) error {
	if capture.Path == "" {
		return fmt.Errorf("path is required")
	}
	path, err := resolvePixelCapturePath(root, capture.Path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > maxComparePNGBytes {
		return fmt.Errorf("PNG is too large: %d bytes > %d", info.Size(), maxComparePNGBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	if capture.SHA256 == "" {
		return fmt.Errorf("sha256 is required")
	}
	gotHash, err := normalizeCompareSHA256(capture.SHA256)
	if err != nil {
		return err
	}
	if gotHash != hash {
		return fmt.Errorf("sha256 = %s, want %s", gotHash, hash)
	}
	if capture.Bytes != 0 && capture.Bytes != len(data) {
		return fmt.Errorf("bytes = %d, want %d", capture.Bytes, len(data))
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode PNG: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return fmt.Errorf("PNG has empty dimensions")
	}
	if bounds.Dx() > maxComparePNGDimension || bounds.Dy() > maxComparePNGDimension {
		return fmt.Errorf("PNG dimensions exceed limit: %dx%d", bounds.Dx(), bounds.Dy())
	}
	if int64(bounds.Dx())*int64(bounds.Dy()) > maxComparePNGDecodedPixels {
		return fmt.Errorf("PNG decoded pixel count exceeds limit: %d", int64(bounds.Dx())*int64(bounds.Dy()))
	}
	if capture.Width != 0 && capture.Width != bounds.Dx() {
		return fmt.Errorf("width = %d, want %d", capture.Width, bounds.Dx())
	}
	if capture.Height != 0 && capture.Height != bounds.Dy() {
		return fmt.Errorf("height = %d, want %d", capture.Height, bounds.Dy())
	}
	if capture.Blank || capture.Placeholder || capture.RendererFailure {
		return fmt.Errorf("capture flags mark invalid visual evidence")
	}
	if imageLooksBlank(img) {
		return fmt.Errorf("PNG appears blank")
	}
	return nil
}

func resolvePixelCapturePath(root comparePathSet, ref string) (string, error) {
	if filepath.IsAbs(ref) {
		real, err := filepath.EvalSymlinks(ref)
		if err != nil {
			return "", err
		}
		if !pathUnderRoot(root.rootReal, real) {
			return "", fmt.Errorf("absolute pixel path resolves outside pixel root: %s", ref)
		}
		return real, nil
	}
	return resolveArtifactRef(root, ref)
}

func normalizeCompareSHA256(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if !compareSHA256Re.MatchString(value) {
		return "", fmt.Errorf("invalid sha256 value %q", value)
	}
	if strings.HasPrefix(value, "sha256:") {
		return value, nil
	}
	return "sha256:" + value, nil
}

func imageLooksBlank(img interface {
	Bounds() image.Rectangle
	At(x, y int) color.Color
}) bool {
	bounds := img.Bounds()
	var first color.Color
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.At(x, y)
			_, _, _, a := c.RGBA()
			if a != 0 {
				if first == nil {
					first = c
				}
				if !colorsEqual(first, c) {
					return false
				}
			}
		}
	}
	return true
}

func colorsEqual(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func resolveExternalArtifactRoot(root string) (comparePathSet, error) {
	if strings.TrimSpace(root) == "" {
		return comparePathSet{}, fmt.Errorf("empty root")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return comparePathSet{}, err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return comparePathSet{}, err
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return comparePathSet{}, err
	}
	if !info.IsDir() {
		return comparePathSet{}, fmt.Errorf("not a directory: %s", root)
	}
	return comparePathSet{root: realRoot, rootReal: realRoot}, nil
}

func requireSummaryParity(summary BrowserSummary, samples []BrowserRawSample, runMode string, source SourceIdentity) error {
	recomputed := SummarizeBrowserSamples(samples, runMode, source)
	summary.GeneratedAt = ""
	recomputed.GeneratedAt = ""
	sortNoiseFlags(summary.NoiseFlags)
	sortNoiseFlags(recomputed.NoiseFlags)
	if !reflect.DeepEqual(summary, recomputed) {
		return fmt.Errorf("summary does not equal raw sample recomputation")
	}
	return nil
}

func sortNoiseFlags(flags []NoiseFlag) {
	sort.Slice(flags, func(i, j int) bool {
		a := flags[i].Group + "\x00" + flags[i].Metric + "\x00" + flags[i].Reason
		b := flags[j].Group + "\x00" + flags[j].Metric + "\x00" + flags[j].Reason
		return a < b
	})
}

func runCompatibilityChecks(report *CompareReport, baseline, candidate *compareLoadedArtifact, opts CompareOptions) {
	okSchema := true
	for label, artifact := range map[string]*compareLoadedArtifact{"baseline": baseline, "candidate": candidate} {
		m := artifact.manifest
		if m.SchemaVersion != BrowserBaselineSchemaVersion || m.Contract != ContractO02 || m.Initiative != Initiative || m.Spec != Spec || m.CorpusID != CorpusID {
			okSchema = false
			report.addCheck("compat.schema."+label, "compatibility", "fail", "artifact identity does not match O0.2 contract")
		}
		if m.Validation.Status != "pass" {
			okSchema = false
			report.addCheck("compat.validation."+label, "compatibility", "fail", "manifest validation status is not pass")
		}
		if opts.Mode == CompareModeSmoke && m.Canonical {
			okSchema = false
			report.addCheck("compat.smoke."+label, "compatibility", "fail", "smoke mode rejects canonical manifests")
		}
	}
	if opts.Mode == CompareModeCanonical && !baseline.manifest.Canonical {
		okSchema = false
		report.addCheck("compat.canonical.baseline", "compatibility", "fail", "canonical mode requires canonical baseline manifest")
	}
	if opts.Mode == CompareModeCanonical && !candidate.manifest.Canonical {
		okSchema = false
		report.addCheck("compat.canonical.candidate", "compatibility", "fail", "canonical mode requires canonical candidate manifest")
	}
	if okSchema {
		report.addCheck("compat.schema", "compatibility", "pass", "")
	}
	report.Compatibility.SchemaCompatible = okSchema

	sourceOK := true
	if report.SelfCompare && !reflect.DeepEqual(baseline.manifest.Source, candidate.manifest.Source) {
		sourceOK = false
		report.addCheck("compat.source.self", "source", "fail", "self-compare requires exact source identity equality")
	}
	for label, artifact := range map[string]*compareLoadedArtifact{"baseline": baseline, "candidate": candidate} {
		if err := requireCompareSource(artifact.manifest.Source, opts.Mode); err != nil {
			sourceOK = false
			report.addCheck("compat.source."+label, "source", "fail", err.Error())
		}
	}
	if runtimeScannerVersion(baseline.manifest.Source) != runtimeScannerVersion(candidate.manifest.Source) {
		sourceOK = false
		report.addCheck("compat.source.runtime-json-scanner", "source", "fail", "runtime JSON scanner versions differ")
	}
	if runtimePhaseClassifier(baseline.manifest.Source) != runtimePhaseClassifier(candidate.manifest.Source) {
		sourceOK = false
		report.addCheck("compat.source.runtime-json-phase", "source", "fail", "runtime JSON phase classifier versions differ")
	}
	if sourceOK {
		report.addCheck("compat.source", "source", "pass", "")
	}
	report.Compatibility.SourceCompatible = sourceOK

	envOK := compareEnvironment(report, baseline, candidate)
	report.Compatibility.EnvironmentCompatible = envOK
	matrixOK := compareSampleMatrix(report, baseline, candidate, opts.Mode)
	report.Compatibility.SampleMatrixCompatible = matrixOK
	artifactOK := compareArtifactPresence(report, baseline, candidate, opts.Mode)
	report.Compatibility.ArtifactsCompatible = artifactOK
}

func requireCompareSource(source SourceIdentity, mode string) error {
	missing := []string{}
	for name, value := range map[string]string{
		"baseRevision":                source.BaseRevision,
		"overlayHash":                 source.OverlayHash,
		"trackedDiffHash":             source.TrackedDiffHash,
		"untrackedIncludedSourceHash": source.UntrackedIncludedSourceHash,
		"inventoryRef":                source.InventoryRef,
		"inventorySha256":             source.InventorySHA256,
	} {
		if strings.TrimSpace(value) == "" || value == "unknown" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("source identity missing required fields: %s", strings.Join(missing, ", "))
	}
	if mode == CompareModeCanonical {
		if !source.StrictInventory || !source.ReconstructionProof {
			return fmt.Errorf("canonical compare requires strict inventory and reconstruction proof")
		}
		if source.CompatibilityAudit == nil {
			return fmt.Errorf("canonical compare requires compatibility audit identity")
		}
		if err := validateCompatibilityAuditIdentity(source.CompatibilityAudit); err != nil {
			return fmt.Errorf("canonical compare requires valid compatibility audit identity: %w", err)
		}
		if source.CompatibilityAudit.ScanStatus != compatibilityScanStatusComplete || source.CompatibilityAudit.Status != "pass" || !source.CompatibilityAudit.CanonicalAvailable {
			return fmt.Errorf("canonical compare requires complete passing compatibility audit")
		}
		if source.CompatibilityAudit.Receipt.Count != canonicalGosx || source.CompatibilityAudit.Receipt.NameSetHash != compatibilityReceiptHash {
			return fmt.Errorf("canonical compare requires pinned 209-name receipt")
		}
	}
	return nil
}

func compareEnvironment(report *CompareReport, baseline, candidate *compareLoadedArtifact) bool {
	ok := true
	if baseline.environment.EnvironmentClass != candidate.environment.EnvironmentClass {
		ok = false
		report.addCheck("compat.environment.class", "environment", "fail", "environmentClass differs")
	}
	if baseline.environment.HardwareClassification != candidate.environment.HardwareClassification {
		ok = false
		report.addCheck("compat.environment.hardware", "environment", "fail", "hardwareClassification differs")
	}
	if !equalAnyString(baseline.environment.Browser, candidate.environment.Browser, "connectionMode") {
		ok = false
		report.addCheck("compat.environment.browser-connection", "environment", "fail", "browser connection mode differs")
	}
	bRemote := compareStringFromAny(baseline.environment.Browser["connectionMode"]) == "remote-cdp"
	cRemote := compareStringFromAny(candidate.environment.Browser["connectionMode"]) == "remote-cdp"
	if bRemote || cRemote {
		bHash, bErr := normalizeCompareSHA256(compareStringFromAny(baseline.environment.Browser["remoteEndpointSHA256"]))
		cHash, cErr := normalizeCompareSHA256(compareStringFromAny(candidate.environment.Browser["remoteEndpointSHA256"]))
		if bErr != nil || cErr != nil || bHash == "" || cHash == "" {
			ok = false
			report.addCheck("compat.environment.remote-cdp", "environment", "fail", "remote CDP requires nonempty valid remoteEndpointSHA256 on both endpoints")
		} else if bHash != cHash {
			ok = false
			report.addCheck("compat.environment.remote-cdp", "environment", "fail", "remote CDP endpoint hash differs")
		}
	}
	for _, key := range []string{"width", "height", "dpr"} {
		if fmt.Sprint(baseline.environment.Viewport[key]) != fmt.Sprint(candidate.environment.Viewport[key]) {
			ok = false
			report.addCheck("compat.environment.viewport."+key, "environment", "fail", "viewport "+key+" differs")
		}
	}
	for _, key := range []string{"headless", "flags", "product", "majorVersion"} {
		if fmt.Sprint(baseline.environment.Browser[key]) != fmt.Sprint(candidate.environment.Browser[key]) {
			ok = false
			report.addCheck("compat.environment.browser."+key, "environment", "fail", "browser "+key+" differs")
		}
	}
	if ok {
		report.addCheck("compat.environment", "environment", "pass", "")
	}
	return ok
}

func compareSampleMatrix(report *CompareReport, baseline, candidate *compareLoadedArtifact, mode string) bool {
	ok := true
	if !equalStringSet(routeIDs(baseline.manifest.Corpus.Routes), routeIDs(candidate.manifest.Corpus.Routes)) {
		ok = false
		report.addCheck("compat.matrix.routes", "sample-matrix", "fail", "route IDs differ")
	}
	if !reflect.DeepEqual(bucketCounts(baseline.raw), bucketCounts(candidate.raw)) {
		ok = false
		report.addCheck("compat.matrix.sample-counts", "sample-matrix", "fail", "sample matrix counts differ")
	}
	if mode == CompareModeCanonical {
		for label, artifact := range map[string]*compareLoadedArtifact{"baseline": baseline, "candidate": candidate} {
			if err := validateCanonicalSampleMatrix(artifact.manifest.Sampling, artifact.raw); err != nil {
				ok = false
				report.addCheck("compat.matrix."+label, "sample-matrix", "fail", err.Error())
			}
		}
	} else {
		for label, artifact := range map[string]*compareLoadedArtifact{"baseline": baseline, "candidate": candidate} {
			if len(routeIDs(artifact.manifest.Corpus.Routes)) == 0 {
				ok = false
				report.addCheck("compat.matrix."+label, "sample-matrix", "fail", "smoke manifest has no routes")
			}
		}
	}
	if ok {
		report.addCheck("compat.matrix", "sample-matrix", "pass", "")
	}
	return ok
}

func compareArtifactPresence(report *CompareReport, baseline, candidate *compareLoadedArtifact, mode string) bool {
	ok := true
	if mode == CompareModeCanonical {
		for label, artifact := range map[string]*compareLoadedArtifact{"baseline": baseline, "candidate": candidate} {
			if artifact.size == nil {
				ok = false
				report.addCheck("compat.artifact.size."+label, "artifact", "fail", "canonical compare requires size evidence")
			}
			if artifact.runtime == nil {
				ok = false
				report.addCheck("compat.artifact.runtime."+label, "artifact", "fail", "canonical compare requires runtime build evidence")
			}
			if artifact.dynamic == nil {
				ok = false
				report.addCheck("compat.artifact.dynamic."+label, "artifact", "fail", "canonical compare requires dynamic JSON evidence")
			}
		}
	}
	if ok {
		report.addCheck("compat.artifacts", "artifact", "pass", "")
	}
	return ok
}

func runMetricChecks(report *CompareReport, baseline, candidate *compareLoadedArtifact, budget *CompareBudget, opts CompareOptions) {
	for _, bucket := range allMetricBuckets(baseline.metricStats, candidate.metricStats) {
		for _, metricID := range compareRequiredMetricIDs {
			rule := thresholdFor(budget, metricID)
			if ok, reason := metricApplicable(opts.Mode, baseline, candidate, bucket, metricID); !ok {
				report.addMetricCheck(metricID, categoryForMetric(metricID), bucket, metricID, "warn", 0, 0, rule, false, false, "metric not applicable: "+reason, false)
				continue
			}
			if rule.RequiresHardware && !isHardwareCompare(baseline, candidate) {
				status := "blocked"
				if opts.Mode == CompareModeSmoke {
					status = "warn"
				}
				report.addMetricCheck(metricID, categoryForMetric(metricID), bucket, metricID, status, 0, 0, rule, false, false, "requires matching hardware environment", true)
				continue
			}
			bStat, bOK := baselineStat(baseline, bucket, metricID)
			cStat, cOK := baselineStat(candidate, bucket, metricID)
			if !bOK || !cOK {
				message := "required metric is missing"
				if strings.HasPrefix(metricID, "memory.wasm") {
					message = "comparator prerequisite missing: browser raw samples do not expose WASM memory pages or bytes"
				}
				report.addMetricCheck(metricID, categoryForMetric(metricID), bucket, metricID, "fail", 0, 0, rule, false, false, message, false)
				continue
			}
			if (bStat.Unstable || cStat.Unstable) && !hasNoiseRerunProof(baseline.summary) && !isDeterministicMetric(metricID) {
				report.addMetricCheck(metricID, categoryForMetric(metricID), bucket, metricID, "blocked", bStat.Median, cStat.Median, rule, bStat.Unstable, cStat.Unstable, "required noisy metric lacks rerun proof", false)
				continue
			}
			status, message := compareThreshold(bStat.Median, cStat.Median, rule)
			if (bStat.Unstable || cStat.Unstable) && status == "pass" && !isDeterministicMetric(metricID) {
				status = "unstable-nonblocking"
				message = "metric remained unstable after rerun proof"
			}
			report.addMetricCheck(metricID, categoryForMetric(metricID), bucket, metricID, status, bStat.Median, cStat.Median, rule, bStat.Unstable, cStat.Unstable, message, false)
		}
	}
}

func runRatchetChecks(report *CompareReport, baseline, candidate *compareLoadedArtifact, budget *CompareBudget, opts CompareOptions) {
	if msg := sourceLinePrerequisite(baseline); msg != "" {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.source.includedJavaScriptLines", Category: "source", Status: "fail", Message: "baseline " + msg, PrerequisiteMissing: true})
	} else if msg := sourceLinePrerequisite(candidate); msg != "" {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.source.includedJavaScriptLines", Category: "source", Status: "fail", Message: "candidate " + msg, PrerequisiteMissing: true})
	} else {
		addValueRatchet(report, "ratchet.source.includedJavaScriptLines", "source", float64(sourceLines(baseline)), float64(sourceLines(candidate)), BudgetThreshold{AllowedAbs: 0, AllowedPct: 0})
	}
	for _, routeID := range commonManifestRoutes(baseline.manifest.Corpus.Routes, candidate.manifest.Corpus.Routes) {
		bCaps := capabilitiesForRoute(baseline.manifest.Corpus.Routes, routeID)
		cCaps := capabilitiesForRoute(candidate.manifest.Corpus.Routes, routeID)
		if !reflect.DeepEqual(bCaps, cCaps) {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.capability." + routeID, Category: "capability", RouteID: routeID, Status: "fail", Message: "route capability set changed"})
		}
	}
	if baseline.size != nil && candidate.size != nil {
		if !reflect.DeepEqual(sizeRouteIDs(baseline.size), sizeRouteIDs(candidate.size)) {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.size.route-set", Category: "size", Status: "fail", Message: "size evidence route sets differ"})
		}
		for _, route := range commonRoutesBySize(baseline.size, candidate.size) {
			b := routeSizeByID(baseline.size, route)
			c := routeSizeByID(candidate.size, route)
			addValueRatchet(report, "ratchet.size."+route+".gzipBytes", "size", float64(b.GzipBytes), float64(c.GzipBytes), thresholdFor(budget, "route.runtime.gzipBytes"))
			addValueRatchet(report, "ratchet.size."+route+".brotliBytes", "size", float64(b.BrotliBytes), float64(c.BrotliBytes), thresholdFor(budget, "route.runtime.brotliBytes"))
			if !reflect.DeepEqual(normalizeAnyCapability(b.Capabilities), normalizeAnyCapability(c.Capabilities)) {
				report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.capability." + route, Category: "capability", RouteID: route, Status: "fail", Message: "route capability set changed"})
			}
		}
	}
	if baseline.runtime != nil && candidate.runtime != nil {
		if !reflect.DeepEqual(runtimeVariantIDs(baseline.runtime), runtimeVariantIDs(candidate.runtime)) {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.wasm.variant-set", Category: "wasm", Status: "fail", Message: "runtime variant sets differ"})
		}
		for _, id := range commonRuntimeVariants(baseline.runtime, candidate.runtime) {
			b, c := runtimeVariantByID(baseline.runtime, id), runtimeVariantByID(candidate.runtime, id)
			addValueRatchet(report, "ratchet.wasm."+id+".bytes", "wasm", float64(b.Bytes), float64(c.Bytes), thresholdFor(budget, "wasm.variant.bytes"))
			addValueRatchet(report, "ratchet.wasm."+id+".gzipBytes", "wasm", float64(b.GzipBytes), float64(c.GzipBytes), thresholdFor(budget, "wasm.variant.gzipBytes"))
		}
	}
	if baseline.manifest.Source.CompatibilityAudit != nil && candidate.manifest.Source.CompatibilityAudit != nil {
		bAudit := baseline.manifest.Source.CompatibilityAudit
		cAudit := candidate.manifest.Source.CompatibilityAudit
		if cAudit.Receipt.Count != canonicalGosx || cAudit.Receipt.NameSetHash != compatibilityReceiptHash {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.global.receipt", Category: "global", Status: "fail", Message: "candidate receipt differs from pinned 209-name receipt"})
		}
		addValueRatchet(report, "ratchet.global.current.count", "global", float64(bAudit.Current.Count), float64(cAudit.Current.Count), BudgetThreshold{AllowedAbs: 0, AllowedPct: 0})
		if bAudit.Current.NameSetHash != "" && cAudit.Current.NameSetHash != "" && bAudit.Current.NameSetHash != cAudit.Current.NameSetHash {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.global.current.hash", Category: "global", Status: "fail", Message: "current __gosx_* name hash changed"})
		}
		if cAudit.Reconciliation.AddedSinceAnchorCount > 0 {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.global.added", Category: "global", Status: "fail", Candidate: float64(cAudit.Reconciliation.AddedSinceAnchorCount), Message: "new __gosx_* names appeared"})
		}
	}
	if baseline.manifest.Source.RuntimeJSONStatic != nil && candidate.manifest.Source.RuntimeJSONStatic != nil {
		b := baseline.manifest.Source.RuntimeJSONStatic.Counts
		c := candidate.manifest.Source.RuntimeJSONStatic.Counts
		addValueRatchet(report, "ratchet.json.static.sites", "json", float64(b.SerializationSiteCount), float64(c.SerializationSiteCount), thresholdFor(budget, "json.static.serializationSites"))
		addValueRatchet(report, "ratchet.json.static.hotPossible", "json", float64(b.SerializationHotPathPossibleCount), float64(c.SerializationHotPathPossibleCount), thresholdFor(budget, "json.static.hotPossible"))
		addValueRatchet(report, "ratchet.json.static.hotConfirmed", "json", float64(b.SerializationHotPathConfirmedCount), float64(c.SerializationHotPathConfirmedCount), thresholdFor(budget, "json.static.hotConfirmed"))
	}
	if baseline.dynamic != nil && candidate.dynamic != nil {
		addValueRatchet(report, "ratchet.json.dynamic.hotProduct", "json", float64(dynamicHotProduct(baseline.dynamic)), float64(dynamicHotProduct(candidate.dynamic)), thresholdFor(budget, "json.dynamic.hotProduct"))
		unknown := dynamicHotUnknown(candidate.dynamic)
		status := "pass"
		msg := ""
		if unknown > 0 {
			status = "fail"
			msg = "dynamic hot unknown JSON count must be zero"
		}
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.json.dynamic.hotUnknown", Category: "json", Candidate: float64(unknown), Status: status, Message: msg})
	}
}

func runPixelChecks(report *CompareReport, baseline, candidate *compareLoadedArtifact, opts CompareOptions) {
	for i, manifest := range candidate.pixels {
		if report.SelfCompare && manifest.Mode == string(visual.PixelModeRecordBaseline) {
			runBaselinePixelSelfCompareCheck(report, baseline, candidate, manifest, i)
			continue
		}
		runCandidatePixelCheck(report, baseline, candidate, manifest, fmt.Sprintf("ratchet.pixel.%d", i))
	}
	explicitOffset := len(candidate.pixels)
	for _, ref := range opts.CandidatePixelManifest {
		var manifest visual.PixelEvidenceManifest
		if err := readJSONStrictFile(ref, &manifest); err != nil {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.pixel.load", Category: "pixel", Status: "fail", Message: err.Error()})
			continue
		}
		if manifest.SchemaVersion != visual.OuroborosPixelSchemaVersion {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.pixel.load", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: fmt.Sprintf("pixel manifest %s schemaVersion = %q", ref, manifest.SchemaVersion)})
			continue
		}
		pixelRoot, err := resolveExternalArtifactRoot(filepath.Dir(ref))
		if err != nil {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.pixel.load", Category: "pixel", Status: "fail", Message: err.Error()})
			continue
		}
		if err := validatePixelManifestFilesForCompare(pixelRoot, manifest); err != nil {
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: "ratchet.pixel.load", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: err.Error()})
			continue
		}
		runCandidatePixelCheck(report, baseline, candidate, manifest, fmt.Sprintf("ratchet.pixel.%d", explicitOffset))
		explicitOffset++
	}
}

func runBaselinePixelSelfCompareCheck(report *CompareReport, baseline, candidate *compareLoadedArtifact, manifest visual.PixelEvidenceManifest, index int) {
	id := fmt.Sprintf("ratchet.pixel.%d", index)
	if manifest.Source.BaseRevision != baseline.manifest.Source.BaseRevision || manifest.Source.OverlayHash != baseline.manifest.Source.OverlayHash || manifest.Source.InventorySHA256 != baseline.manifest.Source.InventorySHA256 {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".source", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "baseline pixel source does not match browser source"})
	}
	if manifest.HardwareClassification != "" && manifest.HardwareClassification != baseline.environment.HardwareClassification {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".hardware", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "baseline pixel hardware class differs from browser environment"})
	}
	if manifest.BackendRequirement == "" {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".backend", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "baseline pixel backend requirement is missing"})
	}
	if !manifest.Certified {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".certified", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "baseline pixel manifest is not certified"})
	}
	if err := validatePixelStateSet(manifest); err != nil {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".states", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: err.Error()})
	}
	for _, state := range manifest.States {
		for _, capture := range state.Captures {
			status := "pass"
			msg := ""
			if capture.Blank || capture.Placeholder || capture.RendererFailure {
				status = "fail"
				msg = "baseline pixel capture is not valid canonical evidence"
			}
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + "." + state.State + "." + strconv.Itoa(capture.Index), Category: "pixel", RouteID: manifest.RouteID, Metric: "pixel.diffPct", Baseline: 0, Candidate: 0, AllowedAbs: manifest.Threshold.EffectivePct, Status: status, Message: msg})
		}
	}
}

func runCandidatePixelCheck(report *CompareReport, baseline, candidate *compareLoadedArtifact, manifest visual.PixelEvidenceManifest, id string) {
	if manifest.Mode != string(visual.PixelModeCandidateComparison) {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".mode", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "candidate pixel manifest must use mode=candidate"})
	}
	if manifest.BaselineSource == nil || manifest.BaselineSource.BaseRevision != baseline.manifest.Source.BaseRevision || manifest.BaselineSource.OverlayHash != baseline.manifest.Source.OverlayHash || manifest.BaselineSource.InventorySHA256 != baseline.manifest.Source.InventorySHA256 {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".baseline-source", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "candidate pixel baselineSource does not match baseline source"})
	}
	if manifest.Source.BaseRevision != candidate.manifest.Source.BaseRevision || manifest.Source.OverlayHash != candidate.manifest.Source.OverlayHash || manifest.Source.InventorySHA256 != candidate.manifest.Source.InventorySHA256 {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".source", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "candidate pixel source does not match candidate source"})
	}
	if manifest.HardwareClassification != "" && manifest.HardwareClassification != candidate.environment.HardwareClassification {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".hardware", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: "pixel hardware class differs from candidate browser environment"})
	}
	if err := validatePixelStateSet(manifest); err != nil {
		report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + ".states", Category: "pixel", RouteID: manifest.RouteID, Status: "fail", Message: err.Error()})
	}
	for _, state := range manifest.States {
		for _, capture := range state.Captures {
			status := "pass"
			msg := ""
			value := 0.0
			allowed := manifest.Threshold.EffectivePct
			if capture.Comparison == nil {
				status = "fail"
				msg = "pixel capture lacks comparison"
			} else {
				value = capture.Comparison.DiffPct
				allowed = capture.Comparison.EffectiveThresholdPct
				if !capture.Comparison.DimensionsMatch {
					status = "fail"
					msg = "pixel comparison dimensions differ"
				} else if capture.Comparison.DiffPct > capture.Comparison.EffectiveThresholdPct {
					status = "fail"
					msg = "pixel comparison failed"
				}
			}
			report.Ratchets = append(report.Ratchets, CompareCheck{ID: id + "." + state.State + "." + strconv.Itoa(capture.Index), Category: "pixel", RouteID: manifest.RouteID, Metric: "pixel.diffPct", Baseline: 0, Candidate: value, AllowedAbs: allowed, Status: status, Message: msg})
		}
	}
}

func buildMetricStats(samples []BrowserRawSample) map[metricBucket]map[string]Stats {
	values := map[metricBucket]map[string][]float64{}
	for _, sample := range samples {
		if sample.Discarded || sample.SampleLane != SampleLaneProduct {
			continue
		}
		bucket := metricBucket{RouteID: sample.RouteID, CacheMode: sample.CacheMode}
		if values[bucket] == nil {
			values[bucket] = map[string][]float64{}
		}
		for id, val := range deriveSampleMetrics(sample) {
			values[bucket][id] = append(values[bucket][id], val)
		}
	}
	out := map[metricBucket]map[string]Stats{}
	for bucket, metrics := range values {
		out[bucket] = map[string]Stats{}
		for id, vals := range metrics {
			out[bucket][id] = ComputeStats(vals)
		}
	}
	return out
}

func deriveSampleMetrics(sample BrowserRawSample) map[string]float64 {
	out := map[string]float64{}
	copyMetric := func(id, key string) {
		if sample.Metrics != nil {
			if v, ok := sample.Metrics[key]; ok {
				out[id] = v
			}
		}
	}
	copyMetric("browser.transferBytes", "transferBytes")
	copyMetric("startup.ttfbMs", "ttfbMs")
	copyMetric("startup.dclMs", "dclMs")
	copyMetric("startup.fullyLoadedMs", "fullyLoadedMs")
	copyMetric("memory.jsHeapUsedMb", "jsHeapUsedMb")
	copyMetric("memory.jsHeapTotalMb", "jsHeapTotalMb")
	copyMetric("memory.domNodeCount", "domNodeCount")
	copyMetric("memory.wasmPages", "wasmPages")
	copyMetric("memory.wasmBytes", "wasmBytes")
	copyMetric("scene.cpuSubmitP95Ms", "sceneCpuP95Ms")
	copyMetric("scene.rafP95Ms", "rafP95Ms")
	copyMetric("scene.missedVsyncEstimate", "missedVsyncEstimate")
	copyMetric("longTaskTotalMs", "longTaskTotalMs")
	copyMetric("totalBlockingTimeMs", "totalBlockingTimeMs")
	if sample.Proofs.FirstUsable.OK || sample.Proofs.FirstUsable.AtMs != 0 {
		out["startup.firstUsableMs"] = sample.Proofs.FirstUsable.AtMs
	}
	if sample.Page != nil {
		out["hydration.totalMs"] = sample.Page.IslandHydrationMs
		maxIsland := 0.0
		for _, island := range sample.Page.Islands {
			if island.HydrationMs > maxIsland {
				maxIsland = island.HydrationMs
			}
		}
		out["hydration.maxIslandMs"] = maxIsland
		maxDispatch := 0.0
		patchCount := 0
		for _, interaction := range sample.Page.Interactions {
			if interaction.DispatchMs > maxDispatch {
				maxDispatch = interaction.DispatchMs
			}
			patchCount += interaction.PatchCount
		}
		out["interaction.dispatchMs"] = maxDispatch
		out["interaction.patchCount"] = float64(patchCount)
		if sample.Page.Scene != nil {
			out["scene.firstFrameMs"] = sample.Page.Scene.FirstFrameMs
			if sample.Page.Scene.GPU != nil && sample.Page.Scene.GPU.Total != nil {
				out["scene.gpuTotalP95Ms"] = sample.Page.Scene.GPU.Total.Stats.P95
			}
		}
	}
	out["memory.listenerCount"] = float64(sample.Memory.ListenerCount)
	for outID, traceID := range map[string]string{
		"trace.evaluateScriptMs":    "EvaluateScript",
		"trace.compileScriptMs":     "CompileScript",
		"trace.v8CompileMs":         "v8.compile",
		"trace.v8ParseBackgroundMs": "v8.parseOnBackground",
		"trace.wasmCompileMs":       "WebAssembly.Compile",
		"trace.wasmInstantiateMs":   "WebAssembly.Instantiate",
	} {
		if sample.Trace.TotalsMs != nil {
			if v, ok := sample.Trace.TotalsMs[traceID]; ok {
				out[outID] = v
			}
		}
	}
	out["console.entryCount"] = float64(len(sample.Console))
	out["runtime.transferBytes"] = sampleRuntimeTransfer(sample)
	return out
}

func baselineStat(artifact *compareLoadedArtifact, bucket metricBucket, metricID string) (Stats, bool) {
	stats, ok := artifact.metricStats[bucket][metricID]
	return stats, ok
}

func sampleRuntimeTransfer(sample BrowserRawSample) float64 {
	total := 0.0
	for _, record := range sample.Network {
		switch record.RuntimeAssetRole {
		case "runtime", "bootstrap", "wasm", "wasm-exec":
			total += record.TransferredBytes
		}
	}
	return total
}

func runtimeTransferByBucket(samples []BrowserRawSample) map[metricBucket]float64 {
	out := map[metricBucket]float64{}
	for _, sample := range samples {
		if sample.Discarded || sample.SampleLane != SampleLaneProduct {
			continue
		}
		out[metricBucket{RouteID: sample.RouteID, CacheMode: sample.CacheMode}] += sampleRuntimeTransfer(sample)
	}
	return out
}

func consoleCountByBucket(samples []BrowserRawSample) map[metricBucket]float64 {
	out := map[metricBucket]float64{}
	for _, sample := range samples {
		if sample.Discarded || sample.SampleLane != SampleLaneProduct {
			continue
		}
		out[metricBucket{RouteID: sample.RouteID, CacheMode: sample.CacheMode}] += float64(len(sample.Console))
	}
	return out
}

func thresholdFor(budget *CompareBudget, id string) BudgetThreshold {
	if budget != nil {
		if threshold, ok := budget.Defaults[id]; ok {
			if threshold.Direction == "" {
				threshold.Direction = "lower"
			}
			if threshold.Stat == "" {
				threshold.Stat = "median"
			}
			return threshold
		}
	}
	return defaultThreshold(id)
}

func defaultThreshold(id string) BudgetThreshold {
	t := BudgetThreshold{Direction: "lower", Stat: "median"}
	switch id {
	case "browser.transferBytes", "runtime.transferBytes", "console.entryCount", "route.runtime.gzipBytes", "route.runtime.brotliBytes", "wasm.variant.bytes", "wasm.variant.gzipBytes", "json.static.serializationSites", "json.static.hotPossible", "json.static.hotConfirmed", "json.dynamic.hotProduct":
		t.Exact = true
	case "startup.ttfbMs", "startup.dclMs", "startup.fullyLoadedMs", "startup.firstUsableMs":
		t.AllowedPct, t.AllowedAbs = 5, 10
	case "hydration.totalMs", "hydration.maxIslandMs":
		t.AllowedPct, t.AllowedAbs = 5, 5
	case "memory.jsHeapUsedMb", "memory.jsHeapTotalMb":
		t.AllowedPct, t.AllowedAbs = 10, 2
	case "scene.cpuSubmitP95Ms", "scene.rafP95Ms", "scene.gpuTotalP95Ms":
		t.AllowedPct, t.AllowedAbs = 10, 2
		t.RequiresHardware = id != "scene.cpuSubmitP95Ms"
	case "pixel.diffPct":
		t.AllowedPct = visual.MaxCanonicalPixelThresholdPct
	case "memory.wasmPages", "memory.wasmBytes":
		t.Exact = true
	}
	return t
}

func compareThreshold(baseline, candidate float64, threshold BudgetThreshold) (string, string) {
	if threshold.Direction == "" {
		threshold.Direction = "lower"
	}
	if threshold.Direction != "lower" {
		return "blocked", "unsupported threshold direction: " + threshold.Direction
	}
	deltaAbs := candidate - baseline
	if threshold.Exact {
		if deltaAbs > 0 {
			return "fail", "value grew under exact no-growth ratchet"
		}
		return "pass", ""
	}
	fail := false
	if baseline == 0 {
		fail = candidate > threshold.AllowedAbs
	} else {
		deltaPct := 100 * deltaAbs / baseline
		fail = deltaAbs > threshold.AllowedAbs && deltaPct > threshold.AllowedPct
	}
	if fail {
		return "fail", "candidate exceeds absolute and percentage thresholds"
	}
	return "pass", ""
}

func addValueRatchet(report *CompareReport, id, category string, baseline, candidate float64, threshold BudgetThreshold) {
	status, message := compareThreshold(baseline, candidate, threshold)
	check := CompareCheck{
		ID:               id,
		Category:         category,
		Metric:           id,
		Direction:        "lower",
		Stat:             "value",
		Baseline:         baseline,
		Candidate:        candidate,
		DeltaAbs:         candidate - baseline,
		DeltaPct:         percentDelta(baseline, candidate),
		AllowedAbs:       threshold.AllowedAbs,
		AllowedPct:       threshold.AllowedPct,
		ThresholdFormula: "deltaAbs > allowedAbs && deltaPct > allowedPct",
		Status:           status,
		Message:          message,
	}
	report.Ratchets = append(report.Ratchets, check)
}

func (r *CompareReport) addMetricCheck(id, category string, bucket metricBucket, metric string, status string, baseline, candidate float64, threshold BudgetThreshold, bUnstable, cUnstable bool, message string, hardwareBlocked bool) {
	r.Checks = append(r.Checks, CompareCheck{
		ID:                  bucket.RouteID + "." + bucket.CacheMode + "." + id,
		Category:            category,
		RouteID:             bucket.RouteID,
		CacheMode:           bucket.CacheMode,
		Metric:              metric,
		Stat:                "median",
		Direction:           "lower",
		Baseline:            baseline,
		Candidate:           candidate,
		DeltaAbs:            candidate - baseline,
		DeltaPct:            percentDelta(baseline, candidate),
		AllowedAbs:          threshold.AllowedAbs,
		AllowedPct:          threshold.AllowedPct,
		ThresholdFormula:    "deltaAbs > allowedAbs && deltaPct > allowedPct",
		BaselineUnstable:    bUnstable,
		CandidateUnstable:   cUnstable,
		Status:              status,
		Message:             message,
		RequiresHardware:    threshold.RequiresHardware,
		PrerequisiteMissing: strings.HasPrefix(message, "comparator prerequisite missing"),
	})
}

func (r *CompareReport) addCheck(id, category, status, message string) {
	r.Checks = append(r.Checks, CompareCheck{ID: id, Category: category, Status: status, Message: message})
}

func finalizeCompareReport(report *CompareReport) {
	sort.SliceStable(report.Checks, func(i, j int) bool { return report.Checks[i].ID < report.Checks[j].ID })
	sort.SliceStable(report.Ratchets, func(i, j int) bool { return report.Ratchets[i].ID < report.Ratchets[j].ID })
	for _, check := range append(append([]CompareCheck{}, report.Checks...), report.Ratchets...) {
		report.Summary.CheckCount++
		switch check.Status {
		case "pass":
			report.Summary.PassCount++
		case "fail":
			report.Summary.FailCount++
		case "warn":
			report.Summary.WarnCount++
		case "blocked":
			report.Summary.BlockedCount++
			if strings.Contains(check.Message, "noisy metric lacks rerun proof") {
				report.Summary.InconclusiveCount++
			} else {
				report.Summary.FailCount++
			}
		case "unstable-nonblocking":
			report.Summary.UnstableCount++
		}
	}
	switch {
	case report.Summary.FailCount > 0:
		report.Status = CompareStatusFail
		report.ExitCode = 1
	case report.Summary.InconclusiveCount > 0:
		report.Status = CompareStatusInconclusive
		report.ExitCode = 2
	default:
		report.Status = CompareStatusPass
		report.ExitCode = 0
	}
}

func writeCompareReport(path string, report *CompareReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func percentDelta(baseline, candidate float64) float64 {
	if baseline == 0 {
		if candidate == 0 {
			return 0
		}
		return 100
	}
	return 100 * (candidate - baseline) / baseline
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func endpointHash(endpoint CompareArtifactEndpoint) string {
	return endpoint.ManifestSHA256 + "|" + endpoint.RawSamplesSHA256 + "|" + endpoint.SummarySHA256 + "|" + endpoint.EnvironmentSHA256
}

func samePathInput(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa := a
	bb := b
	if info, err := os.Stat(aa); err == nil && info.IsDir() {
		aa = filepath.Join(aa, "manifest.json")
	}
	if info, err := os.Stat(bb); err == nil && info.IsDir() {
		bb = filepath.Join(bb, "manifest.json")
	}
	return samePath(aa, bb)
}

func sha256File(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxCompareHashFileBytes {
		return "", fmt.Errorf("refuse to hash oversized file %s: %d bytes > %d", path, info.Size(), maxCompareHashFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func sourceCoreEqual(a, b SourceIdentity) bool {
	return a.BaseRevision == b.BaseRevision &&
		a.OverlayHash == b.OverlayHash &&
		a.TrackedDiffHash == b.TrackedDiffHash &&
		a.UntrackedIncludedSourceHash == b.UntrackedIncludedSourceHash &&
		a.InventoryRef == b.InventoryRef &&
		a.InventorySHA256 == b.InventorySHA256
}

func runtimeScannerVersion(source SourceIdentity) string {
	if source.RuntimeJSONStatic != nil {
		return source.RuntimeJSONStatic.ScannerVersion
	}
	if source.CompatibilityAudit != nil {
		return source.CompatibilityAudit.Current.EvidenceHash
	}
	return ""
}

func runtimePhaseClassifier(source SourceIdentity) string {
	if source.RuntimeJSONStatic != nil {
		return source.RuntimeJSONStatic.PhaseClassifier
	}
	return ""
}

func equalAnyString(a, b map[string]any, key string) bool {
	return fmt.Sprint(a[key]) == fmt.Sprint(b[key])
}

func compareStringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func routeIDs(routes []FixtureSpec) []string {
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.ID)
	}
	sort.Strings(out)
	return out
}

func equalStringSet(a, b []string) bool {
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func bucketCounts(samples []BrowserRawSample) map[string]int {
	out := map[string]int{}
	for _, sample := range samples {
		key := string(sample.SampleLane) + "/" + sample.RouteID + "/" + sample.CacheMode + "/" + strconv.FormatBool(sample.Pilot) + "/" + strconv.FormatBool(sample.Discarded)
		out[key]++
	}
	return out
}

func allMetricBuckets(a, b map[metricBucket]map[string]Stats) []metricBucket {
	seen := map[metricBucket]bool{}
	for bucket := range a {
		seen[bucket] = true
	}
	for bucket := range b {
		seen[bucket] = true
	}
	out := make([]metricBucket, 0, len(seen))
	for bucket := range seen {
		out = append(out, bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RouteID == out[j].RouteID {
			return out[i].CacheMode < out[j].CacheMode
		}
		return out[i].RouteID < out[j].RouteID
	})
	return out
}

func categoryForMetric(id string) string {
	if idx := strings.IndexByte(id, '.'); idx > 0 {
		return id[:idx]
	}
	return "metric"
}

func metricApplicable(mode string, baseline, candidate *compareLoadedArtifact, bucket metricBucket, metricID string) (bool, string) {
	switch {
	case strings.HasPrefix(metricID, "trace."):
		if mode == CompareModeSmoke && (!bucketTraceCaptured(baseline, bucket) || !bucketTraceCaptured(candidate, bucket)) {
			return false, "trace capture disabled for smoke"
		}
	case strings.HasPrefix(metricID, "scene."):
		if !bucketHasCapability(baseline, bucket.RouteID, "scene3d") && !bucketHasCapability(candidate, bucket.RouteID, "scene3d") {
			return false, "route is not Scene3D-capable"
		}
	case metricID == "memory.wasmPages" || metricID == "memory.wasmBytes":
		if !routeSelectsWASM(baseline, bucket.RouteID) && !routeSelectsWASM(candidate, bucket.RouteID) {
			return false, "route does not select WASM runtime"
		}
	}
	return true, ""
}

func bucketTraceCaptured(artifact *compareLoadedArtifact, bucket metricBucket) bool {
	for _, sample := range artifact.raw {
		if sample.SampleLane == SampleLaneProduct && !sample.Discarded && sample.RouteID == bucket.RouteID && sample.CacheMode == bucket.CacheMode {
			if sample.Trace.Captured {
				return true
			}
		}
	}
	return false
}

func bucketHasCapability(artifact *compareLoadedArtifact, routeID, capability string) bool {
	for _, route := range artifact.manifest.Corpus.Routes {
		if route.ID != routeID {
			continue
		}
		for _, got := range route.ExpectedCapabilities {
			if got == capability {
				return true
			}
		}
	}
	return false
}

func routeSelectsWASM(artifact *compareLoadedArtifact, routeID string) bool {
	for _, route := range artifact.manifest.Corpus.Routes {
		if route.ID == routeID && route.ExpectedTinyGoCurrent != "" && route.ExpectedTinyGoCurrent != "none" {
			return true
		}
	}
	if artifact.inventory != nil {
		for _, variant := range artifact.inventory.Manifest.Variants {
			if variant.Generation != "current" {
				continue
			}
			for _, selected := range variant.SelectedByRoutes {
				if selected == routeID {
					return true
				}
			}
		}
	}
	return false
}

func isDeterministicMetric(id string) bool {
	return strings.Contains(id, "Bytes") || strings.HasPrefix(id, "console.") || strings.HasPrefix(id, "json.") || strings.HasPrefix(id, "route.") || strings.HasPrefix(id, "wasm.")
}

func hasNoiseRerunProof(summary BrowserSummary) bool {
	// BrowserSummary has no rerun-proof fields in the current producer.
	// Canonical and smoke comparisons must stay inconclusive for noisy
	// required metrics until the producer adds explicit proof fields.
	return false
}

func isHardwareCompare(baseline, candidate *compareLoadedArtifact) bool {
	return baseline.environment.HardwareClassification == candidate.environment.HardwareClassification &&
		(baseline.environment.HardwareClassification == "hardware-webgpu" || baseline.environment.HardwareClassification == "hardware-webgl")
}

func sourceLines(artifact *compareLoadedArtifact) int {
	if artifact == nil || artifact.inventory == nil {
		return 0
	}
	return recomputeIncludedJavaScriptLines(*artifact.inventory)
}

func sourceLinePrerequisite(artifact *compareLoadedArtifact) string {
	if artifact == nil || artifact.inventory == nil {
		return "source inventory is unavailable"
	}
	if artifact.inventory.Totals.IncludedJavaScriptLines == 0 {
		return "source inventory totals.includedJavaScriptLines is zero or missing"
	}
	return ""
}

func recomputeIncludedJavaScriptLines(inv Inventory) int {
	total := 0
	for _, file := range inv.Files.Included {
		if file.Language == "javascript" || strings.HasSuffix(file.Path, ".js") || strings.HasSuffix(file.Path, ".mjs") {
			total += file.Lines
		}
	}
	return total
}

func commonManifestRoutes(a, b []FixtureSpec) []string {
	seen := map[string]bool{}
	for _, route := range a {
		seen[route.ID] = false
	}
	for _, route := range b {
		if _, ok := seen[route.ID]; ok {
			seen[route.ID] = true
		}
	}
	var out []string
	for id, ok := range seen {
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func capabilitiesForRoute(routes []FixtureSpec, routeID string) []string {
	for _, route := range routes {
		if route.ID == routeID {
			out := append([]string{}, route.ExpectedCapabilities...)
			sort.Strings(out)
			return out
		}
	}
	return nil
}

func commonRoutesBySize(a, b *SizeEvidence) []string {
	seen := map[string]bool{}
	for _, route := range a.Routes {
		if route.ID != "" {
			seen[route.ID] = false
		} else {
			seen[route.Route] = false
		}
	}
	for _, route := range b.Routes {
		id := route.ID
		if id == "" {
			id = route.Route
		}
		if _, ok := seen[id]; ok {
			seen[id] = true
		}
	}
	var out []string
	for id, ok := range seen {
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func sizeRouteIDs(evidence *SizeEvidence) []string {
	var out []string
	if evidence == nil {
		return out
	}
	for _, route := range evidence.Routes {
		id := route.ID
		if id == "" {
			id = route.Route
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func routeSizeByID(evidence *SizeEvidence, id string) RouteAssetEvidence {
	for _, route := range evidence.Routes {
		routeID := route.ID
		if routeID == "" {
			routeID = route.Route
		}
		if routeID == id {
			return route
		}
	}
	return RouteAssetEvidence{}
}

func normalizeAnyCapability(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

func commonRuntimeVariants(a, b *RuntimeBuildEvidence) []string {
	seen := map[string]bool{}
	for _, variant := range a.Variants {
		seen[variant.ID] = false
	}
	for _, variant := range b.Variants {
		if _, ok := seen[variant.ID]; ok {
			seen[variant.ID] = true
		}
	}
	var out []string
	for id, ok := range seen {
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func runtimeVariantIDs(evidence *RuntimeBuildEvidence) []string {
	var out []string
	if evidence == nil {
		return out
	}
	for _, variant := range evidence.Variants {
		out = append(out, variant.ID)
	}
	sort.Strings(out)
	return out
}

func runtimeVariantByID(evidence *RuntimeBuildEvidence, id string) RuntimeArtifactVariant {
	for _, variant := range evidence.Variants {
		if variant.ID == id {
			return variant
		}
	}
	return RuntimeArtifactVariant{}
}

func dynamicHotProduct(evidence *RuntimeJSONDynamicEvidenceManifest) int {
	count := 0
	for _, event := range evidence.Events {
		if event.HotPath && event.IncludeInProductCounts {
			count++
		}
	}
	return count
}

func dynamicHotUnknown(evidence *RuntimeJSONDynamicEvidenceManifest) int {
	count := 0
	for _, row := range evidence.Matrix {
		count += row.HotUnknownEventCount
	}
	return count
}
