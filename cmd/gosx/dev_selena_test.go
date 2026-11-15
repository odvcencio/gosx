package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevSelenaChangeRebuildsAndReturnsNewShaders(t *testing.T) {
	dir := writeSelenaProject(t, "")
	if err := devSelenaBuild(dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	hook := devSelenaChange(dir)
	if hook == nil {
		t.Fatal("no hook for a project with Selena enabled")
	}
	source := filepath.Join(dir, "materials", "panel.sel")
	edited := strings.Replace(selenaCommandSource, "return tint", "return tint * 0.5", 1)
	if err := os.WriteFile(source, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	updates, err := hook([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Source != "materials/panel.sel" || len(updates[0].Materials) != 1 {
		t.Fatalf("updates = %+v", updates)
	}
	m := updates[0].Materials[0]
	if m["material"] != "Panel" {
		t.Fatalf("material = %v", m["material"])
	}
	frag, _ := m["customFragment"].(string)
	wgsl, _ := m["customFragmentWGSL"].(string)
	if !strings.Contains(frag, "0.5") || !strings.Contains(wgsl, "0.5") {
		t.Fatalf("the update does not carry the edited shader:\nGLSL %s\nWGSL %s", frag, wgsl)
	}
	if _, ok := m["shaderLayout"].(map[string]any); !ok {
		t.Fatalf("shaderLayout = %T", m["shaderLayout"])
	}
	// The artifact on disk is rebuilt too, so the next app restart embeds it.
	data, err := os.ReadFile(source + ".json")
	if err != nil || !strings.Contains(string(data), "0.5") {
		t.Fatalf("artifact not rebuilt: %v", err)
	}

	broken := strings.Replace(selenaCommandSource, "return tint", "return missing", 1)
	if err := os.WriteFile(source, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hook([]string{source}); err == nil || !strings.Contains(err.Error(), "SEL2001") {
		t.Fatalf("broken source error = %v, want SEL2001", err)
	}
}

func TestDevSelenaChangeDisabledFallsBackToReload(t *testing.T) {
	dir := writeSelenaProject(t, `{"build":{"selena":{"disabled":true}}}`)
	if hook := devSelenaChange(dir); hook != nil {
		t.Fatal("build.selena.disabled must leave .sel saves to the reload path")
	}
}

// A cold gosx dev start must not pay for Selena when the app has no .sel
// files: the step finds no sources, writes nothing and prints nothing.
func TestDevSelenaBuildIsInertWithoutSelSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := listTree(t, dir)
	var out strings.Builder
	if err := devSelenaBuild(dir, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("Selena printed for an app with no .sel files: %q", out.String())
	}
	if after := listTree(t, dir); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("Selena changed the tree of an app with no .sel files:\nbefore %v\nafter  %v", before, after)
	}
}

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err == nil {
			names = append(names, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestDevStageLogReportsEachStage(t *testing.T) {
	var out strings.Builder
	log := newDevStageLog(&out)
	log.done("first")
	log.done("second")
	text := out.String()
	for _, want := range []string{"gosx dev: starting", "gosx dev: first (", "gosx dev: second (", "since start)"} {
		if !strings.Contains(text, want) {
			t.Errorf("stage log lacks %q:\n%s", want, text)
		}
	}
}
