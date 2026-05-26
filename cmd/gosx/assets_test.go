package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe"
)

func TestRunAssetsPlanCommandJSON(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "public", "env", "studio.hdr"), "#?RADIANCE\n")

	var out bytes.Buffer
	if err := runAssetsCommand([]string{"plan", "--json", dir}, &out); err != nil {
		t.Fatal(err)
	}
	var report assetpipe.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if report.Totals.Assets != 1 || report.Totals.Environment != 1 {
		t.Fatalf("unexpected totals: %+v", report.Totals)
	}
	if got := report.Assets[0].Path; got != "public/env/studio.hdr" {
		t.Fatalf("asset path = %q", got)
	}
}

func TestRunAssetsPlanCommandHumanSummary(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "public", "audio", "hit.ogg"), "ogg")

	var out bytes.Buffer
	if err := runAssetsCommand([]string{"plan", dir}, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, snippet := range []string{
		"Scene asset plan: 1 assets",
		"public/audio/hit.ogg [audio",
		"positional-audio-node",
		"public/audio/hit.opus",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected %q in output:\n%s", snippet, text)
		}
	}
}

func TestWriteBuildSceneAssetPlanWritesDistReport(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(projectDir, "public", "textures", "albedo.png"), "png")

	report, err := writeBuildSceneAssetPlan(projectDir, distDir)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || report.Totals.Texture != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	data := readFile(t, filepath.Join(distDir, "scene-assets.json"))
	var decoded assetpipe.Report
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Assets[0].Path != "textures/albedo.png" {
		t.Fatalf("dist report path = %q", decoded.Assets[0].Path)
	}
}
