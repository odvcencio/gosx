// Example counter runs the standalone Counter island demo.
//
// Build: go run ./cmd/gosx build-runtime build
// Run: go run ./examples/counter
// Visit: http://localhost:8080
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	if os.Getenv("GOSX_DEV") != "1" {
		for _, name := range []string{"gosx-runtime.wasm", "wasm_exec.js"} {
			if _, err := os.Stat(filepath.Join(filepath.Dir(thisFile), "../..", "build", name)); err != nil {
				log.Fatal("missing island runtime; run `go run ./cmd/gosx build-runtime build` from the repository root, then configure app.SetRuntimeRoot(repositoryRoot)")
			}
		}
	}
	app, err := newApp(filepath.Dir(thisFile))
	if err != nil {
		log.Fatal(err)
	}

	port := getenv("PORT", "8080")
	fmt.Printf("GoSX counter example running at http://localhost:%s\n", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

// newApp compiles the island from root/counter.gsx and returns a server that
// renders it. The test uses it without opening a port.
func newApp(root string) (*server.App, error) {
	counterProgram, counterJSON, err := compileIsland(filepath.Join(root, "counter.gsx"), "Counter")
	if err != nil {
		return nil, fmt.Errorf("compile counter island: %w", err)
	}

	app := server.New()
	runtimeRoot := filepath.Join(root, "../..")
	if os.Getenv("GOSX_DEV") == "1" {
		runtimeRoot = os.Getenv("GOSX_APP_ROOT")
	}
	app.SetRuntimeRoot(runtimeRoot)
	var runtimeHash string
	var runtimeSize int64
	if os.Getenv("GOSX_DEV") != "1" {
		if data, err := os.ReadFile(filepath.Join(root, "../..", "build", "gosx-runtime.wasm")); err == nil {
			runtimeHash, runtimeSize = buildmanifest.ContentHash(data), int64(len(data))
		}
	}
	app.SetPublicDir(root)
	app.Mount("/gosx/islands/Counter.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write(counterJSON)
	}))
	app.Route("/", func(r *http.Request) gosx.Node {
		return counterPage(counterProgram, queryCount(r), runtimeHash, runtimeSize)
	})
	return app, nil
}

// initialProps mirrors CounterProps in counter.gsx. The island reads its
// starting count from the Initial field.
type initialProps struct {
	Initial int
}

// compileIsland compiles a .gsx file and returns the named island's program and
// its JSON wire form: the same Compile, LowerIsland, EncodeJSON steps that
// `gosx dev` runs on each island edit.
func compileIsland(path, component string) (*program.Program, []byte, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	irProg, err := gosx.Compile(source)
	if err != nil {
		return nil, nil, fmt.Errorf("compile %s: %w", path, err)
	}
	for i, comp := range irProg.Components {
		if !comp.IsIsland || comp.Name != component {
			continue
		}
		isl, err := ir.LowerIsland(irProg, i)
		if err != nil {
			return nil, nil, fmt.Errorf("lower island %s: %w", component, err)
		}
		data, err := program.EncodeJSON(isl)
		if err != nil {
			return nil, nil, fmt.Errorf("encode island %s: %w", component, err)
		}
		return isl, data, nil
	}
	return nil, nil, fmt.Errorf("island %q not found in %s", component, path)
}

func queryCount(r *http.Request) int {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	return count
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func counterPage(counterProgram *program.Program, count int, runtimeHash string, runtimeSize int64) gosx.Node {
	islands := island.NewRenderer("counter")
	if runtimeHash != "" {
		islands.SetRuntime("/gosx/runtime.wasm", runtimeHash, runtimeSize)
	}
	islands.SetProgramAsset("Counter", "/gosx/islands/Counter.json", "json", "")

	body := gosx.El("main", gosx.Attrs(gosx.Attr("class", "page-shell")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "counter-panel")),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "eyebrow")), gosx.Text("Island")),
			gosx.El("h1", gosx.Text("GoSX Counter")),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "about")),
				gosx.Text("The server renders the first count. A small compiled program takes over in the browser. Without JavaScript, plain links take its place."),
			),
			islands.RenderIslandFromProgram(counterProgram, initialProps{Initial: count}),
			gosx.RawHTML(fmt.Sprintf(
				`<noscript><div class="counter no-js-counter"><a href="/?count=%d">-</a><span class="count">%d</span><a href="/?count=%d">+</a></div></noscript>`,
				count-1,
				count,
				count+1,
			)),
			gosx.El("a", gosx.Attrs(gosx.Attr("class", "docs-link"), gosx.Attr("href", "https://gosx.m31labs.dev/docs/islands")), gosx.Text("How islands work")),
		),
	)

	return server.HTMLDocument(&server.DocumentContext{
		Title: "GoSX Counter",
		Head: gosx.Fragment(
			gosx.RawHTML(`<link rel="stylesheet" href="/counter.css">`),
			islands.PageHead(),
		),
		Body: body,
	})
}
