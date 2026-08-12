package ouroboros

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

const RuntimeJSONDynamicEvidenceSchemaVersion = "gosx.ouroboros.runtime-json-dynamic-evidence.v1"
const RuntimeJSONDynamicEvidenceContractVersion = "gosx.ouroboros.runtime-json-dynamic-evidence.v1"

const (
	RuntimeJSONDynamicLaneProduct       = "product"
	RuntimeJSONDynamicLaneProbe         = "probe"
	RuntimeJSONDynamicLaneProbeOverhead = "probe-overhead"
)

type RuntimeJSONRawDrain struct {
	SchemaVersion       string                    `json:"schemaVersion"`
	FacadeSchemaVersion int                       `json:"facadeSchemaVersion"`
	Version             string                    `json:"version"`
	Phase               string                    `json:"phase"`
	RouteID             string                    `json:"routeID"`
	Events              []ProbeEvent              `json:"events"`
	DroppedCount        int                       `json:"droppedCount"`
	WrappedGlobals      []string                  `json:"wrappedGlobals"`
	UnwrappedGlobals    []string                  `json:"unwrappedGlobals"`
	KnownGlobals        []string                  `json:"knownGlobals"`
	Limits              RuntimeJSONRawDrainLimits `json:"limits"`
}

type RuntimeJSONRawDrainLimits struct {
	EventLimit int `json:"eventLimit"`
}

type RuntimeJSONDynamicSourceBinding struct {
	BaseRevision                string `json:"baseRevision"`
	OverlayHash                 string `json:"overlayHash"`
	TrackedDiffHash             string `json:"trackedDiffHash"`
	UntrackedIncludedSourceHash string `json:"untrackedIncludedSourceHash"`
	InventorySHA256             string `json:"inventorySha256"`
}

type RuntimeJSONDynamicStaticBinding struct {
	SourceIdentityHash string   `json:"sourceIdentityHash"`
	SemanticHash       string   `json:"semanticHash"`
	CountsHash         string   `json:"countsHash"`
	GlobalNameHash     string   `json:"globalNameHash"`
	ScannerVersion     string   `json:"scannerVersion"`
	PhaseClassifier    string   `json:"phaseClassifier"`
	KnownGlobals       []string `json:"knownGlobals"`
}

type RuntimeJSONDynamicEvidenceInput struct {
	GeneratedAt string                          `json:"generatedAt,omitempty"`
	Source      RuntimeJSONDynamicSourceBinding `json:"source"`
	Static      RuntimeJSONDynamicStaticBinding `json:"static"`
	Samples     []RuntimeJSONDynamicSampleInput `json:"samples"`
}

type RuntimeJSONDynamicSampleInput struct {
	Lane                string               `json:"lane"`
	RouteID             string               `json:"routeID"`
	CacheMode           string               `json:"cacheMode"`
	SampleIndex         int                  `json:"sampleIndex"`
	DurationMs          float64              `json:"durationMs"`
	Pilot               bool                 `json:"pilot"`
	Discarded           bool                 `json:"discarded"`
	ProductPathPrefixes []string             `json:"productPathPrefixes,omitempty"`
	HarnessPathPrefixes []string             `json:"harnessPathPrefixes,omitempty"`
	ProbePathPrefixes   []string             `json:"probePathPrefixes,omitempty"`
	Drain               *RuntimeJSONRawDrain `json:"drain,omitempty"`
}

type RuntimeJSONDynamicEvidenceManifest struct {
	SchemaVersion string                              `json:"schemaVersion"`
	Contract      string                              `json:"contractVersion"`
	GeneratedAt   string                              `json:"generatedAt,omitempty"`
	Source        RuntimeJSONDynamicSourceBinding     `json:"source"`
	Static        RuntimeJSONDynamicStaticBinding     `json:"static"`
	Sampling      RuntimeJSONDynamicSampling          `json:"sampling"`
	Samples       []RuntimeJSONDynamicSample          `json:"samples"`
	Events        []RuntimeJSONDynamicEvent           `json:"events"`
	Matrix        []RuntimeJSONDynamicMatrixRow       `json:"matrix"`
	OverheadPairs []RuntimeJSONDynamicOverheadPair    `json:"overheadPairs,omitempty"`
	Validation    RuntimeJSONDynamicValidationSummary `json:"validation"`
}

type RuntimeJSONDynamicSampling struct {
	CanonicalRoutes              []string `json:"canonicalRoutes"`
	CacheModes                   []string `json:"cacheModes"`
	ProbeOverheadPilotsDiscarded int      `json:"probeOverheadPilotsDiscarded"`
	ProbeEvidenceSamples         int      `json:"probeEvidenceSamples"`
	ProductSamplesHaveEProbe     bool     `json:"productSamplesHaveEProbe"`
}

type RuntimeJSONDynamicSample struct {
	ID                  string               `json:"id"`
	Lane                string               `json:"lane"`
	RouteID             string               `json:"routeID"`
	CacheMode           string               `json:"cacheMode"`
	SampleIndex         int                  `json:"sampleIndex"`
	DurationMs          float64              `json:"durationMs"`
	Pilot               bool                 `json:"pilot"`
	Discarded           bool                 `json:"discarded"`
	Drain               *RuntimeJSONRawDrain `json:"drain,omitempty"`
	ProductPathPrefixes []string             `json:"productPathPrefixes,omitempty"`
	HarnessPathPrefixes []string             `json:"harnessPathPrefixes,omitempty"`
	ProbePathPrefixes   []string             `json:"probePathPrefixes,omitempty"`
}

type RuntimeJSONDynamicEvent struct {
	SampleID               string                        `json:"sampleID"`
	Lane                   string                        `json:"lane"`
	RouteID                string                        `json:"routeID"`
	CacheMode              string                        `json:"cacheMode"`
	SampleIndex            int                           `json:"sampleIndex"`
	Kind                   string                        `json:"kind"`
	Name                   string                        `json:"name"`
	Phase                  string                        `json:"phase"`
	Source                 RuntimeJSONDynamicSource      `json:"source"`
	SourceClass            RuntimeJSONDynamicSourceClass `json:"sourceClass"`
	StackHash              string                        `json:"stackHash,omitempty"`
	ArgCount               int                           `json:"argCount,omitempty"`
	ArgBytes               []int                         `json:"argBytes,omitempty"`
	PayloadBytes           int                           `json:"payloadBytes,omitempty"`
	ResultBytes            int                           `json:"resultBytes,omitempty"`
	Exception              string                        `json:"exception,omitempty"`
	EventKind              int                           `json:"eventKind,omitempty"`
	HotPath                bool                          `json:"hotPath"`
	IncludeInProductCounts bool                          `json:"includeInProductCounts"`
}

type RuntimeJSONDynamicMatrixRow struct {
	RouteID                 string `json:"routeID"`
	CacheMode               string `json:"cacheMode"`
	ProbeOverheadPilotCount int    `json:"probeOverheadPilotCount"`
	ProbeEvidenceCount      int    `json:"probeEvidenceCount"`
	ProductEventCount       int    `json:"productEventCount"`
	HotUnknownEventCount    int    `json:"hotUnknownEventCount"`
	ObservedZeroProduct     bool   `json:"observedZeroProduct"`
}

type RuntimeJSONDynamicOverheadPair struct {
	RouteID           string  `json:"routeID"`
	CacheMode         string  `json:"cacheMode"`
	SampleIndex       int     `json:"sampleIndex"`
	ProductSampleID   string  `json:"productSampleID"`
	ProbeSampleID     string  `json:"probeSampleID"`
	ProductDurationMs float64 `json:"productDurationMs"`
	ProbeDurationMs   float64 `json:"probeDurationMs"`
	OverheadMs        float64 `json:"overheadMs"`
	Informational     bool    `json:"informational"`
}

type RuntimeJSONDynamicValidationSummary struct {
	Status string   `json:"status"`
	Errors []string `json:"errors,omitempty"`
}

func DynamicSourceBindingFromSourceIdentity(source SourceIdentity) RuntimeJSONDynamicSourceBinding {
	return RuntimeJSONDynamicSourceBinding{
		BaseRevision:                source.BaseRevision,
		OverlayHash:                 source.OverlayHash,
		TrackedDiffHash:             source.TrackedDiffHash,
		UntrackedIncludedSourceHash: source.UntrackedIncludedSourceHash,
		InventorySHA256:             source.InventorySHA256,
	}
}

func DynamicStaticBindingFromRuntimeJSONStaticIdentity(static *RuntimeJSONStaticIdentity, knownGlobals []string) RuntimeJSONDynamicStaticBinding {
	if static == nil {
		return RuntimeJSONDynamicStaticBinding{KnownGlobals: uniqueStrings(knownGlobals)}
	}
	return RuntimeJSONDynamicStaticBinding{
		SourceIdentityHash: static.SourceIdentityHash,
		SemanticHash:       static.SemanticHash,
		CountsHash:         static.CountsHash,
		GlobalNameHash:     static.GlobalNameHash,
		ScannerVersion:     static.ScannerVersion,
		PhaseClassifier:    static.PhaseClassifier,
		KnownGlobals:       uniqueStrings(knownGlobals),
	}
}

func BuildRuntimeJSONDynamicEvidence(input RuntimeJSONDynamicEvidenceInput) (*RuntimeJSONDynamicEvidenceManifest, error) {
	manifest := &RuntimeJSONDynamicEvidenceManifest{
		SchemaVersion: RuntimeJSONDynamicEvidenceSchemaVersion,
		Contract:      RuntimeJSONDynamicEvidenceContractVersion,
		GeneratedAt:   input.GeneratedAt,
		Source:        input.Source,
		Static:        normalizeRuntimeJSONDynamicStaticBinding(input.Static),
		Sampling: RuntimeJSONDynamicSampling{
			CanonicalRoutes:              canonicalRouteIDs(),
			CacheModes:                   []string{"cold", "warm"},
			ProbeOverheadPilotsDiscarded: 2,
			ProbeEvidenceSamples:         1,
			ProductSamplesHaveEProbe:     false,
		},
	}
	for _, in := range input.Samples {
		sample := RuntimeJSONDynamicSample{
			ID:                  runtimeJSONDynamicSampleID(in.Lane, in.RouteID, in.CacheMode, in.SampleIndex),
			Lane:                strings.TrimSpace(in.Lane),
			RouteID:             strings.TrimSpace(in.RouteID),
			CacheMode:           strings.TrimSpace(in.CacheMode),
			SampleIndex:         in.SampleIndex,
			DurationMs:          in.DurationMs,
			Pilot:               in.Pilot,
			Discarded:           in.Discarded,
			Drain:               in.Drain,
			ProductPathPrefixes: uniqueStrings(in.ProductPathPrefixes),
			HarnessPathPrefixes: uniqueStrings(in.HarnessPathPrefixes),
			ProbePathPrefixes:   uniqueStrings(in.ProbePathPrefixes),
		}
		manifest.Samples = append(manifest.Samples, sample)
		if sample.Drain == nil {
			continue
		}
		for _, event := range sample.Drain.Events {
			manifest.Events = append(manifest.Events, normalizeRuntimeJSONDynamicEvent(sample, event))
		}
	}
	manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
	manifest.OverheadPairs = buildRuntimeJSONDynamicOverheadPairs(manifest.Samples)
	if err := ValidateRuntimeJSONDynamicEvidenceManifest(manifest); err != nil {
		manifest.Validation = RuntimeJSONDynamicValidationSummary{Status: "fail", Errors: []string{err.Error()}}
		return manifest, err
	}
	manifest.Validation = RuntimeJSONDynamicValidationSummary{Status: "pass"}
	return manifest, nil
}

func ValidateRuntimeJSONDynamicEvidenceManifest(manifest *RuntimeJSONDynamicEvidenceManifest) error {
	var errs []string
	if manifest == nil {
		return fmt.Errorf("dynamic evidence manifest is nil")
	}
	if manifest.SchemaVersion != RuntimeJSONDynamicEvidenceSchemaVersion {
		errs = append(errs, fmt.Sprintf("schemaVersion = %q, want %q", manifest.SchemaVersion, RuntimeJSONDynamicEvidenceSchemaVersion))
	}
	if manifest.Contract != RuntimeJSONDynamicEvidenceContractVersion {
		errs = append(errs, "bad dynamic evidence contractVersion")
	}
	errs = append(errs, validateRuntimeJSONDynamicBindings(manifest.Source, manifest.Static)...)
	samplesByID := map[string]RuntimeJSONDynamicSample{}
	probeByBucket := map[string][]RuntimeJSONDynamicSample{}
	probeOverheadByBucket := map[string][]RuntimeJSONDynamicSample{}
	productPilots := map[string]RuntimeJSONDynamicSample{}
	probeOverheadPilots := map[string]RuntimeJSONDynamicSample{}
	for _, sample := range manifest.Samples {
		if sample.ID == "" {
			errs = append(errs, "sample has empty id")
			continue
		}
		if _, exists := samplesByID[sample.ID]; exists {
			errs = append(errs, "duplicate sample id: "+sample.ID)
		}
		samplesByID[sample.ID] = sample
		if !canonicalRouteIDSet()[sample.RouteID] {
			errs = append(errs, "sample has unknown route: "+sample.RouteID)
		}
		if sample.CacheMode != "cold" && sample.CacheMode != "warm" {
			errs = append(errs, sample.ID+": unknown cache mode")
		}
		switch sample.Lane {
		case RuntimeJSONDynamicLaneProbe:
			probeByBucket[runtimeJSONDynamicBucketKey(sample.RouteID, sample.CacheMode)] = append(probeByBucket[runtimeJSONDynamicBucketKey(sample.RouteID, sample.CacheMode)], sample)
			if sample.Pilot || sample.Discarded {
				errs = append(errs, sample.ID+": kept probe sample must not be pilot or discarded")
			}
			errs = append(errs, validateRuntimeJSONDynamicProbeSample(sample, manifest.Static)...)
		case RuntimeJSONDynamicLaneProbeOverhead:
			probeOverheadByBucket[runtimeJSONDynamicBucketKey(sample.RouteID, sample.CacheMode)] = append(probeOverheadByBucket[runtimeJSONDynamicBucketKey(sample.RouteID, sample.CacheMode)], sample)
			if !(sample.Pilot && sample.Discarded) {
				errs = append(errs, sample.ID+": probe-overhead sample must be a discarded pilot")
			} else {
				probeOverheadPilots[runtimeJSONDynamicPilotKey(sample.RouteID, sample.CacheMode, sample.SampleIndex)] = sample
			}
			errs = append(errs, validateRuntimeJSONDynamicProbeSample(sample, manifest.Static)...)
		case RuntimeJSONDynamicLaneProduct:
			if sample.Drain != nil {
				errs = append(errs, sample.ID+": product performance sample must not carry full E probe drain")
			}
			if sample.Pilot || sample.Discarded {
				productPilots[runtimeJSONDynamicPilotKey(sample.RouteID, sample.CacheMode, sample.SampleIndex)] = sample
			}
		default:
			errs = append(errs, sample.ID+": unknown lane")
		}
	}
	for _, routeID := range canonicalRouteIDs() {
		for _, cacheMode := range []string{"cold", "warm"} {
			key := runtimeJSONDynamicBucketKey(routeID, cacheMode)
			probes := probeByBucket[key]
			overheads := probeOverheadByBucket[key]
			probeIndexes := map[int]bool{}
			for _, sample := range probes {
				if probeIndexes[sample.SampleIndex] {
					errs = append(errs, key+": duplicate probe sample index")
				}
				probeIndexes[sample.SampleIndex] = true
			}
			overheadIndexes := map[int]bool{}
			for _, sample := range overheads {
				if overheadIndexes[sample.SampleIndex] {
					errs = append(errs, key+": duplicate probe-overhead sample index")
				}
				overheadIndexes[sample.SampleIndex] = true
			}
			if len(probes) != 1 || len(overheads) != 2 {
				errs = append(errs, fmt.Sprintf("%s: probe samples = %d probe-overhead pilots = %d, want 1/2", key, len(probes), len(overheads)))
			}
		}
	}
	errs = append(errs, validateRuntimeJSONDynamicRawEventCompleteness(manifest.Samples, manifest.Events)...)
	errs = append(errs, validateRuntimeJSONDynamicEvents(manifest.Events, samplesByID, manifest.Static)...)
	errs = append(errs, validateRuntimeJSONDynamicMatrix(manifest.Matrix, manifest.Samples, manifest.Events)...)
	errs = append(errs, validateRuntimeJSONDynamicProductCoverage(manifest.Events)...)
	errs = append(errs, validateRuntimeJSONDynamicR05(manifest.Samples, manifest.Events)...)
	errs = append(errs, validateRuntimeJSONDynamicOverheadPairs(manifest.OverheadPairs, samplesByID, productPilots, probeOverheadPilots)...)
	if len(errs) > 0 {
		return errors.New(strings.Join(uniqueStrings(errs), "; "))
	}
	return nil
}

func WriteRuntimeJSONDynamicEvidenceManifest(path string, manifest *RuntimeJSONDynamicEvidenceManifest) error {
	if err := ValidateRuntimeJSONDynamicEvidenceManifest(manifest); err != nil {
		return err
	}
	return WriteJSONFile(path, manifest)
}

func ReadRuntimeJSONDynamicEvidenceManifest(path string) (*RuntimeJSONDynamicEvidenceManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return DecodeRuntimeJSONDynamicEvidenceManifestStrict(f)
}

func DecodeRuntimeJSONDynamicEvidenceManifestStrict(r io.Reader) (*RuntimeJSONDynamicEvidenceManifest, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var manifest RuntimeJSONDynamicEvidenceManifest
	if err := dec.Decode(&manifest); err != nil {
		return nil, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("dynamic evidence manifest has trailing JSON")
	}
	if err := ValidateRuntimeJSONDynamicEvidenceManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func normalizeRuntimeJSONDynamicEvent(sample RuntimeJSONDynamicSample, event ProbeEvent) RuntimeJSONDynamicEvent {
	source := RuntimeJSONDynamicSourceFromDetail(event.Detail)
	out := RuntimeJSONDynamicEvent{
		SampleID:     sample.ID,
		Lane:         sample.Lane,
		RouteID:      sample.RouteID,
		CacheMode:    sample.CacheMode,
		SampleIndex:  sample.SampleIndex,
		Kind:         strings.TrimSpace(event.Kind),
		Name:         strings.TrimSpace(event.Name),
		Phase:        strings.TrimSpace(event.Phase),
		Source:       source,
		SourceClass:  classifyRuntimeJSONDynamicEvidenceSource(source, sample.ProductPathPrefixes, sample.HarnessPathPrefixes, sample.ProbePathPrefixes),
		StackHash:    stringFromRuntimeJSONDynamicAny(event.Detail["stackHash"]),
		ArgCount:     intFromRuntimeJSONDynamicAny(event.Detail["argCount"]),
		PayloadBytes: intFromRuntimeJSONDynamicAny(event.Detail["payloadBytes"]),
		ResultBytes:  intFromRuntimeJSONDynamicAny(event.Detail["resultBytes"]),
		Exception:    stringFromRuntimeJSONDynamicAny(event.Detail["exception"]),
		EventKind:    intFromRuntimeJSONDynamicAny(event.Detail["eventKind"]),
	}
	if argBytes, ok := event.Detail["argBytes"].([]any); ok {
		for _, value := range argBytes {
			out.ArgBytes = append(out.ArgBytes, intFromRuntimeJSONDynamicAny(value))
		}
	}
	if routeID := stringFromRuntimeJSONDynamicAny(event.Detail["routeID"]); routeID != "" {
		out.RouteID = routeID
	}
	if out.Phase == "" {
		out.Phase = "unknown"
	}
	out.HotPath = runtimeJSONHotPhases[out.Phase]
	out.IncludeInProductCounts = out.Lane == RuntimeJSONDynamicLaneProbe && !sample.Pilot && !sample.Discarded && out.SourceClass == RuntimeJSONDynamicSourceProduct && (out.Kind == "runtime-call" || out.Kind == "json-call")
	return out
}

func buildRuntimeJSONDynamicMatrix(samples []RuntimeJSONDynamicSample, events []RuntimeJSONDynamicEvent) []RuntimeJSONDynamicMatrixRow {
	rows := map[string]*RuntimeJSONDynamicMatrixRow{}
	for _, routeID := range canonicalRouteIDs() {
		for _, cacheMode := range []string{"cold", "warm"} {
			rows[runtimeJSONDynamicBucketKey(routeID, cacheMode)] = &RuntimeJSONDynamicMatrixRow{RouteID: routeID, CacheMode: cacheMode, ObservedZeroProduct: true}
		}
	}
	for _, sample := range samples {
		row := rows[runtimeJSONDynamicBucketKey(sample.RouteID, sample.CacheMode)]
		if row == nil {
			continue
		}
		if sample.Lane == RuntimeJSONDynamicLaneProbe && !sample.Pilot && !sample.Discarded {
			row.ProbeEvidenceCount++
		}
		if sample.Lane == RuntimeJSONDynamicLaneProbeOverhead && sample.Pilot && sample.Discarded {
			row.ProbeOverheadPilotCount++
		}
	}
	for _, event := range events {
		row := rows[runtimeJSONDynamicBucketKey(event.RouteID, event.CacheMode)]
		if row == nil {
			continue
		}
		if event.IncludeInProductCounts {
			row.ProductEventCount++
			row.ObservedZeroProduct = false
		}
		if event.HotPath && event.SourceClass == RuntimeJSONDynamicSourceUnknown {
			row.HotUnknownEventCount++
		}
	}
	out := make([]RuntimeJSONDynamicMatrixRow, 0, len(rows))
	for _, routeID := range canonicalRouteIDs() {
		for _, cacheMode := range []string{"cold", "warm"} {
			out = append(out, *rows[runtimeJSONDynamicBucketKey(routeID, cacheMode)])
		}
	}
	return out
}

func buildRuntimeJSONDynamicOverheadPairs(samples []RuntimeJSONDynamicSample) []RuntimeJSONDynamicOverheadPair {
	products := map[string]RuntimeJSONDynamicSample{}
	probes := map[string]RuntimeJSONDynamicSample{}
	for _, sample := range samples {
		if !(sample.Pilot && sample.Discarded) {
			continue
		}
		key := runtimeJSONDynamicPilotKey(sample.RouteID, sample.CacheMode, sample.SampleIndex)
		switch sample.Lane {
		case RuntimeJSONDynamicLaneProduct:
			products[key] = sample
		case RuntimeJSONDynamicLaneProbeOverhead:
			probes[key] = sample
		}
	}
	keys := make([]string, 0, len(products))
	for key := range products {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var pairs []RuntimeJSONDynamicOverheadPair
	for _, key := range keys {
		product := products[key]
		probe, ok := probes[key]
		if !ok {
			continue
		}
		pairs = append(pairs, RuntimeJSONDynamicOverheadPair{
			RouteID:           product.RouteID,
			CacheMode:         product.CacheMode,
			SampleIndex:       product.SampleIndex,
			ProductSampleID:   product.ID,
			ProbeSampleID:     probe.ID,
			ProductDurationMs: product.DurationMs,
			ProbeDurationMs:   probe.DurationMs,
			OverheadMs:        probe.DurationMs - product.DurationMs,
			Informational:     true,
		})
	}
	return pairs
}

func validateRuntimeJSONDynamicBindings(source RuntimeJSONDynamicSourceBinding, static RuntimeJSONDynamicStaticBinding) []string {
	var errs []string
	for name, value := range map[string]string{
		"source.baseRevision":                source.BaseRevision,
		"source.overlayHash":                 source.OverlayHash,
		"source.trackedDiffHash":             source.TrackedDiffHash,
		"source.untrackedIncludedSourceHash": source.UntrackedIncludedSourceHash,
		"source.inventorySha256":             source.InventorySHA256,
		"static.sourceIdentityHash":          static.SourceIdentityHash,
		"static.semanticHash":                static.SemanticHash,
		"static.countsHash":                  static.CountsHash,
		"static.globalNameHash":              static.GlobalNameHash,
		"static.scannerVersion":              static.ScannerVersion,
		"static.phaseClassifier":             static.PhaseClassifier,
	} {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, name+" is empty")
		}
	}
	if runtimeJSONDynamicSourceBindingHash(source) != static.SourceIdentityHash {
		errs = append(errs, "source binding does not match static sourceIdentityHash")
	}
	if RuntimeJSONStaticGlobalNameHash(static.KnownGlobals) != static.GlobalNameHash {
		errs = append(errs, "static known global set does not match globalNameHash")
	}
	return errs
}

func validateRuntimeJSONDynamicProbeSample(sample RuntimeJSONDynamicSample, static RuntimeJSONDynamicStaticBinding) []string {
	if sample.Drain == nil {
		return []string{sample.ID + ": probe sample missing raw drain"}
	}
	drain := sample.Drain
	var errs []string
	if drain.SchemaVersion != RuntimeJSONProbeSchemaVersion {
		errs = append(errs, sample.ID+": bad drain schemaVersion")
	}
	if drain.FacadeSchemaVersion != 1 || drain.Version != "1" {
		errs = append(errs, sample.ID+": bad drain facade/version")
	}
	if drain.RouteID != "" && drain.RouteID != sample.RouteID {
		errs = append(errs, sample.ID+": drain routeID mismatch")
	}
	if drain.DroppedCount != 0 {
		errs = append(errs, sample.ID+": droppedCount must be zero")
	}
	if len(drain.UnwrappedGlobals) != 0 {
		errs = append(errs, sample.ID+": unwrapped runtime globals must be empty")
	}
	if !sameStringSlice(uniqueStrings(drain.KnownGlobals), uniqueStrings(static.KnownGlobals)) {
		errs = append(errs, sample.ID+": drain knownGlobals do not match static known global set")
	}
	if hasDuplicateString(drain.WrappedGlobals) {
		errs = append(errs, sample.ID+": wrappedGlobals contains duplicates")
	}
	staticKnown := stringSet(static.KnownGlobals)
	for _, name := range drain.WrappedGlobals {
		if !staticKnown[name] {
			errs = append(errs, sample.ID+": wrappedGlobals contains name outside static known global set: "+name)
		}
	}
	return errs
}

func validateRuntimeJSONDynamicRawEventCompleteness(samples []RuntimeJSONDynamicSample, events []RuntimeJSONDynamicEvent) []string {
	eventsBySample := map[string][]RuntimeJSONDynamicEvent{}
	for _, event := range events {
		eventsBySample[event.SampleID] = append(eventsBySample[event.SampleID], event)
	}
	var errs []string
	for _, sample := range samples {
		if sample.Drain == nil {
			if len(eventsBySample[sample.ID]) != 0 {
				errs = append(errs, sample.ID+": product sample has normalized events without raw drain")
			}
			continue
		}
		got := eventsBySample[sample.ID]
		if len(got) != len(sample.Drain.Events) {
			errs = append(errs, fmt.Sprintf("%s: normalized event count = %d raw drain event count = %d", sample.ID, len(got), len(sample.Drain.Events)))
			continue
		}
		for i, raw := range sample.Drain.Events {
			want := normalizeRuntimeJSONDynamicEvent(sample, raw)
			if !reflect.DeepEqual(got[i], want) {
				errs = append(errs, fmt.Sprintf("%s: normalized event %d does not match raw drain event", sample.ID, i))
			}
		}
	}
	return errs
}

func validateRuntimeJSONDynamicEvents(events []RuntimeJSONDynamicEvent, samples map[string]RuntimeJSONDynamicSample, static RuntimeJSONDynamicStaticBinding) []string {
	var errs []string
	known := map[string]bool{}
	for _, name := range static.KnownGlobals {
		known[name] = true
	}
	for _, event := range events {
		sample, ok := samples[event.SampleID]
		if !ok {
			errs = append(errs, event.SampleID+": event references missing sample")
			continue
		}
		if event.RouteID != sample.RouteID || event.CacheMode != sample.CacheMode || event.SampleIndex != sample.SampleIndex || event.Lane != sample.Lane {
			errs = append(errs, event.SampleID+": event sample binding mismatch")
		}
		expectedSourceClass := classifyRuntimeJSONDynamicEvidenceSource(event.Source, sample.ProductPathPrefixes, sample.HarnessPathPrefixes, sample.ProbePathPrefixes)
		if event.SourceClass != expectedSourceClass {
			errs = append(errs, event.SampleID+": event source classification does not match source paths and prefixes")
		}
		expectedHotPath := runtimeJSONHotPhases[event.Phase]
		if event.HotPath != expectedHotPath {
			errs = append(errs, event.SampleID+": event hotPath does not match phase")
		}
		if expectedHotPath && expectedSourceClass == RuntimeJSONDynamicSourceUnknown {
			errs = append(errs, event.SampleID+": hot event has unknown source")
		}
		if event.Kind == "runtime-call" && !known[event.Name] {
			errs = append(errs, event.SampleID+": runtime-call name outside static known global set: "+event.Name)
		}
		if event.Kind == "runtime-call" && sample.Drain != nil && !stringSet(sample.Drain.WrappedGlobals)[event.Name] {
			errs = append(errs, event.SampleID+": runtime-call name outside wrapped runtime surface: "+event.Name)
		}
		expectedProductCount := sample.Lane == RuntimeJSONDynamicLaneProbe && !sample.Pilot && !sample.Discarded && expectedSourceClass == RuntimeJSONDynamicSourceProduct && (event.Kind == "runtime-call" || event.Kind == "json-call")
		if event.IncludeInProductCounts != expectedProductCount {
			errs = append(errs, event.SampleID+": event product count inclusion does not match lane/source rules")
		}
		if event.Kind == "json-call" && expectedSourceClass != RuntimeJSONDynamicSourceProduct && event.IncludeInProductCounts {
			errs = append(errs, event.SampleID+": JSON event outside product source entered product counts")
		}
	}
	return errs
}

func validateRuntimeJSONDynamicMatrix(matrix []RuntimeJSONDynamicMatrixRow, samples []RuntimeJSONDynamicSample, events []RuntimeJSONDynamicEvent) []string {
	want := buildRuntimeJSONDynamicMatrix(samples, events)
	var errs []string
	if len(matrix) != len(want) {
		errs = append(errs, fmt.Sprintf("matrix row count = %d, want %d", len(matrix), len(want)))
	}
	seen := map[string]bool{}
	for _, row := range matrix {
		key := runtimeJSONDynamicBucketKey(row.RouteID, row.CacheMode)
		if seen[key] {
			errs = append(errs, key+": duplicate matrix row")
		}
		seen[key] = true
	}
	if !reflect.DeepEqual(matrix, want) {
		errs = append(errs, "matrix rows do not match recomputed dynamic evidence matrix")
	}
	return errs
}

func validateRuntimeJSONDynamicProductCoverage(events []RuntimeJSONDynamicEvent) []string {
	required := []string{"R02", "R03", "R06", "R08", "R09A", "R09B", "R10"}
	seen := map[string]bool{}
	for _, event := range events {
		if event.IncludeInProductCounts {
			seen[event.RouteID] = true
		}
	}
	var errs []string
	for _, routeID := range required {
		if !seen[routeID] {
			errs = append(errs, "missing product dynamic coverage on "+routeID)
		}
	}
	return errs
}

func validateRuntimeJSONDynamicR05(samples []RuntimeJSONDynamicSample, events []RuntimeJSONDynamicEvent) []string {
	eventsBySample := map[string][]RuntimeJSONDynamicEvent{}
	for _, event := range events {
		eventsBySample[event.SampleID] = append(eventsBySample[event.SampleID], event)
	}
	var errs []string
	for _, sample := range samples {
		if sample.Lane != RuntimeJSONDynamicLaneProbe || sample.RouteID != "R05" || sample.Pilot || sample.Discarded {
			continue
		}
		count := 0
		for _, event := range eventsBySample[sample.ID] {
			if event.IncludeInProductCounts && event.Kind == "runtime-call" && event.Name == "__gosx_canvas_event" {
				count++
				if event.EventKind != 3 {
					errs = append(errs, sample.ID+": R05 __gosx_canvas_event eventKind must be 3")
				}
				if event.ArgCount < 3 {
					errs = append(errs, sample.ID+": R05 __gosx_canvas_event argCount must be >= 3")
				}
				if event.Exception != "" {
					errs = append(errs, sample.ID+": R05 __gosx_canvas_event must not throw")
				}
			}
		}
		if count != 1 {
			errs = append(errs, fmt.Sprintf("%s: R05 kept probe sample has %d product __gosx_canvas_event calls, want 1", sample.ID, count))
		}
	}
	return errs
}

func validateRuntimeJSONDynamicOverheadPairs(pairs []RuntimeJSONDynamicOverheadPair, samples map[string]RuntimeJSONDynamicSample, productPilots, probeOverheadPilots map[string]RuntimeJSONDynamicSample) []string {
	var errs []string
	seenProductPairs := map[string]bool{}
	seenProbePairs := map[string]bool{}
	if !sameStringSetKeys(productPilots, probeOverheadPilots) {
		errs = append(errs, "product pilot and probe-overhead pilot key sets differ")
	}
	for _, pair := range pairs {
		product, ok := samples[pair.ProductSampleID]
		if !ok || product.Lane != RuntimeJSONDynamicLaneProduct || !product.Pilot || !product.Discarded {
			errs = append(errs, "overhead pair references invalid product pilot: "+pair.ProductSampleID)
			continue
		}
		probe, ok := samples[pair.ProbeSampleID]
		if !ok || probe.Lane != RuntimeJSONDynamicLaneProbeOverhead || !probe.Pilot || !probe.Discarded {
			errs = append(errs, "overhead pair references invalid probe pilot: "+pair.ProbeSampleID)
			continue
		}
		if product.RouteID != probe.RouteID || product.CacheMode != probe.CacheMode || product.SampleIndex != probe.SampleIndex {
			errs = append(errs, "overhead pair route/cache/index mismatch")
		}
		expected := RuntimeJSONDynamicOverheadPair{
			RouteID:           product.RouteID,
			CacheMode:         product.CacheMode,
			SampleIndex:       product.SampleIndex,
			ProductSampleID:   product.ID,
			ProbeSampleID:     probe.ID,
			ProductDurationMs: product.DurationMs,
			ProbeDurationMs:   probe.DurationMs,
			OverheadMs:        probe.DurationMs - product.DurationMs,
			Informational:     true,
		}
		if pair != expected {
			errs = append(errs, "overhead pair does not match referenced product/probe samples")
		}
		key := runtimeJSONDynamicPilotKey(product.RouteID, product.CacheMode, product.SampleIndex)
		probeKey := runtimeJSONDynamicPilotKey(probe.RouteID, probe.CacheMode, probe.SampleIndex)
		if key != probeKey {
			errs = append(errs, "overhead pair key mismatch")
		}
		if seenProductPairs[key] {
			errs = append(errs, "duplicate product/probe overhead pair: "+key)
		}
		if seenProbePairs[probeKey] {
			errs = append(errs, "duplicate probe-overhead pair: "+probeKey)
		}
		seenProductPairs[key] = true
		seenProbePairs[probeKey] = true
	}
	for key := range productPilots {
		if !seenProductPairs[key] {
			errs = append(errs, "unpaired product/probe overhead pilot: "+key)
		}
	}
	for key := range probeOverheadPilots {
		if !seenProbePairs[key] {
			errs = append(errs, "unpaired probe-overhead/product pilot: "+key)
		}
	}
	return errs
}

func normalizeRuntimeJSONDynamicStaticBinding(static RuntimeJSONDynamicStaticBinding) RuntimeJSONDynamicStaticBinding {
	static.KnownGlobals = uniqueStrings(static.KnownGlobals)
	return static
}

const RuntimeJSONDynamicSourceProbe RuntimeJSONDynamicSourceClass = "probe"

func classifyRuntimeJSONDynamicEvidenceSource(source RuntimeJSONDynamicSource, productPaths, harnessPathPrefixes, probePathPrefixes []string) RuntimeJSONDynamicSourceClass {
	path := strings.TrimSpace(source.Path)
	if path == "" {
		return RuntimeJSONDynamicSourceUnknown
	}
	matches := []RuntimeJSONDynamicSourceClass{}
	if runtimeJSONDynamicSourceMatchesExactPath(path, productPaths) {
		matches = append(matches, RuntimeJSONDynamicSourceProduct)
	}
	if runtimeJSONDynamicSourceMatchesPrefix(path, harnessPathPrefixes) {
		matches = append(matches, RuntimeJSONDynamicSourceHarness)
	}
	if runtimeJSONDynamicSourceMatchesPrefix(path, probePathPrefixes) {
		matches = append(matches, RuntimeJSONDynamicSourceProbe)
	}
	if len(matches) != 1 {
		return RuntimeJSONDynamicSourceUnknown
	}
	return matches[0]
}

func runtimeJSONDynamicSourceMatchesExactPath(path string, paths []string) bool {
	for _, allowed := range paths {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if path == allowed {
			return true
		}
	}
	return false
}

func runtimeJSONDynamicSampleID(lane, routeID, cacheMode string, index int) string {
	return strings.TrimSpace(lane) + ":" + strings.TrimSpace(routeID) + ":" + strings.TrimSpace(cacheMode) + fmt.Sprintf(":%03d", index)
}

func runtimeJSONDynamicBucketKey(routeID, cacheMode string) string {
	return routeID + "/" + cacheMode
}

func runtimeJSONDynamicPilotKey(routeID, cacheMode string, index int) string {
	return runtimeJSONDynamicBucketKey(routeID, cacheMode) + fmt.Sprintf("/%03d", index)
}

func runtimeJSONDynamicSourceBindingHash(source RuntimeJSONDynamicSourceBinding) string {
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

func sameStringSetKeys[A, B any](a map[string]A, b map[string]B) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

func hasDuplicateString(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
