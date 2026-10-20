//go:build browser

package ouroboros

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/perf"
)

type ProbeDrainForTest struct {
	Events []ProbeEvent `json:"events"`
}

func TestRuntimeJSONProbePreservesBrowserSemantics(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript([]string{"__gosx_late", "__gosx_async", "__gosx_throw", "__gosx_identity"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject probe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
try {
window.__gosx_late = function(value) { return {ok: value}; };
window.__gosx_identity = function(value) { return value; };
window.__gosx_async = function(value) { return Promise.resolve(value + 1); };
Object.defineProperty(window, "__gosx_throw", {value: function thrower(){ throw new TypeError("boom"); }, configurable: true});
var parsed = JSON.parse('{"value":7}');
var text = JSON.stringify(parsed);
var sameWrapper = window.__gosx_late === window.__gosx_late;
var result = window.__gosx_late("x").ok;
window.__gosx_late.extra = 4;
var extra = window.__gosx_late.extra;
var src = window.__gosx_throw.toString();
window.__gosx_late = function next(value) { return {ok:"next-" + value}; };
var reassigned = window.__gosx_late("x").ok;
window.__gosx_async(2).then(function(v){ window.__asyncResult = v; });
try { JSON.parse("{"); } catch (e) { window.__jsonThrowName = e.name; }
try { window.__gosx_throw(); } catch (e) { window.__gosxThrowName = e.name; }
var getterTouched = false;
var hostile = {};
Object.defineProperty(hostile, "secret", {get:function(){ getterTouched = true; throw new Error("getter touched"); }});
window.__gosx_late(hostile);
var pair = Proxy.revocable({value:1}, {});
pair.revoke();
var revokedResult = window.__gosx_identity(pair.proxy) === pair.proxy;
window.__gosxOuroborosProbe.record("probe", "hostile-public", hostile);
window.__semanticResult = {text:text, sameWrapper:sameWrapper, result:result, reassigned:reassigned, extra:extra, sourceHasName:src.indexOf("thrower") >= 0, getterTouched:getterTouched, revokedResult:revokedResult};
} catch (e) { window.__semanticError = String(e && e.stack || e); }
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if err := d.Evaluate(`new Promise(r => setTimeout(r, 100))`, nil); err != nil {
		t.Fatalf("wait async: %v", err)
	}
	var sem struct {
		Text          string `json:"text"`
		SameWrapper   bool   `json:"sameWrapper"`
		Result        string `json:"result"`
		Reassigned    string `json:"reassigned"`
		Extra         int    `json:"extra"`
		SourceHasName bool   `json:"sourceHasName"`
		GetterTouched bool   `json:"getterTouched"`
		RevokedResult bool   `json:"revokedResult"`
		AsyncResult   int    `json:"asyncResult"`
		JSONThrowName string `json:"jsonThrowName"`
		GosxThrowName string `json:"gosxThrowName"`
		SemanticError string `json:"semanticError"`
	}
	if err := d.Evaluate(`Object.assign({}, window.__semanticResult, {asyncResult: window.__asyncResult, jsonThrowName: window.__jsonThrowName, gosxThrowName: window.__gosxThrowName, semanticError: window.__semanticError || ""})`, &sem); err != nil {
		t.Fatalf("semantic query: %v", err)
	}
	if sem.SemanticError != "" {
		t.Fatalf("semantic page error: %s", sem.SemanticError)
	}
	if sem.Text != `{"value":7}` || !sem.SameWrapper || sem.Result != "x" || sem.Reassigned != "next-x" || sem.Extra != 4 || !sem.SourceHasName || sem.GetterTouched || !sem.RevokedResult || sem.AsyncResult != 3 || sem.JSONThrowName != "SyntaxError" || sem.GosxThrowName != "TypeError" {
		t.Fatalf("semantic result = %+v", sem)
	}
	var drained struct {
		SchemaVersion    string       `json:"schemaVersion"`
		DroppedCount     int          `json:"droppedCount"`
		WrappedGlobals   []string     `json:"wrappedGlobals"`
		UnwrappedGlobals []string     `json:"unwrappedGlobals"`
		Events           []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drained); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drained.SchemaVersion != RuntimeJSONProbeSchemaVersion {
		t.Fatalf("schema version = %q", drained.SchemaVersion)
	}
	counts := map[string]int{}
	for _, event := range drained.Events {
		counts[event.Kind]++
		if event.Phase == "" {
			t.Fatalf("event without explicit phase: %+v", event)
		}
		if source, ok := event.Detail["source"].(map[string]any); ok {
			if path, ok := source["path"].(string); ok && (strings.Contains(path, "?") || strings.Contains(path, "#")) {
				t.Fatalf("unsanitized source path in event: %+v", event)
			}
		}
	}
	if counts["json-call"] < 3 || counts["runtime-call"] < 3 {
		t.Fatalf("event counts = %+v events=%+v", counts, drained.Events)
	}
}

func TestRuntimeJSONProbeAttributesExternalSourceAndStableHash(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript([]string{"__gosx_product"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject probe: %v", err)
	}

	firstServer := newRuntimeJSONAttributionServerForTest(t, "build=one&secret=first")
	defer firstServer.Close()
	secondServer := newRuntimeJSONAttributionServerForTest(t, "build=two&secret=second")
	defer secondServer.Close()

	first := runRuntimeJSONAttributionPageForTest(t, d, firstServer.URL+"/page?nav=first#frag")
	second := runRuntimeJSONAttributionPageForTest(t, d, secondServer.URL+"/page?nav=second#frag")
	for label, event := range map[string]ProbeEvent{"first": first, "second": second} {
		source := RuntimeJSONDynamicSourceFromDetail(event.Detail)
		if source.Path != "/assets/product.js" || source.Line == 0 || source.Column == 0 || source.URLHash == "" {
			t.Fatalf("%s product source = %+v event=%+v", label, source, event)
		}
		if strings.Contains(source.Path, "?") || strings.Contains(source.Path, "#") {
			t.Fatalf("%s product source path was not sanitized: %+v", label, source)
		}
		if got := ClassifyRuntimeJSONDynamicSource(source, []string{"/assets/"}, []string{"/__cdp/"}); got != RuntimeJSONDynamicSourceProduct {
			t.Fatalf("%s product classification = %q source=%+v", label, got, source)
		}
		if stackHash, _ := event.Detail["stackHash"].(string); stackHash == "" {
			t.Fatalf("%s product event lacks stackHash: %+v", label, event)
		}
	}
	if first.Detail["stackHash"] != second.Detail["stackHash"] {
		t.Fatalf("stack hash varied across port/query: first=%+v second=%+v", first, second)
	}
	firstSource := RuntimeJSONDynamicSourceFromDetail(first.Detail)
	secondSource := RuntimeJSONDynamicSourceFromDetail(second.Detail)
	if firstSource != secondSource {
		t.Fatalf("sanitized source varied across port/query: first=%+v second=%+v", firstSource, secondSource)
	}

	if err := d.Evaluate(`JSON.parse('{"cdp":true}')`, nil); err != nil {
		t.Fatalf("CDP eval JSON.parse: %v", err)
	}
	var evalDrain struct {
		Events []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &evalDrain); err != nil {
		t.Fatalf("drain eval: %v", err)
	}
	evalEvent := findRuntimeJSONProbeEventForTest(t, evalDrain.Events, "json-call", "JSON.parse")
	evalSource := RuntimeJSONDynamicSourceFromDetail(evalEvent.Detail)
	if evalSource != (RuntimeJSONDynamicSource{}) {
		t.Fatalf("CDP eval source should remain unknown, got %+v event=%+v", evalSource, evalEvent)
	}
	if got := ClassifyRuntimeJSONDynamicSource(evalSource, []string{"/assets/"}, []string{"/__cdp/"}); got != RuntimeJSONDynamicSourceUnknown {
		t.Fatalf("CDP eval classification = %q", got)
	}
}

func TestRuntimeJSONProbeScanDoesNotEraseLateRuntimeExport(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript([]string{"__gosx_hydrate", "__gosx_canvas_event", "__gosx_render_canvas", "__gosx_runtime_ready"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject probe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
try {
window.__calls = [];
window.__gosx_hydrate = function hydrate(id) { window.__calls.push("hydrate:" + id); return "h:" + id; };
window.__gosx_canvas_event = function canvasEvent(id) { window.__calls.push("event:" + id); return "e:" + id; };
window.__gosx_render_canvas = function renderCanvas(id) { window.__calls.push("render:" + id); return "r:" + id; };
setTimeout(function() {
  var firstA = window.__gosx_hydrate;
  var firstB = window.__gosx_hydrate;
  window.__afterThreeScans = {
    hydrateType: typeof window.__gosx_hydrate,
    eventType: typeof window.__gosx_canvas_event,
    renderType: typeof window.__gosx_render_canvas,
    sameIdentity: firstA === firstB,
    hydrateResult: window.__gosx_hydrate("late"),
    eventResult: window.__gosx_canvas_event("late"),
    renderResult: window.__gosx_render_canvas("late")
  };
  window.__gosxOuroborosProbe.refresh();
  window.__afterRefreshType = typeof window.__gosx_hydrate;
  window.__snapshotBeforeReassign = window.__gosxOuroborosProbe.snapshot();
  window.__gosx_hydrate = function hydrateNext(id) { window.__calls.push("hydrate2:" + id); return "h2:" + id; };
  setTimeout(function() {
    window.__afterReassign = {
      hydrateType: typeof window.__gosx_hydrate,
      hydrateResult: window.__gosx_hydrate("next"),
      sameIdentity: window.__gosx_hydrate === window.__gosx_hydrate
    };
    window.__drainBeforeExternal = window.__gosxOuroborosProbe.drain();
    Object.defineProperty(window, "__gosx_hydrate", {
      value: function hydrateExternal(id) { window.__calls.push("hydrate3:" + id); return "h3:" + id; },
      writable: true,
      configurable: true
    });
    setTimeout(function() {
      window.__afterExternal = {
        hydrateType: typeof window.__gosx_hydrate,
        hydrateResult: window.__gosx_hydrate("external"),
        sameIdentity: window.__gosx_hydrate === window.__gosx_hydrate
      };
      window.__finalDrain = window.__gosxOuroborosProbe.drain();
    }, 180);
  }, 180);
}, 180);
} catch (e) { window.__scanRegressionError = String(e && e.stack || e); }
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if err := d.Evaluate(`new Promise(r => setTimeout(r, 650))`, nil); err != nil {
		t.Fatalf("wait scan: %v", err)
	}
	var got struct {
		Error      string   `json:"error"`
		Calls      []string `json:"calls"`
		AfterScans struct {
			HydrateType   string `json:"hydrateType"`
			EventType     string `json:"eventType"`
			RenderType    string `json:"renderType"`
			SameIdentity  bool   `json:"sameIdentity"`
			HydrateResult string `json:"hydrateResult"`
			EventResult   string `json:"eventResult"`
			RenderResult  string `json:"renderResult"`
		} `json:"afterScans"`
		AfterRefreshType string `json:"afterRefreshType"`
		AfterReassign    struct {
			HydrateType   string `json:"hydrateType"`
			HydrateResult string `json:"hydrateResult"`
			SameIdentity  bool   `json:"sameIdentity"`
		} `json:"afterReassign"`
		AfterExternal struct {
			HydrateType   string `json:"hydrateType"`
			HydrateResult string `json:"hydrateResult"`
			SameIdentity  bool   `json:"sameIdentity"`
		} `json:"afterExternal"`
		Snapshot            ProbeDrainForTest `json:"snapshot"`
		DrainBeforeExternal ProbeDrainForTest `json:"drainBeforeExternal"`
		FinalDrain          ProbeDrainForTest `json:"finalDrain"`
	}
	if err := d.Evaluate(`({
  error: window.__scanRegressionError || "",
  calls: window.__calls || [],
  afterScans: window.__afterThreeScans || {},
  afterRefreshType: window.__afterRefreshType || "",
  afterReassign: window.__afterReassign || {},
  afterExternal: window.__afterExternal || {},
  snapshot: window.__snapshotBeforeReassign || {},
  drainBeforeExternal: window.__drainBeforeExternal || {},
  finalDrain: window.__finalDrain || {}
})`, &got); err != nil {
		t.Fatalf("query export: %v", err)
	}
	if got.Error != "" {
		t.Fatalf("scan regression page error: %s", got.Error)
	}
	if got.AfterScans.HydrateType != "function" || got.AfterScans.EventType != "function" || got.AfterScans.RenderType != "function" || !got.AfterScans.SameIdentity {
		t.Fatalf("late exports not stable after scans: %+v", got.AfterScans)
	}
	if got.AfterScans.HydrateResult != "h:late" || got.AfterScans.EventResult != "e:late" || got.AfterScans.RenderResult != "r:late" {
		t.Fatalf("late export calls failed after scans: %+v", got.AfterScans)
	}
	if got.AfterRefreshType != "function" {
		t.Fatalf("refresh erased hydrate export: %+v", got)
	}
	if got.AfterReassign.HydrateType != "function" || got.AfterReassign.HydrateResult != "h2:next" || !got.AfterReassign.SameIdentity {
		t.Fatalf("reassigned export not stable: %+v", got.AfterReassign)
	}
	if got.AfterExternal.HydrateType != "function" || got.AfterExternal.HydrateResult != "h3:external" || !got.AfterExternal.SameIdentity {
		t.Fatalf("external descriptor replacement was not safely retrapped: %+v", got.AfterExternal)
	}
	for _, want := range []string{"hydrate:late", "event:late", "render:late", "hydrate2:next", "hydrate3:external"} {
		if !containsRuntimeJSONProbeString(got.Calls, want) {
			t.Fatalf("missing call %q in calls %+v", want, got.Calls)
		}
	}
	if countProbeEvents(got.Snapshot.Events, "runtime-call") < 3 {
		t.Fatalf("snapshot did not retain runtime-call evidence: %+v", got.Snapshot.Events)
	}
	if countProbeEvents(got.DrainBeforeExternal.Events, "runtime-call") < 1 || countProbeEvents(got.FinalDrain.Events, "runtime-call") < 1 {
		t.Fatalf("drain did not record reassignment/external calls: before=%+v final=%+v", got.DrainBeforeExternal.Events, got.FinalDrain.Events)
	}
}

func TestRuntimeJSONProbeDoubleInstallIsIdempotent(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript([]string{"__gosx_idempotent", "__gosx_canvas_event", "__gosx_accessor"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if err := d.Evaluate(script, nil); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := d.Evaluate(script, nil); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), nil); err != nil {
		t.Fatalf("clear install events: %v", err)
	}
	if err := d.Evaluate(`(function(){
window.__doubleInstallCalls = [];
window.__gosx_idempotent = function first(value) { window.__doubleInstallCalls.push("first:" + value); return value + 1; };
Object.defineProperty(window, "__gosx_canvas_event", {value:function canvas(id, kind) { window.__doubleInstallCalls.push("canvas:" + typeof kind); return kind === 3 ? "kind:3" : typeof kind; }, writable:true, configurable:true});
var payload = JSON.stringify({value:4});
var parsed = JSON.parse(payload);
var sameWrapper = window.__gosx_idempotent === window.__gosx_idempotent;
var first = window.__gosx_idempotent(parsed.value);
window.__gosx_idempotent = function second(value) { window.__doubleInstallCalls.push("second:" + value); return value + 2; };
var second = window.__gosx_idempotent(5);
var canvasGood = window.__gosx_canvas_event("surface", 3, new Float64Array([1, 2]), "");
var canvasBad = window.__gosx_canvas_event("surface", "bad", new Float64Array([1, 2]), "");
var hostileKind = {valueOf:function(){ throw new Error("coerced"); }, toString:function(){ throw new Error("coerced"); }};
var canvasHostile = window.__gosx_canvas_event("surface", hostileKind, new Float64Array([1, 2]), "");
var frame = document.createElement("iframe");
document.body.appendChild(frame);
function capture(fn) {
  try { return {ok:true, value:fn()}; } catch (e) { return {ok:false, name:e && e.name || ""}; }
}
function accessorDescriptor(target) {
  var value = 7;
  return {configurable:true, enumerable:true, get:function(){ return value; }, set:function(v){ value = v; target.wrote = v; }};
}
var nativeTarget = frame.contentWindow;
var nativeMarker = {};
var probedMarker = {};
var nativeAccessor = capture(function(){ frame.contentWindow.Object.defineProperty(nativeTarget, "__native_accessor", accessorDescriptor(nativeMarker)); nativeTarget.__native_accessor = 9; return nativeTarget.__native_accessor + ":" + nativeMarker.wrote; });
var probedAccessor = capture(function(){ Object.defineProperty(window, "__gosx_accessor", accessorDescriptor(probedMarker)); window.__gosx_accessor = 9; return window.__gosx_accessor + ":" + probedMarker.wrote; });
window.__doubleInstallResult = {
  sameWrapper:sameWrapper,
  first:first,
  second:second,
  canvasGood:canvasGood,
  canvasBad:canvasBad,
  canvasHostile:canvasHostile,
  accessorParity:nativeAccessor.ok === probedAccessor.ok && nativeAccessor.value === probedAccessor.value,
  accessorValue:String(nativeAccessor.value) + ":" + String(probedAccessor.value)
};
})()`, nil); err != nil {
		t.Fatalf("exercise double install: %v", err)
	}
	if err := d.Evaluate(`new Promise(r => setTimeout(r, 160))`, nil); err != nil {
		t.Fatalf("wait scan: %v", err)
	}
	var got struct {
		SameWrapper    bool     `json:"sameWrapper"`
		First          int      `json:"first"`
		Second         int      `json:"second"`
		CanvasGood     string   `json:"canvasGood"`
		CanvasBad      string   `json:"canvasBad"`
		CanvasHostile  string   `json:"canvasHostile"`
		AccessorParity bool     `json:"accessorParity"`
		AccessorValue  string   `json:"accessorValue"`
		Calls          []string `json:"calls"`
	}
	if err := d.Evaluate(`Object.assign({calls: window.__doubleInstallCalls || []}, window.__doubleInstallResult || {})`, &got); err != nil {
		t.Fatalf("query double install result: %v", err)
	}
	if !got.SameWrapper || got.First != 5 || got.Second != 7 || got.CanvasGood != "kind:3" || got.CanvasBad != "string" || got.CanvasHostile != "object" || !got.AccessorParity || got.AccessorValue != "9:9:9:9" {
		t.Fatalf("double install changed runtime semantics: %+v", got)
	}
	var drained struct {
		Events []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drained); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := countRuntimeJSONProbeEventsByKindAndName(drained.Events, "json-call", "JSON.stringify"); got != 1 {
		t.Fatalf("JSON.stringify events = %d, want 1 events=%+v", got, drained.Events)
	}
	if got := countRuntimeJSONProbeEventsByKindAndName(drained.Events, "json-call", "JSON.parse"); got != 1 {
		t.Fatalf("JSON.parse events = %d, want 1 events=%+v", got, drained.Events)
	}
	if got := countRuntimeJSONProbeEventsByKindAndName(drained.Events, "runtime-call", "__gosx_idempotent"); got != 2 {
		t.Fatalf("__gosx_idempotent runtime events = %d, want 2 events=%+v", got, drained.Events)
	}
	if got := countRuntimeJSONProbeEventsByKindAndName(drained.Events, "runtime-call", "__gosx_canvas_event"); got != 3 {
		t.Fatalf("__gosx_canvas_event runtime events = %d, want 3 events=%+v", got, drained.Events)
	}
	if got := countRuntimeJSONCanvasEventKind(drained.Events, 3); got != 1 {
		t.Fatalf("canvas event kind 3 events = %d, want 1 events=%+v", got, drained.Events)
	}
	if got := countRuntimeJSONCanvasEventKind(drained.Events, 0); got != 0 {
		t.Fatalf("invalid canvas event kind leaked into events=%+v", drained.Events)
	}
}

func TestRuntimeJSONProbeReportsPreexistingLockedGlobal(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
Object.defineProperty(window, "__gosx_locked", {value: function locked(){ return "locked"; }, writable: false, configurable: false});
var strictWriteThrows = false;
(function(){ "use strict"; try { window.__gosx_locked = function(){}; } catch (e) { strictWriteThrows = e.name === "TypeError"; } })();
window.__lockedStrictWriteThrows = strictWriteThrows;
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	script, err := RuntimeJSONProbeScript([]string{"__gosx_locked"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := d.Evaluate(script, nil); err != nil {
		t.Fatalf("run probe after locked global exists: %v", err)
	}
	var strictThrows bool
	if err := d.Evaluate(`window.__lockedStrictWriteThrows`, &strictThrows); err != nil {
		t.Fatalf("strict write query: %v", err)
	}
	if !strictThrows {
		t.Fatal("pre-existing locked global did not keep strict-write semantics")
	}
	var drained struct {
		WrappedGlobals   []string `json:"wrappedGlobals"`
		UnwrappedGlobals []string `json:"unwrappedGlobals"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drained); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if strings.Contains(strings.Join(drained.WrappedGlobals, ","), "__gosx_locked") {
		t.Fatalf("locked global was reported wrapped: %+v", drained.WrappedGlobals)
	}
	if !strings.Contains(strings.Join(drained.UnwrappedGlobals, ","), "__gosx_locked") {
		t.Fatalf("locked global was not reported unwrapped: %+v", drained.UnwrappedGlobals)
	}
}

func TestRuntimeJSONProbeDefinePropertyWrapperAvoidsHostileDescriptorEnumeration(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript([]string{"__gosx_defined"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject probe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
var touchedExtra = false;
var descriptor = {
  value: function defined(value) { return value + 1; },
  writable: true,
  configurable: true
};
Object.defineProperty(descriptor, "secret", {enumerable:true, get:function(){ touchedExtra = true; throw new Error("enumerated"); }});
Object.defineProperty(window, "__gosx_defined", descriptor);
var result = window.__gosx_defined(2);
var hostile = {};
Object.defineProperty(hostile, "value", {get:function(){ throw new Error("native value getter"); }});
var unrelatedThrows = false;
try { Object.defineProperty(window, "plain_hostile", hostile); } catch (e) { unrelatedThrows = e.message.indexOf("native value getter") >= 0; }
window.__descriptorResult = {result:result, touchedExtra:touchedExtra, unrelatedThrows:unrelatedThrows};
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var got struct {
		Result          int  `json:"result"`
		TouchedExtra    bool `json:"touchedExtra"`
		UnrelatedThrows bool `json:"unrelatedThrows"`
	}
	if err := d.Evaluate(`window.__descriptorResult`, &got); err != nil {
		t.Fatalf("descriptor result: %v", err)
	}
	if got.Result != 3 || got.TouchedExtra || !got.UnrelatedThrows {
		t.Fatalf("descriptor semantics changed: %+v", got)
	}
	var drained struct {
		WrappedGlobals []string     `json:"wrappedGlobals"`
		Events         []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drained); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(strings.Join(drained.WrappedGlobals, ","), "__gosx_defined") {
		t.Fatalf("defined global was not wrapped: %+v", drained.WrappedGlobals)
	}
}

func TestRuntimeJSONProbeDefinePropertyDescriptorParity(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript([]string{"__gosx_parity_data", "__gosx_parity_accessor", "__gosx_parity_mixed", "__gosx_parity_proxy"})
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject probe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
try {
var frame = document.createElement("iframe");
document.body.appendChild(frame);
var nativeDefine = frame.contentWindow.Object.defineProperty;
var nativeTarget = frame.contentWindow;
function capture(fn) {
  try { return {ok:true, value:fn()}; } catch (e) { return {ok:false, name:e && e.name || "", message:String(e && e.message || e)}; }
}
function sameOutcome(a, b) { return a.ok === b.ok && (a.ok || a.name === b.name); }
function makeUnrelated(label) {
  var touched = {value:false};
  var descriptor = {value:function(){ return label; }, writable:true, configurable:true};
  Object.defineProperty(descriptor, "secret", {enumerable:true, get:function(){ touched.value = true; throw new Error("secret"); }});
  return {descriptor:descriptor, touched:touched};
}
var nativeUnrelated = makeUnrelated("native");
var probedUnrelated = makeUnrelated("probed");
var nativeUnrelatedResult = capture(function(){ nativeDefine(nativeTarget, "__gosx_native_unrelated", nativeUnrelated.descriptor); return nativeTarget.__gosx_native_unrelated(); });
var probedUnrelatedResult = capture(function(){ Object.defineProperty(window, "__gosx_parity_data", probedUnrelated.descriptor); return window.__gosx_parity_data(); });

function throwingStandard() {
  var descriptor = {value:function(){ return 1; }, configurable:true};
  Object.defineProperty(descriptor, "enumerable", {get:function(){ throw new RangeError("enumerable boom"); }});
  return descriptor;
}
var nativeStandard = capture(function(){ nativeDefine(nativeTarget, "__gosx_native_standard", throwingStandard()); });
var probedStandard = capture(function(){ Object.defineProperty(window, "__gosx_parity_standard", throwingStandard()); });

function accessorDescriptor(target) {
  var value = 4;
  return {configurable:true, enumerable:true, get:function(){ return value; }, set:function(v){ value = v; target.wrote = v; }};
}
var nativeAccessor = capture(function(){ var marker = {}; nativeDefine(nativeTarget, "__gosx_native_accessor", accessorDescriptor(marker)); nativeTarget.__gosx_native_accessor = 8; return nativeTarget.__gosx_native_accessor + ":" + marker.wrote; });
var probedAccessor = capture(function(){ var marker = {}; Object.defineProperty(window, "__gosx_parity_accessor", accessorDescriptor(marker)); window.__gosx_parity_accessor = 8; return window.__gosx_parity_accessor + ":" + marker.wrote; });

function mixedDescriptor() {
  return {configurable:true, value:function(){ return 1; }, get:function(){ return 2; }};
}
var nativeMixed = capture(function(){ nativeDefine(nativeTarget, "__gosx_native_mixed", mixedDescriptor()); });
var probedMixed = capture(function(){ Object.defineProperty(window, "__gosx_parity_mixed", mixedDescriptor()); });

function revokedDescriptor() {
  var pair = Proxy.revocable({value:function(){ return 1; }, configurable:true}, {});
  pair.revoke();
  return pair.proxy;
}
var nativeRevoked = capture(function(){ nativeDefine(nativeTarget, "__gosx_native_proxy", revokedDescriptor()); });
var probedRevoked = capture(function(){ Object.defineProperty(window, "__gosx_parity_proxy", revokedDescriptor()); });

window.__descriptorParity = {
  unrelated: sameOutcome(nativeUnrelatedResult, probedUnrelatedResult),
  unrelatedValues: nativeUnrelatedResult.value + ":" + probedUnrelatedResult.value,
  unrelatedTouched: nativeUnrelated.touched.value || probedUnrelated.touched.value,
  standard: sameOutcome(nativeStandard, probedStandard),
  standardName: nativeStandard.name + ":" + probedStandard.name,
  accessor: sameOutcome(nativeAccessor, probedAccessor),
  accessorValues: nativeAccessor.value + ":" + probedAccessor.value,
  mixed: sameOutcome(nativeMixed, probedMixed),
  mixedName: nativeMixed.name + ":" + probedMixed.name,
  revoked: sameOutcome(nativeRevoked, probedRevoked),
  revokedName: nativeRevoked.name + ":" + probedRevoked.name
};
} catch (e) { window.__descriptorParityError = String(e && e.stack || e); }
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var got struct {
		Unrelated        bool   `json:"unrelated"`
		UnrelatedValues  string `json:"unrelatedValues"`
		UnrelatedTouched bool   `json:"unrelatedTouched"`
		Standard         bool   `json:"standard"`
		StandardName     string `json:"standardName"`
		Accessor         bool   `json:"accessor"`
		AccessorValues   string `json:"accessorValues"`
		Mixed            bool   `json:"mixed"`
		MixedName        string `json:"mixedName"`
		Revoked          bool   `json:"revoked"`
		RevokedName      string `json:"revokedName"`
		Error            string `json:"error"`
	}
	if err := d.Evaluate(`Object.assign({error: window.__descriptorParityError || ""}, window.__descriptorParity || {})`, &got); err != nil {
		t.Fatalf("descriptor parity query: %v", err)
	}
	if got.Error != "" {
		t.Fatalf("descriptor parity page error: %s", got.Error)
	}
	if !got.Unrelated || got.UnrelatedValues != "native:probed" || got.UnrelatedTouched {
		t.Fatalf("unrelated descriptor parity failed: %+v", got)
	}
	if !got.Standard || got.StandardName != "RangeError:RangeError" {
		t.Fatalf("standard-key throwing getter parity failed: %+v", got)
	}
	if !got.Accessor || got.AccessorValues != "8:8:8:8" {
		t.Fatalf("accessor descriptor parity failed: %+v", got)
	}
	if !got.Mixed || got.MixedName != "TypeError:TypeError" {
		t.Fatalf("mixed descriptor parity failed: %+v", got)
	}
	if !got.Revoked || got.RevokedName != "TypeError:TypeError" {
		t.Fatalf("revoked proxy descriptor parity failed: %+v", got)
	}
}

func TestRuntimeJSONProbeBoundsEventBuffer(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	script, err := RuntimeJSONProbeScript(nil)
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject probe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
for (var i = 0; i < 9000; i++) JSON.stringify({i:i});
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var drained struct {
		DroppedCount int          `json:"droppedCount"`
		Events       []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drained); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(drained.Events) > 8192 {
		t.Fatalf("events len = %d, want <= 8192", len(drained.Events))
	}
	if drained.DroppedCount == 0 {
		t.Fatal("expected dropped events after buffer overflow")
	}
}

func TestRuntimeJSONProbeDrainResetsDropsAndSanitizesBaseProbeURLs(t *testing.T) {
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	if err := InjectOuroborosProbe(d); err != nil {
		t.Fatalf("InjectOuroborosProbe: %v", err)
	}
	script, err := RuntimeJSONProbeScript(nil)
	if err != nil {
		t.Fatalf("RuntimeJSONProbeScript: %v", err)
	}
	if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
		t.Fatalf("inject runtime probe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
for (var i = 0; i < 9000; i++) JSON.stringify({i:i});
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL + "/route?token=secret#frag"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var first struct {
		DroppedCount int          `json:"droppedCount"`
		Events       []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &first); err != nil {
		t.Fatalf("first drain: %v", err)
	}
	if first.DroppedCount == 0 {
		t.Fatal("first drain did not report dropped events")
	}
	for _, event := range first.Events {
		text := fmt.Sprint(event.Detail)
		if strings.Contains(text, "token=secret") || strings.Contains(text, "#frag") {
			t.Fatalf("probe retained unsanitized URL detail: %+v", event)
		}
	}
	var second struct {
		DroppedCount int          `json:"droppedCount"`
		Events       []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &second); err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if second.DroppedCount != 0 {
		t.Fatalf("dropped count did not reset after drain: %+v", second)
	}
}

func TestRuntimeJSONProbeOverheadIsInformational(t *testing.T) {
	withoutProbe := repeatedRuntimeJSONProbeOverhead(t, false, 5)
	withProbe := repeatedRuntimeJSONProbeOverhead(t, true, 5)
	offMedian := medianFloat(withoutProbe)
	onMedian := medianFloat(withProbe)
	t.Logf("runtime-json probe overhead smoke: off=%v median=%.3fms on=%v median=%.3fms delta=%.3fms ratio=%.3f",
		withoutProbe, offMedian, withProbe, onMedian, onMedian-offMedian, safeProbeRatio(onMedian, offMedian))
}

func requireOuroborosProbeDriver(t *testing.T, timeout time.Duration) *perf.Driver {
	t.Helper()
	d, err := perf.New(perf.WithHeadless(true), perf.WithTimeout(timeout))
	if err == nil {
		t.Cleanup(func() { d.Close() })
		return d
	}
	if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
		t.Fatalf("GOSX_REQUIRE_CHROME is set, but Chrome could not start: %v", err)
	}
	t.Skipf("skipping browser probe test: %v", err)
	return nil
}

func injectRuntimeJSONProbeForTest(d *perf.Driver, script string) error {
	addScript := page.AddScriptToEvaluateOnNewDocument(script)
	return chromedp.Run(d.Context(), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := addScript.Do(ctx)
		return err
	}))
}

func newRuntimeJSONAttributionServerForTest(t *testing.T, scriptQuery string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><html><body><script src="/assets/product.js?%s"></script></body></html>`, scriptQuery)
		case "/assets/product.js":
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, strings.Join([]string{
				"(function(){",
				"window.__gosx_product = function productRuntime(value) { return JSON.stringify({value:value}); };",
				"function runProductJSON(){",
				"  var text = JSON.stringify({route: location.pathname, query: location.search});",
				"  JSON.parse(text);",
				"  window.__gosx_product(7);",
				"}",
				"runProductJSON();",
				"})();",
				"",
			}, "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func runRuntimeJSONAttributionPageForTest(t *testing.T, d *perf.Driver, pageURL string) ProbeEvent {
	t.Helper()
	if err := d.Navigate(pageURL); err != nil {
		t.Fatalf("Navigate %s: %v", pageURL, err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady %s: %v", pageURL, err)
	}
	var drained struct {
		Events []ProbeEvent `json:"events"`
	}
	if err := d.Evaluate(RuntimeJSONDrainExpression(true), &drained); err != nil {
		t.Fatalf("drain %s: %v", pageURL, err)
	}
	return findRuntimeJSONProbeEventForTest(t, drained.Events, "json-call", "JSON.parse")
}

func findRuntimeJSONProbeEventForTest(t *testing.T, events []ProbeEvent, kind, name string) ProbeEvent {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind && event.Name == name {
			return event
		}
	}
	t.Fatalf("missing %s/%s event in %+v", kind, name, events)
	return ProbeEvent{}
}

func runRuntimeJSONProbeOverheadPage(t *testing.T, withProbe bool) float64 {
	t.Helper()
	d := requireOuroborosProbeDriver(t, 15*time.Second)
	if withProbe {
		script, err := RuntimeJSONProbeScript([]string{"__gosx_hot"})
		if err != nil {
			t.Fatalf("RuntimeJSONProbeScript: %v", err)
		}
		if err := injectRuntimeJSONProbeForTest(d, script); err != nil {
			t.Fatalf("inject probe: %v", err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
window.__gosx_hot = function(value) { return value + 1; };
var start = performance.now();
for (var i = 0; i < 1000; i++) {
  var text = JSON.stringify({i:i, value:"x"});
  JSON.parse(text);
  window.__gosx_hot(i);
}
window.__probeSmokeDuration = performance.now() - start;
</script></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	var duration float64
	if err := d.Evaluate(`window.__probeSmokeDuration || 0`, &duration); err != nil {
		t.Fatalf("duration query: %v", err)
	}
	return duration
}

func repeatedRuntimeJSONProbeOverhead(t *testing.T, withProbe bool, count int) []float64 {
	t.Helper()
	out := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, runRuntimeJSONProbeOverheadPage(t, withProbe))
	}
	return out
}

func medianFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64{}, values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func safeProbeRatio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func containsRuntimeJSONProbeString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func countProbeEvents(events []ProbeEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func countRuntimeJSONProbeEventsByKindAndName(events []ProbeEvent, kind, name string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind && event.Name == name {
			count++
		}
	}
	return count
}

func countRuntimeJSONCanvasEventKind(events []ProbeEvent, want int) int {
	count := 0
	for _, event := range events {
		if event.Kind != "runtime-call" || event.Name != "__gosx_canvas_event" {
			continue
		}
		value, ok := event.Detail["eventKind"]
		if !ok {
			continue
		}
		n, ok := numberFromAny(value)
		if ok && int(n) == want {
			count++
		}
	}
	return count
}
