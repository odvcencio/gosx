package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const checkableScene = `{
	"schema":"gosx.scene3d.ir.v1",
	"objects":[
		{"id":"floor","kind":"plane","width":8,"height":8,"rotationX":-1.5708,"y":-1,"materialKind":"matte","color":"#2b3440","texture":"/textures/floor.png"},
		{"id":"hero","kind":"sphere","radius":1,"segments":16,"y":0.2,"materialKind":"standard","color":"#ff8a5b"}
	],
	"lights":[{"id":"key","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.3}]
}`

func writeCheckFixture(t *testing.T, document string) (dir, scenePath, assetsRoot string) {
	t.Helper()
	dir = t.TempDir()
	assetsRoot = filepath.Join(dir, "public")
	mustWriteFile(t, filepath.Join(assetsRoot, "textures", "floor.png"), "png")
	scenePath = filepath.Join(dir, "product.scene.json")
	mustWriteFile(t, scenePath, document)
	return dir, scenePath, assetsRoot
}

// TestRunSceneCheckWritesBaselineThenProvesItUnchanged walks the whole loop:
// create a baseline, then prove a later run reproduces it exactly.
func TestRunSceneCheckWritesBaselineThenProvesItUnchanged(t *testing.T) {
	dir, scenePath, assetsRoot := writeCheckFixture(t, checkableScene)
	golden := filepath.Join(dir, "golden", "product.png")

	var created bytes.Buffer
	err := runSceneCommand([]string{"check", "--assets", assetsRoot, "--golden", golden,
		"--write-golden", "--width", "128", "--height", "96", scenePath}, &created)
	if err != nil {
		t.Fatalf("baseline run failed: %v\n%s", err, created.String())
	}
	if !strings.Contains(created.String(), "wrote a new baseline") {
		t.Fatalf("a written baseline must say so:\n%s", created.String())
	}
	if _, statErr := os.Stat(golden); statErr != nil {
		t.Fatalf("baseline was not written: %v", statErr)
	}

	var proved bytes.Buffer
	err = runSceneCommand([]string{"check", "--assets", assetsRoot, "--golden", golden,
		"--repeat", "3", "--width", "128", "--height", "96", scenePath}, &proved)
	if err != nil {
		t.Fatalf("unchanged run failed: %v\n%s", err, proved.String())
	}
	for _, want := range []string{"Scene3D check: pass", "Determinism: 3 runs, pass", "identical: 12288 pixels compared"} {
		if !strings.Contains(proved.String(), want) {
			t.Fatalf("expected %q in output:\n%s", want, proved.String())
		}
	}
}

// TestRunSceneCheckLocalizesABaselineChange proves the loop reports where the
// frame moved, not only that its hash moved.
func TestRunSceneCheckLocalizesABaselineChange(t *testing.T) {
	dir, scenePath, assetsRoot := writeCheckFixture(t, checkableScene)
	golden := filepath.Join(dir, "golden", "product.png")
	diffOut := filepath.Join(dir, "build", "diff.png")

	if err := runSceneCommand([]string{"check", "--assets", assetsRoot, "--golden", golden,
		"--write-golden", "--width", "128", "--height", "96", scenePath}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(checkableScene, `"y":0.2`, `"y":0.9`, 1)
	mustWriteFile(t, scenePath, moved)

	var out bytes.Buffer
	err := runSceneCommand([]string{"check", "--json", "--assets", assetsRoot, "--golden", golden,
		"--diff-out", diffOut, "--width", "128", "--height", "96", scenePath}, &out)
	if err == nil {
		t.Fatalf("a moved mesh must fail the baseline check:\n%s", out.String())
	}
	var report sceneCheckReport
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode report: %v\n%s", decodeErr, out.String())
	}
	if report.Valid || report.Golden == nil || report.Golden.Diff == nil {
		t.Fatalf("unexpected report: %+v", report)
	}
	diff := report.Golden.Diff
	if diff.Identical || diff.ChangedPixels == 0 || len(diff.Regions) == 0 {
		t.Fatalf("diff must localize the change: %+v", diff)
	}
	if diff.Bounds == nil || diff.Bounds.Width() <= 0 || diff.Bounds.Height() <= 0 {
		t.Fatalf("diff bounds = %+v", diff.Bounds)
	}
	if !report.Steps.GoldenCompared || report.Steps.GoldenBaselineNew {
		t.Fatalf("steps must record a comparison, not a write: %+v", report.Steps)
	}
	data, readErr := os.ReadFile(diffOut)
	if readErr != nil {
		t.Fatalf("diff image was not written: %v", readErr)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("diff output is not PNG")
	}
}

// TestRunSceneCheckReportsMissingBaselineAsFailure proves an absent baseline
// never reads as a pass.
func TestRunSceneCheckReportsMissingBaselineAsFailure(t *testing.T) {
	dir, scenePath, assetsRoot := writeCheckFixture(t, checkableScene)
	golden := filepath.Join(dir, "golden", "absent.png")

	var out bytes.Buffer
	err := runSceneCommand([]string{"check", "--assets", assetsRoot, "--golden", golden,
		"--width", "64", "--height", "48", scenePath}, &out)
	if err == nil {
		t.Fatalf("a missing baseline must fail:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--write-golden") {
		t.Fatalf("output must say how to create the baseline:\n%s", out.String())
	}
}

// TestRunSceneCheckNamesUnresolvedAssets proves an unreachable asset produces
// an actionable message that names the record and the document path.
func TestRunSceneCheckNamesUnresolvedAssets(t *testing.T) {
	broken := strings.Replace(checkableScene, "/textures/floor.png", "/textures/gone.png", 1)
	_, scenePath, assetsRoot := writeCheckFixture(t, broken)

	var out bytes.Buffer
	err := runSceneCommand([]string{"check", "--json", "--assets", assetsRoot,
		"--width", "64", "--height", "48", scenePath}, &out)
	if err == nil {
		t.Fatalf("an unresolved asset must fail:\n%s", out.String())
	}
	var report sceneCheckReport
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode report: %v\n%s", decodeErr, out.String())
	}
	if report.Inspection.AssetResolution == nil || report.Inspection.AssetResolution.Unresolved != 1 {
		t.Fatalf("asset resolution = %+v", report.Inspection.AssetResolution)
	}
	found := false
	for _, diag := range report.Validation.Diagnostics {
		if diag.Code == "scene.asset.unresolved" && diag.ID == "floor" && diag.Path == "objects[0].texture" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unresolved-asset diagnostic naming the record: %+v", report.Validation.Diagnostics)
	}
}

// TestRunSceneCheckWithoutAssetRootSaysItDidNotLook proves a skipped step is
// never reported as a pass.
func TestRunSceneCheckWithoutAssetRootSaysItDidNotLook(t *testing.T) {
	_, scenePath, _ := writeCheckFixture(t, checkableScene)

	var out bytes.Buffer
	if err := runSceneCommand([]string{"check", "--width", "64", "--height", "48", scenePath}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "not checked (pass --assets to resolve them)") {
		t.Fatalf("output must admit that assets were not checked:\n%s", out.String())
	}
}

// TestRunSceneCheckSurfacesPreviewCoverageGaps proves the loop never returns a
// silent blank frame for geometry the CPU rasterizer cannot draw.
func TestRunSceneCheckSurfacesPreviewCoverageGaps(t *testing.T) {
	// "knot" is here to prove a NEGATIVE, and it used to prove the opposite.
	//
	// This test asserted that a torusknot reports unsupported_geometry. That
	// diagnostic was wrong: render/bundle delegates to scene/geom.buildTorusKnot
	// and a headless render of one produces 785 non-background pixels, while
	// preview.CanRasterizeKind returned false for it. So the warning told an
	// author to delete working geometry, which is worse than a missing warning.
	// CanRasterizeKind now delegates to geom.NormalizeKind, the same function
	// render/bundle calls, so the two cannot drift apart again.
	//
	// "gap" uses a kind the rasterizer genuinely cannot draw. rasterizeDraw
	// handles bundle.lit, bundle.unlit, bundle.shadow and particles, and skips
	// bundle.worldLine, so a line list draws nothing on the CPU path.
	//
	// "lamp" is a rect-area light because that is the one authored light kind
	// the CPU preview genuinely cannot shade. Point lights are supported and
	// must not be used as the positive fixture for unsupported_light.
	//
	// Note that a polyhedron would NOT work here: tetrahedron, icosahedron and
	// the rest carry no wire kind of their own. They lower to "gltf-mesh" with
	// baked vertices, so the schema vocabulary rejects their authored names and
	// the document would fail validation before any diagnostic ran.
	document := `{"schema":"gosx.scene3d.ir.v1","objects":[
		{"id":"gap","kind":"lines","positions":[0,0,0,1,1,1]},
		{"id":"knot","kind":"torusknot","radius":0.6,"tube":0.2},
		{"id":"ok","kind":"cube","size":1,"x":-2}
	],"lights":[
		{"id":"key","kind":"directional","directionY":-1,"intensity":1},
		{"id":"lamp","kind":"rect-area","intensity":3,"y":3,"width":4,"height":4}
	]}`
	_, scenePath, _ := writeCheckFixture(t, document)

	var out bytes.Buffer
	if err := runSceneCommand([]string{"check", "--json", "--width", "64", "--height", "48", scenePath}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	var report sceneCheckReport
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode report: %v\n%s", decodeErr, out.String())
	}
	codes := map[string]string{}
	for _, event := range report.Harness.Events {
		if event.Frame == nil {
			continue
		}
		for _, diag := range event.Frame.Diagnostics {
			codes[diag.Code+"/"+diag.Target] = diag.Message
		}
	}
	for _, key := range []string{"scene.preview.unsupported_geometry/gap", "scene.preview.unsupported_light/lamp"} {
		if _, ok := codes[key]; !ok {
			t.Fatalf("expected %s in frame diagnostics: %v", key, codes)
		}
	}
	// The two negatives matter as much as the positives. A diagnostic that names
	// a kind the rasterizer DOES draw sends an author to delete working
	// geometry, so both of these must stay absent.
	for _, key := range []string{
		"scene.preview.unsupported_geometry/ok",
		"scene.preview.unsupported_geometry/knot",
	} {
		if message, ok := codes[key]; ok {
			t.Fatalf("%s is drawable and must not be reported as unsupported, got %q", key, message)
		}
	}
}

// TestRunSceneCheckReportIsReproducible pins the report's value as a machine
// interface. Two runs of the same scene must produce the same evidence hash.
func TestRunSceneCheckReportIsReproducible(t *testing.T) {
	_, scenePath, assetsRoot := writeCheckFixture(t, checkableScene)
	run := func() sceneCheckReport {
		var out bytes.Buffer
		if err := runSceneCommand([]string{"check", "--json", "--assets", assetsRoot,
			"--width", "96", "--height", "64", scenePath}, &out); err != nil {
			t.Fatalf("%v\n%s", err, out.String())
		}
		var report sceneCheckReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	first, second := run(), run()
	if first.Harness.ContentHash == "" || first.Harness.ContentHash != second.Harness.ContentHash {
		t.Fatalf("evidence hash is not reproducible: %s vs %s", first.Harness.ContentHash, second.Harness.ContentHash)
	}
	if first.Schema != sceneCheckSchema {
		t.Fatalf("report schema = %q", first.Schema)
	}
}

func TestRunSceneDiffCommand(t *testing.T) {
	dir, scenePath, _ := writeCheckFixture(t, checkableScene)
	reference := filepath.Join(dir, "reference.png")
	candidate := filepath.Join(dir, "candidate.png")
	diffOut := filepath.Join(dir, "diff.png")

	if err := runSceneCommand([]string{"render", "--out", reference, "--width", "96", "--height", "64", scenePath}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// The same document must diff clean against itself.
	if err := runSceneCommand([]string{"render", "--out", candidate, "--width", "96", "--height", "64", scenePath}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var same bytes.Buffer
	if err := runSceneCommand([]string{"diff", reference, candidate}, &same); err != nil {
		t.Fatalf("identical renders must diff clean: %v\n%s", err, same.String())
	}
	if !strings.Contains(same.String(), "identical:") {
		t.Fatalf("expected an identical summary:\n%s", same.String())
	}

	moved := strings.Replace(checkableScene, `"y":0.2`, `"y":1.1`, 1)
	mustWriteFile(t, scenePath, moved)
	if err := runSceneCommand([]string{"render", "--out", candidate, "--width", "96", "--height", "64", scenePath}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var changed bytes.Buffer
	err := runSceneCommand([]string{"diff", "--json", "--out", diffOut, reference, candidate}, &changed)
	if err == nil {
		t.Fatalf("different renders must exit non-zero:\n%s", changed.String())
	}
	var result struct {
		Identical     bool `json:"identical"`
		ChangedPixels int  `json:"changedPixels"`
		RegionCount   int  `json:"regionCount"`
		Regions       []struct {
			MinX int `json:"minX"`
			MaxX int `json:"maxX"`
		} `json:"regions"`
	}
	if decodeErr := json.Unmarshal(changed.Bytes(), &result); decodeErr != nil {
		t.Fatalf("decode diff report: %v\n%s", decodeErr, changed.String())
	}
	if result.Identical || result.ChangedPixels == 0 || result.RegionCount == 0 || len(result.Regions) == 0 {
		t.Fatalf("unexpected diff report: %+v", result)
	}
	if _, statErr := os.Stat(diffOut); statErr != nil {
		t.Fatalf("diff image was not written: %v", statErr)
	}
}

// TestSceneRenderDoesNotPrintTheSimulationStepAsRenderTime guards the rule that
// no report may present a constant as a measurement. The renderer's clamped
// animation step is exactly 16.67 ms for a single frame.
func TestSceneRenderDoesNotPrintTheSimulationStepAsRenderTime(t *testing.T) {
	_, scenePath, _ := writeCheckFixture(t, checkableScene)
	var out bytes.Buffer
	if err := runSceneCommand([]string{"render", "--out", filepath.Join(t.TempDir(), "o.png"),
		"--width", "64", "--height", "48", scenePath}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "16.67 ms") {
		t.Fatalf("render output printed the fixed simulation step as a measurement:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "measured on this machine") {
		t.Fatalf("render output must label its timing as a measurement:\n%s", out.String())
	}
}
