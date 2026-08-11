package ouroboros

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const RuntimeJSONProbeSchemaVersion = "gosx.ouroboros.runtime-json-probe.v1"

var (
	wireJSONNameRe = regexp.MustCompile(`^(propsJSON|patchJSON|valueJSON)$`)
	gosxExactRe    = regexp.MustCompile(`^__gosx_[A-Za-z0-9_]*$`)
)

const runtimeJSONPhaseClassifierVersion = "gosx.ouroboros.static-phase.v1"
const runtimeJSONStaticScannerVersion = "gosx.ouroboros.runtime-json-static.scanner.v3"

var (
	runtimeJSONAllPhases = []string{"cold-load", "route-load", "input", "dispatch", "reconciliation", "patch", "frame", "network", "debug", "telemetry"}
	runtimeJSONHotPhases = map[string]bool{"input": true, "dispatch": true, "reconciliation": true, "patch": true, "frame": true}
)

type RuntimeJSONProbeOptions struct {
	RepoRoot      string
	InventoryPath string
	ArtifactRoot  string
	GeneratedAt   time.Time
	Git           bool
	scanMode      runtimeJSONInventoryScanMode
}

type runtimeJSONInventoryScanMode int

const (
	runtimeJSONInventoryScanCanonical runtimeJSONInventoryScanMode = iota
	runtimeJSONInventoryScanCompatibilityCurrent
	// Anchor scans read a Git archive without .git metadata. They strict-decode only.
	runtimeJSONInventoryScanCompatibilityAnchorArchive
)

type RuntimeJSONStaticCorpus struct {
	SchemaVersion             string                         `json:"schemaVersion"`
	Contract                  string                         `json:"contractVersion"`
	GeneratedAt               string                         `json:"generatedAt"`
	ScannerVersion            string                         `json:"scannerVersion"`
	PhaseClassifierVersion    string                         `json:"phaseClassifierVersion"`
	CurrentSourceIdentity     SourceIdentity                 `json:"currentSourceIdentity"`
	CurrentSourceIdentityHash string                         `json:"currentSourceIdentityHash"`
	SemanticHash              string                         `json:"semanticHash"`
	CountsHash                string                         `json:"countsHash"`
	GlobalNames               RuntimeJSONStaticGlobalSet     `json:"globalNames"`
	Query                     RuntimeJSONStaticQuery         `json:"query"`
	Source                    SourceIdentity                 `json:"source"`
	Sites                     []RuntimeJSONStaticSite        `json:"sites"`
	Counts                    RuntimeJSONStaticCounts        `json:"counts"`
	Files                     []RuntimeJSONStaticSourceScope `json:"files"`
}

type RuntimeJSONStaticGlobalSet struct {
	Count int      `json:"count"`
	Names []string `json:"names"`
	Hash  string   `json:"hash"`
}

type RuntimeJSONStaticQuery struct {
	ID               string   `json:"id"`
	Version          string   `json:"version"`
	Engine           string   `json:"engine"`
	SourceScopes     []string `json:"sourceScopes"`
	Operations       []string `json:"operations"`
	BridgeOperations []string `json:"bridgeOperations"`
	PhaseClassifier  string   `json:"phaseClassifier"`
	OwnerHeuristic   string   `json:"ownerHeuristic"`
	TargetStatus     string   `json:"targetStatus"`
	FailClosed       bool     `json:"failClosed"`
}

type RuntimeJSONStaticSourceScope struct {
	Path       string `json:"path"`
	Family     string `json:"family"`
	SourceKind string `json:"sourceKind"`
	Lines      int    `json:"lines"`
	Bytes      int64  `json:"bytes"`
}

type RuntimeJSONStaticSite struct {
	Path             string   `json:"path"`
	Line             int      `json:"line"`
	Column           int      `json:"column"`
	SourceFamily     string   `json:"sourceFamily"`
	SourceKind       string   `json:"sourceKind"`
	Operation        string   `json:"operation"`
	GlobalName       string   `json:"globalName,omitempty"`
	Symbol           string   `json:"owningSymbol,omitempty"`
	Phase            string   `json:"phase,omitempty"`
	PhaseStatus      string   `json:"phaseStatus"`
	PossiblePhases   []string `json:"possiblePhases"`
	HotPathPossible  bool     `json:"hotPathPossible"`
	HotPathConfirmed bool     `json:"hotPathConfirmed"`
	PhaseRule        string   `json:"phaseRule"`
	PhaseReason      string   `json:"phaseReason"`
	PhaseEvidence    []string `json:"phaseEvidence,omitempty"`
	TextHash         string   `json:"textHash"`
	ContextHash      string   `json:"contextHash"`
}

type RuntimeJSONStaticCounts struct {
	SerializationSiteCount             int            `json:"serializationSiteCount"`
	JSONParseCount                     int            `json:"jsonParseCount"`
	JSONStringifyCount                 int            `json:"jsonStringifyCount"`
	PropsJSONCount                     int            `json:"propsJSONCount"`
	PatchJSONCount                     int            `json:"patchJSONCount"`
	ValueJSONCount                     int            `json:"valueJSONCount"`
	GosxReadCount                      int            `json:"gosxReadCount"`
	GosxWriteCount                     int            `json:"gosxWriteCount"`
	GosxCallCount                      int            `json:"gosxCallCount"`
	ByOperation                        map[string]int `json:"byOperation"`
	ByPhase                            map[string]int `json:"byPhase"`
	ByPossiblePhase                    map[string]int `json:"byPossiblePhase"`
	ByPhaseStatus                      map[string]int `json:"byPhaseStatus"`
	BySourceFamily                     map[string]int `json:"bySourceFamily"`
	HotPathPossibleCount               int            `json:"hotPathPossibleCount"`
	HotPathConfirmedCount              int            `json:"hotPathConfirmedCount"`
	SerializationHotPathPossibleCount  int            `json:"serializationHotPathPossibleCount"`
	SerializationHotPathConfirmedCount int            `json:"serializationHotPathConfirmedCount"`
	UnknownCount                       int            `json:"unknownCount"`
	AmbiguousCount                     int            `json:"ambiguousCount"`
	ExactCount                         int            `json:"exactCount"`
	TargetStatus                       string         `json:"targetStatus"`
	FailClosed                         bool           `json:"failClosed"`
	FailClosedReason                   string         `json:"failClosedReason,omitempty"`
	UniqueGosxGlobals                  int            `json:"uniqueGosxGlobals"`
}

type RuntimeJSONDynamicSourceClass string

const (
	RuntimeJSONDynamicSourceUnknown RuntimeJSONDynamicSourceClass = "unknown"
	RuntimeJSONDynamicSourceProduct RuntimeJSONDynamicSourceClass = "product"
	RuntimeJSONDynamicSourceHarness RuntimeJSONDynamicSourceClass = "harness"
)

type RuntimeJSONDynamicSource struct {
	URLHash string `json:"urlHash"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

func RuntimeJSONDynamicSourceFromDetail(detail map[string]any) RuntimeJSONDynamicSource {
	if detail == nil {
		return RuntimeJSONDynamicSource{}
	}
	source, ok := detail["source"].(map[string]any)
	if !ok {
		return RuntimeJSONDynamicSource{}
	}
	return RuntimeJSONDynamicSource{
		URLHash: stringFromRuntimeJSONDynamicAny(source["urlHash"]),
		Path:    stringFromRuntimeJSONDynamicAny(source["path"]),
		Line:    intFromRuntimeJSONDynamicAny(source["line"]),
		Column:  intFromRuntimeJSONDynamicAny(source["column"]),
	}
}

func ClassifyRuntimeJSONDynamicSource(source RuntimeJSONDynamicSource, productPathPrefixes, harnessPathPrefixes []string) RuntimeJSONDynamicSourceClass {
	path := strings.TrimSpace(source.Path)
	if path == "" {
		return RuntimeJSONDynamicSourceUnknown
	}
	product := runtimeJSONDynamicSourceMatchesPrefix(path, productPathPrefixes)
	harness := runtimeJSONDynamicSourceMatchesPrefix(path, harnessPathPrefixes)
	switch {
	case product && !harness:
		return RuntimeJSONDynamicSourceProduct
	case harness && !product:
		return RuntimeJSONDynamicSourceHarness
	default:
		return RuntimeJSONDynamicSourceUnknown
	}
}

func runtimeJSONDynamicSourceMatchesPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func stringFromRuntimeJSONDynamicAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func intFromRuntimeJSONDynamicAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	default:
		return 0
	}
}

func CollectRuntimeJSONStaticCorpus(ctx context.Context, opts RuntimeJSONProbeOptions) (*RuntimeJSONStaticCorpus, error) {
	opts = normalizeRuntimeJSONProbeOptions(opts)
	inv, source, err := loadRuntimeJSONInventory(ctx, opts)
	if err != nil {
		return nil, err
	}
	return collectRuntimeJSONStaticCorpusFromInventory(opts, inv, source)
}

func collectRuntimeJSONStaticCorpusFromInventory(opts RuntimeJSONProbeOptions, inv *Inventory, source SourceIdentity) (*RuntimeJSONStaticCorpus, error) {
	files := runtimeJSONSourceFiles(inv)
	query := RuntimeJSONStaticQuery{
		ID:               "gosx.ouroboros.o02.runtime-json-static.ast.v2",
		Version:          "2",
		Engine:           "gotreesitter-js-and-go-ast",
		SourceScopes:     []string{"inventory.files.included", "inventory.files.sidecars", "inventory.files.embedded", "client/wasm/**/*.go production owners"},
		Operations:       []string{"json-parse", "json-stringify", "props-json", "patch-json", "value-json"},
		BridgeOperations: []string{"gosx-read", "gosx-write", "gosx-call"},
		PhaseClassifier:  runtimeJSONPhaseClassifierVersion,
		OwnerHeuristic:   "nearest enclosing function or declaration name from syntax tree",
		TargetStatus:     "undefined/fail-closed",
		FailClosed:       true,
	}
	corpus := &RuntimeJSONStaticCorpus{
		SchemaVersion:             RuntimeJSONProbeSchemaVersion,
		Contract:                  ContractO02,
		GeneratedAt:               opts.GeneratedAt.UTC().Format(time.RFC3339),
		ScannerVersion:            runtimeJSONStaticScannerVersion,
		PhaseClassifierVersion:    runtimeJSONPhaseClassifierVersion,
		CurrentSourceIdentity:     source,
		CurrentSourceIdentityHash: RuntimeJSONStaticSourceIdentityHash(source),
		Query:                     query,
		Source:                    source,
		Counts: RuntimeJSONStaticCounts{
			ByOperation:     map[string]int{},
			ByPhase:         map[string]int{},
			ByPossiblePhase: map[string]int{},
			ByPhaseStatus:   map[string]int{},
			BySourceFamily:  map[string]int{},
			TargetStatus:    "undefined/fail-closed",
		},
	}
	gosxGlobals := map[string]bool{}
	for _, src := range files {
		corpus.Files = append(corpus.Files, RuntimeJSONStaticSourceScope{
			Path:       src.Path,
			Family:     runtimeJSONSourceFamily(src.Path, src.SourceKind),
			SourceKind: src.SourceKind,
			Lines:      src.Lines,
			Bytes:      src.Bytes,
		})
		path := filepath.Join(opts.RepoRoot, filepath.FromSlash(src.Path))
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sites, err := runtimeJSONSitesForFile(src, body, gosxGlobals)
		if err != nil {
			return nil, err
		}
		corpus.Sites = append(corpus.Sites, sites...)
	}
	sort.Slice(corpus.Sites, func(i, j int) bool {
		a, b := corpus.Sites[i], corpus.Sites[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Operation < b.Operation
	})
	sort.Slice(corpus.Files, func(i, j int) bool { return corpus.Files[i].Path < corpus.Files[j].Path })
	for _, site := range corpus.Sites {
		corpus.Counts.ByOperation[site.Operation]++
		if site.Phase != "" {
			corpus.Counts.ByPhase[site.Phase]++
		}
		for _, phase := range site.PossiblePhases {
			corpus.Counts.ByPossiblePhase[phase]++
		}
		corpus.Counts.ByPhaseStatus[site.PhaseStatus]++
		corpus.Counts.BySourceFamily[site.SourceFamily]++
		if site.HotPathPossible {
			corpus.Counts.HotPathPossibleCount++
		}
		if site.HotPathConfirmed {
			corpus.Counts.HotPathConfirmedCount++
		}
		switch site.PhaseStatus {
		case "unknown":
			corpus.Counts.UnknownCount++
		case "ambiguous":
			corpus.Counts.AmbiguousCount++
		case "exact":
			corpus.Counts.ExactCount++
		}
		switch site.Operation {
		case "json-parse":
			corpus.Counts.JSONParseCount++
			corpus.Counts.SerializationSiteCount++
			if site.HotPathPossible {
				corpus.Counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				corpus.Counts.SerializationHotPathConfirmedCount++
			}
		case "json-stringify":
			corpus.Counts.JSONStringifyCount++
			corpus.Counts.SerializationSiteCount++
			if site.HotPathPossible {
				corpus.Counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				corpus.Counts.SerializationHotPathConfirmedCount++
			}
		case "props-json":
			corpus.Counts.PropsJSONCount++
			corpus.Counts.SerializationSiteCount++
			if site.HotPathPossible {
				corpus.Counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				corpus.Counts.SerializationHotPathConfirmedCount++
			}
		case "patch-json":
			corpus.Counts.PatchJSONCount++
			corpus.Counts.SerializationSiteCount++
			if site.HotPathPossible {
				corpus.Counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				corpus.Counts.SerializationHotPathConfirmedCount++
			}
		case "value-json":
			corpus.Counts.ValueJSONCount++
			corpus.Counts.SerializationSiteCount++
			if site.HotPathPossible {
				corpus.Counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				corpus.Counts.SerializationHotPathConfirmedCount++
			}
		case "gosx-read":
			corpus.Counts.GosxReadCount++
		case "gosx-write":
			corpus.Counts.GosxWriteCount++
		case "gosx-call":
			corpus.Counts.GosxCallCount++
		}
	}
	corpus.Counts.UniqueGosxGlobals = len(gosxGlobals)
	corpus.Counts.FailClosed = true
	corpus.Counts.FailClosedReason = "historical 253 query corpus is absent; targetStatus is undefined/fail-closed"
	corpus.GlobalNames = RuntimeJSONStaticGlobalSetFromMap(gosxGlobals)
	corpus.CountsHash = RuntimeJSONStaticCountsHash(corpus.Counts)
	corpus.SemanticHash = RuntimeJSONStaticCorpusSemanticHash(corpus)
	if err := ValidateRuntimeJSONStaticCorpus(corpus); err != nil {
		return nil, err
	}
	return corpus, nil
}

func WriteRuntimeJSONStaticCorpusJSONL(path string, corpus *RuntimeJSONStaticCorpus) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	header := map[string]any{
		"schemaVersion":          corpus.SchemaVersion,
		"contract":               corpus.Contract,
		"generatedAt":            corpus.GeneratedAt,
		"scannerVersion":         corpus.ScannerVersion,
		"phaseClassifierVersion": corpus.PhaseClassifierVersion,
		"semanticHash":           corpus.SemanticHash,
		"countsHash":             corpus.CountsHash,
		"globalNames":            corpus.GlobalNames,
		"query":                  corpus.Query,
		"counts":                 corpus.Counts,
	}
	if err := writeRuntimeJSONL(w, "header", header); err != nil {
		return err
	}
	for _, file := range corpus.Files {
		if err := writeRuntimeJSONL(w, "source-file", file); err != nil {
			return err
		}
	}
	for _, site := range corpus.Sites {
		if err := writeRuntimeJSONL(w, "site", site); err != nil {
			return err
		}
	}
	return nil
}

func ReadRuntimeJSONStaticCorpusJSONLStrict(r io.Reader, expectedSource SourceIdentity) (*RuntimeJSONStaticCorpus, error) {
	corpus := &RuntimeJSONStaticCorpus{
		Source:                    expectedSource,
		CurrentSourceIdentity:     expectedSource,
		CurrentSourceIdentityHash: RuntimeJSONStaticCanonicalSourceIdentityHash(expectedSource),
	}
	reader := bufio.NewReader(r)
	seenHeader := false
	rowCount := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			rowCount++
			if len(bytes.TrimSpace(line)) == 0 {
				return nil, fmt.Errorf("runtime JSON static JSONL row %d is empty", rowCount)
			}
			var row struct {
				Kind  string          `json:"kind"`
				Value json.RawMessage `json:"value"`
			}
			dec := json.NewDecoder(bytes.NewReader(line))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&row); err != nil {
				return nil, fmt.Errorf("runtime JSON static JSONL row %d decode: %w", rowCount, err)
			}
			if err := rejectTrailingJSON(dec); err != nil {
				return nil, fmt.Errorf("runtime JSON static JSONL row %d decode: %w", rowCount, err)
			}
			if len(row.Value) == 0 {
				return nil, fmt.Errorf("runtime JSON static JSONL row %d missing value", rowCount)
			}
			switch row.Kind {
			case "header":
				if rowCount != 1 || seenHeader {
					return nil, fmt.Errorf("runtime JSON static JSONL header must be first and unique")
				}
				seenHeader = true
				var header struct {
					SchemaVersion          string                     `json:"schemaVersion"`
					Contract               string                     `json:"contract"`
					GeneratedAt            string                     `json:"generatedAt"`
					ScannerVersion         string                     `json:"scannerVersion"`
					PhaseClassifierVersion string                     `json:"phaseClassifierVersion"`
					SemanticHash           string                     `json:"semanticHash"`
					CountsHash             string                     `json:"countsHash"`
					GlobalNames            RuntimeJSONStaticGlobalSet `json:"globalNames"`
					Query                  RuntimeJSONStaticQuery     `json:"query"`
					Counts                 RuntimeJSONStaticCounts    `json:"counts"`
				}
				if err := decodeRuntimeJSONStaticRowValue(row.Value, &header); err != nil {
					return nil, fmt.Errorf("runtime JSON static JSONL header decode: %w", err)
				}
				corpus.SchemaVersion = header.SchemaVersion
				corpus.Contract = header.Contract
				corpus.GeneratedAt = header.GeneratedAt
				corpus.ScannerVersion = header.ScannerVersion
				corpus.PhaseClassifierVersion = header.PhaseClassifierVersion
				corpus.SemanticHash = header.SemanticHash
				corpus.CountsHash = header.CountsHash
				corpus.GlobalNames = header.GlobalNames
				corpus.Query = header.Query
				corpus.Counts = header.Counts
			case "source-file":
				if !seenHeader {
					return nil, fmt.Errorf("runtime JSON static JSONL source-file before header")
				}
				var file RuntimeJSONStaticSourceScope
				if err := decodeRuntimeJSONStaticRowValue(row.Value, &file); err != nil {
					return nil, fmt.Errorf("runtime JSON static JSONL source-file decode: %w", err)
				}
				corpus.Files = append(corpus.Files, file)
			case "site":
				if !seenHeader {
					return nil, fmt.Errorf("runtime JSON static JSONL site before header")
				}
				var site RuntimeJSONStaticSite
				if err := decodeRuntimeJSONStaticRowValue(row.Value, &site); err != nil {
					return nil, fmt.Errorf("runtime JSON static JSONL site decode: %w", err)
				}
				corpus.Sites = append(corpus.Sites, site)
			default:
				return nil, fmt.Errorf("runtime JSON static JSONL row %d has unknown kind %q", rowCount, row.Kind)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("runtime JSON static JSONL read: %w", err)
		}
	}
	if rowCount == 0 {
		return nil, fmt.Errorf("runtime JSON static JSONL is empty")
	}
	if !seenHeader {
		return nil, fmt.Errorf("runtime JSON static JSONL missing header")
	}
	if err := ValidateRuntimeJSONStaticCorpus(corpus); err != nil {
		return nil, err
	}
	return corpus, nil
}

func decodeRuntimeJSONStaticRowValue(value json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	return rejectTrailingJSON(dec)
}

func RuntimeJSONProbeScript(gosxNames []string) (string, error) {
	names := uniqueStrings(append([]string{}, gosxNames...))
	body, err := json.Marshal(names)
	if err != nil {
		return "", err
	}
	return strings.Replace(runtimeJSONProbeJS, "__GOSX_KNOWN_NAMES__", string(body), 1), nil
}

func RuntimeJSONKnownProductionNames(inv *Inventory) []string {
	names := make([]string, 0, len(inv.Surface.GosxNames))
	for _, name := range inv.Surface.GosxNames {
		for _, owner := range name.Owners {
			if strings.HasPrefix(owner, "client/wasm/") && strings.HasSuffix(owner, ".go") && !strings.HasSuffix(owner, "_test.go") {
				names = append(names, name.Name)
				break
			}
		}
	}
	return uniqueStrings(names)
}

func CollectRuntimeJSONStaticGlobalSet(ctx context.Context, opts RuntimeJSONProbeOptions) (RuntimeJSONStaticGlobalSet, error) {
	corpus, err := CollectRuntimeJSONStaticCorpus(ctx, opts)
	if err != nil {
		return RuntimeJSONStaticGlobalSet{}, err
	}
	return corpus.GlobalNames, nil
}

func RuntimeJSONStaticGlobalNames(corpus *RuntimeJSONStaticCorpus) []string {
	if corpus == nil {
		return nil
	}
	return RuntimeJSONStaticGlobalSetForSites(corpus.Sites).Names
}

func RuntimeJSONStaticGlobalNameHash(names []string) string {
	return hashJSON(uniqueStrings(names))
}

func RuntimeJSONStaticGlobalSetForSites(sites []RuntimeJSONStaticSite) RuntimeJSONStaticGlobalSet {
	globals := map[string]bool{}
	for _, site := range sites {
		if site.GlobalName != "" {
			globals[site.GlobalName] = true
		}
	}
	return RuntimeJSONStaticGlobalSetFromMap(globals)
}

func RuntimeJSONStaticGlobalSetFromMap(globals map[string]bool) RuntimeJSONStaticGlobalSet {
	names := make([]string, 0, len(globals))
	for name := range globals {
		names = append(names, name)
	}
	names = uniqueStrings(names)
	return RuntimeJSONStaticGlobalSet{
		Count: len(names),
		Names: names,
		Hash:  RuntimeJSONStaticGlobalNameHash(names),
	}
}

func RuntimeJSONStaticCountsHash(counts RuntimeJSONStaticCounts) string {
	return hashJSON(counts)
}

func RuntimeJSONStaticCanonicalSourceIdentityHash(source SourceIdentity) string {
	payload := struct {
		BaseRevision                string `json:"baseRevision"`
		OverlayHash                 string `json:"overlayHash"`
		TrackedDiffHash             string `json:"trackedDiffHash"`
		UntrackedIncludedSourceHash string `json:"untrackedIncludedSourceHash"`
	}{
		BaseRevision:                source.BaseRevision,
		OverlayHash:                 source.OverlayHash,
		TrackedDiffHash:             source.TrackedDiffHash,
		UntrackedIncludedSourceHash: source.UntrackedIncludedSourceHash,
	}
	return hashJSON(payload)
}

func RuntimeJSONStaticSourceIdentityHash(source SourceIdentity) string {
	return RuntimeJSONStaticCanonicalSourceIdentityHash(source)
}

func RuntimeJSONStaticCorpusSemanticHash(corpus *RuntimeJSONStaticCorpus) string {
	if corpus == nil {
		return ""
	}
	payload := struct {
		SchemaVersion          string                         `json:"schemaVersion"`
		Contract               string                         `json:"contractVersion"`
		ScannerVersion         string                         `json:"scannerVersion"`
		PhaseClassifierVersion string                         `json:"phaseClassifierVersion"`
		Query                  RuntimeJSONStaticQuery         `json:"query"`
		Files                  []RuntimeJSONStaticSourceScope `json:"files"`
		Sites                  []RuntimeJSONStaticSite        `json:"sites"`
		GlobalNames            RuntimeJSONStaticGlobalSet     `json:"globalNames"`
	}{
		SchemaVersion:          corpus.SchemaVersion,
		Contract:               corpus.Contract,
		ScannerVersion:         corpus.ScannerVersion,
		PhaseClassifierVersion: corpus.PhaseClassifierVersion,
		Query:                  corpus.Query,
		Files:                  corpus.Files,
		Sites:                  corpus.Sites,
		GlobalNames:            corpus.GlobalNames,
	}
	return hashJSON(payload)
}

func RuntimeJSONDrainExpression(clear bool) string {
	if clear {
		return `window.__gosxOuroborosProbe && window.__gosxOuroborosProbe.drain ? window.__gosxOuroborosProbe.drain() : null`
	}
	return `window.__gosxOuroborosProbe && window.__gosxOuroborosProbe.snapshot ? window.__gosxOuroborosProbe.snapshot() : null`
}

func RuntimeJSONProbeCoverage(events []ProbeEvent, requiredRoutes []string, requiredPhases []string) []string {
	seenKinds := map[string]bool{}
	seenPhases := map[string]bool{}
	seenRoutes := map[string]bool{}
	installed := false
	wrapped := false
	for _, event := range events {
		seenKinds[event.Kind] = true
		seenPhases[event.Phase] = true
		if route, ok := event.Detail["routeID"].(string); ok && route != "" {
			seenRoutes[route] = true
		}
		if event.Kind == "probe" && event.Name == "install" {
			installed = true
		}
		if event.Kind == "probe" && event.Name == "wrap" {
			wrapped = true
		}
	}
	var missing []string
	for _, kind := range []string{"runtime-call", "json-call"} {
		if !seenKinds[kind] && !(installed && wrapped) {
			missing = append(missing, "kind:"+kind)
		}
	}
	for _, phase := range requiredPhases {
		if !seenPhases[phase] && !(installed && wrapped) {
			missing = append(missing, "phase:"+phase)
		}
	}
	for _, route := range requiredRoutes {
		route = strings.TrimSpace(route)
		if route == "" {
			missing = append(missing, "route:empty")
			continue
		}
		if !seenRoutes[route] && !(installed && wrapped) {
			missing = append(missing, "route:"+route)
		}
	}
	sort.Strings(missing)
	return missing
}

func normalizeRuntimeJSONProbeOptions(opts RuntimeJSONProbeOptions) RuntimeJSONProbeOptions {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	if opts.ArtifactRoot == "" {
		opts.ArtifactRoot = filepath.Join(opts.RepoRoot, "build", "ouroboros", "o0.2", "runtime-calls")
	}
	if opts.GeneratedAt.IsZero() {
		opts.GeneratedAt = time.Now().UTC()
	}
	return opts
}

func loadRuntimeJSONInventory(ctx context.Context, opts RuntimeJSONProbeOptions) (*Inventory, SourceIdentity, error) {
	invRef := opts.InventoryPath
	var inv *Inventory
	var reconstruction ReconstructionEvidence
	canonical := opts.scanMode == runtimeJSONInventoryScanCanonical
	proofRequired := opts.scanMode != runtimeJSONInventoryScanCompatibilityAnchorArchive
	if invRef != "" {
		f, err := os.Open(invRef)
		if err != nil {
			return nil, SourceIdentity{}, err
		}
		defer f.Close()
		if canonical {
			inv, err = DecodeInventoryStrict(f)
		} else {
			inv, err = decodeRuntimeJSONInventoryShapeStrict(f)
		}
		if err != nil {
			return nil, SourceIdentity{}, err
		}
		if proofRequired {
			if err := verifyRuntimeJSONInventoryOverlayFresh(ctx, opts.RepoRoot, inv); err != nil {
				return nil, SourceIdentity{}, err
			}
			reconstruction, err = ReplayInventoryReconstruction(ctx, opts.RepoRoot, inv)
			if err != nil {
				return nil, SourceIdentity{}, fmt.Errorf("runtime JSON source inventory replay failed: %w", err)
			}
			if canonical {
				if err := verifyRuntimeJSONInventoryFreshEquality(ctx, opts, inv); err != nil {
					return nil, SourceIdentity{}, err
				}
			}
		}
	} else {
		collected, err := Collect(ctx, CollectOptions{RepoRoot: opts.RepoRoot, ArtifactRoot: opts.ArtifactRoot, GeneratedAt: opts.GeneratedAt, Git: opts.Git, Canopy: false})
		if err != nil {
			return nil, SourceIdentity{}, err
		}
		if err := verifyRuntimeJSONInventoryOverlayFresh(ctx, opts.RepoRoot, collected); err != nil {
			return nil, SourceIdentity{}, err
		}
		reconstruction, err = ReplayInventoryReconstruction(ctx, opts.RepoRoot, collected)
		if err != nil {
			return nil, SourceIdentity{}, fmt.Errorf("runtime JSON source inventory replay failed: %w", err)
		}
		invRef = filepath.Join(opts.ArtifactRoot, "source-inventory.json")
		if err := WriteJSONFile(invRef, collected); err != nil {
			return nil, SourceIdentity{}, err
		}
		f, err := os.Open(invRef)
		if err != nil {
			return nil, SourceIdentity{}, err
		}
		inv, err = decodeRuntimeJSONInventoryShapeStrict(f)
		_ = f.Close()
		if err != nil {
			return nil, SourceIdentity{}, err
		}
	}
	if proofRequired {
		if !reconstruction.Verified {
			return nil, SourceIdentity{}, fmt.Errorf("runtime JSON source inventory replay did not verify")
		}
	}
	hash, err := fileSHA256(invRef)
	if err != nil {
		return nil, SourceIdentity{}, err
	}
	source := SourceIdentity{
		BaseRevision:                inv.BaseRevision,
		OverlayHash:                 inv.OverlayHash,
		TrackedDiffHash:             inv.Overlay.TrackedDiffHash,
		UntrackedIncludedSourceHash: hashUntracked(inv.Overlay.UntrackedSources),
		InventoryRef:                relTo(opts.ArtifactRoot, invRef),
		InventorySHA256:             hash,
		RejectsModuleCacheMismatch:  true,
		StrictInventory:             true,
	}
	if proofRequired {
		source.CurrentOverlayVerified = true
		source.ReconstructionProof = true
		source.Reconstruction = &reconstruction
	}
	if proofRequired && (!source.CurrentOverlayVerified || !source.StrictInventory || !source.ReconstructionProof) {
		return nil, SourceIdentity{}, fmt.Errorf("runtime JSON source identity proof is incomplete")
	}
	return inv, source, nil
}

func decodeRuntimeJSONInventoryShapeStrict(r io.Reader) (*Inventory, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var inv Inventory
	if err := dec.Decode(&inv); err != nil {
		return nil, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("trailing JSON content")
	}
	return &inv, nil
}

func verifyRuntimeJSONInventoryOverlayFresh(ctx context.Context, repoRoot string, inv *Inventory) error {
	if inv == nil {
		return fmt.Errorf("runtime JSON source inventory is nil")
	}
	current, err := BuildOverlayEvidence(ctx, repoRoot, inv.BaseRevision)
	if err != nil {
		return fmt.Errorf("runtime JSON source overlay freshness: %w", err)
	}
	if current.Hash != inv.OverlayHash {
		return fmt.Errorf("runtime JSON source inventory stale overlay: artifact=%s current=%s", inv.OverlayHash, current.Hash)
	}
	return nil
}

func verifyRuntimeJSONInventoryFreshEquality(ctx context.Context, opts RuntimeJSONProbeOptions, inv *Inventory) error {
	fresh, err := Collect(ctx, CollectOptions{RepoRoot: opts.RepoRoot, Git: opts.Git, Canopy: false})
	if err != nil {
		return fmt.Errorf("runtime JSON source fresh-inventory collect: %w", err)
	}
	got := runtimeJSONInventorySemanticHash(inv)
	want := runtimeJSONInventorySemanticHash(fresh)
	if got != want {
		return fmt.Errorf("runtime JSON source fresh-inventory mismatch: supplied=%s fresh=%s", got, want)
	}
	return nil
}

func runtimeJSONInventorySemanticHash(inv *Inventory) string {
	return hashJSON(runtimeJSONInventorySemanticProjection(inv))
}

func runtimeJSONInventorySemanticProjection(inv *Inventory) any {
	if inv == nil {
		return nil
	}
	return struct {
		SchemaVersion string            `json:"schemaVersion"`
		Contract      string            `json:"contractVersion"`
		Initiative    string            `json:"initiative"`
		Spec          string            `json:"spec"`
		CorpusID      string            `json:"corpusID"`
		BaseRevision  string            `json:"baseRevision"`
		OverlayHash   string            `json:"overlayHash"`
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
	}{
		SchemaVersion: inv.SchemaVersion,
		Contract:      inv.Contract,
		Initiative:    inv.Initiative,
		Spec:          inv.Spec,
		CorpusID:      inv.CorpusID,
		BaseRevision:  inv.BaseRevision,
		OverlayHash:   inv.OverlayHash,
		Scope:         inv.Scope,
		Overlay:       normalizeRuntimeJSONInventorySemanticOverlay(inv.Overlay),
		Files:         inv.Files,
		Totals:        inv.Totals,
		Structural:    inv.Structural,
		Surface:       inv.Surface,
		Drift:         inv.Drift,
		Ratchets:      inv.Ratchets,
		Pixels:        inv.Pixels,
		Manifest:      normalizeRuntimeJSONInventorySemanticManifest(inv.Manifest),
	}
}

func normalizeRuntimeJSONInventorySemanticOverlay(overlay OverlayEvidence) OverlayEvidence {
	overlay.PatchPath = ""
	overlay.ArchivePath = ""
	return overlay
}

func normalizeRuntimeJSONInventorySemanticManifest(manifest CorpusManifest) CorpusManifest {
	manifest.GeneratedAt = ""
	manifest.ArtifactRoot = ""
	return manifest
}

func runtimeJSONSourceFiles(inv *Inventory) []SourceFile {
	var out []SourceFile
	out = append(out, inv.Files.Included...)
	out = append(out, inv.Files.Sidecars...)
	out = append(out, inv.Files.Embedded...)
	seen := map[string]bool{}
	for _, name := range inv.Surface.GosxNames {
		for _, owner := range name.Owners {
			if strings.HasPrefix(owner, "client/wasm/") && strings.HasSuffix(owner, ".go") && !seen[owner] {
				if strings.HasSuffix(owner, "_test.go") {
					continue
				}
				seen[owner] = true
				out = append(out, SourceFile{Path: owner, Language: "go", SourceKind: "wasm-bridge"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func runtimeJSONSitesForFile(src SourceFile, body []byte, gosxGlobals map[string]bool) ([]RuntimeJSONStaticSite, error) {
	switch {
	case strings.HasSuffix(src.Path, ".go"):
		return runtimeJSONGoSitesForFile(src, body, gosxGlobals)
	case strings.HasSuffix(src.Path, ".js"), strings.HasSuffix(src.Path, ".ts"), strings.HasSuffix(src.Path, ".tsx"):
		return runtimeJSONJSSitesForFile(src, body, gosxGlobals)
	default:
		return nil, nil
	}
}

func runtimeJSONJSSitesForFile(src SourceFile, body []byte, gosxGlobals map[string]bool) ([]RuntimeJSONStaticSite, error) {
	grammar := grammars.JavascriptLanguage()
	if grammar == nil {
		return nil, fmt.Errorf("gotreesitter JavaScript grammar is unavailable")
	}
	tree, err := gotreesitter.NewParser(grammar).Parse(body)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	if root == nil {
		return nil, nil
	}
	var sites []RuntimeJSONStaticSite
	baseAliases := map[string]bool{"window": true, "globalThis": true, "self": true}
	var walk func(*gotreesitter.Node, string, map[string]bool)
	walk = func(n *gotreesitter.Node, owner string, aliases map[string]bool) {
		if n == nil {
			return
		}
		typ := n.Type(grammar)
		if isIgnoredJSNode(typ) {
			return
		}
		if jsCreatesAliasScope(typ) {
			aliases = cloneStringBoolMap(aliases)
		}
		if nextOwner := jsOwnerName(n, grammar, body); nextOwner != "" {
			owner = nextOwner
		}
		learnJSGlobalAliases(n, grammar, body, aliases)
		if typ == "call_expression" {
			if callee := n.Child(0); callee != nil && callee.Type(grammar) == "member_expression" {
				if op := jsonMemberOperation(callee, grammar, body); op != "" {
					sites = append(sites, staticSiteFromNode(src, callee, grammar, body, op, "", owner))
				}
				if name := gosxMemberName(callee, grammar, body, aliases); name != "" {
					gosxGlobals[name] = true
					sites = append(sites, staticSiteFromNode(src, callee, grammar, body, "gosx-call", name, owner))
				}
			}
			if name := objectDefinePropertyGosxName(n, grammar, body, aliases); name != "" {
				gosxGlobals[name] = true
				sites = append(sites, staticSiteFromNode(src, n, grammar, body, "gosx-write", name, owner))
			}
			if name := loadSceneSubFeatureGosxName(n, grammar, body); name != "" {
				gosxGlobals[name] = true
				sites = append(sites, staticSiteFromNode(src, n, grammar, body, "gosx-write", name, owner))
			}
		}
		if typ == "assignment_expression" {
			if left := n.Child(0); left != nil {
				if name := gosxMemberName(left, grammar, body, aliases); name != "" {
					gosxGlobals[name] = true
					sites = append(sites, staticSiteFromNode(src, left, grammar, body, "gosx-write", name, owner))
				}
			}
		}
		if typ == "member_expression" {
			if name := gosxMemberName(n, grammar, body, aliases); name != "" && !isJSCallCallee(n, grammar) && !isJSAssignmentLeft(n, grammar) {
				gosxGlobals[name] = true
				sites = append(sites, staticSiteFromNode(src, n, grammar, body, "gosx-read", name, owner))
			}
		}
		if typ == "identifier" || typ == "property_identifier" {
			text := n.Text(body)
			if op := wireJSONOperation(text); op != "" {
				sites = append(sites, staticSiteFromNode(src, n, grammar, body, op, "", owner))
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			childAliases := aliases
			if child != nil && jsCreatesAliasScope(child.Type(grammar)) {
				childAliases = cloneStringBoolMap(aliases)
			}
			learnJSGlobalAliases(child, grammar, body, childAliases)
			walk(child, owner, childAliases)
		}
	}
	walk(root, "", baseAliases)
	return sites, nil
}

func runtimeJSONGoSitesForFile(src SourceFile, body []byte, gosxGlobals map[string]bool) ([]RuntimeJSONStaticSite, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src.Path, body, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var sites []RuntimeJSONStaticSite
	ownerFor := func(pos token.Pos) string {
		owner := ""
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil || !n.Pos().IsValid() || !n.End().IsValid() || pos < n.Pos() || pos > n.End() {
				return true
			}
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Name != nil {
					owner = fn.Name.Name
				}
			case *ast.FuncLit:
				owner = "func literal"
			}
			return true
		})
		return owner
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "json" {
					switch sel.Sel.Name {
					case "Unmarshal", "NewDecoder", "Valid":
						sites = append(sites, staticSiteFromGoPos(src, fset, x.Pos(), "json-parse", "", ownerFor(x.Pos()), body))
					case "Marshal", "MarshalIndent", "NewEncoder":
						sites = append(sites, staticSiteFromGoPos(src, fset, x.Pos(), "json-stringify", "", ownerFor(x.Pos()), body))
					}
				}
			}
			if lit, ok := firstStringArg(x); ok && gosxExactRe.MatchString(lit) {
				gosxGlobals[lit] = true
				sites = append(sites, staticSiteFromGoPos(src, fset, x.Pos(), "gosx-write", lit, ownerFor(x.Pos()), body))
			}
		case *ast.Ident:
			if op := wireJSONOperation(x.Name); op != "" {
				sites = append(sites, staticSiteFromGoPos(src, fset, x.Pos(), op, "", ownerFor(x.Pos()), body))
			}
		}
		return true
	})
	return sites, nil
}

func isIgnoredJSNode(typ string) bool {
	switch typ {
	case "comment", "string", "template_string", "regex", "jsx_text":
		return true
	default:
		return false
	}
}

func jsCreatesAliasScope(typ string) bool {
	switch typ {
	case "program", "function_declaration", "function_expression", "arrow_function", "method_definition", "statement_block":
		return true
	default:
		return false
	}
}

func cloneStringBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func jsOwnerName(n *gotreesitter.Node, lang *gotreesitter.Language, body []byte) string {
	switch n.Type(lang) {
	case "function_declaration":
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if child != nil && child.Type(lang) == "identifier" {
				return child.Text(body)
			}
		}
	case "method_definition":
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if child != nil && (child.Type(lang) == "property_identifier" || child.Type(lang) == "identifier") {
				return child.Text(body)
			}
		}
	case "variable_declarator":
		if n.ChildCount() >= 3 {
			left := n.Child(0)
			right := n.Child(2)
			if left != nil && right != nil && (right.Type(lang) == "function_expression" || right.Type(lang) == "arrow_function") {
				return left.Text(body)
			}
		}
	}
	return ""
}

func jsonMemberOperation(n *gotreesitter.Node, lang *gotreesitter.Language, body []byte) string {
	if n == nil || n.Type(lang) != "member_expression" || n.ChildCount() < 3 {
		return ""
	}
	if n.Child(0).Text(body) != "JSON" {
		return ""
	}
	prop := n.Child(n.ChildCount() - 1).Text(body)
	switch prop {
	case "parse":
		return "json-parse"
	case "stringify":
		return "json-stringify"
	default:
		return ""
	}
}

func gosxMemberName(n *gotreesitter.Node, lang *gotreesitter.Language, body []byte, aliases map[string]bool) string {
	if n == nil || n.Type(lang) != "member_expression" || n.ChildCount() < 3 {
		return ""
	}
	root := n.Child(0).Text(body)
	if !aliases[root] {
		return ""
	}
	prop := n.Child(n.ChildCount() - 1).Text(body)
	if gosxExactRe.MatchString(prop) {
		return prop
	}
	return ""
}

func learnJSGlobalAliases(n *gotreesitter.Node, lang *gotreesitter.Language, body []byte, aliases map[string]bool) {
	if n == nil {
		return
	}
	typ := n.Type(lang)
	if typ != "lexical_declaration" && typ != "variable_declaration" {
		return
	}
	for i := 0; i < n.ChildCount(); i++ {
		d := n.Child(i)
		if d == nil || d.Type(lang) != "variable_declarator" || d.ChildCount() < 3 {
			continue
		}
		name := d.Child(0).Text(body)
		value := d.Child(2).Text(body)
		if value == "window" || value == "globalThis" || value == "self" || value == "this" || strings.Contains(value, "globalThis") || strings.Contains(value, "window") || strings.Contains(value, "self") {
			aliases[name] = true
		}
	}
}

func objectDefinePropertyGosxName(n *gotreesitter.Node, lang *gotreesitter.Language, body []byte, aliases map[string]bool) string {
	if n == nil || n.Type(lang) != "call_expression" || n.ChildCount() < 2 {
		return ""
	}
	callee := n.Child(0)
	if callee == nil || callee.Type(lang) != "member_expression" || callee.Text(body) != "Object.defineProperty" {
		return ""
	}
	args := n.Child(1)
	if args == nil || args.Type(lang) != "arguments" {
		return ""
	}
	var values []*gotreesitter.Node
	for i := 0; i < args.ChildCount(); i++ {
		child := args.Child(i)
		if child == nil {
			continue
		}
		typ := child.Type(lang)
		if typ == "(" || typ == ")" || typ == "," {
			continue
		}
		values = append(values, child)
	}
	if len(values) < 2 || !aliases[values[0].Text(body)] {
		return ""
	}
	name := strings.Trim(values[1].Text(body), `"'`)
	if gosxExactRe.MatchString(name) {
		return name
	}
	return ""
}

func loadSceneSubFeatureGosxName(n *gotreesitter.Node, lang *gotreesitter.Language, body []byte) string {
	if n == nil || n.Type(lang) != "call_expression" || n.ChildCount() < 2 {
		return ""
	}
	callee := n.Child(0)
	if callee == nil || callee.Text(body) != "loadSceneSubFeature" {
		return ""
	}
	args := n.Child(1)
	if args == nil || args.Type(lang) != "arguments" {
		return ""
	}
	values := jsArgumentNodes(args, lang)
	if len(values) < 4 {
		return ""
	}
	if values[3].Type(lang) != "string" {
		return ""
	}
	name := strings.Trim(values[3].Text(body), `"'`)
	if gosxExactRe.MatchString(name) {
		return name
	}
	return ""
}

func jsArgumentNodes(args *gotreesitter.Node, lang *gotreesitter.Language) []*gotreesitter.Node {
	var values []*gotreesitter.Node
	if args == nil {
		return values
	}
	for i := 0; i < args.ChildCount(); i++ {
		child := args.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "(", ")", ",":
			continue
		default:
			values = append(values, child)
		}
	}
	return values
}

func isJSCallCallee(n *gotreesitter.Node, lang *gotreesitter.Language) bool {
	parent := n.Parent()
	return parent != nil && parent.Type(lang) == "call_expression" && parent.ChildCount() > 0 && sameTSNode(parent.Child(0), n)
}

func isJSAssignmentLeft(n *gotreesitter.Node, lang *gotreesitter.Language) bool {
	parent := n.Parent()
	return parent != nil && parent.Type(lang) == "assignment_expression" && parent.ChildCount() > 0 && sameTSNode(parent.Child(0), n)
}

func sameTSNode(a, b *gotreesitter.Node) bool {
	if a == nil || b == nil {
		return false
	}
	return a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}

func wireJSONOperation(name string) string {
	if !wireJSONNameRe.MatchString(name) {
		return ""
	}
	switch name {
	case "propsJSON":
		return "props-json"
	case "patchJSON":
		return "patch-json"
	case "valueJSON":
		return "value-json"
	default:
		return ""
	}
}

func staticSiteFromNode(src SourceFile, n *gotreesitter.Node, lang *gotreesitter.Language, body []byte, op, globalName, owner string) RuntimeJSONStaticSite {
	line := sourceLine(body, int(n.StartPoint().Row)+1)
	owner = fallbackOwner(owner, src, op, line)
	site := RuntimeJSONStaticSite{
		Path:         src.Path,
		Line:         int(n.StartPoint().Row) + 1,
		Column:       int(n.StartPoint().Column) + 1,
		SourceFamily: runtimeJSONSourceFamily(src.Path, src.SourceKind),
		SourceKind:   src.SourceKind,
		Operation:    op,
		GlobalName:   globalName,
		Symbol:       owner,
		TextHash:     shortHash(strings.TrimSpace(line)),
	}
	applyRuntimeJSONPhaseClassification(&site, line)
	return site
}

func staticSiteFromGoPos(src SourceFile, fset *token.FileSet, pos token.Pos, op, globalName, owner string, body []byte) RuntimeJSONStaticSite {
	p := fset.Position(pos)
	line := sourceLine(body, p.Line)
	owner = fallbackOwner(owner, src, op, line)
	site := RuntimeJSONStaticSite{
		Path:         src.Path,
		Line:         p.Line,
		Column:       p.Column,
		SourceFamily: runtimeJSONSourceFamily(src.Path, src.SourceKind),
		SourceKind:   src.SourceKind,
		Operation:    op,
		GlobalName:   globalName,
		Symbol:       owner,
		TextHash:     shortHash(strings.TrimSpace(line)),
	}
	applyRuntimeJSONPhaseClassification(&site, line)
	return site
}

type runtimeJSONPhaseClassification struct {
	status   string
	phases   []string
	rule     string
	reason   string
	evidence []string
}

func applyRuntimeJSONPhaseClassification(site *RuntimeJSONStaticSite, line string) {
	classification := classifyRuntimeJSONStaticPhase(*site, line)
	site.PhaseStatus = classification.status
	site.PossiblePhases = append([]string{}, classification.phases...)
	site.PhaseRule = classification.rule
	site.PhaseReason = classification.reason
	site.PhaseEvidence = append([]string{}, classification.evidence...)
	site.HotPathPossible = phasesContainHot(site.PossiblePhases)
	site.HotPathConfirmed = false
	site.Phase = ""
	if classification.status == "exact" && len(classification.phases) == 1 {
		site.Phase = classification.phases[0]
		site.HotPathConfirmed = runtimeJSONHotPhases[site.Phase]
		site.HotPathPossible = site.HotPathConfirmed
	}
	site.ContextHash = shortHash(strings.Join([]string{
		site.Path,
		strconv.Itoa(site.Line),
		strconv.Itoa(site.Column),
		site.Operation,
		site.GlobalName,
		site.Symbol,
		site.PhaseStatus,
		strings.Join(site.PossiblePhases, ","),
		site.PhaseRule,
		site.TextHash,
	}, "\x00"))
}

func classifyRuntimeJSONStaticPhase(site RuntimeJSONStaticSite, line string) runtimeJSONPhaseClassification {
	path := strings.ToLower(site.Path)
	owner := strings.ToLower(site.Symbol)
	global := strings.ToLower(site.GlobalName)
	op := strings.ToLower(site.Operation)
	lowerLine := strings.ToLower(line)

	if strings.Contains(path, "navigation_runtime") && strings.Contains(lowerLine, "json.parse") && (strings.Contains(lowerLine, "textcontent") || strings.Contains(lowerLine, "route") || strings.Contains(lowerLine, "config")) {
		return runtimeJSONStaticPhase("R10", "navigation runtime parses embedded route data", []string{"route-load"}, "path:navigation_runtime", "line:json.parse route/textContent")
	}
	if strings.Contains(lowerLine, "socket.send(json.stringify({ event:") || strings.Contains(lowerLine, "socket.send(json.stringify({event:") {
		return runtimeJSONStaticPhase("R10", "tail hub event serialization is a network send", []string{"network"}, "line:socket.send(JSON.stringify({event")
	}
	if op == "patch-json" || strings.Contains(owner, "patch") || strings.Contains(lowerLine, "patchjson") || strings.Contains(lowerLine, "applypatch") {
		return runtimeJSONStaticPhase("R10", "patch token structurally identifies patch work", []string{"patch"}, "op:"+site.Operation, "owner:"+site.Symbol)
	}
	if strings.Contains(path, "textlayout") && (strings.Contains(owner, "sharedsignal") || op == "value-json" || strings.Contains(lowerLine, "valuejson")) {
		return runtimeJSONStaticPhase("R10", "text layout shared values can originate from input and reconcile into frame work", []string{"input", "reconciliation", "frame"}, "path:textlayout", "owner:"+site.Symbol, "op:"+site.Operation)
	}
	if strings.Contains(global, "__gosx_set_input_batch") || strings.Contains(lowerLine, "setinputbatch") {
		return runtimeJSONStaticPhase("R10", "input batch bridge is structurally input phase", []string{"input"}, "bridge:"+site.GlobalName)
	}

	if class, ok := phaseByToken(global, "R20", "bridge name token"); ok {
		return class
	}
	if class, ok := phaseByToken(owner, "R30", "owner token"); ok {
		return class
	}
	if class, ok := phaseByPath(path); ok {
		return class
	}
	if class, ok := phaseByToken(lowerLine, "R50", "line token"); ok {
		return class
	}
	return runtimeJSONUnknownPhase("no ranked static phase rule matched", "path:"+site.Path, "owner:"+site.Symbol, "op:"+site.Operation)
}

func phaseByPath(path string) (runtimeJSONPhaseClassification, bool) {
	switch {
	case strings.Contains(path, "navigation_runtime"):
		return runtimeJSONStaticPhase("R40", "navigation runtime file can load routes, handle input, or use network", []string{"route-load", "input", "network"}, "path:navigation_runtime"), true
	case strings.Contains(path, "telemetry"):
		return runtimeJSONStaticPhase("R40", "telemetry path token", []string{"telemetry"}, "path:telemetry"), true
	case strings.Contains(path, "hub") || strings.Contains(path, "socket"):
		return runtimeJSONStaticPhase("R40", "hub or socket path token", []string{"network"}, "path:"+path), true
	case strings.Contains(path, "textlayout"):
		return runtimeJSONStaticPhase("R40", "text layout path can affect input, reconciliation, and frame", []string{"input", "reconciliation", "frame"}, "path:textlayout"), true
	case strings.Contains(path, "scene"):
		return runtimeJSONStaticPhase("R40", "scene path can affect frame and interactive debug surfaces", []string{"input", "frame", "debug", "telemetry"}, "path:scene"), true
	case strings.Contains(path, "declarative") || strings.Contains(path, "region"):
		return runtimeJSONStaticPhase("R40", "declarative region path can reconcile and patch", []string{"reconciliation", "patch"}, "path:"+path), true
	case strings.Contains(path, "controller"):
		return runtimeJSONStaticPhase("R40", "controller path token", []string{"input"}, "path:controller"), true
	default:
		return runtimeJSONPhaseClassification{}, false
	}
}

func phaseByToken(text, rule, reason string) (runtimeJSONPhaseClassification, bool) {
	text = strings.ToLower(text)
	switch {
	case text == "":
		return runtimeJSONPhaseClassification{}, false
	case strings.Contains(text, "input") || strings.Contains(text, "pointer") || strings.Contains(text, "keydown") || strings.Contains(text, "click"):
		return runtimeJSONStaticPhase(rule, reason+" matched input token", []string{"input"}, "token:input"), true
	case strings.Contains(text, "action") || strings.Contains(text, "dispatch") || strings.Contains(text, "submit"):
		return runtimeJSONStaticPhase(rule, reason+" matched dispatch token", []string{"dispatch"}, "token:dispatch"), true
	case strings.Contains(text, "patch") || strings.Contains(text, "apply"):
		return runtimeJSONStaticPhase(rule, reason+" matched patch token", []string{"patch"}, "token:patch"), true
	case strings.Contains(text, "hydrate") || strings.Contains(text, "reconcile") || strings.Contains(text, "shared_signal") || strings.Contains(text, "sharedsignal") || strings.Contains(text, "signal"):
		return runtimeJSONStaticPhase(rule, reason+" matched reconciliation token", []string{"reconciliation"}, "token:reconciliation"), true
	case strings.Contains(text, "frame") || strings.Contains(text, "tick") || strings.Contains(text, "render") || strings.Contains(text, "raf") || strings.Contains(text, "scene3d"):
		return runtimeJSONStaticPhase(rule, reason+" matched frame token", []string{"frame"}, "token:frame"), true
	case strings.Contains(text, "socket") || strings.Contains(text, "fetch") || strings.Contains(text, "hub") || strings.Contains(text, "sendbeacon"):
		return runtimeJSONStaticPhase(rule, reason+" matched network token", []string{"network"}, "token:network"), true
	case strings.Contains(text, "telemetry") || strings.Contains(text, "emit"):
		return runtimeJSONStaticPhase(rule, reason+" matched telemetry token", []string{"telemetry"}, "token:telemetry"), true
	case strings.Contains(text, "debug") || strings.Contains(text, "diagnostic"):
		return runtimeJSONStaticPhase(rule, reason+" matched debug token", []string{"debug"}, "token:debug"), true
	case strings.Contains(text, "manifest") || strings.Contains(text, "route") || strings.Contains(text, "load") || strings.Contains(text, "bootstrap"):
		return runtimeJSONStaticPhase(rule, reason+" matched route-load token", []string{"route-load"}, "token:route-load"), true
	default:
		return runtimeJSONPhaseClassification{}, false
	}
}

func runtimeJSONStaticPhase(rule, reason string, phases []string, evidence ...string) runtimeJSONPhaseClassification {
	phases = normalizeRuntimeJSONPhaseSet(phases)
	if len(phases) == 0 {
		return runtimeJSONUnknownPhase(reason, evidence...)
	}
	status := "exact"
	if len(phases) > 1 {
		status = "ambiguous"
	}
	return runtimeJSONPhaseClassification{
		status:   status,
		phases:   phases,
		rule:     rule,
		reason:   reason,
		evidence: sanitizedPhaseEvidence(evidence),
	}
}

func runtimeJSONUnknownPhase(reason string, evidence ...string) runtimeJSONPhaseClassification {
	return runtimeJSONPhaseClassification{
		status:   "unknown",
		phases:   append([]string{}, runtimeJSONAllPhases...),
		rule:     "R90",
		reason:   reason,
		evidence: sanitizedPhaseEvidence(evidence),
	}
}

func normalizeRuntimeJSONPhaseSet(phases []string) []string {
	allowed := map[string]bool{}
	for _, phase := range phases {
		allowed[normalizeO02Phase(phase)] = true
	}
	var out []string
	for _, phase := range runtimeJSONAllPhases {
		if allowed[phase] {
			out = append(out, phase)
		}
	}
	return out
}

func phasesContainHot(phases []string) bool {
	for _, phase := range phases {
		if runtimeJSONHotPhases[phase] {
			return true
		}
	}
	return false
}

func sanitizedPhaseEvidence(evidence []string) []string {
	out := make([]string, 0, len(evidence))
	for _, item := range evidence {
		item = strings.TrimSpace(strings.ReplaceAll(item, "\n", " "))
		if item == "" {
			continue
		}
		if len(item) > 120 {
			item = item[:120]
		}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func fallbackOwner(owner string, src SourceFile, op, line string) string {
	if owner != "" {
		return owner
	}
	if strings.Contains(op, "json") {
		return strings.TrimSuffix(filepath.Base(src.Path), filepath.Ext(src.Path))
	}
	if strings.Contains(line, "Object.defineProperty") {
		return "Object.defineProperty"
	}
	return strings.TrimSuffix(filepath.Base(src.Path), filepath.Ext(src.Path))
}

func sourceLine(body []byte, lineNo int) string {
	if lineNo <= 0 {
		return ""
	}
	lines := strings.Split(string(body), "\n")
	if lineNo > len(lines) {
		return ""
	}
	return lines[lineNo-1]
}

func firstStringArg(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func runtimeJSONSourceFamily(path, kind string) string {
	switch {
	case strings.HasPrefix(path, "client/js/bootstrap-src/"):
		return "bootstrap-runtime"
	case kind == "sidecar":
		return "runtime-sidecar"
	case kind == "embedded":
		return "embedded-browser"
	case strings.HasPrefix(path, "client/wasm/"):
		return "wasm-bridge"
	default:
		return "first-party-browser"
	}
}

func normalizeO02Phase(phase string) string {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(phase)), " ", "-") {
	case "cold-load", "route-load", "input", "dispatch", "reconciliation", "patch", "frame", "network", "debug", "telemetry":
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(phase)), " ", "-")
	case "load", "route":
		return "route-load"
	default:
		return "unknown"
	}
}

func ValidateRuntimeJSONStaticCorpus(corpus *RuntimeJSONStaticCorpus) error {
	if corpus == nil {
		return fmt.Errorf("runtime JSON static corpus is nil")
	}
	if corpus.SchemaVersion != RuntimeJSONProbeSchemaVersion {
		return fmt.Errorf("schema version = %q, want %q", corpus.SchemaVersion, RuntimeJSONProbeSchemaVersion)
	}
	if corpus.Contract != ContractO02 {
		return fmt.Errorf("contract version = %q, want %q", corpus.Contract, ContractO02)
	}
	if corpus.ScannerVersion != runtimeJSONStaticScannerVersion {
		return fmt.Errorf("scanner version = %q, want %q", corpus.ScannerVersion, runtimeJSONStaticScannerVersion)
	}
	if corpus.PhaseClassifierVersion != runtimeJSONPhaseClassifierVersion {
		return fmt.Errorf("phase classifier field = %q, want %q", corpus.PhaseClassifierVersion, runtimeJSONPhaseClassifierVersion)
	}
	if corpus.Query.PhaseClassifier != runtimeJSONPhaseClassifierVersion {
		return fmt.Errorf("phase classifier version = %q, want %q", corpus.Query.PhaseClassifier, runtimeJSONPhaseClassifierVersion)
	}
	if !sameSourceIdentity(corpus.CurrentSourceIdentity, corpus.Source) {
		return fmt.Errorf("current source identity does not match source")
	}
	if got, want := corpus.CurrentSourceIdentityHash, RuntimeJSONStaticSourceIdentityHash(corpus.Source); got != want {
		return fmt.Errorf("current source identity hash = %q, want %q", got, want)
	}
	counts := RuntimeJSONStaticCounts{
		ByOperation:      map[string]int{},
		ByPhase:          map[string]int{},
		ByPossiblePhase:  map[string]int{},
		ByPhaseStatus:    map[string]int{},
		BySourceFamily:   map[string]int{},
		TargetStatus:     corpus.Counts.TargetStatus,
		FailClosed:       corpus.Counts.FailClosed,
		FailClosedReason: corpus.Counts.FailClosedReason,
	}
	globals := map[string]bool{}
	for i, site := range corpus.Sites {
		if err := validateRuntimeJSONStaticSite(site); err != nil {
			return fmt.Errorf("site[%d] %s:%d:%d: %w", i, site.Path, site.Line, site.Column, err)
		}
		counts.ByOperation[site.Operation]++
		if site.Phase != "" {
			counts.ByPhase[site.Phase]++
		}
		for _, phase := range site.PossiblePhases {
			counts.ByPossiblePhase[phase]++
		}
		counts.ByPhaseStatus[site.PhaseStatus]++
		counts.BySourceFamily[site.SourceFamily]++
		if site.GlobalName != "" {
			globals[site.GlobalName] = true
		}
		if site.HotPathPossible {
			counts.HotPathPossibleCount++
		}
		if site.HotPathConfirmed {
			counts.HotPathConfirmedCount++
		}
		switch site.PhaseStatus {
		case "unknown":
			counts.UnknownCount++
		case "ambiguous":
			counts.AmbiguousCount++
		case "exact":
			counts.ExactCount++
		}
		if isRuntimeJSONSerializationOperation(site.Operation) {
			counts.SerializationSiteCount++
			if site.HotPathPossible {
				counts.SerializationHotPathPossibleCount++
			}
			if site.HotPathConfirmed {
				counts.SerializationHotPathConfirmedCount++
			}
		}
		switch site.Operation {
		case "json-parse":
			counts.JSONParseCount++
		case "json-stringify":
			counts.JSONStringifyCount++
		case "props-json":
			counts.PropsJSONCount++
		case "patch-json":
			counts.PatchJSONCount++
		case "value-json":
			counts.ValueJSONCount++
		case "gosx-read":
			counts.GosxReadCount++
		case "gosx-write":
			counts.GosxWriteCount++
		case "gosx-call":
			counts.GosxCallCount++
		default:
			return fmt.Errorf("site[%d] has unknown operation %q", i, site.Operation)
		}
	}
	counts.UniqueGosxGlobals = len(globals)
	if err := compareRuntimeJSONStaticCounts(corpus.Counts, counts); err != nil {
		return err
	}
	globalSet := RuntimeJSONStaticGlobalSetFromMap(globals)
	if !sameRuntimeJSONStaticGlobalSet(corpus.GlobalNames, globalSet) {
		return fmt.Errorf("global name set = %+v, want %+v", corpus.GlobalNames, globalSet)
	}
	if got, want := corpus.CountsHash, RuntimeJSONStaticCountsHash(counts); got != want {
		return fmt.Errorf("countsHash = %q, want %q", got, want)
	}
	if got, want := corpus.SemanticHash, RuntimeJSONStaticCorpusSemanticHash(corpus); got != want {
		return fmt.Errorf("semanticHash = %q, want %q", got, want)
	}
	return nil
}

func validateRuntimeJSONStaticSite(site RuntimeJSONStaticSite) error {
	if site.PhaseRule == "" || site.PhaseReason == "" {
		return fmt.Errorf("missing phase rule or reason")
	}
	if site.TextHash == "" || site.ContextHash == "" {
		return fmt.Errorf("missing textHash or contextHash")
	}
	if isRuntimeJSONSerializationOperation(site.Operation) && strings.TrimSpace(site.Symbol) == "" {
		return fmt.Errorf("serialization row has empty owning symbol")
	}
	if len(site.PossiblePhases) == 0 {
		return fmt.Errorf("possiblePhases is empty")
	}
	seen := map[string]bool{}
	for _, phase := range site.PossiblePhases {
		if !runtimeJSONPhaseAllowed(phase) {
			return fmt.Errorf("possible phase %q is not allowed", phase)
		}
		if seen[phase] {
			return fmt.Errorf("duplicate possible phase %q", phase)
		}
		seen[phase] = true
	}
	if !sameStringSlice(site.PossiblePhases, normalizeRuntimeJSONPhaseSet(site.PossiblePhases)) {
		return fmt.Errorf("possiblePhases are not canonical ordered: %v", site.PossiblePhases)
	}
	hotPossible := phasesContainHot(site.PossiblePhases)
	switch site.PhaseStatus {
	case "exact":
		if len(site.PossiblePhases) != 1 {
			return fmt.Errorf("exact row has %d possible phases", len(site.PossiblePhases))
		}
		if site.Phase != site.PossiblePhases[0] {
			return fmt.Errorf("exact phase %q does not match possible phase %q", site.Phase, site.PossiblePhases[0])
		}
		if site.HotPathConfirmed != runtimeJSONHotPhases[site.Phase] {
			return fmt.Errorf("hotPathConfirmed mismatch")
		}
		if site.HotPathPossible != site.HotPathConfirmed {
			return fmt.Errorf("exact hotPathPossible mismatch")
		}
	case "ambiguous":
		if len(site.PossiblePhases) < 2 {
			return fmt.Errorf("ambiguous row has %d possible phases", len(site.PossiblePhases))
		}
		if site.Phase != "" {
			return fmt.Errorf("ambiguous row set compatibility phase %q", site.Phase)
		}
		if site.HotPathConfirmed {
			return fmt.Errorf("ambiguous row confirmed hot path")
		}
		if site.HotPathPossible != hotPossible {
			return fmt.Errorf("ambiguous hotPathPossible mismatch")
		}
	case "unknown":
		if site.Phase != "" {
			return fmt.Errorf("unknown row set compatibility phase %q", site.Phase)
		}
		if !sameStringSlice(site.PossiblePhases, runtimeJSONAllPhases) {
			return fmt.Errorf("unknown row possible phases = %v", site.PossiblePhases)
		}
		if !site.HotPathPossible || site.HotPathConfirmed {
			return fmt.Errorf("unknown row hot path flags are invalid")
		}
		if site.PhaseRule != "R90" {
			return fmt.Errorf("unknown row uses phase rule %q", site.PhaseRule)
		}
	default:
		return fmt.Errorf("invalid phaseStatus %q", site.PhaseStatus)
	}
	return nil
}

func compareRuntimeJSONStaticCounts(got, want RuntimeJSONStaticCounts) error {
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"serializationSiteCount", got.SerializationSiteCount, want.SerializationSiteCount},
		{"jsonParseCount", got.JSONParseCount, want.JSONParseCount},
		{"jsonStringifyCount", got.JSONStringifyCount, want.JSONStringifyCount},
		{"propsJSONCount", got.PropsJSONCount, want.PropsJSONCount},
		{"patchJSONCount", got.PatchJSONCount, want.PatchJSONCount},
		{"valueJSONCount", got.ValueJSONCount, want.ValueJSONCount},
		{"gosxReadCount", got.GosxReadCount, want.GosxReadCount},
		{"gosxWriteCount", got.GosxWriteCount, want.GosxWriteCount},
		{"gosxCallCount", got.GosxCallCount, want.GosxCallCount},
		{"hotPathPossibleCount", got.HotPathPossibleCount, want.HotPathPossibleCount},
		{"hotPathConfirmedCount", got.HotPathConfirmedCount, want.HotPathConfirmedCount},
		{"serializationHotPathPossibleCount", got.SerializationHotPathPossibleCount, want.SerializationHotPathPossibleCount},
		{"serializationHotPathConfirmedCount", got.SerializationHotPathConfirmedCount, want.SerializationHotPathConfirmedCount},
		{"unknownCount", got.UnknownCount, want.UnknownCount},
		{"ambiguousCount", got.AmbiguousCount, want.AmbiguousCount},
		{"exactCount", got.ExactCount, want.ExactCount},
		{"uniqueGosxGlobals", got.UniqueGosxGlobals, want.UniqueGosxGlobals},
	}
	for _, check := range checks {
		if check.got != check.want {
			return fmt.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
	for _, check := range []struct {
		name string
		got  map[string]int
		want map[string]int
	}{
		{"byOperation", got.ByOperation, want.ByOperation},
		{"byPhase", got.ByPhase, want.ByPhase},
		{"byPossiblePhase", got.ByPossiblePhase, want.ByPossiblePhase},
		{"byPhaseStatus", got.ByPhaseStatus, want.ByPhaseStatus},
		{"bySourceFamily", got.BySourceFamily, want.BySourceFamily},
	} {
		if !sameStringIntMap(check.got, check.want) {
			return fmt.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
	return nil
}

func isRuntimeJSONSerializationOperation(op string) bool {
	switch op {
	case "json-parse", "json-stringify", "props-json", "patch-json", "value-json":
		return true
	default:
		return false
	}
}

func runtimeJSONPhaseAllowed(phase string) bool {
	for _, allowed := range runtimeJSONAllPhases {
		if phase == allowed {
			return true
		}
	}
	return false
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameStringIntMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if b[k] != av {
			return false
		}
	}
	return true
}

func sameRuntimeJSONStaticGlobalSet(a, b RuntimeJSONStaticGlobalSet) bool {
	return a.Count == b.Count && a.Hash == b.Hash && sameStringSlice(a.Names, b.Names)
}

func sameSourceIdentity(a, b SourceIdentity) bool {
	return RuntimeJSONStaticSourceIdentityHash(a) == RuntimeJSONStaticSourceIdentityHash(b)
}

func hashJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func shortHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func writeRuntimeJSONL(w *bufio.Writer, kind string, value any) error {
	row := map[string]any{"kind": kind, "value": value}
	body, err := json.Marshal(row)
	if err != nil {
		return err
	}
	_, err = w.Write(append(body, '\n'))
	return err
}

func RuntimeJSONStaticCorpusSummary(corpus *RuntimeJSONStaticCorpus) string {
	if corpus == nil {
		return "runtime-json static corpus: nil"
	}
	return fmt.Sprintf("runtime-json static corpus: serializationSites=%d serializationHotPathPossible=%d serializationHotPathConfirmed=%d exact=%d ambiguous=%d unknown=%d gosxGlobals=%d failClosed=%v targetStatus=%s query=%s",
		corpus.Counts.SerializationSiteCount,
		corpus.Counts.SerializationHotPathPossibleCount,
		corpus.Counts.SerializationHotPathConfirmedCount,
		corpus.Counts.ExactCount,
		corpus.Counts.AmbiguousCount,
		corpus.Counts.UnknownCount,
		corpus.Counts.UniqueGosxGlobals,
		corpus.Counts.FailClosed,
		corpus.Counts.TargetStatus,
		corpus.Query.ID,
	)
}
