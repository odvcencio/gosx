package ouroboros

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	cdpBrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"

	"m31labs.dev/gosx/perf"
	"m31labs.dev/gosx/visual"
)

const BrowserBaselineSchemaVersion = "gosx.ouroboros.browser-baseline.v1"

const r10MinimumSampleTimeout = 60 * time.Second
const canonicalBrowserToolOutputLimit = 64 << 10

type SampleLane string

const (
	SampleLaneProduct       SampleLane = "product"
	SampleLaneProbe         SampleLane = "probe"
	SampleLaneProbeOverhead SampleLane = "probe-overhead"
)

type BrowserBaselineOptions struct {
	RepoRoot           string
	CorpusPath         string
	InventoryPath      string
	SourceIdentityPath string
	ArtifactRoot       string
	EvidenceRoot       string
	DocsBaseURL        string
	PixelManifest      string
	BaseURL            string
	FixtureApp         string
	Serve              bool
	Port               int
	Samples            string
	Routes             []string
	Headless           bool
	Timeout            time.Duration
	ChromeWebSocketURL string
	HeapSnapshots      bool
	Trace              bool
	Coverage           bool
	ViewportWidth      int
	ViewportHeight     int
	DPR                float64
	Environment        string
	RuntimeProbeNames  []string
}

type BrowserBaselineResult struct {
	ManifestPath    string
	EnvironmentPath string
	SummaryPath     string
	RawSamplesPath  string
	CommandLogPath  string
	ArtifactRoot    string
	SampleCount     int
	DiscardedCount  int
	Canonical       bool
}

type BrowserManifest struct {
	SchemaVersion string              `json:"schemaVersion"`
	Contract      string              `json:"contractVersion"`
	Initiative    string              `json:"initiative"`
	Spec          string              `json:"spec"`
	CorpusID      string              `json:"corpusID"`
	GeneratedAt   string              `json:"generatedAt"`
	ArtifactRoot  string              `json:"artifactRoot"`
	Source        SourceIdentity      `json:"source"`
	Corpus        FixtureCorpus       `json:"corpus"`
	Sampling      SamplingPlan        `json:"sampling"`
	Environment   string              `json:"environmentRef"`
	RawSamples    string              `json:"rawSamplesRef"`
	Summary       string              `json:"summaryRef"`
	Commands      string              `json:"commandsRef"`
	Probe         ProbeSchemaIdentity `json:"probe"`
	DynamicProbe  string              `json:"dynamicProbeRef,omitempty"`
	Validation    BaselineValidation  `json:"validation"`
	Canonical     bool                `json:"canonical"`
}

type ProbeSchemaIdentity struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Facade        string   `json:"facade"`
	EventKinds    []string `json:"eventKinds"`
	InjectedByCDP bool     `json:"injectedByCDP"`
	ProductAsset  bool     `json:"productAsset"`
}

type FixtureCorpus struct {
	SchemaVersion   string        `json:"schemaVersion"`
	Contract        string        `json:"contractVersion"`
	CorpusID        string        `json:"corpusID"`
	FixtureApp      string        `json:"fixtureApp"`
	Authoring       []string      `json:"authoring,omitempty"`
	Routes          []FixtureSpec `json:"routes"`
	SourceCorpusRef string        `json:"sourceCorpusRef"`
}

type FixtureSpec struct {
	ID                      string   `json:"id"`
	Route                   string   `json:"route"`
	FixtureApp              string   `json:"fixtureApp"`
	ExpectedCapabilities    []string `json:"expectedCapabilities"`
	ExpectedTinyGoCurrent   string   `json:"expectedTinyGoCurrent"`
	ExpectedTinyGoFuture    string   `json:"expectedTinyGoFuture"`
	ServerBuildMode         string   `json:"serverBuildMode,omitempty"`
	RequiredInteractions    []string `json:"requiredInteractions,omitempty"`
	RequiredScreenshots     []string `json:"requiredScreenshots,omitempty"`
	RoutePlanAssertions     []string `json:"routePlanAssertions,omitempty"`
	DisallowedRuntimeAssets []string `json:"disallowedRuntimeAssets,omitempty"`
	External                bool     `json:"external,omitempty"`
}

type SamplingPlan struct {
	Name                 string `json:"name"`
	Canonical            bool   `json:"canonical"`
	PilotsDiscarded      int    `json:"pilotsDiscarded"`
	ColdSamples          int    `json:"coldSamples"`
	WarmSamples          int    `json:"warmSamples"`
	SceneColdSamples     int    `json:"sceneColdSamples"`
	SceneWarmSamples     int    `json:"sceneWarmSamples"`
	CanUpdateBaseline    bool   `json:"canUpdateBaseline"`
	BaselineUpdatePolicy string `json:"baselineUpdatePolicy"`
}

type EnvironmentReport struct {
	SchemaVersion          string            `json:"schemaVersion"`
	GeneratedAt            string            `json:"generatedAt"`
	EnvironmentClass       string            `json:"environmentClass"`
	OS                     map[string]string `json:"os"`
	CPU                    map[string]string `json:"cpu"`
	Memory                 map[string]string `json:"memory"`
	Power                  map[string]string `json:"power"`
	Tools                  map[string]string `json:"tools"`
	Browser                map[string]any    `json:"browser"`
	Viewport               map[string]any    `json:"viewport"`
	GPU                    map[string]any    `json:"gpu"`
	Server                 map[string]any    `json:"server"`
	Network                map[string]string `json:"network"`
	RuntimeManifest        map[string]string `json:"runtimeManifest"`
	HardwareClassification string            `json:"hardwareClassification"`
	Unknowns               []string          `json:"unknowns"`
}

type BrowserRawSample struct {
	SchemaVersion    string                     `json:"schemaVersion"`
	Contract         string                     `json:"contractVersion"`
	Kind             string                     `json:"kind"`
	RunMode          string                     `json:"runMode"`
	RouteID          string                     `json:"routeID"`
	Route            string                     `json:"route"`
	URL              string                     `json:"url"`
	SampleLane       SampleLane                 `json:"sampleLane"`
	CacheMode        string                     `json:"cacheMode"`
	SampleIndex      int                        `json:"sampleIndex"`
	Pilot            bool                       `json:"pilot"`
	Discarded        bool                       `json:"discarded"`
	StartedAt        string                     `json:"startedAt"`
	DurationMs       float64                    `json:"durationMs"`
	Source           SourceIdentity             `json:"source"`
	Artifacts        SampleArtifacts            `json:"artifacts"`
	Page             *perf.PageReport           `json:"page,omitempty"`
	Proofs           ProofBundle                `json:"proofs"`
	Trace            TraceSampleSummary         `json:"trace"`
	Coverage         CoverageSampleSummary      `json:"coverage"`
	Memory           perf.MemoryStats           `json:"memory"`
	Console          []perf.ConsoleEntry        `json:"console,omitempty"`
	Network          []NetworkRecord            `json:"network,omitempty"`
	ProbeEvents      []ProbeEvent               `json:"probeEvents,omitempty"`
	RuntimeJSONDrain *RuntimeJSONRawDrain       `json:"runtimeJSONDrain,omitempty"`
	Errors           []string                   `json:"errors,omitempty"`
	Metrics          map[string]float64         `json:"metrics"`
	Notes            map[string]string          `json:"notes,omitempty"`
	Raw              map[string]json.RawMessage `json:"raw,omitempty"`
}

type NetworkRecord struct {
	RequestID           string            `json:"requestID"`
	URL                 string            `json:"url"`
	DocumentURL         string            `json:"documentURL,omitempty"`
	Method              string            `json:"method,omitempty"`
	Role                string            `json:"role"`
	Status              int64             `json:"status"`
	MimeType            string            `json:"mimeType,omitempty"`
	Protocol            string            `json:"protocol,omitempty"`
	EncodedDataLength   float64           `json:"encodedDataLength"`
	TransferredBytes    float64           `json:"transferredBytes"`
	HeaderBytes         int               `json:"headerBytes,omitempty"`
	DecodedBodyBytes    int64             `json:"decodedBodyBytes,omitempty"`
	FromDiskCache       bool              `json:"fromDiskCache"`
	FromServiceWorker   bool              `json:"fromServiceWorker"`
	FromPrefetchCache   bool              `json:"fromPrefetchCache"`
	Immutable           bool              `json:"immutable"`
	CacheControl        string            `json:"cacheControl,omitempty"`
	RuntimeAssetRole    string            `json:"runtimeAssetRole,omitempty"`
	UnresolvedAssetRole bool              `json:"unresolvedAssetRole,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
}

type SampleArtifacts struct {
	TraceRef            string   `json:"traceRef,omitempty"`
	CoverageRef         string   `json:"coverageRef,omitempty"`
	HeapSnapshotRef     string   `json:"heapSnapshotRef,omitempty"`
	ScreenshotRef       string   `json:"screenshotRef,omitempty"`
	ExternalProcessRef  string   `json:"externalProcessRef,omitempty"`
	ExternalEvidenceRef string   `json:"externalEvidenceRef,omitempty"`
	WaterProfileRef     string   `json:"waterProfileRef,omitempty"`
	PixelManifestRefs   []string `json:"pixelManifestRefs,omitempty"`
}

type ExternalRouteEvidence struct {
	RouteID           string                  `json:"routeID"`
	URL               string                  `json:"url"`
	FixtureApp        string                  `json:"fixtureApp"`
	BaseURL           string                  `json:"baseURL"`
	Canonical         bool                    `json:"canonical"`
	Process           ExternalProcessIdentity `json:"process"`
	WaterProfileRef   string                  `json:"waterProfileRef,omitempty"`
	PixelManifestRefs []string                `json:"pixelManifestRefs,omitempty"`
	Errors            []string                `json:"errors,omitempty"`
	Raw               map[string]any          `json:"raw,omitempty"`
}

type ExternalProcessIdentity struct {
	Managed   bool   `json:"managed"`
	PID       int    `json:"pid,omitempty"`
	Command   string `json:"command,omitempty"`
	Directory string `json:"directory,omitempty"`
	BaseURL   string `json:"baseURL"`
}

type ProofBundle struct {
	Required        []string       `json:"required"`
	Observed        []string       `json:"observed"`
	FirstUsable     ProofPayload   `json:"firstUsable"`
	Interaction     []ProofPayload `json:"interaction,omitempty"`
	FailClosed      bool           `json:"failClosed"`
	MissingRequired []string       `json:"missingRequired,omitempty"`
	RoutePlan       map[string]any `json:"routePlan,omitempty"`
	Registry        map[string]any `json:"registry,omitempty"`
}

type ProofPayload struct {
	Name     string         `json:"name"`
	AtMs     float64        `json:"atMs"`
	OK       bool           `json:"ok"`
	Payload  map[string]any `json:"payload,omitempty"`
	Selector string         `json:"selector,omitempty"`
	Message  string         `json:"message,omitempty"`
}

type TraceSampleSummary struct {
	Captured bool                 `json:"captured"`
	Ref      string               `json:"ref,omitempty"`
	Events   []perf.TraceHotEvent `json:"events,omitempty"`
	TotalsMs map[string]float64   `json:"totalsMs,omitempty"`
}

type CoverageSampleSummary struct {
	Captured    bool                 `json:"captured"`
	Ref         string               `json:"ref,omitempty"`
	ScriptCount int                  `json:"scriptCount"`
	UsedBytes   int                  `json:"usedBytes"`
	UnusedBytes int                  `json:"unusedBytes"`
	Entries     []perf.CoverageEntry `json:"entries,omitempty"`
}

type BrowserSummary struct {
	SchemaVersion string                          `json:"schemaVersion"`
	GeneratedAt   string                          `json:"generatedAt"`
	RunMode       string                          `json:"runMode"`
	Source        SourceIdentity                  `json:"source"`
	Groups        map[string]map[string]MetricSet `json:"groups"`
	SampleCount   int                             `json:"sampleCount"`
	Discarded     int                             `json:"discardedSampleCount"`
	NoiseFlags    []NoiseFlag                     `json:"noiseFlags,omitempty"`
}

type BaselineValidation struct {
	Status   string   `json:"status"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type MetricSet map[string]Stats

type Stats struct {
	N        int     `json:"n"`
	Mean     float64 `json:"mean"`
	Median   float64 `json:"median"`
	P75      float64 `json:"p75"`
	P95      float64 `json:"p95"`
	P99      float64 `json:"p99"`
	Max      float64 `json:"max"`
	MAD      float64 `json:"mad"`
	IQR      float64 `json:"iqr"`
	NoisyMAD bool    `json:"noisyMad"`
	NoisyIQR bool    `json:"noisyIqr"`
	Unstable bool    `json:"unstable"`
}

type NoiseFlag struct {
	Group  string  `json:"group"`
	Metric string  `json:"metric"`
	Reason string  `json:"reason"`
	Ratio  float64 `json:"ratio"`
}

func RunBrowserBaseline(ctx context.Context, opts BrowserBaselineOptions) (result *BrowserBaselineResult, err error) {
	opts = normalizeBrowserOptions(opts)
	artifactWriteStarted := false
	defer func() {
		if err == nil {
			return
		}
		err = sanitizeArtifactErrorForOptions(opts, err)
		if artifactWriteStarted {
			writeBoundaryFailure(opts, err)
		}
	}()
	plan, err := samplingPlan(opts.Samples)
	if err != nil {
		return nil, err
	}
	if plan.Canonical {
		if opts.InventoryPath == "" {
			return nil, fmt.Errorf("canonical browser baseline requires --inventory")
		}
		if opts.ArtifactRoot == "" || strings.Contains(filepath.Base(opts.ArtifactRoot), "smoke") {
			return nil, fmt.Errorf("canonical browser baseline requires an explicit non-smoke artifact root")
		}
		opts.Trace = true
		opts.Coverage = true
		opts.HeapSnapshots = true
	} else if existingCanonicalManifest(opts.ArtifactRoot) {
		return nil, fmt.Errorf("smoke run refuses to overwrite canonical artifact root: %s", opts.ArtifactRoot)
	}
	corpus, err := LoadFixtureCorpus(opts.CorpusPath)
	if err != nil {
		return nil, err
	}
	corpus.SourceCorpusRef = relTo(opts.ArtifactRoot, opts.CorpusPath)
	if plan.Canonical {
		if err := validateCanonicalRouteSelection(corpus.Routes, opts.Routes); err != nil {
			return nil, err
		}
		if _, err := preflightCanonicalBrowserInputs(ctx, opts, corpus.Routes); err != nil {
			return nil, err
		}
	}
	originalInventoryPath := opts.InventoryPath
	artifactWriteStarted = true
	if plan.Canonical {
		materializedInventory, err := MaterializeCanonicalInventory(ctx, opts.RepoRoot, opts.InventoryPath, opts.ArtifactRoot)
		if err != nil {
			return nil, err
		}
		opts.InventoryPath = materializedInventory
	}
	if err := os.MkdirAll(opts.ArtifactRoot, 0o755); err != nil {
		return nil, err
	}

	commandLogPath := filepath.Join(opts.ArtifactRoot, "commands.log")
	commandLog, err := os.Create(commandLogPath)
	if err != nil {
		return nil, err
	}
	defer commandLog.Close()
	logBrowserCommand(commandLog, opts, "gosx perf ouroboros", os.Args[1:])

	source, err := browserSourceIdentity(ctx, opts)
	if err != nil {
		return nil, err
	}
	if plan.Canonical {
		if err := validateCanonicalEvidenceRefs(opts, source); err != nil {
			return nil, err
		}
	}
	opts.RuntimeProbeNames = append([]string{}, source.RuntimeProbeNames...)
	selected := filterRoutes(corpus.Routes, opts.Routes)
	if len(selected) == 0 {
		return nil, fmt.Errorf("no routes selected")
	}
	corpus.Routes = selected

	server, err := maybeStartFixtureServer(ctx, opts, corpus.Routes, commandLog)
	if err != nil {
		return nil, err
	}
	if server != nil {
		defer server.Close()
		opts.BaseURL = server.baseURL
	}
	docsServer, err := maybeStartExternalDocsServer(ctx, opts, corpus.Routes, commandLog)
	if err != nil {
		return nil, err
	}
	if docsServer != nil {
		defer docsServer.Close()
		opts.DocsBaseURL = docsServer.baseURL
	}

	env, err := CollectBrowserEnvironment(ctx, opts)
	if err != nil {
		return nil, err
	}
	env.Server["baseURL"] = opts.BaseURL
	env.Server["fixtureApp"] = opts.FixtureApp
	if hasExternalRoute(corpus.Routes) {
		env.Server["docsBaseURL"] = opts.DocsBaseURL
		if docsServer != nil {
			env.Server["externalDocsProcess"] = ExternalProcessIdentity{
				Managed:   true,
				PID:       docsServer.pid,
				Command:   strings.Join(docsServer.args, " "),
				Directory: docsServer.dir,
				BaseURL:   docsServer.baseURL,
			}
		} else {
			env.Server["externalDocsProcess"] = ExternalProcessIdentity{Managed: false, BaseURL: opts.DocsBaseURL}
		}
	}

	envPath := filepath.Join(opts.ArtifactRoot, "environment.json")
	rawPath := filepath.Join(opts.ArtifactRoot, "perf", "raw-samples.jsonl")
	summaryPath := filepath.Join(opts.ArtifactRoot, "summaries", "browser-summary.json")
	manifestPath := filepath.Join(opts.ArtifactRoot, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o755); err != nil {
		return nil, err
	}
	if err := WriteJSONFile(envPath, env); err != nil {
		return nil, err
	}

	rawFile, err := os.Create(rawPath)
	if err != nil {
		return nil, err
	}
	defer rawFile.Close()
	rawWriter := bufio.NewWriter(rawFile)
	defer rawWriter.Flush()

	externalEvidence := map[string]ExternalRouteEvidence{}
	for _, route := range corpus.Routes {
		if route.ID == "R10" {
			ev := collectR10ExternalEvidence(ctx, opts, route, docsServer, commandLog)
			externalEvidence[route.ID] = ev
		}
	}

	var samples []BrowserRawSample
	for _, route := range corpus.Routes {
		routeSamples, err := runRouteSamples(ctx, opts, plan, source, route, externalEvidence[route.ID], rawWriter, commandLog)
		if err != nil {
			return nil, err
		}
		samples = append(samples, routeSamples...)
	}

	dynamicProbeRef := ""
	if plan.Canonical {
		ref, err := writeRuntimeJSONDynamicEvidence(opts, source, samples)
		if err != nil {
			return nil, err
		}
		dynamicProbeRef = ref
	}

	validation := ValidateBrowserBaseline(plan, samples, source, env, opts)
	canonical := plan.Canonical && validation.Status == "pass"
	summary := SummarizeBrowserSamples(samples, opts.Samples, source)
	if err := WriteJSONFile(summaryPath, summary); err != nil {
		return nil, err
	}

	manifest := BrowserManifest{
		SchemaVersion: BrowserBaselineSchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		ArtifactRoot:  opts.ArtifactRoot,
		Source:        source,
		Corpus:        corpus,
		Sampling:      plan,
		Environment:   relTo(opts.ArtifactRoot, envPath),
		RawSamples:    relTo(opts.ArtifactRoot, rawPath),
		Summary:       relTo(opts.ArtifactRoot, summaryPath),
		Commands:      relTo(opts.ArtifactRoot, commandLogPath),
		Probe:         DefaultProbeSchemaIdentity(),
		DynamicProbe:  dynamicProbeRef,
		Validation:    validation,
		Canonical:     canonical,
	}
	if plan.Canonical {
		if err := revalidateCanonicalBrowserPreseedArtifacts(ctx, opts, originalInventoryPath, corpus.Routes, source); err != nil {
			return nil, err
		}
	}
	if err := WriteJSONFile(manifestPath, manifest); err != nil {
		return nil, err
	}
	if validation.Status != "pass" {
		if err := WriteJSONFile(filepath.Join(opts.ArtifactRoot, "failure.json"), validation); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("browser baseline failed validation: %s", strings.Join(validation.Errors, "; "))
	}

	discarded := 0
	acceptedProduct := 0
	for _, s := range samples {
		if s.Discarded {
			discarded++
		}
		if s.SampleLane == SampleLaneProduct && !s.Discarded {
			acceptedProduct++
		}
	}
	return &BrowserBaselineResult{
		ManifestPath:    manifestPath,
		EnvironmentPath: envPath,
		SummaryPath:     summaryPath,
		RawSamplesPath:  rawPath,
		CommandLogPath:  commandLogPath,
		ArtifactRoot:    opts.ArtifactRoot,
		SampleCount:     acceptedProduct,
		DiscardedCount:  discarded,
		Canonical:       canonical,
	}, nil
}

func normalizeBrowserOptions(opts BrowserBaselineOptions) BrowserBaselineOptions {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	if opts.CorpusPath == "" {
		opts.CorpusPath = filepath.Join(opts.RepoRoot, "examples", "ouroboros-corpus", "fixtures.v1.json")
	}
	if opts.ArtifactRoot == "" {
		opts.ArtifactRoot = filepath.Join(opts.RepoRoot, "build", "ouroboros", "o0.2", "browser-smoke")
	}
	if opts.EvidenceRoot == "" {
		opts.EvidenceRoot = opts.ArtifactRoot
	}
	if opts.FixtureApp == "" {
		opts.FixtureApp = filepath.Join(opts.RepoRoot, "examples", "ouroboros-corpus")
	}
	if opts.Port == 0 {
		opts.Port = 0
	}
	if opts.Samples == "" {
		opts.Samples = "smoke"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.ViewportWidth == 0 {
		opts.ViewportWidth = 1280
	}
	if opts.ViewportHeight == 0 {
		opts.ViewportHeight = 720
	}
	if opts.DPR == 0 {
		opts.DPR = 1
	}
	if opts.Environment == "" {
		opts.Environment = "headless-logic"
	}
	return opts
}

func samplingPlan(name string) (SamplingPlan, error) {
	switch name {
	case "baseline":
		return SamplingPlan{Name: name, Canonical: true, PilotsDiscarded: 2, ColdSamples: 11, WarmSamples: 21, SceneColdSamples: 7, SceneWarmSamples: 15, CanUpdateBaseline: true, BaselineUpdatePolicy: "canonical O0.2 only"}, nil
	case "smoke":
		return SamplingPlan{Name: name, Canonical: false, PilotsDiscarded: 0, ColdSamples: 1, WarmSamples: 1, SceneColdSamples: 1, SceneWarmSamples: 1, CanUpdateBaseline: false, BaselineUpdatePolicy: "reduced smoke can never update canonical baseline"}, nil
	default:
		return SamplingPlan{}, fmt.Errorf("unknown sample plan %q", name)
	}
}

func LoadFixtureCorpus(path string) (FixtureCorpus, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return FixtureCorpus{}, err
	}
	var doc struct {
		SchemaVersion string        `json:"schemaVersion"`
		Contract      string        `json:"contractVersion"`
		CorpusID      string        `json:"corpusID"`
		FixtureApp    string        `json:"fixtureApp"`
		Authoring     []string      `json:"authoring"`
		Routes        []FixtureSpec `json:"routes"`
		FixtureRoutes []FixtureSpec `json:"fixtureRoutes"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return FixtureCorpus{}, err
	}
	routes := doc.Routes
	if len(routes) == 0 {
		routes = doc.FixtureRoutes
	}
	if len(routes) == 0 {
		return FixtureCorpus{}, fmt.Errorf("fixture corpus %s has no routes", path)
	}
	return FixtureCorpus{
		SchemaVersion: doc.SchemaVersion,
		Contract:      doc.Contract,
		CorpusID:      doc.CorpusID,
		FixtureApp:    doc.FixtureApp,
		Authoring:     doc.Authoring,
		Routes:        routes,
	}, nil
}

func filterRoutes(routes []FixtureSpec, keep []string) []FixtureSpec {
	if len(keep) == 0 {
		return routes
	}
	wanted := map[string]bool{}
	for _, id := range keep {
		wanted[strings.ToUpper(strings.TrimSpace(id))] = true
	}
	out := make([]FixtureSpec, 0, len(routes))
	for _, r := range routes {
		if wanted[strings.ToUpper(r.ID)] {
			out = append(out, r)
		}
	}
	return out
}

func validateCanonicalRouteSelection(routes []FixtureSpec, selected []string) error {
	if len(selected) > 0 {
		return fmt.Errorf("canonical browser baseline must run the complete O0.2 corpus; --routes is not allowed")
	}
	seen := map[string]bool{}
	for _, route := range routes {
		if seen[route.ID] {
			return fmt.Errorf("canonical browser baseline corpus has duplicate route: %s", route.ID)
		}
		seen[route.ID] = true
	}
	required := map[string]bool{}
	for _, id := range canonicalRouteIDs() {
		required[id] = true
	}
	var missing []string
	for _, id := range canonicalRouteIDs() {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("canonical browser baseline corpus missing routes: %s", strings.Join(missing, ", "))
	}
	var extra []string
	for id := range seen {
		if !required[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		return fmt.Errorf("canonical browser baseline corpus has extra routes: %s", strings.Join(extra, ", "))
	}
	return nil
}

func canonicalRequiredCounts(routeID string) (int, int) {
	switch routeID {
	case "R08", "R10":
		return 7, 15
	default:
		return 11, 21
	}
}

func hasExternalRoute(routes []FixtureSpec) bool {
	for _, route := range routes {
		if route.External {
			return true
		}
	}
	return false
}

func hasNonExternalRoute(routes []FixtureSpec) bool {
	for _, route := range routes {
		if !route.External {
			return true
		}
	}
	return false
}

func firstR10Route(routes []FixtureSpec) (FixtureSpec, bool) {
	for _, route := range routes {
		if route.ID == "R10" {
			return route, true
		}
	}
	return FixtureSpec{}, false
}

func runRouteSamples(ctx context.Context, opts BrowserBaselineOptions, plan SamplingPlan, source SourceIdentity, route FixtureSpec, external ExternalRouteEvidence, raw io.Writer, log io.Writer) ([]BrowserRawSample, error) {
	sampleOpts := opts
	sampleOpts.Timeout = effectiveRouteTimeout(opts, route)
	cold, warm := plan.ColdSamples, plan.WarmSamples
	if isSceneRoute(route) {
		cold, warm = plan.SceneColdSamples, plan.SceneWarmSamples
	}
	var out []BrowserRawSample
	totalCold := plan.PilotsDiscarded + cold
	for i := 0; i < totalCold; i++ {
		sampleCtx, cancel := context.WithTimeout(ctx, sampleOpts.Timeout)
		s, err := runSingleSample(sampleCtx, sampleOpts, source, route, external, SampleLaneProduct, "cold", i, i < plan.PilotsDiscarded, nil, log)
		cancel()
		if err != nil {
			return nil, err
		}
		if err := writeRawSample(raw, s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	warmDriver, err := newOuroborosDriver(ctx, sampleOpts, SampleLaneProduct)
	if err != nil {
		return nil, err
	}
	defer warmDriver.Close()
	primeDriver, primeCancel := warmDriver.WithOperationContext(ctx, sampleOpts.Timeout)
	if err := primeWarmRoute(primeDriver, sampleOpts, route); err != nil {
		primeCancel()
		return nil, err
	}
	primeCancel()
	totalWarm := plan.PilotsDiscarded + warm
	for i := 0; i < totalWarm; i++ {
		sampleCtx, cancel := context.WithTimeout(ctx, sampleOpts.Timeout)
		s, err := runSingleSample(sampleCtx, sampleOpts, source, route, external, SampleLaneProduct, "warm", i, i < plan.PilotsDiscarded, warmDriver, log)
		cancel()
		if err != nil {
			return nil, err
		}
		if err := writeRawSample(raw, s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if plan.Canonical {
		for i := 0; i < plan.PilotsDiscarded; i++ {
			sampleCtx, cancel := context.WithTimeout(ctx, sampleOpts.Timeout)
			s, err := runSingleSample(sampleCtx, sampleOpts, source, route, external, SampleLaneProbeOverhead, "cold", i, true, nil, log)
			cancel()
			if err != nil {
				return nil, err
			}
			if err := writeRawSample(raw, s); err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		sampleCtx, cancel := context.WithTimeout(ctx, sampleOpts.Timeout)
		s, err := runSingleSample(sampleCtx, sampleOpts, source, route, external, SampleLaneProbe, "cold", 0, false, nil, log)
		cancel()
		if err != nil {
			return nil, err
		}
		if err := writeRawSample(raw, s); err != nil {
			return nil, err
		}
		out = append(out, s)

		probeWarmDriver, err := newOuroborosDriver(ctx, sampleOpts, SampleLaneProbe)
		if err != nil {
			return nil, err
		}
		defer probeWarmDriver.Close()
		primeDriver, primeCancel := probeWarmDriver.WithOperationContext(ctx, sampleOpts.Timeout)
		if err := primeWarmRoute(primeDriver, sampleOpts, route); err != nil {
			primeCancel()
			return nil, err
		}
		primeCancel()
		for i := 0; i < plan.PilotsDiscarded; i++ {
			sampleCtx, cancel := context.WithTimeout(ctx, sampleOpts.Timeout)
			s, err := runSingleSample(sampleCtx, sampleOpts, source, route, external, SampleLaneProbeOverhead, "warm", i, true, probeWarmDriver, log)
			cancel()
			if err != nil {
				return nil, err
			}
			if err := writeRawSample(raw, s); err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		sampleCtx, cancel = context.WithTimeout(ctx, sampleOpts.Timeout)
		s, err = runSingleSample(sampleCtx, sampleOpts, source, route, external, SampleLaneProbe, "warm", 0, false, probeWarmDriver, log)
		cancel()
		if err != nil {
			return nil, err
		}
		if err := writeRawSample(raw, s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func effectiveRouteTimeout(opts BrowserBaselineOptions, route FixtureSpec) time.Duration {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if route.ID == "R10" && timeout < r10MinimumSampleTimeout {
		return r10MinimumSampleTimeout
	}
	return timeout
}

func runSingleSample(ctx context.Context, opts BrowserBaselineOptions, source SourceIdentity, route FixtureSpec, external ExternalRouteEvidence, lane SampleLane, cacheMode string, idx int, pilot bool, existing *perf.Driver, log io.Writer) (BrowserRawSample, error) {
	start := time.Now()
	d := existing
	if d == nil {
		var err error
		d, err = newOuroborosDriver(ctx, opts, lane)
		if err != nil {
			return BrowserRawSample{}, err
		}
		defer d.Close()
	}
	opDriver, opCancel := d.WithOperationContext(ctx, opts.Timeout)
	defer opCancel()
	d = opDriver

	url, err := routeURL(opts, route)
	if err != nil {
		return BrowserRawSample{}, err
	}
	artifacts := sampleArtifactPaths(opts.ArtifactRoot, route.ID, string(lane), cacheMode, idx)
	if !opts.Trace {
		artifacts.TraceRef = ""
	}
	if !opts.Coverage {
		artifacts.CoverageRef = ""
	}
	if !opts.HeapSnapshots {
		artifacts.HeapSnapshotRef = ""
	}
	if external.RouteID != "" {
		artifacts.ExternalProcessRef = filepath.Join("external", external.RouteID, "process.json")
		artifacts.ExternalEvidenceRef = filepath.Join("external", external.RouteID, "evidence.json")
		artifacts.WaterProfileRef = external.WaterProfileRef
		artifacts.PixelManifestRefs = append([]string{}, external.PixelManifestRefs...)
	}
	if refs := acceptedPixelRefsForRoute(opts, route.ID); len(refs) > 0 {
		artifacts.PixelManifestRefs = refs
	}
	console, err := perf.StartConsoleCapture(d)
	if err != nil {
		return BrowserRawSample{}, err
	}
	networkCapture, err := startNetworkCapture(d)
	if err != nil {
		return BrowserRawSample{}, err
	}
	defer networkCapture.Stop()
	var (
		pageReport     *perf.PageReport
		mem            perf.MemoryStats
		proofs         ProofBundle
		interactionErr error
		heapErr        error
	)
	measured := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := setProbeRoutePhase(d, route.ID, "route-load"); err != nil {
			return err
		}
		if err := d.Navigate(url); err != nil {
			return err
		}
		if err := setProbeRoutePhase(d, route.ID, "route-load"); err != nil {
			return err
		}
		if err := d.WaitReady(); err != nil {
			return err
		}
		if err := d.Evaluate(`new Promise(r => setTimeout(r, 150))`, nil); err != nil {
			return err
		}
		if err := setProbePhase(d, "input"); err != nil {
			return err
		}
		proofs, interactionErr = executeRouteStateMachine(d, route, external, lane)
		if isSceneRoute(route) {
			waitScene(d, 60)
		}
		if err := setProbePhase(d, "telemetry"); err != nil {
			return err
		}
		var err error
		pageReport, err = perf.CollectPageReport(d, url)
		if err != nil {
			return err
		}
		mem, err = perf.QueryMemoryStats(d)
		if err != nil {
			return err
		}
		if opts.HeapSnapshots {
			snap, err := perf.TakeHeapSnapshotAfterGC(d)
			if err != nil {
				heapErr = err
			} else {
				if err := os.MkdirAll(filepath.Dir(artifacts.HeapSnapshotRef), 0o755); err != nil {
					heapErr = err
				} else if err := os.WriteFile(artifacts.HeapSnapshotRef, snap, 0o644); err != nil {
					heapErr = err
				}
			}
		}
		return nil
	}
	var traceBytes []byte
	runMeasured := measured
	if opts.Trace {
		runMeasured = func() error {
			var err error
			traceBytes, err = perf.CaptureTrace(d, measured)
			return err
		}
	}
	var coverage []perf.CoverageEntry
	if opts.Coverage {
		coverage, err = perf.CaptureCoverage(d, runMeasured)
	} else {
		err = runMeasured()
	}
	if err != nil {
		return BrowserRawSample{}, err
	}
	if len(traceBytes) > 0 {
		if err := os.MkdirAll(filepath.Dir(artifacts.TraceRef), 0o755); err != nil {
			return BrowserRawSample{}, err
		}
		if err := os.WriteFile(artifacts.TraceRef, traceBytes, 0o644); err != nil {
			return BrowserRawSample{}, err
		}
	}
	if len(coverage) > 0 {
		if err := os.MkdirAll(filepath.Dir(artifacts.CoverageRef), 0o755); err != nil {
			return BrowserRawSample{}, err
		}
		if err := WriteJSONFile(artifacts.CoverageRef, coverage); err != nil {
			return BrowserRawSample{}, err
		}
	}

	consoleEntries := console.Entries()
	pageReport.ConsoleEntries = consoleEntries

	var drain *RuntimeJSONRawDrain
	var probe []ProbeEvent
	if lane != SampleLaneProduct {
		drain = collectRuntimeJSONDrain(d)
		probe = drainEvents(drain)
	}
	netRecords := networkCapture.Records()
	traceSummary := summarizeTrace(relTo(opts.ArtifactRoot, artifacts.TraceRef), traceBytes)
	coverageSummary := summarizeCoverage(relTo(opts.ArtifactRoot, artifacts.CoverageRef), coverage)
	sample := BrowserRawSample{
		SchemaVersion:    BrowserBaselineSchemaVersion,
		Contract:         ContractO02,
		Kind:             "browser-sample",
		RunMode:          opts.Samples,
		RouteID:          route.ID,
		Route:            route.Route,
		URL:              url,
		SampleLane:       lane,
		CacheMode:        cacheMode,
		SampleIndex:      idx,
		Pilot:            pilot,
		Discarded:        pilot,
		StartedAt:        start.UTC().Format(time.RFC3339Nano),
		DurationMs:       float64(time.Since(start).Microseconds()) / 1000,
		Source:           source,
		Artifacts:        relativeArtifacts(opts.ArtifactRoot, artifacts),
		Page:             pageReport,
		Proofs:           proofs,
		Trace:            traceSummary,
		Coverage:         coverageSummary,
		Memory:           mem,
		Console:          consoleEntries,
		Network:          netRecords,
		ProbeEvents:      probe,
		RuntimeJSONDrain: drain,
		Metrics:          sampleMetrics(pageReport, mem),
	}
	sample.Metrics["durationMs"] = sample.DurationMs
	if proofs.FailClosed {
		sample.Errors = append(sample.Errors, "missing required route proof")
	}
	for _, externalErr := range external.Errors {
		sample.Errors = append(sample.Errors, "external evidence: "+externalErr)
	}
	if interactionErr != nil {
		sample.Errors = append(sample.Errors, "interaction: "+interactionErr.Error())
	}
	if heapErr != nil {
		sample.Errors = append(sample.Errors, "heap snapshot: "+heapErr.Error())
	}
	if len(consoleEntries) > 0 {
		sample.Errors = append(sample.Errors, "console entries captured")
	}
	logCommand(log, "sample", []string{route.ID, string(lane), cacheMode, fmt.Sprint(idx), url})
	return sample, nil
}

func newOuroborosDriver(ctx context.Context, opts BrowserBaselineOptions, lane SampleLane) (*perf.Driver, error) {
	d, err := perf.New(perf.WithHeadless(opts.Headless), perf.WithTimeout(0), perf.WithRemoteWebSocketURL(opts.ChromeWebSocketURL))
	if err != nil {
		return nil, err
	}
	if err := bindDriverTarget(ctx, d, opts.Timeout); err != nil {
		d.Close()
		return nil, err
	}
	setupDriver, setupCancel := d.WithOperationContext(ctx, opts.Timeout)
	defer setupCancel()
	if lane == SampleLaneProbe || lane == SampleLaneProbeOverhead {
		runtimeJSONProbe, err := RuntimeJSONProbeScript(opts.RuntimeProbeNames)
		if err != nil {
			d.Close()
			return nil, err
		}
		if err := injectPreloadScript(setupDriver, runtimeJSONProbe); err != nil {
			d.Close()
			return nil, err
		}
	}
	if opts.ViewportWidth > 0 && opts.ViewportHeight > 0 {
		if err := setupDriver.Run(emulation.SetDeviceMetricsOverride(int64(opts.ViewportWidth), int64(opts.ViewportHeight), opts.DPR, false)); err != nil {
			d.Close()
			return nil, err
		}
	}
	return d, nil
}

func bindDriverTarget(ctx context.Context, d *perf.Driver, timeout time.Duration) error {
	watchCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		watchCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	done := make(chan struct{})
	go func() {
		select {
		case <-watchCtx.Done():
			d.Close()
		case <-done:
		}
	}()
	err := d.BindTarget()
	close(done)
	return err
}

func setProbePhase(d *perf.Driver, phase string) error {
	return d.Evaluate(fmt.Sprintf(`(function(){
		var p = window.__gosxOuroborosProbe;
		if (!p) return;
		if (p.setPhase) p.setPhase(%q);
		if (p.refresh) p.refresh();
	})()`, phase), nil)
}

func setProbeRoutePhase(d *perf.Driver, routeID, phase string) error {
	return d.Evaluate(fmt.Sprintf(`(function(){
		var p = window.__gosxOuroborosProbe;
		if (!p) return;
		if (p.setRoute) p.setRoute(%q);
		if (p.setPhase) p.setPhase(%q);
		if (p.refresh) p.refresh();
	})()`, routeID, phase), nil)
}

func primeWarmRoute(d *perf.Driver, opts BrowserBaselineOptions, route FixtureSpec) error {
	url, err := routeURL(opts, route)
	if err != nil {
		return err
	}
	if err := d.Navigate(url); err != nil {
		return err
	}
	if err := d.WaitReady(); err != nil {
		return err
	}
	return d.Evaluate(`new Promise(r => setTimeout(r, 200))`, nil)
}

func routeURL(opts BrowserBaselineOptions, route FixtureSpec) (string, error) {
	if route.External {
		if opts.DocsBaseURL == "" {
			return "", fmt.Errorf("external route %s requires --docs-base-url", route.ID)
		}
		routePath := route.Route
		if idx := strings.Index(routePath, ":"); idx > 0 {
			routePath = routePath[idx+1:]
		}
		return strings.TrimRight(opts.DocsBaseURL, "/") + "/" + strings.TrimLeft(routePath, "/"), nil
	}
	if strings.HasPrefix(route.Route, "http://") || strings.HasPrefix(route.Route, "https://") {
		return route.Route, nil
	}
	routePath := route.Route
	if idx := strings.Index(routePath, ":"); idx > 0 {
		routePath = routePath[idx+1:]
	}
	if strings.Contains(routePath, " -> ") {
		routePath = strings.Split(routePath, " -> ")[0]
	}
	return strings.TrimRight(opts.BaseURL, "/") + "/" + strings.TrimLeft(routePath, "/"), nil
}

func collectR10ExternalEvidence(ctx context.Context, opts BrowserBaselineOptions, route FixtureSpec, docsServer *fixtureServer, log io.Writer) ExternalRouteEvidence {
	ev := ExternalRouteEvidence{
		RouteID:    route.ID,
		FixtureApp: route.FixtureApp,
		BaseURL:    opts.DocsBaseURL,
		Canonical:  opts.Samples == "baseline",
		Raw:        map[string]any{},
	}
	if docsServer != nil {
		ev.Process = ExternalProcessIdentity{
			Managed:   true,
			PID:       docsServer.pid,
			Command:   strings.Join(docsServer.args, " "),
			Directory: docsServer.dir,
			BaseURL:   docsServer.baseURL,
		}
	} else {
		ev.Process = ExternalProcessIdentity{Managed: false, BaseURL: opts.DocsBaseURL}
	}
	root := filepath.Join(opts.ArtifactRoot, "external", route.ID)
	_ = WriteJSONFile(filepath.Join(root, "process.json"), ev.Process)
	url, err := routeURL(opts, route)
	if err != nil {
		ev.Errors = append(ev.Errors, err.Error())
		_ = WriteJSONFile(filepath.Join(root, "evidence.json"), ev)
		return ev
	}
	ev.URL = url
	if strings.TrimSpace(opts.DocsBaseURL) == "" {
		ev.Errors = append(ev.Errors, "missing docs base URL")
	}

	profileRef, profileErr := runWaterProfileEvidence(ctx, opts, route, url, log)
	if profileErr != nil {
		ev.Errors = append(ev.Errors, sanitizeArtifactTextForOptions(opts, profileErr.Error()))
	} else {
		ev.WaterProfileRef = profileRef
	}

	if opts.Samples == "baseline" {
		ev.PixelManifestRefs = acceptedPixelRefsForRoute(opts, route.ID)
	}
	_ = WriteJSONFile(filepath.Join(root, "evidence.json"), ev)
	return ev
}

func runWaterProfileEvidence(ctx context.Context, opts BrowserBaselineOptions, route FixtureSpec, url string, log io.Writer) (string, error) {
	script := filepath.Join(opts.RepoRoot, "scripts", "water-profile-evidence.mjs")
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("water profile script missing: %w", err)
	}
	outDir := filepath.Join(opts.ArtifactRoot, "external", route.ID, "water-profile")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	timeout := opts.Timeout
	if timeout < 60*time.Second {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	args := []string{
		"scripts/water-profile-evidence.mjs",
		"--url", url,
		"--out-dir", outDir,
		"--timeout-ms", fmt.Sprint(timeout.Milliseconds()),
		"--width", fmt.Sprint(opts.ViewportWidth),
		"--height", fmt.Sprint(opts.ViewportHeight),
	}
	node := resolveNodeExecutable()
	cmd := exec.CommandContext(runCtx, node, args...)
	cmd.Dir = opts.RepoRoot
	cmd.Stdout = log
	cmd.Stderr = log
	logCommand(log, safeCommandLabel(node), args)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("water profile evidence failed: %w", err)
	}
	reportPath := filepath.Join(outDir, "water-profile-evidence.json")
	body, err := os.ReadFile(reportPath)
	if err != nil {
		return "", fmt.Errorf("water profile report missing: %w", err)
	}
	var report struct {
		Validation struct {
			Passed   bool     `json:"passed"`
			Failures []string `json:"failures"`
		} `json:"validation"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return "", fmt.Errorf("water profile report invalid: %w", err)
	}
	if !report.Validation.Passed {
		return "", fmt.Errorf("water profile evidence failed validation: %s", strings.Join(report.Validation.Failures, "; "))
	}
	return relTo(opts.ArtifactRoot, reportPath), nil
}

func acceptedPixelRefsForRoute(opts BrowserBaselineOptions, routeID string) []string {
	var out []string
	for _, ref := range splitEvidenceRefs(opts.PixelManifest) {
		path, err := resolveEvidenceRef(opts.EvidenceRoot, ref)
		if err != nil {
			continue
		}
		manifest, err := readStrictPixelManifest(path)
		if err != nil {
			continue
		}
		if manifest.RouteID == routeID {
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return out
}

func executeRouteStateMachine(d *perf.Driver, route FixtureSpec, external ExternalRouteEvidence, lane SampleLane) (ProofBundle, error) {
	var raw routeProofResult
	if err := d.Evaluate(routeStateMachineJS(route, lane), &raw); err != nil {
		return ProofBundle{
			Required:        route.RequiredInteractions,
			FailClosed:      true,
			MissingRequired: []string{"route state machine evaluation"},
			FirstUsable:     ProofPayload{Name: route.ID, OK: false, Message: err.Error()},
		}, err
	}
	bundle := raw.toBundle(route)
	if route.ID == "R10" {
		if bundle.RoutePlan == nil {
			bundle.RoutePlan = map[string]any{}
		}
		bundle.RoutePlan["externalEvidence"] = map[string]any{
			"url":               external.URL,
			"baseURL":           external.BaseURL,
			"waterProfileRef":   external.WaterProfileRef,
			"pixelManifestRefs": external.PixelManifestRefs,
			"errors":            external.Errors,
		}
		bundle.MissingRequired = append(bundle.MissingRequired, r10ExternalMissing(external)...)
	}
	if len(bundle.MissingRequired) > 0 {
		bundle.FailClosed = true
	}
	if !bundle.FirstUsable.OK {
		bundle.FailClosed = true
	}
	if bundle.FailClosed {
		return bundle, fmt.Errorf("route %s proof failed: %s", route.ID, strings.Join(bundle.MissingRequired, ", "))
	}
	return bundle, nil
}

func r10ExternalMissing(external ExternalRouteEvidence) []string {
	var missing []string
	for _, externalErr := range external.Errors {
		missing = append(missing, "externalEvidence:"+externalErr)
	}
	if external.URL == "" || external.BaseURL == "" {
		missing = append(missing, "external docs fixture")
	}
	if external.WaterProfileRef == "" {
		missing = append(missing, "water profile evidence")
	}
	if external.Canonical && len(external.PixelManifestRefs) < 2 {
		missing = append(missing, "webgpu/webgl pixel evidence")
	}
	return uniqueStrings(missing)
}

func preflightCanonicalBrowserInputs(ctx context.Context, opts BrowserBaselineOptions, routes []FixtureSpec) (bool, error) {
	if err := validateCanonicalEvidenceRoot(opts); err != nil {
		return false, err
	}
	root, err := resolveRepoRootForEvidence(opts.RepoRoot)
	if err != nil {
		return false, err
	}
	plan, err := PredictCanonicalInventoryMaterialization(ctx, root, opts.InventoryPath, opts.ArtifactRoot)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(opts.SourceIdentityPath) != "" {
		handoff, err := ReadSourceIdentityHandoffStrict(opts.SourceIdentityPath)
		if err != nil {
			return false, err
		}
		if err := ValidateSourceIdentityHandoffForMaterialization(handoff, plan); err != nil {
			return false, err
		}
	}
	source := sourceIdentityFromMaterialization(plan)
	preseed, err := validateCanonicalBrowserRootPreseed(ctx, opts, plan, source, routes)
	if err != nil {
		return false, err
	}
	if err := validateCanonicalEvidenceRefs(opts, source); err != nil {
		return false, err
	}
	return preseed, nil
}

func sourceIdentityFromMaterialization(plan CanonicalInventoryMaterialization) SourceIdentity {
	inv := plan.Inventory
	probeNames := RuntimeJSONKnownProductionNames(inv)
	return SourceIdentity{
		BaseRevision:                inv.BaseRevision,
		OverlayHash:                 inv.OverlayHash,
		TrackedDiffHash:             inv.Overlay.TrackedDiffHash,
		UntrackedIncludedSourceHash: hashUntracked(inv.Overlay.UntrackedSources),
		InventoryRef:                plan.InventoryRef,
		InventorySHA256:             plan.SHA256,
		RejectsModuleCacheMismatch:  true,
		CurrentOverlayVerified:      true,
		StrictInventory:             true,
		ReconstructionProof:         true,
		Reconstruction: &ReconstructionEvidence{
			Method:              inventoryReconstructionMethod,
			BaseRevision:        inv.BaseRevision,
			ObservedOverlayHash: inv.OverlayHash,
			UntrackedCount:      len(inv.Overlay.UntrackedSources),
			Isolated:            true,
			Applied:             true,
			Verified:            true,
		},
		RuntimeProbeNameCount: len(probeNames),
		RuntimeProbeNames:     probeNames,
	}
}

func validateCanonicalEvidenceRoot(opts BrowserBaselineOptions) error {
	if strings.TrimSpace(opts.EvidenceRoot) == "" || samePath(opts.EvidenceRoot, opts.ArtifactRoot) {
		return fmt.Errorf("canonical run requires --evidence-root separate from the output artifact root")
	}
	return nil
}

func revalidateCanonicalBrowserPreseedArtifacts(ctx context.Context, opts BrowserBaselineOptions, inventoryPath string, routes []FixtureSpec, source SourceIdentity) error {
	root, err := resolveRepoRootForEvidence(opts.RepoRoot)
	if err != nil {
		return err
	}
	plan, err := PredictCanonicalInventoryMaterialization(ctx, root, inventoryPath, opts.ArtifactRoot)
	if err != nil {
		return err
	}
	predicted := sourceIdentityFromMaterialization(plan)
	if !canonicalBrowserSourceIdentityMatches(source, predicted) {
		return fmt.Errorf("canonical browser preseed revalidation source mismatch")
	}
	if err := validateCanonicalBrowserMaterializedInventory(ctx, plan); err != nil {
		return err
	}
	if err := validateCanonicalBrowserRuntimePreseed(opts, filepath.Join(plan.ArtifactRoot, "wasm", "runtime-artifacts.json"), source); err != nil {
		return err
	}
	if err := validateCanonicalBrowserSizePreseed(plan.RepoRoot, filepath.Join(plan.ArtifactRoot, "size", "route-assets.json"), source, routes); err != nil {
		return err
	}
	return nil
}

func validateCanonicalBrowserRootPreseed(ctx context.Context, opts BrowserBaselineOptions, plan CanonicalInventoryMaterialization, source SourceIdentity, routes []FixtureSpec) (bool, error) {
	root := plan.ArtifactRoot
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("canonical browser preflight requires exact preseed root: %s", root)
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("canonical browser preseed root must not be a symlink: %s", root)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("canonical browser preseed root must be a directory: %s", root)
	}
	allowedExact, allowedDirs, err := canonicalBrowserAllowedPreseedPaths(root, plan)
	if err != nil {
		return false, err
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("canonical browser preseed rejects symlink: %s", rel)
		}
		if canonicalBrowserOwnedPreseedPath(rel) {
			return fmt.Errorf("canonical browser preseed contains browser-owned artifact: %s", rel)
		}
		if entry.IsDir() {
			if !allowedDirs[rel] && !underAllowedPreseedDir(rel, allowedDirs) {
				return fmt.Errorf("canonical browser preseed contains unknown directory: %s", rel)
			}
			return nil
		}
		if !allowedExact[rel] && !underAllowedPreseedDir(rel, allowedDirs) {
			return fmt.Errorf("canonical browser preseed contains unknown file: %s", rel)
		}
		return nil
	}); err != nil {
		return false, err
	}
	if err := validateCanonicalBrowserMaterializedInventory(ctx, plan); err != nil {
		return false, err
	}
	if err := validateCanonicalBrowserRuntimePreseed(opts, filepath.Join(root, "wasm", "runtime-artifacts.json"), source); err != nil {
		return false, err
	}
	if err := validateCanonicalBrowserSizePreseed(plan.RepoRoot, filepath.Join(root, "size", "route-assets.json"), source, routes); err != nil {
		return false, err
	}
	return true, nil
}

func canonicalBrowserAllowedPreseedPaths(root string, plan CanonicalInventoryMaterialization) (map[string]bool, map[string]bool, error) {
	exact := map[string]bool{
		"source/source-inventory.json": true,
		"wasm/runtime-artifacts.json":  true,
		"size/route-assets.json":       true,
	}
	dirs := map[string]bool{
		"source": true,
		"wasm":   true,
		"size":   true,
	}
	for _, ref := range []string{plan.Inventory.Overlay.PatchPath, plan.Inventory.Overlay.ArchivePath} {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		rel, err := canonicalBrowserPreseedRefRel(plan, ref)
		if err != nil {
			return nil, nil, err
		}
		if !strings.HasPrefix(rel, "source/") {
			return nil, nil, fmt.Errorf("canonical browser preseed overlay ref must stay under source/: %s", rel)
		}
		dirs[filepath.ToSlash(filepath.Dir(rel))] = true
		if strings.HasSuffix(ref, "untracked-sources") {
			dirs[rel] = true
			continue
		}
		exact[rel] = true
	}
	if runtimeEvidence, err := readCanonicalBrowserRuntimeEvidenceStrict(root, filepath.Join(root, "wasm", "runtime-artifacts.json")); err == nil {
		for _, variant := range runtimeEvidence.Variants {
			if variant.Generation != "current" || variant.Status != "measured" {
				continue
			}
			rel, err := canonicalBrowserPreseedFileRel(root, variant.SourcePath)
			if err != nil {
				continue
			}
			exact[rel] = true
			dirs[filepath.ToSlash(filepath.Dir(rel))] = true
		}
	}
	if sizeEvidence, err := readCanonicalBrowserSizeEvidenceStrict(root, filepath.Join(root, "size", "route-assets.json")); err == nil {
		for _, ref := range []string{sizeEvidence.ManifestPath, sizeEvidence.ExportPath} {
			if strings.TrimSpace(ref) == "" {
				continue
			}
			rel, err := canonicalBrowserPreseedFileRel(root, ref)
			if err != nil {
				continue
			}
			exact[rel] = true
			dirs[filepath.ToSlash(filepath.Dir(rel))] = true
		}
	}
	return exact, dirs, nil
}

func canonicalBrowserPreseedRefRel(plan CanonicalInventoryMaterialization, ref string) (string, error) {
	path := filepath.FromSlash(ref)
	if !filepath.IsAbs(path) {
		path = filepath.Join(plan.RepoRoot, path)
	}
	rel, err := filepath.Rel(plan.ArtifactRoot, filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("canonical browser preseed overlay ref escapes artifact root: %s", ref)
	}
	return filepath.ToSlash(rel), nil
}

func underAllowedPreseedDir(rel string, dirs map[string]bool) bool {
	for dir := range dirs {
		if strings.HasPrefix(rel, dir+"/") && strings.HasPrefix(dir, "source/") {
			return true
		}
	}
	return false
}

func canonicalBrowserOwnedPreseedPath(rel string) bool {
	switch rel {
	case "manifest.json", "commands.log", "environment.json", "failure.json",
		"perf", "summaries", "traces", "coverage", "heaps", "dynamic":
		return true
	}
	for _, prefix := range []string{"perf/", "summaries/", "traces/", "coverage/", "heaps/", "dynamic/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func validateCanonicalBrowserMaterializedInventory(ctx context.Context, plan CanonicalInventoryMaterialization) error {
	file, err := canonicalBrowserOpenRegularPreseedFile(plan.ArtifactRoot, plan.Path, "source/source-inventory.json")
	if err != nil {
		return fmt.Errorf("canonical browser preseed requires source/source-inventory.json: %w", err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("canonical browser preseed source inventory read: %w", err)
	}
	if !bytes.Equal(body, plan.Bytes) {
		return fmt.Errorf("canonical browser preseed source inventory differs from predicted materialization")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("canonical browser preseed source inventory invalid: %w", err)
	}
	existing, err := DecodeInventoryStrict(file)
	if err != nil {
		return fmt.Errorf("canonical browser preseed source inventory invalid: %w", err)
	}
	if err := requireInventoryReplay(ctx, plan.RepoRoot, existing); err != nil {
		return fmt.Errorf("canonical browser preseed source inventory invalid: %w", err)
	}
	return nil
}

func validateCanonicalBrowserRuntimePreseed(opts BrowserBaselineOptions, path string, source SourceIdentity) error {
	repoRoot := opts.RepoRoot
	ev, err := readCanonicalBrowserRuntimeEvidenceStrict(opts.ArtifactRoot, path)
	if err != nil {
		return fmt.Errorf("canonical browser preseed requires wasm/runtime-artifacts.json: %w", err)
	}
	if ev.SchemaVersion != SchemaVersion {
		return fmt.Errorf("canonical browser preseed runtime schemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
	}
	if ev.Contract != ContractO02 {
		return fmt.Errorf("canonical browser preseed runtime contractVersion = %q, want %q", ev.Contract, ContractO02)
	}
	if err := validateRuntimeEvidenceForCompare(ev); err != nil {
		return fmt.Errorf("canonical browser preseed runtime evidence invalid: %w", err)
	}
	if err := validateCanonicalBrowserRuntimeToolStatuses(opts, ev); err != nil {
		return fmt.Errorf("canonical browser preseed runtime evidence invalid: %w", err)
	}
	if err := validateCanonicalBrowserFutureRuntimeRows(ev); err != nil {
		return fmt.Errorf("canonical browser preseed runtime evidence invalid: %w", err)
	}
	recomputedBuildInput, err := BuildInputEvidenceForRepo(repoRoot, "", "")
	if err != nil {
		return fmt.Errorf("canonical browser preseed runtime build input recompute: %w", err)
	}
	if !canonicalBrowserBuildInputMatches(ev.BuildInput, recomputedBuildInput) {
		return fmt.Errorf("canonical browser preseed runtime build input mismatch")
	}
	if err := validateCanonicalBrowserMeasuredRuntimeRows(repoRoot, ev); err != nil {
		return fmt.Errorf("canonical browser preseed runtime evidence invalid: %w", err)
	}
	if !canonicalBrowserSourceIdentityMatches(ev.Source, source) {
		return fmt.Errorf("canonical browser preseed runtime source mismatch")
	}
	return nil
}

func readCanonicalBrowserRuntimeEvidenceStrict(root, path string) (*RuntimeBuildEvidence, error) {
	file, err := canonicalBrowserOpenRegularPreseedFile(root, path, "wasm/runtime-artifacts.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out RuntimeBuildEvidence
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("read runtime build evidence: %w", err)
	}
	if err := rejectTrailingJSON(dec); err != nil {
		return nil, fmt.Errorf("read runtime build evidence: %w", err)
	}
	return &out, nil
}

func readCanonicalBrowserSizeEvidenceStrict(root, path string) (*SizeEvidence, error) {
	file, err := canonicalBrowserOpenRegularPreseedFile(root, path, "size/route-assets.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out SizeEvidence
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("read size evidence: %w", err)
	}
	if err := rejectTrailingJSON(dec); err != nil {
		return nil, fmt.Errorf("read size evidence: %w", err)
	}
	return &out, nil
}

func canonicalBrowserOpenRegularPreseedFile(root, path, label string) (*os.File, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootAbs, cleanPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%s escapes preseed root", label)
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		return nil, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("preseed root must be a real directory")
	}
	current := rootAbs
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("%s parent directory must be real: %s", label, filepath.ToSlash(part))
		}
	}
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("%s changed while opening", label)
	}
	return file, nil
}

type canonicalBrowserToolVerifier struct {
	paths map[string]canonicalBrowserTrustedTool
	env   []string
}

type canonicalBrowserTrustedTool struct {
	name     string
	livePath string
	resolved string
}

func validateCanonicalBrowserRuntimeToolStatuses(opts BrowserBaselineOptions, ev *RuntimeBuildEvidence) error {
	verifier, err := newCanonicalBrowserToolVerifier(opts, ev)
	if err != nil {
		return err
	}
	actualGo, err := verifier.status("go", ev.GoVersion, "version")
	if err != nil {
		return err
	}
	if !canonicalBrowserToolStatusMatches(ev.GoVersion, actualGo) {
		return fmt.Errorf("go tool status mismatch")
	}
	actualTinyGo, err := verifier.status("tinygo", ev.TinyGo, "version")
	if err != nil {
		return err
	}
	if !canonicalBrowserToolStatusMatches(ev.TinyGo, actualTinyGo) {
		return fmt.Errorf("tinygo tool status mismatch")
	}
	if ev.WasmOpt.Available {
		actual, err := verifier.status("wasm-opt", ev.WasmOpt, "--version")
		if err != nil {
			return err
		}
		if !canonicalBrowserToolStatusMatches(ev.WasmOpt, actual) {
			return fmt.Errorf("wasm-opt tool status mismatch")
		}
		return nil
	}
	if ev.WasmOpt.Name != "wasm-opt" || ev.WasmOpt.Path != "" || ev.WasmOpt.Version != "" || ev.WasmOpt.GOROOT != "" || ev.WasmOpt.TinyGoRoot != "" || len(ev.WasmOpt.Environment) != 0 || strings.TrimSpace(ev.WasmOpt.Error) == "" {
		return fmt.Errorf("unavailable wasm-opt tool status mismatch")
	}
	if _, err := exec.LookPath("wasm-opt"); err == nil {
		return fmt.Errorf("unavailable wasm-opt is present on PATH")
	}
	return nil
}

func newCanonicalBrowserToolVerifier(opts BrowserBaselineOptions, ev *RuntimeBuildEvidence) (*canonicalBrowserToolVerifier, error) {
	names := []string{"go", "tinygo"}
	if ev.WasmOpt.Available {
		names = append(names, "wasm-opt")
	}
	paths := map[string]canonicalBrowserTrustedTool{}
	var dirs []string
	seenDir := map[string]bool{}
	for _, name := range names {
		livePath, err := exec.LookPath(name)
		if err != nil {
			return nil, fmt.Errorf("%s live tool lookup: %w", name, err)
		}
		live, err := canonicalBrowserResolveTrustedLiveTool(opts, name, livePath)
		if err != nil {
			return nil, err
		}
		recorded := canonicalBrowserRecordedToolStatus(ev, name)
		if err := canonicalBrowserRecordedToolMatchesTrusted(opts, name, recorded, live); err != nil {
			return nil, err
		}
		paths[name] = live
		dir := filepath.Dir(live.resolved)
		if !seenDir[dir] {
			seenDir[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return &canonicalBrowserToolVerifier{
		paths: paths,
		env:   canonicalBrowserMinimalToolEnv(dirs),
	}, nil
}

func canonicalBrowserRecordedToolStatus(ev *RuntimeBuildEvidence, name string) ToolStatus {
	switch name {
	case "go":
		return ev.GoVersion
	case "tinygo":
		return ev.TinyGo
	case "wasm-opt":
		return ev.WasmOpt
	default:
		return ToolStatus{Name: name}
	}
}

func canonicalBrowserResolveTrustedLiveTool(opts BrowserBaselineOptions, name, path string) (canonicalBrowserTrustedTool, error) {
	resolved, err := canonicalBrowserResolvedExecutablePath(name, path)
	if err != nil {
		return canonicalBrowserTrustedTool{}, err
	}
	if err := canonicalBrowserRejectToolPathInRoots(opts, name, resolved); err != nil {
		return canonicalBrowserTrustedTool{}, err
	}
	return canonicalBrowserTrustedTool{name: name, livePath: path, resolved: resolved}, nil
}

func canonicalBrowserRecordedToolMatchesTrusted(opts BrowserBaselineOptions, name string, recorded ToolStatus, live canonicalBrowserTrustedTool) error {
	if recorded.Name != name {
		return fmt.Errorf("%s tool name mismatch", name)
	}
	if !recorded.Available {
		return fmt.Errorf("%s tool is unavailable", name)
	}
	if strings.TrimSpace(recorded.Path) == "" {
		return fmt.Errorf("%s tool path is empty", name)
	}
	if err := canonicalBrowserRejectToolPathInRoots(opts, name, recorded.Path); err != nil {
		return err
	}
	recordedResolved, err := canonicalBrowserResolvedExecutablePath(name, recorded.Path)
	if err != nil {
		return err
	}
	if err := canonicalBrowserRejectToolPathInRoots(opts, name, recordedResolved); err != nil {
		return err
	}
	liveInfo, err := os.Stat(live.resolved)
	if err != nil {
		return fmt.Errorf("%s live tool stat: %w", name, err)
	}
	recordedInfo, err := os.Stat(recordedResolved)
	if err != nil {
		return fmt.Errorf("%s recorded tool stat: %w", name, err)
	}
	if !os.SameFile(liveInfo, recordedInfo) {
		return fmt.Errorf("%s recorded tool path does not match live tool", name)
	}
	return nil
}

func canonicalBrowserResolvedExecutablePath(name, ref string) (string, error) {
	path, err := filepath.Abs(ref)
	if err != nil {
		return "", fmt.Errorf("%s tool path: %w", name, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("%s tool path: %w", name, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s tool path must not be a directory", name)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s tool path resolve: %w", name, err)
	}
	resolvedInfo, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("%s resolved tool path: %w", name, err)
	}
	if resolvedInfo.Mode()&os.ModeSymlink != 0 || !resolvedInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%s resolved tool path must be a regular file", name)
	}
	return resolved, nil
}

func canonicalBrowserRejectToolPathInRoots(opts BrowserBaselineOptions, name, ref string) error {
	for label, root := range map[string]string{
		"repo":     opts.RepoRoot,
		"artifact": opts.ArtifactRoot,
		"evidence": opts.EvidenceRoot,
	} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		inside, err := canonicalBrowserPathInsideRoot(root, ref)
		if err != nil {
			return fmt.Errorf("%s tool %s root check: %w", name, label, err)
		}
		if inside {
			return fmt.Errorf("%s tool path must not be under %s root", name, label)
		}
	}
	return nil
}

func canonicalBrowserPathInsideRoot(root, ref string) (bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	refAbs, err := filepath.Abs(ref)
	if err != nil {
		return false, err
	}
	if canonicalBrowserCleanPathInside(rootAbs, refAbs) {
		return true, nil
	}
	rootEval, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		if os.IsNotExist(err) {
			rootEval = filepath.Clean(rootAbs)
		} else {
			return false, err
		}
	}
	refEval, err := filepath.EvalSymlinks(refAbs)
	if err != nil {
		if os.IsNotExist(err) {
			refEval = filepath.Clean(refAbs)
		} else {
			return false, err
		}
	}
	return canonicalBrowserCleanPathInside(rootEval, refEval), nil
}

func canonicalBrowserCleanPathInside(root, ref string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(ref))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func (v *canonicalBrowserToolVerifier) status(name string, recorded ToolStatus, args ...string) (ToolStatus, error) {
	tool, ok := v.paths[name]
	if !ok {
		return ToolStatus{}, fmt.Errorf("%s trusted tool is unavailable", name)
	}
	out, err := canonicalBrowserCommandOutput(tool.resolved, filepath.Dir(tool.resolved), v.env, args...)
	status := ToolStatus{Name: name, Path: recorded.Path, Available: true}
	if err != nil {
		status.Available = true
		status.Error = err.Error()
		return status, nil
	}
	status.Version = out
	status.Environment = v.environment(name)
	if name == "go" {
		status.GOROOT, _ = canonicalBrowserCommandOutput(tool.resolved, filepath.Dir(tool.resolved), v.env, "env", "GOROOT")
	}
	if name == "tinygo" {
		status.TinyGoRoot, _ = canonicalBrowserCommandOutput(tool.resolved, filepath.Dir(tool.resolved), v.env, "env", "TINYGOROOT")
		status.GOROOT, _ = canonicalBrowserCommandOutput(tool.resolved, filepath.Dir(tool.resolved), v.env, "env", "GOROOT")
	}
	return status, nil
}

func canonicalBrowserCommandOutput(path, dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out canonicalBrowserCappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if out.overflow {
		return "", fmt.Errorf("tool output exceeds %d bytes", canonicalBrowserToolOutputLimit)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

type canonicalBrowserCappedBuffer struct {
	buf      bytes.Buffer
	overflow bool
}

func (b *canonicalBrowserCappedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > canonicalBrowserToolOutputLimit {
		remaining := canonicalBrowserToolOutputLimit - b.buf.Len()
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.overflow = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *canonicalBrowserCappedBuffer) String() string {
	return b.buf.String()
}

func (v *canonicalBrowserToolVerifier) environment(name string) map[string]string {
	env := map[string]string{}
	tool, ok := v.paths[name]
	if !ok {
		return nil
	}
	if name == "go" || name == "tinygo" {
		if goroot, err := canonicalBrowserCommandOutput(tool.resolved, filepath.Dir(tool.resolved), v.env, "env", "GOROOT"); err == nil && goroot != "" {
			env["GOROOT"] = goroot
		}
		if goTool, ok := v.paths["go"]; ok {
			if gowork, err := canonicalBrowserCommandOutput(goTool.resolved, filepath.Dir(goTool.resolved), v.env, "env", "GOWORK"); err == nil && gowork != "" {
				env["GOWORK"] = gowork
			}
		}
	}
	if name == "tinygo" {
		if tinyRoot, err := canonicalBrowserCommandOutput(tool.resolved, filepath.Dir(tool.resolved), v.env, "env", "TINYGOROOT"); err == nil && tinyRoot != "" {
			env["TINYGOROOT"] = tinyRoot
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func canonicalBrowserMinimalToolEnv(dirs []string) []string {
	cleaned := make([]string, 0, len(dirs))
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		cleaned = append(cleaned, dir)
	}
	env := []string{"PATH=" + strings.Join(cleaned, string(os.PathListSeparator))}
	if runtime.GOOS == "windows" {
		for _, key := range []string{"SystemRoot", "WINDIR"} {
			if value := os.Getenv(key); value != "" {
				env = append(env, key+"="+value)
			}
		}
	}
	return env
}

func canonicalBrowserToolStatusMatches(got, want ToolStatus) bool {
	return got.Name == want.Name &&
		got.Path == want.Path &&
		got.Version == want.Version &&
		got.GOROOT == want.GOROOT &&
		got.TinyGoRoot == want.TinyGoRoot &&
		got.Available == want.Available &&
		got.Error == want.Error &&
		sameStringMap(got.Environment, want.Environment)
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func validateCanonicalBrowserSizePreseed(repoRoot, path string, source SourceIdentity, routes []FixtureSpec) error {
	artifactRoot := filepath.Dir(filepath.Dir(path))
	ev, err := readCanonicalBrowserSizeEvidenceStrict(artifactRoot, path)
	if err != nil {
		return fmt.Errorf("canonical browser preseed requires size/route-assets.json: %w", err)
	}
	if ev.SchemaVersion != SchemaVersion {
		return fmt.Errorf("canonical browser preseed size schemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
	}
	if ev.Contract != ContractO02 {
		return fmt.Errorf("canonical browser preseed size contractVersion = %q, want %q", ev.Contract, ContractO02)
	}
	if !ev.Canonical {
		return fmt.Errorf("canonical browser preseed size evidence must be canonical")
	}
	if err := validateSizeEvidenceForCompare(ev, routes); err != nil {
		return fmt.Errorf("canonical browser preseed size evidence invalid: %w", err)
	}
	if err := validateCanonicalBrowserSizeEvidenceDetails(repoRoot, ev, routes); err != nil {
		return fmt.Errorf("canonical browser preseed size evidence invalid: %w", err)
	}
	if !canonicalBrowserSourceIdentityMatches(ev.Source, source) {
		return fmt.Errorf("canonical browser preseed size source mismatch")
	}
	return nil
}

func validateCanonicalBrowserMeasuredRuntimeRows(repoRoot string, ev *RuntimeBuildEvidence) error {
	outputDir, err := canonicalBrowserContainedRepoPath(repoRoot, ev.OutputDir)
	if err != nil {
		return fmt.Errorf("runtime outputDir: %w", err)
	}
	for _, variant := range ev.Variants {
		if variant.Generation != "current" || variant.Status != "measured" {
			continue
		}
		if variant.Bytes <= 0 || variant.GzipBytes <= 0 || variant.BrotliBytes <= 0 {
			return fmt.Errorf("measured runtime variant %s requires positive byte metrics", variant.ID)
		}
		if variant.SizeBytes == nil || *variant.SizeBytes <= 0 || int64(*variant.SizeBytes) != variant.Bytes {
			return fmt.Errorf("measured runtime variant %s requires coherent sizeBytes", variant.ID)
		}
		if variant.File == "" || variant.SHA256 == "" || variant.SourcePath == "" {
			return fmt.Errorf("measured runtime variant %s requires file, sourcePath, and sha256", variant.ID)
		}
		if !canonicalBrowserRuntimeVariantFileMatches(variant.ID, variant.File) {
			return fmt.Errorf("measured runtime variant %s file = %q does not match canonical target", variant.ID, variant.File)
		}
		if !validSourceIdentitySHA256("sha256:" + strings.TrimPrefix(variant.SHA256, "sha256:")) {
			return fmt.Errorf("measured runtime variant %s has invalid sha256", variant.ID)
		}
		if len(variant.BuildArgs) == 0 || len(variant.BuildTags) == 0 || variant.TinyGoVersion == "" || variant.GoVersion == "" {
			return fmt.Errorf("measured runtime variant %s requires build and tool evidence", variant.ID)
		}
		if err := validateCanonicalBrowserRuntimeBuildContract(variant); err != nil {
			return err
		}
		if variant.Shim == nil || variant.Shim.Bytes <= 0 || variant.Shim.SHA256 == "" || variant.Shim.SourcePath == "" {
			return fmt.Errorf("measured runtime variant %s requires shim evidence", variant.ID)
		}
		if err := validateCanonicalBrowserRuntimeToolBinding(ev, variant); err != nil {
			return err
		}
		path, err := canonicalBrowserRuntimeOutputPath(outputDir, variant.SourcePath)
		if err != nil {
			return fmt.Errorf("measured runtime variant %s sourcePath: %w", variant.ID, err)
		}
		if filepath.Base(path) != variant.File {
			return fmt.Errorf("measured runtime variant %s file = %q, want basename %q", variant.ID, variant.File, filepath.Base(path))
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("measured runtime variant %s sourcePath: %w", variant.ID, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("measured runtime variant %s sourcePath must be a regular file", variant.ID)
		}
		metrics, err := MetricsForFile(path)
		if err != nil {
			return fmt.Errorf("measured runtime variant %s sourcePath metrics: %w", variant.ID, err)
		}
		if metrics.SHA256 != strings.TrimPrefix(variant.SHA256, "sha256:") ||
			metrics.Bytes != variant.Bytes ||
			metrics.GzipBytes != variant.GzipBytes ||
			metrics.BrotliBytes != variant.BrotliBytes {
			return fmt.Errorf("measured runtime variant %s metrics mismatch", variant.ID)
		}
		if err := validateCanonicalBrowserShimEvidence(ev, variant); err != nil {
			return err
		}
	}
	return nil
}

func validateCanonicalBrowserFutureRuntimeRows(ev *RuntimeBuildEvidence) error {
	seen := map[string]bool{}
	for _, variant := range ev.Variants {
		if variant.Generation != "future" || variant.Status != "planned" {
			continue
		}
		seen[variant.ID] = true
		if !sameStringSlice(variant.PlannedSelectedBy, canonicalBrowserFutureRuntimeSelectedRoutes(variant.ID)) {
			return fmt.Errorf("future runtime variant %s selected routes mismatch", variant.ID)
		}
		if variant.MissingReason != "planned O1-O6 bounded TinyGo variant; no artifact exists in O0.2" {
			return fmt.Errorf("future runtime variant %s missingReason mismatch", variant.ID)
		}
		if variant.SizeBytes != nil || variant.BudgetBytes != nil || variant.File != "" || variant.SourcePath != "" || variant.SHA256 != "" ||
			variant.Bytes != 0 || variant.GzipBytes != 0 || variant.BrotliBytes != 0 || len(variant.BuildArgs) != 0 ||
			len(variant.BuildTags) != 0 || variant.TinyGoVersion != "" || variant.GoVersion != "" || variant.WasmOptVersion != "" ||
			variant.WasmOptAvailable || variant.Optimized || variant.Shim != nil {
			return fmt.Errorf("future runtime variant %s must not carry measured metadata", variant.ID)
		}
	}
	for _, id := range []string{"core", "engine", "collab", "full"} {
		if !seen[id] {
			return fmt.Errorf("future runtime variant %s missing", id)
		}
	}
	return nil
}

func canonicalBrowserFutureRuntimeSelectedRoutes(id string) []string {
	switch id {
	case "core":
		return []string{"R01", "R02", "R03", "R04", "R09A", "R09B"}
	case "engine":
		return []string{"R05", "R07", "R08", "R10"}
	case "collab":
		return []string{"R06"}
	case "full":
		return []string{}
	default:
		return nil
	}
}

func canonicalBrowserRuntimeVariantFileMatches(id, file string) bool {
	switch id {
	case "runtime":
		return file == "gosx-runtime.wasm"
	case "islands":
		return file == "gosx-runtime-islands.wasm"
	default:
		return false
	}
}

func validateCanonicalBrowserRuntimeToolBinding(ev *RuntimeBuildEvidence, variant RuntimeArtifactVariant) error {
	if ev.TinyGo.Version == "" || ev.GoVersion.Version == "" {
		return fmt.Errorf("measured runtime variant %s requires top-level tool versions", variant.ID)
	}
	if !ev.TinyGo.Available || !ev.GoVersion.Available {
		return fmt.Errorf("measured runtime variant %s requires available TinyGo and Go tools", variant.ID)
	}
	if variant.TinyGoVersion != ev.TinyGo.Version || variant.GoVersion != ev.GoVersion.Version {
		return fmt.Errorf("measured runtime variant %s tool version mismatch", variant.ID)
	}
	if variant.WasmOptVersion != ev.WasmOpt.Version || variant.WasmOptAvailable != ev.WasmOpt.Available {
		return fmt.Errorf("measured runtime variant %s wasm-opt tool mismatch", variant.ID)
	}
	if !sameStringSlice(variant.PlannedSelectedBy, canonicalBrowserRuntimeSelectedRoutes(variant.ID)) {
		return fmt.Errorf("measured runtime variant %s selected routes mismatch", variant.ID)
	}
	return nil
}

func canonicalBrowserRuntimeSelectedRoutes(id string) []string {
	switch id {
	case "runtime":
		return []string{"R05", "R06", "R07", "R08", "R10"}
	case "islands":
		return []string{"R02", "R03", "R09A", "R09B"}
	default:
		return nil
	}
}

func validateCanonicalBrowserRuntimeBuildContract(variant RuntimeArtifactVariant) error {
	if !sameStringSlice(variant.BuildTags, canonicalBrowserRuntimeBuildTags(variant.ID)) {
		return fmt.Errorf("measured runtime variant %s build tags mismatch", variant.ID)
	}
	if !sameStringSlice(variant.BuildArgs, canonicalBrowserRuntimeBuildArgs(variant)) {
		return fmt.Errorf("measured runtime variant %s build args mismatch", variant.ID)
	}
	return nil
}

func canonicalBrowserRuntimeBuildTags(id string) []string {
	switch id {
	case "runtime":
		return []string{"tinygo", "gosx_tiny_runtime"}
	case "islands":
		return []string{"tinygo", "gosx_tiny_runtime", "gosx_tiny_islands_only"}
	default:
		return nil
	}
}

func canonicalBrowserRuntimeBuildArgs(variant RuntimeArtifactVariant) []string {
	tags := strings.Join(canonicalBrowserRuntimeBuildTags(variant.ID), " ")
	return []string{
		"build",
		"-target", "wasm",
		"-tags=" + tags,
		"-no-debug",
		"-panic=trap",
		"-o", variant.SourcePath,
		"m31labs.dev/gosx/client/wasm",
	}
}

func validateCanonicalBrowserSizeEvidenceDetails(repoRoot string, ev *SizeEvidence, routes []FixtureSpec) error {
	if !validSourceIdentitySHA256(ev.BuildInput.ManifestSHA256) ||
		!validSourceIdentitySHA256(ev.BuildInput.ExportSHA256) {
		return fmt.Errorf("canonical size evidence requires manifest and export hashes")
	}
	if len(ev.Unresolved) > 0 {
		return fmt.Errorf("canonical size evidence has unresolved refs")
	}
	if ev.Totals.RouteCount != len(ev.Routes) || ev.Totals.AssetCount != len(ev.Assets) {
		return fmt.Errorf("canonical size evidence totals do not match routes/assets")
	}
	distDir, err := canonicalBrowserContainedRepoPath(repoRoot, ev.DistDir)
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
	manifestPath, err := validateCanonicalBrowserBuildInputPath(repoRoot, ev.ManifestPath, "manifestPath")
	if err != nil {
		return err
	}
	exportPath, err := validateCanonicalBrowserBuildInputPath(repoRoot, ev.ExportPath, "exportPath")
	if err != nil {
		return err
	}
	if !samePath(manifestPath, filepath.Join(distDir, "build.json")) {
		return fmt.Errorf("canonical size evidence manifestPath must be DistDir/build.json")
	}
	if !samePath(exportPath, filepath.Join(distDir, "export.json")) {
		return fmt.Errorf("canonical size evidence exportPath must be DistDir/export.json")
	}
	recomputed, err := BuildInputEvidenceForRepo(repoRoot, manifestPath, exportPath)
	if err != nil {
		return fmt.Errorf("canonical size evidence build input recompute: %w", err)
	}
	if !canonicalBrowserBuildInputMatches(ev.BuildInput, recomputed) {
		return fmt.Errorf("canonical size evidence build input mismatch")
	}
	if err := validateCanonicalBrowserSizeReplay(repoRoot, ev, distDir, manifestPath); err != nil {
		return err
	}
	wantPath := map[string]string{}
	for _, route := range routes {
		wantPath[route.ID] = route.Route
	}
	for _, route := range ev.Routes {
		if wantPath[route.ID] == "" {
			return fmt.Errorf("canonical size evidence has unexpected route %s", route.ID)
		}
		if route.Route != wantPath[route.ID] {
			return fmt.Errorf("canonical size route %s path = %q, want %q", route.ID, route.Route, wantPath[route.ID])
		}
	}
	return nil
}

func validateCanonicalBrowserSizeReplay(repoRoot string, ev *SizeEvidence, distDir, manifestPath string) error {
	exportManifest, err := loadExportEvidence(filepath.Join(distDir, "export.json"), true)
	if err != nil {
		return fmt.Errorf("canonical size evidence export replay: %w", err)
	}
	if err := validateExportHTMLAttribution(distDir, exportManifest); err != nil {
		return fmt.Errorf("canonical size evidence HTML attribution replay: %w", err)
	}
	if len(exportManifest.routes) != len(canonicalRouteIDs()) {
		return fmt.Errorf("canonical size evidence replay route count = %d, want %d", len(exportManifest.routes), len(canonicalRouteIDs()))
	}
	replayed, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: manifestPath,
		DistDir:      distDir,
		RepoRoot:     repoRoot,
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
	if !sameSizeEvidenceAssets(ev.Assets, replayed.Assets) {
		return fmt.Errorf("canonical size evidence assets mismatch")
	}
	if !sameSizeEvidenceRoutes(ev.Routes, replayed.Routes) {
		return fmt.Errorf("canonical size evidence routes mismatch")
	}
	if ev.Totals != replayed.Totals {
		return fmt.Errorf("canonical size evidence totals mismatch")
	}
	return nil
}

func sameSizeEvidenceAssets(a, b []TransferredAsset) bool {
	bodyA, errA := json.Marshal(a)
	bodyB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(bodyA, bodyB)
}

func sameSizeEvidenceRoutes(a, b []RouteAssetEvidence) bool {
	bodyA, errA := json.Marshal(a)
	bodyB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(bodyA, bodyB)
}

func validateCanonicalBrowserShimEvidence(ev *RuntimeBuildEvidence, variant RuntimeArtifactVariant) error {
	shim := variant.Shim
	info, err := os.Lstat(shim.SourcePath)
	if err != nil {
		return fmt.Errorf("measured runtime variant %s shim: %w", variant.ID, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("measured runtime variant %s shim must be a regular file", variant.ID)
	}
	metrics, err := MetricsForFile(shim.SourcePath)
	if err != nil {
		return fmt.Errorf("measured runtime variant %s shim metrics: %w", variant.ID, err)
	}
	if metrics.SHA256 != shim.SHA256 || metrics.Bytes != shim.Bytes || metrics.GzipBytes != shim.GzipBytes || metrics.BrotliBytes != shim.BrotliBytes {
		return fmt.Errorf("measured runtime variant %s shim metrics mismatch", variant.ID)
	}
	if ev.TinyGo.TinyGoRoot == "" {
		return fmt.Errorf("measured runtime variant %s shim requires tinygoRoot provenance", variant.ID)
	}
	rootEval, err := filepath.EvalSymlinks(ev.TinyGo.TinyGoRoot)
	if err != nil {
		return fmt.Errorf("measured runtime variant %s tinygoRoot: %w", variant.ID, err)
	}
	shimEval, err := filepath.EvalSymlinks(shim.SourcePath)
	if err != nil {
		return fmt.Errorf("measured runtime variant %s shim: %w", variant.ID, err)
	}
	rel, err := filepath.Rel(rootEval, shimEval)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("measured runtime variant %s shim is not under tinygoRoot", variant.ID)
	}
	publishedPath, err := canonicalBrowserRuntimeOutputPath(ev.OutputDir, "wasm_exec.js")
	if err != nil {
		return fmt.Errorf("measured runtime variant %s published shim: %w", variant.ID, err)
	}
	publishedInfo, err := os.Lstat(publishedPath)
	if err != nil {
		return fmt.Errorf("measured runtime variant %s published shim: %w", variant.ID, err)
	}
	if publishedInfo.Mode()&os.ModeSymlink != 0 || !publishedInfo.Mode().IsRegular() {
		return fmt.Errorf("measured runtime variant %s published shim must be a regular file", variant.ID)
	}
	publishedMetrics, err := MetricsForFile(publishedPath)
	if err != nil {
		return fmt.Errorf("measured runtime variant %s published shim metrics: %w", variant.ID, err)
	}
	if publishedMetrics.SHA256 != metrics.SHA256 ||
		publishedMetrics.Bytes != metrics.Bytes ||
		publishedMetrics.GzipBytes != metrics.GzipBytes ||
		publishedMetrics.BrotliBytes != metrics.BrotliBytes {
		return fmt.Errorf("measured runtime variant %s published shim metrics mismatch", variant.ID)
	}
	return nil
}

func validateCanonicalBrowserBuildInputPath(repoRoot, ref, label string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("canonical size evidence requires %s", label)
	}
	path, err := canonicalBrowserContainedRepoPath(repoRoot, ref)
	if err != nil {
		return "", fmt.Errorf("canonical size evidence %s: %w", label, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("canonical size evidence %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("canonical size evidence %s must be a regular file", label)
	}
	return path, nil
}

func canonicalBrowserBuildInputMatches(got, want BuildInputEvidence) bool {
	return got.GoSXModuleDir == want.GoSXModuleDir &&
		got.GoSXModuleVersion == want.GoSXModuleVersion &&
		got.GoWork == want.GoWork &&
		got.GoWorkSHA256 == want.GoWorkSHA256 &&
		got.GoModSHA256 == want.GoModSHA256 &&
		got.GoSumSHA256 == want.GoSumSHA256 &&
		got.ManifestSHA256 == want.ManifestSHA256 &&
		got.ExportSHA256 == want.ExportSHA256 &&
		got.RejectsModuleCacheMismatch == want.RejectsModuleCacheMismatch
}

func canonicalBrowserRuntimeOutputPath(outputDir, ref string) (string, error) {
	path := filepath.FromSlash(ref)
	if !filepath.IsAbs(path) {
		path = filepath.Join(outputDir, path)
	}
	outputEval, err := filepath.EvalSymlinks(outputDir)
	if err != nil {
		return "", err
	}
	pathEval, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(outputEval, pathEval)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes runtime outputDir: %s", ref)
	}
	return pathEval, nil
}

func canonicalBrowserContainedRepoPath(repoRoot, ref string) (string, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("path is empty")
	}
	path := filepath.FromSlash(ref)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	pathEval, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootEval, pathEval)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes repo root: %s", ref)
	}
	return pathEval, nil
}

func canonicalBrowserPreseedFileRel(root, ref string) (string, error) {
	path := filepath.FromSlash(ref)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes preseed root: %s", ref)
	}
	return filepath.ToSlash(rel), nil
}

func canonicalBrowserSourceIdentityMatches(got, want SourceIdentity) bool {
	return got.BaseRevision == want.BaseRevision &&
		got.OverlayHash == want.OverlayHash &&
		got.TrackedDiffHash == want.TrackedDiffHash &&
		got.UntrackedIncludedSourceHash == want.UntrackedIncludedSourceHash &&
		got.InventoryRef == want.InventoryRef &&
		got.InventorySHA256 == want.InventorySHA256 &&
		got.StrictInventory == want.StrictInventory &&
		got.CurrentOverlayVerified == want.CurrentOverlayVerified &&
		got.ReconstructionProof == want.ReconstructionProof
}

func validateCanonicalEvidenceRefs(opts BrowserBaselineOptions, source SourceIdentity) error {
	if err := validateCanonicalEvidenceRoot(opts); err != nil {
		return err
	}
	if source.InventoryRef == "" || strings.HasPrefix(source.InventoryRef, "..") || filepath.IsAbs(source.InventoryRef) {
		return fmt.Errorf("canonical source inventory ref must stay under artifact root: %s", source.InventoryRef)
	}
	if !strings.HasPrefix(filepath.ToSlash(source.InventoryRef), "source/") {
		return fmt.Errorf("canonical source inventory ref must use source materialization: %s", source.InventoryRef)
	}
	manifests, err := validatePixelManifestRefs(opts, source)
	if err != nil {
		return err
	}
	required := map[string]map[string]bool{
		"R08": {"webgpu": false, "webgl": false},
		"R10": {"webgpu": false, "webgl": false},
	}
	for _, manifest := range manifests {
		route := required[manifest.RouteID]
		if route == nil {
			return fmt.Errorf("canonical pixel evidence has unexpected route %s", manifest.RouteID)
		}
		if _, ok := route[manifest.BackendRequirement]; !ok {
			return fmt.Errorf("canonical pixel evidence has unexpected backend %s/%s", manifest.RouteID, manifest.BackendRequirement)
		}
		if route[manifest.BackendRequirement] {
			return fmt.Errorf("canonical pixel evidence has duplicate manifest for %s/%s", manifest.RouteID, manifest.BackendRequirement)
		}
		route[manifest.BackendRequirement] = true
	}
	var missing []string
	for _, routeID := range []string{"R08", "R10"} {
		for _, backend := range []string{"webgpu", "webgl"} {
			if !required[routeID][backend] {
				missing = append(missing, routeID+"/"+backend)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("canonical pixel evidence missing hardware manifests: %s", strings.Join(missing, ", "))
	}
	return nil
}

func writeRuntimeJSONDynamicEvidence(opts BrowserBaselineOptions, source SourceIdentity, samples []BrowserRawSample) (string, error) {
	manifest, err := buildRuntimeJSONDynamicEvidenceFromSamples(source, samples)
	if err != nil {
		return "", err
	}
	path := filepath.Join(opts.ArtifactRoot, "dynamic", "runtime-json-evidence.json")
	if err := WriteRuntimeJSONDynamicEvidenceManifest(path, manifest); err != nil {
		return "", err
	}
	return relTo(opts.ArtifactRoot, path), nil
}

func validateRuntimeJSONDynamicEvidenceSamples(source SourceIdentity, samples []BrowserRawSample) error {
	_, err := buildRuntimeJSONDynamicEvidenceFromSamples(source, samples)
	if err != nil {
		return fmt.Errorf("generated dynamic runtime JSON evidence: %w", err)
	}
	return nil
}

func buildRuntimeJSONDynamicEvidenceFromSamples(source SourceIdentity, samples []BrowserRawSample) (*RuntimeJSONDynamicEvidenceManifest, error) {
	input := RuntimeJSONDynamicEvidenceInput{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      DynamicSourceBindingFromSourceIdentity(source),
		Static:      DynamicStaticBindingFromRuntimeJSONStaticIdentity(source.RuntimeJSONStatic, source.RuntimeProbeNames),
	}
	for _, sample := range samples {
		lane, ok := runtimeJSONDynamicLaneForBrowserSample(sample)
		if !ok {
			continue
		}
		input.Samples = append(input.Samples, RuntimeJSONDynamicSampleInput{
			Lane:                lane,
			RouteID:             sample.RouteID,
			CacheMode:           sample.CacheMode,
			SampleIndex:         sample.SampleIndex,
			DurationMs:          sample.DurationMs,
			Pilot:               sample.Pilot,
			Discarded:           sample.Discarded,
			ProductPathPrefixes: productPathPrefixesForSample(sample),
			HarnessPathPrefixes: []string{"/__gosx_ouroboros_harness/"},
			ProbePathPrefixes:   []string{"/__gosx_ouroboros_probe/"},
			Drain:               sample.RuntimeJSONDrain,
		})
	}
	return BuildRuntimeJSONDynamicEvidence(input)
}

func runtimeJSONDynamicLaneForBrowserSample(sample BrowserRawSample) (string, bool) {
	switch sample.SampleLane {
	case SampleLaneProduct:
		return RuntimeJSONDynamicLaneProduct, true
	case SampleLaneProbe:
		return RuntimeJSONDynamicLaneProbe, true
	case SampleLaneProbeOverhead:
		return RuntimeJSONDynamicLaneProbeOverhead, true
	default:
		return "", false
	}
}

func productPathPrefixesForSample(sample BrowserRawSample) []string {
	var prefixes []string
	for _, record := range sample.Network {
		if record.RuntimeAssetRole == "" || record.UnresolvedAssetRole {
			continue
		}
		switch record.RuntimeAssetRole {
		case "runtime", "bootstrap":
		default:
			continue
		}
		u, err := url.Parse(record.URL)
		if err != nil || u.Path == "" {
			continue
		}
		prefixes = append(prefixes, u.EscapedPath())
	}
	return uniqueStrings(prefixes)
}

func validatePixelManifestRefs(opts BrowserBaselineOptions, source SourceIdentity) ([]visual.PixelEvidenceManifest, error) {
	manifests, err := readCanonicalPixelManifestRefs(opts)
	if err != nil {
		return nil, err
	}
	for i, manifest := range manifests {
		path, _ := resolveEvidenceRef(opts.EvidenceRoot, splitEvidenceRefs(opts.PixelManifest)[i])
		if err := validateCanonicalPixelManifest(path, manifest, opts, source); err != nil {
			return nil, err
		}
	}
	return manifests, nil
}

func readCanonicalPixelManifestRefs(opts BrowserBaselineOptions) ([]visual.PixelEvidenceManifest, error) {
	refs := splitEvidenceRefs(opts.PixelManifest)
	if len(refs) != 4 {
		return nil, fmt.Errorf("canonical run requires exactly four accepted O02-F pixel manifest refs")
	}
	seenRefs := map[string]bool{}
	var manifests []visual.PixelEvidenceManifest
	for _, ref := range refs {
		path, err := resolveEvidenceRef(opts.EvidenceRoot, ref)
		if err != nil {
			return nil, fmt.Errorf("pixel manifest %q: %w", ref, err)
		}
		if seenRefs[path] {
			return nil, fmt.Errorf("duplicate pixel manifest ref %q", ref)
		}
		seenRefs[path] = true
		manifest, err := readStrictPixelManifest(path)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

func readStrictPixelManifest(path string) (visual.PixelEvidenceManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return visual.PixelEvidenceManifest{}, fmt.Errorf("pixel manifest %s: %w", path, err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var manifest visual.PixelEvidenceManifest
	if err := dec.Decode(&manifest); err != nil {
		return visual.PixelEvidenceManifest{}, fmt.Errorf("pixel manifest %s strict decode: %w", path, err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return visual.PixelEvidenceManifest{}, fmt.Errorf("pixel manifest %s has trailing JSON", path)
	}
	return manifest, nil
}

func validateCanonicalPixelManifest(path string, manifest visual.PixelEvidenceManifest, opts BrowserBaselineOptions, source SourceIdentity) error {
	backend := visual.RequireBackend(manifest.BackendRequirement)
	if backend != visual.RequireBackendWebGPU && backend != visual.RequireBackendWebGL {
		return fmt.Errorf("pixel manifest %s backendRequirement = %q", path, manifest.BackendRequirement)
	}
	expectedSource := visual.PixelSourceIdentity{
		BaseRevision:    source.BaseRevision,
		OverlayHash:     source.OverlayHash,
		InventorySHA256: source.InventorySHA256,
	}
	_, err := visual.ValidateCanonicalPixelBaselineManifest(path, expectedSource, visual.PixelEvidenceOptions{
		Mode:           visual.PixelModeRecordBaseline,
		RouteID:        manifest.RouteID,
		Backend:        backend,
		ForceWebGL:     backend == visual.RequireBackendWebGL,
		CanvasSelector: "canvas",
		Viewport:       visual.Viewport{Width: opts.ViewportWidth, Height: opts.ViewportHeight, Scale: opts.DPR},
	})
	return err
}

func validateSceneSamplePixelManifestRefs(sample BrowserRawSample, opts BrowserBaselineOptions) error {
	if len(sample.Artifacts.PixelManifestRefs) != 2 {
		return fmt.Errorf("accepted O02-F pixel manifest refs=%d want 2", len(sample.Artifacts.PixelManifestRefs))
	}
	if strings.TrimSpace(opts.EvidenceRoot) == "" {
		return fmt.Errorf("accepted O02-F pixel manifest refs require evidence root")
	}
	seen := map[string]bool{"webgpu": false, "webgl": false}
	seenPath := map[string]bool{}
	for _, ref := range sample.Artifacts.PixelManifestRefs {
		path, err := resolveEvidenceRef(opts.EvidenceRoot, ref)
		if err != nil {
			return fmt.Errorf("pixel manifest %q: %w", ref, err)
		}
		if seenPath[path] {
			return fmt.Errorf("duplicate pixel manifest ref %q", ref)
		}
		seenPath[path] = true
		manifest, err := readStrictPixelManifest(path)
		if err != nil {
			return err
		}
		if manifest.RouteID != sample.RouteID {
			return fmt.Errorf("pixel manifest %q routeID = %s, want %s", ref, manifest.RouteID, sample.RouteID)
		}
		backend := manifest.BackendRequirement
		if _, ok := seen[backend]; !ok {
			return fmt.Errorf("pixel manifest %q backendRequirement = %s, want webgpu or webgl", ref, backend)
		}
		if seen[backend] {
			return fmt.Errorf("duplicate pixel manifest backend %s", backend)
		}
		seen[backend] = true
	}
	for _, backend := range []string{"webgpu", "webgl"} {
		if !seen[backend] {
			return fmt.Errorf("missing %s pixel manifest ref", backend)
		}
	}
	return nil
}

func splitEvidenceRefs(value string) []string {
	parts := strings.Split(value, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func resolveEvidenceRef(root, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("ref is empty")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, filepath.FromSlash(ref))
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !isSubpath(base, realPath) {
		return "", fmt.Errorf("ref escapes artifact root")
	}
	return realPath, nil
}

func containedEvidencePath(root, path string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, filepath.FromSlash(candidate))
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !isSubpath(base, candidate) {
		return "", fmt.Errorf("evidence path escapes root")
	}
	return candidate, nil
}

type routeProofResult struct {
	RouteID     string           `json:"routeID"`
	Path        string           `json:"path"`
	Marker      string           `json:"marker"`
	Capability  string           `json:"capability"`
	FirstUsable ProofPayload     `json:"firstUsable"`
	Proofs      []ProofPayload   `json:"proofs"`
	Missing     []string         `json:"missing"`
	Registry    map[string]any   `json:"registry"`
	RoutePlan   map[string]any   `json:"routePlan"`
	Navigation  map[string]any   `json:"navigation,omitempty"`
	Assertions  map[string]bool  `json:"assertions"`
	Assets      []map[string]any `json:"assets,omitempty"`
	Disallowed  []string         `json:"disallowed,omitempty"`
}

func (r routeProofResult) toBundle(route FixtureSpec) ProofBundle {
	observed := []string{}
	for _, proof := range r.Proofs {
		if proof.OK {
			observed = append(observed, proof.Name)
		}
	}
	missing := append([]string{}, r.Missing...)
	for _, assertion := range route.RoutePlanAssertions {
		if !r.Assertions[assertion] {
			missing = append(missing, "routePlanAssertion:"+assertion)
		}
	}
	for _, asset := range r.Disallowed {
		missing = append(missing, "disallowedAsset:"+asset)
	}
	if r.RouteID == "" {
		missing = append(missing, "route marker")
	}
	return ProofBundle{
		Required:        route.RequiredInteractions,
		Observed:        observed,
		FirstUsable:     r.FirstUsable,
		Interaction:     r.Proofs,
		FailClosed:      len(missing) > 0 || !r.FirstUsable.OK,
		MissingRequired: uniqueStrings(missing),
		RoutePlan:       r.RoutePlan,
		Registry:        r.Registry,
	}
}

func routeStateMachineJS(route FixtureSpec, lane SampleLane) string {
	routeID, _ := json.Marshal(route.ID)
	assertions, _ := json.Marshal(route.RoutePlanAssertions)
	disallowed, _ := json.Marshal(route.DisallowedRuntimeAssets)
	laneJSON, _ := json.Marshal(string(lane))
	return fmt.Sprintf(`(async function(){
		var routeID = %s;
		var sampleLane = %s;
			var routeAssertions = %s || [];
			var disallowed = %s || [];
		var missing = [];
		var proofs = [];
		function now(){ return performance.now ? performance.now() : 0; }
		function qs(sel){ return document.querySelector(sel); }
		function qsa(sel){ return Array.prototype.slice.call(document.querySelectorAll(sel)); }
		function text(sel){ var n = qs(sel); return n ? (n.textContent || "") : ""; }
		function proof(name, ok, payload, selector, message) {
			var p = {name:name, ok:!!ok, atMs:now(), payload:payload || {}, selector:selector || "", message:message || ""};
			proofs.push(p);
			if (!p.ok) missing.push(name);
			return p;
		}
		async function settle(ms){ await new Promise(function(r){ setTimeout(r, ms || 80); }); }
		async function waitFor(fn, timeoutMS, stepMS) {
			var deadline = now() + (timeoutMS || 2000);
			var last = false;
			while (now() <= deadline) {
				try {
					last = fn();
					if (last) return last;
				} catch (_) {
					last = false;
				}
				await settle(stepMS || 50);
			}
			return last;
		}
		async function click(sel) {
			var n = qs(sel);
			if (!n) return false;
			n.click();
			await settle(120);
			return true;
		}
		async function submitFormValid(sel, name) {
			var result = await submitFormValidResult(sel, name);
			return result.ok;
		}
		async function submitFormValidResult(sel, name) {
			var f = qs(sel || "form");
			if (!f) return {ok:false, missing:"form"};
			var value = name === undefined ? "valid" : name;
			var input = f.querySelector("input[name=name]");
			if (input) {
				input.value = value;
				input.dispatchEvent(new Event("input", {bubbles:true}));
			}
			if (window.fetch) {
				try {
					var body = new URLSearchParams();
					body.set("name", value);
					var resp = await fetch(f.getAttribute("action") || location.pathname, {method:"POST", headers:{Accept:"application/json"}, body: body});
					return {ok:resp.ok, status:resp.status || 0, redirected:!!resp.redirected, url:resp.url || ""};
				} catch (e) { return {ok:false, error:String(e && (e.message || e)).slice(0, 160)}; }
			}
			return {ok:false, missing:"fetch"};
		}
		async function submitFormStructuredResult(sel, name) {
			var f = qs(sel || "form");
			if (!f) return {ok:false, missing:"form"};
			var value = name === undefined ? "valid" : name;
			var input = f.querySelector("input[name=name]");
			if (input) {
				input.value = value;
				input.dispatchEvent(new Event("input", {bubbles:true}));
			}
			if (!window.fetch) return {ok:false, missing:"fetch"};
			try {
				var body = new URLSearchParams();
				body.set("name", value);
				var resp = await fetch(f.getAttribute("action") || location.pathname, {method:"POST", headers:{Accept:"application/json"}, body: body});
				var textBody = await resp.text();
				var parsed = null;
				try { parsed = textBody ? JSON.parse(textBody) : null; } catch (_) { parsed = null; }
				return {ok:resp.ok, status:resp.status || 0, redirected:!!resp.redirected, url:resp.url || "", json:parsed, body:textBody.slice(0, 240), accept:"application/json"};
			} catch (e) { return {ok:false, error:String(e && (e.message || e)).slice(0, 160), accept:"application/json"}; }
		}
		async function submitFormBrowserRedirectResult(sel, name) {
			var f = qs(sel || "form");
			if (!f) return {ok:false, missing:"form"};
			var value = name === undefined ? "valid" : name;
			var input = f.querySelector("input[name=name]");
			if (input) {
				input.value = value;
				input.dispatchEvent(new Event("input", {bubbles:true}));
			}
			if (!window.fetch) return {ok:false, missing:"fetch"};
			try {
				var body = new URLSearchParams();
				body.set("name", value);
				var accept = "text/html,application/xhtml+xml,*/*;q=0.8";
				var resp = await fetch(f.getAttribute("action") || location.pathname, {method:"POST", headers:{Accept:accept}, body: body, redirect:"follow"});
				return {ok:resp.ok, status:resp.status || 0, redirected:!!resp.redirected, url:resp.url || "", accept:accept};
			} catch (e) { return {ok:false, error:String(e && (e.message || e)).slice(0, 160), accept:"text/html,application/xhtml+xml,*/*;q=0.8"}; }
		}
		function applyActionHTML(result) {
			var target = qs("#action-state");
			var data = result && result.json && result.json.data;
			var html = data && typeof data.html === "string" ? data.html : "";
			if (!target || !html) return false;
			target.innerHTML = html;
			return true;
		}
		function registry() {
			var out = {};
			try {
				var g = window.__gosx || {};
				out.islands = g.islands && typeof g.islands.size === "number" ? g.islands.size : 0;
				out.engines = g.engines && typeof g.engines.size === "number" ? g.engines.size : 0;
				out.hubs = g.hubs && typeof g.hubs.size === "number" ? g.hubs.size : 0;
				out.controllers = g.controllers && typeof g.controllers.size === "number" ? g.controllers.size : 0;
			} catch (_) {}
			return out;
		}
		function runtimeAssets() {
			return performance.getEntriesByType("resource").map(function(e){
				return {name:e.name, type:e.initiatorType || "", transferSize:e.transferSize || 0};
			});
		}
		function gosxManifest() {
			var n = document.getElementById("gosx-manifest");
			if (!n) return null;
			try { return JSON.parse(n.textContent || "{}"); } catch (_) { return null; }
		}
		function videoManifestEntry() {
			var manifest = gosxManifest();
			var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
			for (var i = 0; i < engines.length; i++) {
				var engine = engines[i] || {};
				if (engine.kind === "video" && engine.component === "GoSXVideo") {
					var props = engine.props || {};
					if (typeof props === "string") {
						try { props = JSON.parse(props); } catch (_) { props = {}; }
					}
					return {engine:engine, props:props};
				}
			}
			return null;
		}
		function scene3DManifestEntry() {
			var manifest = gosxManifest();
			var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
			for (var i = 0; i < engines.length; i++) {
				var engine = engines[i] || {};
				if (engine.kind === "surface" && engine.component === "GoSXScene3D") return engine;
			}
			return null;
		}
		function sceneBackendSnapshot(sel) {
			var mount = qs(sel);
			var canvas = mount ? mount.querySelector("canvas") : null;
			var out = {ok:false, mount:!!mount, canvas:!!canvas, backend:"", renderGPU:"", truthRaw:"", truth:null, reason:"missing mount"};
			if (!mount) return out;
			if (!canvas) {
				out.reason = "missing canvas";
				return out;
			}
			out.backend = mount.getAttribute("data-gosx-scene3d-backend") || "";
			out.renderGPU = mount.getAttribute("data-gosx-scene3d-render-gpu") || "";
			out.truthRaw = mount.getAttribute("data-gosx-scene3d-render-backend-truth") || "";
			if (!out.backend || out.renderGPU !== "true" || !out.truthRaw) {
				out.reason = "backend attributes incomplete";
				return out;
			}
			try {
				out.truth = JSON.parse(out.truthRaw);
			} catch (e) {
				out.reason = "malformed backend truth";
				out.error = String(e && (e.message || e)).slice(0, 160);
				return out;
			}
			if (!out.truth || typeof out.truth !== "object") {
				out.reason = "backend truth is not an object";
				return out;
			}
			if (out.truth.backend !== out.backend) {
				out.reason = "backend mismatch";
				return out;
			}
			if (out.truth.gpu !== true) {
				out.reason = "gpu truth is false";
				return out;
			}
			if (out.truth.deviceLost === true) {
				out.reason = "device lost";
				return out;
			}
			if (out.truth.initError || out.truth.lastError) {
				out.reason = "backend error";
				return out;
			}
			out.ok = true;
			out.reason = "backend committed";
			return out;
		}
		async function waitSceneMountCanvas(sel) {
			var found = await waitFor(function(){
				var mount = qs(sel);
				var canvas = mount ? mount.querySelector("canvas") : null;
				return mount && canvas ? {mount:mount, canvas:canvas} : false;
			}, 10000, 50);
			return found || {mount:qs(sel), canvas: qs(sel) ? qs(sel).querySelector("canvas") : null};
		}
		function exactExternalR10WaterPath() {
			return routeID === "R10" && (location.pathname === "/demos/water" || location.pathname.indexOf("/demos/water/") === 0) && !qsa("[data-fixture-local-copy]").length;
		}
		function sceneTelemetrySnapshot(mount) {
			var out = {ok:false, reason:"", orbit:null, camera:null};
			if (!mount) {
				out.reason = "missing mount";
				return out;
			}
			var fn = window.__gosx_scene3d_telemetry;
			if (typeof fn !== "function") {
				out.reason = "missing telemetry";
				return out;
			}
			var raw;
			try {
				raw = fn(mount);
			} catch (e) {
				out.reason = "telemetry threw";
				out.error = String(e && (e.message || e)).slice(0, 160);
				return out;
			}
			var orbit = raw && raw.orbit;
			var camera = raw && raw.camera;
			function finite(n){ return Number.isFinite(Number(n)); }
			function vec3(value) {
				if (!value) return null;
				if (Array.isArray(value)) return {x:Number(value[0]), y:Number(value[1]), z:Number(value[2])};
				return {x:Number(value.x), y:Number(value.y), z:Number(value.z)};
			}
			function cameraPosition(value) {
				return vec3(value && value.position) || vec3(value);
			}
			function cameraRotation(value) {
				var nested = vec3(value && value.rotation);
				if (nested) return nested;
				if (!value) return null;
				return {x:Number(value.rotationX), y:Number(value.rotationY), z:Number(value.rotationZ)};
			}
			var position = cameraPosition(camera);
			var rotation = cameraRotation(camera);
			if (!orbit || !finite(orbit.yaw) || !finite(orbit.pitch) || !finite(orbit.radius)) {
				out.reason = "missing finite orbit";
				return out;
			}
			if (!position || !rotation || !finite(position.x) || !finite(position.y) || !finite(position.z) || !finite(rotation.x) || !finite(rotation.y) || !finite(rotation.z)) {
				out.reason = "missing finite camera";
				return out;
			}
			out.ok = true;
			out.reason = "telemetry ready";
			out.orbit = {yaw:Number(orbit.yaw), pitch:Number(orbit.pitch), radius:Number(orbit.radius)};
			out.camera = {position:position, rotation:rotation};
			return out;
		}
		function telemetryDelta(before, after) {
			function abs(a, b){ return Math.abs(Number(a || 0) - Number(b || 0)); }
			var orbit = {
				yaw: abs(before.orbit.yaw, after.orbit.yaw),
				pitch: abs(before.orbit.pitch, after.orbit.pitch),
				radius: abs(before.orbit.radius, after.orbit.radius)
			};
			var camera = {
				position: {
					x: abs(before.camera.position.x, after.camera.position.x),
					y: abs(before.camera.position.y, after.camera.position.y),
					z: abs(before.camera.position.z, after.camera.position.z)
				},
				rotation: {
					x: abs(before.camera.rotation.x, after.camera.rotation.x),
					y: abs(before.camera.rotation.y, after.camera.rotation.y),
					z: abs(before.camera.rotation.z, after.camera.rotation.z)
				}
			};
			var orbitMax = Math.max(orbit.yaw, orbit.pitch, orbit.radius);
			var cameraMax = Math.max(camera.position.x, camera.position.y, camera.position.z, camera.rotation.x, camera.rotation.y, camera.rotation.z);
			return {orbit:orbit, camera:camera, orbitMax:orbitMax, cameraMax:cameraMax};
		}
		async function proveSceneOrbitInput(sel) {
			var found = await waitSceneMountCanvas(sel);
			var mount = found.mount;
			var canvas = found.canvas;
			var before = sceneTelemetrySnapshot(mount);
			var out = {ok:false, handled:false, mount:!!mount, canvas:!!canvas, before:before, after:null, deltas:null, reason:before.reason || ""};
			if (!canvas || !before.ok) {
				return out;
			}
			var r = canvas.getBoundingClientRect();
			var startX = r.left + r.width / 2;
			var startY = r.top + r.height / 2;
			try {
				canvas.dispatchEvent(new PointerEvent("pointerdown", {clientX:startX, clientY:startY, bubbles:true, pointerId:1, button:0, buttons:1}));
				canvas.dispatchEvent(new PointerEvent("pointermove", {clientX:startX+72, clientY:startY+38, bubbles:true, pointerId:1, button:0, buttons:1}));
				canvas.dispatchEvent(new PointerEvent("pointermove", {clientX:startX+96, clientY:startY+54, bubbles:true, pointerId:1, button:0, buttons:1}));
				canvas.dispatchEvent(new PointerEvent("pointerup", {clientX:startX+96, clientY:startY+54, bubbles:true, pointerId:1, button:0, buttons:0}));
				out.handled = true;
			} catch (e) {
				out.reason = "dispatch failed";
				out.error = String(e && (e.message || e)).slice(0, 160);
				return out;
			}
			var changed = await waitFor(function(){
				var after = sceneTelemetrySnapshot(mount);
				if (!after.ok) return false;
				var deltas = telemetryDelta(before, after);
				if (deltas.orbitMax >= 0.01 && deltas.cameraMax >= 0.01) return {after:after, deltas:deltas};
				return false;
			}, 1500, 50);
			if (changed) {
				out.ok = true;
				out.reason = "orbit and camera changed";
				out.after = changed.after;
				out.deltas = changed.deltas;
				return out;
			}
			out.after = sceneTelemetrySnapshot(mount);
			if (out.after && out.after.ok) {
				out.deltas = telemetryDelta(before, out.after);
			}
			out.reason = "orbit or camera unchanged";
			return out;
		}
		async function waitSceneBackendCommit(sel) {
			var timeout = Number(window.__gosxOuroborosSceneBackendWaitMS || 10000);
			if (!Number.isFinite(timeout) || timeout < 1000 || timeout > 10000) timeout = 10000;
			var found = await waitFor(function(){
				var snap = sceneBackendSnapshot(sel);
				return snap.ok ? snap : false;
			}, timeout, 50);
			return found || sceneBackendSnapshot(sel);
		}
		async function sharedSelectionManifestProof() {
			var manifest = gosxManifest();
			var islands = manifest && Array.isArray(manifest.islands) ? manifest.islands : [];
			var entries = islands.filter(function(i){ return i && i.component === "SharedSelection" && i.programRef === "/_ouroboros/islands/SharedSelection.json"; });
			function containsSelectionSignal(value) {
				if (value === "$ouroboros.selection") return true;
				if (Array.isArray(value)) return value.some(containsSelectionSignal);
				if (value && typeof value === "object") {
					return Object.keys(value).some(function(k){ return k === "$ouroboros.selection" || containsSelectionSignal(value[k]); });
				}
				return false;
			}
			var programOK = false;
			if (entries.length >= 2 && window.fetch) {
				try {
					var resp = await fetch("/_ouroboros/islands/SharedSelection.json", {headers:{Accept:"application/json"}});
					var program = await resp.json();
					programOK = resp.ok && containsSelectionSignal(program);
				} catch (_) {
					programOK = false;
				}
			}
			return {ok:entries.length >= 2 && programOK, entryCount:entries.length, programOK:programOK};
		}
		function hasRuntimeAsset(pattern) {
			return runtimeAssets().some(function(a){ return a.name.indexOf(pattern) >= 0; });
		}
			function canvasPixelProof(canvas) {
				var c = canvas || qs("canvas");
				var out = {ok:false, reason:"", canvas:!!c, width:c ? c.width || 0 : 0, height:c ? c.height || 0 : 0, samples:[]};
				if (!c) {
					out.reason = "missing canvas";
					return out;
				}
				if (!c.width || !c.height) {
					out.reason = "zero canvas dimensions";
					return out;
				}
				if (typeof c.getContext !== "function") {
					out.reason = "missing getContext";
					return out;
				}
				try {
					var ctx = c.getContext("2d");
					var points = [
						[0.50, 0.50],
						[0.25, 0.25],
						[0.75, 0.25],
						[0.25, 0.75],
						[0.75, 0.75]
					];
					if (ctx && typeof ctx.getImageData === "function") {
						for (var i = 0; i < points.length; i++) {
							var x = Math.max(0, Math.min(c.width - 1, Math.floor(c.width * points[i][0])));
							var y = Math.max(0, Math.min(c.height - 1, Math.floor(c.height * points[i][1])));
							var data = ctx.getImageData(x, y, 1, 1).data;
							var sample = {x:x, y:y, rgba:[data[0] || 0, data[1] || 0, data[2] || 0, data[3] || 0], backend:"2d"};
							out.samples.push(sample);
							if (sample.rgba[0] || sample.rgba[1] || sample.rgba[2] || sample.rgba[3]) {
								out.ok = true;
								out.reason = "nonblank sample";
								out.pixel = sample;
								return out;
							}
						}
					}
					var gl = null;
					try { gl = c.getContext("webgl2") || c.getContext("webgl"); } catch (_) { gl = null; }
					if (gl && typeof gl.readPixels === "function") {
						for (var j = 0; j < points.length; j++) {
							var gx = Math.max(0, Math.min(c.width - 1, Math.floor(c.width * points[j][0])));
							var gy = Math.max(0, Math.min(c.height - 1, Math.floor(c.height * points[j][1])));
							var px = new Uint8Array(4);
							gl.readPixels(gx, gy, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, px);
							var glSample = {x:gx, y:gy, rgba:[px[0] || 0, px[1] || 0, px[2] || 0, px[3] || 0], backend:"webgl"};
							out.samples.push(glSample);
							if (glSample.rgba[0] || glSample.rgba[1] || glSample.rgba[2] || glSample.rgba[3]) {
								out.ok = true;
								out.reason = "nonblank sample";
								out.pixel = glSample;
								return out;
							}
						}
					}
					out.reason = "all sampled pixels blank";
					return out;
				} catch (e) {
					out.reason = "pixel read failed";
					out.error = String(e && (e.message || e)).slice(0, 160);
					return out;
				}
			}
			function canvasNonBlank(canvas) {
				return canvasPixelProof(canvas).ok;
			}
			function sharedSignalValue(name) {
				try {
					var getter = window.__gosx_get_shared_signal;
					if (typeof getter === "function") {
						var raw = getter(name);
						if (typeof raw === "string") {
							try {
								var parsed = JSON.parse(raw);
								if (typeof parsed === "string") return parsed;
							} catch (_) {}
						}
						if (typeof raw === "string") return raw;
					}
				} catch (_) {
				}
				try {
					var values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
					if (values && typeof values.has === "function" && values.has(name)) {
						var value = values.get(name);
						if (typeof value === "string") return value;
						if (value && typeof value.status === "string") return value.status;
						return JSON.stringify(value == null ? null : value);
					}
				} catch (_) {}
				return "";
			}
			function clearSharedSignal(name) {
				var before = sharedSignalValue(name);
				var out = {before:before, after:"", method:"none", ok:false};
				try {
					var setter = window.__gosx_set_shared_signal;
					if (typeof setter === "function") {
						setter(name, "\"\"");
						out.method = "runtime setter";
					} else {
						var values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
						if (values && typeof values.set === "function") {
							values.set(name, "");
							out.method = "shared signal map";
						}
					}
				} catch (e) {
					out.error = String(e && (e.message || e)).slice(0, 160);
				}
				out.after = sharedSignalValue(name);
				out.ok = out.after === "";
				return out;
			}
				function probeEvents() {
					var probe = window.__gosxOuroborosProbe;
					return probe && Array.isArray(probe.events) ? probe.events : null;
				}
				function probeSnapshot() {
					var events = probeEvents();
					return {
						available: !!events,
						count: events ? events.length : 0
					};
				}
				function refreshRuntimeProbe() {
					try {
						var probe = window.__gosxOuroborosProbe;
						if (probe && typeof probe.refresh === "function") probe.refresh();
					} catch (_) {}
				}
					function runtimeCallKind(detail) {
						if (!detail || typeof detail !== "object") return null;
						var n = Number(detail.eventKind);
						return Number.isFinite(n) ? n : null;
					}
				function newCanvasEventCalls(startIndex) {
					var events = probeEvents();
					if (!events) return null;
					var out = [];
					for (var i = Math.max(0, startIndex || 0); i < events.length; i++) {
						var event = events[i] || {};
						if (event.kind === "runtime-call" && event.name === "__gosx_canvas_event") {
							var detail = event.detail || {};
							out.push({
								index: i,
								phase: event.phase || "",
								name: event.name || "",
								kind: event.kind || "",
								argCount: Number(detail.argCount) || 0,
								argTypes: Array.isArray(detail.argTypes) ? detail.argTypes.slice(0, 8) : [],
								argBytes: Array.isArray(detail.argBytes) ? detail.argBytes.slice(0, 8) : [],
								resultType: detail.resultType || "",
								resultBytes: Number(detail.resultBytes) || 0,
								exception: detail.exception || "",
								runtimeKind: runtimeCallKind(detail),
								kindEvidenceAvailable: runtimeCallKind(detail) !== null
							});
						}
					}
					return out;
				}
					function canvasPickProof(snapshot, surfaceID, before, after) {
						var calls = newCanvasEventCalls(snapshot.count);
						var selected = sharedSignalValue("$surface.event.selectedID");
						var hasProbe = !!(snapshot && snapshot.available && calls);
						var accepted = (calls || []).filter(function(call){
							return !call.exception && call.argCount >= 3 && call.kindEvidenceAvailable && call.runtimeKind === 3;
						});
					return {
						ok: hasProbe && calls.length === 1 && accepted.length === 1 && selected === "alpha" && before === "" && after === "alpha",
						surfaceID: surfaceID,
						callCount: calls ? calls.length : 0,
						acceptedCallCount: accepted.length,
						observedCall: accepted[0] || null,
						calls: calls || [],
						selectionBefore: before,
							selectionAfter: after,
							selectedID: selected,
							probe: {available: !!(snapshot && snapshot.available), beforeCount: snapshot ? snapshot.count || 0 : 0, afterCount: probeEvents() ? probeEvents().length : 0},
							kindEvidenceRequired: true
		};
		}
		function routeRoot(){ return qs("[data-route-id]"); }
		var root = routeRoot();
		var assertionEvidence = {};
		var externalR10Route = exactExternalR10WaterPath();
		proof("route marker", !!root || externalR10Route, {routeID: root ? root.getAttribute("data-route-id") : (externalR10Route ? "R10" : ""), path: location.pathname, external: externalR10Route}, "[data-route-id]");
		var firstUsable = {name:"unset", ok:false, atMs:now(), payload:{}};
		switch (routeID) {
		case "R00":
			firstUsable = proof("ssr-no-runtime", !!root && !window.__gosx && !hasRuntimeAsset("/gosx/"), {runtime: !!window.__gosx});
			break;
		case "R01":
			firstUsable = proof("lite-ready", !!root && hasRuntimeAsset("/gosx/"), {assets: runtimeAssets().length});
			proof("lite-query-form", !!qs("form[data-lite-action] input[name=q]"));
			break;
		case "R02":
			var counterNode = qs(".counter");
			var counterBefore = counterNode ? counterNode.textContent || "" : "";
			await click(".counter button:last-of-type");
			counterNode = qs(".counter");
			var counterAfter = counterNode ? counterNode.textContent || "" : "";
			firstUsable = proof("counter-visible-patch", counterAfter !== counterBefore && /3/.test(counterAfter), {before: counterBefore.slice(0, 120), after: counterAfter.slice(0, 200)});
			proof("valid-action", await submitFormValid("form", "counter"));
			break;
		case "R03":
			await click(".shared-selection button");
			var texts = qsa(".shared-selection").map(function(n){ return n.textContent || ""; });
			assertionEvidence.sharedSignalManifest = await sharedSelectionManifestProof();
			firstUsable = proof("shared-signal-both-islands", texts.length >= 2 && texts.every(function(t){ return t.indexOf("beta") >= 0; }), {texts:texts});
			break;
		case "R04":
			var form = qs("form");
			if (form) {
				var input = form.querySelector("input[name=name]");
				if (input) input.value = "";
				var validationBefore = text("#action-state");
			}
			var invalidResult = await submitFormStructuredResult("form", "");
			var appliedActionHTML = applyActionHTML(invalidResult);
			var validationAfter = text("#action-state");
			var fieldError = text("[data-field-error]");
			var invalidJSON = invalidResult && invalidResult.json ? invalidResult.json : {};
			var validation = invalidResult.status === 422 && invalidJSON.ok === false && !!(invalidJSON.fieldErrors && invalidJSON.fieldErrors.name) && validationAfter !== validationBefore && /required|error/.test(validationAfter + fieldError);
			firstUsable = proof("visible-validation", validation, {before:validationBefore, after:validationAfter, fieldError:fieldError, invalid:invalidResult, appliedActionHTML:appliedActionHTML});
			proof("structured-validation-response", invalidResult.status === 422 && invalidJSON.ok === false && !!(invalidJSON.fieldErrors && invalidJSON.fieldErrors.name), invalidResult);
			var validResult = await submitFormBrowserRedirectResult("form", "baseline");
			proof("valid-result", validResult.ok && validResult.redirected && validResult.accept.indexOf("application/json") < 0, validResult);
			break;
			case "R05":
				await waitFor(function(){ return qs("#ouroboros-board[data-gosx-surface-id], canvas[data-gosx-surface-id]"); }, 4000, 50);
					var c = qs("#ouroboros-board") || qs("canvas");
					var pixel = await waitFor(function(){
						var p = canvasPixelProof(c);
						return p.ok ? p : false;
					}, 3000, 50);
					var pixelDiag = pixel || canvasPixelProof(c);
					refreshRuntimeProbe();
					var eventSnapshot = probeSnapshot();
					var clear = clearSharedSignal("$surface.event.selectedID");
					var pick = {ok:false, surfaceID: c ? c.getAttribute("data-gosx-surface-id") || "" : "", callCount:0, acceptedCallCount:0, selectionBefore:clear.before, selectionAfter:sharedSignalValue("$surface.event.selectedID")};
				if (c) {
					var r = c.getBoundingClientRect();
					var x = r.left + r.width / 2 - 36;
					var y = r.top + r.height / 2 + 4;
					pick.target = {clientX:x, clientY:y, localX:r.width / 2 - 36, localY:r.height / 2 + 4, cssWidth:r.width, cssHeight:r.height};
					c.dispatchEvent(new PointerEvent("pointerdown", {clientX:x, clientY:y, bubbles:true, pointerId:1, button:0, buttons:1}));
					c.dispatchEvent(new PointerEvent("pointerup", {clientX:x, clientY:y, bubbles:true, pointerId:1, button:0, buttons:0}));
						await settle(80);
						if (sampleLane === "product") {
							pick = await waitFor(function(){
								var current = sharedSignalValue("$surface.event.selectedID");
								if (current === "alpha" && clear.after === "") {
									return {ok:true, surfaceID:c.getAttribute("data-gosx-surface-id") || "", selectedID:current, selectionBefore:clear.after, selectionAfter:current, directABI:false};
								}
								return false;
							}, 3000, 50) || {ok:false, selectedID:sharedSignalValue("$surface.event.selectedID"), selectionBefore:clear.after, directABI:false};
						} else {
							pick = await waitFor(function(){
								var current = sharedSignalValue("$surface.event.selectedID");
								var proof = canvasPickProof(eventSnapshot, c.getAttribute("data-gosx-surface-id") || "", clear.after, current);
								return proof.ok ? proof : false;
							}, 3000, 50) || canvasPickProof(eventSnapshot, c.getAttribute("data-gosx-surface-id") || "", clear.after, sharedSignalValue("$surface.event.selectedID"));
						}
					}
				proof("canvasboard-nonblank", !!(pixel && pixel.ok), {canvas: !!c, pixel: pixel || null, diagnostics: pixelDiag, surfaceID: c ? c.getAttribute("data-gosx-surface-id") || "" : ""});
				proof("canvasboard-dom-pick-accepted", !!(c && pick && pick.ok) && qsa("[data-canvas-board-issue]").length === 0, pick);
				firstUsable = proof("canvasboard-ready", !!(pixel && pixel.ok && pick && pick.ok) && qsa("[data-canvas-board-issue]").length === 0, {pixel: pixel || null, pick: pick, clearSelection: clear});
				break;
		case "R06":
			var echoed = await new Promise(function(resolve){
				try {
					var ws = new WebSocket(location.origin.replace(/^http/, "ws") + "/_ouroboros/hub/echo");
					var done = false;
					var ignored = [];
					function isR06ControlEvent(name){ return name === "__welcome"; }
					function finish(v){ if (!done) { done = true; try { ws.close(); } catch(_){} resolve(v); } }
					ws.onopen = function(){ try { ws.send(JSON.stringify({event:"echo", data:{}})); } catch(_) { finish(false); } };
					ws.onmessage = function(event){
						try {
							var msg = JSON.parse(event.data || "{}");
							var data = typeof msg.data === "string" ? JSON.parse(msg.data) : (msg.data || {});
							if (msg.event === "echo" && data.status === "echo") {
								finish({ok:true, event:msg.event || "", data:data, ignored:ignored});
								return;
							}
							if (!isR06ControlEvent(msg.event || "")) {
								finish({ok:false, event:msg.event || "", data:data, ignored:ignored, unexpected:true});
								return;
							}
							ignored.push({event:msg.event || "", data:data});
						} catch (e) {
							finish({ok:false, error:String(e && (e.message || e)).slice(0, 160)});
						}
					};
					ws.onerror = function(){ finish(false); };
					setTimeout(function(){ finish(false); }, 1500);
				} catch (_) { resolve(false); }
			});
			var echoSignal = await waitFor(function(){
				var value = sharedSignalValue("$ouroboros.echo");
				return value.indexOf("echo") >= 0 ? value : false;
			}, 1500, 50) || sharedSignalValue("$ouroboros.echo");
			firstUsable = proof("socket-echo-applied", !!(echoed && echoed.ok) && echoSignal.indexOf("echo") >= 0, {hub:"ouroboros-echo", echoed:echoed, signal:echoSignal});
			break;
		case "R07":
			var v = qs("video");
			if (v) { try { v.muted = true; await v.play(); v.pause(); } catch (_) {} }
			var mediaReady = proof("media-ready", !!v && v.readyState >= 1, {readyState:v ? v.readyState : 0});
			proof("drift-decision", !!v && !!qs("[data-route-id]"));
			var syncSocket = await new Promise(function(resolve){
				try {
					var ws = new WebSocket(location.origin.replace(/^http/, "ws") + "/_ouroboros/video-sync");
					var done = false;
					function finish(v){ if (!done) { done = true; try { ws.close(); } catch(_){} resolve(v); } }
					ws.onmessage = function(event){
						try {
							var msg = JSON.parse(event.data || "{}");
							var payload = typeof msg.data === "string" ? JSON.parse(msg.data) : (msg.data || {});
							finish({
								ok: msg.event === "sync" && payload.type === "sync" && payload.mediaID === "/media/ouroboros-placeholder.mp4" && Number(payload.position) === 0 && payload.playing === false,
								event: msg.event || "",
								payload: payload
							});
						} catch (e) {
							finish({ok:false, error:String(e && (e.message || e)).slice(0, 160)});
						}
					};
					ws.onerror = function(){ finish(false); };
					setTimeout(function(){ finish(false); }, 1500);
				} catch (_) { resolve(false); }
			});
			proof("same-origin-sync-socket", !!(syncSocket && syncSocket.ok), {origin: location.origin, sync: syncSocket});
			firstUsable = proof("media-sync-ready", !!(mediaReady && mediaReady.ok && syncSocket && syncSocket.ok), {mediaReady: mediaReady ? mediaReady.ok : false, sync: syncSocket});
			break;
		case "R08":
			var sceneEntry = scene3DManifestEntry();
			var sceneCaps = sceneEntry && Array.isArray(sceneEntry.capabilities) ? sceneEntry.capabilities : [];
			assertionEvidence.scene3dSelectable = {
				engine: !!sceneEntry,
				webgpu: sceneCaps.indexOf("webgpu") >= 0,
				webgl: sceneCaps.indexOf("webgl") >= 0,
				capabilities: sceneCaps
			};
			var sceneMountCanvas = await waitSceneMountCanvas("[data-gosx-scene3d]");
			var sceneOrbit = await proveSceneOrbitInput("[data-gosx-scene3d]");
			var sceneBackendCommit = await waitSceneBackendCommit("[data-gosx-scene3d]");
			assertionEvidence.scene3dSelectable.backend = sceneBackendCommit.backend || "";
			assertionEvidence.scene3dSelectable.renderGPU = sceneBackendCommit.renderGPU || "";
			assertionEvidence.scene3dSelectable.backendTruth = sceneBackendCommit.truth || null;
			assertionEvidence.scene3dSelectable.backendCommit = sceneBackendCommit;
			firstUsable = proof("scene-backend-committed", !!sceneMountCanvas.canvas && !!(assertionEvidence.scene3dSelectable && assertionEvidence.scene3dSelectable.engine && sceneBackendCommit.ok), {canvas:!!sceneMountCanvas.canvas, scene3d:assertionEvidence.scene3dSelectable});
			proof("scene-orbit-state-changed", !!(sceneOrbit && sceneOrbit.ok), sceneOrbit);
			break;
		case "R10":
			var waterMountCanvas = await waitSceneMountCanvas("[data-gosx-scene3d-mounted]");
			var waterOrbit = await proveSceneOrbitInput("[data-gosx-scene3d-mounted]");
			var waterBackendCommit = await waitSceneBackendCommit("[data-gosx-scene3d-mounted]");
			var waterAssets = runtimeAssets();
			var scene3dAsset = waterAssets.some(function(a){ return /bootstrap-feature-scene3d|runtime\.wasm|wasm_exec/.test(a.name); });
			firstUsable = proof("water-scene-backend-committed", !!waterMountCanvas.mount && !!waterMountCanvas.canvas && waterBackendCommit.ok, {mount:!!waterMountCanvas.mount, canvas:!!waterMountCanvas.canvas, backend:waterBackendCommit.backend, renderGPU:waterBackendCommit.renderGPU, backendTruth:waterBackendCommit.truth || null, backendCommit:waterBackendCommit, pixelManifestRefs:"canonical sample artifacts"});
			proof("water-runtime-assets", scene3dAsset, {assets: waterAssets.map(function(a){ return a.name; }).filter(function(n){ return /\/gosx\//.test(n); })});
			proof("water-orbit-state-changed", !!(waterOrbit && waterOrbit.ok), waterOrbit);
			break;
		default:
			if (routeID.indexOf("R09") === 0) {
				var before = location.pathname;
				var oldRoot = routeRoot();
				var navEntriesBefore = performance.getEntriesByType("navigation").length;
				await click("a[data-gosx-link], a");
				await settle(350);
				var after = location.pathname;
				var newRoot = routeRoot();
				var sameDoc = before !== after && performance.getEntriesByType("navigation").length === navEntriesBefore;
				firstUsable = proof("same-document-navigation", sameDoc, {before:before, after:after});
				proof("old-root-removed", oldRoot !== newRoot);
				proof("navigation-remount", !!newRoot && newRoot.getAttribute("data-route-id") !== "");
			} else {
				firstUsable = proof("generic-route-ready", !!root);
			}
		}
		var assertions = {};
		routeAssertions.forEach(function(a){
			switch (a) {
			case "no bootstrap": assertions[a] = !hasRuntimeAsset("/gosx/"); break;
			case "no manifest": assertions[a] = !hasRuntimeAsset("manifest"); break;
			case "bootstrap mode lite": assertions[a] = hasRuntimeAsset("/gosx/") || !!window.__gosx; break;
			case "no wasm":
			case "no wasm runtime path": assertions[a] = !hasRuntimeAsset(".wasm"); break;
			case "data-gosx-link": assertions[a] = !!qs("a[data-gosx-link], a"); break;
			case "same-document navigation": assertions[a] = proofs.some(function(p){ return p.name === "same-document-navigation" && p.ok; }); break;
			case "island dispose/remount evidence": assertions[a] = proofs.some(function(p){ return p.name === "navigation-remount" && p.ok; }); break;
			case "CanvasBoard marker": assertions[a] = !!qs("[data-canvas-board-runtime], canvas"); break;
			case "canvas2d surface": assertions[a] = !!qs("canvas"); break;
			case "hub manifest": assertions[a] = !!qs("[data-hub]"); break;
			case "echo binding": assertions[a] = !!qs("[data-signal]"); break;
			case "video engine": assertions[a] = !!qs("video"); break;
			case "local media endpoint": assertions[a] = !!qs("video[src]"); break;
			case "same-origin sync socket": assertions[a] = proofs.some(function(p){ return p.name === "same-origin-sync-socket" && p.ok; }); break;
			case "Scene3D engine": assertions[a] = !!qs("[data-gosx-scene3d], canvas"); break;
			case "scene3d feature path": assertions[a] = !!qs("[data-gosx-scene3d], canvas"); break;
			case "WebGPU remains selectable": assertions[a] = !!(assertionEvidence.scene3dSelectable && assertionEvidence.scene3dSelectable.engine && assertionEvidence.scene3dSelectable.webgpu && assertionEvidence.scene3dSelectable.webgl && !!qs("canvas")); break;
			case "one island": assertions[a] = registry().islands === 1 || qsa("[data-gosx-island]").length === 1; break;
			case "patch runtime": assertions[a] = typeof window.__gosx_apply_patch === "function" || hasRuntimeAsset("patch"); break;
			case "valid action endpoint": assertions[a] = proofs.some(function(p){ return p.name === "valid-action" && p.ok; }); break;
			case "five islands": assertions[a] = registry().islands >= 5 || qsa("[data-gosx-island]").length >= 5; break;
			case "shared signal program": assertions[a] = typeof window.__gosx_get_shared_signal === "function" || typeof window.__gosx_set_shared_signal === "function"; break;
			case "shared signal manifest entry": assertions[a] = !!(assertionEvidence.sharedSignalManifest && assertionEvidence.sharedSignalManifest.ok); break;
			case "declarative action marker": assertions[a] = !!qs("form[data-lite-action], [data-action-state]"); break;
			case "validation response": assertions[a] = proofs.some(function(p){ return p.name === "visible-validation" && p.ok; }); break;
			case "redirect response": assertions[a] = proofs.some(function(p){ return p.name === "valid-result" && p.ok && p.payload && (p.payload.redirected || (p.payload.status >= 300 && p.payload.status < 400)); }); break;
			case "follow sync props":
				var videoEntry = videoManifestEntry();
				assertions[a] = !!(videoEntry && videoEntry.props && videoEntry.props.syncMode === "follow" && videoEntry.props.sync === "/_ouroboros/video-sync" && videoEntry.props.src === "/media/ouroboros-placeholder.mp4");
				break;
			case "external reference only": assertions[a] = exactExternalR10WaterPath(); break;
			default: assertions[a] = false;
			}
		});
		var disallowedHits = [];
		var hasPackage = !!qs('script[src*="package.json"]');
		disallowed.forEach(function(item){
			if (item === "app-side-js" && qsa('script[src]').some(function(s){ return !/\/gosx\/|wasm_exec|chrome-extension/.test(s.src); })) disallowedHits.push(item);
			if (item === "app-side-ts" && qsa('script[type*="typescript"]').length) disallowedHits.push(item);
			if (item === "package-json" && hasPackage) disallowedHits.push(item);
			if (item === "fixture-local-copy" && location.pathname.indexOf("/demos/water") !== 0) disallowedHits.push(item);
		});
		return {
			routeID: root ? root.getAttribute("data-route-id") : (externalR10Route ? "R10" : ""),
			path: location.pathname,
			marker: root ? root.getAttribute("data-marker") : "",
			capability: root ? root.getAttribute("data-expected-capability") : "",
			firstUsable: firstUsable,
			proofs: proofs,
			missing: missing,
			registry: registry(),
			routePlan: {title: document.title || "", capability: root ? root.getAttribute("data-expected-capability") : "", assertions: assertions},
			assertions: assertions,
			assets: runtimeAssets(),
			disallowed: disallowedHits
		};
	})()
//# sourceURL=http://gosx.invalid/__gosx_ouroboros_harness/route-state-machine.js`, string(routeID), string(laneJSON), string(assertions), string(disallowed))
}

func executeRouteInteraction(d *perf.Driver, route FixtureSpec) error {
	switch {
	case strings.HasPrefix(route.ID, "R02"):
		_ = perf.Click(d, "button")
	case strings.HasPrefix(route.ID, "R03"):
		_ = perf.Click(d, ".shared-selection button")
	case strings.HasPrefix(route.ID, "R04"):
		_ = perf.Click(d, "form button[type=submit]")
	case strings.HasPrefix(route.ID, "R05"):
		return d.Evaluate(`(function(){
			var c = document.querySelector("canvas");
			if (!c) return false;
			var r = c.getBoundingClientRect();
			var e = new PointerEvent("pointerdown", {clientX:r.left+r.width/2, clientY:r.top+r.height/2, bubbles:true});
			c.dispatchEvent(e);
			return true;
		})()`, nil)
	case strings.HasPrefix(route.ID, "R06"):
		return d.Evaluate(`(async function(){
			var url = location.origin.replace(/^http/, "ws") + "/_ouroboros/hub/echo";
			return new Promise(function(resolve) {
				try {
					var ws = new WebSocket(url);
					var done = false;
					function isR06ControlEvent(name){ return name === "__welcome"; }
					function finish(v){ if (!done) { done = true; try { ws.close(); } catch(_){} resolve(v); } }
					ws.onopen = function(){ try { ws.send(JSON.stringify({event:"echo", data:{}})); } catch(_) { finish(false); } };
					ws.onmessage = function(event){
						try {
							var msg = JSON.parse(event.data || "{}");
							var data = typeof msg.data === "string" ? JSON.parse(msg.data) : (msg.data || {});
							if (msg.event === "echo" && data.status === "echo") finish(true);
							if (!isR06ControlEvent(msg.event || "")) finish(false);
						} catch (_) {
							finish(false);
						}
					};
					ws.onerror = function(){ finish(false); };
					setTimeout(function(){ finish(false); }, 1000);
				} catch (_) { resolve(false); }
			});
		})()`, nil)
	case strings.HasPrefix(route.ID, "R07"):
		return d.Evaluate(`(async function(){
			var v = document.querySelector("video");
			if (!v) return false;
			try { v.muted = true; await v.play(); v.pause(); } catch (_) {}
			return v.readyState >= 1;
		})()`, nil)
	case strings.HasPrefix(route.ID, "R08"), strings.HasPrefix(route.ID, "R10"):
		return d.Evaluate(`(function(){
			var c = document.querySelector("canvas");
			if (!c) return false;
			var r = c.getBoundingClientRect();
			c.dispatchEvent(new PointerEvent("pointerdown", {clientX:r.left+r.width/2, clientY:r.top+r.height/2, bubbles:true}));
			c.dispatchEvent(new PointerEvent("pointermove", {clientX:r.left+r.width/2+40, clientY:r.top+r.height/2+20, bubbles:true}));
			c.dispatchEvent(new PointerEvent("pointerup", {clientX:r.left+r.width/2+40, clientY:r.top+r.height/2+20, bubbles:true}));
			return true;
		})()`, nil)
	case strings.HasPrefix(route.ID, "R09"):
		return perf.Click(d, "a[data-gosx-link], a")
	}
	return nil
}

func collectProofs(d *perf.Driver, route FixtureSpec) ProofBundle {
	var raw struct {
		RouteID      string         `json:"routeID"`
		Marker       string         `json:"marker"`
		Capability   string         `json:"capability"`
		ReadyAt      float64        `json:"readyAt"`
		Canvas       bool           `json:"canvas"`
		CanvasPixels map[string]any `json:"canvasPixels"`
		VideoReady   bool           `json:"videoReady"`
		ActionState  string         `json:"actionState"`
		Path         string         `json:"path"`
		ProbeCount   int            `json:"probeCount"`
		Registry     map[string]any `json:"registry"`
		RoutePlan    map[string]any `json:"routePlan"`
	}
	_ = d.Evaluate(`(function(){
		var root = document.querySelector("[data-route-id]");
		var ready = performance.getEntriesByName("gosx:ready")[0];
		var canvas = document.querySelector("canvas");
		var video = document.querySelector("video");
		var probe = window.__gosxOuroborosProbe;
		var registry = {};
		try {
			var g = window.__gosx || {};
			registry.islands = g.islands && typeof g.islands.size === "number" ? g.islands.size : 0;
			registry.engines = g.engines && typeof g.engines.size === "number" ? g.engines.size : 0;
			registry.hubs = g.hubs && typeof g.hubs.size === "number" ? g.hubs.size : 0;
		} catch (_) {}
		return {
			routeID: root ? root.getAttribute("data-route-id") : "",
			marker: root ? root.getAttribute("data-marker") : "",
			capability: root ? root.getAttribute("data-expected-capability") : "",
			readyAt: ready ? ready.startTime : 0,
			canvas: !!canvas,
			canvasPixels: canvas ? {width: canvas.width || 0, height: canvas.height || 0} : {},
			videoReady: video ? video.readyState >= 1 : false,
			actionState: (document.querySelector("[data-action-state]") || {}).textContent || "",
			path: location.pathname,
			probeCount: probe && probe.events ? probe.events.length : 0,
			registry: registry,
			routePlan: {title: document.title || "", capability: root ? root.getAttribute("data-expected-capability") : ""}
		};
	})()`, &raw)
	required := route.RequiredInteractions
	var observed []string
	missing := []string{}
	first := ProofPayload{Name: "route-marker", AtMs: raw.ReadyAt, OK: raw.RouteID != "", Payload: map[string]any{"routeID": raw.RouteID, "path": raw.Path, "marker": raw.Marker, "capability": raw.Capability}}
	if first.OK {
		observed = append(observed, "route marker")
	} else {
		missing = append(missing, "route marker")
	}
	if strings.HasPrefix(route.ID, "R05") || strings.HasPrefix(route.ID, "R08") || strings.HasPrefix(route.ID, "R10") {
		if raw.Canvas {
			observed = append(observed, "canvas present")
		} else {
			missing = append(missing, "canvas present")
		}
	}
	if strings.HasPrefix(route.ID, "R07") {
		if raw.VideoReady {
			observed = append(observed, "video ready")
		} else {
			missing = append(missing, "video ready")
		}
	}
	return ProofBundle{
		Required:        required,
		Observed:        observed,
		FirstUsable:     first,
		FailClosed:      len(missing) > 0,
		MissingRequired: missing,
		RoutePlan:       raw.RoutePlan,
		Registry:        raw.Registry,
	}
}

func collectProbeEvents(d *perf.Driver) []ProbeEvent {
	var snapshot struct {
		Events []ProbeEvent `json:"events"`
	}
	_ = d.Evaluate(`(function(){
		var probe = window.__gosxOuroborosProbe;
		if (!probe || !Array.isArray(probe.events)) return {events:[]};
		var events = probe.events.slice();
		probe.events.length = 0;
		return {events:events};
	})()`, &snapshot)
	return snapshot.Events
}

func collectRuntimeJSONDrain(d *perf.Driver) *RuntimeJSONRawDrain {
	var drain *RuntimeJSONRawDrain
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drain); err != nil {
		return nil
	}
	if drain == nil || drain.SchemaVersion == "" {
		return nil
	}
	return drain
}

func drainEvents(drain *RuntimeJSONRawDrain) []ProbeEvent {
	if drain == nil {
		return nil
	}
	return append([]ProbeEvent{}, drain.Events...)
}

func summarizeTrace(path string, data []byte) TraceSampleSummary {
	if len(data) == 0 {
		return TraceSampleSummary{}
	}
	events, _ := perf.SummarizeTrace(data, 20, 0)
	totals := map[string]float64{}
	for _, e := range events {
		totals[e.Name] += e.Duration
	}
	return TraceSampleSummary{Captured: true, Ref: path, Events: events, TotalsMs: totals}
}

func summarizeCoverage(path string, entries []perf.CoverageEntry) CoverageSampleSummary {
	if len(entries) == 0 {
		return CoverageSampleSummary{}
	}
	s := CoverageSampleSummary{Captured: len(entries) > 0, Ref: path, ScriptCount: len(entries)}
	for _, e := range entries {
		s.UsedBytes += e.UsedBytes
		s.UnusedBytes += e.UnusedBytes
	}
	if len(entries) <= 20 {
		s.Entries = entries
	}
	return s
}

func sampleMetrics(page *perf.PageReport, mem perf.MemoryStats) map[string]float64 {
	m := map[string]float64{
		"ttfbMs":              page.TTFBMs,
		"dclMs":               page.DCLMs,
		"fullyLoadedMs":       page.FullyLoadedMs,
		"jsHeapUsedMb":        mem.JSHeapUsedMB,
		"jsHeapTotalMb":       mem.JSHeapTotalMB,
		"domNodeCount":        float64(mem.DOMNodeCount),
		"requestCount":        float64(len(page.Resources)),
		"transferBytes":       float64(page.TotalBytesTransferred),
		"longTaskCount":       float64(page.LongTaskCount),
		"longTaskTotalMs":     page.LongTaskTotalMs,
		"totalBlockingTimeMs": page.TotalBlockingTimeMs,
		"consoleErrorCount":   float64(len(page.ConsoleEntries)),
	}
	if page.Scene != nil {
		m["sceneFrameCount"] = float64(page.Scene.FrameCount)
		m["sceneCpuP95Ms"] = page.Scene.FrameStats.P95
		m["sceneCpuP99Ms"] = page.Scene.FrameStats.P99
		m["sceneCpuMaxMs"] = page.Scene.FrameStats.Max
		if page.Scene.Presentation != nil {
			m["rafP95Ms"] = page.Scene.Presentation.Stats.P95
			m["missedVsyncEstimate"] = float64(page.Scene.Presentation.EstimatedMissedVsyncs)
		}
	}
	return m
}

func sampleArtifactPaths(root, routeID, lane, cacheMode string, idx int) SampleArtifacts {
	name := fmt.Sprintf("%s-%s-%s-%03d", routeID, lane, cacheMode, idx)
	return SampleArtifacts{
		TraceRef:        filepath.Join(root, "traces", name+".trace.json"),
		CoverageRef:     filepath.Join(root, "coverage", name+".coverage.json"),
		HeapSnapshotRef: filepath.Join(root, "heaps", name+".heapsnapshot"),
	}
}

func relativeArtifacts(root string, a SampleArtifacts) SampleArtifacts {
	a.TraceRef = relTo(root, a.TraceRef)
	a.CoverageRef = relTo(root, a.CoverageRef)
	a.HeapSnapshotRef = relTo(root, a.HeapSnapshotRef)
	a.ScreenshotRef = relTo(root, a.ScreenshotRef)
	return a
}

func writeRawSample(w io.Writer, sample BrowserRawSample) error {
	enc, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	_, err = w.Write(append(enc, '\n'))
	return err
}

func ValidateBrowserBaseline(plan SamplingPlan, samples []BrowserRawSample, source SourceIdentity, env EnvironmentReport, opts BrowserBaselineOptions) BaselineValidation {
	var errs []string
	var warnings []string
	if source.BaseRevision == "" || source.OverlayHash == "" {
		errs = append(errs, "missing source identity")
	}
	if source.RuntimeJSONStatic == nil || !source.RuntimeJSONStatic.Validated {
		errs = append(errs, "missing validated runtime JSON static corpus")
	} else if err := validateRuntimeJSONStaticIdentity(source.RuntimeJSONStatic); err != nil {
		errs = append(errs, err.Error())
	}
	if source.CompatibilityAudit == nil {
		warnings = append(warnings, "compatibility audit unavailable; run cannot become canonical")
	} else if err := validateCompatibilityAuditIdentity(source.CompatibilityAudit); err != nil {
		errs = append(errs, err.Error())
	} else if source.CompatibilityAudit.Status != "pass" || !source.CompatibilityAudit.CanonicalAvailable {
		warnings = append(warnings, "compatibility audit unavailable; run cannot become canonical")
	}
	if plan.Canonical {
		if !source.StrictInventory || !source.CurrentOverlayVerified {
			errs = append(errs, "canonical run requires strict fresh inventory")
		}
		if !source.ReconstructionProof {
			errs = append(errs, "canonical run requires reconstruction proof")
		}
		if source.Reconstruction == nil || !source.Reconstruction.Isolated || !source.Reconstruction.Applied || !source.Reconstruction.Verified {
			errs = append(errs, "canonical run requires isolated reconstruction replay")
		} else if source.Reconstruction.BaseRevision != source.BaseRevision || source.Reconstruction.ObservedOverlayHash != source.OverlayHash {
			errs = append(errs, "canonical reconstruction replay source mismatch")
		}
		if !opts.Trace {
			errs = append(errs, "canonical run requires trace capture")
		}
		if !opts.Coverage {
			errs = append(errs, "canonical run requires coverage capture")
		}
		if !opts.HeapSnapshots {
			errs = append(errs, "canonical run requires heap snapshots")
		}
		if !canonicalHardwareClass(env.HardwareClassification) {
			errs = append(errs, "canonical run requires observed hardware-webgpu or hardware-webgl classification")
		}
		if err := validateCanonicalSampleMatrix(plan, samples); err != nil {
			errs = append(errs, err.Error())
		}
		if err := validateRuntimeJSONDynamicEvidenceSamples(source, samples); err != nil {
			errs = append(errs, err.Error())
		}
		if source.CompatibilityAudit == nil {
			errs = append(errs, "canonical run requires compatibility audit")
		} else {
			if source.CompatibilityAudit.Status != "pass" {
				errs = append(errs, "canonical run requires passing compatibility audit")
			}
			if !source.CompatibilityAudit.CanonicalAvailable {
				errs = append(errs, "canonical run requires compatibility audit canonical availability")
			}
			if source.CompatibilityAudit.Receipt.Count != canonicalGosx || source.CompatibilityAudit.Receipt.NameSetHash != compatibilityReceiptHash {
				errs = append(errs, "canonical run requires pinned 209 compatibility receipt")
			}
			if source.CompatibilityAudit.Reconciliation.AddedSinceAnchorCount != 0 || source.CompatibilityAudit.Reconciliation.RemovedSinceAnchorCount != 0 {
				errs = append(errs, "canonical run requires zero compatibility anchor/current drift")
			}
		}
		if opts.PixelManifest == "" {
			errs = append(errs, "canonical Scene3D routes require accepted O02-F pixel manifests")
		}
	}
	if len(samples) == 0 {
		errs = append(errs, "no samples recorded")
	}
	for _, sample := range samples {
		prefix := sample.RouteID + "/" + sample.CacheMode
		if sample.SchemaVersion != BrowserBaselineSchemaVersion {
			errs = append(errs, prefix+": bad sample schema")
		}
		if sample.Source.BaseRevision != source.BaseRevision || sample.Source.OverlayHash != source.OverlayHash {
			errs = append(errs, prefix+": source identity mismatch")
		}
		if sample.Page == nil {
			errs = append(errs, prefix+": missing page report")
		}
		if sample.Proofs.FailClosed {
			errs = append(errs, prefix+": fail-closed proof")
		}
		if !sample.Proofs.FirstUsable.OK {
			errs = append(errs, prefix+": missing first usable proof")
		}
		if len(sample.Errors) > 0 {
			errs = append(errs, prefix+": "+strings.Join(sample.Errors, ", "))
		}
		if len(sample.Console) > 0 {
			errs = append(errs, prefix+": console entries captured")
		}
		if len(sample.Network) == 0 {
			errs = append(errs, prefix+": missing CDP network records")
		}
		if plan.Canonical {
			if sample.SampleLane == SampleLaneProduct {
				if sample.RuntimeJSONDrain != nil {
					errs = append(errs, prefix+": product sample leaked full runtime JSON probe")
				}
				if sample.RouteID == "R08" || sample.RouteID == "R10" {
					if err := validateSceneSamplePixelManifestRefs(sample, opts); err != nil {
						errs = append(errs, prefix+": "+err.Error())
					}
					if sample.Page == nil || sample.Page.Scene == nil || sample.Page.Scene.FrameCount <= 0 {
						errs = append(errs, prefix+": missing Scene3D first-frame telemetry")
					}
				}
				if !sample.Trace.Captured {
					errs = append(errs, prefix+": missing trace")
				}
				if !sample.Coverage.Captured {
					errs = append(errs, prefix+": missing coverage")
				}
				if sample.Artifacts.HeapSnapshotRef == "" {
					errs = append(errs, prefix+": missing heap snapshot ref")
				}
			}
		}
		if sample.SampleLane == SampleLaneProduct {
			if len(sample.ProbeEvents) > 0 {
				errs = append(errs, prefix+": product sample leaked probe events")
			}
		} else {
			if sample.RuntimeJSONDrain == nil {
				errs = append(errs, prefix+": missing runtime JSON probe drain")
			}
			if sample.RouteID == "R00" {
				if !probeHasPhase(sample.ProbeEvents, "route-load") {
					errs = append(errs, prefix+": dynamic probe coverage missing phase:route-load")
				}
			} else if missing := runtimeProbeCoverageMissing(sample.ProbeEvents, []string{sample.RouteID}, []string{"route-load"}); len(missing) > 0 {
				errs = append(errs, prefix+": dynamic probe coverage missing "+strings.Join(missing, ", "))
			}
		}
	}
	if !plan.Canonical {
		warnings = append(warnings, "reduced smoke cannot update canonical baseline")
	}
	status := "pass"
	if len(errs) > 0 {
		status = "fail"
	}
	return BaselineValidation{Status: status, Errors: uniqueStrings(errs), Warnings: uniqueStrings(warnings)}
}

func canonicalHardwareClass(class string) bool {
	return class == "hardware-webgpu" || class == "hardware-webgl"
}

func validateCanonicalSampleMatrix(plan SamplingPlan, samples []BrowserRawSample) error {
	counts := map[string]map[string]int{}
	pilots := map[string]map[string]int{}
	probes := map[string]map[string]int{}
	probePilots := map[string]map[string]int{}
	mislabeledPilots := map[string]int{}
	requiredRoutes := canonicalRouteIDSet()
	var missing []string
	for _, sample := range samples {
		if !requiredRoutes[sample.RouteID] {
			missing = append(missing, fmt.Sprintf("%s/%s unknown route", sample.RouteID, sample.CacheMode))
			continue
		}
		if sample.CacheMode != "cold" && sample.CacheMode != "warm" {
			missing = append(missing, fmt.Sprintf("%s/%s unknown cache bucket", sample.RouteID, sample.CacheMode))
			continue
		}
		switch sample.SampleLane {
		case SampleLaneProduct:
		case SampleLaneProbe:
			if sample.Pilot || sample.Discarded {
				missing = append(missing, fmt.Sprintf("%s/%s probe kept sample is mislabeled", sample.RouteID, sample.CacheMode))
				continue
			}
			if probes[sample.RouteID] == nil {
				probes[sample.RouteID] = map[string]int{}
			}
			probes[sample.RouteID][sample.CacheMode]++
			continue
		case SampleLaneProbeOverhead:
			if !(sample.Pilot && sample.Discarded) {
				missing = append(missing, fmt.Sprintf("%s/%s probe-overhead sample is not a discarded pilot", sample.RouteID, sample.CacheMode))
				continue
			}
			if probePilots[sample.RouteID] == nil {
				probePilots[sample.RouteID] = map[string]int{}
			}
			probePilots[sample.RouteID][sample.CacheMode]++
			continue
		default:
			missing = append(missing, fmt.Sprintf("%s/%s unknown sample lane %q", sample.RouteID, sample.CacheMode, sample.SampleLane))
			continue
		}
		if sample.Pilot || sample.Discarded {
			if sample.Pilot && sample.Discarded {
				if pilots[sample.RouteID] == nil {
					pilots[sample.RouteID] = map[string]int{}
				}
				pilots[sample.RouteID][sample.CacheMode]++
			} else if sample.Pilot != sample.Discarded {
				mislabeledPilots[sample.RouteID+"/"+sample.CacheMode]++
			}
			continue
		}
		if counts[sample.RouteID] == nil {
			counts[sample.RouteID] = map[string]int{}
		}
		counts[sample.RouteID][sample.CacheMode]++
	}
	for _, routeID := range canonicalRouteIDs() {
		cold, warm := canonicalRequiredCounts(routeID)
		for _, want := range []struct {
			cache string
			count int
		}{
			{"cold", cold},
			{"warm", warm},
		} {
			got := counts[routeID][want.cache]
			if got != want.count {
				missing = append(missing, fmt.Sprintf("%s/%s=%d want %d", routeID, want.cache, got, want.count))
			}
			gotPilots := pilots[routeID][want.cache]
			if gotPilots != plan.PilotsDiscarded {
				missing = append(missing, fmt.Sprintf("%s/%s pilots=%d want %d", routeID, want.cache, gotPilots, plan.PilotsDiscarded))
			}
			gotProbe := probes[routeID][want.cache]
			if gotProbe != 1 {
				missing = append(missing, fmt.Sprintf("%s/%s probe=%d want 1", routeID, want.cache, gotProbe))
			}
			gotProbePilots := probePilots[routeID][want.cache]
			if gotProbePilots != plan.PilotsDiscarded {
				missing = append(missing, fmt.Sprintf("%s/%s probe-overhead=%d want %d", routeID, want.cache, gotProbePilots, plan.PilotsDiscarded))
			}
		}
	}
	for bucket, count := range mislabeledPilots {
		missing = append(missing, fmt.Sprintf("%s mislabeled pilots=%d", bucket, count))
	}
	if len(missing) > 0 {
		return fmt.Errorf("canonical sample matrix incomplete: %s", strings.Join(missing, "; "))
	}
	return nil
}

func probeCoversInteractionWindow(events []ProbeEvent) bool {
	routeLoad := false
	for _, event := range events {
		if event.Phase == "route-load" {
			routeLoad = true
		}
		if event.Kind == "interaction" {
			return true
		}
	}
	return routeLoad
}

func probeHasPhase(events []ProbeEvent, phase string) bool {
	for _, event := range events {
		if event.Phase == phase {
			return true
		}
	}
	return false
}

func runtimeProbeCoverageMissing(events []ProbeEvent, requiredRoutes []string, requiredPhases []string) []string {
	missing := RuntimeJSONProbeCoverage(events, requiredRoutes, requiredPhases)
	if !runtimeProbeHasWrappedSurface(events) {
		return missing
	}
	filtered := missing[:0]
	for _, item := range missing {
		if item == "kind:runtime-call" || item == "kind:json-call" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func runtimeProbeHasWrappedSurface(events []ProbeEvent) bool {
	for _, event := range events {
		if event.Kind != "probe" || (event.Name != "install" && event.Name != "refresh") {
			continue
		}
		if n, ok := numberFromAny(event.Detail["wrappedCount"]); ok && n > 0 {
			return true
		}
	}
	return false
}

func existingCanonicalManifest(root string) bool {
	body, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return false
	}
	var doc struct {
		Canonical bool `json:"canonical"`
	}
	return json.Unmarshal(body, &doc) == nil && doc.Canonical
}

type networkCapture struct {
	mu      sync.Mutex
	records map[network.RequestID]*NetworkRecord
	cancel  context.CancelFunc
	d       *perf.Driver
}

func startNetworkCapture(d *perf.Driver) (*networkCapture, error) {
	c := &networkCapture{records: map[network.RequestID]*NetworkRecord{}, d: d}
	c.cancel = d.ListenTarget(func(ev any) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			rec := c.records[e.RequestID]
			if rec == nil {
				rec = &NetworkRecord{RequestID: string(e.RequestID)}
				c.records[e.RequestID] = rec
			}
			rec.DocumentURL = e.DocumentURL
			rec.Role = string(e.Type)
			if e.Request != nil {
				rec.URL = e.Request.URL
				rec.Method = e.Request.Method
			}
		case *network.EventResponseReceived:
			rec := c.records[e.RequestID]
			if rec == nil {
				rec = &NetworkRecord{RequestID: string(e.RequestID)}
				c.records[e.RequestID] = rec
			}
			rec.Role = string(e.Type)
			if e.Response != nil {
				rec.URL = e.Response.URL
				rec.Status = e.Response.Status
				rec.MimeType = e.Response.MimeType
				rec.Protocol = e.Response.Protocol
				rec.EncodedDataLength = e.Response.EncodedDataLength
				rec.TransferredBytes = e.Response.EncodedDataLength
				rec.FromDiskCache = e.Response.FromDiskCache
				rec.FromServiceWorker = e.Response.FromServiceWorker
				rec.FromPrefetchCache = e.Response.FromPrefetchCache
				rec.CacheControl = headerValue(e.Response.Headers, "cache-control")
				rec.Immutable = strings.Contains(strings.ToLower(rec.CacheControl), "immutable")
				rec.HeaderBytes = approximateHeaderBytes(e.Response.Headers)
				rec.Headers = headersToStrings(e.Response.Headers)
				rec.RuntimeAssetRole, rec.UnresolvedAssetRole = classifyRuntimeAsset(rec.URL)
			}
		case *network.EventLoadingFinished:
			rec := c.records[e.RequestID]
			if rec == nil {
				rec = &NetworkRecord{RequestID: string(e.RequestID)}
				c.records[e.RequestID] = rec
			}
			rec.TransferredBytes = e.EncodedDataLength
			if rec.EncodedDataLength == 0 {
				rec.EncodedDataLength = e.EncodedDataLength
			}
		}
	})
	if err := d.Run(network.Enable()); err != nil {
		c.cancel()
		return nil, err
	}
	return c, nil
}

func (c *networkCapture) Stop() {
	if c == nil {
		return
	}
	_ = c.d.Run(network.Disable())
	c.cancel()
}

func (c *networkCapture) Records() []NetworkRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]NetworkRecord, 0, len(c.records))
	for _, rec := range c.records {
		if rec.URL == "" {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role == out[j].Role {
			return out[i].URL < out[j].URL
		}
		return out[i].Role < out[j].Role
	})
	return out
}

func headerValue(headers map[string]any, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func headersToStrings(headers map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func approximateHeaderBytes(headers map[string]any) int {
	total := 0
	for k, v := range headers {
		total += len(k) + len(fmt.Sprint(v)) + 4
	}
	return total
}

func classifyRuntimeAsset(url string) (string, bool) {
	if strings.Contains(url, "/gosx/") || strings.Contains(url, "wasm_exec") || strings.Contains(url, ".wasm") {
		switch {
		case strings.Contains(url, ".wasm"):
			return "wasm", false
		case strings.Contains(url, "wasm_exec"):
			return "wasm-exec", false
		case strings.Contains(url, "bootstrap"):
			return "bootstrap", false
		default:
			return "runtime", false
		}
	}
	if strings.Contains(url, "bootstrap") || strings.Contains(url, "__gosx") {
		return "runtime-unknown", true
	}
	return "", false
}

func SummarizeBrowserSamples(samples []BrowserRawSample, runMode string, source SourceIdentity) BrowserSummary {
	values := map[string]map[string]map[string][]float64{}
	discarded := 0
	acceptedProduct := 0
	for _, s := range samples {
		if s.Discarded {
			discarded++
			continue
		}
		if s.SampleLane != SampleLaneProduct {
			continue
		}
		acceptedProduct++
		group := s.RouteID + "/" + s.CacheMode
		if values[group] == nil {
			values[group] = map[string]map[string][]float64{}
		}
		if values[group]["metrics"] == nil {
			values[group]["metrics"] = map[string][]float64{}
		}
		for k, v := range s.Metrics {
			values[group]["metrics"][k] = append(values[group]["metrics"][k], v)
		}
	}
	groups := map[string]map[string]MetricSet{}
	var flags []NoiseFlag
	for group, categories := range values {
		groups[group] = map[string]MetricSet{}
		for category, metrics := range categories {
			set := MetricSet{}
			for metric, vals := range metrics {
				stat := ComputeStats(vals)
				set[metric] = stat
				if stat.NoisyMAD {
					flags = append(flags, NoiseFlag{Group: group, Metric: metric, Reason: "MAD / median > 0.10", Ratio: safeRatio(stat.MAD, stat.Median)})
				}
				if stat.NoisyIQR {
					flags = append(flags, NoiseFlag{Group: group, Metric: metric, Reason: "IQR / median > 0.20", Ratio: safeRatio(stat.IQR, stat.Median)})
				}
			}
			groups[group][category] = set
		}
	}
	return BrowserSummary{
		SchemaVersion: BrowserBaselineSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		RunMode:       runMode,
		Source:        source,
		Groups:        groups,
		SampleCount:   acceptedProduct,
		Discarded:     discarded,
		NoiseFlags:    flags,
	}
}

func ComputeStats(values []float64) Stats {
	clean := make([]float64, 0, len(values))
	for _, v := range values {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			clean = append(clean, v)
		}
	}
	sort.Float64s(clean)
	if len(clean) == 0 {
		return Stats{}
	}
	median := percentile(clean, 50)
	dev := make([]float64, len(clean))
	for i, v := range clean {
		dev[i] = math.Abs(v - median)
	}
	sort.Float64s(dev)
	stat := Stats{
		N:      len(clean),
		Mean:   mean(clean),
		Median: median,
		P75:    percentile(clean, 75),
		P95:    percentile(clean, 95),
		P99:    percentile(clean, 99),
		Max:    clean[len(clean)-1],
		MAD:    percentile(dev, 50),
		IQR:    percentile(clean, 75) - percentile(clean, 25),
	}
	stat.NoisyMAD = median != 0 && stat.MAD/median > 0.10
	stat.NoisyIQR = median != 0 && stat.IQR/median > 0.20
	stat.Unstable = stat.NoisyMAD || stat.NoisyIQR
	return stat
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(rank-float64(lo))
}

func mean(v []float64) float64 {
	var total float64
	for _, n := range v {
		total += n
	}
	return total / float64(len(v))
}

func safeRatio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func browserSourceIdentity(ctx context.Context, opts BrowserBaselineOptions) (SourceIdentity, error) {
	var inv Inventory
	var invRef string
	strictInventory := false
	fresh := false
	if opts.InventoryPath != "" {
		f, err := os.Open(opts.InventoryPath)
		if err != nil {
			return SourceIdentity{}, err
		}
		decoded, err := DecodeInventoryStrict(f)
		_ = f.Close()
		if err != nil {
			return SourceIdentity{}, err
		}
		if err := ValidateInventoryFresh(ctx, opts.RepoRoot, decoded); err != nil {
			return SourceIdentity{}, err
		}
		inv = *decoded
		invRef = opts.InventoryPath
		strictInventory = true
		fresh = true
	} else {
		collected, err := Collect(ctx, CollectOptions{RepoRoot: opts.RepoRoot, ArtifactRoot: opts.ArtifactRoot, Canopy: false, Git: true})
		if err != nil {
			return SourceIdentity{}, err
		}
		inv = *collected
		invRef = filepath.Join(opts.ArtifactRoot, "source-inventory.json")
		if err := WriteJSONFile(invRef, inv); err != nil {
			return SourceIdentity{}, err
		}
	}
	hash, _ := fileSHA256(invRef)
	verifiedAt := ""
	if fresh {
		verifiedAt = time.Now().UTC().Format(time.RFC3339)
	}
	probeNames := RuntimeJSONKnownProductionNames(&inv)
	staticIdentity, err := collectRuntimeJSONStaticIdentity(ctx, RuntimeJSONProbeOptions{
		RepoRoot:     opts.RepoRoot,
		ArtifactRoot: opts.ArtifactRoot,
		GeneratedAt:  time.Now().UTC(),
	}, invRef)
	if err != nil {
		return SourceIdentity{}, err
	}
	compatibilityIdentity := compatibilityAuditIdentityFromInventory(inv.Surface.CompatibilityAudit)
	if err := requireRuntimeJSONStaticCompatibilityMatch(staticIdentity, compatibilityIdentity); err != nil {
		return SourceIdentity{}, err
	}
	reconstructionEvidence, err := ReplayInventoryReconstruction(ctx, opts.RepoRoot, &inv)
	reconstructionProof := err == nil && reconstructionEvidence.Isolated && reconstructionEvidence.Applied && reconstructionEvidence.Verified
	if err != nil {
		if strictInventory {
			return SourceIdentity{}, fmt.Errorf("reconstruction proof: %w", err)
		}
		reconstructionProof = false
	}
	var reconstruction *ReconstructionEvidence
	if reconstructionEvidence.Method != "" {
		reconstruction = &reconstructionEvidence
	}
	return SourceIdentity{
		BaseRevision:                 inv.BaseRevision,
		OverlayHash:                  inv.OverlayHash,
		TrackedDiffHash:              inv.Overlay.TrackedDiffHash,
		UntrackedIncludedSourceHash:  hashUntracked(inv.Overlay.UntrackedSources),
		InventoryRef:                 relTo(opts.ArtifactRoot, invRef),
		InventorySHA256:              hash,
		RejectsModuleCacheMismatch:   strictInventory,
		CurrentOverlayVerified:       fresh,
		CurrentOverlayVerificationAt: verifiedAt,
		StrictInventory:              strictInventory,
		ReconstructionProof:          reconstructionProof,
		Reconstruction:               reconstruction,
		RuntimeProbeNameCount:        len(probeNames),
		RuntimeProbeNames:            probeNames,
		RuntimeJSONStatic:            &staticIdentity,
		CompatibilityAudit:           compatibilityIdentity,
	}, nil
}

func CollectBrowserEnvironment(ctx context.Context, opts BrowserBaselineOptions) (EnvironmentReport, error) {
	selection, chromeErr := perf.ResolveAllocatorSelection(opts.ChromeWebSocketURL)
	node := resolveNodeExecutable()
	tools := map[string]string{
		"go":             commandVersion("go", "version"),
		"tinygo":         commandVersion("tinygo", "version"),
		"wasm-opt":       commandVersion("wasm-opt", "--version"),
		"node":           commandVersion(node, "--version"),
		"canopy":         commandVersion("canopy", "--version"),
		"gotreesitter":   "linked",
		"browserVersion": "unknown",
	}
	browser := map[string]any{
		"connectionMode": string(selection.Mode),
		"executable":     "unknown",
		"flags":          []string{"headless", "enable-unsafe-webgpu", "enable-unsafe-swiftshader", "use-gl=angle", "use-angle=gl-egl", "ignore-gpu-blocklist"},
		"headless":       opts.Headless,
		"profile":        "chromedp temporary profile",
		"closeSemantics": "owned-process",
	}
	if selection.Mode == perf.AllocatorModeRemote {
		browser["executable"] = "remote-cdp"
		browser["flags"] = "remote-not-controlled"
		browser["headless"] = "remote-not-controlled"
		browser["profile"] = "remote user-owned profile"
		browser["remoteEndpointSHA256"] = selection.RemoteWebSocketURLSHA256
		browser["closeSemantics"] = "session-only"
	} else if chromeErr == nil {
		browser["executable"] = safeCommandLabel(selection.ChromePath)
		tools["browserVersion"] = commandVersion(selection.ChromePath, "--version")
	}
	env := EnvironmentReport{
		SchemaVersion:    BrowserBaselineSchemaVersion,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		EnvironmentClass: opts.Environment,
		OS: map[string]string{
			"goos":   runtime.GOOS,
			"goarch": runtime.GOARCH,
			"kernel": commandVersion("uname", "-srvmo"),
			"wsl":    detectWSL(),
		},
		CPU: map[string]string{
			"model":        firstCPUModel(),
			"logicalCores": fmt.Sprint(runtime.NumCPU()),
			"goMaxProcs":   fmt.Sprint(runtime.GOMAXPROCS(0)),
			"goVersion":    runtime.Version(),
			"goExperiment": os.Getenv("GOEXPERIMENT"),
			"frequencyGov": readTrim("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"),
		},
		Memory: map[string]string{
			"memTotal":      meminfoValue("MemTotal"),
			"cgroupLimit":   readTrim("/sys/fs/cgroup/memory.max"),
			"cgroupV1Limit": readTrim("/sys/fs/cgroup/memory/memory.limit_in_bytes"),
		},
		Power: map[string]string{
			"battery":       firstGlobTrim("/sys/class/power_supply/BAT*/status"),
			"thermalSignal": firstGlobTrim("/sys/class/thermal/thermal_zone*/temp"),
			"powerProfile":  commandVersion("powerprofilesctl", "get"),
		},
		Tools:                  tools,
		Browser:                browser,
		Viewport:               map[string]any{"width": opts.ViewportWidth, "height": opts.ViewportHeight, "deviceScaleFactor": opts.DPR},
		GPU:                    map[string]any{"webgpu": "unknown", "webgl": "unknown"},
		Server:                 map[string]any{"command": "go run .", "port": opts.Port, "buildMode": "dev", "runtimeRoot": "unknown", "assetManifestHash": "unknown"},
		Network:                map[string]string{"mode": "localhost", "cacheMode": "cold-and-warm", "localhost": "true"},
		RuntimeManifest:        map[string]string{"identity": "unknown"},
		HardwareClassification: "headless-logic",
	}
	fillGPUIdentity(ctx, opts, &env)
	env.Unknowns = collectEnvironmentUnknowns(env)
	return env, nil
}

func fillGPUIdentity(ctx context.Context, opts BrowserBaselineOptions, env *EnvironmentReport) {
	d, err := newOuroborosDriver(ctx, opts, SampleLaneProduct)
	if err != nil {
		env.GPU["probeError"] = redactRemoteEndpointErrorForOptions(opts, err).Error()
		return
	}
	defer d.Close()
	probeDriver, probeCancel := d.WithOperationContext(ctx, opts.Timeout)
	defer probeCancel()
	if err := queryBrowserVersion(probeDriver, env); err != nil {
		redacted := redactRemoteEndpointErrorForOptions(opts, err).Error()
		env.Browser["versionProbeError"] = redacted
		env.GPU["probeError"] = redacted
		return
	}
	if opts.BaseURL != "" {
		_ = probeDriver.Navigate(opts.BaseURL)
		_ = probeDriver.WaitReady()
	}
	var gpu map[string]any
	if err := probeDriver.Evaluate(`(async function(){
		var out = {webgpuAvailable:false, webgl:{}, userAgent:navigator.userAgent};
		try {
			out.webgpuAvailable = !!navigator.gpu;
			if (navigator.gpu) {
				var adapter = await navigator.gpu.requestAdapter();
				out.webgpuAdapter = adapter ? {
					isFallbackAdapter: !!adapter.isFallbackAdapter,
					info: adapter.info || {}
				} : null;
			}
		} catch (e) { out.webgpuError = String(e && e.message || e); }
		try {
			var c = document.createElement("canvas");
			var gl = c.getContext("webgl2") || c.getContext("webgl");
			if (gl) {
				var dbg = gl.getExtension("WEBGL_debug_renderer_info");
				out.webgl = {
					vendor: dbg ? gl.getParameter(dbg.UNMASKED_VENDOR_WEBGL) : gl.getParameter(gl.VENDOR),
					renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER),
					version: gl.getParameter(gl.VERSION),
					extensions: gl.getSupportedExtensions() || []
				};
			}
		} catch (e) { out.webglError = String(e && e.message || e); }
		return out;
	})()`, &gpu); err != nil {
		env.GPU["probeError"] = redactRemoteEndpointErrorForOptions(opts, err).Error()
		return
	}
	env.GPU = gpu
	if ua, ok := gpu["userAgent"].(string); ok {
		env.Browser["userAgent"] = ua
	}
	env.HardwareClassification = classifyHardware(opts.Environment, gpu)
}

func queryBrowserVersion(d *perf.Driver, env *EnvironmentReport) error {
	return d.RunFunc(func(ctx context.Context) error {
		protocol, product, revision, userAgent, jsVersion, err := cdpBrowser.GetVersion().Do(ctx)
		if err != nil {
			return err
		}
		recordBrowserVersion(env, protocol, product, revision, userAgent, jsVersion)
		return nil
	})
}

func recordBrowserVersion(env *EnvironmentReport, protocol, product, revision, userAgent, jsVersion string) {
	if product != "" {
		env.Browser["product"] = product
		env.Tools["browserVersion"] = product
	}
	if protocol != "" {
		env.Browser["protocolVersion"] = protocol
	}
	if revision != "" {
		env.Browser["revision"] = revision
	}
	if userAgent != "" {
		env.Browser["userAgent"] = userAgent
	}
	if jsVersion != "" {
		env.Browser["jsVersion"] = jsVersion
	}
}

var remoteEndpointErrorPattern = regexp.MustCompile(`(?i)\b(?:wss?|https?)://[^\s"'<>]+`)

func redactRemoteEndpointError(err error) string {
	if err == nil {
		return ""
	}
	return remoteEndpointErrorPattern.ReplaceAllString(err.Error(), "remote-cdp-endpoint")
}

func redactRemoteEndpointErrorForOptions(opts BrowserBaselineOptions, err error) error {
	if err == nil || !remoteEndpointConfigured(opts) {
		return err
	}
	return fmt.Errorf("%s", redactRemoteEndpointTextForOptions(opts, err.Error()))
}

func sanitizeArtifactErrorForOptions(opts BrowserBaselineOptions, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", sanitizeArtifactTextForOptions(opts, err.Error()))
}

func sanitizeArtifactTextForOptions(opts BrowserBaselineOptions, text string) string {
	text = redactRemoteEndpointTextForOptions(opts, text)
	for _, value := range []string{
		strings.TrimSpace(os.Getenv("NODE")),
		strings.TrimSpace(os.Getenv("GOSX_BROWSER_EXECUTABLE")),
	} {
		if value == "" {
			continue
		}
		label := safeCommandLabel(value)
		text = strings.ReplaceAll(text, value, label)
		if clean := filepath.Clean(value); clean != value {
			text = strings.ReplaceAll(text, clean, label)
		}
	}
	return text
}

func redactRemoteEndpointTextForOptions(opts BrowserBaselineOptions, text string) string {
	redacted := remoteEndpointErrorPattern.ReplaceAllString(text, "remote-cdp-endpoint")
	raw := strings.TrimSpace(opts.ChromeWebSocketURL)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("CHROME_WS_URL"))
	}
	u, err := url.Parse(raw)
	if err != nil {
		return redacted
	}
	replacements := []string{raw, u.String(), u.Path, strings.TrimPrefix(u.Path, "/"), u.EscapedPath(), strings.TrimPrefix(u.EscapedPath(), "/"), u.RawQuery, u.Fragment}
	if base := pathBase(u.Path); base != "" {
		replacements = append(replacements, base)
	}
	if u.User != nil {
		replacements = append(replacements, u.User.Username())
		if pass, ok := u.User.Password(); ok {
			replacements = append(replacements, pass)
		}
	}
	for key, values := range u.Query() {
		replacements = append(replacements, key)
		replacements = append(replacements, values...)
	}
	for _, value := range replacements {
		if value == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, value, "remote-cdp-redacted")
	}
	return redacted
}

func pathBase(value string) string {
	value = strings.TrimRight(value, "/")
	if value == "" {
		return ""
	}
	idx := strings.LastIndex(value, "/")
	if idx >= 0 {
		return value[idx+1:]
	}
	return value
}

func remoteEndpointConfigured(opts BrowserBaselineOptions) bool {
	return strings.TrimSpace(opts.ChromeWebSocketURL) != "" || strings.TrimSpace(os.Getenv("CHROME_WS_URL")) != ""
}

func writeRemoteBoundaryFailure(opts BrowserBaselineOptions, err error) {
	writeBoundaryFailure(opts, err)
}

func writeBoundaryFailure(opts BrowserBaselineOptions, err error) {
	if err == nil || strings.TrimSpace(opts.ArtifactRoot) == "" {
		return
	}
	path := filepath.Join(opts.ArtifactRoot, "failure.json")
	if _, statErr := os.Stat(path); statErr == nil {
		return
	}
	_ = WriteJSONFile(path, BaselineValidation{
		Status: "fail",
		Errors: []string{sanitizeArtifactTextForOptions(opts, err.Error())},
	})
}

func classifyHardware(_ string, gpu map[string]any) string {
	if containsSoftwareRaster(gpu) {
		return "software-raster"
	}
	if hasObservedWebGPUAdapter(gpu) {
		return "hardware-webgpu"
	}
	if hasObservedWebGLRenderer(gpu) {
		return "hardware-webgl"
	}
	return "headless-logic"
}

func hasObservedWebGPUAdapter(gpu map[string]any) bool {
	adapter, ok := gpu["webgpuAdapter"].(map[string]any)
	if !ok || adapter == nil {
		return false
	}
	if fallback, ok := adapter["isFallbackAdapter"].(bool); ok && fallback {
		return false
	}
	return hasPositiveHardwareIdentity(adapter)
}

func hasObservedWebGLRenderer(gpu map[string]any) bool {
	webgl, ok := gpu["webgl"].(map[string]any)
	if !ok || webgl == nil {
		return false
	}
	renderer, _ := webgl["renderer"].(string)
	return isPositiveHardwareText(renderer)
}

func containsSoftwareRaster(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if containsSoftwareRaster(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsSoftwareRaster(child) {
				return true
			}
		}
	case []string:
		for _, child := range v {
			if containsSoftwareRaster(child) {
				return true
			}
		}
	case string:
		text := strings.ToLower(v)
		for _, token := range []string{
			"swiftshader",
			"swangle",
			"llvmpipe",
			"softpipe",
			"lavapipe",
			"software raster",
			"software renderer",
			"mesa offscreen",
			"microsoft basic",
			"basic render driver",
			"warp",
		} {
			if strings.Contains(text, token) {
				return true
			}
		}
	}
	return false
}

func hasPositiveHardwareIdentity(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "isFallbackAdapter" {
				continue
			}
			if hasPositiveHardwareIdentity(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if hasPositiveHardwareIdentity(child) {
				return true
			}
		}
	case []string:
		for _, child := range v {
			if hasPositiveHardwareIdentity(child) {
				return true
			}
		}
	case string:
		return isPositiveHardwareText(v)
	}
	return false
}

func isPositiveHardwareText(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return false
	}
	for _, generic := range []string{"unknown", "null", "undefined", "false", "true", "gpu", "webgpu", "webgl", "{}"} {
		if text == generic {
			return false
		}
	}
	return !containsSoftwareRaster(text)
}

type fixtureServer struct {
	cmd     *exec.Cmd
	baseURL string
	cancel  context.CancelFunc
	dir     string
	args    []string
	pid     int
}

func (s *fixtureServer) Close() {
	if s == nil {
		return
	}
	if s.cmd != nil {
		terminateFixtureCommand(s.cmd)
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func maybeStartFixtureServer(ctx context.Context, opts BrowserBaselineOptions, routes []FixtureSpec, log io.Writer) (*fixtureServer, error) {
	if !hasNonExternalRoute(routes) {
		return nil, nil
	}
	if !opts.Serve {
		if opts.BaseURL == "" {
			return nil, fmt.Errorf("--base-url is required unless --serve is set")
		}
		return nil, nil
	}
	app := opts.FixtureApp
	if app == "" {
		app = filepath.Join(opts.RepoRoot, "examples", "ouroboros-corpus")
	}
	return startGoRunFixtureServer(ctx, app, opts.Port, []string{"/readyz"}, log)
}

func maybeStartExternalDocsServer(ctx context.Context, opts BrowserBaselineOptions, routes []FixtureSpec, log io.Writer) (*fixtureServer, error) {
	route, ok := firstR10Route(routes)
	if !ok {
		return nil, nil
	}
	if opts.DocsBaseURL != "" {
		return nil, nil
	}
	if !opts.Serve {
		return nil, fmt.Errorf("external route %s requires --docs-base-url or --serve with docs fixture", route.ID)
	}
	app := route.FixtureApp
	if app == "" {
		app = "examples/gosx-docs"
	}
	if !filepath.IsAbs(app) {
		app = filepath.Join(opts.RepoRoot, filepath.FromSlash(app))
	}
	return startGoRunFixtureServer(ctx, app, 0, []string{"/readyz", "/demos/water"}, log)
}

func startGoRunFixtureServer(ctx context.Context, app string, port int, readyPaths []string, log io.Writer) (*fixtureServer, error) {
	if port == 0 {
		p, err := freePort()
		if err != nil {
			return nil, err
		}
		port = p
	}
	serverCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(serverCtx, "go", "run", ".")
	cmd.Dir = app
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
	cmd.Stdout = log
	cmd.Stderr = log
	configureFixtureCommand(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		if fixtureServerReady(ctx, client, baseURL, readyPaths) {
			return &fixtureServer{cmd: cmd, baseURL: baseURL, cancel: cancel, dir: app, args: []string{"go", "run", "."}, pid: cmd.Process.Pid}, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	cancel()
	terminateFixtureCommand(cmd)
	return nil, fmt.Errorf("fixture server did not become ready at %s", baseURL)
}

func fixtureServerReady(ctx context.Context, client *http.Client, baseURL string, readyPaths []string) bool {
	if len(readyPaths) == 0 {
		return false
	}
	for _, readyPath := range readyPaths {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+readyPath, nil)
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			return false
		}
	}
	return true
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitScene(d *perf.Driver, frames int) {
	deadline := time.Now().Add(10 * time.Second)
	expr := fmt.Sprintf(`window.__gosx_perf && window.__gosx_perf.frameCount >= %d`, frames)
	for time.Now().Before(deadline) {
		var ok bool
		if err := d.Evaluate(expr, &ok); err == nil && ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func isSceneRoute(route FixtureSpec) bool {
	for _, cap := range route.ExpectedCapabilities {
		c := strings.ToLower(cap)
		if c == "scene3d" || c == "webgpu" || c == "webgl" || c == "water" || c == "animation" {
			return true
		}
	}
	return strings.Contains(strings.ToLower(route.Route), "scene") || strings.Contains(strings.ToLower(route.Route), "water")
}

func logCommand(w io.Writer, name string, args []string) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339Nano), name, strings.Join(args, " "))
}

func resolveNodeExecutable() string {
	node := strings.TrimSpace(os.Getenv("NODE"))
	if node == "" {
		return "node"
	}
	return node
}

func safeCommandLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "command"
	}
	if base := filepath.Base(name); base != "." && base != string(filepath.Separator) && base != "" {
		return base
	}
	return name
}

func logBrowserCommand(w io.Writer, opts BrowserBaselineOptions, name string, args []string) {
	if !remoteEndpointConfigured(opts) {
		logCommand(w, name, args)
		return
	}
	redacted := make([]string, 0, len(args))
	for _, arg := range args {
		redacted = append(redacted, redactRemoteEndpointTextForOptions(opts, arg))
	}
	logCommand(w, name, redacted)
}

func commandVersion(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func detectWSL() string {
	version := strings.ToLower(readTrim("/proc/version"))
	if strings.Contains(version, "microsoft") || strings.Contains(version, "wsl") {
		return "true"
	}
	return "false"
}

func firstCPUModel() string {
	body, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func meminfoValue(key string) string {
	body, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "unknown"
	}
	prefix := key + ":"
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return "unknown"
}

func readTrim(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(body))
}

func firstGlobTrim(pattern string) string {
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "unknown"
	}
	return readTrim(matches[0])
}

func collectEnvironmentUnknowns(env EnvironmentReport) []string {
	var unknowns []string
	for group, vals := range map[string]map[string]string{"os": env.OS, "cpu": env.CPU, "memory": env.Memory, "power": env.Power, "tools": env.Tools, "network": env.Network, "runtimeManifest": env.RuntimeManifest} {
		for k, v := range vals {
			if v == "" || v == "unknown" {
				unknowns = append(unknowns, group+"."+k)
			}
		}
	}
	sort.Strings(unknowns)
	return unknowns
}

func DefaultProbeSchemaIdentity() ProbeSchemaIdentity {
	return ProbeSchemaIdentity{
		Name:          "gosx-ouroboros-cdp-probe",
		Version:       "1",
		Facade:        "__gosxOuroborosProbe",
		EventKinds:    []string{"mark", "navigation", "ready", "interaction", "resource", "longtask", "error", "runtime-call", "json-call"},
		InjectedByCDP: true,
		ProductAsset:  false,
	}
}
