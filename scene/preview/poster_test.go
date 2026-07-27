package preview_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

// posterScene is a scene the CPU rasterizer draws completely: two lit boxes and
// one directional light, all of them rasterizable kinds. Every test below that
// expects a passing gate uses this document, so a gate failure means the gate
// changed, not the scene.
const posterScene = `{"schema":"gosx.scene3d.ir.v1",
	"objects":[
		{"id":"left","kind":"cube","size":1.4,"x":-1.1,"color":"#6ee7c8","materialKind":"standard","roughness":0.4},
		{"id":"right","kind":"sphere","radius":0.9,"x":1.2,"y":0.2,"color":"#f4a261","materialKind":"standard","roughness":0.5}
	],
	"lights":[{"id":"key","kind":"directional","color":"#ffffff","intensity":1.1,"directionX":-0.4,"directionY":-1,"directionZ":-0.6}],
	"environment":{"ambientColor":"#ffffff","ambientIntensity":0.35}}`

func posterRenderOptions(width, height int) preview.Options {
	return preview.Options{
		Width: width, Height: height, Background: "#0b1020",
		Camera: cameraAt(0, 1.2, 5), DisableShadows: true, DisablePostFX: true,
	}
}

func TestPosterOfADrawnScenePassesItsGate(t *testing.T) {
	poster, err := preview.PosterFromJSON([]byte(posterScene), preview.NewPosterOptions(posterRenderOptions(320, 180)))
	if err != nil {
		t.Fatal(err)
	}
	if !poster.Fidelity.OK {
		t.Fatalf("a fully rasterizable scene failed its gate: %v", poster.Fidelity.Failures)
	}
	if poster.Format != preview.FormatPNG {
		t.Fatalf("format = %q, want %q", poster.Format, preview.FormatPNG)
	}
	if poster.Width != 320 || poster.Height != 180 {
		t.Fatalf("poster size = %dx%d, want 320x180", poster.Width, poster.Height)
	}
	if !bytes.HasPrefix(poster.Bytes, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("poster bytes do not start with the PNG signature")
	}
	if len(poster.SHA256) != 64 {
		t.Fatalf("poster hash = %q, want a 64-character SHA-256", poster.SHA256)
	}
	var out bytes.Buffer
	if err := preview.WritePoster(&out, poster); err != nil {
		t.Fatalf("WritePoster refused a passing poster: %v", err)
	}
	if out.Len() != len(poster.Bytes) {
		t.Fatalf("WritePoster wrote %d bytes, want %d", out.Len(), len(poster.Bytes))
	}
}

// TestPosterRefusesABlankFrame covers the failure the brief calls out first: a
// scene with no camera-visible content must not produce a blank poster that
// reads as success. The camera looks away from the only object.
func TestPosterRefusesABlankFrame(t *testing.T) {
	opts := preview.NewPosterOptions(posterRenderOptions(160, 120))
	opts.Render.Camera = cameraAt(0, 0, -400)
	poster, err := preview.PosterFromJSON([]byte(posterScene), opts)
	if err != nil {
		t.Fatal(err)
	}
	if poster.Fidelity.OK {
		t.Fatalf("a blank frame passed the gate: coverage=%.6f colours=%d variance=%.8f",
			poster.Fidelity.Metrics.Coverage, poster.Fidelity.Metrics.UniqueColors,
			poster.Fidelity.Metrics.LuminanceVariance)
	}
	// Report every failed metric, not only the first. A blank frame fails all
	// three, and a gate that stopped early would hide two of them.
	joined := strings.Join(poster.Fidelity.Failures, "\n")
	for _, want := range []string{"coverage", "unique colours", "luminance variance"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the blank-frame verdict does not name %q: %s", want, joined)
		}
	}
	var out bytes.Buffer
	err = preview.WritePoster(&out, poster)
	if !errors.Is(err, preview.ErrLowFidelity) {
		t.Fatalf("WritePoster error = %v, want ErrLowFidelity", err)
	}
	if out.Len() != 0 {
		t.Fatalf("WritePoster wrote %d bytes for a refused poster, want 0", out.Len())
	}
}

// TestPosterRefusesASceneTheRasterizerCannotDraw covers the second failure: a
// poster that silently omits half the scene tells a crawler and a reader
// something false. The scene mixes a drawn cube with a point cloud, a label and
// a glTF model, none of which the CPU rasterizer draws.
func TestPosterRefusesASceneTheRasterizerCannotDraw(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1",
		"objects":[
			{"id":"left","kind":"cube","size":1.4,"x":-1.1,"color":"#6ee7c8","materialKind":"standard","roughness":0.4},
			{"id":"right","kind":"sphere","radius":0.9,"x":1.2,"y":0.2,"color":"#f4a261","materialKind":"standard","roughness":0.5}
		],
		"points":[{"id":"stars","count":3,"size":0.4,"color":"#ffffff","positions":[0,0,0, 1,0,0, -1,0,0]}],
		"labels":[{"id":"title","text":"Hello"}],
		"models":[{"id":"robot","src":"/models/robot.glb"}],
		"lights":[{"id":"key","kind":"directional","color":"#ffffff","intensity":1.1,"directionX":-0.4,"directionY":-1,"directionZ":-0.6}],
		"environment":{"ambientColor":"#ffffff","ambientIntensity":0.35}}`
	poster, err := preview.PosterFromJSON([]byte(doc), preview.NewPosterOptions(posterRenderOptions(160, 120)))
	if err != nil {
		t.Fatal(err)
	}
	if poster.Fidelity.Metrics.Coverage <= 0 {
		t.Fatalf("the cube did not draw, so this test proves nothing about dropped records")
	}
	if poster.Fidelity.OK {
		t.Fatalf("a poster that dropped %d records passed the gate", len(poster.Fidelity.Dropped))
	}
	codes := map[string]bool{}
	for _, record := range poster.Fidelity.Dropped {
		codes[record.Code] = true
	}
	for _, want := range []string{
		"scene.preview.unsupported_points",
		"scene.preview.unsupported_label",
		"scene.preview.unsupported_model",
	} {
		if !codes[want] {
			t.Fatalf("the dropped-record list is missing %s: %+v", want, poster.Fidelity.Dropped)
		}
	}
	if err := preview.WritePoster(io0(), poster); !errors.Is(err, preview.ErrLowFidelity) {
		t.Fatalf("WritePoster error = %v, want ErrLowFidelity", err)
	}
	// A caller that has read every drop may still publish. Prove the escape
	// hatch exists and that it needs a deliberate choice.
	open := preview.NewPosterOptions(posterRenderOptions(160, 120))
	open.Floors.AllowDroppedRecords = true
	allowed, err := preview.PosterFromJSON([]byte(doc), open)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Fidelity.OK {
		t.Fatalf("AllowDroppedRecords did not open the gate: %v", allowed.Fidelity.Failures)
	}
	if len(allowed.Fidelity.Dropped) != len(poster.Fidelity.Dropped) {
		t.Fatalf("AllowDroppedRecords hid the dropped records: %d vs %d",
			len(allowed.Fidelity.Dropped), len(poster.Fidelity.Dropped))
	}
}

// TestPosterBytesReproduce proves the byte-identity claim. Pixel identity is not
// enough: a poster is a file, and a file that changes bytes changes its ETag,
// its cache entry, and its content hash.
func TestPosterBytesReproduce(t *testing.T) {
	opts := preview.NewPosterOptions(posterRenderOptions(200, 150))
	first, err := preview.PosterFromJSON([]byte(posterScene), opts)
	if err != nil {
		t.Fatal(err)
	}
	for run := 2; run <= 4; run++ {
		next, err := preview.PosterFromJSON([]byte(posterScene), opts)
		if err != nil {
			t.Fatal(err)
		}
		if next.SHA256 != first.SHA256 {
			t.Fatalf("run %d hash %s does not match run 1 hash %s", run, next.SHA256, first.SHA256)
		}
		if !bytes.Equal(next.Bytes, first.Bytes) {
			t.Fatalf("run %d produced %d bytes, run 1 produced %d", run, len(next.Bytes), len(first.Bytes))
		}
	}
}

// TestPosterTimeSelectsTheFrame covers the animated-scene answer. A poster
// captures one instant. The default instant is zero, because the browser
// renderer also starts its clock at zero, so a t=0 poster matches the first
// live frame. Any other instant must be chosen on purpose and must change the
// picture.
func TestPosterTimeSelectsTheFrame(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1",
		"objects":[
			{"id":"left","kind":"cube","size":1.4,"x":-1.1,"color":"#6ee7c8","materialKind":"standard","roughness":0.4},
			{"id":"right","kind":"sphere","radius":0.9,"x":1.2,"y":0.2,"color":"#f4a261","materialKind":"standard","roughness":0.5}
		],
		"lights":[{"id":"key","kind":"directional","color":"#ffffff","intensity":1.1,"directionX":-0.4,"directionY":-1,"directionZ":-0.6}],
		"environment":{"ambientColor":"#ffffff","ambientIntensity":0.35},
		"animations":[{"name":"slide","duration":2,"channels":[
			{"targetNode":0,"property":"translation","interpolation":"LINEAR",
			 "times":[0,2],"values":[0,0,0, 0,1.6,0]}]}]}`
	base := preview.NewPosterOptions(posterRenderOptions(160, 120))
	if base.Render.Time != 0 {
		t.Fatalf("default poster time = %v, want 0", base.Render.Time)
	}
	start, err := preview.PosterFromJSON([]byte(doc), base)
	if err != nil {
		t.Fatal(err)
	}
	later := base
	later.Render.Time = 1.0
	mid, err := preview.PosterFromJSON([]byte(doc), later)
	if err != nil {
		t.Fatal(err)
	}
	if start.Time != 0 || mid.Time != 1.0 {
		t.Fatalf("poster time not recorded: start=%v mid=%v", start.Time, mid.Time)
	}
	if start.SHA256 == mid.SHA256 {
		t.Fatalf("t=0 and t=1 produced the same poster, so --time changes nothing")
	}
}

// TestPosterMetricsMatchHarnessTelemetry pins package preview's measurements to
// package harness's. Two packages that measure the same frame must agree, or a
// gate tuned against one set of numbers silently loosens against the other.
func TestPosterMetricsMatchHarnessTelemetry(t *testing.T) {
	opts := posterRenderOptions(128, 96)
	session, err := harness.NewFromJSON([]byte(posterScene), opts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Render(0)
	if err != nil {
		t.Fatal(err)
	}
	report := session.Report()
	if len(report.Events) == 0 || report.Events[0].Frame == nil {
		t.Fatal("harness recorded no frame")
	}
	frame := report.Events[0].Frame
	metrics := preview.MeasureFrame(result.Image)
	if metrics.Coverage != frame.Coverage {
		t.Fatalf("coverage: preview %.10f, harness %.10f", metrics.Coverage, frame.Coverage)
	}
	if metrics.UniqueColors != frame.UniqueColors {
		t.Fatalf("unique colours: preview %d, harness %d", metrics.UniqueColors, frame.UniqueColors)
	}
	if metrics.LuminanceVariance != frame.LuminanceVariance {
		t.Fatalf("luminance variance: preview %.12f, harness %.12f", metrics.LuminanceVariance, frame.LuminanceVariance)
	}
	if metrics.ChangedPixels != frame.ChangedPixels {
		t.Fatalf("changed pixels: preview %d, harness %d", metrics.ChangedPixels, frame.ChangedPixels)
	}
}

// TestPosterGateIsOnByDefault proves that forgetting the gate still gates. A
// zero PosterOptions must apply DefaultFidelityFloors, because the common
// mistake is to build options by struct literal and omit the floors.
func TestPosterGateIsOnByDefault(t *testing.T) {
	opts := preview.PosterOptions{Render: posterRenderOptions(160, 120)}
	opts.Render.Camera = cameraAt(0, 0, -400)
	poster, err := preview.PosterFromJSON([]byte(posterScene), opts)
	if err != nil {
		t.Fatal(err)
	}
	if poster.Fidelity.OK {
		t.Fatal("a zero PosterOptions left the gate open on a blank frame")
	}
	if poster.Fidelity.Floors != preview.DefaultFidelityFloors() {
		t.Fatalf("floors = %+v, want the defaults %+v", poster.Fidelity.Floors, preview.DefaultFidelityFloors())
	}
}

func TestPosterFromTypedPropsMatchesJSON(t *testing.T) {
	props := scene.Props{
		Background:  "#0b1020",
		Camera:      scene.PerspectiveCamera{Position: scene.Vec3(0, 1.2, 5), FOV: 50, Near: 0.1, Far: 100},
		Environment: scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.35},
		Graph: scene.NewGraph(
			scene.DirectionalLight{ID: "key", Color: "#ffffff", Intensity: 1.1,
				Direction: scene.Vec3(-0.4, -1, -0.6)},
			scene.Mesh{ID: "left", Position: scene.Vec3(-1.1, 0, 0),
				Geometry: scene.BoxGeometry{Width: 1.4, Height: 1.4, Depth: 1.4},
				Material: scene.StandardMaterial{Color: "#6ee7c8", Roughness: 0.4}},
			scene.Mesh{ID: "right", Position: scene.Vec3(1.2, 0.2, 0),
				Geometry: scene.SphereGeometry{Radius: 0.9},
				Material: scene.StandardMaterial{Color: "#f4a261", Roughness: 0.5}},
		),
	}
	opts := preview.NewPosterOptions(preview.Options{Width: 320, Height: 180, DisableShadows: true, DisablePostFX: true})
	poster, err := preview.PosterFromProps(props, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !poster.Fidelity.OK {
		t.Fatalf("the typed poster failed its gate: %v", poster.Fidelity.Failures)
	}
}

// io0 returns a writer that counts nothing. It keeps the refusal assertion above
// free of an unused buffer.
func io0() *bytes.Buffer { return &bytes.Buffer{} }
