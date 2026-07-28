package harness_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/imagediff"
	"m31labs.dev/gosx/scene/preview"
)

func goldenProps(color string, x float64) scene.Props {
	return scene.Props{
		Background:  "#000000",
		Camera:      scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 50, Near: 0.1, Far: 50},
		Environment: scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.6},
		Graph: scene.NewGraph(scene.Mesh{
			ID: "hero", Position: scene.Vec3(x, 0, 0),
			Geometry: scene.BoxGeometry{Width: 1.5, Height: 1.5, Depth: 1.5},
			Material: scene.StandardMaterial{Color: color, Roughness: 0.5},
		}),
	}
}

func goldenOptions() preview.Options {
	return preview.Options{Width: 128, Height: 96, DisableShadows: true, DisablePostFX: true}
}

func renderReference(t *testing.T, props scene.Props) *image.RGBA {
	t.Helper()
	result, err := preview.Render(props, goldenOptions())
	if err != nil {
		t.Fatal(err)
	}
	return result.Image
}

// TestRequireGoldenLocalizesTheChange proves the native loop can say where a
// frame changed, not only that its hash moved.
func TestRequireGoldenLocalizesTheChange(t *testing.T) {
	reference := renderReference(t, goldenProps("#69e3c7", -1.5))

	session := harness.New(goldenProps("#69e3c7", 1.5), goldenOptions())
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	diff, err := session.RequireGolden("hero position", "baseline.png", reference, imagediff.Options{TileSize: 16})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Identical || diff.ChangedPixels == 0 {
		t.Fatalf("moving the mesh must change pixels: %+v", diff)
	}
	if len(diff.Regions) == 0 || diff.Bounds == nil {
		t.Fatalf("diff must localize the change: %+v", diff)
	}
	if err := session.Validate(); err == nil {
		t.Fatal("a required golden mismatch must invalidate the report")
	}
	report := session.Report()
	last := report.Events[len(report.Events)-1]
	if last.Kind != "golden" || last.Golden == nil || !last.Golden.Required {
		t.Fatalf("golden event = %+v", last)
	}
	if !strings.Contains(strings.Join(report.Problems, " "), "baseline.png") {
		t.Fatalf("problem must name the reference: %v", report.Problems)
	}
}

// TestRequireGoldenAcceptsAnUnchangedFrame proves the same loop passes when the
// scene really did not change.
func TestRequireGoldenAcceptsAnUnchangedFrame(t *testing.T) {
	props := goldenProps("#69e3c7", 0)
	reference := renderReference(t, props)

	session := harness.New(props, goldenOptions())
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	diff, err := session.RequireGolden("unchanged", "baseline.png", reference, imagediff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Identical || diff.ChangedPixels != 0 {
		t.Fatalf("an unchanged scene must match its baseline: %s", diff.Summary())
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestRequireGoldenFileFailsOnMissingBaseline proves a missing baseline never
// looks like a pass.
func TestRequireGoldenFileFailsOnMissingBaseline(t *testing.T) {
	session := harness.New(goldenProps("#69e3c7", 0), goldenOptions())
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RequireGoldenFile("absent", filepath.Join(t.TempDir(), "nope.png"), imagediff.Options{}); err == nil {
		t.Fatal("expected a read error for a missing baseline")
	}
	if err := session.Validate(); err == nil {
		t.Fatal("a missing baseline must invalidate the report")
	}
}

func TestRequireGoldenFileReadsPNGFromDisk(t *testing.T) {
	props := goldenProps("#8060ff", 0)
	path := filepath.Join(t.TempDir(), "baseline.png")
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, renderReference(t, props)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	session := harness.New(props, goldenOptions())
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	diff, err := session.RequireGoldenFile("disk baseline", path, imagediff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Identical {
		t.Fatalf("a PNG round trip must not change pixels: %s", diff.Summary())
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestCertifyDeterminismRunsRealRepeats proves the determinism verdict comes
// from repeated renders instead of an assumption.
func TestCertifyDeterminismRunsRealRepeats(t *testing.T) {
	session := harness.New(goldenProps("#69e3c7", 0), goldenOptions())
	telemetry, err := session.CertifyDeterminism("static frame", 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !telemetry.Identical || telemetry.Runs != 4 || len(telemetry.Divergences) != 0 {
		t.Fatalf("determinism telemetry = %+v", telemetry)
	}
	if telemetry.PixelSHA256 == "" {
		t.Fatal("determinism telemetry must carry the pixel hash it compared")
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestReportContentHashIsStableAndSensitive proves the report hash means what
// a machine consumer needs it to mean.
func TestReportContentHashIsStableAndSensitive(t *testing.T) {
	build := func(props scene.Props) harness.Report {
		session := harness.New(props, goldenOptions())
		if _, err := session.Render(0); err != nil {
			t.Fatal(err)
		}
		session.Trace("down", scene.Ray{Origin: scene.Vec3(0, 4, 0), Direction: scene.Vec3(0, -1, 0)})
		return session.Report()
	}
	first := build(goldenProps("#69e3c7", 0))
	second := build(goldenProps("#69e3c7", 0))
	if first.ContentHash == "" || first.ContentHash != second.ContentHash {
		t.Fatalf("identical sessions must hash the same: %s vs %s", first.ContentHash, second.ContentHash)
	}
	changed := build(goldenProps("#ff2211", 0))
	if changed.ContentHash == first.ContentHash {
		t.Fatal("a changed material must change the report hash")
	}
}

// TestWriteJSONIsByteIdenticalForIdenticalSessions pins the report's value as a
// machine interface: stable keys, stable order, no wall-clock timings.
func TestWriteJSONIsByteIdenticalForIdenticalSessions(t *testing.T) {
	encode := func() []byte {
		session := harness.New(goldenProps("#69e3c7", 0), goldenOptions())
		if _, err := session.Render(0); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Render(0.5); err != nil {
			t.Fatal(err)
		}
		session.Trace("probe", scene.Ray{Origin: scene.Vec3(0, 4, 0), Direction: scene.Vec3(0, -1, 0)})
		var out bytes.Buffer
		if err := session.WriteJSON(&out); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	first, second := encode(), encode()
	if !bytes.Equal(first, second) {
		t.Fatalf("session JSON is not reproducible:\n%s\n---\n%s", first, second)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["contentHash"]; !ok {
		t.Fatalf("report must publish contentHash: %s", first)
	}
	events, _ := decoded["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("expected three events, got %d", len(events))
	}
	for index, raw := range events {
		event, _ := raw.(map[string]any)
		if sequence, _ := event["sequence"].(float64); int(sequence) != index+1 {
			t.Fatalf("event %d has sequence %v; sequence must number events from one", index, event["sequence"])
		}
	}
}

// TestJSONSessionRendersAndRefusesToFakeATrace proves a JSON session reaches
// the same rasterizer, and that it reports the missing typed graph rather than
// returning an empty hit list that would read as a real miss.
func TestJSONSessionRendersAndRefusesToFakeATrace(t *testing.T) {
	document := []byte(`{"schema":"gosx.scene3d.ir.v1",
		"objects":[{"id":"hero","kind":"cube","size":2,"color":"#69e3c7"}],
		"lights":[{"id":"key","kind":"directional","directionY":-1,"intensity":1}]}`)
	session, err := harness.NewFromJSON(document, goldenOptions())
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Render(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bundle.InstancedMeshes) != 1 {
		t.Fatalf("JSON session bundle = %+v", result.Bundle.InstancedMeshes)
	}
	report := session.Report()
	if report.Scene.Objects != 1 || report.Scene.Lights != 1 {
		t.Fatalf("JSON session summary = %+v", report.Scene)
	}
	if report.Events[0].Frame.Coverage <= 0 {
		t.Fatalf("JSON session frame telemetry = %+v", report.Events[0].Frame)
	}

	session.Trace("down", scene.Ray{Origin: scene.Vec3(0, 4, 0), Direction: scene.Vec3(0, -1, 0)})
	if err := session.Validate(); err == nil {
		t.Fatal("a trace without a typed graph must invalidate the report")
	}
	if !strings.Contains(strings.Join(session.Report().Problems, " "), "no typed scene graph") {
		t.Fatalf("problem must name the cause: %v", session.Report().Problems)
	}
}
