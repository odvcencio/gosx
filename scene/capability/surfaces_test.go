package capability

import (
	"os"
	"strings"
	"testing"
)

// This file answers two structural questions about the gate's SHAPE, so an
// unexamined gap does not stay unexamined.
//
//  1. GoSX ships five render surfaces and Backend names three. Why?
//  2. Canvas2D sits in allBackends and, until the line-dashed correction, in zero
//     Matrix rows. Was that a stated rule or an oversight?

const (
	renderBundleRendPath = "../../render/bundle/renderer.go"
	headlessDocPath      = "../../render/gpu/headless/doc.go"
	headlessDevicePath   = "../../render/gpu/headless/device.go"
	previewCoveragePath  = "../preview/coverage.go"
)

// TestFiveRenderSurfacesReduceToThreeBackends records why Backend stops at three,
// instead of adding BackendNative and BackendHeadless.
//
// The two candidates fail for DIFFERENT reasons, and conflating them is what made
// the gap look like one oversight.
//
// render/gpu/headless is not a render surface. It is a gpu.Device. Every method
// on headless.Device satisfies the gpu.Device interface, and
// render/bundle.Renderer holds a gpu.Device field. So headless sits BENEATH the
// renderer, not beside it. A Matrix row for it would describe a device that draws
// nothing on its own, and it would double-count whatever row render/bundle got.
//
// render/bundle is a real surface, and it still needs no row, because Matrix
// answers a SELECTION question. Verdict returns the subset of one browser tab's
// backends that can draw a scene, and chooseSceneBackend in
// 20a-scene-mount-backend.js picks one from that subset. A Go program that calls
// scene/preview.Render has already chosen render/bundle. There is no second
// candidate to fall back to and no runtime to obey the verdict, so a row would
// produce an answer nobody reads.
//
// render/bundle DOES need honesty, and it already has a finer mechanism than a
// per-backend boolean: engine.RenderDiagnostic, emitted per RECORD by
// geometryDiagnostic, lightDiagnostics and materialCoverageDiagnostic in
// scene/preview. A boolean says "this backend cannot do meshes". A diagnostic says
// "record knot draws no pixels, and here is why". The second is strictly more
// useful for a surface with only one candidate.
//
// The test fails if either premise stops holding.
func TestFiveRenderSurfacesReduceToThreeBackends(t *testing.T) {
	// Premise 1: the browser gate has exactly three candidates, and a runtime
	// chooses among them.
	if len(allBackends) != 3 {
		t.Fatalf("allBackends holds %d entries: %v. If a fourth surface became selectable at runtime, "+
			"give it a row and re-read this test", len(allBackends), allBackends)
	}
	mount := readRenderer(t, mountBackendPath)
	if !strings.Contains(mount, "function chooseSceneBackend(backendCaps, prefs, availability) {") {
		t.Error("chooseSceneBackend moved; the whole reason Matrix has rows is that something SELECTS " +
			"a backend from Capable")
	}
	for _, token := range []string{`b === "webgpu"`, `b === "webgl" || b === "webgl2"`, `b === "canvas2d" || b === "canvas"`} {
		if !strings.Contains(mount, token) {
			t.Errorf("chooseSceneBackend no longer selects on %s; the candidate set changed", token)
		}
	}

	// Premise 2: headless is a device under the renderer, not a surface beside it.
	renderer := mustRead(t, renderBundleRendPath)
	if !strings.Contains(renderer, "device        gpu.Device") {
		t.Error("render/bundle.Renderer no longer holds a gpu.Device; headless may have become a " +
			"peer surface, and then it needs its own row")
	}
	device := mustRead(t, headlessDevicePath)
	for _, method := range []string{
		"func (d *Device) CreateRenderPipeline(desc gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {",
		"func (d *Device) CreateComputePipeline(desc gpu.ComputePipelineDesc) (gpu.ComputePipeline, error) {",
		"func (d *Device) CreateCommandEncoder() gpu.CommandEncoder {",
	} {
		if !strings.Contains(device, method) {
			t.Errorf("headless.Device no longer implements %q; re-check whether it is still a gpu.Device", method)
		}
	}
	doc := mustRead(t, headlessDocPath)
	if !strings.Contains(doc, "pure-Go implementation of gpu.Device") {
		t.Error("the headless package doc no longer calls itself a gpu.Device implementation; " +
			"re-read what it is before deciding it needs no row")
	}

	// Premise 3: the native surface carries its own honesty mechanism, per record.
	coverage := mustRead(t, previewCoveragePath)
	for _, symbol := range []string{
		"func geometryDiagnostic(kind, id string) (engine.RenderDiagnostic, bool) {",
		"func lightDiagnostics(lights []engine.RenderLight) []engine.RenderDiagnostic {",
		"var knownUnsupportedKinds = map[string]string{",
	} {
		if !strings.Contains(coverage, symbol) {
			t.Errorf("scene/preview lost %q. That per-record diagnostic is the reason render/bundle "+
				"needs no Matrix row; without it, the native surface has NO honesty gate", symbol)
		}
	}

	// And no Backend constant may name either surface without a row, because a
	// named-but-rowless backend is a trap. See the next test.
	for _, name := range []Backend{"native", "bundle", "headless", "desktop"} {
		if _, rowed := Matrix[FeatureSkinning][name]; rowed {
			t.Errorf("Matrix names backend %q. Populate every row for it and add it to allBackends, "+
				"or drop the key", name)
		}
	}
}

// TestVerdictTreatsAnUnknownBackendAsIncapable pins the hazard a new Backend
// constant would create, so the reasoning above rests on measured behaviour.
//
// Verdict uses the caller's required list VERBATIM as its candidate set. It never
// checks the list against allBackends. So a backend with no Matrix keys is
// unsupported for every named feature, and a REQUIRED feature then empties
// Capable.
//
// That is exactly the failure mode the water-object-mesh-shadow-pass correction
// was written to avoid, reachable here through a typo rather than a wrong cell.
// Adding BackendNative or BackendHeadless without populating every row would arm
// it. This test states the behaviour so the next reader cannot be surprised by it.
func TestVerdictTreatsAnUnknownBackendAsIncapable(t *testing.T) {
	const unknown Backend = "no-such-backend"

	// A droppable feature keeps the unknown backend and reports the gap.
	caps := Verdict([]Feature{FeatureIBL}, []Backend{unknown}, DefaultPolicy())
	if len(caps.Capable) != 1 || caps.Capable[0] != unknown {
		t.Fatalf("an unknown backend must survive a droppable feature; Capable=%v", caps.Capable)
	}
	if got := caps.Degraded[unknown]; len(got) != 1 || got[0] != FeatureIBL {
		t.Fatalf("an unknown backend must be degraded by every droppable feature; Degraded=%v", caps.Degraded)
	}

	// A required feature empties Capable. This is the trap.
	caps = Verdict([]Feature{FeatureSkinning}, []Backend{unknown}, DefaultPolicy())
	if len(caps.Capable) != 0 {
		t.Fatalf("a required feature must exclude an unknown backend; Capable=%v", caps.Capable)
	}
	if !hasExcludeReason(caps.Reasons, FeatureSkinning, unknown) {
		t.Fatalf("the exclusion must carry a named reason; Reasons=%v", caps.Reasons)
	}

	// Every constant Backend must therefore either be absent from Verdict's
	// default candidates or carry real rows. Check that no declared Backend is
	// rowless across the whole Matrix.
	for _, b := range allBackends {
		rows := 0
		for _, row := range Matrix {
			if _, ok := row[b]; ok {
				rows++
			}
		}
		if rows == 0 {
			t.Errorf("backend %s sits in allBackends with zero Matrix keys, so supports() returns false "+
				"for every named feature. Give it rows or state the blanket rule in capability.go", b)
		}
	}
}

// TestCanvas2DBlanketExclusionIsStated answers the second question.
//
// Before the line-dashed correction Canvas2D sat in allBackends and in zero rows.
// That produced a real result — every named feature unsupported — from no written
// decision. A reader could not tell an intended blanket exclusion from thirteen
// forgotten keys.
//
// It is now BOTH stated and partly wrong, which is the honest position:
//
//   - Stated: the CANVAS2D BLANKET RULE comment in capability.go names the
//     evidence. createSceneCanvasRenderer draws renderSceneCanvasPoints and
//     renderSceneCanvasWorldBundle and nothing else, so Canvas2D rasterizes no
//     triangle. Every mesh feature is honestly false.
//   - Partly wrong: Canvas2D is the ONLY backend that draws a dash pattern, so
//     line-dashed now carries a canvas2d key. See linedashed_test.go.
//
// This test pins the shape of that answer: at least one real key, and a blanket
// rule for the rest.
func TestCanvas2DBlanketExclusionIsStated(t *testing.T) {
	// The rule must be written down where the reader meets the Matrix.
	source := mustRead(t, "capability.go")
	for _, phrase := range []string{
		"CANVAS2D BLANKET RULE",
		"18-scene-canvas.js",
		"It rasterizes no triangle",
		"STATED blanket exclusion",
	} {
		if !strings.Contains(source, phrase) {
			t.Errorf("capability.go must state the Canvas2D rule; %q is missing. A silent blanket "+
				"exclusion is indistinguishable from thirteen forgotten keys", phrase)
		}
	}

	// And the rule must have at least one earned exception, or it is a rule about
	// nothing and the blanket is back.
	keys := 0
	for feature, row := range Matrix {
		if value, ok := row[BackendCanvas2D]; ok {
			keys++
			if !value {
				t.Errorf("Matrix[%s][canvas2d] is explicitly false. The blanket rule already covers "+
					"false, so an explicit false key adds noise; delete it or explain why it differs", feature)
			}
		}
	}
	if keys == 0 {
		t.Error("Canvas2D carries no Matrix key at all, so the blanket rule covers every feature. " +
			"18-scene-canvas.js calls setLineDash, so at least line-dashed must be true")
	}
	if !Matrix[FeatureLineDashed][BackendCanvas2D] {
		t.Error("line-dashed is the one feature Canvas2D implements; its key must be true")
	}

	// The blanket must still hold for the features that need a mesh. Spot-check
	// the three that would be most tempting to grant.
	for _, feature := range []Feature{FeatureSkinning, FeatureIBL, FeatureWaterSim} {
		if supports(BackendCanvas2D, feature) {
			t.Errorf("Canvas2D claims %s. It rasterizes no triangle, so it can shade no mesh feature", feature)
		}
	}
}

// mustRead reads a Go source file for a structural assertion. It is separate from
// readRenderer only so a failure names the right kind of file.
func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
