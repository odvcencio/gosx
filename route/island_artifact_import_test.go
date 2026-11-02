package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
)

func TestImportedIslandUsesVerifiedBuildWithoutGoToolchain(t *testing.T) {
	root := t.TempDir()
	source := []byte("package app\nimport model \"example.test/app/model\"\ntype Props struct { Flags map[string]model.Flag }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.Flags[\"missing\"]}</div>\n}\n")
	for name, body := range map[string][]byte{
		"go.mod":         []byte("module example.test/app\n\ngo 1.26\n"),
		"model/types.go": []byte("package model\ntype Flag bool\n"),
		"app/page.gsx":   source,
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = filepath.Join(root, "app"), "example.test/app/app"
	lowered, err := ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := program.EncodeBinary(lowered)
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "dist", "assets", "islands", "Sample.gxi")
	if err := os.MkdirAll(filepath.Dir(artifact), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, data, 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{{Name: "Sample", Format: "bin", SourceFile: "app/page.gsx", SourceHash: buildmanifest.ContentHash(source), HashedAsset: buildmanifest.HashedAsset{File: "Sample.gxi", Hash: buildmanifest.ContentHash(data), Size: int64(len(data))}}}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "build.json"), encoded, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "model", "types.go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	t.Setenv("GOSX_DEV", "")
	t.Setenv("GOSX_ENV", "production")
	renderer := newFileProgramRenderer(p, fileRenderOptions{SourceFile: filepath.Join(root, "app", "page.gsx")})
	got, err := renderer.islandProgram("Sample")
	if err != nil {
		t.Fatal(err)
	}
	if html := island.RenderResolvedHTML(got, vm.ResolveInitialTree(got, `{"Flags":null}`)); html != "<div>false</div>" {
		t.Fatalf("rendered %q", html)
	}
	if err := os.WriteFile(artifact, []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newFileProgramRenderer(p, fileRenderOptions{SourceFile: filepath.Join(root, "app", "page.gsx")}).islandProgram("Sample"); err == nil || !strings.Contains(err.Error(), "hash check") {
		t.Fatalf("tampered artifact error = %v", err)
	}
}
