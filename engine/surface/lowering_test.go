package surface

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
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

func TestLowerToBytecodePackageHelpers(t *testing.T) {
	tests := []struct {
		name  string
		files []ir.SurfaceSourceFile
		want  float64
	}{
		{
			name: "cross-file functions, structs, initializers and import aliases",
			files: []ir.SurfaceSourceFile{
				{Path: "main.go", Content: []byte(`package handlers
func Result() float64 { return length(vec2{3, 4}) + float64(len(label())) + padding }
`)},
				{Path: "math.go", Content: []byte(`package handlers
import tool "math"
func length(v vec2) float64 { return root(v.X*v.X + v.Y*v.Y) }
func root(n float64) float64 { return tool.Sqrt(n) }
`)},
				{Path: "text.go", Content: []byte(`package handlers
import tool "strings"
func label() string { return tool.ToUpper("coast") }
`)},
				{Path: "model.go", Content: []byte(`package handlers
type vec2 struct { X, Y float64 }
var padding = root(4)
`)},
			},
			want: 12,
		},
		{
			name: "later file retains its callable functions",
			files: []ir.SurfaceSourceFile{
				{Path: "first.go", Content: []byte("package handlers\nfunc First() int { return 1 }\n")},
				{Path: "second.go", Content: []byte("package handlers\nfunc Result() int { return twice(21) }\nfunc twice(n int) int { return n * 2 }\n")},
			},
			want: 42,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := LowerToBytecode(&ir.SurfaceProgram{Name: "Package", SourceFiles: tt.files}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(res.JSONPath)
			if err != nil {
				t.Fatal(err)
			}
			var p program.Program
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			machine := vm.NewVM(&p, nil)
			vm.InitSignals(machine, &p)
			nativeDir := t.TempDir()
			for _, file := range tt.files {
				source := strings.Replace(string(file.Content), "package handlers", "package main", 1)
				if err := os.WriteFile(filepath.Join(nativeDir, file.Path), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for name, source := range map[string]string{
				"go.mod":    "module native.test/packagecheck\n\ngo 1.26\n",
				"native.go": "package main\nimport \"fmt\"\nfunc main(){fmt.Println(Result())}\n",
			} {
				if err := os.WriteFile(filepath.Join(nativeDir, name), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("go", "run", ".")
			command.Dir = nativeDir
			command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("compiled Go: %v\n%s", err, output)
			}
			native, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
			if err != nil || native != tt.want {
				t.Fatalf("compiled Go result = %s, want %v: %v", output, tt.want, err)
			}
			for _, handler := range p.Handlers {
				if handler.Name == "Result" {
					if got := machine.EvalWithFrame(handler.Body[0]).Number(); got != tt.want {
						t.Fatalf("Result() = %v, want %v; diagnostics: %v", got, tt.want, machine.Diagnostics())
					}
					if diagnostics := machine.Diagnostics(); len(diagnostics) != 0 {
						t.Fatalf("VM diagnostics: %v", diagnostics)
					}
					return
				}
			}
			t.Fatal("missing Result handler")
		})
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
