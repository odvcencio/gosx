package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/cert"
	sceneschema "m31labs.dev/gosx/scene/schema"
)

func TestRunSceneCertifyCommandJSON(t *testing.T) {
	var out bytes.Buffer
	if err := runSceneCommand([]string{"certify", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	var report cert.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if report.Schema != cert.Schema {
		t.Fatalf("schema = %q", report.Schema)
	}
	if report.Summary.Features == 0 {
		t.Fatal("expected features in certification report")
	}
}

func TestRunSceneCertifyStrictPasses(t *testing.T) {
	var out bytes.Buffer
	if err := runSceneCommand([]string{"certify", "--strict"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Strict gate: pass") {
		t.Fatalf("expected strict pass output:\n%s", out.String())
	}
}

func TestRunSceneSchemaCommand(t *testing.T) {
	var out bytes.Buffer
	if err := runSceneCommand([]string{"schema"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"title": "GoSX Scene3D IR"`) {
		t.Fatalf("expected schema JSON on stdout:\n%s", out.String())
	}

	outPath := filepath.Join(t.TempDir(), "scene-schema.json")
	if err := runSceneCommand([]string{"schema", "--out", outPath}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"const": "gosx.scene3d.ir.v1"`) {
		t.Fatalf("expected schema JSON in output file:\n%s", string(data))
	}
}

func TestRunSceneValidateCommandJSON(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "demo.scene.json"), `{
		"objects":[{"id":"cube","kind":"cube","size":1}],
		"html":[{"id":"panel","mode":"texture","html":"<b>Panel</b>","fallback":"Panel","textureWidth":64,"textureHeight":64}]
	}`)

	var out bytes.Buffer
	if err := runSceneCommand([]string{"validate", "--json", "--strict", dir}, &out); err != nil {
		t.Fatal(err)
	}
	var report sceneValidationCommandReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if !report.Valid || len(report.Files) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Summary.Files != 1 || report.Summary.Passed != 1 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if report.Summary.Diagnostics != 0 {
		t.Fatalf("expected no diagnostics, got summary: %+v", report.Summary)
	}
	for _, severity := range []sceneschema.Severity{sceneschema.Info, sceneschema.Warn, sceneschema.Error, sceneschema.Fatal} {
		if _, ok := report.Summary.SeverityCounts[severity]; !ok {
			t.Fatalf("expected %s severity count in summary: %+v", severity, report.Summary)
		}
	}
}

func TestRunSceneValidateCommandFailsInvalidScene(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "scene.json")
	mustWriteFile(t, file, `{"objects":[{"id":"dup","kind":"cube"},{"id":"dup","kind":"sphere"}]}`)

	var out bytes.Buffer
	err := runSceneCommand([]string{"validate", file}, &out)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(out.String(), "scene.id.duplicate") {
		t.Fatalf("expected duplicate diagnostic in output:\n%s", out.String())
	}
}

func TestRunSceneValidateCommandHumanSummary(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.scene.json")
	invalid := filepath.Join(dir, "invalid.scene.json")
	mustWriteFile(t, valid, `{"objects":[{"id":"cube","kind":"cube","size":1}]}`)
	mustWriteFile(t, invalid, `{"objects":[{"id":"dup","kind":"cube"},{"id":"dup","kind":"sphere"}]}`)

	var out bytes.Buffer
	err := runSceneCommand([]string{"validate", dir}, &out)
	if err == nil {
		t.Fatal("expected validation error")
	}
	text := out.String()
	for _, want := range []string{
		"Scene3D validation: fail",
		"Files: 1 passed, 1 failed, 2 total",
		"Diagnostics: 1 total, info=0, warn=0, error=1, fatal=0",
		"scene.id.duplicate",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output:\n%s", want, text)
		}
	}
}

func TestRunSceneValidateCommandJSONSummary(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "scene.json")
	mustWriteFile(t, file, `{"objects":[{"id":"dup","kind":"cube"},{"id":"dup","kind":"sphere"}]}`)

	var out bytes.Buffer
	err := runSceneCommand([]string{"validate", "--json", file}, &out)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var report sceneValidationCommandReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if report.Valid {
		t.Fatalf("expected invalid report: %+v", report)
	}
	if report.Summary.Files != 1 || report.Summary.Passed != 0 || report.Summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if report.Summary.Diagnostics != 1 || report.Summary.SeverityCounts[sceneschema.Error] != 1 {
		t.Fatalf("unexpected diagnostic counts: %+v", report.Summary)
	}
}

func TestRunSceneInspectCommandJSON(t *testing.T) {
	dir := t.TempDir()
	assetsDir := filepath.Join(dir, "public")
	mustWriteFile(t, filepath.Join(assetsDir, "textures", "albedo.png"), "png")
	mustWriteFile(t, filepath.Join(assetsDir, "models", "ship.glb"), "glb")
	scenePath := filepath.Join(dir, "product.scene.json")
	mustWriteFile(t, scenePath, `{
		"schema":"gosx.scene3d.ir.v1",
		"objects":[{"id":"cube","kind":"cube","texture":"/textures/albedo.png"}],
		"models":[{"id":"ship","src":"/models/ship.glb"}],
		"points":[{"id":"sparkles","count":4}],
		"instancedMeshes":[{"id":"parts","kind":"torus","count":3}],
		"html":[{"id":"panel","target":"cube","mode":"texture","html":"<p>Status</p>","fallback":"Status","textureWidth":64,"textureHeight":64}],
		"lights":[{"id":"sun","kind":"directional","castShadow":true,"shadowSize":128}],
		"postEffects":[{"kind":"bloom"}]
	}`)

	var out bytes.Buffer
	if err := runSceneCommand([]string{"inspect", "--json", "--cert", "--assets", assetsDir, scenePath}, &out); err != nil {
		t.Fatal(err)
	}
	var report sceneInspectionCommandReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if !report.Valid || len(report.Scenes) != 1 {
		t.Fatalf("unexpected inspection report: %+v", report)
	}
	if report.Certification == nil || report.Certification.Summary.Features == 0 {
		t.Fatalf("expected certification report: %+v", report.Certification)
	}
	if report.AssetPlan == nil || report.AssetPlan.Totals.Assets != 2 {
		t.Fatalf("expected asset plan with two assets: %+v", report.AssetPlan)
	}
	sceneReport := report.Scenes[0]
	if sceneReport.Surface.Objects != 1 || sceneReport.Surface.Models != 1 || sceneReport.Surface.HTML != 1 {
		t.Fatalf("unexpected surface summary: %+v", sceneReport.Surface)
	}
	if sceneReport.FeatureUse["html.texture"] != 1 || sceneReport.FeatureUse["postfx.bloom"] != 1 {
		t.Fatalf("unexpected feature use: %+v", sceneReport.FeatureUse)
	}
	if sceneReport.Memory.TotalGPUBytes == 0 {
		t.Fatalf("expected non-zero GPU memory estimate: %+v", sceneReport.Memory)
	}
	if len(sceneReport.Fallbacks) != 1 || sceneReport.Fallbacks[0].Feature != "html.texture" {
		t.Fatalf("expected HTML texture fallback: %+v", sceneReport.Fallbacks)
	}
}

func TestRunSceneInspectBudgetFails(t *testing.T) {
	dir := t.TempDir()
	scenePath := filepath.Join(dir, "scene.json")
	budgetPath := filepath.Join(dir, "scene-budget.json")
	mustWriteFile(t, scenePath, `{"objects":[{"id":"cube","kind":"cube"}]}`)
	mustWriteFile(t, budgetPath, `{"scene3d":{"maxInitialGPUBytes":1}}`)

	var out bytes.Buffer
	err := runSceneCommand([]string{"inspect", "--budget", budgetPath, scenePath}, &out)
	if err == nil {
		t.Fatal("expected budget failure")
	}
	text := out.String()
	for _, want := range []string{"Scene3D inspection: fail", "initialGPUBytes: fail"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output:\n%s", want, text)
		}
	}
}
