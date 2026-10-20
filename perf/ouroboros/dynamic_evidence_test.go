package ouroboros

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRuntimeJSONDynamicEvidenceManifestValidatesCanonicalProbeLane(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	if manifest.Validation.Status != "pass" {
		t.Fatalf("validation status = %q", manifest.Validation.Status)
	}
	if len(manifest.Matrix) != len(canonicalRouteIDs())*2 {
		t.Fatalf("matrix rows = %d", len(manifest.Matrix))
	}
	if len(manifest.OverheadPairs) != len(canonicalRouteIDs())*2*2 {
		t.Fatalf("overhead pairs = %d", len(manifest.OverheadPairs))
	}
	assertValidRuntimeJSONDynamicEvidence(t, manifest)
	for _, row := range manifest.Matrix {
		if row.ProbeOverheadPilotCount != 2 || row.ProbeEvidenceCount != 1 {
			t.Fatalf("bad matrix row: %+v", row)
		}
		if row.ProductEventCount == 0 && !row.ObservedZeroProduct {
			t.Fatalf("zero row did not prove observed zero: %+v", row)
		}
	}
}

func TestRuntimeJSONDynamicEvidenceRejectsMissingBucket(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Samples = filterDynamicSamples(manifest.Samples, func(s RuntimeJSONDynamicSample) bool {
		return !(s.RouteID == "R02" && s.CacheMode == "warm" && (s.Lane == RuntimeJSONDynamicLaneProbe || s.Lane == RuntimeJSONDynamicLaneProbeOverhead))
	})
	manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "R02/warm: probe samples")
}

func TestRuntimeJSONDynamicEvidenceRejectsExtraSample(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	extra := firstKeptProbeSample(*manifest, "R00", "cold")
	extra.ID = runtimeJSONDynamicSampleID(extra.Lane, extra.RouteID, extra.CacheMode, 9)
	extra.SampleIndex = 9
	manifest.Samples = append(manifest.Samples, extra)
	manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "R00/cold: probe samples = 2 probe-overhead pilots = 2, want 1/2")
}

func TestRuntimeJSONDynamicEvidenceRejectsDrops(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	firstProbeSample(manifest, "R02", "cold").Drain.DroppedCount = 1
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "droppedCount must be zero")
}

func TestRuntimeJSONDynamicEvidenceAllowsR00ZeroWrappedSurface(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	for _, cacheMode := range []string{"cold", "warm"} {
		if got := firstProbeSample(manifest, "R00", cacheMode).Drain.WrappedGlobals; len(got) != 0 {
			t.Fatalf("R00/%s wrappedGlobals = %v, want empty", cacheMode, got)
		}
	}
	assertValidRuntimeJSONDynamicEvidence(t, manifest)
}

func TestRuntimeJSONDynamicEvidenceValidatesWrappedSurfaceSubsetAndRuntimeCalls(t *testing.T) {
	t.Run("subset positive", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		firstProbeSample(manifest, "R02", "cold").Drain.WrappedGlobals = []string{"__gosx_action"}
		assertValidRuntimeJSONDynamicEvidence(t, manifest)
	})
	t.Run("duplicate", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		firstProbeSample(manifest, "R02", "cold").Drain.WrappedGlobals = []string{"__gosx_action", "__gosx_action"}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "wrappedGlobals contains duplicates")
	})
	t.Run("outside static", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		firstProbeSample(manifest, "R02", "cold").Drain.WrappedGlobals = []string{"__gosx_action", "__gosx_not_static"}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "wrappedGlobals contains name outside static known global set")
	})
	t.Run("runtime outside wrapped", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		firstProbeSample(manifest, "R05", "cold").Drain.WrappedGlobals = []string{"__gosx_action"}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "runtime-call name outside wrapped runtime surface")
	})
	t.Run("unwrapped", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		firstProbeSample(manifest, "R02", "cold").Drain.UnwrappedGlobals = []string{"__gosx_canvas_event:non-configurable"}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "unwrapped runtime globals must be empty")
	})
}

func TestRuntimeJSONDynamicEvidenceRejectsSourceMismatch(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Source.OverlayHash = "sha256:other"
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "source binding does not match static sourceIdentityHash")
}

func TestRuntimeJSONDynamicEvidenceRejectsStaticMismatch(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Static.KnownGlobals = append(manifest.Static.KnownGlobals, "__gosx_new")
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "static known global set does not match globalNameHash")
}

func TestRuntimeJSONDynamicEvidenceRejectsUnknownHotJSON(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	sample := firstKeptProbeSample(*manifest, "R02", "cold")
	event := runtimeJSONDynamicJSONEvent(sample, "/unclassified/app.js", "input")
	event.SourceClass = RuntimeJSONDynamicSourceUnknown
	event.HotPath = true
	manifest.Events = append(manifest.Events, event)
	manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "hot event has unknown source")
}

func TestRuntimeJSONDynamicEvidenceRecomputesPersistedDerivedEventFields(t *testing.T) {
	t.Run("source class", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		for i := range manifest.Events {
			if manifest.Events[i].SourceClass == RuntimeJSONDynamicSourceProduct {
				manifest.Events[i].SourceClass = RuntimeJSONDynamicSourceHarness
				break
			}
		}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "event source classification does not match source paths and prefixes")
	})
	t.Run("hot path", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		for i := range manifest.Events {
			if manifest.Events[i].Phase == "input" {
				manifest.Events[i].HotPath = false
				break
			}
		}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "event hotPath does not match phase")
	})
	t.Run("product count", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		for i := range manifest.Events {
			if manifest.Events[i].IncludeInProductCounts {
				manifest.Events[i].IncludeInProductCounts = false
				break
			}
		}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "event product count inclusion does not match lane/source rules")
	})
}

func TestRuntimeJSONDynamicEvidenceRejectsRawDrainNormalizedEventMismatch(t *testing.T) {
	t.Run("missing normalized", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		sample := firstKeptProbeSample(*manifest, "R02", "cold")
		manifest.Events = filterDynamicEvents(manifest.Events, func(e RuntimeJSONDynamicEvent) bool {
			return e.SampleID != sample.ID
		})
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "normalized event count")
	})
	t.Run("mutated normalized", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		for i := range manifest.Events {
			if manifest.Events[i].Kind == "runtime-call" {
				manifest.Events[i].StackHash = "changed"
				break
			}
		}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "does not match raw drain event")
	})
}

func TestRuntimeJSONDynamicEvidenceExcludesHarnessJSONFromProductCounts(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	for _, event := range manifest.Events {
		if event.Kind == "json-call" && event.SourceClass == RuntimeJSONDynamicSourceHarness && event.IncludeInProductCounts {
			t.Fatalf("harness JSON entered product counts: %+v", event)
		}
	}
	assertValidRuntimeJSONDynamicEvidence(t, manifest)

	manifest.Events = append(manifest.Events, runtimeJSONDynamicJSONEvent(firstKeptProbeSample(*manifest, "R02", "cold"), "/__harness__/runner.js", "input"))
	last := &manifest.Events[len(manifest.Events)-1]
	last.SourceClass = RuntimeJSONDynamicSourceHarness
	last.IncludeInProductCounts = true
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "JSON event outside product source entered product counts")
}

func TestRuntimeJSONDynamicEvidenceRejectsRuntimeNameOutsideStaticSet(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Events = append(manifest.Events, runtimeJSONDynamicRuntimeEvent(firstKeptProbeSample(*manifest, "R02", "cold"), "__gosx_not_static", "/assets/app.js", "input"))
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "runtime-call name outside static known global set")
}

func TestRuntimeJSONDynamicEvidenceProductSourceRequiresExactPath(t *testing.T) {
	t.Run("exact positive", func(t *testing.T) {
		source := RuntimeJSONDynamicSource{Path: "/gosx/bootstrap.js"}
		got := classifyRuntimeJSONDynamicEvidenceSource(source, []string{"/gosx/bootstrap.js"}, []string{"/__harness__/"}, []string{"/__probe__/"})
		if got != RuntimeJSONDynamicSourceProduct {
			t.Fatalf("classify exact product path = %q, want %q", got, RuntimeJSONDynamicSourceProduct)
		}
	})
	t.Run("sibling negative", func(t *testing.T) {
		source := RuntimeJSONDynamicSource{Path: "/gosx/bootstrap.js.map"}
		got := classifyRuntimeJSONDynamicEvidenceSource(source, []string{"/gosx/bootstrap.js"}, []string{"/__harness__/"}, []string{"/__probe__/"})
		if got != RuntimeJSONDynamicSourceUnknown {
			t.Fatalf("classify product sibling path = %q, want %q", got, RuntimeJSONDynamicSourceUnknown)
		}
	})
	t.Run("manifest rejects sibling as product", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		sample := firstProbeSample(manifest, "R02", "cold")
		sample.ProductPathPrefixes = []string{"/gosx/bootstrap.js"}
		for i := range sample.Drain.Events {
			if sample.Drain.Events[i].Kind == "runtime-call" {
				source := sample.Drain.Events[i].Detail["source"].(map[string]any)
				source["path"] = "/gosx/bootstrap.js.map"
				break
			}
		}
		for i := range manifest.Events {
			if manifest.Events[i].SampleID == sample.ID && manifest.Events[i].Kind == "runtime-call" {
				manifest.Events[i] = normalizeRuntimeJSONDynamicEvent(*sample, sample.Drain.Events[2])
				break
			}
		}
		manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "hot event has unknown source")
	})
}

func TestRuntimeJSONDynamicEvidenceOverheadEventsCannotInflateProductCoverageOrCounts(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	overhead := firstProbeOverheadSamplePtr(manifest, "R00", "cold")
	raw := productRuntimeProbeEvent("R00", "__gosx_action", 0, 1)
	overhead.Drain.Events = append(overhead.Drain.Events, raw)
	overhead.Drain.WrappedGlobals = []string{"__gosx_action"}
	event := normalizeRuntimeJSONDynamicEvent(*overhead, raw)
	manifest.Events = append(manifest.Events, event)
	manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
	for _, row := range manifest.Matrix {
		if row.RouteID == "R00" && row.CacheMode == "cold" && row.ProductEventCount != 0 {
			t.Fatalf("overhead event inflated product count: %+v", row)
		}
	}
	assertValidRuntimeJSONDynamicEvidence(t, manifest)

	manifest.Events[len(manifest.Events)-1].IncludeInProductCounts = true
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "event product count inclusion does not match lane/source rules")
}

func TestRuntimeJSONDynamicEvidenceRejectsWrongLanePilotLabels(t *testing.T) {
	t.Run("probe discarded", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		sample := firstProbeSample(manifest, "R02", "cold")
		sample.Pilot = true
		sample.Discarded = true
		manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "kept probe sample must not be pilot or discarded")
	})
	t.Run("overhead kept", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		sample := firstProbeOverheadSamplePtr(manifest, "R02", "cold")
		sample.Pilot = false
		sample.Discarded = false
		manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "probe-overhead sample must be a discarded pilot")
	})
}

func TestRuntimeJSONDynamicEvidenceRequiresExactOneProbeTwoOverheadCounts(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Samples = filterDynamicSamples(manifest.Samples, func(s RuntimeJSONDynamicSample) bool {
		return !(s.Lane == RuntimeJSONDynamicLaneProbeOverhead && s.RouteID == "R03" && s.CacheMode == "cold" && s.SampleIndex == 1)
	})
	manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "R03/cold: probe samples = 1 probe-overhead pilots = 1, want 1/2")
}

func TestRuntimeJSONDynamicEvidenceRejectsR05MissingWrongAndDuplicateCanvasEvent(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		manifest.Events = filterDynamicEvents(manifest.Events, func(e RuntimeJSONDynamicEvent) bool {
			return !(e.RouteID == "R05" && e.CacheMode == "cold" && e.Name == "__gosx_canvas_event")
		})
		manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "R05 kept probe sample has 0")
	})
	t.Run("wrong", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		for i := range manifest.Events {
			if manifest.Events[i].RouteID == "R05" && manifest.Events[i].Name == "__gosx_canvas_event" {
				manifest.Events[i].EventKind = 2
				break
			}
		}
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "eventKind must be 3")
	})
	t.Run("duplicate", func(t *testing.T) {
		manifest := validRuntimeJSONDynamicEvidenceManifest(t)
		event := runtimeJSONDynamicRuntimeEvent(firstKeptProbeSample(*manifest, "R05", "cold"), "__gosx_canvas_event", "/assets/app.js", "input")
		event.EventKind = 3
		event.ArgCount = 3
		manifest.Events = append(manifest.Events, event)
		manifest.Matrix = buildRuntimeJSONDynamicMatrix(manifest.Samples, manifest.Events)
		assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "R05 kept probe sample has 2")
	})
}

func TestRuntimeJSONDynamicEvidenceRejectsUnpairedOverhead(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.OverheadPairs = manifest.OverheadPairs[:len(manifest.OverheadPairs)-1]
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "unpaired product/probe overhead pilot")
}

func TestRuntimeJSONDynamicEvidenceRejectsProbeOverheadWithoutProductPilot(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Samples = filterDynamicSamples(manifest.Samples, func(s RuntimeJSONDynamicSample) bool {
		return !(s.Lane == RuntimeJSONDynamicLaneProduct && s.RouteID == "R04" && s.CacheMode == "warm" && s.SampleIndex == 1)
	})
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "product pilot and probe-overhead pilot key sets differ")
}

func TestRuntimeJSONDynamicEvidenceRejectsDuplicateOverheadPair(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.OverheadPairs = append(manifest.OverheadPairs, manifest.OverheadPairs[0])
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "duplicate product/probe overhead pair")
}

func TestRuntimeJSONDynamicEvidenceRejectsTamperedOverheadPairFields(t *testing.T) {
	cases := []struct {
		name string
		edit func(*RuntimeJSONDynamicOverheadPair)
	}{
		{name: "route", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.RouteID = "R99" }},
		{name: "cache", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.CacheMode = "hot" }},
		{name: "index", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.SampleIndex = 99 }},
		{name: "product id", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.ProductSampleID = "missing" }},
		{name: "probe id", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.ProbeSampleID = "missing" }},
		{name: "product duration", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.ProductDurationMs++ }},
		{name: "probe duration", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.ProbeDurationMs++ }},
		{name: "delta", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.OverheadMs++ }},
		{name: "informational", edit: func(pair *RuntimeJSONDynamicOverheadPair) { pair.Informational = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validRuntimeJSONDynamicEvidenceManifest(t)
			tc.edit(&manifest.OverheadPairs[0])
			assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "overhead pair")
		})
	}
}

func TestRuntimeJSONDynamicEvidenceRejectsExtraMatrixRow(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Matrix = append(manifest.Matrix, RuntimeJSONDynamicMatrixRow{RouteID: "R99", CacheMode: "cold"})
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "matrix row count = 25, want 24")
}

func TestRuntimeJSONDynamicEvidenceRejectsAlteredMatrixCounts(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	manifest.Matrix[0].ProbeEvidenceCount++
	assertInvalidRuntimeJSONDynamicEvidence(t, manifest, "matrix rows do not match recomputed dynamic evidence matrix")
}

func TestRuntimeJSONDynamicEvidenceStrictJSONHelpers(t *testing.T) {
	manifest := validRuntimeJSONDynamicEvidenceManifest(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "dynamic.json")
	if err := WriteRuntimeJSONDynamicEvidenceManifest(path, manifest); err != nil {
		t.Fatalf("WriteRuntimeJSONDynamicEvidenceManifest: %v", err)
	}
	if _, err := ReadRuntimeJSONDynamicEvidenceManifest(path); err != nil {
		t.Fatalf("ReadRuntimeJSONDynamicEvidenceManifest: %v", err)
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := DecodeRuntimeJSONDynamicEvidenceManifestStrict(bytes.NewReader(append(body, []byte("\n{}")...))); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("strict decode accepted trailing JSON: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("Unmarshal map: %v", err)
	}
	raw["unknownField"] = true
	unknown, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal unknown: %v", err)
	}
	if _, err := DecodeRuntimeJSONDynamicEvidenceManifestStrict(bytes.NewReader(unknown)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("strict decode accepted unknown field: %v", err)
	}
}

func validRuntimeJSONDynamicEvidenceManifest(t *testing.T) *RuntimeJSONDynamicEvidenceManifest {
	t.Helper()
	source := RuntimeJSONDynamicSourceBinding{
		BaseRevision:                "abc123",
		OverlayHash:                 "sha256:overlay",
		TrackedDiffHash:             "sha256:tracked",
		UntrackedIncludedSourceHash: "sha256:untracked",
		InventorySHA256:             "sha256:inventory",
	}
	known := []string{"__gosx_action", "__gosx_canvas_event"}
	static := RuntimeJSONDynamicStaticBinding{
		SourceIdentityHash: runtimeJSONDynamicSourceBindingHash(source),
		SemanticHash:       "sha256:semantic",
		CountsHash:         "sha256:counts",
		GlobalNameHash:     RuntimeJSONStaticGlobalNameHash(known),
		ScannerVersion:     runtimeJSONStaticScannerVersion,
		PhaseClassifier:    runtimeJSONPhaseClassifierVersion,
		KnownGlobals:       known,
	}
	input := RuntimeJSONDynamicEvidenceInput{GeneratedAt: "2026-08-08T00:00:00Z", Source: source, Static: static}
	requiredProduct := map[string]bool{"R02": true, "R03": true, "R05": true, "R06": true, "R08": true, "R09A": true, "R09B": true, "R10": true}
	for _, routeID := range canonicalRouteIDs() {
		for _, cacheMode := range []string{"cold", "warm"} {
			for i := 0; i < 2; i++ {
				input.Samples = append(input.Samples,
					runtimeJSONDynamicInputSample(RuntimeJSONDynamicLaneProduct, routeID, cacheMode, i, true, nil),
					runtimeJSONDynamicInputSample(RuntimeJSONDynamicLaneProbeOverhead, routeID, cacheMode, i, true, []ProbeEvent{probeInstallEvent(routeID)}),
				)
			}
			var events []ProbeEvent
			events = append(events, probeInstallEvent(routeID))
			events = append(events, harnessJSONProbeEvent(routeID))
			if requiredProduct[routeID] {
				if routeID == "R05" {
					events = append(events, productRuntimeProbeEvent(routeID, "__gosx_canvas_event", 3, 3))
				} else {
					events = append(events, productRuntimeProbeEvent(routeID, "__gosx_action", 0, 1))
				}
			}
			input.Samples = append(input.Samples, runtimeJSONDynamicInputSample(RuntimeJSONDynamicLaneProbe, routeID, cacheMode, 0, false, events))
		}
	}
	manifest, err := BuildRuntimeJSONDynamicEvidence(input)
	if err != nil {
		t.Fatalf("BuildRuntimeJSONDynamicEvidence: %v", err)
	}
	return manifest
}

func runtimeJSONDynamicInputSample(lane, routeID, cacheMode string, index int, pilot bool, events []ProbeEvent) RuntimeJSONDynamicSampleInput {
	var drain *RuntimeJSONRawDrain
	if lane == RuntimeJSONDynamicLaneProbe || lane == RuntimeJSONDynamicLaneProbeOverhead {
		wrapped := wrappedGlobalsForProbeEvents(events)
		drain = &RuntimeJSONRawDrain{
			SchemaVersion:       RuntimeJSONProbeSchemaVersion,
			FacadeSchemaVersion: 1,
			Version:             "1",
			Phase:               "input",
			RouteID:             routeID,
			Events:              events,
			WrappedGlobals:      wrapped,
			KnownGlobals:        []string{"__gosx_action", "__gosx_canvas_event"},
			Limits:              RuntimeJSONRawDrainLimits{EventLimit: 8192},
		}
	}
	return RuntimeJSONDynamicSampleInput{
		Lane:                lane,
		RouteID:             routeID,
		CacheMode:           cacheMode,
		SampleIndex:         index,
		DurationMs:          float64(100 + index),
		Pilot:               pilot,
		Discarded:           pilot,
		ProductPathPrefixes: []string{"/assets/app.js"},
		HarnessPathPrefixes: []string{"/__harness__/"},
		ProbePathPrefixes:   []string{"/__probe__/"},
		Drain:               drain,
	}
}

func probeInstallEvent(routeID string) ProbeEvent {
	return ProbeEvent{Kind: "probe", Name: "install", Phase: "route-load", Detail: map[string]any{"routeID": routeID}}
}

func harnessJSONProbeEvent(routeID string) ProbeEvent {
	return ProbeEvent{Kind: "json-call", Name: "JSON.parse", Phase: "input", Detail: map[string]any{
		"routeID":      routeID,
		"payloadBytes": float64(12),
		"resultBytes":  float64(4),
		"stackHash":    "harness-stack",
		"source":       map[string]any{"path": "/__harness__/runner.js", "line": float64(1), "column": float64(1), "urlHash": "h1"},
	}}
}

func productRuntimeProbeEvent(routeID, name string, eventKind, argCount int) ProbeEvent {
	detail := map[string]any{
		"routeID":     routeID,
		"argCount":    float64(argCount),
		"argBytes":    []any{float64(1), float64(2), float64(3)},
		"resultBytes": float64(1),
		"stackHash":   "product-stack",
		"source":      map[string]any{"path": "/assets/app.js", "line": float64(2), "column": float64(3), "urlHash": "p1"},
	}
	if eventKind > 0 {
		detail["eventKind"] = float64(eventKind)
	}
	return ProbeEvent{Kind: "runtime-call", Name: name, Phase: "input", Detail: detail}
}

func wrappedGlobalsForProbeEvents(events []ProbeEvent) []string {
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

func runtimeJSONDynamicRuntimeEvent(sample RuntimeJSONDynamicSample, name, path, phase string) RuntimeJSONDynamicEvent {
	return RuntimeJSONDynamicEvent{
		SampleID:               sample.ID,
		Lane:                   sample.Lane,
		RouteID:                sample.RouteID,
		CacheMode:              sample.CacheMode,
		SampleIndex:            sample.SampleIndex,
		Kind:                   "runtime-call",
		Name:                   name,
		Phase:                  phase,
		Source:                 RuntimeJSONDynamicSource{Path: path, Line: 1, Column: 1},
		SourceClass:            RuntimeJSONDynamicSourceProduct,
		StackHash:              "stack",
		ArgCount:               1,
		ResultBytes:            1,
		HotPath:                runtimeJSONHotPhases[phase],
		IncludeInProductCounts: true,
	}
}

func runtimeJSONDynamicJSONEvent(sample RuntimeJSONDynamicSample, path, phase string) RuntimeJSONDynamicEvent {
	sourceClass := RuntimeJSONDynamicSourceProduct
	if strings.HasPrefix(path, "/__harness__/") {
		sourceClass = RuntimeJSONDynamicSourceHarness
	}
	return RuntimeJSONDynamicEvent{
		SampleID:               sample.ID,
		Lane:                   sample.Lane,
		RouteID:                sample.RouteID,
		CacheMode:              sample.CacheMode,
		SampleIndex:            sample.SampleIndex,
		Kind:                   "json-call",
		Name:                   "JSON.parse",
		Phase:                  phase,
		Source:                 RuntimeJSONDynamicSource{Path: path, Line: 1, Column: 1},
		SourceClass:            sourceClass,
		StackHash:              "stack",
		PayloadBytes:           1,
		ResultBytes:            1,
		HotPath:                runtimeJSONHotPhases[phase],
		IncludeInProductCounts: sourceClass == RuntimeJSONDynamicSourceProduct,
	}
}

func firstProbeSample(manifest *RuntimeJSONDynamicEvidenceManifest, routeID, cacheMode string) *RuntimeJSONDynamicSample {
	for i := range manifest.Samples {
		if manifest.Samples[i].Lane == RuntimeJSONDynamicLaneProbe && manifest.Samples[i].RouteID == routeID && manifest.Samples[i].CacheMode == cacheMode {
			return &manifest.Samples[i]
		}
	}
	return nil
}

func firstKeptProbeSample(manifest RuntimeJSONDynamicEvidenceManifest, routeID, cacheMode string) RuntimeJSONDynamicSample {
	for _, sample := range manifest.Samples {
		if sample.Lane == RuntimeJSONDynamicLaneProbe && sample.RouteID == routeID && sample.CacheMode == cacheMode && !sample.Pilot && !sample.Discarded {
			return sample
		}
	}
	return RuntimeJSONDynamicSample{}
}

func firstProbeOverheadSample(manifest RuntimeJSONDynamicEvidenceManifest, routeID, cacheMode string) RuntimeJSONDynamicSample {
	for _, sample := range manifest.Samples {
		if sample.Lane == RuntimeJSONDynamicLaneProbeOverhead && sample.RouteID == routeID && sample.CacheMode == cacheMode {
			return sample
		}
	}
	return RuntimeJSONDynamicSample{}
}

func firstProbeOverheadSamplePtr(manifest *RuntimeJSONDynamicEvidenceManifest, routeID, cacheMode string) *RuntimeJSONDynamicSample {
	for i := range manifest.Samples {
		if manifest.Samples[i].Lane == RuntimeJSONDynamicLaneProbeOverhead && manifest.Samples[i].RouteID == routeID && manifest.Samples[i].CacheMode == cacheMode {
			return &manifest.Samples[i]
		}
	}
	return nil
}

func filterDynamicSamples(samples []RuntimeJSONDynamicSample, keep func(RuntimeJSONDynamicSample) bool) []RuntimeJSONDynamicSample {
	var out []RuntimeJSONDynamicSample
	for _, sample := range samples {
		if keep(sample) {
			out = append(out, sample)
		}
	}
	return out
}

func filterDynamicEvents(events []RuntimeJSONDynamicEvent, keep func(RuntimeJSONDynamicEvent) bool) []RuntimeJSONDynamicEvent {
	var out []RuntimeJSONDynamicEvent
	for _, event := range events {
		if keep(event) {
			out = append(out, event)
		}
	}
	return out
}

func assertValidRuntimeJSONDynamicEvidence(t *testing.T, manifest *RuntimeJSONDynamicEvidenceManifest) {
	t.Helper()
	if err := ValidateRuntimeJSONDynamicEvidenceManifest(manifest); err != nil {
		t.Fatalf("ValidateRuntimeJSONDynamicEvidenceManifest: %v", err)
	}
}

func assertInvalidRuntimeJSONDynamicEvidence(t *testing.T, manifest *RuntimeJSONDynamicEvidenceManifest, want string) {
	t.Helper()
	err := ValidateRuntimeJSONDynamicEvidenceManifest(manifest)
	if err == nil {
		t.Fatalf("ValidateRuntimeJSONDynamicEvidenceManifest passed, want %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRuntimeJSONDynamicEvidenceManifest error = %v, want substring %q", err, want)
	}
}
