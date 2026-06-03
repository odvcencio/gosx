// Command hotswap is the Phase-0 hot-swap demo for `gosx dev`.
//
// It renders a single interactive island whose program is compiled from
// counter.gsx in this directory. Run it under the dev server:
//
//	gosx dev ./examples/hotswap
//
// then open the printed URL, click the counter up a few times, and edit
// counter.gsx (the label, the step, a class). `gosx dev` recompiles just that
// island and pushes the new bytecode over its dev socket; the running island
// hot-swaps in place — signal state (your current count) is preserved and the
// page does NOT reload. That program/patch delivery over the dev socket is the
// Track B payoff (see dev/hotswap.go and the injected client snippet in
// dev/server.go).
//
// Run standalone (without the dev server) and it still works as a plain island
// demo — it serves its own compiled program at /gosx/islands/Counter.json. Under
// `gosx dev`, the dev server serves that path from its staged build instead and
// owns the hot-swap.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(thisFile)

	counterProgram, counterJSON, err := compileIsland(filepath.Join(root, "counter.gsx"), "Counter")
	if err != nil {
		log.Fatalf("compile counter island: %v", err)
	}
	log.Printf("island %s compiled: %d nodes, %d exprs, %d bytes JSON",
		counterProgram.Name, len(counterProgram.Nodes), len(counterProgram.Exprs), len(counterJSON))

	app := server.New()
	app.SetPublicDir(root)
	// Standalone fallback: serve the compiled program directly. `gosx dev`
	// shadows /gosx/islands/* with its staged, hot-swappable build, so this
	// mount only matters when the demo is run without the dev server.
	app.Mount("/gosx/islands/Counter.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write(counterJSON)
	}))
	app.Route("/", func(_ *http.Request) gosx.Node {
		return counterPage(counterProgram)
	})

	port := getenv("PORT", "8080")
	fmt.Printf("GoSX hot-swap demo running at http://localhost:%s\n", port)
	fmt.Println("Tip: run `gosx dev ./examples/hotswap`, then edit counter.gsx and watch the island swap without a reload.")
	log.Fatal(app.ListenAndServe(":" + port))
}

// compileIsland compiles a .gsx file and returns the named island's program
// plus its JSON wire form. It is the same Compile -> LowerIsland -> EncodeJSON
// pipeline `gosx dev` runs on every island edit (see dev/hotswap.go), so the
// program this demo server-renders is byte-identical to the one the dev socket
// hot-swaps in.
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

func counterPage(counterProgram *program.Program) gosx.Node {
	islands := island.NewRenderer("hotswap")
	islands.SetProgramAsset("Counter", "/gosx/islands/Counter.json", "json", "")

	body := gosx.El("main", gosx.Attrs(gosx.Attr("class", "page-shell")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "panel")),
			gosx.El("h1", gosx.Text("GoSX hot-swap demo")),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "hint")),
				gosx.Text("Click the counter, then edit counter.gsx and save. The island swaps in place — your count survives, no page reload."),
			),
			islands.RenderIslandFromProgram(counterProgram, map[string]int{}),
		),
	)

	return server.HTMLDocument(
		"GoSX hot-swap demo",
		gosx.Fragment(
			gosx.RawHTML(`<meta name="viewport" content="width=device-width, initial-scale=1">`),
			gosx.RawHTML(`<link rel="stylesheet" href="/hotswap.css">`),
			islands.PageHead(),
		),
		body,
	)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
