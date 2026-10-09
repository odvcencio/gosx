package main

import (
	"m31labs.dev/gosx"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/aot"
)

func TestBuildIslandAdmissionOptIn(t *testing.T) {
	calls := 0
	check := func(*ir.Program, int) (aot.Unit, error) { calls++; return aot.Unit{}, nil }
	programs := []*IslandProgramSource{{Candidate: &ir.Program{}, ComponentIndex: 0}}
	for _, backend := range []string{"", "vm"} {
		if err := admitBuildIslands(programs, backend, check); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("default VM build invoked admission")
	}
	if err := admitBuildIslands(programs, "auto", check); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("auto build did not invoke admission once")
	}
	if err := admitBuildIslands(programs, "unknown", check); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

func TestInitStarterIslandAdmission(t *testing.T) {
	files, err := scaffoldFilesForTemplate("example.test/starter", initTemplateApp)
	if err != nil {
		t.Fatal(err)
	}
	islands := 0
	for _, file := range files {
		if !strings.HasSuffix(file.Path, ".gsx") {
			continue
		}
		p, err := gosx.Compile([]byte(file.Contents))
		if err != nil {
			t.Fatal(err)
		}
		p.Dir, p.PackagePath = t.TempDir(), "example.test/starter/app"
		for i, component := range p.Components {
			if component.IsIsland {
				islands++
				if _, err := ir.LowerIslandAOT(p, i); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Logf("starter island count: %d", islands)
}

func TestBuildIslandBackendConfig(t *testing.T) {
	dir := t.TempDir()
	for _, backend := range []string{"vm", "auto", "unknown"} {
		if err := os.WriteFile(filepath.Join(dir, "gosx.config.json"), []byte(`{"build":{"islands":{"backend":"`+backend+`"}}}`), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadProjectConfig(dir)
		if backend == "unknown" {
			if err == nil {
				t.Fatal("unknown backend accepted")
			}
			continue
		}
		if err != nil || cfg.Build.Islands.Backend != backend {
			t.Fatal(cfg, err)
		}
	}
}
