package surface

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func TestLowerToBytecodeWritesJSON(t *testing.T) {
	cacheDir := t.TempDir()
	sp := &ir.SurfaceProgram{
		Name: "Demo",
		SourceFiles: []ir.SurfaceSourceFile{
			{
				Path: "demo.go",
				Content: []byte(`package handlers

func F() int { return 42 }
`),
			},
		},
	}

	res, err := LowerToBytecode(sp, cacheDir)
	if err != nil {
		t.Fatalf("LowerToBytecode: %v", err)
	}
	if res.JSONPath == "" || res.Hash == "" {
		t.Fatalf("empty result: %+v", res)
	}
	if !strings.HasSuffix(res.JSONURL, ".json") {
		t.Errorf("JSONURL not .json: %s", res.JSONURL)
	}
	if res.SurfaceKind != program.SurfaceCanvas2D {
		t.Errorf("surface kind = %v, want Canvas2D", res.SurfaceKind)
	}

	raw, err := os.ReadFile(res.JSONPath)
	if err != nil {
		t.Fatalf("read JSON: %v", err)
	}
	var got program.Program
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if got.Name != "Demo" {
		t.Errorf("program name = %q, want Demo", got.Name)
	}
	if len(got.Handlers) != 1 {
		t.Errorf("handlers = %d, want 1", len(got.Handlers))
	}
}

func TestLowerToBytecodeHashStableAcrossCalls(t *testing.T) {
	cacheDir := t.TempDir()
	sp := &ir.SurfaceProgram{
		Name: "Stable",
		SourceFiles: []ir.SurfaceSourceFile{
			{Path: "s.go", Content: []byte("package handlers\nfunc F() int { return 1 }\n")},
		},
	}
	r1, err := LowerToBytecode(sp, cacheDir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	r2, err := LowerToBytecode(sp, cacheDir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if r1.Hash != r2.Hash {
		t.Errorf("hash unstable: %s vs %s", r1.Hash, r2.Hash)
	}
}

func TestLowerToBytecodeRejectsNil(t *testing.T) {
	if _, err := LowerToBytecode(nil, t.TempDir()); err == nil {
		t.Error("expected error for nil SurfaceProgram")
	}
}

func TestLowerToBytecodeMergesMultiFilePackages(t *testing.T) {
	cacheDir := t.TempDir()
	sp := &ir.SurfaceProgram{
		Name: "Multi",
		SourceFiles: []ir.SurfaceSourceFile{
			{Path: "a.go", Content: []byte("package handlers\nfunc A() int { return 1 }\n")},
			{Path: "b.go", Content: []byte("package handlers\nfunc B() int { return 2 }\n")},
		},
	}
	res, err := LowerToBytecode(sp, cacheDir)
	if err != nil {
		t.Fatalf("LowerToBytecode: %v", err)
	}
	raw, _ := os.ReadFile(res.JSONPath)
	var got program.Program
	_ = json.Unmarshal(raw, &got)
	if len(got.Handlers) != 2 {
		t.Errorf("handlers = %d, want 2 (one per file)", len(got.Handlers))
	}
}

func TestLowerToBytecodeWritesIntoCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	sp := &ir.SurfaceProgram{
		Name: "Cached",
		SourceFiles: []ir.SurfaceSourceFile{
			{Path: "s.go", Content: []byte("package handlers\nfunc F() int { return 0 }\n")},
		},
	}
	res, err := LowerToBytecode(sp, cacheDir)
	if err != nil {
		t.Fatalf("LowerToBytecode: %v", err)
	}
	rel, err := filepath.Rel(cacheDir, res.JSONPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("JSON written outside cache dir: %s", res.JSONPath)
	}
}
