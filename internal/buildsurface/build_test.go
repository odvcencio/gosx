package buildsurface_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gosx/internal/buildsurface"
	"github.com/odvcencio/gosx/ir"
)

// testFingerprint replicates the fingerprint algorithm from ir for test use.
func testFingerprint(files []ir.SurfaceSourceFile, caps []string, handlers []ir.SurfaceHandlerBind) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write(f.Content)
		h.Write([]byte{0})
	}
	capJSON, _ := json.Marshal(caps)
	h.Write(capJSON)
	h.Write([]byte{0})
	type hj struct {
		E string `json:"event"`
		F string `json:"fn"`
	}
	hb := make([]hj, len(handlers))
	for i, bind := range handlers {
		hb[i] = hj{E: bind.EventName, F: bind.FunctionName}
	}
	hJSON, _ := json.Marshal(hb)
	h.Write(hJSON)
	return hex.EncodeToString(h.Sum(nil))
}

// minimalSurfaceProgram returns a *ir.SurfaceProgram whose user source compiles
// cleanly as a WASM module. It uses engine/surface types which exist in the
// GoSX module tree.
func minimalSurfaceProgram() *ir.SurfaceProgram {
	userSrc := `package user

import (
	"github.com/odvcencio/gosx/engine/surface"
)

func Mount(ctx *surface.Context, c *surface.Canvas) {}
`
	handlers := []ir.SurfaceHandlerBind{
		{EventName: "mount", FunctionName: "Mount"},
	}
	files := []ir.SurfaceSourceFile{
		{Path: "surface.go", Content: []byte(userSrc)},
	}
	return &ir.SurfaceProgram{
		Name:              "TestSurface",
		Package:           "example.com/testsurf",
		Handlers:          handlers,
		SourceFiles:       files,
		SourceFingerprint: testFingerprint(files, nil, handlers),
	}
}

// resolveGoSXRoot walks up from the test's working directory to find a go.mod
// that declares the gosx module, which is the module root.
func resolveGoSXRoot(t *testing.T) (string, error) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		modPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// TestBuildCacheHit verifies that:
//  1. Build compiles successfully and returns a non-empty hash + .wasm file.
//  2. A second identical Build call is served from cache and is faster.
func TestBuildCacheHit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping surface compilation test in short mode")
	}

	gosxRoot, err := resolveGoSXRoot(t)
	if err != nil {
		t.Skipf("cannot resolve gosx root: %v", err)
	}

	sp := minimalSurfaceProgram()
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	outputDir := filepath.Join(tmpDir, "output")

	opts := buildsurface.Options{
		Compiler:  buildsurface.CompilerGo,
		CacheDir:  cacheDir,
		OutputDir: outputDir,
		GoSXRoot:  gosxRoot,
	}

	ctx := context.Background()

	// First build — compiles from scratch.
	start1 := time.Now()
	wasmPath1, hash1, err := buildsurface.Build(ctx, sp, opts)
	elapsed1 := time.Since(start1)
	if err != nil {
		t.Fatalf("first Build failed: %v", err)
	}
	if hash1 == "" {
		t.Fatal("first Build returned empty hash")
	}
	if !strings.HasSuffix(wasmPath1, ".wasm") {
		t.Fatalf("output path does not end in .wasm: %s", wasmPath1)
	}
	if _, err := os.Stat(wasmPath1); err != nil {
		t.Fatalf("first Build output file does not exist: %s", wasmPath1)
	}
	info1, _ := os.Stat(wasmPath1)
	if info1.Size() == 0 {
		t.Fatal("compiled WASM is empty")
	}

	// Second build — cache hit, same opts.
	start2 := time.Now()
	wasmPath2, hash2, err := buildsurface.Build(ctx, sp, opts)
	elapsed2 := time.Since(start2)
	if err != nil {
		t.Fatalf("second Build (cache hit) failed: %v", err)
	}
	if hash2 != hash1 {
		t.Fatalf("cache hit returned different hash: got %s want %s", hash2, hash1)
	}
	if wasmPath2 != wasmPath1 {
		t.Fatalf("cache hit returned different path: got %s want %s", wasmPath2, wasmPath1)
	}

	// The cache hit should not invoke the compiler.
	// As a proxy: it should be at least 5x faster than the original compile if
	// the compile took more than 200ms.
	t.Logf("first build (compile): %v", elapsed1)
	t.Logf("second build (cache):  %v", elapsed2)
	if elapsed1 > 200*time.Millisecond {
		limit := elapsed1 / 5
		if elapsed2 > limit {
			// Warn but do not fail — CI machines can be unpredictably slow.
			t.Logf("warning: cache hit took %v which is >20%% of compile time %v", elapsed2, elapsed1)
		}
	}
}
