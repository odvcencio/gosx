package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// posterableScene draws two lit primitives that the CPU rasterizer builds in
// full. Every test that expects a written poster uses it, so a refusal means the
// gate changed rather than the scene.
const posterableScene = `{
	"schema":"gosx.scene3d.ir.v1",
	"objects":[
		{"id":"floor","kind":"plane","width":10,"height":10,"rotationX":-1.5708,"y":-1.2,"materialKind":"matte","color":"#2b3440"},
		{"id":"hero","kind":"sphere","radius":1.1,"segments":24,"y":0.1,"materialKind":"standard","color":"#ff8a5b","roughness":0.45}
	],
	"lights":[{"id":"key","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.3}],
	"environment":{"ambientColor":"#ffffff","ambientIntensity":0.35}
}`

// blankPosterScene renders nothing the camera can see. The default camera sits
// at z=7 looking down -Z, and this object sits far behind it.
const blankPosterScene = `{
	"schema":"gosx.scene3d.ir.v1",
	"objects":[{"id":"hidden","kind":"cube","size":1,"z":600}],
	"lights":[{"id":"key","kind":"directional","directionY":-1,"intensity":1.2}]
}`

// droppedPosterScene draws one primitive and carries three record kinds the CPU
// rasterizer cannot draw.
const droppedPosterScene = `{
	"schema":"gosx.scene3d.ir.v1",
	"objects":[
		{"id":"floor","kind":"plane","width":10,"height":10,"rotationX":-1.5708,"y":-1.2,"materialKind":"matte","color":"#2b3440"},
		{"id":"hero","kind":"sphere","radius":1.1,"segments":24,"y":0.1,"materialKind":"standard","color":"#ff8a5b","roughness":0.45}
	],
	"points":[{"id":"stars","count":3,"size":0.4,"color":"#ffffff","positions":[0,0,0, 1,0,0, -1,0,0]}],
	"labels":[{"id":"caption","text":"Hero"}],
	"lights":[{"id":"key","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.3}],
	"environment":{"ambientColor":"#ffffff","ambientIntensity":0.35}
}`

func writePosterFixture(t *testing.T, name, document string) (dir, scenePath string) {
	t.Helper()
	dir = t.TempDir()
	scenePath = filepath.Join(dir, name)
	mustWriteFile(t, scenePath, document)
	return dir, scenePath
}

func TestRunScenePosterWritesADrawnScene(t *testing.T) {
	dir, scenePath := writePosterFixture(t, "hero.scene.json", posterableScene)
	out := filepath.Join(dir, "posters", "hero.png")

	var stdout bytes.Buffer
	err := runSceneCommand([]string{"poster", "--out", out, "--width", "160", "--height", "120", scenePath}, &stdout)
	if err != nil {
		t.Fatalf("poster run failed: %v\n%s", err, stdout.String())
	}
	for _, want := range []string{"Scene3D poster: pass", "Fidelity: pass", "Wrote: " + out} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in output:\n%s", want, stdout.String())
		}
	}
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("poster was not written: %v", readErr)
	}
	if !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("poster is not a PNG: first bytes %x", data[:8])
	}
}

// TestRunScenePosterRefusesABlankFrame proves the command does not ship an
// image that reads as success and shows nothing. It must also leave no file.
func TestRunScenePosterRefusesABlankFrame(t *testing.T) {
	dir, scenePath := writePosterFixture(t, "blank.scene.json", blankPosterScene)
	out := filepath.Join(dir, "posters", "blank.png")

	var stdout bytes.Buffer
	err := runSceneCommand([]string{"poster", "--out", out, "--width", "128", "--height", "96", scenePath}, &stdout)
	if err == nil {
		t.Fatalf("a blank poster exited zero:\n%s", stdout.String())
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("a refused poster left a file at %s", out)
	}
	text := stdout.String()
	if !strings.Contains(text, "Scene3D poster: fail") || !strings.Contains(text, "Fidelity: fail") {
		t.Fatalf("the refusal is not visible in the output:\n%s", text)
	}
	// Report every failed metric, not only the first.
	for _, want := range []string{"coverage", "unique colours", "luminance variance"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the refusal does not name %q:\n%s", want, text)
		}
	}
}

// TestRunScenePosterRefusesDroppedRecords proves the fidelity gate reads the
// coverage diagnostics that scene/preview already emits.
func TestRunScenePosterRefusesDroppedRecords(t *testing.T) {
	dir, scenePath := writePosterFixture(t, "dropped.scene.json", droppedPosterScene)
	out := filepath.Join(dir, "posters", "dropped.png")

	var stdout bytes.Buffer
	err := runSceneCommand([]string{"poster", "--out", out, "--width", "160", "--height", "120", scenePath}, &stdout)
	if err == nil {
		t.Fatalf("a poster that dropped records exited zero:\n%s", stdout.String())
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("a refused poster left a file at %s", out)
	}
	text := stdout.String()
	for _, want := range []string{"scene.preview.unsupported_points", "scene.preview.unsupported_label", "dropped"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the refusal does not name %q:\n%s", want, text)
		}
	}

	// The same scene must publish when the author reads the drops and decides
	// the poster still tells the truth.
	var allowed bytes.Buffer
	err = runSceneCommand([]string{"poster", "--allow-dropped", "--out", out,
		"--width", "160", "--height", "120", scenePath}, &allowed)
	if err != nil {
		t.Fatalf("--allow-dropped did not publish: %v\n%s", err, allowed.String())
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("--allow-dropped wrote no poster: %v", statErr)
	}
	// The drops must stay visible even when the gate lets them through.
	if !strings.Contains(allowed.String(), "scene.preview.unsupported_points") {
		t.Fatalf("--allow-dropped hid the dropped records:\n%s", allowed.String())
	}
}

// TestRunScenePosterCertifiesPixelsAndBytes proves both halves of the
// reproducibility claim reach the report.
func TestRunScenePosterCertifiesPixelsAndBytes(t *testing.T) {
	dir, scenePath := writePosterFixture(t, "hero.scene.json", posterableScene)
	out := filepath.Join(dir, "hero.png")

	var stdout bytes.Buffer
	err := runSceneCommand([]string{"poster", "--json", "--repeat", "3", "--out", out,
		"--width", "128", "--height", "96", scenePath}, &stdout)
	if err != nil {
		t.Fatalf("poster run failed: %v\n%s", err, stdout.String())
	}
	var report scenePosterReport
	if decodeErr := json.Unmarshal(stdout.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode report: %v\n%s", decodeErr, stdout.String())
	}
	if report.Schema != scenePosterSchema || !report.Valid {
		t.Fatalf("unexpected report head: schema=%q valid=%v", report.Schema, report.Valid)
	}
	if len(report.Posters) != 1 {
		t.Fatalf("expected one poster, got %d", len(report.Posters))
	}
	entry := report.Posters[0]
	if entry.Repeat == nil {
		t.Fatal("the report carries no repeat evidence")
	}
	if !entry.Repeat.PixelIdentical || !entry.Repeat.ByteIdentical {
		t.Fatalf("repeat evidence: pixels=%v bytes=%v divergence=%q",
			entry.Repeat.PixelIdentical, entry.Repeat.ByteIdentical, entry.Repeat.Divergence)
	}
	if entry.Repeat.Runs != 3 {
		t.Fatalf("runs = %d, want 3", entry.Repeat.Runs)
	}
	if !report.Steps.PixelCertified || !report.Steps.ByteCertified {
		t.Fatalf("steps did not record both certificates: %+v", report.Steps)
	}
	// The reported hash must be the hash of the file on disk. A report hash that
	// described some other bytes would be worse than no hash.
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != entry.SHA256 {
		t.Fatalf("report hash %s does not match the file on disk %s",
			entry.SHA256, hex.EncodeToString(sum[:]))
	}
	if entry.Fidelity.Metrics.Coverage <= 0 || entry.Fidelity.Metrics.UniqueColors < 8 {
		t.Fatalf("frame metrics look empty: %+v", entry.Fidelity.Metrics)
	}
}

// TestRunScenePosterRendersADirectory covers the build case: one command, many
// scenes, one total cost.
func TestRunScenePosterRendersADirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.scene.json", "b.scene.json", "c.scene.json"} {
		mustWriteFile(t, filepath.Join(dir, name), posterableScene)
	}
	outDir := filepath.Join(dir, "posters")

	var stdout bytes.Buffer
	err := runSceneCommand([]string{"poster", "--json", "--out-dir", outDir,
		"--width", "96", "--height", "72", dir}, &stdout)
	if err != nil {
		t.Fatalf("directory run failed: %v\n%s", err, stdout.String())
	}
	var report scenePosterReport
	if decodeErr := json.Unmarshal(stdout.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode report: %v", decodeErr)
	}
	if report.Steps.Scenes != 3 || report.Steps.Written != 3 {
		t.Fatalf("steps = %+v, want 3 scenes and 3 written", report.Steps)
	}
	if report.Totals.Bytes <= 0 || report.Totals.WallTimeMS <= 0 {
		t.Fatalf("totals look unmeasured: %+v", report.Totals)
	}
	for _, name := range []string{"a.poster.png", "b.poster.png", "c.poster.png"} {
		if _, statErr := os.Stat(filepath.Join(outDir, name)); statErr != nil {
			t.Fatalf("missing %s: %v", name, statErr)
		}
	}
	// Identical scenes must produce identical posters, so a build can share one
	// cache entry across routes that show the same surface.
	first := report.Posters[0].SHA256
	for _, entry := range report.Posters[1:] {
		if entry.SHA256 != first {
			t.Fatalf("identical scenes produced different posters: %s vs %s", first, entry.SHA256)
		}
	}
}

// TestRunScenePosterGatesWithoutWriting lets continuous integration ask "would
// this poster be honest" without producing an artifact.
func TestRunScenePosterGatesWithoutWriting(t *testing.T) {
	_, scenePath := writePosterFixture(t, "hero.scene.json", posterableScene)
	var stdout bytes.Buffer
	err := runSceneCommand([]string{"poster", "--width", "96", "--height", "72", scenePath}, &stdout)
	if err != nil {
		t.Fatalf("gate-only run failed: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "Wrote nothing") {
		t.Fatalf("a gate-only run must say it wrote nothing:\n%s", stdout.String())
	}
}

func TestScenePosterUsageIsReachable(t *testing.T) {
	var stdout bytes.Buffer
	if err := runSceneCommand([]string{"poster", "--help"}, &stdout); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gosx scene poster", "--repeat", "--allow-dropped", "t=0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("usage does not mention %q:\n%s", want, stdout.String())
		}
	}
	var root bytes.Buffer
	if err := runSceneCommand(nil, &root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(root.String(), "gosx scene poster") {
		t.Fatalf("scene usage does not list the poster subcommand:\n%s", root.String())
	}
}
