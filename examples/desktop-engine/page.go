package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/server"
)

//go:embed panel.gsx
var panelSource []byte

// newHandler serves the page, the island program and the GoSX runtime assets
// from runtimeRoot (a directory with build/ from `gosx build-runtime build`,
// or a repository checkout).
func newHandler(runtimeRoot string) (http.Handler, error) {
	prog, err := compilePanel(runtimeRoot)
	if err != nil {
		return nil, err
	}
	programJSON, err := program.EncodeJSON(prog)
	if err != nil {
		return nil, err
	}
	app := server.New()
	app.SetRuntimeRoot(runtimeRoot)
	app.Mount("/app/Panel.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(programJSON)
	}))
	app.Route("/", func(*http.Request) gosx.Node { return page(prog, runtimeRoot) })
	return app.Build(), nil
}

// runtimeAsset describes the island runtime that the server will serve at
// /gosx/runtime.wasm; the page loader verifies it against this hash. Without
// the file (before build-runtime has run) the page has no hash and its island
// does not hydrate.
func runtimeAsset(root string) (string, int64) {
	data, err := os.ReadFile(filepath.Join(root, "build", "gosx-runtime.wasm"))
	if err != nil {
		return "", 0
	}
	return buildmanifest.ContentHash(data), int64(len(data))
}

func compilePanel(dir string) (*program.Program, error) {
	irProg, err := gosx.Compile(panelSource)
	if err != nil {
		return nil, fmt.Errorf("compile panel.gsx: %w", err)
	}
	irProg.Dir = dir
	irProg.PackagePath = "example.com/desktop-engine"
	for i, c := range irProg.Components {
		if c.IsIsland {
			return ir.LowerIsland(irProg, i)
		}
	}
	return nil, fmt.Errorf("panel.gsx has no island")
}

// page renders the island and the engine mount. The engine is declared as a
// worker engine: in a browser it would run as a WASM worker from WASMPath. In
// the desktop host the page runtime finds the native engine registered under
// the same component name and attaches to it instead, so the island is the same.
func page(prog *program.Program, runtimeRoot string) gosx.Node {
	r := island.NewRenderer("desktop-engine")
	// The island runtime (WASM) hydrates the island; the engine needs none of it.
	hash, size := runtimeAsset(runtimeRoot)
	r.SetRuntime("/gosx/runtime.wasm", hash, size)
	r.SetProgramAsset("Panel", "/app/Panel.json", "json", "")
	cfg := engine.Config{
		Name: component, Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, Thread: engine.ThreadWorker,
		MountID: mountID, WASMPath: "/engines/counter.wasm", Props: json.RawMessage(`{}`),
	}
	body := gosx.Fragment(
		r.RenderIslandFromProgram(prog, struct{}{}),
		r.RenderEngine(cfg, gosx.Node{}),
	)
	return server.HTMLDocument(&server.DocumentContext{
		Title: "GoSX native engine",
		Head: gosx.Fragment(
			gosx.RawHTML(`<style>`+strings.TrimSpace(css)+`</style>`),
			r.PageHead(),
		),
		Body: body,
	})
}

const css = `
body{margin:0;padding:clamp(16px,4vw,32px);background:#10151c;color:#dde6f0;font:16px/1.5 "Segoe UI",system-ui,sans-serif}
.panel{max-width:520px;margin:0 auto}
b{font-variant-numeric:tabular-nums}
button{font:inherit;min-height:44px;padding:6px 14px;margin-right:8px}
`
