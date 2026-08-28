package ouroboros

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const (
	SchemaVersion   = "gosx.ouroboros.baseline.v2"
	SchemaVersionV1 = "gosx.ouroboros.baseline.v1"
	ContractO02     = "O0.2"
	Initiative      = "initiative.gosx-ouroboros-runtime-refactor"
	Spec            = "spec.gosx-ouroboros-runtime.v0.1"
	CorpusID        = "gosx-ouroboros-o0.2-v1"
	OverlayClean    = "sha256:clean"
	// portableArtifactRoot is the serialized root for self-contained evidence
	// bundles. The physical collection directory is an execution detail; refs
	// inside an inventory are either repository-relative reconstruction refs or
	// bundle-relative evidence refs.
	portableArtifactRoot = "."
	canonicalLines       = 87086
	canonicalGosx        = 209
	canonicalJSON        = 253

	compatibilityAuditSchemaVersion = "gosx.ouroboros.compatibility-audit.v1"
	compatibilityAuditScope         = "client/js/bootstrap-src/**/*.js + client/wasm/**/*.go"
	compatibilityFullRuntimeScope   = "inventory.files.included + inventory.files.sidecars + inventory.files.embedded + client/wasm/**/*.go production owners"
	compatibilityReceiptMethod      = "gosx.ouroboros.compatibility-receipt.raw-name.v1"
	compatibilityReceiptClassifier  = "none"
	compatibilityFullMethod         = runtimeJSONStaticScannerVersion
	compatibilityFullClassifier     = runtimeJSONPhaseClassifierVersion
	compatibilityReceiptHash        = "sha256:4e844ef2a7c2b8ba37fe9889164d4b5ca4638a139d1e4f24f948a922bc785cea"
)

var (
	gosxNameRe      = regexp.MustCompile(`\b__gosx_[A-Za-z0-9_]*\b`)
	jsonCandidateRe = regexp.MustCompile(`\bJSON\.(parse|stringify)\b|\b(propsJSON|patchJSON|valueJSON)\b`)
	importExportRe  = regexp.MustCompile(`^\s*(import|export)\b`)
	globalReadRe    = regexp.MustCompile(`\b(window|globalThis)\.([A-Za-z_$][A-Za-z0-9_$]*)`)
	globalWriteRe   = regexp.MustCompile(`\b(window|globalThis)\.([A-Za-z_$][A-Za-z0-9_$]*)\s*=`)
	canopySymbolsRe = regexp.MustCompile(`\bsymbols=([0-9]+)\b`)
	canopyJSFilesRe = regexp.MustCompile(`javascript files=([0-9]+) symbols=([0-9]+)`)
	gitRevisionRe   = regexp.MustCompile(`^[a-f0-9]{7,40}$`)
)

//go:embed compatibility_receipt.v1.json
var compatibilityReceiptBytes []byte

type CollectOptions struct {
	RepoRoot     string
	ArtifactRoot string
	GeneratedAt  time.Time
	Canopy       bool
	Git          bool
}

type Inventory struct {
	SchemaVersion string            `json:"schemaVersion"`
	Contract      string            `json:"contractVersion"`
	Initiative    string            `json:"initiative"`
	Spec          string            `json:"spec"`
	CorpusID      string            `json:"corpusID"`
	BaseRevision  string            `json:"baseRevision"`
	OverlayHash   string            `json:"overlayHash"`
	GeneratedAt   string            `json:"generatedAt"`
	ArtifactRoot  string            `json:"artifactRoot"`
	Scope         Scope             `json:"scope"`
	Overlay       OverlayEvidence   `json:"overlay"`
	Files         FileInventory     `json:"files"`
	Totals        Totals            `json:"totals"`
	Structural    Structural        `json:"structural"`
	Surface       Surface           `json:"surface"`
	Drift         DriftReport       `json:"drift"`
	Ratchets      []ScopeRatchet    `json:"ratchets"`
	Pixels        *PixelArtifactRef `json:"pixels,omitempty"`
	Manifest      CorpusManifest    `json:"manifest"`
}

type Scope struct {
	Included []ScopeRule `json:"included"`
	Excluded []ScopeRule `json:"excluded"`
}

type ScopeRule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

type FileInventory struct {
	Included []SourceFile   `json:"included"`
	Sidecars []SourceFile   `json:"sidecars"`
	Embedded []SourceFile   `json:"embedded"`
	Excluded []ExcludedFile `json:"excluded"`
	Audit    []ExcludedFile `json:"audit"`
}

type OverlayEvidence struct {
	Status                string                `json:"status"`
	Hash                  string                `json:"hash"`
	BaseRevision          string                `json:"baseRevision"`
	TrackedDiffHash       string                `json:"trackedDiffHash"`
	TrackedCachedDiffHash string                `json:"trackedCachedDiffHash"`
	UntrackedSources      []UntrackedSourceHash `json:"untrackedSources"`
	ExcludedPaths         []OverlayExcludedPath `json:"excludedPaths,omitempty"`
	PatchPath             string                `json:"patchPath,omitempty"`
	ArchivePath           string                `json:"archivePath,omitempty"`
	Recreate              []string              `json:"recreate"`
}

type UntrackedSourceHash struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Mode   string `json:"mode"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type OverlayExcludedPath struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type SourceFile struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	SourceKind  string `json:"sourceKind"`
	Reason      string `json:"reason,omitempty"`
	Lines       int    `json:"lines"`
	Bytes       int64  `json:"bytes"`
	GzipBytes   int    `json:"gzipBytes"`
	BrotliBytes int    `json:"brotliBytes"`
	ParseOK     bool   `json:"parseOK"`
	ParseError  string `json:"parseError,omitempty"`
}

type ExcludedFile struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	Language string `json:"language,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
}

type Totals struct {
	IncludedFiles                 int            `json:"includedFiles"`
	IncludedJavaScriptLines       int            `json:"includedJavaScriptLines"`
	IncludedBytes                 int64          `json:"includedBytes"`
	IncludedGzipBytes             int            `json:"includedGzipBytes"`
	IncludedBrotliBytes           int            `json:"includedBrotliBytes"`
	SidecarFiles                  int            `json:"sidecarFiles"`
	SidecarJavaScriptLines        int            `json:"sidecarJavaScriptLines"`
	SidecarBytes                  int64          `json:"sidecarBytes"`
	EmbeddedFiles                 int            `json:"embeddedFiles"`
	EmbeddedJavaScriptLines       int            `json:"embeddedJavaScriptLines"`
	EmbeddedBytes                 int64          `json:"embeddedBytes"`
	BroaderBrowserFiles           int            `json:"broaderBrowserFiles"`
	BroaderBrowserJavaScriptLines int            `json:"broaderBrowserJavaScriptLines"`
	BroaderBrowserBytes           int64          `json:"broaderBrowserBytes"`
	ExcludedFiles                 int            `json:"excludedFiles"`
	AuditFiles                    int            `json:"auditFiles"`
	ByExtension                   map[string]int `json:"byExtension"`
	GeneratedFiles                int            `json:"generatedFiles"`
	VendorFiles                   int            `json:"vendorFiles"`
	ProbeFiles                    int            `json:"probeFiles"`
	TestFiles                     int            `json:"testFiles"`
	RuntimeTypeScriptFiles        int            `json:"runtimeTypeScriptFiles"`
	RuntimeTypeScriptExclusions   []string       `json:"runtimeTypeScriptExclusions,omitempty"`
	RuntimeSemanticGate           string         `json:"runtimeSemanticGate"`
	RuntimeAmbientFacade          string         `json:"runtimeAmbientFacade"`
}

type Structural struct {
	CanopyVersion    string       `json:"canopyVersion,omitempty"`
	CanopyFiles      int          `json:"canopyFiles,omitempty"`
	CanopySymbols    int          `json:"canopySymbols,omitempty"`
	CanopyRawStats   string       `json:"canopyRawStats,omitempty"`
	TopHotspots      []string     `json:"topHotspots,omitempty"`
	Complexity       string       `json:"complexitySummary,omitempty"`
	Gotreesitter     ParseSummary `json:"gotreesitter"`
	ImportsExports   []Location   `json:"importsExports"`
	FreeGlobalReads  []string     `json:"freeGlobalReads"`
	FreeGlobalWrites []string     `json:"freeGlobalWrites"`
}

type ParseSummary struct {
	Language string     `json:"language"`
	Parsed   int        `json:"parsed"`
	Failed   int        `json:"failed"`
	Failures []Location `json:"failures,omitempty"`
}

type Surface struct {
	GosxNames                     []GosxName          `json:"gosxNames"`
	GosxNameCount                 int                 `json:"gosxNameCount"`
	GosxProductionNameCount       int                 `json:"gosxProductionNameCount"`
	GosxJavaScriptNameCount       int                 `json:"gosxJavaScriptNameCount"`
	AssignedBrowserRootCount      int                 `json:"assignedBrowserRootCount"`
	AssignedWindowCount           int                 `json:"assignedWindowCount"`
	GoPublishedABICount           int                 `json:"goPublishedABICount"`
	HostCallbackCount             int                 `json:"hostCallbackCount"`
	BroaderBrowserGosxNames       []GosxName          `json:"broaderBrowserGosxNames"`
	BroaderBrowserNameCount       int                 `json:"broaderBrowserNameCount"`
	BroaderSerializationSiteCount int                 `json:"broaderSerializationSiteCount"`
	SerializationSites            []SerializationSite `json:"serializationSites"`
	SerializationSiteCount        int                 `json:"serializationSiteCount"`
	CompatibilityAudit            CompatibilityAudit  `json:"compatibilityAudit"`
	PublicFacadeCount             int                 `json:"publicFacadeCount"`
}

type GosxName struct {
	Name               string   `json:"name"`
	Owners             []string `json:"owners"`
	SourceFamilies     []string `json:"sourceFamilies"`
	CompatibilityClass string   `json:"compatibilityClass"`
}

type SerializationSite struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Kind  string `json:"kind"`
	Phase string `json:"phase"`
	Text  string `json:"text"`
}

type CompatibilityAudit struct {
	SchemaVersion      string                       `json:"schemaVersion"`
	Status             string                       `json:"status"`
	CanonicalAvailable bool                         `json:"canonicalAvailable"`
	Receipt            CompatibilityNameSetEvidence `json:"receipt"`
	Anchor             CompatibilityNameSetEvidence `json:"anchor"`
	Current            CompatibilityNameSetEvidence `json:"current"`
	Reconciliation     CompatibilityReconciliation  `json:"reconciliation"`
	Notes              []string                     `json:"notes,omitempty"`
}

type CompatibilityNameSetEvidence struct {
	SourceIdentity                CompatibilitySourceIdentity `json:"sourceIdentity"`
	Scope                         string                      `json:"scope"`
	MethodVersion                 string                      `json:"methodVersion"`
	ClassifierVersion             string                      `json:"classifierVersion"`
	Names                         []string                    `json:"names"`
	Count                         int                         `json:"count"`
	NameSetHash                   string                      `json:"nameSetHash"`
	EvidenceHash                  string                      `json:"evidenceHash"`
	RuntimeJSONSourceIdentityHash string                      `json:"runtimeJSONSourceIdentityHash,omitempty"`
	RuntimeJSONSemanticHash       string                      `json:"runtimeJSONSemanticHash,omitempty"`
	RuntimeJSONCountsHash         string                      `json:"runtimeJSONCountsHash,omitempty"`
	RuntimeJSONGlobalNameHash     string                      `json:"runtimeJSONGlobalNameHash,omitempty"`
}

type CompatibilitySourceIdentity struct {
	Kind         string `json:"kind"`
	Revision     string `json:"revision,omitempty"`
	OverlayHash  string `json:"overlayHash,omitempty"`
	ArtifactPath string `json:"artifactPath,omitempty"`
}

type CompatibilityReconciliation struct {
	RecoveredPreexisting []string `json:"recoveredPreexisting"`
	AddedSinceAnchor     []string `json:"addedSinceAnchor"`
	RemovedSinceAnchor   []string `json:"removedSinceAnchor"`
	MissingFromAnchor    []string `json:"missingFromAnchor"`
}

type compatibilityReceiptArtifact struct {
	SchemaVersion string   `json:"schemaVersion"`
	Count         int      `json:"count"`
	NameSetHash   string   `json:"nameSetHash"`
	Names         []string `json:"names"`
}

type Location struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text,omitempty"`
}

type DriftReport struct {
	Status           string    `json:"status"`
	DriftExplained   bool      `json:"driftExplained"`
	AcceptedDecision string    `json:"acceptedDecision,omitempty"`
	Canonical        Canonical `json:"canonical"`
	Measured         Canonical `json:"measured"`
	Deltas           Canonical `json:"deltas"`
	Notes            []string  `json:"notes,omitempty"`
}

type Canonical struct {
	JavaScriptLines          int    `json:"javascriptLines"`
	JavaScriptBytes          int64  `json:"javascriptBytes,omitempty"`
	GosxNameCount            int    `json:"gosxNameCount"`
	GosxProductionNameCount  int    `json:"gosxProductionNameCount,omitempty"`
	GosxJavaScriptNameCount  int    `json:"gosxJavaScriptNameCount,omitempty"`
	AssignedBrowserRootCount int    `json:"assignedBrowserRootCount,omitempty"`
	AssignedWindowCount      int    `json:"assignedWindowCount,omitempty"`
	GoPublishedABICount      int    `json:"goPublishedABICount,omitempty"`
	HostCallbackCount        int    `json:"hostCallbackCount,omitempty"`
	SerializationSiteCount   int    `json:"serializationSiteCount"`
	SerializationStatus      string `json:"serializationStatus,omitempty"`
}

type ScopeRatchet struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Target     int64  `json:"target,omitempty"`
	Measured   int64  `json:"measured,omitempty"`
	Delta      int64  `json:"delta,omitempty"`
	Status     string `json:"status"`
	Definition string `json:"definition"`
}

type PixelArtifactRef struct {
	SchemaVersion string            `json:"schemaVersion"`
	ManifestPath  string            `json:"manifestPath"`
	Initial       []PixelCaptureRef `json:"initial"`
	Settled       []PixelCaptureRef `json:"settled"`
}

type PixelCaptureRef struct {
	RouteID string `json:"routeID"`
	Backend string `json:"backend"`
	Path    string `json:"path"`
	Hash    string `json:"hash,omitempty"`
}

type CorpusManifest struct {
	SchemaVersion string            `json:"schemaVersion"`
	Contract      string            `json:"contractVersion"`
	Initiative    string            `json:"initiative"`
	Spec          string            `json:"spec"`
	CorpusID      string            `json:"corpusID"`
	BaseRevision  string            `json:"baseRevision"`
	OverlayHash   string            `json:"overlayHash"`
	GeneratedAt   string            `json:"generatedAt"`
	ArtifactRoot  string            `json:"artifactRoot"`
	Canonical     Canonical         `json:"canonicalTargets"`
	Scope         Scope             `json:"scope"`
	FixtureRoutes []FixtureRoute    `json:"fixtureRoutes"`
	Variants      []RuntimeVariant  `json:"runtimeVariants"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type FixtureRoute struct {
	ID                    string   `json:"id"`
	Route                 string   `json:"route"`
	FixtureApp            string   `json:"fixtureApp"`
	Purpose               string   `json:"purpose"`
	ExpectedRuntime       string   `json:"expectedRuntime"`
	ExpectedTinyGoCurrent string   `json:"expectedTinyGoCurrent"`
	ExpectedTinyGoFuture  string   `json:"expectedTinyGoFuture"`
	Capabilities          []string `json:"expectedCapabilities"`
}

type RuntimeVariant struct {
	ID               string       `json:"id"`
	Generation       string       `json:"generation"`
	Status           string       `json:"status"`
	SizeBytes        *int64       `json:"sizeBytes"`
	BudgetBytes      *int64       `json:"budgetBytes"`
	SizeArtifact     *ArtifactRef `json:"sizeArtifact"`
	WASMArtifact     *ArtifactRef `json:"wasmArtifact"`
	SelectedByRoutes []string     `json:"selectedByRoutes"`
}

type ArtifactRef struct {
	SchemaVersion string `json:"schemaVersion"`
	Path          string `json:"path"`
	BaseRevision  string `json:"baseRevision"`
	OverlayHash   string `json:"overlayHash"`
	SHA256        string `json:"sha256,omitempty"`
}

func DefaultScope() Scope {
	return Scope{
		Included: []ScopeRule{
			{Pattern: "client/js/bootstrap-src/**/*.js", Reason: "first-party authored browser runtime source"},
			{Pattern: "client/js/{patch,relay,stripe-bridge}.js", Reason: "first-party hand-authored browser runtime sidecars, outside the historical line scoreboard"},
			{Pattern: "//go:embed browser-source patterns (*.js, *.ts, *.tsx)", Reason: "first-party embedded browser runtime source outside client/js"},
		},
		Excluded: []ScopeRule{
			{Pattern: "client/js/bootstrap*.js", Reason: "generated deployment JavaScript bundles"},
			{Pattern: "client/js/*.js.map, client/js/*.gz, client/js/*.br", Reason: "source maps and compressed sidecars"},
			{Pattern: "**/*test*.js, **/*.test.mjs, scripts/**/*.mjs", Reason: "benchmark, test, and probe scripts"},
			{Pattern: "**/wasm_exec.js", Reason: "Go or TinyGo distribution shim"},
			{Pattern: "vendor JavaScript sidecars", Reason: "third-party browser code such as HLS"},
			{Pattern: "examples/**/public/**/*.js", Reason: "example application public JavaScript"},
			{Pattern: "client/runtime/generated/**/*.ts", Reason: "generated O0.1 ABI scaffolding, not runtime authoring source"},
		},
	}
}

func Collect(ctx context.Context, opts CollectOptions) (*Inventory, error) {
	root := opts.RepoRoot
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	inv := &Inventory{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		ArtifactRoot:  portableArtifactRoot,
		Scope:         DefaultScope(),
		Totals: Totals{
			ByExtension: map[string]int{},
		},
		Files: FileInventory{
			Included: []SourceFile{},
			Sidecars: []SourceFile{},
			Embedded: []SourceFile{},
			Excluded: []ExcludedFile{},
			Audit:    []ExcludedFile{},
		},
		Manifest: DefaultCorpusManifest(),
	}
	inv.Manifest.GeneratedAt = inv.GeneratedAt
	inv.Manifest.ArtifactRoot = portableArtifactRoot

	if opts.Git {
		base, err := gitOutput(ctx, absRoot, "git", "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		inv.BaseRevision = strings.TrimSpace(base)
		overlay, err := BuildOverlayEvidence(ctx, absRoot, inv.BaseRevision)
		if err != nil {
			return nil, err
		}
		inv.Overlay = overlay
		inv.OverlayHash = overlay.Hash
	}
	if inv.BaseRevision == "" {
		inv.BaseRevision = "unknown"
	}
	if inv.OverlayHash == "" {
		inv.OverlayHash = OverlayClean
	}
	if inv.Overlay.Hash == "" {
		inv.Overlay = OverlayEvidence{
			Status:       "clean",
			Hash:         inv.OverlayHash,
			BaseRevision: inv.BaseRevision,
			Recreate:     []string{"git checkout " + inv.BaseRevision},
		}
	}
	anchorManifestArtifacts(&inv.Manifest, inv.BaseRevision, inv.OverlayHash)

	if err := collectFiles(absRoot, inv); err != nil {
		return nil, err
	}
	if err := collectCompatibilitySurface(absRoot, inv); err != nil {
		return nil, err
	}
	if err := recomputeGosxSurfaceCounts(absRoot, inv); err != nil {
		return nil, err
	}
	if err := collectCompatibilityAudit(ctx, absRoot, inv); err != nil {
		return nil, err
	}
	if opts.Canopy {
		fillCanopy(ctx, absRoot, inv)
	}
	fillDrift(inv)
	return inv, nil
}

func DefaultCorpusManifest() CorpusManifest {
	return CorpusManifest{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		Canonical: Canonical{
			JavaScriptLines:          canonicalLines,
			JavaScriptBytes:          3815610,
			GosxNameCount:            canonicalGosx,
			GosxProductionNameCount:  208,
			GosxJavaScriptNameCount:  178,
			AssignedBrowserRootCount: 121,
			AssignedWindowCount:      120,
			GoPublishedABICount:      64,
			HostCallbackCount:        6,
			SerializationSiteCount:   canonicalJSON,
			SerializationStatus:      "historical-query-unreproducible-fail-closed",
		},
		Scope: DefaultScope(),
		FixtureRoutes: []FixtureRoute{
			{ID: "R00", Route: "/static", FixtureApp: "examples/ouroboros-corpus", Purpose: "SSR only", ExpectedRuntime: "none", ExpectedTinyGoCurrent: "none", ExpectedTinyGoFuture: "none", Capabilities: []string{"ssr"}},
			{ID: "R01", Route: "/lite", FixtureApp: "examples/ouroboros-corpus", Purpose: "declarative regions/actions without WASM", ExpectedRuntime: "lite", ExpectedTinyGoCurrent: "none", ExpectedTinyGoFuture: "core", Capabilities: []string{"regions", "actions"}},
			{ID: "R02", Route: "/island/counter", FixtureApp: "examples/ouroboros-corpus", Purpose: "one visual island and one action", ExpectedRuntime: "islands", ExpectedTinyGoCurrent: "islands", ExpectedTinyGoFuture: "core", Capabilities: []string{"island", "action"}},
			{ID: "R03", Route: "/islands/kitchen", FixtureApp: "examples/ouroboros-corpus", Purpose: "multiple islands and shared signals", ExpectedRuntime: "islands", ExpectedTinyGoCurrent: "islands", ExpectedTinyGoFuture: "core", Capabilities: []string{"islands", "signals"}},
			{ID: "R04", Route: "/action/form", FixtureApp: "examples/ouroboros-corpus", Purpose: "server action validation and redirect", ExpectedRuntime: "action bridge", ExpectedTinyGoCurrent: "none", ExpectedTinyGoFuture: "core", Capabilities: []string{"actions", "redirect"}},
			{ID: "R05", Route: "/canvas-board", FixtureApp: "examples/ouroboros-corpus", Purpose: "CanvasBoard engine surface", ExpectedRuntime: "engine", ExpectedTinyGoCurrent: "runtime", ExpectedTinyGoFuture: "engine", Capabilities: []string{"canvas", "engine"}},
			{ID: "R06", Route: "/hub/echo", FixtureApp: "examples/ouroboros-corpus", Purpose: "hub bind, fanout, and shared signal update", ExpectedRuntime: "collab", ExpectedTinyGoCurrent: "runtime", ExpectedTinyGoFuture: "collab", Capabilities: []string{"hub", "signals"}},
			{ID: "R07", Route: "/video-sync", FixtureApp: "examples/ouroboros-corpus", Purpose: "video engine and drift bridge", ExpectedRuntime: "video", ExpectedTinyGoCurrent: "runtime", ExpectedTinyGoFuture: "engine", Capabilities: []string{"video", "engine"}},
			{ID: "R08", Route: "/scene/basic", FixtureApp: "examples/ouroboros-corpus", Purpose: "bounded Scene3D PBR scene", ExpectedRuntime: "scene3d", ExpectedTinyGoCurrent: "runtime", ExpectedTinyGoFuture: "engine", Capabilities: []string{"scene3d", "webgpu", "webgl"}},
			{ID: "R09A", Route: "/navigation/a", FixtureApp: "examples/ouroboros-corpus", Purpose: "client navigation entry route", ExpectedRuntime: "navigation", ExpectedTinyGoCurrent: "none", ExpectedTinyGoFuture: "core", Capabilities: []string{"navigation", "dispose"}},
			{ID: "R09B", Route: "/navigation/b", FixtureApp: "examples/ouroboros-corpus", Purpose: "client navigation target route", ExpectedRuntime: "navigation", ExpectedTinyGoCurrent: "none", ExpectedTinyGoFuture: "core", Capabilities: []string{"navigation", "dispose"}},
			{ID: "R10", Route: "/demos/water", FixtureApp: "examples/gosx-docs", Purpose: "flagship heavy Scene3D route", ExpectedRuntime: "water scene3d", ExpectedTinyGoCurrent: "runtime", ExpectedTinyGoFuture: "engine", Capabilities: []string{"scene3d", "webgpu", "webgl", "water"}},
		},
		Variants: []RuntimeVariant{
			{ID: "runtime", Generation: "current", Status: "measured", SizeBytes: int64Pointer(1564711), BudgetBytes: int64Pointer(1564711), SizeArtifact: variantArtifact("sizes/runtime.json"), WASMArtifact: variantArtifact("wasm/runtime.wasm"), SelectedByRoutes: []string{"R05", "R06", "R07", "R08", "R10"}},
			{ID: "islands", Generation: "current", Status: "measured", SizeBytes: int64Pointer(801474), BudgetBytes: int64Pointer(801474), SizeArtifact: variantArtifact("sizes/islands.json"), WASMArtifact: variantArtifact("wasm/islands.wasm"), SelectedByRoutes: []string{"R02", "R03"}},
			{ID: "core", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R01", "R02", "R03", "R04", "R09A", "R09B"}},
			{ID: "engine", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R05", "R07", "R08", "R10"}},
			{ID: "collab", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R06"}},
			{ID: "full", Generation: "future", Status: "planned", SelectedByRoutes: []string{}},
		},
	}
}

func int64Pointer(value int64) *int64 { return &value }

func variantArtifact(path string) *ArtifactRef {
	return &ArtifactRef{
		SchemaVersion: "gosx.ouroboros.artifact-ref.v1",
		Path:          path,
		BaseRevision:  "__required_at_runtime__",
		OverlayHash:   OverlayClean,
	}
}

func anchorManifestArtifacts(manifest *CorpusManifest, baseRevision, overlayHash string) {
	manifest.BaseRevision = baseRevision
	manifest.OverlayHash = overlayHash
	for i := range manifest.Variants {
		if manifest.Variants[i].SizeArtifact != nil {
			manifest.Variants[i].SizeArtifact.BaseRevision = baseRevision
			manifest.Variants[i].SizeArtifact.OverlayHash = overlayHash
		}
		if manifest.Variants[i].WASMArtifact != nil {
			manifest.Variants[i].WASMArtifact.BaseRevision = baseRevision
			manifest.Variants[i].WASMArtifact.OverlayHash = overlayHash
		}
	}
}

func WriteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func WriteOverlayArtifacts(ctx context.Context, repoRoot, artifactDir string, evidence OverlayEvidence) error {
	if artifactDir == "" {
		return nil
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"tracked-overlay.patch", "overlay.untracked.json", "untracked-sources"} {
		target, err := safeJoin(artifactDir, name)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	patch, err := gitOutput(ctx, repoRoot, "git", trackedOverlayDiffArgs()...)
	if err != nil {
		return err
	}
	patchPath, err := safeJoin(artifactDir, "tracked-overlay.patch")
	if err != nil {
		return err
	}
	if err := os.WriteFile(patchPath, []byte(patch), 0o644); err != nil {
		return err
	}
	manifestPath, err := safeJoin(artifactDir, "overlay.untracked.json")
	if err != nil {
		return err
	}
	manifestFile, err := os.Create(manifestPath)
	if err != nil {
		return err
	}
	if err := WriteJSON(manifestFile, evidence.UntrackedSources); err != nil {
		_ = manifestFile.Close()
		return err
	}
	if err := manifestFile.Close(); err != nil {
		return err
	}
	contentRoot, err := safeJoin(artifactDir, "untracked-sources")
	if err != nil {
		return err
	}
	for _, src := range evidence.UntrackedSources {
		to, err := safeJoin(contentRoot, src.Path)
		if err != nil {
			return err
		}
		current, body, err := readUntrackedSource(repoRoot, src.Path)
		if err != nil {
			return err
		}
		if current.SHA256 != src.SHA256 || current.Mode != src.Mode || current.Type != src.Type {
			return fmt.Errorf("untracked source %s hash changed before archive write", src.Path)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		switch src.Type {
		case "symlink":
			if err := validateSafeSymlinkTarget(string(body)); err != nil {
				return err
			}
			_ = os.Remove(to)
			if err := os.Symlink(string(body), to); err != nil {
				return err
			}
		default:
			mode, err := strconv.ParseUint(src.Mode, 8, 32)
			if err != nil {
				return err
			}
			if err := os.WriteFile(to, body, os.FileMode(mode)); err != nil {
				return err
			}
			if err := os.Chmod(to, os.FileMode(mode)); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectFiles(root string, inv *Inventory) error {
	includeRoot := filepath.Join(root, "client", "js", "bootstrap-src")
	if err := filepath.WalkDir(includeRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// bootstrap-src is mixed .js/.ts. Collecting only .js drops every
		// migrated source from the inventory and reads as anchor drift.
		if d.IsDir() || !isBrowserSourceExt(path) {
			return nil
		}
		return collectIncludedFile(root, path, inv)
	}); err != nil {
		return err
	}
	sidecarRoot := filepath.Join(root, "client", "js")
	if err := filepath.WalkDir(sidecarRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != sidecarRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".js" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isFirstPartySidecar(rel) {
			return collectSidecarFile(root, path, inv)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := collectEmbeddedBrowserSources(root, inv); err != nil {
		return err
	}
	if err := collectRuntimeHostSources(root, inv); err != nil {
		return err
	}
	if err := collectRuntimeSceneSources(root, inv); err != nil {
		return err
	}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".canopy" || name == "node_modules" || name == "dist" || name == "build" || name == ".worktrees" || name == "tmp" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isIncludedRuntimeSource(rel) {
			return nil
		}
		if isCollectedSource(rel, inv) {
			return nil
		}
		if ex, ok := classifyExcluded(rel); ok {
			info, statErr := d.Info()
			if statErr == nil {
				ex.Bytes = info.Size()
			}
			inv.Files.Excluded = append(inv.Files.Excluded, ex)
			inv.Totals.ByExtension[strings.ToLower(filepath.Ext(rel))]++
			switch ex.Kind {
			case "generated":
				inv.Totals.GeneratedFiles++
			case "vendor":
				inv.Totals.VendorFiles++
			case "probe":
				inv.Totals.ProbeFiles++
			case "test":
				inv.Totals.TestFiles++
			}
			if ex.Language == "typescript" && strings.Contains(rel, "client/runtime/") {
				inv.Totals.RuntimeTypeScriptExclusions = append(inv.Totals.RuntimeTypeScriptExclusions, rel)
			}
			return nil
		}
		if isBrowserSourceExt(rel) {
			inv.Files.Audit = append(inv.Files.Audit, ExcludedFile{
				Path:     rel,
				Kind:     "unclassified",
				Reason:   "browser source candidate not served, embedded, generated, vendor, probe, test, or explicit runtime source",
				Language: languageForPath(rel),
			})
			inv.Totals.ByExtension[strings.ToLower(filepath.Ext(rel))]++
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(inv.Files.Included, func(i, j int) bool { return inv.Files.Included[i].Path < inv.Files.Included[j].Path })
	sort.Slice(inv.Files.Sidecars, func(i, j int) bool { return inv.Files.Sidecars[i].Path < inv.Files.Sidecars[j].Path })
	sort.Slice(inv.Files.Embedded, func(i, j int) bool { return inv.Files.Embedded[i].Path < inv.Files.Embedded[j].Path })
	sort.Slice(inv.Files.Excluded, func(i, j int) bool { return inv.Files.Excluded[i].Path < inv.Files.Excluded[j].Path })
	sort.Slice(inv.Files.Audit, func(i, j int) bool { return inv.Files.Audit[i].Path < inv.Files.Audit[j].Path })
	sort.Strings(inv.Structural.FreeGlobalReads)
	sort.Strings(inv.Structural.FreeGlobalWrites)
	sort.Slice(inv.Structural.ImportsExports, func(i, j int) bool {
		if inv.Structural.ImportsExports[i].Path == inv.Structural.ImportsExports[j].Path {
			return inv.Structural.ImportsExports[i].Line < inv.Structural.ImportsExports[j].Line
		}
		return inv.Structural.ImportsExports[i].Path < inv.Structural.ImportsExports[j].Path
	})
	inv.Totals.IncludedFiles = len(inv.Files.Included)
	inv.Totals.SidecarFiles = len(inv.Files.Sidecars)
	inv.Totals.EmbeddedFiles = len(inv.Files.Embedded)
	inv.Totals.ExcludedFiles = len(inv.Files.Excluded)
	inv.Totals.AuditFiles = len(inv.Files.Audit)
	inv.Totals.BroaderBrowserFiles = inv.Totals.IncludedFiles + inv.Totals.SidecarFiles + inv.Totals.EmbeddedFiles
	inv.Totals.BroaderBrowserJavaScriptLines = inv.Totals.IncludedJavaScriptLines + inv.Totals.SidecarJavaScriptLines + inv.Totals.EmbeddedJavaScriptLines
	inv.Totals.BroaderBrowserBytes = inv.Totals.IncludedBytes + inv.Totals.SidecarBytes + inv.Totals.EmbeddedBytes
	inv.Totals.RuntimeTypeScriptFiles = 0
	// O6 is not inferred from a suffix count. The executable gates live in
	// cmd/buildbootstrap: strict generated-contract type checking, typed-source
	// transpilation/closure, complete source-graph reachability and the single
	// ambient compatibility adapter audit.
	inv.Totals.RuntimeSemanticGate = "cmd/buildbootstrap + make test-runtime-types"
	inv.Totals.RuntimeAmbientFacade = "client/runtime/host/compatibility.ts"
	sort.Strings(inv.Totals.RuntimeTypeScriptExclusions)
	return nil
}

func collectSidecarFile(root, path string, inv *Inventory) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := countLines(body)
	src := SourceFile{
		Path:        rel,
		Language:    "javascript",
		SourceKind:  "sidecar",
		Reason:      "first-party hand-authored browser runtime sidecar served from /gosx",
		Lines:       lines,
		Bytes:       int64(len(body)),
		GzipBytes:   compressedSize(body, "gzip"),
		BrotliBytes: compressedSize(body, "brotli"),
	}
	if err := parseJavaScript(body); err != nil {
		src.ParseError = err.Error()
	} else {
		src.ParseOK = true
	}
	inv.Files.Sidecars = append(inv.Files.Sidecars, src)
	inv.Totals.SidecarJavaScriptLines += lines
	inv.Totals.SidecarBytes += int64(len(body))
	collectTextEvidence(rel, string(body), inv)
	return nil
}

func collectEmbeddedBrowserSources(root string, inv *Inventory) error {
	seen := map[string]bool{}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipAuditDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		for _, pattern := range embedPatterns(string(body)) {
			matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
			if err != nil {
				return err
			}
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || info.IsDir() || !isBrowserSourceExt(match) {
					continue
				}
				rel, _ := filepath.Rel(root, match)
				rel = filepath.ToSlash(rel)
				if seen[rel] || isIncludedRuntimeSource(rel) || isFirstPartySidecar(rel) {
					continue
				}
				if _, excluded := classifyExcluded(rel); excluded {
					continue
				}
				seen[rel] = true
				if err := collectEmbeddedFile(root, match, inv); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func collectEmbeddedFile(root, path string, inv *Inventory) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := countLines(body)
	src := SourceFile{
		Path:        rel,
		Language:    languageForPath(rel),
		SourceKind:  "embedded",
		Reason:      "first-party browser source discovered from //go:embed",
		Lines:       lines,
		Bytes:       int64(len(body)),
		GzipBytes:   compressedSize(body, "gzip"),
		BrotliBytes: compressedSize(body, "brotli"),
	}
	if isBrowserSourceExt(rel) {
		if err := parseBrowserSource(rel, body); err != nil {
			src.ParseError = err.Error()
		} else {
			src.ParseOK = true
		}
	}
	inv.Files.Embedded = append(inv.Files.Embedded, src)
	inv.Totals.EmbeddedJavaScriptLines += lines
	inv.Totals.EmbeddedBytes += int64(len(body))
	collectTextEvidence(rel, string(body), inv)
	return nil
}

func embedPatterns(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:embed") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "//go:embed")))
		out = append(out, fields...)
	}
	return out
}

func skipAuditDir(name string) bool {
	switch name {
	case ".git", ".canopy", ".worktrees", "build", "dist", "tmp", "node_modules":
		return true
	default:
		return false
	}
}

// runtimeHostSourceScanAllowlist names the client/runtime/host typed-authority
// modules the O2 static scan pulls in directly, ahead of their own
// navigation_asset.go-style go:embed graduation (compare navigation_asset.go,
// which already embeds compatibility.ts and navigation.ts). Each entry here
// owns at least one ambient name that gosxHostCompatibility.install(...)
// publishes and that has no other still-scanned source (legacy sidecar under
// client/js/, or a client/wasm/*.go caller) keeping it visible. Growing this
// list is a scan-completeness fix, not a receipt or classifier version
// change: it does not alter canonicalGosx, compatibilityReceiptHash, or any
// governance-gated denominator.
var runtimeHostSourceScanAllowlist = map[string]bool{
	"client/runtime/host/actions.ts":         true,
	"client/runtime/host/dom.ts":             true,
	"client/runtime/host/engine-disposal.ts": true,
	"client/runtime/host/events.ts":          true,
	"client/runtime/host/facade.ts":          true,
	"client/runtime/host/regions.ts":         true,
	"client/runtime/host/stream.ts":          true,
}

func collectRuntimeHostSources(root string, inv *Inventory) error {
	hostRoot := filepath.Join(root, "client", "runtime", "host")
	if _, err := os.Stat(hostRoot); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(hostRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if !runtimeHostSourceScanAllowlist[rel] {
			return nil
		}
		if isCollectedSource(rel, inv) {
			return nil
		}
		if _, excluded := classifyExcluded(rel); excluded {
			return nil
		}
		return collectRuntimeHostFile(root, path, inv)
	})
}

func collectRuntimeHostFile(root, path string, inv *Inventory) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := countLines(body)
	src := SourceFile{
		Path:        rel,
		Language:    languageForPath(rel),
		SourceKind:  "runtime-host",
		Reason:      "first-party typed-authority browser host source, embedded on its own migration schedule",
		Lines:       lines,
		Bytes:       int64(len(body)),
		GzipBytes:   compressedSize(body, "gzip"),
		BrotliBytes: compressedSize(body, "brotli"),
	}
	if err := parseJavaScript(body); err != nil {
		src.ParseError = err.Error()
	} else {
		src.ParseOK = true
	}
	inv.Files.Sidecars = append(inv.Files.Sidecars, src)
	inv.Totals.SidecarJavaScriptLines += lines
	inv.Totals.SidecarBytes += int64(len(body))
	collectTextEvidence(rel, string(body), inv)
	return nil
}

// collectRuntimeSceneSources scans client/runtime/scene3d, the O3 slice
// destination for the scene3d mount, backend-selection, and WebGL/WebGPU
// modules that used to live under client/js/bootstrap-src (compare
// collectRuntimeHostSources, which does the same job for the O2 host move).
// Unlike the host directory, every *.ts file under client/runtime/scene3d is
// a cmd/buildbootstrap production source (see the sourceFile("../runtime/
// scene3d/...") calls in cmd/buildbootstrap/main.go), so a directory scan
// tracks the bundler's file set without a hand-maintained allowlist that
// would need editing on every new scene3d module. This is a scan-
// completeness fix, not a receipt or classifier version change: it does not
// alter canonicalGosx, compatibilityReceiptHash, or any governance-gated
// denominator.
func collectRuntimeSceneSources(root string, inv *Inventory) error {
	sceneRoot := filepath.Join(root, "client", "runtime", "scene3d")
	if _, err := os.Stat(sceneRoot); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(sceneRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".ts" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isCollectedSource(rel, inv) {
			return nil
		}
		if _, excluded := classifyExcluded(rel); excluded {
			return nil
		}
		return collectRuntimeSceneFile(root, path, inv)
	})
}

func collectRuntimeSceneFile(root, path string, inv *Inventory) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := countLines(body)
	src := SourceFile{
		Path:        rel,
		Language:    languageForPath(rel),
		SourceKind:  "runtime-scene3d",
		Reason:      "first-party typed-authority scene3d browser runtime source, bundled by cmd/buildbootstrap",
		Lines:       lines,
		Bytes:       int64(len(body)),
		GzipBytes:   compressedSize(body, "gzip"),
		BrotliBytes: compressedSize(body, "brotli"),
	}
	if err := parseJavaScript(body); err != nil {
		src.ParseError = err.Error()
	} else {
		src.ParseOK = true
	}
	inv.Files.Sidecars = append(inv.Files.Sidecars, src)
	inv.Totals.SidecarJavaScriptLines += lines
	inv.Totals.SidecarBytes += int64(len(body))
	collectTextEvidence(rel, string(body), inv)
	return nil
}

func collectIncludedFile(root, path string, inv *Inventory) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := countLines(body)
	src := SourceFile{
		Path:        rel,
		Language:    "javascript",
		SourceKind:  "scoreboard",
		Reason:      "historical first-party authored browser runtime source scoreboard",
		Lines:       lines,
		Bytes:       int64(len(body)),
		GzipBytes:   compressedSize(body, "gzip"),
		BrotliBytes: compressedSize(body, "brotli"),
	}
	if err := parseJavaScript(body); err != nil {
		src.ParseError = err.Error()
		inv.Structural.Gotreesitter.Failed++
		inv.Structural.Gotreesitter.Failures = append(inv.Structural.Gotreesitter.Failures, Location{Path: rel, Line: 0, Text: err.Error()})
	} else {
		src.ParseOK = true
		inv.Structural.Gotreesitter.Parsed++
	}
	inv.Structural.Gotreesitter.Language = "javascript"
	inv.Files.Included = append(inv.Files.Included, src)
	inv.Totals.IncludedJavaScriptLines += lines
	inv.Totals.IncludedBytes += int64(len(body))
	inv.Totals.IncludedGzipBytes += src.GzipBytes
	inv.Totals.IncludedBrotliBytes += src.BrotliBytes
	inv.Totals.ByExtension[".js"]++
	collectTextEvidence(rel, string(body), inv)
	return nil
}

func collectTextEvidence(rel, text string, inv *Inventory) {
	lines := splitLines(text)
	nameOwners := map[string]map[string]bool{}
	for _, existing := range inv.Surface.GosxNames {
		nameOwners[existing.Name] = map[string]bool{}
		for _, owner := range existing.Owners {
			nameOwners[existing.Name][owner] = true
		}
	}
	reads := map[string]bool{}
	writes := map[string]bool{}
	for _, r := range inv.Structural.FreeGlobalReads {
		reads[r] = true
	}
	for _, r := range inv.Structural.FreeGlobalWrites {
		writes[r] = true
	}
	for index, line := range lines {
		lineNo := index + 1
		if importExportRe.MatchString(line) {
			inv.Structural.ImportsExports = append(inv.Structural.ImportsExports, Location{Path: rel, Line: lineNo, Text: strings.TrimSpace(line)})
		}
		for _, match := range gosxNameRe.FindAllString(line, -1) {
			if nameOwners[match] == nil {
				nameOwners[match] = map[string]bool{}
			}
			nameOwners[match][rel] = true
		}
		for _, m := range jsonCandidateRe.FindAllStringSubmatch(line, -1) {
			kind := m[0]
			inv.Surface.SerializationSites = append(inv.Surface.SerializationSites, SerializationSite{
				Path: rel, Line: lineNo, Kind: kind, Phase: classifyPhase(rel, line), Text: strings.TrimSpace(line),
			})
		}
		for _, m := range globalReadRe.FindAllStringSubmatch(line, -1) {
			reads[m[2]] = true
		}
		for _, m := range globalWriteRe.FindAllStringSubmatch(line, -1) {
			writes[m[2]] = true
		}
	}
	inv.Surface.GosxNames = gosxRecordsFromOwners(nameOwners, sourceFamilyForPath)
	for name := range reads {
		inv.Structural.FreeGlobalReads = append(inv.Structural.FreeGlobalReads, name)
	}
	for name := range writes {
		inv.Structural.FreeGlobalWrites = append(inv.Structural.FreeGlobalWrites, name)
	}
	inv.Structural.FreeGlobalReads = uniqueStrings(inv.Structural.FreeGlobalReads)
	inv.Structural.FreeGlobalWrites = uniqueStrings(inv.Structural.FreeGlobalWrites)
	sort.Slice(inv.Surface.SerializationSites, func(i, j int) bool {
		if inv.Surface.SerializationSites[i].Path == inv.Surface.SerializationSites[j].Path {
			return inv.Surface.SerializationSites[i].Line < inv.Surface.SerializationSites[j].Line
		}
		return inv.Surface.SerializationSites[i].Path < inv.Surface.SerializationSites[j].Path
	})
	inv.Surface.GosxNameCount = len(inv.Surface.GosxNames)
	inv.Surface.SerializationSiteCount = len(inv.Surface.SerializationSites)
	inv.Surface.PublicFacadeCount = inv.Surface.GosxNameCount
}

// splitLines is the allocation-light equivalent of splitting on \r?\n. The
// inventory scanner runs this over every browser source file, so routing the
// operation through regexp.Split makes large overlays needlessly expensive.
// Keep a terminal carriage return when it is not part of a CRLF sequence to
// preserve the regexp's behavior exactly.
func splitLines(text string) []string {
	lines := make([]string, 0, strings.Count(text, "\n")+1)
	start := 0
	for index := 0; index < len(text); index++ {
		if text[index] != '\n' {
			continue
		}
		end := index
		if end > start && text[end-1] == '\r' {
			end--
		}
		lines = append(lines, text[start:end])
		start = index + 1
	}
	return append(lines, text[start:])
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	out := values[:0]
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func collectCompatibilitySurface(root string, inv *Inventory) error {
	wasmRoot := filepath.Join(root, "client", "wasm")
	if _, err := os.Stat(wasmRoot); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(wasmRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		collectTextEvidence(filepath.ToSlash(rel), string(body), inv)
		return nil
	})
}

func recomputeGosxSurfaceCounts(root string, inv *Inventory) error {
	jsFiles, err := sourceFiles(filepath.Join(root, "client", "js", "bootstrap-src"), ".js", false)
	if err != nil {
		return err
	}
	wasmAll, err := sourceFiles(filepath.Join(root, "client", "wasm"), ".go", true)
	if err != nil {
		return err
	}
	wasmProd := make([]string, 0, len(wasmAll))
	for _, path := range wasmAll {
		if !strings.HasSuffix(path, "_test.go") {
			wasmProd = append(wasmProd, path)
		}
	}
	allOwners := map[string]map[string]bool{}
	if err := collectGosxNamesInto(root, append(append([]string{}, jsFiles...), wasmAll...), allOwners); err != nil {
		return err
	}
	prodNames := map[string]bool{}
	if err := collectNameSet(append(append([]string{}, jsFiles...), wasmProd...), gosxNameRe, prodNames); err != nil {
		return err
	}
	jsNames := map[string]bool{}
	if err := collectNameSet(jsFiles, gosxNameRe, jsNames); err != nil {
		return err
	}
	rootAssigned := map[string]bool{}
	windowAssigned := map[string]bool{}
	goPublished := map[string]bool{}
	hostCallbacks := map[string]bool{}
	if err := collectNameSet(jsFiles, regexp.MustCompile(`(?m)^\s*window\.(__gosx(?:_[A-Za-z0-9_]+)?)\s*=`), rootAssigned); err != nil {
		return err
	}
	if err := collectNameSet(jsFiles, regexp.MustCompile(`(?m)^\s*window\.(__gosx_[A-Za-z0-9_]*)\s*=`), windowAssigned); err != nil {
		return err
	}
	if err := collectNameSet(wasmProd, regexp.MustCompile(`setRuntimeFunc\("(__gosx_[A-Za-z0-9_]*)"`), goPublished); err != nil {
		return err
	}
	if err := collectNameSet(wasmProd, regexp.MustCompile(`js\.Global\(\)\.(?:Get|Call)\("(__gosx_[A-Za-z0-9_]*)"`), hostCallbacks); err != nil {
		return err
	}
	inv.Surface.GosxNames = gosxRecordsFromOwners(allOwners, sourceFamilyForPath)
	inv.Surface.GosxNameCount = len(allOwners)
	inv.Surface.GosxProductionNameCount = len(prodNames)
	inv.Surface.GosxJavaScriptNameCount = len(jsNames)
	inv.Surface.AssignedBrowserRootCount = len(rootAssigned)
	inv.Surface.AssignedWindowCount = len(windowAssigned)
	inv.Surface.GoPublishedABICount = len(goPublished)
	inv.Surface.HostCallbackCount = len(hostCallbacks)
	inv.Surface.PublicFacadeCount = 0
	broaderFiles := inventorySourcePaths(root, inv.Files.Included, inv.Files.Sidecars, inv.Files.Embedded)
	broaderOwners := map[string]map[string]bool{}
	if err := collectGosxNamesInto(root, broaderFiles, broaderOwners); err != nil {
		return err
	}
	broaderSerialization, err := countSerializationSites(broaderFiles)
	if err != nil {
		return err
	}
	inv.Surface.BroaderBrowserGosxNames = gosxRecordsFromOwners(broaderOwners, sourceFamilyForPath)
	inv.Surface.BroaderBrowserNameCount = len(inv.Surface.BroaderBrowserGosxNames)
	inv.Surface.BroaderSerializationSiteCount = broaderSerialization
	return nil
}

func collectCompatibilityAudit(ctx context.Context, root string, inv *Inventory) error {
	receipt, err := loadCompatibilityReceipt()
	if err != nil {
		inv.Surface.CompatibilityAudit = failClosedCompatibilityAudit("load receipt: " + err.Error())
		return nil
	}
	receiptEvidence := compatibilityEvidenceFromNames(receipt.Names, CompatibilitySourceIdentity{
		Kind:         "pinned-receipt",
		ArtifactPath: "perf/ouroboros/compatibility_receipt.v1.json",
	})
	if receipt.Count != canonicalGosx || receipt.NameSetHash != compatibilityReceiptHash || receiptEvidence.NameSetHash != receipt.NameSetHash {
		inv.Surface.CompatibilityAudit = failClosedCompatibilityAudit("receipt count or hash mismatch")
		inv.Surface.CompatibilityAudit.Receipt = receiptEvidence
		return nil
	}
	if !validGitRevision(inv.BaseRevision) {
		inv.Surface.CompatibilityAudit = failClosedCompatibilityAudit("unsafe baseRevision for anchor archive")
		inv.Surface.CompatibilityAudit.Receipt = receiptEvidence
		return nil
	}
	anchor, err := scanFullRuntimeJSONNameSetAtRevision(ctx, root, inv.BaseRevision, CompatibilitySourceIdentity{Kind: "clean-anchor", Revision: inv.BaseRevision, OverlayHash: OverlayClean})
	if err != nil {
		inv.Surface.CompatibilityAudit = failClosedCompatibilityAudit("scan anchor: " + err.Error())
		inv.Surface.CompatibilityAudit.Receipt = receiptEvidence
		return nil
	}
	current, err := scanFullRuntimeJSONNameSet(ctx, root, *inv, CompatibilitySourceIdentity{Kind: "current-overlay", Revision: inv.BaseRevision, OverlayHash: inv.OverlayHash})
	if err != nil {
		inv.Surface.CompatibilityAudit = failClosedCompatibilityAudit("scan current: " + err.Error())
		inv.Surface.CompatibilityAudit.Receipt = receiptEvidence
		inv.Surface.CompatibilityAudit.Anchor = anchor
		return nil
	}
	reconciliation := CompatibilityReconciliation{
		RecoveredPreexisting: differenceStrings(anchor.Names, receiptEvidence.Names),
		AddedSinceAnchor:     differenceStrings(current.Names, anchor.Names),
		RemovedSinceAnchor:   differenceStrings(anchor.Names, current.Names),
		MissingFromAnchor:    differenceStrings(receiptEvidence.Names, anchor.Names),
	}
	available := len(reconciliation.AddedSinceAnchor) == 0 && len(reconciliation.RemovedSinceAnchor) == 0
	status := "pass"
	if !available {
		status = "fail-closed"
	}
	inv.Surface.CompatibilityAudit = CompatibilityAudit{
		SchemaVersion:      compatibilityAuditSchemaVersion,
		Status:             status,
		CanonicalAvailable: available,
		Receipt:            receiptEvidence,
		Anchor:             anchor,
		Current:            current,
		Reconciliation:     reconciliation,
	}
	if len(reconciliation.RecoveredPreexisting) > 0 {
		inv.Surface.CompatibilityAudit.Notes = append(inv.Surface.CompatibilityAudit.Notes, "full scanner anchor contains names absent from the pinned raw receipt")
	}
	if len(reconciliation.MissingFromAnchor) > 0 {
		inv.Surface.CompatibilityAudit.Notes = append(inv.Surface.CompatibilityAudit.Notes, "pinned raw receipt contains placeholder names absent from the full scanner anchor")
	}
	if !available {
		inv.Surface.CompatibilityAudit.Notes = append(inv.Surface.CompatibilityAudit.Notes, "canonical compatibility availability failed closed")
	}
	return nil
}

func failClosedCompatibilityAudit(note string) CompatibilityAudit {
	return CompatibilityAudit{
		SchemaVersion:      compatibilityAuditSchemaVersion,
		Status:             "fail-closed",
		CanonicalAvailable: false,
		Reconciliation: CompatibilityReconciliation{
			RecoveredPreexisting: []string{},
			AddedSinceAnchor:     []string{},
			RemovedSinceAnchor:   []string{},
			MissingFromAnchor:    []string{},
		},
		Notes: []string{note},
	}
}

func loadCompatibilityReceipt() (compatibilityReceiptArtifact, error) {
	var receipt compatibilityReceiptArtifact
	if err := json.Unmarshal(compatibilityReceiptBytes, &receipt); err != nil {
		return receipt, err
	}
	names := uniqueStrings(append([]string{}, receipt.Names...))
	if len(names) != len(receipt.Names) {
		return receipt, fmt.Errorf("receipt names are not unique")
	}
	if !sort.StringsAreSorted(receipt.Names) {
		return receipt, fmt.Errorf("receipt names are not sorted")
	}
	if receipt.SchemaVersion != "gosx.ouroboros.compatibility-receipt.v1" {
		return receipt, fmt.Errorf("schemaVersion = %q", receipt.SchemaVersion)
	}
	if receipt.Count != len(receipt.Names) {
		return receipt, fmt.Errorf("count = %d, names = %d", receipt.Count, len(receipt.Names))
	}
	if nameSetHash(receipt.Names) != receipt.NameSetHash {
		return receipt, fmt.Errorf("nameSetHash mismatch")
	}
	return receipt, nil
}

func scanCompatibilityNameSet(root string, identity CompatibilitySourceIdentity) (CompatibilityNameSetEvidence, error) {
	files, err := compatibilityAuditSourceFiles(root)
	if err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	return scanCompatibilityNameSetFromFiles(files, func(src SourceFile) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(src.Path)))
	}, identity)
}

func scanCompatibilityNameSetAtRevision(ctx context.Context, repoRoot, revision string, identity CompatibilitySourceIdentity) (CompatibilityNameSetEvidence, error) {
	if !validGitRevision(revision) {
		return CompatibilityNameSetEvidence{}, fmt.Errorf("unsafe revision %q", revision)
	}
	out, err := gitOutput(ctx, repoRoot, "git", "ls-tree", "-r", "--name-only", revision, "--", "client/js/bootstrap-src", "client/wasm")
	if err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	var files []SourceFile
	for _, rel := range strings.Split(out, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || !isSafeRepoRelPath(rel) {
			continue
		}
		switch {
		case strings.HasPrefix(rel, "client/js/bootstrap-src/") && strings.HasSuffix(rel, ".js"):
			files = append(files, SourceFile{Path: rel, Language: "javascript", SourceKind: "compatibility-bootstrap"})
		case strings.HasPrefix(rel, "client/wasm/") && strings.HasSuffix(rel, ".go"):
			files = append(files, SourceFile{Path: rel, Language: "go", SourceKind: "compatibility-wasm"})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return scanCompatibilityNameSetFromFiles(files, func(src SourceFile) ([]byte, error) {
		body, err := gitOutput(ctx, repoRoot, "git", "show", revision+":"+src.Path)
		if err != nil {
			return nil, err
		}
		return []byte(body), nil
	}, identity)
}

func scanCompatibilityNameSetFromFiles(files []SourceFile, read func(SourceFile) ([]byte, error), identity CompatibilitySourceIdentity) (CompatibilityNameSetEvidence, error) {
	names := map[string]bool{}
	var evidence []string
	for _, src := range files {
		body, err := read(src)
		if err != nil {
			return CompatibilityNameSetEvidence{}, err
		}
		for index, line := range splitLines(string(body)) {
			for _, name := range gosxNameRe.FindAllString(line, -1) {
				if ignoreGosxRawName(name) {
					continue
				}
				names[name] = true
				evidence = append(evidence, src.Path+"#"+strconv.Itoa(index+1)+":global-name:"+name)
			}
		}
	}
	sorted := sortedBoolKeys(names)
	return compatibilityEvidenceFromNamesWithEvidence(sorted, identity, evidence), nil
}

func scanFullRuntimeJSONNameSetAtRevision(ctx context.Context, repoRoot, revision string, identity CompatibilitySourceIdentity) (CompatibilityNameSetEvidence, error) {
	if !validGitRevision(revision) {
		return CompatibilityNameSetEvidence{}, fmt.Errorf("unsafe revision %q", revision)
	}
	archiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	anchorRoot, err := archiveFullRevisionToTempDir(archiveCtx, repoRoot, revision)
	if err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	defer os.RemoveAll(anchorRoot)
	anchorInv, err := Collect(ctx, CollectOptions{RepoRoot: anchorRoot, ArtifactRoot: filepath.Join(anchorRoot, "build", "ouroboros-anchor"), Canopy: false, Git: false})
	if err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	anchorInv.BaseRevision = revision
	anchorInv.OverlayHash = OverlayClean
	anchorInv.Overlay = OverlayEvidence{
		Status:       "clean",
		Hash:         OverlayClean,
		BaseRevision: revision,
		Recreate:     []string{"git checkout " + revision},
	}
	anchorManifestArtifacts(&anchorInv.Manifest, revision, OverlayClean)
	return scanFullRuntimeJSONNameSet(ctx, anchorRoot, *anchorInv, identity)
}

func scanFullRuntimeJSONNameSet(ctx context.Context, root string, inv Inventory, identity CompatibilitySourceIdentity) (CompatibilityNameSetEvidence, error) {
	artifactRoot, err := os.MkdirTemp("", "gosx-o02-runtime-json-static-*")
	if err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	defer os.RemoveAll(artifactRoot)
	invPath := filepath.Join(artifactRoot, "source-inventory.json")
	if err := WriteJSONFile(invPath, inv); err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	generatedAt := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, inv.GeneratedAt); err == nil {
		generatedAt = parsed
	}
	corpus, err := CollectRuntimeJSONStaticCorpus(ctx, RuntimeJSONProbeOptions{
		RepoRoot:      root,
		InventoryPath: invPath,
		ArtifactRoot:  artifactRoot,
		GeneratedAt:   generatedAt,
		Git:           false,
	})
	if err != nil {
		return CompatibilityNameSetEvidence{}, err
	}
	names := RuntimeJSONStaticGlobalNames(corpus)
	nameSet := map[string]bool{}
	for _, name := range names {
		nameSet[name] = true
	}
	evidence := make([]string, 0, len(corpus.Sites))
	for _, site := range corpus.Sites {
		if site.GlobalName == "" || !nameSet[site.GlobalName] {
			continue
		}
		evidence = append(evidence, site.Path+"#"+strconv.Itoa(site.Line)+":"+site.Operation+":"+site.GlobalName)
	}
	set := compatibilityEvidenceFromNamesWithEvidenceAndScope(names, identity, compatibilityFullRuntimeScope, evidence)
	set.RuntimeJSONSourceIdentityHash = RuntimeJSONStaticCanonicalSourceIdentityHash(corpus.CurrentSourceIdentity)
	set.RuntimeJSONSemanticHash = corpus.SemanticHash
	set.RuntimeJSONCountsHash = corpus.CountsHash
	set.RuntimeJSONGlobalNameHash = corpus.GlobalNames.Hash
	set.EvidenceHash = compatibilityEvidenceHash(set)
	return set, nil
}

func isSafeRepoRelPath(rel string) bool {
	return rel != "" && !filepath.IsAbs(rel) && !strings.Contains(rel, "\x00") && rel == filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))) && rel != ".." && !strings.HasPrefix(rel, "../")
}

func compatibilityAuditSourceFiles(root string) ([]SourceFile, error) {
	var out []SourceFile
	jsFiles, err := sourceFiles(filepath.Join(root, "client", "js", "bootstrap-src"), ".js", false)
	if err != nil {
		return nil, err
	}
	for _, path := range jsFiles {
		rel, _ := filepath.Rel(root, path)
		out = append(out, SourceFile{Path: filepath.ToSlash(rel), Language: "javascript", SourceKind: "compatibility-bootstrap"})
	}
	goFiles, err := sourceFiles(filepath.Join(root, "client", "wasm"), ".go", true)
	if err != nil {
		return nil, err
	}
	for _, path := range goFiles {
		rel, _ := filepath.Rel(root, path)
		out = append(out, SourceFile{Path: filepath.ToSlash(rel), Language: "go", SourceKind: "compatibility-wasm"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func compatibilityEvidenceFromNames(names []string, identity CompatibilitySourceIdentity) CompatibilityNameSetEvidence {
	return compatibilityReceiptEvidenceFromNames(names, identity)
}

func compatibilityReceiptEvidenceFromNames(names []string, identity CompatibilitySourceIdentity) CompatibilityNameSetEvidence {
	return compatibilityEvidenceFromNamesWithEvidenceAndMetadata(names, identity, compatibilityAuditScope, compatibilityReceiptMethod, compatibilityReceiptClassifier, nil)
}

func compatibilityEvidenceFromNamesWithEvidence(names []string, identity CompatibilitySourceIdentity, evidence []string) CompatibilityNameSetEvidence {
	return compatibilityEvidenceFromNamesWithEvidenceAndMetadata(names, identity, compatibilityAuditScope, compatibilityReceiptMethod, compatibilityReceiptClassifier, evidence)
}

func compatibilityEvidenceFromNamesWithEvidenceAndScope(names []string, identity CompatibilitySourceIdentity, scope string, evidence []string) CompatibilityNameSetEvidence {
	return compatibilityEvidenceFromNamesWithEvidenceAndMetadata(names, identity, scope, compatibilityFullMethod, compatibilityFullClassifier, evidence)
}

func compatibilityEvidenceFromNamesWithEvidenceAndMetadata(names []string, identity CompatibilitySourceIdentity, scope, method, classifier string, evidence []string) CompatibilityNameSetEvidence {
	names = uniqueStrings(append([]string{}, names...))
	sort.Strings(evidence)
	set := CompatibilityNameSetEvidence{
		SourceIdentity:    identity,
		Scope:             scope,
		MethodVersion:     method,
		ClassifierVersion: classifier,
		Names:             names,
		Count:             len(names),
		NameSetHash:       nameSetHash(names),
	}
	set.EvidenceHash = compatibilityEvidenceHash(set)
	return set
}

func nameSetHash(names []string) string {
	return sha256String("gosx.ouroboros.name-set.v1\n" + strings.Join(names, "\n"))
}

func compatibilityEvidenceHash(set CompatibilityNameSetEvidence) string {
	body, _ := json.Marshal(struct {
		SchemaVersion                 string                      `json:"schemaVersion"`
		SourceIdentity                CompatibilitySourceIdentity `json:"sourceIdentity"`
		Scope                         string                      `json:"scope"`
		MethodVersion                 string                      `json:"methodVersion"`
		ClassifierVersion             string                      `json:"classifierVersion"`
		Names                         []string                    `json:"names"`
		Count                         int                         `json:"count"`
		NameSetHash                   string                      `json:"nameSetHash"`
		RuntimeJSONSourceIdentityHash string                      `json:"runtimeJSONSourceIdentityHash,omitempty"`
		RuntimeJSONSemanticHash       string                      `json:"runtimeJSONSemanticHash,omitempty"`
		RuntimeJSONCountsHash         string                      `json:"runtimeJSONCountsHash,omitempty"`
		RuntimeJSONGlobalNameHash     string                      `json:"runtimeJSONGlobalNameHash,omitempty"`
	}{
		SchemaVersion:                 compatibilityAuditSchemaVersion,
		SourceIdentity:                set.SourceIdentity,
		Scope:                         set.Scope,
		MethodVersion:                 set.MethodVersion,
		ClassifierVersion:             set.ClassifierVersion,
		Names:                         set.Names,
		Count:                         set.Count,
		NameSetHash:                   set.NameSetHash,
		RuntimeJSONSourceIdentityHash: set.RuntimeJSONSourceIdentityHash,
		RuntimeJSONSemanticHash:       set.RuntimeJSONSemanticHash,
		RuntimeJSONCountsHash:         set.RuntimeJSONCountsHash,
		RuntimeJSONGlobalNameHash:     set.RuntimeJSONGlobalNameHash,
	})
	return sha256String(string(body))
}

func differenceStrings(left, right []string) []string {
	rightSet := map[string]bool{}
	for _, value := range right {
		rightSet[value] = true
	}
	out := []string{}
	for _, value := range left {
		if !rightSet[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func validGitRevision(revision string) bool {
	return gitRevisionRe.MatchString(revision)
}

func archiveRevisionToTempDir(ctx context.Context, repoRoot, revision string) (string, error) {
	if !validGitRevision(revision) {
		return "", fmt.Errorf("unsafe revision %q", revision)
	}
	pathspecs, err := archiveCompatibilityPathspecs(ctx, repoRoot, revision)
	if err != nil {
		return "", err
	}
	return archiveRevisionToTempDirWithPathspecs(ctx, repoRoot, revision, pathspecs)
}

func archiveFullRevisionToTempDir(ctx context.Context, repoRoot, revision string) (string, error) {
	if !validGitRevision(revision) {
		return "", fmt.Errorf("unsafe revision %q", revision)
	}
	pathspecs, err := archiveFullRuntimePathspecs(ctx, repoRoot, revision)
	if err != nil {
		return "", err
	}
	return archiveRevisionToTempDirWithPathspecs(ctx, repoRoot, revision, pathspecs)
}

func archiveFullRuntimePathspecs(ctx context.Context, repoRoot, revision string) ([]string, error) {
	list, err := gitOutput(ctx, repoRoot, "git", "ls-tree", "-r", "--name-only", revision)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, rel := range strings.Split(list, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || !isSafeRepoRelPath(rel) || !isFullRuntimeArchiveSource(rel) {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("revision %s has no full runtime audit paths", revision)
	}
	return out, nil
}

func isFullRuntimeArchiveSource(rel string) bool {
	if strings.HasPrefix(rel, ".canopy/") ||
		strings.HasPrefix(rel, ".tiller/") ||
		strings.HasPrefix(rel, ".worktrees/") ||
		strings.HasPrefix(rel, "build/") ||
		strings.HasPrefix(rel, "dist/") ||
		strings.HasPrefix(rel, "tmp/") ||
		strings.Contains(rel, "/node_modules/") ||
		strings.HasPrefix(rel, "node_modules/") {
		return false
	}
	if isGeneratedClientJSArtifact(rel) {
		return false
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".go", ".js", ".ts", ".tsx":
		return true
	default:
		return false
	}
}

func archiveFullRuntimeCandidatePathspecs() []string {
	return []string{
		":(glob)**/*.go",
		":(glob)**/*.js",
		":(glob)**/*.ts",
		":(glob)**/*.tsx",
		":(exclude).canopy/**",
		":(exclude).tiller/**",
		":(exclude).worktrees/**",
		":(exclude)build/**",
		":(exclude)dist/**",
		":(exclude)tmp/**",
		":(exclude)node_modules/**",
		":(glob,exclude)client/js/bootstrap*.js",
		":(glob,exclude)client/js/*wasm_exec*.js",
		":(glob,exclude)client/js/hls*.js",
		":(glob,exclude)client/js/*.js.map",
	}
}

func archiveRevisionToTempDirWithPathspecs(ctx context.Context, repoRoot, revision string, pathspecs []string) (string, error) {
	dir, err := os.MkdirTemp("", "gosx-o02-anchor-*")
	if err != nil {
		return "", err
	}
	archiveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := []string{"archive", "--format=tar", revision}
	if len(pathspecs) > 0 {
		args = append(append(args, "--"), pathspecs...)
	}
	cmd := exec.CommandContext(archiveCtx, "git", args...)
	cmd.Dir = repoRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	tr := tar.NewReader(stdout)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cancel()
			_ = stdout.Close()
			_ = cmd.Wait()
			_ = os.RemoveAll(dir)
			return "", err
		}
		if err := extractArchiveMember(dir, header, tr); err != nil {
			cancel()
			_ = stdout.Close()
			_ = cmd.Wait()
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	if err := cmd.Wait(); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func archiveCompatibilityPathspecs(ctx context.Context, repoRoot, revision string) ([]string, error) {
	var out []string
	for _, path := range []string{"client/js/bootstrap-src", "client/wasm"} {
		cmd := exec.CommandContext(ctx, "git", "cat-file", "-e", revision+":"+path)
		cmd.Dir = repoRoot
		if err := cmd.Run(); err == nil {
			out = append(out, path)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("revision %s has no compatibility audit paths", revision)
	}
	return out, nil
}

func extractArchiveMember(root string, header *tar.Header, r io.Reader) error {
	if header == nil {
		return nil
	}
	target, err := safeArchivePath(root, header.Name)
	if err != nil {
		return err
	}
	switch header.Typeflag {
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		return nil
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, r); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	default:
		return fmt.Errorf("unsupported archive member %s type %c", header.Name, header.Typeflag)
	}
}

func safeArchivePath(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\x00") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive path escapes root: %q", name)
	}
	return target, nil
}

func gosxRecordsFromOwners(owners map[string]map[string]bool, familyForPath func(string) string) []GosxName {
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)
	records := make([]GosxName, 0, len(names))
	for _, name := range names {
		ownerPaths := make([]string, 0, len(owners[name]))
		families := map[string]bool{}
		for owner := range owners[name] {
			ownerPaths = append(ownerPaths, owner)
			if family := familyForPath(owner); family != "" {
				families[family] = true
			}
		}
		sort.Strings(ownerPaths)
		records = append(records, GosxName{
			Name:               name,
			Owners:             ownerPaths,
			SourceFamilies:     sortedBoolKeys(families),
			CompatibilityClass: compatibilityClass(name),
		})
	}
	return records
}

func sourceFamilyForPath(path string) string {
	switch {
	case strings.HasPrefix(path, "client/js/bootstrap-src/"):
		return "bootstrap"
	case strings.HasPrefix(path, "client/js/"):
		return "sidecar"
	case strings.HasPrefix(path, "client/wasm/"):
		if strings.HasSuffix(path, "_test.go") {
			return "wasm-test"
		}
		return "wasm"
	default:
		return "embedded"
	}
}

func sortedBoolKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func countSerializationSites(paths []string) (int, error) {
	total := 0
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		total += len(jsonCandidateRe.FindAllStringSubmatch(string(body), -1))
	}
	return total, nil
}

func inventorySourcePaths(root string, lists ...[]SourceFile) []string {
	var out []string
	for _, list := range lists {
		for _, src := range list {
			out = append(out, filepath.Join(root, filepath.FromSlash(src.Path)))
		}
	}
	sort.Strings(out)
	return out
}

func sourceFiles(root, ext string, missingOK bool) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if missingOK && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		out = append(out, path)
		return nil
	})
	sort.Strings(out)
	return out, err
}

func collectGosxNamesInto(root string, paths []string, owners map[string]map[string]bool) error {
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, name := range gosxNameRe.FindAllString(string(body), -1) {
			if ignoreGosxRawName(name) {
				continue
			}
			if owners[name] == nil {
				owners[name] = map[string]bool{}
			}
			owners[name][rel] = true
		}
	}
	return nil
}

func collectNameSet(paths []string, re *regexp.Regexp, out map[string]bool) error {
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range re.FindAllStringSubmatch(string(body), -1) {
			if len(match) > 1 {
				if ignoreGosxRawName(match[1]) {
					continue
				}
				out[match[1]] = true
			} else if len(match) == 1 {
				if ignoreGosxRawName(match[0]) {
					continue
				}
				out[match[0]] = true
			}
		}
	}
	return nil
}

func ignoreGosxRawName(name string) bool {
	return name == "__gosx_bench_exports"
}

func classifyExcluded(rel string) (ExcludedFile, bool) {
	ext := strings.ToLower(filepath.Ext(rel))
	ex := ExcludedFile{Path: rel}
	if ext == ".map" || ext == ".gz" || ext == ".br" {
		ex.Kind = "generated"
		ex.Reason = "source map or compressed sidecar"
		return ex, true
	}
	if strings.Contains(rel, "wasm_exec.js") {
		ex.Kind = "generated"
		ex.Language = "javascript"
		ex.Reason = "Go or TinyGo wasm_exec.js distribution shim"
		return ex, true
	}
	if strings.HasSuffix(rel, ".test.js") || strings.HasSuffix(rel, ".test.mjs") || strings.Contains(rel, "test-harness") || strings.HasPrefix(rel, "scripts/") || rel == "perf/instrument.js" {
		if ext == ".js" || ext == ".mjs" {
			ex.Kind = "test"
			ex.Language = "javascript"
			ex.Reason = "benchmark, test, or probe script"
			if strings.Contains(rel, "probe") || strings.HasPrefix(rel, "scripts/") {
				ex.Kind = "probe"
			}
			return ex, true
		}
	}
	if strings.HasPrefix(rel, "client/runtime/generated/") && (ext == ".ts" || ext == ".tsx") {
		ex.Kind = "generated"
		ex.Language = "typescript"
		ex.Reason = "generated O0.1 ABI scaffold"
		return ex, true
	}
	if strings.HasPrefix(rel, "cmd/gosx/templates/") && isBrowserSourceExt(rel) {
		ex.Kind = "template"
		ex.Language = languageForPath(rel)
		ex.Reason = "scaffold template browser asset outside the GoSX runtime baseline"
		return ex, true
	}
	if strings.HasPrefix(rel, "editor/vscode/") && isBrowserSourceExt(rel) {
		ex.Kind = "tooling"
		ex.Language = languageForPath(rel)
		ex.Reason = "VS Code extension tooling source, not GoSX browser runtime"
		return ex, true
	}
	if strings.Contains(rel, "/testdata/") && isBrowserSourceExt(rel) {
		ex.Kind = "test"
		ex.Language = languageForPath(rel)
		ex.Reason = "testdata browser source fixture"
		return ex, true
	}
	if strings.HasPrefix(rel, "client/js/") && ext == ".js" && !isFirstPartySidecar(rel) {
		ex.Kind = "generated"
		ex.Language = "javascript"
		ex.Reason = "generated deployment JavaScript"
		if strings.Contains(strings.ToLower(rel), "hls") || strings.Contains(strings.ToLower(rel), "vendor") {
			ex.Kind = "vendor"
			ex.Reason = "third-party browser JavaScript"
		}
		return ex, true
	}
	if strings.HasPrefix(rel, "examples/") && strings.Contains(rel, "/public/") && ext == ".js" {
		ex.Kind = "example"
		ex.Language = "javascript"
		ex.Reason = "example application public JavaScript"
		return ex, true
	}
	return ex, false
}

func isIncludedRuntimeSource(rel string) bool {
	return strings.HasPrefix(rel, "client/js/bootstrap-src/") && strings.HasSuffix(rel, ".js")
}

func isCollectedSource(rel string, inv *Inventory) bool {
	for _, list := range [][]SourceFile{inv.Files.Included, inv.Files.Sidecars, inv.Files.Embedded} {
		for _, src := range list {
			if src.Path == rel {
				return true
			}
		}
	}
	return false
}

func isBrowserSourceExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".js" || ext == ".ts" || ext == ".tsx"
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	default:
		return "javascript"
	}
}

func isFirstPartySidecar(rel string) bool {
	if !strings.HasPrefix(rel, "client/js/") || strings.Count(rel, "/") != 2 || !strings.HasSuffix(rel, ".js") {
		return false
	}
	name := filepath.Base(rel)
	if strings.HasPrefix(name, "bootstrap") ||
		strings.Contains(name, "wasm_exec") ||
		strings.Contains(name, "hls") ||
		strings.Contains(name, ".test.") ||
		strings.Contains(name, "bench") ||
		strings.Contains(name, "test-harness") ||
		strings.HasPrefix(name, "runtime-") {
		return false
	}
	return true
}

func fillCanopy(ctx context.Context, root string, inv *Inventory) {
	if version, err := gitOutput(ctx, root, "canopy", "--version"); err == nil {
		inv.Structural.CanopyVersion = strings.TrimSpace(version)
	}
	stats, err := gitOutput(ctx, root, "canopy", "index", "stats")
	if err != nil {
		inv.Structural.CanopyRawStats = "unavailable: " + err.Error()
		return
	}
	inv.Structural.CanopyRawStats = strings.TrimSpace(stats)
	if match := canopySymbolsRe.FindStringSubmatch(stats); len(match) == 2 {
		inv.Structural.CanopySymbols, _ = strconv.Atoi(match[1])
	}
	if match := canopyJSFilesRe.FindStringSubmatch(stats); len(match) == 3 {
		inv.Structural.CanopyFiles, _ = strconv.Atoi(match[1])
	}
	inTop := false
	for _, line := range strings.Split(stats, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "top files") {
			inTop = true
			continue
		}
		if inTop && line != "" {
			inv.Structural.TopHotspots = append(inv.Structural.TopHotspots, line)
		}
	}
	inv.Structural.Complexity = "Canopy index stats recorded; run `canopy analyze complexity client/js/bootstrap-src` for detailed gates."
}

func fillDrift(inv *Inventory) {
	measured := Canonical{
		JavaScriptLines:          inv.Totals.IncludedJavaScriptLines,
		JavaScriptBytes:          inv.Totals.IncludedBytes,
		GosxNameCount:            inv.Surface.GosxNameCount,
		GosxProductionNameCount:  inv.Surface.GosxProductionNameCount,
		GosxJavaScriptNameCount:  inv.Surface.GosxJavaScriptNameCount,
		AssignedBrowserRootCount: inv.Surface.AssignedBrowserRootCount,
		AssignedWindowCount:      inv.Surface.AssignedWindowCount,
		GoPublishedABICount:      inv.Surface.GoPublishedABICount,
		HostCallbackCount:        inv.Surface.HostCallbackCount,
		SerializationSiteCount:   inv.Surface.SerializationSiteCount,
		SerializationStatus:      "raw-regex-only",
	}
	canonical := Canonical{
		JavaScriptLines:          canonicalLines,
		JavaScriptBytes:          3815610,
		GosxNameCount:            canonicalGosx,
		GosxProductionNameCount:  208,
		GosxJavaScriptNameCount:  178,
		AssignedBrowserRootCount: 121,
		AssignedWindowCount:      120,
		GoPublishedABICount:      64,
		HostCallbackCount:        6,
		SerializationSiteCount:   canonicalJSON,
		SerializationStatus:      "undefined",
	}
	deltas := Canonical{
		JavaScriptLines:          measured.JavaScriptLines - canonical.JavaScriptLines,
		JavaScriptBytes:          measured.JavaScriptBytes - canonical.JavaScriptBytes,
		GosxNameCount:            measured.GosxNameCount - canonical.GosxNameCount,
		GosxProductionNameCount:  measured.GosxProductionNameCount - canonical.GosxProductionNameCount,
		GosxJavaScriptNameCount:  measured.GosxJavaScriptNameCount - canonical.GosxJavaScriptNameCount,
		AssignedBrowserRootCount: measured.AssignedBrowserRootCount - canonical.AssignedBrowserRootCount,
		AssignedWindowCount:      measured.AssignedWindowCount - canonical.AssignedWindowCount,
		GoPublishedABICount:      measured.GoPublishedABICount - canonical.GoPublishedABICount,
		HostCallbackCount:        measured.HostCallbackCount - canonical.HostCallbackCount,
		SerializationSiteCount:   measured.SerializationSiteCount - canonical.SerializationSiteCount,
	}
	status := "pass"
	notes := []string{}
	if deltas.JavaScriptLines > 0 || deltas.JavaScriptBytes > 0 || deltas.GosxNameCount > 0 || deltas.GosxProductionNameCount > 0 || deltas.GosxJavaScriptNameCount > 0 || deltas.AssignedBrowserRootCount > 0 || deltas.AssignedWindowCount > 0 || deltas.GoPublishedABICount > 0 || deltas.HostCallbackCount > 0 || deltas.SerializationSiteCount != 0 {
		status = "fail-closed"
		notes = append(notes, "Current inventory exceeds a historical O0.2 ceiling or does not reproduce the fail-closed serialization denominator.")
	}
	notes = append(notes, "The immutable v1 receipt and corpus values are historical ceilings, not equality pins to the receipt revision; reductions pass and growth fails closed.")
	notes = append(notes, "The 209 compatibility ceiling is raw distinct __gosx_* over bootstrap-src plus all client/wasm, including tests.")
	notes = append(notes, "The production compatibility raw ceiling is 208 over bootstrap-src plus non-test client/wasm.")
	notes = append(notes, "The 253 serialization denominator lacks a reproducible exact query and JSONL corpus; this collector fails it closed.")
	inv.Drift = DriftReport{
		Status:         status,
		DriftExplained: false,
		Canonical:      canonical,
		Measured:       measured,
		Deltas:         deltas,
		Notes:          notes,
	}
	inv.Ratchets = []ScopeRatchet{
		{
			ID:         "js-source-lines",
			Scope:      "client/js/bootstrap-src/**/*.js",
			Target:     int64(canonical.JavaScriptLines),
			Measured:   int64(measured.JavaScriptLines),
			Delta:      int64(deltas.JavaScriptLines),
			Status:     passFail(deltas.JavaScriptLines <= 0),
			Definition: "Monotonic historical ceiling: wc -l over first-party authored browser runtime JavaScript source; reductions pass, growth fails closed.",
		},
		{
			ID:         "js-source-bytes",
			Scope:      "client/js/bootstrap-src/**/*.js",
			Target:     canonical.JavaScriptBytes,
			Measured:   measured.JavaScriptBytes,
			Delta:      deltas.JavaScriptBytes,
			Status:     passFail(deltas.JavaScriptBytes <= 0),
			Definition: "Monotonic historical ceiling: raw byte count over first-party authored browser runtime JavaScript source; reductions pass, growth fails closed.",
		},
		{
			ID:         "compat-gosx-raw-all",
			Scope:      "client/js/bootstrap-src/**/*.js + client/wasm/**/*.go including tests",
			Target:     int64(canonical.GosxNameCount),
			Measured:   int64(measured.GosxNameCount),
			Delta:      int64(deltas.GosxNameCount),
			Status:     passFail(deltas.GosxNameCount <= 0),
			Definition: "Monotonic historical ceiling: distinct raw __gosx_* tokens over browser runtime source and all WASM host bridge code.",
		},
		{
			ID:         "compat-gosx-production-raw",
			Scope:      "client/js/bootstrap-src/**/*.js + client/wasm/**/*.go excluding tests",
			Target:     int64(canonical.GosxProductionNameCount),
			Measured:   int64(measured.GosxProductionNameCount),
			Delta:      int64(deltas.GosxProductionNameCount),
			Status:     passFail(deltas.GosxProductionNameCount <= 0),
			Definition: "Monotonic historical ceiling: distinct raw __gosx_* tokens over production browser runtime source and production WASM host bridge code.",
		},
		{
			ID:         "compat-gosx-js-raw",
			Scope:      "client/js/bootstrap-src/**/*.js",
			Target:     int64(canonical.GosxJavaScriptNameCount),
			Measured:   int64(measured.GosxJavaScriptNameCount),
			Delta:      int64(deltas.GosxJavaScriptNameCount),
			Status:     passFail(deltas.GosxJavaScriptNameCount <= 0),
			Definition: "Monotonic historical ceiling: distinct raw __gosx_* tokens over authored browser runtime JavaScript.",
		},
		{
			ID:         "compat-assigned-browser-root",
			Scope:      "client/js/bootstrap-src/**/*.js",
			Target:     int64(canonical.AssignedBrowserRootCount),
			Measured:   int64(measured.AssignedBrowserRootCount),
			Delta:      int64(deltas.AssignedBrowserRootCount),
			Status:     passFail(deltas.AssignedBrowserRootCount <= 0),
			Definition: "Monotonic historical ceiling: distinct assigned window.__gosx_* or globalThis.__gosx_* browser-root names.",
		},
		{
			ID:         "compat-assigned-window",
			Scope:      "client/js/bootstrap-src/**/*.js",
			Target:     int64(canonical.AssignedWindowCount),
			Measured:   int64(measured.AssignedWindowCount),
			Delta:      int64(deltas.AssignedWindowCount),
			Status:     passFail(deltas.AssignedWindowCount <= 0),
			Definition: "Monotonic historical ceiling: distinct assigned window.__gosx_* names.",
		},
		{
			ID:         "compat-go-published-abi",
			Scope:      "client/wasm/**/*.go excluding tests",
			Target:     int64(canonical.GoPublishedABICount),
			Measured:   int64(measured.GoPublishedABICount),
			Delta:      int64(deltas.GoPublishedABICount),
			Status:     passFail(deltas.GoPublishedABICount <= 0),
			Definition: "Monotonic historical ceiling: distinct __gosx_* names published through setRuntimeFunc in production WASM host code.",
		},
		{
			ID:         "compat-host-callbacks",
			Scope:      "client/wasm/**/*.go excluding tests",
			Target:     int64(canonical.HostCallbackCount),
			Measured:   int64(measured.HostCallbackCount),
			Delta:      int64(deltas.HostCallbackCount),
			Status:     passFail(deltas.HostCallbackCount <= 0),
			Definition: "Monotonic historical ceiling: distinct __gosx_* host callback names read or called from production WASM host code.",
		},
		{
			ID:         "serialization-candidates",
			Scope:      "undefined",
			Target:     int64(canonical.SerializationSiteCount),
			Measured:   int64(measured.SerializationSiteCount),
			Delta:      int64(deltas.SerializationSiteCount),
			Status:     "fail-closed",
			Definition: "Canonical denominator is not reproducible until an exact query and JSONL source corpus are recorded.",
		},
	}
}

func passFail(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail-closed"
}

func OverlayHash(ctx context.Context, root string) (string, error) {
	evidence, err := BuildOverlayEvidence(ctx, root, "")
	if err != nil {
		return "", err
	}
	return evidence.Hash, nil
}

func BuildOverlayEvidence(ctx context.Context, root, baseRevision string) (OverlayEvidence, error) {
	status, err := gitOutput(ctx, root, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return OverlayEvidence{}, err
	}
	evidence := OverlayEvidence{
		Status:       "clean",
		Hash:         OverlayClean,
		BaseRevision: baseRevision,
		Recreate:     []string{"git checkout " + baseRevision},
	}
	if status == "" {
		return evidence, nil
	}
	evidence.Status = "dirty"
	h := sha256.New()
	io.WriteString(h, "gosx-ouroboros-overlay-v1\n")
	trackedDiff, err := gitOutput(ctx, root, "git", trackedOverlayDiffArgs()...)
	if err != nil {
		return OverlayEvidence{}, err
	}
	evidence.TrackedDiffHash = sha256String(trackedDiff)
	evidence.TrackedCachedDiffHash = "not-hashed"
	io.WriteString(h, trackedDiff)
	records := parsePorcelainStatus(status)
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	for _, rec := range records {
		code := rec.Code
		path := rec.Path
		if path == "" {
			continue
		}
		if reason := overlayExclusionReason(path); reason != "" {
			evidence.ExcludedPaths = append(evidence.ExcludedPaths, OverlayExcludedPath{Path: path, Reason: reason})
			continue
		}
		if !strings.Contains(code, "?") {
			continue
		}
		src, body, readErr := readUntrackedSource(root, path)
		if readErr != nil {
			return OverlayEvidence{}, readErr
		}
		evidence.UntrackedSources = append(evidence.UntrackedSources, src)
		io.WriteString(h, "\nuntracked:")
		io.WriteString(h, path)
		io.WriteString(h, " ")
		io.WriteString(h, src.Type)
		io.WriteString(h, " ")
		io.WriteString(h, src.Mode)
		io.WriteString(h, "\n")
		h.Write(body)
	}
	sort.Slice(evidence.UntrackedSources, func(i, j int) bool {
		return evidence.UntrackedSources[i].Path < evidence.UntrackedSources[j].Path
	})
	sort.Slice(evidence.ExcludedPaths, func(i, j int) bool {
		return evidence.ExcludedPaths[i].Path < evidence.ExcludedPaths[j].Path
	})
	evidence.Hash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	evidence.Recreate = []string{
		"git checkout " + baseRevision,
		"git apply --index --binary tracked-overlay.patch",
		"restore untracked source files listed in overlay.untracked.json by path and sha256",
	}
	return evidence, nil
}

func readUntrackedSource(root, path string) (UntrackedSourceHash, []byte, error) {
	full, err := safeJoin(root, path)
	if err != nil {
		return UntrackedSourceHash{}, nil, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return UntrackedSourceHash{}, nil, err
	}
	src := UntrackedSourceHash{
		Path: path,
		Mode: fmt.Sprintf("%04o", info.Mode().Perm()),
	}
	var body []byte
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(full)
		if err != nil {
			return UntrackedSourceHash{}, nil, err
		}
		if err := validateSafeSymlinkTarget(target); err != nil {
			return UntrackedSourceHash{}, nil, err
		}
		src.Type = "symlink"
		body = []byte(target)
	case info.Mode().IsRegular():
		body, err = os.ReadFile(full)
		if err != nil {
			return UntrackedSourceHash{}, nil, err
		}
		src.Type = "file"
	default:
		return UntrackedSourceHash{}, nil, fmt.Errorf("unsupported untracked source type %s for %s", info.Mode().String(), path)
	}
	sum := sha256.Sum256(body)
	src.Bytes = int64(len(body))
	src.SHA256 = hex.EncodeToString(sum[:])
	return src, body, nil
}

func safeJoin(root, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty relative path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path rejected: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(rootAbs, clean)
	relBack, err := filepath.Rel(rootAbs, full)
	if err != nil {
		return "", err
	}
	if relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) || filepath.IsAbs(relBack) {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	return full, nil
}

func validateSafeSymlinkTarget(target string) error {
	if target == "" {
		return fmt.Errorf("empty symlink target rejected")
	}
	if filepath.IsAbs(target) {
		return fmt.Errorf("absolute symlink target rejected: %s", target)
	}
	clean := filepath.Clean(filepath.FromSlash(target))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("escaping symlink target rejected: %s", target)
	}
	return nil
}

func overlayExclusionReason(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case strings.HasPrefix(path, ".git/"), strings.HasPrefix(path, ".canopy/"), strings.HasPrefix(path, ".worktrees/"):
		return "VCS, coordination, or structural-index state"
	case strings.HasPrefix(path, "tmp/"), strings.HasPrefix(path, "build/"), strings.HasPrefix(path, "dist/"):
		return "build or proof artifact noise"
	case path == "ouroboros-corpus":
		return "local build binary noise"
	case strings.HasPrefix(path, ".tiller/"):
		return "agent scratch and run ledger noise"
	case path == "perf/ouroboros/source-inventory.json":
		return "generated source-inventory artifact"
	case path == "perf/ouroboros/tracked-overlay.patch" || path == "perf/ouroboros/overlay.untracked.json" || strings.HasPrefix(path, "perf/ouroboros/untracked-sources/"):
		return "generated overlay reconstruction artifact"
	case isGeneratedClientJSArtifact(path):
		return "generated deployment JavaScript or compressed sidecar"
	case ext == ".map" || ext == ".gz" || ext == ".br":
		return "source map or compressed sidecar"
	default:
		return ""
	}
}

func sourceOverlayPathspecs() []string {
	return []string{
		".",
		":(exclude).canopy/**",
		":(exclude).tiller/**",
		":(exclude).worktrees/**",
		":(exclude)build/**",
		":(exclude)dist/**",
		":(exclude)tmp/**",
		":(glob,exclude)client/js/bootstrap*.js",
		":(glob,exclude)client/js/*wasm_exec*.js",
		":(glob,exclude)client/js/hls*.js",
		":(glob,exclude)client/js/*.js.map",
		":(glob,exclude)client/js/*.gz",
		":(glob,exclude)client/js/*.br",
		":(exclude)perf/ouroboros/source-inventory.json",
		":(exclude)perf/ouroboros/tracked-overlay.patch",
		":(exclude)perf/ouroboros/overlay.untracked.json",
		":(exclude)perf/ouroboros/untracked-sources/**",
	}
}

func trackedOverlayDiffArgs() []string {
	args := []string{"diff", "--full-index", "--binary", "--no-ext-diff", "HEAD", "--"}
	return append(args, sourceOverlayPathspecs()...)
}

func isGeneratedClientJSArtifact(path string) bool {
	if !strings.HasPrefix(path, "client/js/") || strings.Count(path, "/") != 2 {
		return false
	}
	name := filepath.Base(path)
	return strings.HasPrefix(name, "bootstrap") ||
		strings.Contains(name, "wasm_exec") ||
		strings.HasPrefix(name, "hls") ||
		strings.HasSuffix(name, ".map") ||
		strings.HasSuffix(name, ".gz") ||
		strings.HasSuffix(name, ".br")
}

type porcelainRecord struct {
	Code string
	Path string
}

func parsePorcelainStatus(status string) []porcelainRecord {
	parts := strings.Split(status, "\x00")
	records := []porcelainRecord{}
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		code := part[:2]
		path := part[3:]
		if code[0] == 'R' || code[0] == 'C' {
			if i+1 < len(parts) {
				i++
			}
		}
		records = append(records, porcelainRecord{Code: code, Path: path})
	}
	return records
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func gitOutput(ctx context.Context, dir string, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return string(out), nil
}

// parseBrowserSource parses a first-party browser source with the grammar its
// extension names. bootstrap-src is a mixed directory: sources that parse
// standalone are TypeScript, while the concatenation fragments stay JavaScript.
// Parsing a .ts file with the JavaScript grammar drops its symbols from the
// inventory, which shows up as unexplained anchor drift.
func parseBrowserSource(rel string, body []byte) error {
	switch languageForPath(rel) {
	case "typescript":
		return parseWithGrammar(grammars.TypescriptLanguage(), "TypeScript", body)
	case "tsx":
		return parseWithGrammar(grammars.TsxLanguage(), "TSX", body)
	default:
		return parseJavaScript(body)
	}
}

func parseWithGrammar(grammar *gotreesitter.Language, name string, body []byte) error {
	if grammar == nil {
		return fmt.Errorf("gotreesitter %s grammar is unavailable", name)
	}
	tree, err := gotreesitter.NewParser(grammar).Parse(body)
	if err != nil {
		return err
	}
	root := tree.RootNode()
	if root == nil {
		return errors.New("gotreesitter returned no syntax tree")
	}
	if root.IsError() || root.HasError() {
		return fmt.Errorf("syntax tree has error node %s", root.Type(grammar))
	}
	return nil
}

func parseJavaScript(body []byte) error {
	grammar := grammars.JavascriptLanguage()
	if grammar == nil {
		return errors.New("gotreesitter JavaScript grammar is unavailable")
	}
	tree, err := gotreesitter.NewParser(grammar).Parse(body)
	if err != nil {
		return err
	}
	root := tree.RootNode()
	if root == nil {
		return errors.New("gotreesitter returned no syntax tree")
	}
	if root.IsError() || root.HasError() {
		return fmt.Errorf("syntax tree has error node %s", root.Type(grammar))
	}
	return nil
}

func compressedSize(body []byte, kind string) int {
	var buf bytes.Buffer
	switch kind {
	case "gzip":
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(body)
		_ = w.Close()
	case "brotli":
		w := brotli.NewWriterLevel(&buf, brotli.BestCompression)
		_, _ = w.Write(body)
		_ = w.Close()
	}
	return buf.Len()
}

func countLines(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	return bytes.Count(body, []byte{'\n'})
}

func classifyPhase(path, line string) string {
	lower := strings.ToLower(path + " " + line)
	switch {
	case strings.Contains(lower, "frame") || strings.Contains(lower, "raf"):
		return "frame"
	case strings.Contains(lower, "patch") || strings.Contains(lower, "reconcile"):
		return "patch"
	case strings.Contains(lower, "input") || strings.Contains(lower, "event"):
		return "input"
	case strings.Contains(lower, "dispatch"):
		return "dispatch"
	case strings.Contains(lower, "telemetry") || strings.Contains(lower, "debug"):
		return "telemetry"
	case strings.Contains(lower, "load") || strings.Contains(lower, "manifest"):
		return "route load"
	default:
		return "unclassified"
	}
}

func compatibilityClass(name string) string {
	switch {
	case strings.Contains(name, "scene3d"):
		return "scene3d"
	case strings.Contains(name, "runtime"):
		return "runtime"
	case strings.Contains(name, "hydrate") || strings.Contains(name, "island"):
		return "islands"
	case strings.Contains(name, "hub") || strings.Contains(name, "signal"):
		return "collab"
	case strings.Contains(name, "canvas") || strings.Contains(name, "engine"):
		return "engine"
	default:
		return "legacy-global"
	}
}
