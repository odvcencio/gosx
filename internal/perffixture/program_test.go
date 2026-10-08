package perffixture

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/engine"
)

func TestPerfFixtureSharedProgramProducesDrawableFrame(t *testing.T) {
	data, err := SharedProgram()
	if err != nil {
		t.Fatal(err)
	}
	p, err := engine.DecodeProgramJSON(data)
	if err != nil || p.Name != "BudgetEngine" || len(p.EngineNodes) != 2 {
		t.Fatal("engine program is not a camera and cube", err)
	}
	b := bridge.New()
	commands, err := b.HydrateEngine("fixture", "BudgetEngine", `{"background":"#18212b"}`, data, "json")
	if err != nil || len(commands) == 0 {
		t.Fatal("shared VM did not hydrate the app program", err)
	}
	if _, err := b.TickEngine("fixture"); err != nil {
		t.Fatal(err)
	}
	frame, err := b.RenderEngine("fixture", 360, 240, 0)
	if err != nil || frame.ObjectCount != 1 || frame.VertexCount == 0 || len(frame.Positions) == 0 {
		t.Fatalf("program did not produce drawable geometry: objects=%d vertices=%d: %v", frame.ObjectCount, frame.VertexCount, err)
	}
	b.DisposeEngine("fixture")
	if _, err := b.RenderEngine("fixture", 360, 240, 0); err == nil {
		t.Fatal("disposed fixture retained a live VM")
	}
	again, err := SharedProgram()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("program generation is not deterministic", err)
	}
}

func TestPerfFixtureAppFactoryRunsAndDisposes(t *testing.T) {
	source, err := ProgramSource("js")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "engine.js")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "testdata/engine-test.cjs", file)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("app factory interaction/disposal: %s: %v", out, err)
	}
}

func TestPerfFixtureProgramSourcesAreAuthoredAndClosed(t *testing.T) {
	for _, kind := range []string{"js", "go-wasm"} {
		source, err := ProgramSource(kind)
		if err != nil || len(source) == 0 || !bytes.Contains(source, []byte("BudgetEngine")) {
			t.Fatal("actual app program source missing", err)
		}
		source[0] = 0
		again, err := ProgramSource(kind)
		if err != nil || again[0] == 0 {
			t.Fatal("caller altered embedded app source", err)
		}
	}
	if _, err := ProgramSource("private-kind"); err == nil || strings.Contains(err.Error(), "private-kind") {
		t.Fatal("unknown program accepted or echoed", err)
	}
}

func TestPerfFixtureGoWASMModuleRegistersInteractsAndDisposes(t *testing.T) {
	source, err := ProgramSource("go-wasm")
	if err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	file := filepath.Join(scratch, "main.go")
	module := filepath.Join(scratch, "engine.wasm")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", module, file)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GOOS=") && !strings.HasPrefix(value, "GOARCH=") && !strings.HasPrefix(value, "GOWORK=") {
			build.Env = append(build.Env, value)
		}
	}
	build.Env = append(build.Env, "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile actual Go-WASM fixture: %s: %v", out, err)
	}
	probe := exec.CommandContext(ctx, "node", "testdata/go-wasm-test.cjs", filepath.Join(runtime.GOROOT(), "lib/wasm/wasm_exec.js"), module)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("actual Go-WASM fixture interaction/disposal: %s: %v", out, err)
	}
}
