package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/selena/artifact"
)

const selenaCommandSource = `material Panel {
    param tint : color = rgb(0.2, 0.6, 0.9)
    surface(geo) -> color {
        return tint
    }
}
`

func writeSelenaProject(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "gosx.config.json"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "materials", "panel.sel"), []byte(selenaCommandSource), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSelenaCommandBuildsAndChecksArtifacts(t *testing.T) {
	dir := writeSelenaProject(t, `{"build":{"selena":{"targets":["wgsl","gles","metal"]}}}`)
	var out bytes.Buffer
	if err := runSelenaCommand([]string{"build", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Selena: 1 artifacts (1 written)") || !strings.Contains(out.String(), "materials/panel.sel.json") {
		t.Fatalf("build output %q", out.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "materials", "panel.sel.json"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := artifact.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Options.Targets) != 3 {
		t.Fatalf("config targets not applied: %v", file.Options.Targets)
	}

	out.Reset()
	if err := runSelenaCommand([]string{"check", dir}, &out); err != nil {
		t.Fatalf("fresh artifacts failed check: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("check output %q", out.String())
	}

	edited := strings.Replace(selenaCommandSource, "0.9", "0.5", 1)
	if err := os.WriteFile(filepath.Join(dir, "materials", "panel.sel"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runSelenaCommand([]string{"check", dir}, &out)
	if err == nil || !strings.Contains(err.Error(), "materials/panel.sel") {
		t.Fatalf("check after an edit = %v, want drift naming the source", err)
	}
}

func TestSelenaBuildStepRespectsDisabledAndEmptyProjects(t *testing.T) {
	dir := writeSelenaProject(t, `{"build":{"selena":{"disabled":true}}}`)
	cfg, err := loadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runSelenaBuildStep(dir, cfg.Build.Selena, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("disabled step printed %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "materials", "panel.sel.json")); !os.IsNotExist(err) {
		t.Fatalf("disabled step wrote an artifact: %v", err)
	}
	if err := runSelenaBuildStep(t.TempDir(), cfg.Build.Selena, &out); err != nil || out.Len() != 0 {
		t.Fatalf("empty project: %v %q", err, out.String())
	}
}

func TestSelenaCommandRejectsBadUsage(t *testing.T) {
	var out bytes.Buffer
	if err := runSelenaCommand([]string{"publish"}, &out); err == nil {
		t.Fatal("accepted an unknown subcommand")
	}
	if err := runSelenaCommand([]string{"build", "a", "b"}, &out); err == nil {
		t.Fatal("accepted extra arguments")
	}
	out.Reset()
	if err := runSelenaCommand(nil, &out); err != nil || !strings.Contains(out.String(), "gosx selena build") {
		t.Fatalf("usage: %v %q", err, out.String())
	}
}
