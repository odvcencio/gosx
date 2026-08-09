package ouroboros

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/perf"
)

type SourceIdentity struct {
	BaseRevision                 string                      `json:"baseRevision"`
	OverlayHash                  string                      `json:"overlayHash"`
	TrackedDiffHash              string                      `json:"trackedDiffHash"`
	UntrackedIncludedSourceHash  string                      `json:"untrackedIncludedSourceHash"`
	InventoryRef                 string                      `json:"inventoryRef"`
	InventorySHA256              string                      `json:"inventorySha256"`
	RejectsModuleCacheMismatch   bool                        `json:"rejectsModuleCacheMismatch"`
	CurrentOverlayVerified       bool                        `json:"currentOverlayVerified"`
	CurrentOverlayVerificationAt string                      `json:"currentOverlayVerificationAt"`
	StrictInventory              bool                        `json:"strictInventory"`
	ReconstructionProof          bool                        `json:"reconstructionProof"`
	Reconstruction               *ReconstructionEvidence     `json:"reconstruction,omitempty"`
	RuntimeProbeNameCount        int                         `json:"runtimeProbeNameCount"`
	RuntimeProbeNames            []string                    `json:"-"`
	RuntimeJSONStatic            *RuntimeJSONStaticIdentity  `json:"runtimeJSONStatic,omitempty"`
	CompatibilityAudit           *CompatibilityAuditIdentity `json:"compatibilityAudit,omitempty"`
}

type RuntimeJSONStaticIdentity struct {
	Ref                string                  `json:"ref"`
	SchemaVersion      string                  `json:"schemaVersion"`
	ScannerVersion     string                  `json:"scannerVersion"`
	QueryID            string                  `json:"queryID"`
	PhaseClassifier    string                  `json:"phaseClassifier"`
	SourceIdentityHash string                  `json:"sourceIdentityHash"`
	SemanticHash       string                  `json:"semanticHash"`
	CountsHash         string                  `json:"countsHash"`
	GlobalNameHash     string                  `json:"globalNameHash"`
	Validated          bool                    `json:"validated"`
	Counts             RuntimeJSONStaticCounts `json:"counts"`
}

type CompatibilityAuditIdentity struct {
	SchemaVersion                 string                         `json:"schemaVersion"`
	Status                        string                         `json:"status"`
	CanonicalAvailable            bool                           `json:"canonicalAvailable"`
	Receipt                       CompatibilityNameSetSummary    `json:"receipt"`
	Anchor                        CompatibilityNameSetSummary    `json:"anchor"`
	Current                       CompatibilityNameSetSummary    `json:"current"`
	Reconciliation                CompatibilityReconciliationRef `json:"reconciliation"`
	RuntimeJSONSourceIdentityHash string                         `json:"runtimeJSONSourceIdentityHash,omitempty"`
	RuntimeJSONSemanticHash       string                         `json:"runtimeJSONSemanticHash,omitempty"`
	RuntimeJSONCountsHash         string                         `json:"runtimeJSONCountsHash,omitempty"`
	RuntimeJSONGlobalNameHash     string                         `json:"runtimeJSONGlobalNameHash,omitempty"`
}

type CompatibilityNameSetSummary struct {
	Count        int    `json:"count"`
	NameSetHash  string `json:"nameSetHash"`
	EvidenceHash string `json:"evidenceHash"`
}

type CompatibilityReconciliationRef struct {
	RecoveredPreexistingCount int    `json:"recoveredPreexistingCount"`
	RecoveredPreexistingHash  string `json:"recoveredPreexistingHash"`
	MissingFromAnchorCount    int    `json:"missingFromAnchorCount"`
	MissingFromAnchorHash     string `json:"missingFromAnchorHash"`
	AddedSinceAnchorCount     int    `json:"addedSinceAnchorCount"`
	AddedSinceAnchorHash      string `json:"addedSinceAnchorHash"`
	RemovedSinceAnchorCount   int    `json:"removedSinceAnchorCount"`
	RemovedSinceAnchorHash    string `json:"removedSinceAnchorHash"`
}

type ProbeEvent struct {
	Kind      string         `json:"kind"`
	Phase     string         `json:"phase"`
	Name      string         `json:"name"`
	StartTime float64        `json:"startTime"`
	Detail    map[string]any `json:"detail,omitempty"`
}

func canonicalRouteIDs() []string {
	return []string{"R00", "R01", "R02", "R03", "R04", "R05", "R06", "R07", "R08", "R09A", "R09B", "R10"}
}

func canonicalRouteIDSet() map[string]bool {
	out := map[string]bool{}
	for _, id := range canonicalRouteIDs() {
		out[id] = true
	}
	return out
}

func WriteJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func isSubpath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil {
		if realA, err := filepath.EvalSymlinks(aa); err == nil {
			aa = realA
		}
	}
	if errB == nil {
		if realB, err := filepath.EvalSymlinks(bb); err == nil {
			bb = realB
		}
	}
	return errA == nil && errB == nil && aa == bb
}

func hashUntracked(src []UntrackedSourceHash) string {
	if len(src) == 0 {
		return "sha256:clean"
	}
	sort.Slice(src, func(i, j int) bool { return src[i].Path < src[j].Path })
	h := sha256.New()
	for _, s := range src {
		_, _ = io.WriteString(h, s.Path+"\x00"+s.Type+"\x00"+s.Mode+"\x00"+s.SHA256+"\n")
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func fileSHA256(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func relTo(root, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func InjectOuroborosProbe(d *perf.Driver) error {
	return injectPreloadScript(d, ouroborosProbeJS)
}

func injectPreloadScript(d *perf.Driver, script string) error {
	addScript := page.AddScriptToEvaluateOnNewDocument(script)
	return chromedp.Run(d.Context(), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := addScript.Do(ctx)
		return err
	}))
}

func numberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

const ouroborosProbeJS = `
(function(){
  "use strict";
  if (window.__gosxOuroborosProbe) return;
  var limit = 8192;
  function safeURL(value) {
    value = String(value || "");
    var cut = value.length;
    var q = value.indexOf("?");
    var h = value.indexOf("#");
    if (q >= 0 && q < cut) cut = q;
    if (h >= 0 && h < cut) cut = h;
    return value.slice(0, cut);
  }
  var probe = window.__gosxOuroborosProbe = {
    version: "1",
    phase: "cold-load",
    events: [],
    setPhase: function(phase) {
      probe.phase = String(phase || "unknown");
      probe.mark("phase", {phase: probe.phase});
    },
    record: function(kind, name, detail) {
      var entry = {
        kind: String(kind || "mark"),
        phase: String(probe.phase || "unknown"),
        name: String(name || ""),
        startTime: performance.now ? performance.now() : 0,
        detail: sanitize(detail)
      };
      probe.events.push(entry);
      if (probe.events.length > limit) {
        probe.events.splice(0, Math.floor(limit / 2));
      }
      return entry;
    },
    mark: function(name, detail) {
      return probe.record("mark", name, detail);
    }
  };
  function sanitize(value) {
    if (value == null) return null;
    try {
      return JSON.parse(JSON.stringify(value));
    } catch (_) {
      return {unserializable: true, type: typeof value};
    }
  }
  probe.mark("preload-installed", {url: safeURL(location.href)});
  window.addEventListener("DOMContentLoaded", function(){
    probe.setPhase("route-load");
    probe.record("navigation", "DOMContentLoaded", {url: safeURL(location.href)});
  }, {once:true});
  window.addEventListener("load", function(){
    probe.setPhase("route-load");
    probe.record("navigation", "load", {url: safeURL(location.href)});
  }, {once:true});
  document.addEventListener("gosx:ready", function(event){
    probe.setPhase("route-load");
    probe.record("ready", "gosx:ready", event && event.detail || null);
  }, {once:true});
  window.addEventListener("error", function(event){
    probe.record("error", "window.error", {
      message: event.message || "",
      filename: event.filename || "",
      lineno: event.lineno || 0,
      colno: event.colno || 0
    });
  });
  window.addEventListener("unhandledrejection", function(event){
    probe.record("error", "unhandledrejection", {
      reason: String(event.reason && event.reason.message || event.reason || "")
    });
  });
  try {
    var observer = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      for (var i = 0; i < entries.length; i++) {
        probe.record("longtask", entries[i].name || "longtask", {
          duration: entries[i].duration,
          startTime: entries[i].startTime
        });
      }
    });
    observer.observe({type: "longtask", buffered: true});
  } catch (_) {}
})();`
